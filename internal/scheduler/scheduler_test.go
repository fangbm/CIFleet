package scheduler

import (
	"testing"

	"github.com/fangbm/cifleet/internal/model"
)

func TestPlaceLinuxDocker(t *testing.T) {
	nodes := []model.Node{
		{
			ID:       "e5",
			OS:       model.OSLinux,
			Arch:     model.ArchAMD64,
			Backends: []model.BackendKind{model.BackendDocker, model.BackendKVM},
			Online:   true,
			Capacity: model.Capacity{FreeCPU: 8, FreeMemoryM: 16000},
		},
	}

	got, err := New().Place(model.JobRequirements{
		OS:        model.OSLinux,
		Arch:      model.ArchAMD64,
		Isolation: model.IsolationContainer,
	}, nodes)
	if err != nil {
		t.Fatalf("Place() error = %v", err)
	}
	if got.NodeID != "e5" || got.Backend != model.BackendDocker {
		t.Fatalf("unexpected placement: %+v", got)
	}
}

func TestPlaceWindowsHyperV(t *testing.T) {
	nodes := []model.Node{
		{
			ID:       "win-pc",
			OS:       model.OSWindows,
			Arch:     model.ArchAMD64,
			Backends: []model.BackendKind{model.BackendHyperV},
			Online:   true,
			Capacity: model.Capacity{FreeCPU: 4, FreeMemoryM: 8192},
		},
	}

	got, err := New().Place(model.JobRequirements{
		OS:        model.OSWindows,
		Arch:      model.ArchAMD64,
		Isolation: model.IsolationVM,
	}, nodes)
	if err != nil {
		t.Fatalf("Place() error = %v", err)
	}
	if got.Backend != model.BackendHyperV {
		t.Fatalf("unexpected placement: %+v", got)
	}
}

func TestRejectWrongArchitecture(t *testing.T) {
	nodes := []model.Node{{
		ID:       "pi",
		OS:       model.OSLinux,
		Arch:     model.ArchARM64,
		Backends: []model.BackendKind{model.BackendDocker},
		Online:   true,
	}}

	_, err := New().Place(model.JobRequirements{
		OS:        model.OSLinux,
		Arch:      model.ArchAMD64,
		Isolation: model.IsolationContainer,
	}, nodes)
	if err != ErrNoPlacement {
		t.Fatalf("expected ErrNoPlacement, got %v", err)
	}
}
