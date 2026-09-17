package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/fangbm/cifleet/internal/model"
)

type ServiceConfig struct {
	NodeID, Endpoint, ControllerURL    string
	HeartbeatInterval, CleanupInterval time.Duration
	Capabilities, Labels               []string
}
type Service struct {
	cfg     ServiceConfig
	backend Backend
	client  *http.Client
	log     *slog.Logger
}

func NewService(cfg ServiceConfig, b Backend, client *http.Client, log *slog.Logger) *Service {
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 15 * time.Second
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = time.Minute
	}
	return &Service{cfg: cfg, backend: b, client: client, log: log}
}
func (s *Service) Run(ctx context.Context) error {
	if s.cfg.NodeID == "" {
		return fmt.Errorf("node id is required")
	}
	if s.backend == nil {
		return fmt.Errorf("backend is required")
	}
	if s.client == nil {
		return fmt.Errorf("http client is required")
	}
	heartbeatTicker := time.NewTicker(s.cfg.HeartbeatInterval)
	cleanupTicker := time.NewTicker(s.cfg.CleanupInterval)
	defer heartbeatTicker.Stop()
	defer cleanupTicker.Stop()
	s.sendHeartbeat(ctx)
	s.cleanup(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-heartbeatTicker.C:
			s.sendHeartbeat(ctx)
		case now := <-cleanupTicker.C:
			s.cleanupAt(ctx, now.UTC())
		}
	}
}
func (s *Service) sendHeartbeat(ctx context.Context) {
	osName, arch, err := runtimePlatform()
	if err != nil {
		s.log.Error("cannot determine runtime platform", "error", err)
		return
	}
	node := model.Node{ID: s.cfg.NodeID, Endpoint: s.cfg.Endpoint, OS: osName, Arch: arch, Capabilities: append([]string(nil), s.cfg.Capabilities...), Labels: append([]string(nil), s.cfg.Labels...), Online: true}
	capacity, err := s.backend.Capacity(ctx)
	if err != nil {
		s.log.Warn("capacity probe failed", "error", err)
	} else {
		node.Backends = []model.BackendKind{s.backend.Kind()}
		node.Capacity = capacity
	}
	payload, err := json.Marshal(node)
	if err != nil {
		s.log.Error("encode heartbeat", "error", err)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.ControllerURL+"/v1/nodes/heartbeat", bytes.NewReader(payload))
	if err != nil {
		s.log.Error("build heartbeat request", "error", err)
		return
	}
	req.Header.Set("content-type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		s.log.Warn("heartbeat failed", "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		s.log.Warn("heartbeat rejected", "status", resp.StatusCode)
		return
	}
	s.log.Debug("heartbeat accepted", "node_id", s.cfg.NodeID)
}
func (s *Service) cleanup(ctx context.Context) { s.cleanupAt(ctx, time.Now().UTC()) }
func (s *Service) cleanupAt(ctx context.Context, now time.Time) {
	removed, err := s.backend.CleanupExpired(ctx, now)
	if err != nil {
		s.log.Warn("orphan cleanup failed", "error", err)
		return
	}
	if removed > 0 {
		s.log.Info("expired instances removed", "count", removed)
	}
}
