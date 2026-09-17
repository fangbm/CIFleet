package controller

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/fangbm/cifleet/internal/backend"
	"github.com/fangbm/cifleet/internal/githubapp"
	"github.com/fangbm/cifleet/internal/model"
)

type fakeGitHubAPI struct {
	run         githubapp.WorkflowRun
	jit         githubapp.JITConfig
	getCalls    int
	jitCalls    int
	deleteCalls int
}

func (f *fakeGitHubAPI) GetWorkflowRun(context.Context, int64, string, string, int64) (githubapp.WorkflowRun, error) {
	f.getCalls++
	return f.run, nil
}
func (f *fakeGitHubAPI) GenerateJITConfig(_ context.Context, _ int64, _, _, name string, _ int64, labels []string) (githubapp.JITConfig, error) {
	f.jitCalls++
	if name == "" || len(labels) == 0 {
		panic("invalid JIT request")
	}
	return f.jit, nil
}
func (f *fakeGitHubAPI) DeleteRunner(context.Context, int64, string, string, int64) error {
	f.deleteCalls++
	return nil
}

type fakeAgentClient struct {
	creates    int
	destroys   int
	lastSpec   backend.JobSpec
	instance   *backend.Instance
	createErr  error
	destroyErr error
}

func (f *fakeAgentClient) CreateInstance(_ context.Context, node model.Node, spec backend.JobSpec) (*backend.Instance, error) {
	f.creates++
	f.lastSpec = spec
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.instance != nil {
		return f.instance, nil
	}
	return &backend.Instance{ID: "container123", Backend: model.BackendDocker, NodeID: node.ID, Deadline: time.Now().Add(time.Hour)}, nil
}
func (f *fakeAgentClient) DestroyInstance(_ context.Context, _ model.Node, _ string) error {
	f.destroys++
	return f.destroyErr
}

func testControllerWithE5() *Server {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(log, time.Minute)
	s.nodes["e5"] = model.Node{ID: "e5", Endpoint: "https://e5:8090", OS: model.OSLinux, Arch: model.ArchAMD64, Backends: []model.BackendKind{model.BackendDocker}, Capabilities: []string{"container", "docker"}, Capacity: model.Capacity{TotalCPU: 12, FreeCPU: 12, TotalMemoryM: 32768, FreeMemoryM: 30000}, Online: true, LastSeen: time.Now().UTC()}
	return s
}
func trustedRun() githubapp.WorkflowRun {
	return githubapp.WorkflowRun{ID: 10, Event: "push", Repository: githubapp.RepositoryRef{FullName: "fangbm/CIFleet", Private: false}, HeadRepository: &githubapp.RepositoryRef{FullName: "fangbm/CIFleet"}}
}
func queuedEvent() WorkflowJobEvent {
	return WorkflowJobEvent{Action: "queued", InstallationID: 42, Repository: RepositoryInfo{FullName: "fangbm/CIFleet", Owner: "fangbm", Name: "CIFleet"}, Job: WorkflowJobInfo{ID: 12, RunID: 10, Labels: []string{"self-hosted", "cifleet", "linux-x64"}}}
}

