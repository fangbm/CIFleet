package docker

import (
	"context"
	"errors"

	"github.com/fangbm/cifleet/internal/backend"
	"github.com/fangbm/cifleet/internal/model"
)

type Backend struct{}

func New() *Backend                        { return &Backend{} }
func (b *Backend) Kind() model.BackendKind { return model.BackendDocker }
func (b *Backend) Capacity(context.Context) (model.Capacity, error) {
	return model.Capacity{}, errors.New("Docker backend capacity probe not implemented")
}
func (b *Backend) Create(context.Context, backend.JobSpec) (*backend.Instance, error) {
	return nil, errors.New("Docker backend create not implemented")
}
func (b *Backend) Destroy(context.Context, string) error {
	return errors.New("Docker backend destroy not implemented")
}
