package backend

import (
	"context"

	"github.com/fangbm/cifleet/internal/model"
)

type JobSpec struct {
	ID           string
	Repository   string
	RunnerConfig string
	CPU          int
	MemoryM      int
	Environment  map[string]string
	CachePaths   []string
}

type Instance struct {
	ID      string
	Backend model.BackendKind
	NodeID  string
}

type Backend interface {
	Kind() model.BackendKind
	Capacity(ctx context.Context) (model.Capacity, error)
	Create(ctx context.Context, spec JobSpec) (*Instance, error)
	Destroy(ctx context.Context, instanceID string) error
}
