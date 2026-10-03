package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gorilla/sessions"
	"github.com/screwys/igloo/internal/config"
	"github.com/screwys/igloo/internal/db"
	"github.com/screwys/igloo/internal/i18n"
	"github.com/screwys/igloo/internal/storage"
	"github.com/screwys/igloo/internal/worker"
)

var webFixtureOwner *db.DB
var webFixtureURL string
var webFixtureRoot string
var webFixtureError error
var webFixtureOnce sync.Once
var webFixtureResetSQL string

func TestMain(m *testing.M) {
	status := m.Run()
	if webFixtureOwner != nil {
		if err := webFixtureOwner.Close(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			status = 1
		}
	}
	if webFixtureRoot != "" {
		if err := os.RemoveAll(webFixtureRoot); err != nil {
			fmt.Fprintln(os.Stderr, err)
			status = 1
		}
	}
	os.Exit(status)
}

func testWebConfig(t *testing.T, stateRoot string) *config.Config {
	t.Helper()
	markWebTestStateRoot(t, stateRoot)
	layout, err := storage.New(stateRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	return &config.Config{SecretKey: "test-key", Storage: layout}
}

func setTestStateRoot(t *testing.T, cfg *config.Config, stateRoot string) {
	t.Helper()
	markWebTestStateRoot(t, stateRoot)
	layout, err := storage.New(stateRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Storage = layout
}

func markWebTestStateRoot(t *testing.T, stateRoot string) {
	t.Helper()
	if err := os.MkdirAll(stateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateRoot, ".igloo-state-root"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func decodeInto(raw []byte, into any) error {
	return json.Unmarshal(raw, into)
}

// testServer bundles the live Server with a test-only mux so we can dispatch
// requests directly without the real middleware chain. Auth is injected via
// attachTestAuth; unauthenticated handlers should see userFromContext(ctx) == nil.
type testServer struct {
	*Server
	mux *http.ServeMux
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()

	webFixtureOnce.Do(func() {
		webFixtureError = func() error {
			var err error
			webFixtureRoot, err = os.MkdirTemp("", "igloo-web-suite-")
			if err != nil {
				return err
			}
			// Start only the suite's server, without changing the configured benchmark connection.
			configured, hadConfigured := os.LookupEnv("IGLOO_DATABASE_URL")
			if err := os.Unsetenv("IGLOO_DATABASE_URL"); err != nil {
				return err
			}
			var openErr error
			webFixtureOwner, openErr = db.OpenAtStateRoot(webFixtureRoot)
			if hadConfigured {
				if err := os.Setenv("IGLOO_DATABASE_URL", configured); err != nil {
					return err
				}
			}
			if openErr != nil {
				return openErr
			}
			var connection struct {
				URL string `json:"url"`
			}
			raw, err := os.ReadFile(filepath.Join(webFixtureRoot, ".postgresql-connection.json"))
			if err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &connection); err != nil {
				return err
			}
			webFixtureURL = connection.URL
			if err := webFixtureOwner.QueryRow(`
				SELECT 'TRUNCATE TABLE ' ||
					string_agg(format('%I.%I', schemaname, tablename), ', ' ORDER BY tablename) ||
					' RESTART IDENTITY CASCADE'
				FROM pg_tables
				WHERE schemaname = 'public' AND tablename <> 'goose_db_version'
			`).Scan(&webFixtureResetSQL); err != nil {
				return err
			}
			return webFixtureOwner.RecordAndroidFeedRetention(0, 1)
		}()
	})
	if webFixtureError != nil {
		t.Fatal(webFixtureError)
	}

	stateRoot := t.TempDir()
	cfg := testWebConfig(t, stateRoot)
	d, err := db.OpenWithOptions(cfg.Storage, db.OpenOptions{DatabaseURL: webFixtureURL})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
		if err := webFixtureOwner.WithWrite(func(tx *sql.Tx) error {
			if _, err := tx.Exec(webFixtureResetSQL); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO android_sync_clock (id, epoch, revision)
				VALUES (1, replace(gen_random_uuid()::TEXT, '-', ''), 0)`); err != nil {
				return err
			}
			_, err := tx.Exec(`INSERT INTO android_feed_retention (id, feed_days, reconciled_at_ms)
				VALUES (1, 0, 1)`)
			return err
		}); err != nil {
			t.Error(err)
		}
	})
	descriptor, err := json.Marshal(struct {
		URL      string `json:"url"`
		OwnerPID int    `json:"owner_pid"`
	}{webFixtureURL, os.Getpid()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateRoot, ".postgresql-connection.json"), descriptor, 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		db:      d,
		cfg:     cfg,
		store:   sessions.NewCookieStore([]byte("test-key")),
		workers: worker.NewManager(d, cfg),
		staticV: func(path string) string {
			return "/static/" + path + "?v=test"
		},
		i18n: i18n.NewCatalog(),
	}

	mux := http.NewServeMux()
	s.registerFeedAPIRoutes(mux)
	s.registerFeedSourceAPIRoutes(mux)
	s.registerBookmarkAPIRoutes(mux)
	s.registerSyncAPIRoutes(mux)
	s.registerVideoAPIRoutes(mux)
	s.registerShortsAPIRoutes(mux)
	s.registerProfileAPIRoutes(mux)
	s.registerAndroidSyncAPIRoutes(mux)
	s.registerMutationAPIRoutes(mux)
	s.registerChannelAPIRoutes(mux)
	s.registerThreadAPIRoutes(mux)
	s.registerI18NAPIRoutes(mux)
	mux.HandleFunc("GET /thread/{tweetID}", s.handlePageThread)
	mux.HandleFunc("GET /api/media/thumbnail/{videoID}", s.handleThumbnail)
	mux.HandleFunc("GET /api/media/avatar/{channelID}", s.handleChannelAvatar)
	mux.HandleFunc("GET /api/media/comment-avatar/{ownerID}", s.handleCommentAuthorAvatar)

	return &testServer{Server: s, mux: mux}
}

// attachTestAuth returns a copy of r whose context has a userInfo with the
// given username under the real userContextKey — matching what enforceAuth
// would have set. Use "" to leave the request unauthenticated.
func attachTestAuth(r *http.Request, username string) *http.Request {
	return attachTestAuthRole(r, username, "user")
}

func attachTestAuthRole(r *http.Request, username, role string) *http.Request {
	if username == "" {
		return r
	}
	if role == "" {
		role = "user"
	}
	ctx := context.WithValue(r.Context(), userContextKey, &userInfo{
		Username: username,
		Role:     role,
	})
	return r.WithContext(ctx)
}
