package download

import (
	"context"
	"errors"
	"github.com/screwys/igloo/internal/model"
	"log"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	maxHTTPVideoDownloadBytes       int64 = 4 << 30
	defaultHTTPVideoDownloadTimeout       = 2 * time.Hour
)

// Opts configures a Download call.
type Opts struct {
	OutputDir          string // Directory to write files into.
	ID                 string // Used in output filename when set.
	Cookies            string // Path to cookies file (for yt-dlp).
	CookiesFromBrowser string // Browser name for --cookies-from-browser (e.g. "firefox").
	Format             string // yt-dlp -f format string (overrides default FormatSort when set).
	Subtitles          bool   // Download English subtitles as VTT.
}

// Downloader is the unified entry point that routes to the correct backend.
// Two download engines: yt-dlp (videos) and gallery-dl (slideshows, fallback).
// HTTP is a utility for direct CDN URLs (avatars, Twitter media).
type Downloader struct {
	YtDlp     *YtDlpWrapper
	GalleryDL *GalleryDLWrapper
	HTTP      *HTTPDownloader
	sink      OperationSink
}

// NewDownloader returns a Downloader with default clients.
func NewDownloader(cookiesDir string) *Downloader {
	d := &Downloader{
		YtDlp:     &YtDlpWrapper{CookiesDir: cookiesDir},
		GalleryDL: &GalleryDLWrapper{Runner: CommandRunner{}},
		HTTP:      NewHTTPDownloader(),
	}
	return d
}

func (d *Downloader) SetOperationSink(sink OperationSink) {
	d.sink = sink
	if d.YtDlp != nil {
		d.YtDlp.OperationSink = sink
	}
	if d.GalleryDL != nil {
		d.GalleryDL.OperationSink = sink
	}
}

// Download routes the request to the appropriate backend and returns
// the paths of all files that were written.
//
// Routing:
//  1. TikTok/Instagram URL → gallery-dl first for slideshows/reels
//  2. Direct CDN (pbs.twimg.com, video.twimg.com, photo/image) → HTTP
//  3. Default → yt-dlp
func (d *Downloader) Download(ctx context.Context, rawURL string, mediaType string, opts Opts) ([]string, error) {
	start := time.Now()
	platform := platformFromURL(rawURL)
	tool := "yt-dlp"
	if IsTikTokURL(rawURL) || IsInstagramURL(rawURL) {
		tool = "gallery-dl/yt-dlp"
	} else if isDirectMedia(rawURL, mediaType) {
		tool = "http"
	}
	var paths []string
	var err error
	defer func() {
		files, bytes := summarizePaths(paths)
		recordOperation(ctx, d.sink, model.DownloaderOperation{
			Operation:   "media.download",
			Platform:    platform,
			Subject:     subjectForURL(rawURL),
			Tool:        tool,
			StartedAtMs: start.UnixMilli(),
			EndedAtMs:   time.Now().UnixMilli(),
			Status:      statusForError(err),
			ErrorKind:   ClassifyError(err, nil),
			Error:       errorString(err, nil),
			CookieLabel: CookieLabel(opts.Cookies, opts.CookiesFromBrowser),
			FileCount:   files,
			MediaCount:  files,
			Bytes:       bytes,
		})
	}()
	if IsTikTokURL(rawURL) {
		paths, err = d.downloadTikTok(ctx, rawURL, opts)
		return paths, err
	}
	if IsInstagramURL(rawURL) {
		paths, err = d.downloadGalleryDLFirst(ctx, canonicalInstagramURL(rawURL), opts)
		return paths, err
	}
	if isDirectMedia(rawURL, mediaType) {
		filename := opts.ID + mediaExtFromURL(rawURL)
		httpOpts := directMediaHTTPOptions(rawURL, mediaType)
		var p string
		p, err = d.HTTP.DownloadFileWithOptions(ctx, rawURL, opts.OutputDir, filename, httpOpts)
		if err != nil {
			// Try lower quality variants for twimg photos that 403/404 on orig/large/etc.
			var httpErr *HTTPStatusError
			tryURL := rawURL
			for errors.As(err, &httpErr) && (httpErr.StatusCode == 404 || httpErr.StatusCode == 403) && isTwimgPhoto(tryURL) {
				fallbackURL, ok := nextTwimgQuality(tryURL)
				if !ok {
					break
				}
				log.Printf("[download] twimg 404 on %s, trying %s", tryURL, fallbackURL)
				tryURL = fallbackURL
				p, err = d.HTTP.DownloadFileWithOptions(ctx, fallbackURL, opts.OutputDir, filename, httpOpts)
			}
			if err != nil {
				return nil, err
			}
		}
		paths = []string{p}
		return paths, nil
	}
	paths, err = d.YtDlp.Download(ctx, rawURL, opts)
	return paths, err
}

