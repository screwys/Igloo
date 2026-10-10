package web

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/screwys/igloo/internal/components"
	"github.com/screwys/igloo/internal/download"
)

type youtubeStreamResource struct {
	url       string
	headers   map[string]string
	chunkSize int64
	relative  bool
	template  bool
	templates []string
	children  []string
}

type youtubeStreamSession struct {
	id             string
	videoID        string
	info           *download.PlaybackInfo
	manifestType   string
	preferIndexed  bool
	indexed        bool
	textTracks     []components.StreamTextTrack
	manifest       []byte
	rootResource   string
	client         *http.Client
	mu             sync.Mutex
	resources      map[string]youtubeStreamResource
	resourceIDs    map[string]string
	nextResourceID uint64
	lastUsed       time.Time
	preparedAt     time.Time
	liveDirectory  string
	liveCancel     context.CancelFunc
}

func (s *Server) registerYouTubeStreamRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/youtube/{videoID}/stream", s.handleYouTubeStreamStart)
	mux.HandleFunc("POST /api/youtube/{videoID}/captions", s.handleYouTubeCaptionTracks)
	mux.HandleFunc("GET /api/youtube/streams/{sessionID}/manifest", s.handleYouTubeStreamManifest)
	mux.HandleFunc("GET /api/youtube/streams/{sessionID}/media/{resourceID}/{rest...}", s.handleYouTubeStreamMedia)
}

