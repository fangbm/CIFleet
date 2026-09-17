package controller

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/fangbm/cifleet/internal/model"
	"github.com/fangbm/cifleet/internal/scheduler"
)

type Server struct {
	log       *slog.Logger
	scheduler *scheduler.Scheduler
	nodeTTL   time.Duration
	mu        sync.RWMutex
	nodes     map[string]model.Node
}

func New(log *slog.Logger, nodeTTL ...time.Duration) *Server {
	ttl := 45 * time.Second
	if len(nodeTTL) > 0 && nodeTTL[0] > 0 {
		ttl = nodeTTL[0]
	}
	return &Server{log: log, scheduler: scheduler.New(), nodeTTL: ttl, nodes: make(map[string]model.Node)}
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
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&n); err != nil || n.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid node heartbeat"})
		return
	}
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		peerCN := r.TLS.PeerCertificates[0].Subject.CommonName
		if peerCN == "" || subtle.ConstantTimeCompare([]byte(peerCN), []byte(n.ID)) != 1 {
			s.log.Warn("heartbeat certificate identity mismatch", "node_id", n.ID, "certificate_cn", peerCN)
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "client certificate identity does not match node id"})
			return
		}
	}
	n.LastSeen = time.Now().UTC()
	n.Online = true
	s.mu.Lock()
	s.nodes[n.ID] = n
	s.mu.Unlock()
	writeJSON(w, http.StatusAccepted, n)
}

func (s *Server) listNodes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.snapshotNodes(time.Now().UTC()))
}

func (s *Server) schedule(w http.ResponseWriter, r *http.Request) {
	var req model.JobRequirements
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid requirements"})
		return
	}
	placement, err := s.scheduler.Place(req, s.snapshotNodes(time.Now().UTC()))
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, placement)
}

func (s *Server) snapshotNodes(now time.Time) []model.Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	nodes := make([]model.Node, 0, len(s.nodes))
	for _, stored := range s.nodes {
		n := stored
		n.Online = !n.LastSeen.IsZero() && now.Sub(n.LastSeen) <= s.nodeTTL
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
