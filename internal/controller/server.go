package controller

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/fangbm/cifleet/internal/model"
	"github.com/fangbm/cifleet/internal/scheduler"
)

type Server struct {
	log       *slog.Logger
	scheduler *scheduler.Scheduler
	mu        sync.RWMutex
	nodes     map[string]model.Node
}

func New(log *slog.Logger) *Server {
	return &Server{log: log, scheduler: scheduler.New(), nodes: make(map[string]model.Node)}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /v1/nodes", s.listNodes)
	mux.HandleFunc("POST /v1/nodes/heartbeat", s.heartbeat)
	mux.HandleFunc("POST /v1/schedule", s.schedule)
	return mux
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	var n model.Node
	if err := json.NewDecoder(r.Body).Decode(&n); err != nil || n.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid node heartbeat"})
		return
	}
	n.LastSeen = time.Now().UTC()
	n.Online = true
	s.mu.Lock()
	s.nodes[n.ID] = n
	s.mu.Unlock()
	writeJSON(w, http.StatusAccepted, n)
}

func (s *Server) listNodes(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	nodes := make([]model.Node, 0, len(s.nodes))
	for _, n := range s.nodes {
		nodes = append(nodes, n)
	}
	writeJSON(w, http.StatusOK, nodes)
}

func (s *Server) schedule(w http.ResponseWriter, r *http.Request) {
	var req model.JobRequirements
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid requirements"})
		return
	}
	s.mu.RLock()
	nodes := make([]model.Node, 0, len(s.nodes))
	for _, n := range s.nodes {
		nodes = append(nodes, n)
	}
	s.mu.RUnlock()
	placement, err := s.scheduler.Place(req, nodes)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, placement)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