func TestRunnerManagerProvisionsAndCleansJITRunner(t *testing.T) {
	gh := &fakeGitHubAPI{run: trustedRun(), jit: githubapp.JITConfig{RunnerID: 77, EncodedConfig: "jit-secret"}}
	agents := &fakeAgentClient{}
	manager, err := NewRunnerManager(RunnerManagerConfig{StateFile: t.TempDir() + "/state.json", RunnerCPU: 2, RunnerMemoryM: 1024, RunnerTimeout: time.Hour}, testControllerWithE5(), gh, agents, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	event := queuedEvent()
	if err := manager.Process(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if agents.creates != 1 || gh.jitCalls != 1 {
		t.Fatalf("expected one provision: creates=%d jit=%d", agents.creates, gh.jitCalls)
	}
	if agents.lastSpec.Image != "ghcr.io/actions/actions-runner:latest" {
		t.Fatalf("unexpected image: %s", agents.lastSpec.Image)
	}
	if got := agents.lastSpec.Environment["ACTIONS_RUNNER_INPUT_JITCONFIG"]; got != "jit-secret" {
		t.Fatalf("JIT config missing: %q", got)
	}
	if len(agents.lastSpec.Command) != 1 || agents.lastSpec.Command[0] != "/home/runner/run.sh" {
		t.Fatalf("unexpected runner command: %v", agents.lastSpec.Command)
	}
	if err := manager.Process(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if agents.creates != 1 || gh.jitCalls != 1 {
		t.Fatal("duplicate queued delivery reprovisioned runner")
	}
	complete := event
	complete.Action = "completed"
	complete.Job.RunnerName = "cifleet-e5-12-a1"
	if err := manager.Process(context.Background(), complete); err != nil {
		t.Fatal(err)
	}
	if agents.destroys != 1 {
		t.Fatalf("expected instance destroy, got %d", agents.destroys)
	}
	manager.mu.Lock()
	status := manager.jobs[12].Status
	manager.mu.Unlock()
	if status != jobCompleted {
		t.Fatalf("expected completed tombstone, got %s", status)
	}
}

func TestRunnerManagerRejectsPublicForkPR(t *testing.T) {
	gh := &fakeGitHubAPI{run: githubapp.WorkflowRun{ID: 10, Event: "pull_request", Repository: githubapp.RepositoryRef{FullName: "fangbm/CIFleet", Private: false}, HeadRepository: &githubapp.RepositoryRef{FullName: "someone/CIFleet"}}, jit: githubapp.JITConfig{RunnerID: 77, EncodedConfig: "jit"}}
	agents := &fakeAgentClient{}
	manager, err := NewRunnerManager(RunnerManagerConfig{}, testControllerWithE5(), gh, agents, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Process(context.Background(), queuedEvent()); err != nil {
		t.Fatal(err)
	}
	if agents.creates != 0 || gh.jitCalls != 0 {
		t.Fatal("fork PR was allowed to reach self-hosted runner")
	}
	manager.mu.Lock()
	status := manager.jobs[12].Status
	manager.mu.Unlock()
	if status != jobRejected {
		t.Fatalf("expected rejected tombstone, got %s", status)
	}
}

func TestRunnerManagerKeepsJobPendingUntilCapacityAppears(t *testing.T) {
	controller := testControllerWithE5()
	node := controller.nodes["e5"]
	node.Online = false
	node.LastSeen = time.Now().Add(-2 * time.Minute)
	controller.nodes["e5"] = node
	gh := &fakeGitHubAPI{run: trustedRun(), jit: githubapp.JITConfig{RunnerID: 77, EncodedConfig: "jit"}}
	agents := &fakeAgentClient{}
	manager, err := NewRunnerManager(RunnerManagerConfig{}, controller, gh, agents, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Process(context.Background(), queuedEvent()); err != nil {
		t.Fatal(err)
	}
	if agents.creates != 0 {
		t.Fatal("offline node received runner")
	}
	manager.mu.Lock()
	status := manager.jobs[12].Status
	manager.mu.Unlock()
	if status != jobPending {
		t.Fatalf("expected pending, got %s", status)
	}
	node.Online = true
	node.LastSeen = time.Now()
	controller.nodes["e5"] = node
	if err := manager.tryProvision(context.Background(), 12); err != nil {
		t.Fatal(err)
	}
	if agents.creates != 1 {
		t.Fatal("pending job was not provisioned after capacity returned")
	}
}

func TestRequirementsFromLabels(t *testing.T) {
	req, err := requirementsFromLabels("owner/repo", []string{"self-hosted", "cifleet", "linux-x64", "cifleet-cap-docker"}, 4, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if req.OS != model.OSLinux || req.Arch != model.ArchAMD64 || req.Isolation != model.IsolationContainer || req.CPU != 4 || len(req.Capabilities) != 1 || req.Capabilities[0] != "docker" {
		t.Fatalf("unexpected requirements: %+v", req)
	}
}

func TestReconcileRecoversInterruptedProvisioning(t *testing.T) {
	gh := &fakeGitHubAPI{run: trustedRun(), jit: githubapp.JITConfig{RunnerID: 88, EncodedConfig: "fresh-jit"}}
	agents := &fakeAgentClient{}
	manager, err := NewRunnerManager(RunnerManagerConfig{StateFile: t.TempDir() + "/state.json"}, testControllerWithE5(), gh, agents, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	manager.jobs[12] = JobRecord{JobID: 12, RunID: 10, InstallationID: 42, Repository: "fangbm/CIFleet", Owner: "fangbm", Repo: "CIFleet", Labels: []string{"self-hosted", "cifleet", "linux-x64"}, Status: jobProvisioning, NodeID: "e5", RunnerID: 77, RunnerName: "cifleet-e5-12-a1", Attempt: 1, CPU: 2, MemoryM: 1024, UpdatedAt: time.Now().Add(-time.Minute)}
	manager.reconcile(context.Background(), time.Now().UTC())
	if agents.destroys == 0 {
		t.Fatal("interrupted provisioning did not clean deterministic container")
	}
	if gh.deleteCalls == 0 {
		t.Fatal("interrupted provisioning did not delete orphan JIT runner")
	}
	if agents.creates != 1 || gh.jitCalls != 1 {
		t.Fatalf("interrupted provisioning was not retried: creates=%d jit=%d", agents.creates, gh.jitCalls)
	}
	manager.mu.Lock()
	status := manager.jobs[12].Status
	attempt := manager.jobs[12].Attempt
	manager.mu.Unlock()
	if status != jobRunning || attempt != 2 {
		t.Fatalf("expected recovered running attempt 2, got status=%s attempt=%d", status, attempt)
	}
}
