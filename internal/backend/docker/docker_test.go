package docker

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fangbm/cifleet/internal/backend"
)

type fakeRunner struct {
	calls [][]string
	fn    func(name string, args []string) (string, error)
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	if f.fn != nil {
		return f.fn(name, args)
	}
	return "", nil
}
func TestCreateAddsIsolationLabelsAndRepositoryScopedCache(t *testing.T) {
	cacheRoot := t.TempDir()
	fake := &fakeRunner{fn: func(_ string, args []string) (string, error) {
		if len(args) > 0 && args[0] == "run" {
			return "container123", nil
		}
		return "", nil
	}}
	b := newWithRunner(Config{Binary: "docker", NodeID: "e5", CacheRoot: cacheRoot, DefaultTimeout: time.Hour}, fake)
	instance, err := b.Create(context.Background(), backend.JobSpec{ID: "job-1", Repository: "fangbm/CIFleet", Image: "ubuntu:24.04", Command: []string{"sleep", "60"}, CPU: 2, MemoryM: 1024, CacheMounts: []backend.CacheMount{{Name: "go-build", Target: "/cache/go-build"}}})
	if err != nil {
		t.Fatal(err)
	}
	if instance.ID != "container123" || instance.NodeID != "e5" {
		t.Fatalf("unexpected instance: %+v", instance)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("expected one docker call, got %d", len(fake.calls))
	}
	joined := strings.Join(fake.calls[0], " ")
	for _, wanted := range []string{"--label io.cifleet.managed=true", "--label io.cifleet.repository=fangbm/CIFleet", "--cpus 2", "--memory 1024m", "ubuntu:24.04 sleep 60"} {
		if !strings.Contains(joined, wanted) {
			t.Fatalf("docker args missing %q: %s", wanted, joined)
		}
	}
	hostPath, err := b.cachePath("fangbm/CIFleet", "go-build")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(joined, "type=bind,src="+hostPath+",dst=/cache/go-build") {
		t.Fatalf("repository cache mount missing: %s", joined)
	}
	if filepath.Dir(filepath.Dir(hostPath)) != cacheRoot {
		t.Fatalf("cache escaped root: %s", hostPath)
	}
}
func TestCachePathSeparatesRepositories(t *testing.T) {
	b := New(Config{CacheRoot: t.TempDir()})
	a, _ := b.cachePath("owner/a", "cargo")
	bPath, _ := b.cachePath("owner/b", "cargo")
	if a == bPath {
		t.Fatalf("different repositories share cache path: %s", a)
	}
}
func TestCleanupExpiredUsesPersistentDeadlineLabel(t *testing.T) {
	now := time.Unix(2000, 0).UTC()
	fake := &fakeRunner{fn: func(_ string, args []string) (string, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(joined, "ps -a"):
			return "old\nnew", nil
		case strings.Contains(joined, "inspect") && strings.HasSuffix(joined, " old"):
			return "1000", nil
		case strings.Contains(joined, "inspect") && strings.HasSuffix(joined, " new"):
			return "3000", nil
		case joined == "rm -f old":
			return "old", nil
		default:
			return "", fmt.Errorf("unexpected docker args: %s", joined)
		}
	}}
	b := newWithRunner(Config{Binary: "docker"}, fake)
	removed, err := b.CleanupExpired(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("expected one expired container removed, got %d", removed)
	}
}
func TestRejectsUnsafeCacheTarget(t *testing.T) {
	b := newWithRunner(Config{CacheRoot: t.TempDir()}, &fakeRunner{})
	_, err := b.Create(context.Background(), backend.JobSpec{ID: "job", Repository: "owner/repo", Image: "ubuntu", CacheMounts: []backend.CacheMount{{Name: "x", Target: "../escape"}}})
	if err == nil {
		t.Fatal("expected relative cache target to be rejected")
	}
}

func TestDestroyTreatsMissingContainerAsSuccess(t *testing.T) {
	fake := &fakeRunner{fn: func(_ string, args []string) (string, error) {
		if strings.Join(args, " ") == "rm -f missing" {
			return "", fmt.Errorf("docker rm -f missing: No such container: missing")
		}
		return "", nil
	}}
	b := newWithRunner(Config{Binary: "docker"}, fake)
	if err := b.Destroy(context.Background(), "missing"); err != nil {
		t.Fatalf("expected missing container destroy to be idempotent: %v", err)
	}
}
