package controller

import (
	"context"
	"github.com/fangbm/cifleet/internal/model"
	"github.com/fangbm/cifleet/internal/scheduler"
	"strconv"
	"strings"
	"time"
)

func (m *RunnerManager) handleCompleted(ctx context.Context, event WorkflowJobEvent) error {
	if event.Job.ID <= 0 {
		return nil
	}
	m.mu.Lock()
	record, ok := m.jobs[event.Job.ID]
	m.mu.Unlock()
	if !ok {
		if node, found := m.nodeFromRunnerName(event.Job.RunnerName, event.Job.ID); found {
			_ = m.agents.DestroyInstance(ctx, node, containerName(event.Job.ID))
		}
		m.mu.Lock()
		m.jobs[event.Job.ID] = JobRecord{JobID: event.Job.ID, RunID: event.Job.RunID, InstallationID: event.InstallationID, Repository: event.Repository.FullName, Status: jobCompleted, UpdatedAt: time.Now().UTC()}
		err := m.saveLocked()
		m.mu.Unlock()
		return err
	}
	if record.Status == jobCompleted {
		return nil
	}
	if record.NodeID != "" {
		if node, found := m.rawNode(record.NodeID); found {
			instanceID := record.InstanceID
			if instanceID == "" {
				instanceID = containerName(event.Job.ID)
			}
			if err := m.agents.DestroyInstance(ctx, node, instanceID); err != nil {
				m.mu.Lock()
				current := m.jobs[event.Job.ID]
				current.Status = jobCleanupPending
				current.UpdatedAt = time.Now().UTC()
				m.jobs[event.Job.ID] = current
				saveErr := m.saveLocked()
				m.mu.Unlock()
				if saveErr != nil {
					return saveErr
				}
				return err
			}
		}
	}
	if record.RunnerID > 0 {
		_ = m.github.DeleteRunner(ctx, record.InstallationID, record.Owner, record.Repo, record.RunnerID)
	}
	m.mu.Lock()
	current := m.jobs[event.Job.ID]
	current.Status = jobCompleted
	current.InstanceID = ""
	current.UpdatedAt = time.Now().UTC()
	m.jobs[event.Job.ID] = current
	err := m.saveLocked()
	m.mu.Unlock()
	return err
}

func (m *RunnerManager) reconcile(ctx context.Context, now time.Time) {
	m.mu.Lock()
	ids := make([]int64, 0, len(m.jobs))
	pruned := false
	for id, record := range m.jobs {
		if (record.Status == jobCompleted || record.Status == jobRejected) && now.Sub(record.UpdatedAt) > m.cfg.TombstoneTTL {
			delete(m.jobs, id)
			pruned = true
			continue
		}
		ids = append(ids, id)
	}
	if pruned {
		_ = m.saveLocked()
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.mu.Lock()
		record, ok := m.jobs[id]
		m.mu.Unlock()
		if !ok {
			continue
		}
		switch record.Status {
		case jobPending:
			if err := m.tryProvision(ctx, id); err != nil {
				m.log.Warn("retry pending workflow job", "job_id", id, "error", err)
			}
		case jobProvisioning:
			// Provisioning is transient while the controller is alive. Seeing it during
			// reconciliation means a previous process likely died after creating the JIT
			// runner but before persisting the Docker instance. Tear down both deterministic
			// sides and retry from pending instead of leaving the GitHub job queued until TTL.
			if err := m.recoverProvisioning(ctx, record); err != nil {
				m.log.Warn("recover interrupted runner provisioning", "job_id", id, "error", err)
			}
		case jobCleanupPending:
			if err := m.cleanupRecord(ctx, record); err != nil {
				m.log.Warn("retry workflow job cleanup", "job_id", id, "error", err)
			}
		case jobRunning:
			if !record.Deadline.IsZero() && now.After(record.Deadline.Add(2*time.Minute)) {
				if err := m.cleanupRecord(ctx, record); err != nil {
					m.log.Warn("cleanup expired workflow job", "job_id", id, "error", err)
				}
			}
		}
	}
}

