package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/screwys/igloo/internal/model"
)

func TestDownloaderReportEndpointsRequireAdmin(t *testing.T) {
	srv := newTestServer(t)
	for _, tc := range []struct {
		name   string
		method string
		path   string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{"latest", http.MethodGet, "/api/downloader/report/latest", srv.handleDownloaderReportLatest},
		{"operations", http.MethodGet, "/api/downloader/operations", srv.handleDownloaderOperations},
		{"run", http.MethodPost, "/api/downloader/report/run", srv.handleDownloaderReportRun},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req = req.WithContext(contextWithUser(req, "alice", "user"))
			rec := httptest.NewRecorder()
			tc.call(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestDownloaderOperationsEndpointReturnsRedactedSummaries(t *testing.T) {
	srv := newTestServer(t)
	if err := srv.db.RecordDownloaderOperation(context.Background(), model.DownloaderOperation{
		Operation:   "x.gallerydl.dump",
		Platform:    "twitter",
		Subject:     "https://x.com/example",
		Tool:        "gallery-dl",
		StartedAtMs: time.Now().UnixMilli(),
		EndedAtMs:   time.Now().UnixMilli(),
		Status:      "failure",
		ErrorKind:   "auth",
		Error:       "cookies=***",
		CookieLabel: "x.com_cookies.txt",
		SummaryJSON: `{"args":["--cookies","***"]}`,
	}); err != nil {
		t.Fatalf("RecordDownloaderOperation: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/downloader/operations", nil)
	req = req.WithContext(contextWithUser(req, "admin", "admin"))
	rec := httptest.NewRecorder()
	srv.handleDownloaderOperations(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Operations []model.DownloaderOperation `json:"operations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Operations) != 1 {
		t.Fatalf("operations = %#v", body.Operations)
	}
	if body.Operations[0].CookieLabel != "x.com_cookies.txt" || body.Operations[0].ErrorKind != "auth" {
		t.Fatalf("operation = %#v", body.Operations[0])
	}
}
