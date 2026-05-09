package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestHandleTweetMediaMoveRejectsHTMLDisguisedAsMP4(t *testing.T) {
	srv := newTestServer(t)

	stagingDir := t.TempDir()
	archiveDir := t.TempDir()
	categoryID, err := srv.db.CreateBookmarkCategory("alice", "Memes", archiveDir)
	if err != nil {
		t.Fatalf("CreateBookmarkCategory: %v", err)
	}
	if err := srv.db.SetSetting("", "x_media_staging_dir", stagingDir); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	stagedName := "tmp_2052388503678337240_0.mp4"
	stagedPath := filepath.Join(stagingDir, stagedName)
	if err := os.WriteFile(stagedPath, []byte("<!DOCTYPE html><html><body>not a video</body></html>"), 0o644); err != nil {
		t.Fatalf("write staged fixture: %v", err)
	}

	body := strings.NewReader(`{
		"handle": "compliantvc",
		"label": "nokia",
		"category_id": ` + strconv.FormatInt(categoryID, 10) + `,
		"staged_files": [{"staging_name": "` + stagedName + `", "ext": ".mp4"}]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/tweet-media-move", body)
	req.Header.Set("Content-Type", "application/json")
	req = attachTestAuth(req, "alice")
	rec := httptest.NewRecorder()

	srv.handleTweetMediaMove(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d - %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Success bool     `json:"success"`
		Moved   []string `json:"moved"`
		Failed  []string `json:"failed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Success {
		t.Fatalf("success: got true, response=%s", rec.Body.String())
	}
	if len(resp.Moved) != 0 {
		t.Fatalf("moved: got %v, want none", resp.Moved)
	}
	if len(resp.Failed) != 1 || resp.Failed[0] != stagedName {
		t.Fatalf("failed: got %v, want [%s]", resp.Failed, stagedName)
	}
	if _, err := os.Stat(filepath.Join(archiveDir, "compliantvc nokia 001.mp4")); !os.IsNotExist(err) {
		t.Fatalf("invalid mp4 was archived, stat err=%v", err)
	}
	if _, err := os.Stat(stagedPath); !os.IsNotExist(err) {
		t.Fatalf("invalid staging file should be removed, stat err=%v", err)
	}
}