func directMediaHTTPOptions(rawURL, mediaType string) HTTPDownloadOptions {
	if !isDirectVideoMedia(rawURL, mediaType) {
		return HTTPDownloadOptions{}
	}
	return HTTPDownloadOptions{
		MaxBytes: maxHTTPVideoDownloadBytes,
		Timeout:  defaultHTTPVideoDownloadTimeout,
	}
}

// downloadTikTok handles TikTok URLs with slideshow detection.
// gallery-dl is tried first — it handles slideshows natively (images + audio)
// with clean 1-based numbering. For regular videos, falls back to yt-dlp.
func (d *Downloader) downloadTikTok(ctx context.Context, rawURL string, opts Opts) ([]string, error) {
	// gallery-dl handles TikTok slideshows natively (images + audio).
	// It fails fast on regular videos, so there's no significant overhead.
	gdlPaths, gdlErr := d.GalleryDL.Download(ctx, rawURL, opts.OutputDir, opts.ID, opts.Cookies)
	if gdlErr == nil && len(gdlPaths) > 0 {
		return gdlPaths, nil
	}

	// If gallery-dl reached the post and got a permanent 404/403 (deleted,
	// private, geo-restricted), propagate it. yt-dlp would just emit a
	// misleading "IP blocked" error and keep the job looping forever.
	var httpErr *HTTPStatusError
	if errors.As(gdlErr, &httpErr) && (httpErr.StatusCode == 404 || httpErr.StatusCode == 403) {
		return nil, gdlErr
	}

	// gallery-dl failed or returned nothing — it's a regular video. Use yt-dlp.
	return d.YtDlp.Download(ctx, rawURL, opts)
}

func (d *Downloader) downloadGalleryDLFirst(ctx context.Context, rawURL string, opts Opts) ([]string, error) {
	gdlPaths, gdlErr := d.GalleryDL.Download(ctx, rawURL, opts.OutputDir, opts.ID, opts.Cookies)
	if gdlErr == nil && len(gdlPaths) > 0 {
		return gdlPaths, nil
	}
	var httpErr *HTTPStatusError
	if errors.As(gdlErr, &httpErr) && (httpErr.StatusCode == 404 || httpErr.StatusCode == 403) {
		return nil, gdlErr
	}
	ytPaths, ytErr := d.YtDlp.Download(ctx, rawURL, opts)
	if ytErr == nil && len(ytPaths) > 0 {
		return ytPaths, nil
	}
	return ytPaths, ytErr
}

// isDirectMedia reports whether the URL or mediaType indicates media that
// should be fetched directly via HTTP rather than via yt-dlp.
// This covers Twitter CDN photos (pbs.twimg.com) and videos (video.twimg.com),
// as well as any URL with mediaType "photo" or "image".
func isDirectMedia(rawURL, mediaType string) bool {
	mt := strings.ToLower(mediaType)
	host, path, ok := httpURLParts(rawURL)
	if mt == "photo" || mt == "image" {
		return ok
	}
	if hostMatches(host, "pbs.twimg.com") && strings.HasPrefix(path, "/media/") {
		return true
	}
	if hostMatches(host, "video.twimg.com") {
		return true
	}
	return false
}

func isDirectVideoMedia(rawURL, mediaType string) bool {
	mt := strings.ToLower(mediaType)
	if mt == "video" || mt == "gif" || mt == "animated_gif" {
		return true
	}
	host, _, ok := httpURLParts(rawURL)
	return ok && hostMatches(host, "video.twimg.com")
}

// mediaExtFromURL returns the file extension for the given media URL.
// Detects .mp4, .png, and .webp from the URL path; defaults to .jpg.
func mediaExtFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ".jpg"
	}
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".mp4":
		return ".mp4"
	case ".png":
		return ".png"
	case ".webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

// twimgQualities is the fallback chain for pbs.twimg.com photo sizes.
var twimgQualities = []string{"orig", "large", "medium", "small"}

// isTwimgPhoto reports whether the URL is a pbs.twimg.com photo.
func isTwimgPhoto(rawURL string) bool {
	host, path, ok := httpURLParts(rawURL)
	return ok && hostMatches(host, "pbs.twimg.com") && strings.HasPrefix(path, "/media/")
}

// nextTwimgQuality returns the URL with the next lower quality level.
// E.g. name=orig → name=large, name=large → name=medium.
func nextTwimgQuality(rawURL string) (string, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}
	q := u.Query()
	current := q.Get("name")
	for i, qual := range twimgQualities {
		if qual == current && i+1 < len(twimgQualities) {
			q.Set("name", twimgQualities[i+1])
			u.RawQuery = q.Encode()
			return u.String(), true
		}
	}
	return "", false
}