func (s *Server) handleYouTubeStreamStart(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	var body struct {
		PreferIndexed bool `json:"prefer_indexed"`
		ForceFresh    bool `json:"force_fresh"`
	}
	if err := decodeJSON(w, r, &body); err != nil && !errors.Is(err, io.EOF) {
		if requestBodyTooLarge(err) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": requestBodyTooLargeMessage})
			return
		}
		writeJSONError(w, http.StatusBadRequest, "invalid_stream_request", "Invalid stream request")
		return
	}
	videoID := strings.TrimSpace(r.PathValue("videoID"))
	if videoID == "" {
		writeJSONError(w, 400, "invalid_video", "Video required")
		return
	}
	if owner, ok := s.videoAssetOwner(videoID); ok {
		if file := s.canonicalStreamAsset(owner); file != nil {
			writeJSON(w, 200, map[string]any{"success": true, "video_id": videoID, "player_url": "/player/" + url.PathEscape(videoID),
				"media_url": "/api/videos/" + url.PathEscape(videoID) + "/stream", "media_type": file.asset.ContentType,
				"indexed": false, "text_tracks": []components.StreamTextTrack{}})
			return
		}
	}
	if !body.ForceFresh && s.cfg.PlatformEnabled("youtube") {
		if session := s.recentYouTubeStream(videoID, body.PreferIndexed); session != nil {
			writeYouTubeStreamResponse(w, session)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	info, err := s.workers.ResolveYouTubePlayback(ctx, videoID)
	if err != nil {
		if tool, _ := download.ErrorOperationContext(err); tool == "" {
			writeJSONError(w, 500, "stream_storage", "Could not prepare the YouTube stream")
			return
		}
		if download.ClassifyFailure(err, nil, 0).Kind == download.ErrorKindAuth {
			writeJSONError(w, 502, "youtube_auth_required", "YouTube authentication failed.")
			return
		}
		writeJSONError(w, 502, "stream_extraction", "Could not find a playable YouTube stream")
		return
	}
	client := *download.NewHTTPDownloader().Client
	client.Timeout = 0
	session := &youtubeStreamSession{id: rand.Text(), videoID: info.ID, info: info, client: &client,
		preferIndexed: body.PreferIndexed,
		resources:     make(map[string]youtubeStreamResource), resourceIDs: make(map[string]string), lastUsed: time.Now()}
	if err := session.prepare(ctx); err != nil {
		writeJSONError(w, 502, "stream_prepare", "Could not prepare the YouTube stream")
		return
	}
	s.storeYouTubeStream(session)
	writeYouTubeStreamResponse(w, session)
}

func writeYouTubeStreamResponse(w http.ResponseWriter, session *youtubeStreamSession) {
	writeJSON(w, 200, map[string]any{"success": true, "video_id": session.videoID,
		"player_url":   "/player/" + url.PathEscape(session.videoID) + "?stream=" + session.id,
		"manifest_url": "/api/youtube/streams/" + session.id + "/manifest", "manifest_type": session.manifestType, "session_id": session.id,
		"indexed": session.indexed, "audio_language": session.audioLanguage(), "text_tracks": session.textTracks})
}

func (session *youtubeStreamSession) audioLanguage() string {
	var preferred *download.PlaybackFormat
	for i := range session.info.Formats {
		format := &session.info.Formats[i]
		if format.Language == "" || format.AudioCodec == "" || format.AudioCodec == "none" {
			continue
		}
		if preferred == nil || format.LanguagePreference > preferred.LanguagePreference {
			preferred = format
		}
	}
	if preferred == nil {
		return ""
	}
	return preferred.Language
}

func (s *Server) recentYouTubeStream(videoID string, preferIndexed bool) *youtubeStreamSession {
	status, err := s.db.YouTubeBroadcastLiveStatus(videoID)
	if err != nil {
		return nil
	}
	s.youtubeStreamsMu.Lock()
	defer s.youtubeStreamsMu.Unlock()
	var recent *youtubeStreamSession
	for _, session := range s.youtubeStreams {
		if status != "" && status != session.info.LiveStatus {
			continue
		}
		if session.videoID != videoID || session.preferIndexed != preferIndexed ||
			len(session.manifest) == 0 && session.rootResource == "" {
			continue
		}
		if time.Since(session.preparedAt) > 5*time.Minute {
			continue
		}
		if recent == nil || session.preparedAt.After(recent.preparedAt) {
			recent = session
		}
	}
	if recent != nil {
		recent.mu.Lock()
		recent.lastUsed = time.Now()
		recent.mu.Unlock()
	}
	return recent
}

func (s *Server) handleYouTubeCaptionTracks(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	videoID := strings.TrimSpace(r.PathValue("videoID"))
	if videoID == "" {
		writeJSONError(w, 400, "invalid_video", "Video required")
		return
	}
	downloader := s.workers.Downloader()
	if downloader == nil || downloader.YtDlp == nil || !s.cfg.PlatformEnabled("youtube") {
		writeJSONError(w, 503, "captions_unavailable", "Subtitles could not load")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	info, err := downloader.YtDlp.FetchPlayback(ctx, "https://www.youtube.com/watch?v="+url.QueryEscape(videoID), s.cookieOptsFor("youtube"))
	if err != nil {
		writeJSONError(w, 502, "caption_extraction", "Subtitles could not load")
		return
	}
	client := *download.NewHTTPDownloader().Client
	client.Timeout = 0
	session := &youtubeStreamSession{id: rand.Text(), videoID: info.ID, info: info, client: &client,
		resources: make(map[string]youtubeStreamResource), resourceIDs: make(map[string]string), lastUsed: time.Now()}
	session.textTracks = session.captionTracks()
	s.storeYouTubeStream(session)
	writeJSON(w, 200, map[string]any{"success": true, "text_tracks": session.textTracks})
}

func (s *Server) storeYouTubeStream(session *youtubeStreamSession) {
	s.youtubeStreamsMu.Lock()
	session.preparedAt = time.Now()
	if s.youtubeStreams == nil {
		s.youtubeStreams = make(map[string]*youtubeStreamSession)
	}
	for id, old := range s.youtubeStreams {
		old.mu.Lock()
		expired := time.Since(old.lastUsed) > 2*time.Hour
		old.mu.Unlock()
		if expired {
			delete(s.youtubeStreams, id)
		}
	}
	s.youtubeStreams[session.id] = session
	s.youtubeStreamsMu.Unlock()
}

func (s *Server) youtubeStream(id string) *youtubeStreamSession {
	s.youtubeStreamsMu.Lock()
	session := s.youtubeStreams[id]
	s.youtubeStreamsMu.Unlock()
	if session != nil {
		session.mu.Lock()
		session.lastUsed = time.Now()
		session.mu.Unlock()
	}
	return session
}

func (s *Server) handleYouTubeStreamManifest(w http.ResponseWriter, r *http.Request) {
	session := s.youtubeStream(r.PathValue("sessionID"))
	if session == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if len(session.manifest) > 0 {
		contentType := "application/dash+xml"
		if session.manifestType == "hls" {
			contentType = "application/vnd.apple.mpegurl"
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(session.manifest)
		return
	}
	session.serveResource(w, r, session.rootResource, "")
}

func (s *Server) handleYouTubeStreamMedia(w http.ResponseWriter, r *http.Request) {
	session := s.youtubeStream(r.PathValue("sessionID"))
	if session == nil {
		http.NotFound(w, r)
		return
	}
	session.serveResource(w, r, r.PathValue("resourceID"), r.PathValue("rest"))
}

const youtubeStreamChunkSize int64 = 10 << 20

func (session *youtubeStreamSession) addResource(rawURL string, headers map[string]string, options ...map[string]any) string {
	chunkSize := youtubeStreamChunkSize
	if len(options) > 0 {
		if size := download.PlaybackHTTPChunkSize(options[0]); size > 0 {
			chunkSize = min(chunkSize, size)
		}
	}
	id := session.registerResource(youtubeStreamResource{url: rawURL, headers: headers, chunkSize: chunkSize})
	return session.resourcePath(id)
}

func (session *youtubeStreamSession) resourcePath(id string) string {
	return "/api/youtube/streams/" + session.id + "/media/" + id + "/"
}

func streamResourceKey(resource youtubeStreamResource) string {
	key := resource.url
	if resource.relative {
		key = "relative:" + key
	}
	if resource.template {
		key = "template:" + key
	}
	return key
}

func (session *youtubeStreamSession) registerResource(resource youtubeStreamResource) string {
	session.mu.Lock()
	defer session.mu.Unlock()
	key := streamResourceKey(resource)
	id, exists := session.resourceIDs[key]
	if !exists {
		id = strconv.FormatUint(session.nextResourceID, 10)
		session.nextResourceID++
		session.resourceIDs[key] = id
		session.resources[id] = resource
	} else if old := session.resources[id]; resource.chunkSize < old.chunkSize {
		old.chunkSize = resource.chunkSize
		session.resources[id] = old
	}
	return id
}

func playbackHeaders(base, specific map[string]string) map[string]string {
	headers := make(map[string]string, len(base)+len(specific))
	for key, value := range base {
		headers[key] = value
	}
	for key, value := range specific {
		headers[key] = value
	}
	return headers
}

func (session *youtubeStreamSession) prepare(ctx context.Context) error {
	session.textTracks = []components.StreamTextTrack{}
	// Live and still-processing recordings use the source's real segment timeline.
	if session.info.IsLive || session.info.Metadata["live_status"] == "post_live" {
		return session.prepareUpstreamManifest()
	}
	if !session.preferIndexed {
		for _, format := range session.info.Formats {
			if format.ManifestURL != "" {
				return session.prepareUpstreamManifest()
			}
		}
	}
	type indexedFormat struct {
		format download.PlaybackFormat
		index  download.PlaybackIndex
		err    error
	}
	var formats []download.PlaybackFormat
	for _, format := range session.info.Formats {
		if format.URL == "" || format.Protocol != "https" && format.Protocol != "http" {
			continue
		}
		if format.Ext != "mp4" && format.Ext != "m4a" && format.Ext != "webm" {
			continue
		}
		if format.VideoCodec == "none" && format.AudioCodec == "none" {
			continue
		}
		format.Headers = playbackHeaders(session.info.Headers, format.Headers)
		formats = append(formats, format)
	}
	results := make([]indexedFormat, len(formats))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, format := range formats {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[i].err = ctx.Err()
				return
			}
			defer func() { <-sem }()
			index, err := download.ProbePlaybackIndex(ctx, session.client, format)
			results[i] = indexedFormat{format: format, index: index, err: err}
		}()
	}
	wg.Wait()
	var body strings.Builder
	videoCount, audioCount := 0, 0
	audioLanguage := session.audioLanguage()
	for _, result := range results {
		if result.err != nil {
			continue
		}
		format := result.format
		kind := "video"
		codec := format.VideoCodec
		if codec == "none" || codec == "" {
			kind, codec = "audio", format.AudioCodec
		}
		if kind == "video" {
			videoCount++
		} else {
			audioCount++
		}
		if format.AudioCodec != "none" && format.AudioCodec != "" && kind == "video" {
			audioCount++
			codec += "," + format.AudioCodec
		}
		container := "mp4"
		if format.Ext == "webm" {
			container = "webm"
		}
		fmt.Fprintf(&body, `<AdaptationSet contentType="%s" mimeType="%s/%s" lang="%s"><Representation id="%s" bandwidth="%d" codecs="%s"`, kind, kind, container, xmlString(format.Language), xmlString(format.ID), max(int(format.Bitrate*1000), 1), xmlString(codec))
		if kind == "video" {
			fmt.Fprintf(&body, ` width="%d" height="%d" frameRate="%s"`, format.Width, format.Height, strconv.FormatFloat(format.FPS, 'f', -1, 64))
		}
		if kind == "audio" && format.SampleRate > 0 {
			fmt.Fprintf(&body, ` audioSamplingRate="%d"`, format.SampleRate)
		}
		mediaURL := session.addResource(format.URL, format.Headers, format.DownloaderOptions)
		fmt.Fprintf(&body, `><BaseURL>%s</BaseURL><SegmentBase indexRange="%d-%d"><Initialization range="%d-%d"/></SegmentBase></Representation>`, xmlString(mediaURL), result.index.IndexStart, result.index.IndexEnd, result.index.InitializationStart, result.index.InitializationEnd)
		if kind == "audio" && audioLanguage != "" && format.Language == audioLanguage {
			body.WriteString(`<Role schemeIdUri="urn:mpeg:dash:role:2011" value="main"/>`)
		}
		body.WriteString(`</AdaptationSet>`)
	}
	if videoCount == 0 || audioCount == 0 {
		return session.prepareUpstreamManifest()
	}
	for _, track := range session.captionTracks() {
		role, suffix := "subtitle", ""
		if track.Automatic {
			role, suffix = "caption", "_auto"
		}
		fmt.Fprintf(&body, `<AdaptationSet contentType="text" mimeType="text/vtt" lang="%s"><Role schemeIdUri="urn:mpeg:dash:role:2011" value="%s"/><Label>%s</Label><Representation id="caption-%s%s"><BaseURL>%s</BaseURL></Representation></AdaptationSet>`, xmlString(track.Language), role, xmlString(track.Label), xmlString(track.Language), suffix, xmlString(track.URL))
	}
	session.indexed = true
	session.manifestType = "dash"
	session.manifest = []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="static" profiles="urn:mpeg:dash:profile:isoff-on-demand:2011" minBufferTime="PT1S" mediaPresentationDuration="PT%sS"><Period>%s</Period></MPD>`, strconv.FormatFloat(session.info.Duration, 'f', -1, 64), body.String()))
	return nil
}

func (session *youtubeStreamSession) captionTracks() []components.StreamTextTrack {
	tracks := []components.StreamTextTrack{}
	for _, source := range []struct {
		subtitles map[string][]download.PlaybackSubtitle
		automatic bool
	}{{session.info.Subtitles, false}, {session.info.AutomaticCaptions, true}} {
		for language, subtitles := range source.subtitles {
			for _, subtitle := range subtitles {
				if subtitle.Ext != "vtt" || subtitle.URL == "" {
					continue
				}
				label := subtitle.Name
				if label == "" {
					label = language
				}
				if source.automatic {
					label += " (auto)"
				}
				tracks = append(tracks, components.StreamTextTrack{
					URL:      session.addResource(subtitle.URL, session.info.Headers),
					Language: language, Label: label, Automatic: source.automatic,
				})
				break
			}
		}
	}
	sort.Slice(tracks, func(i, j int) bool {
		if tracks[i].Language != tracks[j].Language {
			return tracks[i].Language < tracks[j].Language
		}
		return !tracks[i].Automatic && tracks[j].Automatic
	})
	return tracks
}

func xmlString(value string) string {
	var out bytes.Buffer
	_ = xml.EscapeText(&out, []byte(value))
	return out.String()
}

func (session *youtubeStreamSession) prepareUpstreamManifest() error {
	for _, format := range session.info.Formats {
		manifestURL := format.ManifestURL
		if manifestURL == "" && strings.HasPrefix(format.Protocol, "m3u8") {
			manifestURL = format.URL
		}
		if manifestURL == "" {
			continue
		}
		kind := "dash"
		if strings.HasPrefix(format.Protocol, "m3u8") {
			kind = "hls"
		}
		session.manifestType = kind
		session.indexed = false
		resourceURL := session.addResource(manifestURL, playbackHeaders(session.info.Headers, format.Headers), format.DownloaderOptions)
		session.rootResource = strings.TrimSuffix(path.Base(strings.TrimSuffix(resourceURL, "/")), "/")
		session.textTracks = session.captionTracks()
		if kind == "hls" {
			session.prepareMuxedHLS(manifestURL)
		}
		return nil
	}
	return errors.New("no indexed video or segment manifest available")
}

func (session *youtubeStreamSession) prepareMuxedHLS(manifestURL string) {
	var formats []download.PlaybackFormat
	for _, format := range session.info.Formats {
		if format.ManifestURL != manifestURL || !strings.HasPrefix(format.Protocol, "m3u8") {
			continue
		}
		if format.Language == "" || format.VideoCodec == "" || format.VideoCodec == "none" || format.AudioCodec == "" || format.AudioCodec == "none" {
			return
		}
		formats = append(formats, format)
	}
	if len(formats) == 0 {
		return
	}
	// yt-dlp supplies the languages that muxed upstream manifests can omit.
	var body strings.Builder
	body.WriteString("#EXTM3U\n#EXT-X-VERSION:6\n")
	groups := make(map[string]string)
	var children []string
	audioLanguage := session.audioLanguage()
	for _, format := range formats {
		group, exists := groups[format.Language]
		if !exists {
			group = fmt.Sprintf("igloo-audio-%d", len(groups))
			groups[format.Language] = group
			isDefault := "NO"
			if format.Language == audioLanguage {
				isDefault = "YES"
			}
			fmt.Fprintf(&body, "#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=%q,NAME=%q,LANGUAGE=%q,DEFAULT=%s,AUTOSELECT=YES\n", group, format.Language, format.Language, isDefault)
		}
		fmt.Fprintf(&body, "#EXT-X-STREAM-INF:BANDWIDTH=%d,CODECS=%q,AUDIO=%q", max(int(format.Bitrate*1000), 1), format.VideoCodec+","+format.AudioCodec, group)
		if format.Width > 0 && format.Height > 0 {
			fmt.Fprintf(&body, ",RESOLUTION=%dx%d", format.Width, format.Height)
		}
		if format.FPS > 0 {
			fmt.Fprintf(&body, ",FRAME-RATE=%s", strconv.FormatFloat(format.FPS, 'f', -1, 64))
		}
		switch format.DynamicRange {
		case "SDR", "HLG":
			fmt.Fprintf(&body, ",VIDEO-RANGE=%s", format.DynamicRange)
		case "HDR10", "HDR10+", "HDR12", "DV":
			body.WriteString(",VIDEO-RANGE=PQ")
		}
		mediaURL := session.addResource(format.URL, playbackHeaders(session.info.Headers, format.Headers), format.DownloaderOptions)
		fmt.Fprintf(&body, "\n%s\n", mediaURL)
		children = append(children, path.Base(strings.TrimSuffix(mediaURL, "/")))
	}
	session.manifest = []byte(body.String())
	session.replaceManifestChildren(session.rootResource, children)
}

func (session *youtubeStreamSession) serveResource(w http.ResponseWriter, r *http.Request, resourceID, suffix string) {
	resource, target, resourceID, err := session.resolveResource(resourceID, suffix, r.URL.Query())
	if err != nil {
		http.NotFound(w, r)
		return
	}
	chunkSize := resource.chunkSize
	if chunkSize <= 0 {
		chunkSize = youtubeStreamChunkSize
	}
	initialRange := r.Header.Get("Range")
	if session.manifestType == "hls" && initialRange == "bytes=0-" {
		initialRange = ""
	}
	if session.manifestType != "hls" || initialRange != "" {
		initialRange = firstStreamRange(initialRange, chunkSize)
	}
	if r.Method == http.MethodHead && session.manifestType != "hls" {
		initialRange = "bytes=0-0"
	}
	response, err := session.requestResource(r.Context(), resource, target, initialRange)
	if err != nil {
		http.Error(w, "Could not read the upstream media", http.StatusBadGateway)
		return
	}
	defer func(body io.ReadCloser) { _ = body.Close() }(response.Body)
	if response.StatusCode != 200 && response.StatusCode != 206 {
		if value := response.Header.Get("Content-Range"); value != "" {
			w.Header().Set("Content-Range", value)
		}
		http.Error(w, "The upstream media is unavailable", response.StatusCode)
		return
	}
	contentType := response.Header.Get("Content-Type")
	isHLS := strings.Contains(strings.ToLower(contentType), "mpegurl") || strings.HasSuffix(target.Path, ".m3u8") || strings.Contains(target.Path, "/manifest/hls")
	isDASH := strings.Contains(contentType, "dash+xml") || strings.HasSuffix(target.Path, ".mpd") || strings.Contains(target.Path, "/manifest/dash")
	w.Header().Set("Cache-Control", "private, no-store")
	if isHLS || isDASH {
		var data bytes.Buffer
		if response.StatusCode == http.StatusPartialContent && !strings.HasSuffix(response.Header.Get("Content-Range"), "/*") {
			_, _, size, err := streamContentRange(response)
			if err != nil || size > 8<<20 {
				http.Error(w, "Could not read the media manifest", http.StatusBadGateway)
				return
			}
			err = session.copyResourceRange(&data, r.Context(), resource, target, streamByteRange{0, size - 1}, response)
			if err != nil {
				status := http.StatusBadGateway
				var upstream *streamUpstreamStatusError
				if errors.As(err, &upstream) && upstream.status >= 400 {
					status = upstream.status
				}
				http.Error(w, "Could not read the media manifest", status)
				return
			}
		} else {
			if _, err := io.Copy(&data, io.LimitReader(response.Body, (8<<20)+1)); err != nil || data.Len() > 8<<20 {
				http.Error(w, "Could not read the media manifest", http.StatusBadGateway)
				return
			}
		}
		if response.Request != nil && response.Request.URL != nil {
			target = response.Request.URL
		}
		var rewritten []byte
		var children []string
		if isHLS {
			rewritten, children, err = session.rewriteHLS(data.Bytes(), target, resource.headers, chunkSize)
			contentType = "application/vnd.apple.mpegurl"
		} else {
			rewritten, children, err = session.rewriteDASH(data.Bytes(), target, resource.headers, chunkSize)
			contentType = "application/dash+xml"
		}
		if err != nil {
			session.replaceManifestChildren("", nil)
			http.Error(w, "Could not read the media manifest", http.StatusBadGateway)
			return
		}
		session.replaceManifestChildren(resourceID, children)
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(rewritten)))
		_, _ = w.Write(rewritten)
		return
	}
	// Empty subtitle segments still need a WebVTT header for the player.
	if strings.HasPrefix(contentType, "text/vtt") {
		reader := bufio.NewReader(response.Body)
		if _, err := reader.Peek(1); errors.Is(err, io.EOF) {
			w.Header().Set("Content-Type", "text/vtt")
			w.Header().Set("Content-Length", "8")
			_, _ = w.Write([]byte("WEBVTT\n\n"))
			return
		} else if err != nil {
			http.Error(w, "Could not read the subtitle segment", http.StatusBadGateway)
			return
		}
		response.Body = struct {
			io.Reader
			io.Closer
		}{reader, response.Body}
	}
	if response.StatusCode == http.StatusOK || session.manifestType == "hls" && strings.HasSuffix(response.Header.Get("Content-Range"), "/*") {
		for _, key := range []string{"Content-Type", "Content-Length", "Accept-Ranges", "Content-Range"} {
			if value := response.Header.Get(key); value != "" {
				w.Header().Set(key, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = io.Copy(w, response.Body)
		return
	}
	_, _, size, err := streamContentRange(response)
	if err != nil {
		http.Error(w, "Invalid upstream media range", http.StatusBadGateway)
		return
	}
	ranges, err := parseStreamRanges(r.Header.Get("Range"), size)
	if err != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		http.Error(w, "Invalid media range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	w.Header().Set("Accept-Ranges", "bytes")
	if len(ranges) == 1 {
		selected := ranges[0]
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", strconv.FormatInt(selected.end-selected.start+1, 10))
		status := http.StatusOK
		if r.Header.Get("Range") != "" {
			status = http.StatusPartialContent
			w.Header().Set("Content-Range", streamRangeHeader(selected, size))
		}
		w.WriteHeader(status)
		if r.Method == http.MethodHead {
			return
		}
		_ = session.copyResourceRange(w, r.Context(), resource, target, selected, response)
		return
	}
	writer := multipart.NewWriter(w)
	var overhead bytes.Buffer
	count := multipart.NewWriter(&overhead)
	if err := count.SetBoundary(writer.Boundary()); err != nil {
		http.Error(w, "Could not prepare media ranges", http.StatusBadGateway)
		return
	}
	length := int64(0)
	for _, selected := range ranges {
		if _, err := count.CreatePart(streamRangeMIMEHeader(selected, size, contentType)); err != nil {
			http.Error(w, "Could not prepare media ranges", http.StatusBadGateway)
			return
		}
		length += selected.end - selected.start + 1
	}
	if err := count.Close(); err != nil {
		http.Error(w, "Could not prepare media ranges", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "multipart/byteranges; boundary="+writer.Boundary())
	w.Header().Set("Content-Length", strconv.FormatInt(length+int64(overhead.Len()), 10))
	w.WriteHeader(http.StatusPartialContent)
	if r.Method == http.MethodHead {
		return
	}
	for _, selected := range ranges {
		part, err := writer.CreatePart(streamRangeMIMEHeader(selected, size, contentType))
		if err != nil {
			return
		}
		if err := session.copyResourceRange(part, r.Context(), resource, target, selected, response); err != nil {
			return
		}
		response = nil
	}
	_ = writer.Close()
}

type streamByteRange struct{ start, end int64 }

type streamUpstreamStatusError struct{ status int }

func (err *streamUpstreamStatusError) Error() string {
	return fmt.Sprintf("upstream media returned HTTP %d", err.status)
}

func streamRangeHeader(selected streamByteRange, size int64) string {
	return fmt.Sprintf("bytes %d-%d/%d", selected.start, selected.end, size)
}

func streamRangeMIMEHeader(selected streamByteRange, size int64, contentType string) textproto.MIMEHeader {
	return textproto.MIMEHeader{"Content-Type": {contentType}, "Content-Range": {streamRangeHeader(selected, size)}}
}

func firstStreamRange(raw string, chunkSize int64) string {
	start, end := int64(0), chunkSize-1
	if strings.HasPrefix(raw, "bytes=") {
		left, right, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(raw, "bytes=")), "-")
		if !ok || left == "" || strings.Contains(raw, ",") {
			return "bytes=0-0"
		}
		parsed, err := strconv.ParseInt(left, 10, 64)
		if err != nil || parsed < 0 {
			return "bytes=0-0"
		}
		start, end = parsed, parsed+chunkSize-1
		if right != "" {
			if requestedEnd, err := strconv.ParseInt(right, 10, 64); err == nil {
				end = min(end, requestedEnd)
			}
		}
	}
	return fmt.Sprintf("bytes=%d-%d", start, end)
}

func parseStreamRanges(raw string, size int64) ([]streamByteRange, error) {
	if raw == "" {
		return []streamByteRange{{0, size - 1}}, nil
	}
	if !strings.HasPrefix(raw, "bytes=") {
		return nil, errors.New("invalid media range")
	}
	var ranges []streamByteRange
	for _, value := range strings.Split(strings.TrimPrefix(raw, "bytes="), ",") {
		left, right, ok := strings.Cut(strings.TrimSpace(value), "-")
		if !ok {
			return nil, errors.New("invalid media range")
		}
		selected := streamByteRange{0, size - 1}
		if left == "" {
			suffix, err := strconv.ParseInt(right, 10, 64)
			if err != nil || suffix <= 0 {
				return nil, errors.New("invalid media range")
			}
			selected.start = max(int64(0), size-suffix)
		} else {
			start, err := strconv.ParseInt(left, 10, 64)
			if err != nil || start < 0 {
				return nil, errors.New("invalid media range")
			}
			selected.start = start
			if right != "" {
				end, err := strconv.ParseInt(right, 10, 64)
				if err != nil || end < start {
					return nil, errors.New("invalid media range")
				}
				selected.end = min(end, size-1)
			}
			if start >= size {
				continue
			}
		}
		ranges = append(ranges, selected)
	}
	if len(ranges) == 0 {
		return nil, errors.New("media range is outside the source")
	}
	return ranges, nil
}

func streamContentRange(response *http.Response) (int64, int64, int64, error) {
	var start, end, size int64
	if count, err := fmt.Sscanf(response.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &size); err != nil || count != 3 || start < 0 || end < start || size <= end {
		return 0, 0, 0, errors.New("invalid upstream media range")
	}
	return start, end, size, nil
}

func (session *youtubeStreamSession) requestResource(ctx context.Context, resource youtubeStreamResource, target *url.URL, selected string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	for key, value := range resource.headers {
		request.Header.Set(key, value)
	}
	request.Header.Set("Accept-Encoding", "identity")
	if selected != "" {
		request.Header.Set("Range", selected)
	} else {
		request.Header.Del("Range")
	}
	return session.client.Do(request)
}

func (session *youtubeStreamSession) copyResourceRange(writer io.Writer, ctx context.Context, resource youtubeStreamResource, target *url.URL, selected streamByteRange, initial *http.Response) error {
	chunkSize := resource.chunkSize
	if chunkSize <= 0 {
		chunkSize = youtubeStreamChunkSize
	}
	for position := selected.start; position <= selected.end; {
		response := initial
		initial = nil
		if response != nil {
			start, _, _, err := streamContentRange(response)
			if err != nil || start != position {
				_ = response.Body.Close()
				response = nil
			}
		}
		if response == nil {
			var err error
			response, err = session.requestResource(ctx, resource, target, fmt.Sprintf("bytes=%d-%d", position, min(selected.end, position+chunkSize-1)))
			if err != nil {
				return err
			}
		}
		if response.StatusCode != http.StatusPartialContent {
			_ = response.Body.Close()
			return &streamUpstreamStatusError{status: response.StatusCode}
		}
		start, end, _, err := streamContentRange(response)
		if err != nil || start != position {
			_ = response.Body.Close()
			return errors.New("upstream returned the wrong media range")
		}
		length := min(end, selected.end) - position + 1
		_, err = io.CopyN(writer, response.Body, length)
		_ = response.Body.Close()
		if err != nil {
			return err
		}
		position += length
	}
	return nil
}

func (session *youtubeStreamSession) rewriteHLS(data []byte, base *url.URL, headers map[string]string, chunkSize int64) ([]byte, []string, error) {
	var children []string
	add := func(raw string) (string, error) {
		reference, err := url.Parse(raw)
		if err != nil {
			return "", err
		}
		id := session.registerResource(youtubeStreamResource{url: base.ResolveReference(reference).String(), headers: headers, chunkSize: chunkSize})
		children = append(children, id)
		return session.resourcePath(id), nil
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "#") {
			resourcePath, err := add(trimmed)
			if err != nil {
				return nil, nil, err
			}
			lines[i] = resourcePath
			continue
		}
		start := strings.Index(line, `URI="`)
		if start < 0 {
			continue
		}
		start += len(`URI="`)
		end := strings.Index(line[start:], `"`)
		if end < 0 {
			return nil, nil, errors.New("invalid media manifest URI")
		}
		end += start
		resourcePath, err := add(line[start:end])
		if err != nil {
			return nil, nil, err
		}
		lines[i] = line[:start] + resourcePath + line[end:]
	}
	return []byte(strings.Join(lines, "\n")), children, nil
}

