package controller

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/fangbm/cifleet/internal/githubapp"
)

const (
	jobPending        = "pending"
	jobProvisioning   = "provisioning"
	jobRunning        = "running"
	jobCleanupPending = "cleanup_pending"
	jobCompleted      = "completed"
	jobRejected       = "rejected"
)

var ErrEventQueueFull = errors.New("GitHub event queue is full")

type GitHubAPI interface {
	GetWorkflowRun(ctx context.Context, installationID int64, owner, repo string, runID int64) (githubapp.WorkflowRun, error)
	GenerateJITConfig(ctx context.Context, installationID int64, owner, repo, runnerName string, runnerGroupID int64, labels []string) (githubapp.JITConfig, error)
	DeleteRunner(ctx context.Context, installationID int64, owner, repo string, runnerID int64) error
}

type WorkflowJobEvent struct {
	DeliveryID     string
	Action         string
	InstallationID int64
	Repository     RepositoryInfo
	Job            WorkflowJobInfo
}

type RepositoryInfo struct {
	FullName string
	Owner    string
	Name     string
	Private  bool
}

type WorkflowJobInfo struct {
	ID         int64
	RunID      int64
	Name       string
	Labels     []string
	RunnerName string
}

type RunnerManagerConfig struct {
	MarkerLabel   string
	RunnerGroupID int64
	RunnerImage   string
	RunnerCPU     int
	RunnerMemoryM int
	RunnerTimeout time.Duration
	StateFile     string
	RetryInterval time.Duration
	TombstoneTTL  time.Duration
	QueueSize     int
}

type JobRecord struct {
	JobID          int64     `json:"job_id"`
	RunID          int64     `json:"run_id"`
	InstallationID int64     `json:"installation_id"`
	Repository     string    `json:"repository"`
	Owner          string    `json:"owner"`
	Repo           string    `json:"repo"`
	Labels         []string  `json:"labels"`
	Status         string    `json:"status"`
	NodeID         string    `json:"node_id,omitempty"`
	InstanceID     string    `json:"instance_id,omitempty"`
	RunnerName     string    `json:"runner_name,omitempty"`
	RunnerID       int64     `json:"runner_id,omitempty"`
	CPU            int       `json:"cpu,omitempty"`
	MemoryM        int       `json:"memory_mb,omitempty"`
	Attempt        int       `json:"attempt,omitempty"`
	Deadline       time.Time `json:"deadline,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type persistedJobState struct {
	Jobs map[int64]JobRecord `json:"jobs"`
}

type RunnerManager struct {
	cfg        RunnerManagerConfig
	controller *Server
	github     GitHubAPI
	agents     AgentClient
	log        *slog.Logger
	queue      chan WorkflowJobEvent

	mu   sync.Mutex
	jobs map[int64]JobRecord
}

func NewRunnerManager(cfg RunnerManagerConfig, controller *Server, github GitHubAPI, agents AgentClient, log *slog.Logger) (*RunnerManager, error) {
	if controller == nil || github == nil || agents == nil {
		return nil, errors.New("controller, GitHub client and agent client are required")
	}
	if log == nil {
		log = slog.Default()
	}
	if strings.TrimSpace(cfg.MarkerLabel) == "" {
		cfg.MarkerLabel = "cifleet"
	}
	if cfg.RunnerGroupID <= 0 {
		cfg.RunnerGroupID = 1
	}
	if cfg.RunnerImage == "" {
		cfg.RunnerImage = "ghcr.io/actions/actions-runner:latest"
	}
	if cfg.RunnerCPU <= 0 {
		cfg.RunnerCPU = 2
	}
	if cfg.RunnerMemoryM <= 0 {
		cfg.RunnerMemoryM = 4096
	}
	if cfg.RunnerTimeout <= 0 {
		cfg.RunnerTimeout = 2 * time.Hour
	}
	if cfg.RetryInterval <= 0 {
		cfg.RetryInterval = 15 * time.Second
	}
	if cfg.TombstoneTTL <= 0 {
		cfg.TombstoneTTL = 24 * time.Hour
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 256
	}
	m := &RunnerManager{cfg: cfg, controller: controller, github: github, agents: agents, log: log, queue: make(chan WorkflowJobEvent, cfg.QueueSize), jobs: make(map[int64]JobRecord)}
	if err := m.loadState(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *RunnerManager) Submit(event WorkflowJobEvent) error {
	select {
	case m.queue <- event:
		return nil
	default:
		return ErrEventQueueFull
	}
}

func (m *RunnerManager) Run(ctx context.Context) error {
	ticker := time.NewTicker(m.cfg.RetryInterval)
	defer ticker.Stop()
	m.reconcile(ctx, time.Now().UTC())
	for {
		select {
		case <-ctx.Done():
			return nil
		case event := <-m.queue:
			if err := m.Process(ctx, event); err != nil {
				m.log.Error("process GitHub workflow job event", "job_id", event.Job.ID, "action", event.Action, "error", err)
			}
		case now := <-ticker.C:
			m.reconcile(ctx, now.UTC())
		}
	}
}

func (m *RunnerManager) Process(ctx context.Context, event WorkflowJobEvent) error {
	switch event.Action {
	case "queued":
		return m.handleQueued(ctx, event)
	case "completed":
		return m.handleCompleted(ctx, event)
	default:
		return nil
	}
}

func (m *RunnerManager) handleQueued(ctx context.Context, event WorkflowJobEvent) error {
	if event.Job.ID <= 0 || event.Job.RunID <= 0 || event.InstallationID <= 0 {
		return errors.New("workflow job event is missing job, run or installation id")
	}
	if !hasLabel(event.Job.Labels, m.cfg.MarkerLabel) {
		return nil
	}
	owner, repo, err := repoParts(event.Repository)
	if err != nil {
		return err
	}

	m.mu.Lock()
	if _, exists := m.jobs[event.Job.ID]; exists {
		m.mu.Unlock()
		return nil
	}
	now := time.Now().UTC()
	record := JobRecord{JobID: event.Job.ID, RunID: event.Job.RunID, InstallationID: event.InstallationID, Repository: event.Repository.FullName, Owner: owner, Repo: repo, Labels: append([]string(nil), event.Job.Labels...), Status: jobPending, CPU: m.cfg.RunnerCPU, MemoryM: m.cfg.RunnerMemoryM, UpdatedAt: now}
	m.jobs[event.Job.ID] = record
	if err := m.saveLocked(); err != nil {
		delete(m.jobs, event.Job.ID)
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()
	return m.tryProvision(ctx, event.Job.ID)
}
