package exportbundle

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/screwys/igloo/internal/db"
)

// MediaFile is a local media file and the portable archive path used by full
// exports and automatic backups.
type MediaFile struct {
	SourcePath  string
	ArchivePath string
}

// CollectBookmarkMedia gathers the same bookmark media payload used by full
// export: bookmarked item media, quoted item media, and companion audio files.
func CollectBookmarkMedia(store *db.DB, dataDir string, bookmarks []db.BookmarkExport) []MediaFile {
	var files []MediaFile
	seenPath := make(map[string]bool)
	countByBookmark := make(map[string]int)
	add := func(bookmarkID, path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(dataDir, path)
		}
		cleanPath := filepath.Clean(path)
		info, err := os.Stat(cleanPath)
		if err != nil || info.IsDir() || seenPath[cleanPath] {
			return
		}
		seenPath[cleanPath] = true
		safeID := sanitizeFilename(bookmarkID)
		if safeID == "" {
			safeID = "item"
		}
		idx := countByBookmark[safeID]
		countByBookmark[safeID] = idx + 1
		ext := strings.ToLower(filepath.Ext(cleanPath))
		if ext == "" {
			ext = ".bin"
		}
		files = append(files, MediaFile{
			SourcePath:  cleanPath,
			ArchivePath: filepath.ToSlash(filepath.Join("media", "bookmarks", safeID, fmt.Sprintf("%03d%s", idx, ext))),
		})
	}

	for _, bm := range bookmarks {
		for _, path := range collectSlides(store, dataDir, bm.VideoID) {
			add(bm.VideoID, path)
		}
		if path := audioPath(store, dataDir, bm.VideoID); path != "" {
			add(bm.VideoID, path)
		}
		items, err := store.GetFeedItemsForTweetIDs([]string{bm.VideoID})
		if err == nil {
			if item, ok := items[bm.VideoID]; ok && item.QuoteTweetID != "" {
				for _, path := range collectSlides(store, dataDir, item.QuoteTweetID) {
					add(bm.VideoID, path)
				}
				if path := audioPath(store, dataDir, item.QuoteTweetID); path != "" {
					add(bm.VideoID, path)
				}
			}
		}
	}
	return files
}

// CollectAvatarMedia gathers cached profile avatars included by full export.
func CollectAvatarMedia(dataDir string) []MediaFile {
	if strings.TrimSpace(dataDir) == "" {
		return nil
	}
	root := filepath.Join(dataDir, "thumbnails", "avatars")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	files := make([]MediaFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		name := entry.Name()
		if !AvatarMediaName(name) {
			continue
		}
		sourcePath := filepath.Join(root, name)
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, MediaFile{
			SourcePath:  sourcePath,
			ArchivePath: filepath.ToSlash(filepath.Join("media", "avatars", name)),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].ArchivePath < files[j].ArchivePath
	})
	return files
}

func AvatarMediaName(name string) bool {
	if strings.TrimSpace(name) == "" || name != filepath.Base(name) {
		return false
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return true
	default:
		return false
	}
}

func collectSlides(store *db.DB, dataDir, tweetID string) []string {
	video, err := store.GetVideo(tweetID)
	if err == nil && video != nil {
		meta := video.ParseMetadata()
		if meta != nil && len(meta.Slides) > 0 {
			var slides []string
			for i := range meta.Slides {
				path := meta.SlidePath(i)
				if fullPath, ok := resolveDataPathUnder(dataDir, path); ok {
					if _, err := os.Stat(fullPath); err == nil {
						slides = append(slides, fullPath)
					}
				}
			}
			if len(slides) > 0 {
				return slides
			}
		}
		if video.FilePath != "" {
			if fullPath, ok := resolveDataPathUnder(dataDir, video.FilePath); ok {
				if _, err := os.Stat(fullPath); err == nil {
					return []string{fullPath}
				}
			}
		}
	}

	var mediaFileSlides []string
	for idx := 0; idx < 20; idx++ {
		if path := findFeedMediaFile(store, dataDir, tweetID, idx); path != "" {
			mediaFileSlides = append(mediaFileSlides, path)
			continue
		}
		if len(mediaFileSlides) == 0 && idx >= 4 {
			break
		}
	}
	if len(mediaFileSlides) > 0 {
		return mediaFileSlides
	}

	feedMediaDir := filepath.Join(dataDir, "feed_media")
	var feedSlides []string
	for idx := 0; idx < 20; idx++ {
		for _, ext := range []string{".jpg", ".png", ".webp", ".mp4"} {
			path := filepath.Join(feedMediaDir, fmt.Sprintf("%s_%d%s", tweetID, idx, ext))
			if _, err := os.Stat(path); err == nil {
				feedSlides = append(feedSlides, path)
				break
			}
		}
		if len(feedSlides) == 0 && idx >= 4 {
			break
		}
	}
	return feedSlides
}

func audioPath(store *db.DB, dataDir, videoID string) string {
	video, _ := store.GetVideo(videoID)
	audioExts := []string{".mp3", ".m4a", ".ogg", ".aac"}
	if video != nil && video.FilePath != "" {
		dir := filepath.Dir(resolveDataPath(dataDir, video.FilePath))
		stem := strings.TrimSuffix(filepath.Base(video.FilePath), filepath.Ext(video.FilePath))
		for _, ext := range audioExts {
			for _, candidateStem := range []string{videoID, videoID + "_0", stem, stem + "_0"} {
				candidate := filepath.Join(dir, candidateStem+ext)
				if _, err := os.Stat(candidate); err == nil {
					return candidate
				}
			}
		}
	}
	return findFeedMediaAudioFile(store, dataDir, videoID)
}