func (m *RunnerManager) recoverProvisioning(ctx context.Context, record JobRecord) error {
	if record.NodeID != "" {
		if node, ok := m.rawNode(record.NodeID); ok {
			_ = m.agents.DestroyInstance(ctx, node, containerName(record.JobID))
		}
	}
	if record.RunnerID > 0 {
		_ = m.github.DeleteRunner(ctx, record.InstallationID, record.Owner, record.Repo, record.RunnerID)
	}
	m.mu.Lock()
	current, ok := m.jobs[record.JobID]
	if !ok || current.Status != jobProvisioning {
		m.mu.Unlock()
		return nil
	}
	current.Status = jobPending
	current.NodeID = ""
	current.InstanceID = ""
	current.RunnerName = ""
	current.RunnerID = 0
	current.Deadline = time.Time{}
	current.UpdatedAt = time.Now().UTC()
	m.jobs[record.JobID] = current
	err := m.saveLocked()
	m.mu.Unlock()
	if err != nil {
		return err
	}
	return m.tryProvision(ctx, record.JobID)
}

func (m *RunnerManager) cleanupRecord(ctx context.Context, record JobRecord) error {
	if record.NodeID != "" {
		if node, ok := m.rawNode(record.NodeID); ok {
			instanceID := record.InstanceID
			if instanceID == "" {
				instanceID = containerName(record.JobID)
			}
			if err := m.agents.DestroyInstance(ctx, node, instanceID); err != nil {
				return err
			}
		}
	}
	if record.RunnerID > 0 {
		_ = m.github.DeleteRunner(ctx, record.InstallationID, record.Owner, record.Repo, record.RunnerID)
	}
	m.mu.Lock()
	current := m.jobs[record.JobID]
	current.Status = jobCompleted
	current.InstanceID = ""
	current.UpdatedAt = time.Now().UTC()
	m.jobs[record.JobID] = current
	err := m.saveLocked()
	m.mu.Unlock()
	return err
}

func (m *RunnerManager) place(req model.JobRequirements) (model.Placement, model.Node, error) {
	nodes := m.controller.snapshotNodes(time.Now().UTC())
	reservations := m.reservations()
	for i := range nodes {
		r := reservations[nodes[i].ID]
		if nodes[i].Capacity.TotalCPU > 0 {
			nodes[i].Capacity.FreeCPU = minInt(nodes[i].Capacity.FreeCPU, maxInt(0, nodes[i].Capacity.TotalCPU-r.TotalCPU))
		}
		if nodes[i].Capacity.TotalMemoryM > 0 {
			nodes[i].Capacity.FreeMemoryM = minInt(nodes[i].Capacity.FreeMemoryM, maxInt(0, nodes[i].Capacity.TotalMemoryM-r.TotalMemoryM))
		}
		nodes[i].Capacity.RunningJobs = maxInt(nodes[i].Capacity.RunningJobs, r.RunningJobs)
	}
	placement, err := m.controller.scheduler.Place(req, nodes)
	if err != nil {
		return model.Placement{}, model.Node{}, err
	}
	for _, node := range nodes {
		if node.ID == placement.NodeID {
			return placement, node, nil
		}
	}
	return model.Placement{}, model.Node{}, scheduler.ErrNoPlacement
}

func (m *RunnerManager) reservations() map[string]model.Capacity {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]model.Capacity)
	for _, record := range m.jobs {
		if record.NodeID == "" || (record.Status != jobProvisioning && record.Status != jobRunning && record.Status != jobCleanupPending) {
			continue
		}
		r := out[record.NodeID]
		r.TotalCPU += record.CPU
		r.TotalMemoryM += record.MemoryM
		r.RunningJobs++
		out[record.NodeID] = r
	}
	return out
}
func (m *RunnerManager) rawNode(id string) (model.Node, bool) {
	m.controller.mu.RLock()
	defer m.controller.mu.RUnlock()
	node, ok := m.controller.nodes[id]
	return node, ok
}
func (m *RunnerManager) nodeFromRunnerName(name string, jobID int64) (model.Node, bool) {
	if name == "" {
		return model.Node{}, false
	}
	m.controller.mu.RLock()
	defer m.controller.mu.RUnlock()
	suffix := "-" + strconv.FormatInt(jobID, 10)
	for _, node := range m.controller.nodes {
		prefix := "cifleet-" + safeName(node.ID) + "-"
		if strings.HasPrefix(name, prefix) && strings.Contains(name, suffix) {
			return node, true
		}
	}
	return model.Node{}, false
}
