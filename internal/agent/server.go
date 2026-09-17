package agent

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"
)

type Server struct {
	NodeID string
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "node_id": s.NodeID, "goos": runtime.GOOS,
			"goarch": runtime.GOARCH, "time": time.Now().UTC(),
		})
	})
	return mux
}