var dashTemplateTokens = regexp.MustCompile(`\$[A-Za-z][A-Za-z0-9_]*(?:%0?[0-9]*[diouxX])?\$|\$\$`)

func (session *youtubeStreamSession) addDASHReference(raw string, headers map[string]string, chunkSize int64, template bool) (string, string, error) {
	reference, err := url.Parse(dashTemplateTokens.ReplaceAllString(raw, "_"))
	if err != nil {
		return "", "", err
	}
	resource := youtubeStreamResource{url: raw, headers: headers, chunkSize: chunkSize, relative: !reference.IsAbs(), template: template}
	if template {
		seen := make(map[string]bool)
		for _, token := range dashTemplateTokens.FindAllString(raw, -1) {
			if token != "$$" && !seen[token] {
				resource.templates = append(resource.templates, token)
				seen[token] = true
			}
		}
	}
	id := session.registerResource(resource)
	local := session.resourcePath(id)
	if resource.relative {
		local = "__ref/" + id + "/"
	}
	for i, token := range resource.templates {
		if i == 0 {
			local += "?"
		} else {
			local += "&"
		}
		local += "t" + strconv.Itoa(i) + "=" + token
	}
	return local, id, nil
}

func expandDASHResource(resource youtubeStreamResource, values url.Values) (string, error) {
	raw := resource.url
	for i, token := range resource.templates {
		parameter, ok := values["t"+strconv.Itoa(i)]
		if !ok || len(parameter) != 1 {
			return "", errors.New("missing DASH template value")
		}
		value := url.PathEscape(parameter[0])
		value = strings.NewReplacer("&", "%26", "=", "%3D").Replace(value)
		raw = strings.ReplaceAll(raw, token, value)
	}
	if resource.template {
		raw = strings.ReplaceAll(raw, "$$", "$")
	}
	return raw, nil
}

