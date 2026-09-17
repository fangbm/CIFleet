package scheduler

import (
	"errors"
	"sort"

	"github.com/fangbm/cifleet/internal/model"
)

var ErrNoPlacement = errors.New("no eligible node found")

type Scheduler struct{}

func New() *Scheduler { return &Scheduler{} }

func (s *Scheduler) Place(req model.JobRequirements, nodes []model.Node) (model.Placement, error) {
	candidates := make([]candidate, 0, len(nodes))
	for _, n := range nodes {
		if !eligible(req, n) {
			continue
		}
		backend, ok := selectBackend(req.Isolation, n.Backends)
		if !ok {
			continue
		}
		candidates = append(candidates, candidate{
			placement:  model.Placement{NodeID: n.ID, Backend: backend},
			freeCPU:    n.Capacity.FreeCPU,
			freeMemory: n.Capacity.FreeMemoryM,
			running:    n.Capacity.RunningJobs,
		})
	}

	if len(candidates) == 0 {
		return model.Placement{}, ErrNoPlacement
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].running != candidates[j].running {
			return candidates[i].running < candidates[j].running
		}
		if candidates[i].freeCPU != candidates[j].freeCPU {
			return candidates[i].freeCPU > candidates[j].freeCPU
		}
		return candidates[i].freeMemory > candidates[j].freeMemory
	})

	return candidates[0].placement, nil
}

type candidate struct {
	placement  model.Placement
	freeCPU    int
	freeMemory int
	running    int
}

func eligible(req model.JobRequirements, n model.Node) bool {
	if !n.Online || req.OS != n.OS || req.Arch != n.Arch {
		return false
	}
	if req.CPU > 0 && n.Capacity.FreeCPU < req.CPU {
		return false
	}
	if req.MemoryM > 0 && n.Capacity.FreeMemoryM < req.MemoryM {
		return false
	}
	for _, capability := range req.Capabilities {
		if !contains(n.Capabilities, capability) {
			return false
		}
	}
	return true
}

func selectBackend(isolation model.Isolation, backends []model.BackendKind) (model.BackendKind, bool) {
	preferred := map[model.Isolation][]model.BackendKind{
		model.IsolationContainer: {model.BackendDocker},
		model.IsolationVM:        {model.BackendKVM, model.BackendHyperV},
		model.IsolationNative:    {model.BackendNative},
	}[isolation]
	for _, p := range preferred {
		for _, b := range backends {
			if p == b {
				return b, true
			}
		}
	}
	return "", false
}

func contains(values []string, wanted string) bool {
	for _, v := range values {
		if v == wanted {
			return true
		}
	}
	return false
}
