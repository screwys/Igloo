package web

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/screwys/igloo/internal/auth"
	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/exportbundle"
	"github.com/screwys/igloo/internal/fullimport"
	"github.com/screwys/igloo/internal/restore"
)

// ── Config export / import ────────────────────────────────────────────────────

func (s *Server) handleConfigExport(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	userID := ""
	if user := userFromContext(r.Context()); user != nil {
		userID = user.Username
	}
	cfg, err := s.db.ExportConfig(userID)
	if err != nil {
		slog.Error("ExportConfig", "err", err)
		writeJSON(w, 500, map[string]any{"error": "export error"})
		return
	}

	if dir := s.configuredExportDir(); dir != "" {
		path, err := writeExportFile(dir, "igloo-config", ".json", func(dst io.Writer) error {
			return writeConfigExportJSON(dst, cfg)
		})
		if err != nil {
			slog.Error("ExportConfig save", "dir", dir, "err", err)
			writeJSON(w, 500, map[string]any{"error": "export save error"})
			return
		}
		writeJSON(w, 200, map[string]any{
			"success": true,
			"saved":   true,
			"path":    path,
		})
		return
	}

	// Config export is a downloadable JSON document, not an API response —
	// envelope fields would pollute the archived file. apiPath() excludes it.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="igloo-config-%s.json"`,
			time.Now().UTC().Format("2006-01-02")))
	w.WriteHeader(200)
	if err := writeConfigExportJSON(w, cfg); err != nil {
		slog.Error("ExportConfig write", "err", err)
	}
}

