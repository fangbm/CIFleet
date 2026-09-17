package backend

import (
	"context"
	"time"

	"github.com/fangbm/cifleet/internal/model"
)

type CacheMount struct {
	Name     string `json:"name"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only,omitempty"`
}

type JobSpec struct {
	ID             string            `json:"id"`
	Repository     string            `json:"repository"`
	RunnerConfig   string            `json:"runner_config,omitempty"`
	Image          string            `json:"image"`
	Command        []string          `json:"command,omitempty"`
	CPU            int               `json:"cpu,omitempty"`
	MemoryM        int               `json:"memory_mb,omitempty"`
	Environment    map[string]string `json:"environment,omitempty"`
	CacheMounts    []CacheMount      `json:"cache_mounts,omitempty"`
	Timeout        time.Duration     `json:"-"`
	TimeoutSeconds int64             `json:"timeout_seconds,omitempty"`
}

func (s JobSpec) EffectiveTimeout(fallback time.Duration) time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	if s.TimeoutSeconds > 0 {
		return time.Duration(s.TimeoutSeconds) * time.Second
	}
	return fallback
}

type Instance struct {
	ID       string            `json:"id"`
	Backend  model.BackendKind `json:"backend"`
	NodeID   string            `json:"node_id"`
	Deadline time.Time         `json:"deadline,omitempty"`
}

type Backend interface {
	Kind() model.BackendKind
	Capacity(ctx context.Context) (model.Capacity, error)
	Create(ctx context.Context, spec JobSpec) (*Instance, error)
	Destroy(ctx context.Context, instanceID string) error
}

type Janitor interface {
	CleanupExpired(ctx context.Context, now time.Time) (int, error)
}
