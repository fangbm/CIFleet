package agent

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/fangbm/cifleet/internal/backend"
	"github.com/fangbm/cifleet/internal/model"
)

type Backend interface {
	backend.Backend
	backend.Janitor
}

type Server struct {
	NodeID             string
	ControllerIdentity string
	Backend            Backend
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /v1/capacity", s.capacity)
	mux.HandleFunc("POST /v1/instances", s.createInstance)
	mux.HandleFunc("DELETE /v1/instances/{id}", s.destroyInstance)
	return mux
}
func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "node_id": s.NodeID, "goos": runtime.GOOS, "goarch": runtime.GOARCH, "time": time.Now().UTC()})
}
func (s *Server) capacity(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeController(w, r) {
		return
	}
	if s.Backend == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "backend unavailable"})
		return
	}
	capacity, err := s.Backend.Capacity(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, capacity)
}
func (s *Server) createInstance(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeController(w, r) {
		return
	}
	if s.Backend == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "backend unavailable"})
		return
	}
	var spec backend.JobSpec
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&spec); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid job spec: " + err.Error()})
		return
	}
	instance, err := s.Backend.Create(r.Context(), spec)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, instance)
}
func (s *Server) destroyInstance(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeController(w, r) {
		return
	}
	if s.Backend == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "backend unavailable"})
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "instance id is required"})
		return
	}
	if err := s.Backend.Destroy(r.Context(), id); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authorizeController(w http.ResponseWriter, r *http.Request) bool {
	// Plain HTTP is only available through the explicit development flag. In mTLS mode,
	// mutating worker APIs are restricted to the controller certificate rather than any
	// certificate issued by the CIFleet CA.
	if r.TLS == nil {
		return true
	}
	wanted := s.ControllerIdentity
	if wanted == "" {
		wanted = "controller"
	}
	if len(r.TLS.PeerCertificates) == 0 {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "controller client certificate is required"})
		return false
	}
	got := r.TLS.PeerCertificates[0].Subject.CommonName
	if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(wanted)) != 1 {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "client certificate is not the CIFleet controller"})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	if value != nil {
		_ = json.NewEncoder(w).Encode(value)
	}
}

var errUnsupportedPlatform = errors.New("unsupported platform")

func runtimePlatform() (model.OS, model.Arch, error) {
	var osName model.OS
	switch runtime.GOOS {
	case "linux":
		osName = model.OSLinux
	case "windows":
		osName = model.OSWindows
	case "darwin":
		osName = model.OSMacOS
	default:
		return "", "", errUnsupportedPlatform
	}
	var arch model.Arch
	switch runtime.GOARCH {
	case "amd64":
		arch = model.ArchAMD64
	case "arm64":
		arch = model.ArchARM64
	default:
		return "", "", errUnsupportedPlatform
	}
	return osName, arch, nil
}
