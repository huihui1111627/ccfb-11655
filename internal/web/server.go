// Package web serves the LoRa network dashboard and its JSON API. All
// simulation work happens inside the lorasim/internal/sim engine; this package
// only caches results and renders them.
package web

import (
	"encoding/json"
	"net/http"
	"sync"

	"lorasim/internal/sim"
)

// Server holds the cached comparison result behind a mutex so a re-run cannot
// race a reader. The simulator itself is deterministic and single-threaded.
type Server struct {
	mu      sync.RWMutex
	current *sim.Comparison
	seed    int64
	nodes   int
	hours   int
}

// NewServer runs the initial comparison used to populate the dashboard.
func NewServer(seed int64, nodes, hours int) *Server {
	s := &Server{seed: seed, nodes: nodes, hours: hours}
	s.current = sim.Compare(seed, nodes, int64(hours)*3600)
	return s
}

// Handler wires the API and the embedded static dashboard.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/comparison", s.handleComparison)
	mux.HandleFunc("POST /api/rerun", s.handleRerun)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("GET /", http.FileServerFS(staticFS()))
	return mux
}

func (s *Server) handleComparison(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	writeJSON(w, http.StatusOK, s.current)
}

// rerunRequest optionally overrides the simulation parameters.
type rerunRequest struct {
	Seed  *int64 `json:"seed"`
	Nodes *int   `json:"nodes"`
	Hours *int   `json:"hours"`
}

func (s *Server) handleRerun(w http.ResponseWriter, r *http.Request) {
	var req rerunRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	s.mu.Lock()
	if req.Seed != nil {
		s.seed = *req.Seed
	}
	if req.Nodes != nil && *req.Nodes > 0 && *req.Nodes <= 2000 {
		s.nodes = *req.Nodes
	}
	if req.Hours != nil && *req.Hours > 0 && *req.Hours <= 168 {
		s.hours = *req.Hours
	}
	cmp := sim.Compare(s.seed, s.nodes, int64(s.hours)*3600)
	s.current = cmp
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, cmp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
