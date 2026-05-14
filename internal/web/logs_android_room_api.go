package web

import (
	"encoding/json"
	"net/http"
	"strings"
)

// ── Room query relay ──────────────────────────────────────────────────────────

func (s *Server) handleRoomQueryPost(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]any{"success": false, "error": "invalid JSON"})
		return
	}
	q := strings.TrimSpace(body.Query)
	if q == "" {
		writeJSON(w, 400, map[string]any{"success": false, "error": "empty query"})
		return
	}
	upper := strings.ToUpper(q)
	if !strings.HasPrefix(upper, "SELECT") &&
		!strings.HasPrefix(upper, "PRAGMA") &&
		!strings.HasPrefix(upper, "EXPLAIN") {
		writeJSON(w, 400, map[string]any{"success": false, "error": "only SELECT/PRAGMA/EXPLAIN allowed"})
		return
	}

	android.mu.Lock()
	android.roomQuery = &q
	android.roomResult = nil
	android.mu.Unlock()

	writeJSON(w, 200, map[string]any{"success": true})
}

func (s *Server) handleRoomQueryCheck(w http.ResponseWriter, r *http.Request) {
	android.mu.Lock()
	q := android.roomQuery
	android.roomQuery = nil
	android.mu.Unlock()

	if q == nil {
		writeJSON(w, 200, map[string]any{"has_query": false})
		return
	}
	writeJSON(w, 200, map[string]any{"has_query": true, "query": *q})
}

func (s *Server) handleRoomQueryResultPost(w http.ResponseWriter, r *http.Request) {
	var body any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]any{"success": false, "error": "invalid JSON"})
		return
	}

	android.mu.Lock()
	android.roomResult = body
	android.mu.Unlock()

	writeJSON(w, 200, map[string]any{"success": true})
}

func (s *Server) handleRoomQueryResultGet(w http.ResponseWriter, r *http.Request) {
	android.mu.Lock()
	result := android.roomResult
	android.mu.Unlock()

	if result == nil {
		writeJSON(w, 200, map[string]any{"has_result": false})
		return
	}
	writeJSON(w, 200, map[string]any{"has_result": true, "result": result})
}