func (s *Server) handleConfigExportFull(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	userID := ""
	if user := userFromContext(r.Context()); user != nil {
		userID = user.Username
	}
	cfg, err := s.db.ExportFullData(userID)
	if err != nil {
		slog.Error("ExportFullData", "err", err)
		writeJSON(w, 500, map[string]any{"error": "export error"})
		return
	}

	mediaFiles := exportbundle.CollectBookmarkMedia(s.db, s.cfg.DataDir, cfg.Bookmarks)
	mediaFiles = append(mediaFiles, exportbundle.CollectAvatarMedia(s.cfg.DataDir)...)
	runtimeFiles := s.collectFullExportRuntimeConfigFiles()
	runtimeManifest := s.fullExportRuntimeManifest()

	if dir := s.configuredExportDir(); dir != "" {
		path, err := writeExportFile(dir, "igloo-full", ".zip", func(dst io.Writer) error {
			return writeFullExportZip(dst, cfg, mediaFiles, runtimeFiles, runtimeManifest)
		})
		if err != nil {
			slog.Error("ExportFullData save", "dir", dir, "err", err)
			writeJSON(w, 500, map[string]any{"error": "export save error"})
			return
		}
		writeJSON(w, 200, map[string]any{
			"success": true,
			"saved":   true,
			"path":    path,
		})
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="igloo-full-%s.zip"`,
			time.Now().UTC().Format("2006-01-02")))
	w.WriteHeader(200)
	if err := writeFullExportZip(w, cfg, mediaFiles, runtimeFiles, runtimeManifest); err != nil {
		slog.Error("ExportFullData zip write", "err", err)
	}
}

func writeConfigExportJSON(w io.Writer, cfg db.ConfigExport) error {
	return json.NewEncoder(w).Encode(cfg)
}

func writeFullExportZip(w io.Writer, cfg db.ConfigExport, mediaFiles []exportbundle.MediaFile, runtimeFiles []fullExportRuntimeFile, runtimeManifest fullimport.RuntimeManifest) error {
	zw := zip.NewWriter(w)
	if err := writeFullExportJSON(zw, cfg); err != nil {
		_ = zw.Close()
		return err
	}
	if err := writeFullExportRuntimeManifest(zw, runtimeManifest); err != nil {
		_ = zw.Close()
		return err
	}
	for _, file := range runtimeFiles {
		if err := writeFullExportRuntimeConfigFile(zw, file); err != nil {
			slog.Warn("ExportFullData runtime config file skipped", "path", file.SourcePath, "err", err)
		}
	}
	for _, file := range mediaFiles {
		if err := writeFullExportMediaFile(zw, file); err != nil {
			slog.Warn("ExportFullData media file skipped", "path", file.SourcePath, "err", err)
		}
	}
	return zw.Close()
}

func (s *Server) configuredExportDir() string {
	if !s.db.BoolSetting("backup_enabled") {
		return ""
	}
	dir, _ := s.db.GetSetting("backup_dir", "")
	dir = strings.TrimSpace(dir)
	if dir != "" && !filepath.IsAbs(dir) {
		slog.Error("configured export dir is not absolute", "dir", dir)
		return ""
	}
	return dir
}

func writeExportFile(dir, prefix, ext string, write func(io.Writer) error) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", fmt.Errorf("export dir is required")
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("export dir must be absolute: %s", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create export dir: %w", err)
	}
	stamp := time.Now().UTC().Format("2006-01-02-150405")
	name := fmt.Sprintf("%s-%s%s", prefix, stamp, ext)
	tmp, err := os.CreateTemp(dir, "."+prefix+"-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temp export: %w", err)
	}
	tmpPath := tmp.Name()
	closed := false
	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpPath)
	}()
	if err := write(tmp); err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		closed = true
		return "", err
	}
	closed = true
	finalPath := filepath.Join(dir, name)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return "", fmt.Errorf("rename export: %w", err)
	}
	return finalPath, nil
}

type fullExportRuntimeFile struct {
	SourcePath  string
	ArchivePath string
}

func writeFullExportJSON(zw *zip.Writer, cfg db.ConfigExport) error {
	f, err := zw.Create("export.json")
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(cfg)
}

func writeFullExportRuntimeManifest(zw *zip.Writer, manifest fullimport.RuntimeManifest) error {
	f, err := zw.Create("runtime.json")
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(manifest)
}

func writeFullExportRuntimeConfigFile(zw *zip.Writer, file fullExportRuntimeFile) error {
	src, err := os.Open(file.SourcePath)
	if err != nil {
		return err
	}
	defer func() {
		_ = src.Close()
	}()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("runtime config path is not a regular file")
	}
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = file.ArchivePath
	hdr.Method = zip.Deflate
	dst, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	return err
}

func writeFullExportMediaFile(zw *zip.Writer, file exportbundle.MediaFile) error {
	src, err := os.Open(file.SourcePath)
	if err != nil {
		return err
	}
	defer func() {
		_ = src.Close()
	}()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("media path is directory")
	}
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = file.ArchivePath
	hdr.Method = zip.Deflate
	dst, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	return err
}

func (s *Server) fullExportRuntimeManifest() fullimport.RuntimeManifest {
	if s == nil || s.cfg == nil {
		return fullimport.RuntimeManifest{Version: 1}
	}
	return fullimport.RuntimeManifest{
		Version:   1,
		DataDir:   s.cfg.DataDir,
		ConfigDir: s.cfg.ConfDir,
		RepoDir:   s.repoDirForRuntimeExport(),
	}
}

func (s *Server) repoDirForRuntimeExport() string {
	if s == nil || s.cfg == nil {
		return ""
	}
	if strings.TrimSpace(s.cfg.RepoDir) != "" {
		return s.cfg.RepoDir
	}
	if strings.TrimSpace(s.cfg.StaticDir) != "" {
		return filepath.Dir(s.cfg.StaticDir)
	}
	return ""
}

func (s *Server) collectFullExportRuntimeConfigFiles() []fullExportRuntimeFile {
	if s == nil || s.cfg == nil || strings.TrimSpace(s.cfg.ConfDir) == "" {
		return nil
	}
	root := filepath.Clean(s.cfg.ConfDir)
	var files []fullExportRuntimeFile
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			slog.Warn("ExportFullData runtime config path skipped", "path", path, "err", err)
			return nil
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			slog.Warn("ExportFullData runtime config rel path skipped", "path", path, "err", err)
			return nil
		}
		if skipFullExportRuntimeConfigPath(rel) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			slog.Warn("ExportFullData runtime config stat skipped", "path", path, "err", err)
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		files = append(files, fullExportRuntimeFile{
			SourcePath:  path,
			ArchivePath: filepath.ToSlash(filepath.Join("config", rel)),
		})
		return nil
	}); err != nil {
		slog.Warn("ExportFullData runtime config walk failed", "dir", root, "err", err)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].ArchivePath < files[j].ArchivePath
	})
	return files
}

func skipFullExportRuntimeConfigPath(rel string) bool {
	rel = filepath.Clean(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true
	}
	base := filepath.Base(rel)
	if base == "" || strings.HasPrefix(base, ".") {
		return true
	}
	for _, prefix := range []string{".auth_users_", ".config_", ".upload_", ".import-media-", ".import-config-"} {
		if strings.HasPrefix(base, prefix) {
			return true
		}
	}
	return false
}

func (s *Server) handleConfigImport(w http.ResponseWriter, r *http.Request) {
	isHTMX := r.Header.Get("HX-Request") != ""
	if !requireAdmin(w, r) {
		return
	}

	importErr := func(code int, msg string) {
		if isHTMX {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(code)
			_, _ = fmt.Fprintf(w, `<span class="status-message error">%s</span>`, template.HTMLEscapeString(msg))
			return
		}
		writeJSON(w, code, map[string]any{"error": msg})
	}
	importOK := func(msg string) {
		if isHTMX {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprintf(w, `<span class="status-message success">%s</span><script>setTimeout(function(){window.location.reload()},2000)</script>`, template.HTMLEscapeString(msg))
			return
		}
	}

	if err := r.ParseMultipartForm(8 << 20); err != nil {
		importErr(400, "multipart parse error")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		importErr(400, "missing file")
		return
	}
	defer func() {
		_ = file.Close()
	}()

	data, err := io.ReadAll(file)
	if err != nil {
		importErr(500, "read error")
		return
	}

	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		if err := restore.StageTarball(bytes.NewReader(data), s.cfg.DataDir); err != nil {
			slog.Error("StageTarball", "err", err)
			importErr(400, "tarball error: "+err.Error())
			return
		}
		slog.Info("restore: staged, exiting for systemd restart")
		if isHTMX {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, `<span class="status-message success">Restore staged. Igloo is restarting…</span><script>setTimeout(function(){window.location.reload()},12000)</script>`)
		} else {
			writeJSON(w, 200, map[string]any{"success": true, "format": "tarball", "restart": true})
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		go func() {
			time.Sleep(500 * time.Millisecond)
			os.Exit(1)
		}()
		return
	}

	if fullimport.IsZipPayload(data) {
		userID := ""
		if user := userFromContext(r.Context()); user != nil {
			userID = user.Username
		}
		replace := r.FormValue("mode") == "replace"
		result, restoredMedia, restoredConfig, err := fullimport.ImportFullExportZip(s.db, s.cfg.DataDir, s.cfg.ConfDir, s.repoDirForRuntimeExport(), data, userID, replace)
		if err != nil {
			slog.Error("ImportFullExportZip", "err", err)
			importErr(400, "zip import error: "+err.Error())
			return
		}
		if restoredConfig > 0 {
			auth.InvalidateCache()
		}
		var parts []string
		if result.AddedChannels > 0 {
			parts = append(parts, fmt.Sprintf("%d subscriptions", result.AddedChannels))
		}
		if result.AddedBookmarks > 0 {
			parts = append(parts, fmt.Sprintf("%d bookmarks", result.AddedBookmarks))
		}
		if result.AddedCategories > 0 {
			parts = append(parts, fmt.Sprintf("%d categories", result.AddedCategories))
		}
		if restoredMedia > 0 {
			parts = append(parts, fmt.Sprintf("%d media files", restoredMedia))
		}
		if restoredConfig > 0 {
			parts = append(parts, fmt.Sprintf("%d config files", restoredConfig))
		}
		summary := "Import complete"
		if len(parts) > 0 {
			summary = "Imported: " + strings.Join(parts, ", ")
		}
		importOK(summary)
		if !isHTMX {
			writeJSON(w, 200, map[string]any{
				"success": true, "format": "full_export_zip",
				"added_channels": result.AddedChannels, "added_bookmarks": result.AddedBookmarks,
				"added_categories": result.AddedCategories, "updated_settings": result.UpdatedSettings,
				"restored_media": restoredMedia, "restored_config_files": restoredConfig, "skipped": result.Skipped,
			})
		}
		return
	}

	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		importErr(400, "empty file")
		return
	}

	userID := ""
	if user := userFromContext(r.Context()); user != nil {
		userID = user.Username
	}
	replace := r.FormValue("mode") == "replace"

	switch trimmed[0] {
	case '{':
		var cfgExport db.ConfigExport
		if err := json.Unmarshal(trimmed, &cfgExport); err != nil {
			importErr(400, "invalid config JSON")
			return
		}
		result, err := s.db.ImportConfig(cfgExport, userID, replace)
		if err != nil {
			slog.Error("ImportConfig", "err", err)
			importErr(500, "import error")
			return
		}
		var parts []string
		if result.AddedChannels > 0 {
			parts = append(parts, fmt.Sprintf("%d subscriptions", result.AddedChannels))
		}
		if result.AddedBookmarks > 0 {
			parts = append(parts, fmt.Sprintf("%d bookmarks", result.AddedBookmarks))
		}
		if result.AddedCategories > 0 {
			parts = append(parts, fmt.Sprintf("%d categories", result.AddedCategories))
		}
		if result.UpdatedSettings > 0 {
			parts = append(parts, fmt.Sprintf("%d settings", result.UpdatedSettings))
		}
		summary := "Import complete"
		if len(parts) > 0 {
			summary = "Imported: " + strings.Join(parts, ", ")
		}
		importOK(summary)
		if !isHTMX {
			writeJSON(w, 200, map[string]any{
				"success": true, "format": "full_config",
				"added_channels": result.AddedChannels, "added_bookmarks": result.AddedBookmarks,
				"added_categories": result.AddedCategories, "updated_settings": result.UpdatedSettings,
				"skipped": result.Skipped,
			})
		}

	case '[':
		var urls []string
		if err := json.Unmarshal(trimmed, &urls); err != nil {
			importErr(400, "invalid subscription array JSON")
			return
		}
		added, skipped := s.importSubscriptionList(r.Context(), urls)
		importOK(fmt.Sprintf("Imported %d channels (%d skipped)", added, skipped))
		if !isHTMX {
			writeJSON(w, 200, map[string]any{"success": true, "format": "subscription_list", "added_channels": added, "skipped": skipped})
		}

	case '<':
		channels := parseOPML(trimmed)
		added, skipped := s.importChannelList(r.Context(), channels)
		importOK(fmt.Sprintf("Imported %d channels (%d skipped)", added, skipped))
		if !isHTMX {
			writeJSON(w, 200, map[string]any{"success": true, "format": "opml", "added_channels": added, "skipped": skipped})
		}

	default:
		importErr(400, "unrecognized format")
	}
}