func (session *youtubeStreamSession) resolveResource(id, suffix string, values url.Values) (youtubeStreamResource, *url.URL, string, error) {
	session.mu.Lock()
	defer session.mu.Unlock()
	resource, ok := session.resources[id]
	if !ok || resource.relative {
		return youtubeStreamResource{}, nil, "", errors.New("unknown stream resource")
	}
	raw, err := expandDASHResource(resource, values)
	if err != nil {
		return youtubeStreamResource{}, nil, "", err
	}
	target, err := url.Parse(raw)
	if err != nil {
		return youtubeStreamResource{}, nil, "", err
	}
	parts := strings.Split(strings.Trim(suffix, "/"), "/")
	if suffix != "" {
		if len(parts)%2 != 0 {
			return youtubeStreamResource{}, nil, "", errors.New("unknown stream reference")
		}
		for i := 0; i < len(parts); i += 2 {
			if parts[i] != "__ref" {
				return youtubeStreamResource{}, nil, "", errors.New("unknown stream reference")
			}
			id = parts[i+1]
			reference, ok := session.resources[id]
			if !ok || !reference.relative {
				return youtubeStreamResource{}, nil, "", errors.New("unknown stream reference")
			}
			raw, err := expandDASHResource(reference, values)
			if err != nil {
				return youtubeStreamResource{}, nil, "", err
			}
			parsed, err := url.Parse(raw)
			if err != nil {
				return youtubeStreamResource{}, nil, "", err
			}
			target = target.ResolveReference(parsed)
			resource = reference
		}
	}
	return resource, target, id, nil
}