type feedMediaOwnerRef struct {
	ownerType string
	ownerID   string
	handle    string
}

func resolveFeedMediaRefs(store *db.DB, tweetID string) []feedMediaOwnerRef {
	items, err := store.GetFeedItemsForTweetIDs([]string{tweetID})
	if err != nil {
		return nil
	}
	fi, ok := items[tweetID]
	if !ok {
		return nil
	}

	refs := []feedMediaOwnerRef{{
		ownerType: "feed_media",
		ownerID:   tweetID,
		handle:    firstNonEmpty(fi.SourceHandle, fi.AuthorHandle),
	}}
	if fi.QuoteTweetID != "" {
		refs = append(refs, feedMediaOwnerRef{
			ownerType: "quote_media",
			ownerID:   fi.QuoteTweetID,
			handle:    firstNonEmpty(fi.QuoteAuthorHandle, fi.AuthorHandle, fi.SourceHandle),
		})
	}
	return refs
}

func findFeedMediaAudioFile(store *db.DB, dataDir, tweetID string) string {
	for _, ref := range resolveFeedMediaRefs(store, tweetID) {
		if relPath, err := store.GetMediaFileAudioPath(ref.ownerType, ref.ownerID); err == nil {
			absPath := resolveDataPath(dataDir, relPath)
			if _, err := os.Stat(absPath); err == nil {
				return absPath
			}
		}
	}
	return ""
}

func findFeedMediaFile(store *db.DB, dataDir, tweetID string, index int) string {
	refs := resolveFeedMediaRefs(store, tweetID)
	for _, ref := range refs {
		if relPath, err := store.GetMediaFilePath(ref.ownerType, ref.ownerID, index); err == nil {
			absPath := resolveDataPath(dataDir, relPath)
			if _, err := os.Stat(absPath); err == nil {
				return absPath
			}
		}
	}
	if path := findDirectFeedMediaFile(store, dataDir, tweetID, index); path != "" {
		return path
	}
	if len(refs) == 0 {
		return findFeedMediaByQuoteTweetID(store, dataDir, tweetID, index)
	}
	for _, ref := range refs {
		if path := probeMediaFile(dataDir, ref.handle, ref.ownerID, index); path != "" {
			return path
		}
	}
	return ""
}

func findDirectFeedMediaFile(store *db.DB, dataDir, tweetID string, index int) string {
	for _, ownerType := range []string{"feed_media", "quote_media"} {
		if relPath, err := store.GetMediaFilePath(ownerType, tweetID, index); err == nil {
			absPath := resolveDataPath(dataDir, relPath)
			if _, err := os.Stat(absPath); err == nil {
				return absPath
			}
		}
	}
	return ""
}

func findFeedMediaByQuoteTweetID(store *db.DB, dataDir, quoteTweetID string, index int) string {
	var sourceHandle string
	_ = store.WithRead(func(conn *sql.DB) error {
		return conn.QueryRow(
			"SELECT COALESCE(quote_author_handle, author_handle) FROM feed_items WHERE quote_tweet_id = ? LIMIT 1",
			quoteTweetID,
		).Scan(&sourceHandle)
	})
	if sourceHandle == "" {
		return ""
	}
	return probeMediaFile(dataDir, sourceHandle, quoteTweetID, index)
}

func probeMediaFile(dataDir, handle, tweetID string, index int) string {
	handle, ok := safeLegacyTwitterMediaSegment(handle)
	if !ok {
		return ""
	}
	tweetID, ok = safeLegacyTwitterMediaSegment(tweetID)
	if !ok {
		return ""
	}
	for _, root := range []string{"media", "videos"} {
		baseDir, ok := resolveDataPathUnder(dataDir, filepath.Join(root, "twitter", handle))
		if !ok {
			continue
		}
		for _, fileIndex := range []int{index, index + 1} {
			for _, ext := range []string{".jpg", ".png", ".webp", ".mp4"} {
				path := filepath.Join(baseDir, fmt.Sprintf("%s_%d%s", tweetID, fileIndex, ext))
				fullPath, ok := resolveDataPathUnder(dataDir, path)
				if !ok {
					continue
				}
				if _, err := os.Stat(fullPath); err == nil {
					return fullPath
				}
			}
		}
	}
	return ""
}

func resolveDataPath(dataDir, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dataDir, path)
}

func resolveDataPathUnder(dataDir, path string) (string, bool) {
	if strings.TrimSpace(dataDir) == "" || strings.TrimSpace(path) == "" {
		return "", false
	}
	abs := resolveDataPath(dataDir, path)
	baseAbs, err := filepath.Abs(dataDir)
	if err != nil {
		return "", false
	}
	abs, err = filepath.Abs(abs)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(baseAbs, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return abs, true
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func safeLegacyTwitterMediaSegment(raw string) (string, bool) {
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "@"))
	if raw == "" || raw == "." || raw == ".." || filepath.Base(raw) != raw || filepath.Clean(raw) != raw {
		return "", false
	}
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return "", false
	}
	return raw, true
}

func sanitizeFilename(name string) string {
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
	)
	s := replacer.Replace(strings.TrimSpace(name))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
