package controller

import (
	"context"
	"errors"
	"fmt"
	"github.com/fangbm/cifleet/internal/backend"
	"github.com/fangbm/cifleet/internal/model"
	"github.com/fangbm/cifleet/internal/scheduler"
	"time"
)

func (m *RunnerManager) tryProvision(ctx context.Context, jobID int64) error {
	m.mu.Lock()
	record, ok := m.jobs[jobID]
	if !ok || record.Status != jobPending {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	run, err := m.github.GetWorkflowRun(ctx, record.InstallationID, record.Owner, record.Repo, record.RunID)
	if err != nil {
		return fmt.Errorf("get workflow run trust context: %w", err)
	}
	if trusted, reason := trustedWorkflowRun(run, record.Repository); !trusted {
		m.mu.Lock()
		current := m.jobs[jobID]
		current.Status = jobRejected
		current.UpdatedAt = time.Now().UTC()
		m.jobs[jobID] = current
		saveErr := m.saveLocked()
		m.mu.Unlock()
		m.log.Warn("CIFleet rejected workflow job", "job_id", jobID, "repository", record.Repository, "reason", reason)
		return saveErr
	}

	req, err := requirementsFromLabels(record.Repository, record.Labels, record.CPU, record.MemoryM)
	if err != nil {
		m.mu.Lock()
		current := m.jobs[jobID]
		current.Status = jobRejected
		current.UpdatedAt = time.Now().UTC()
		m.jobs[jobID] = current
		saveErr := m.saveLocked()
		m.mu.Unlock()
		if saveErr != nil {
			return saveErr
		}
		return err
	}
	placement, node, err := m.place(req)
	if err != nil {
		if errors.Is(err, scheduler.ErrNoPlacement) {
			m.log.Debug("workflow job waiting for capacity", "job_id", jobID, "repository", record.Repository)
			return nil
		}
		return err
	}
	if placement.Backend != model.BackendDocker {
		return fmt.Errorf("runner launcher for backend %q is not implemented in M2", placement.Backend)
	}

	m.mu.Lock()
	record = m.jobs[jobID]
	record.Attempt++
	attempt := record.Attempt
	m.mu.Unlock()
	runnerName := runnerName(node.ID, jobID, attempt)
	jit, err := m.github.GenerateJITConfig(ctx, record.InstallationID, record.Owner, record.Repo, runnerName, m.cfg.RunnerGroupID, record.Labels)
	if err != nil {
		return fmt.Errorf("generate JIT runner config: %w", err)
	}

	now := time.Now().UTC()
	m.mu.Lock()
	record = m.jobs[jobID]
	record.Status = jobProvisioning
	record.NodeID = node.ID
	record.RunnerName = runnerName
	record.RunnerID = jit.RunnerID
	record.Attempt = attempt
	record.Deadline = now.Add(m.cfg.RunnerTimeout)
	record.UpdatedAt = now
	m.jobs[jobID] = record
	if err := m.saveLocked(); err != nil {
		m.mu.Unlock()
		_ = m.github.DeleteRunner(ctx, record.InstallationID, record.Owner, record.Repo, jit.RunnerID)
		return err
	}
	m.mu.Unlock()

	spec := backend.JobSpec{ID: containerJobID(jobID), Repository: record.Repository, Image: m.cfg.RunnerImage, Command: []string{"/home/runner/run.sh"}, CPU: record.CPU, MemoryM: record.MemoryM, TimeoutSeconds: int64(m.cfg.RunnerTimeout / time.Second), Environment: map[string]string{"ACTIONS_RUNNER_INPUT_JITCONFIG": jit.EncodedConfig, "ACTIONS_RUNNER_PRINT_LOG_TO_STDOUT": "1"}}
	instance, err := m.agents.CreateInstance(ctx, node, spec)
	if err != nil {
		_ = m.agents.DestroyInstance(context.Background(), node, containerName(jobID))
		_ = m.github.DeleteRunner(context.Background(), record.InstallationID, record.Owner, record.Repo, jit.RunnerID)
		m.mu.Lock()
		current := m.jobs[jobID]
		current.Status = jobPending
		current.NodeID = ""
		current.InstanceID = ""
		current.RunnerName = ""
		current.RunnerID = 0
		current.Deadline = time.Time{}
		current.UpdatedAt = time.Now().UTC()
		m.jobs[jobID] = current
		saveErr := m.saveLocked()
		m.mu.Unlock()
		if saveErr != nil {
			return saveErr
		}
		return fmt.Errorf("create runner instance: %w", err)
	}
	m.mu.Lock()
	current := m.jobs[jobID]
	current.Status = jobRunning
	current.InstanceID = instance.ID
	if !instance.Deadline.IsZero() {
		current.Deadline = instance.Deadline
	}
	current.UpdatedAt = time.Now().UTC()
	m.jobs[jobID] = current
	err = m.saveLocked()
	m.mu.Unlock()
	if err == nil {
		m.log.Info("JIT runner started", "job_id", jobID, "repository", record.Repository, "node_id", node.ID, "runner", runnerName, "instance", instance.ID)
	}
	return err
}