func (session *youtubeStreamSession) replaceManifestChildren(id string, children []string) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if id != "" {
		resource := session.resources[id]
		resource.children = children
		session.resources[id] = resource
	}
	if session.rootResource == "" {
		return
	}
	live := make(map[string]bool)
	var visit func(string)
	visit = func(id string) {
		if live[id] {
			return
		}
		live[id] = true
		for _, child := range session.resources[id].children {
			visit(child)
		}
	}
	visit(session.rootResource)
	for _, track := range session.textTracks {
		visit(path.Base(strings.TrimSuffix(track.URL, "/")))
	}
	for id, resource := range session.resources {
		if !live[id] {
			delete(session.resources, id)
			delete(session.resourceIDs, streamResourceKey(resource))
		}
	}
}

func stripDASHNamespaceDeclarations(element xml.StartElement) xml.StartElement {
	attrs := element.Attr[:0]
	for _, attr := range element.Attr {
		if attr.Name.Space != "xmlns" && (attr.Name.Space != "" || attr.Name.Local != "xmlns") {
			attrs = append(attrs, attr)
		}
	}
	element.Attr = attrs
	return element
}

func dashResourceAttribute(element, attribute string) bool {
	switch element {
	case "SegmentTemplate":
		return attribute == "media" || attribute == "initialization" || attribute == "index" || attribute == "bitstreamSwitching"
	case "SegmentURL":
		return attribute == "media" || attribute == "index"
	case "Initialization", "RepresentationIndex", "BitstreamSwitching":
		return attribute == "sourceURL"
	}
	return false
}

