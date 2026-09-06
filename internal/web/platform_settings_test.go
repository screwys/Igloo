package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/screwys/igloo/internal/model"
)

func TestThreadFetchSettingsPersistAndValidate(t *testing.T) {
	srv := newTestServer(t)
	for _, mode := range []string{"never", "starred", "always"} {
		req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(`{"x_thread_fetch_mode":"`+mode+`","x_thread_auto_post_limit":4,"x_community_notes_enabled":false}`))
		req.Header.Set("Content-Type", "application/json")
		req = attachTestAuthRole(req, "sample_admin", "admin")
		rec := httptest.NewRecorder()
		srv.handleUpdateSettings(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("mode %s: status %d: %s", mode, rec.Code, rec.Body.String())
		}
		for key, want := range map[string]string{"x_thread_fetch_mode": mode, "x_thread_auto_post_limit": "4", "x_community_notes_enabled": "false"} {
			got, err := srv.db.GetSetting(key, "")
			if err != nil || got != want {
				t.Fatalf("%s = %q, %v; want %q", key, got, err, want)
			}
		}
	}
	for _, body := range []map[string]string{{"x_thread_fetch_mode": "invalid"}, {"x_thread_auto_post_limit": "0"}, {"x_thread_auto_post_limit": "21"}, {"x_thread_auto_post_limit": "invalid"}} {
		if err := validateSettingsUpdate(body); err == nil {
			t.Fatalf("accepted invalid settings %v", body)
		}
	}
}

func TestPhotoAndCarouselMusicURL(t *testing.T) {
	for _, platform := range []string{"instagram", "tiktok"} {
		for _, kind := range []string{"image", "slideshow"} {
			got := videoToJSON(model.Video{VideoID: "sample_post", Platform: platform, MediaKind: kind})
			if got["audio_url"] != "/api/media/audio/sample_post" {
				t.Fatalf("%s %s audio URL = %v", platform, kind, got["audio_url"])
			}
		}
	}
}