func (session *youtubeStreamSession) rewriteDASH(data []byte, base *url.URL, headers map[string]string, chunkSize int64) ([]byte, []string, error) {
	scan := xml.NewDecoder(bytes.NewReader(data))
	depth, rootHasBase := 0, false
	for {
		token, err := scan.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			if depth == 1 && value.Name.Local == "BaseURL" {
				rootHasBase = true
			}
			depth++
		case xml.EndElement:
			depth--
		}
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var result bytes.Buffer
	encoder := xml.NewEncoder(&result)
	var stack []string
	var children []string
	add := func(raw string, template bool) (string, error) {
		local, id, err := session.addDASHReference(raw, headers, chunkSize, template)
		if err == nil {
			children = append(children, id)
		}
		return local, err
	}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			value = stripDASHNamespaceDeclarations(value)
			if value.Name.Local == "BaseURL" || value.Name.Local == "Location" {
				var original string
				if err := decoder.DecodeElement(&original, &value); err != nil {
					return nil, nil, err
				}
				original = strings.TrimSpace(original)
				if len(stack) == 1 || value.Name.Local == "Location" {
					reference, err := url.Parse(original)
					if err != nil {
						return nil, nil, err
					}
					original = base.ResolveReference(reference).String()
				}
				local, err := add(original, false)
				if err != nil {
					return nil, nil, err
				}
				if err := encoder.EncodeToken(value); err != nil {
					return nil, nil, err
				}
				if err := encoder.EncodeToken(xml.CharData(local)); err != nil {
					return nil, nil, err
				}
				if err := encoder.EncodeToken(value.End()); err != nil {
					return nil, nil, err
				}
				continue
			}
			for i, attr := range value.Attr {
				if !dashResourceAttribute(value.Name.Local, attr.Name.Local) && attr.Name.Local != "href" {
					continue
				}
				raw := attr.Value
				if attr.Name.Local == "href" {
					reference, err := url.Parse(raw)
					if err != nil {
						return nil, nil, err
					}
					raw = base.ResolveReference(reference).String()
				}
				local, err := add(raw, value.Name.Local == "SegmentTemplate")
				if err != nil {
					return nil, nil, err
				}
				value.Attr[i].Value = local
			}
			stack = append(stack, value.Name.Local)
			if err := encoder.EncodeToken(value); err != nil {
				return nil, nil, err
			}
			if len(stack) == 1 && value.Name.Local == "MPD" && !rootHasBase {
				local, err := add(base.String(), false)
				if err != nil {
					return nil, nil, err
				}
				baseElement := xml.StartElement{Name: xml.Name{Space: value.Name.Space, Local: "BaseURL"}}
				if err := encoder.EncodeToken(baseElement); err != nil {
					return nil, nil, err
				}
				if err := encoder.EncodeToken(xml.CharData(local)); err != nil {
					return nil, nil, err
				}
				if err := encoder.EncodeToken(baseElement.End()); err != nil {
					return nil, nil, err
				}
			}
			continue
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
		if err := encoder.EncodeToken(token); err != nil {
			return nil, nil, err
		}
	}
	if err := encoder.Flush(); err != nil {
		return nil, nil, err
	}
	return result.Bytes(), children, nil
}
