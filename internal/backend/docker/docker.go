package docker

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/fangbm/cifleet/internal/backend"
	"github.com/fangbm/cifleet/internal/model"
)

const managedLabel = "io.cifleet.managed"

type Config struct {
	Binary         string
	NodeID         string
	CacheRoot      string
	DefaultTimeout time.Duration
}

type commandRunner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(out)), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

type Backend struct {
	cfg Config
	run commandRunner
}

func New(cfg Config) *Backend {
	if cfg.Binary == "" {
		cfg.Binary = "docker"
	}
	if cfg.CacheRoot == "" {
		cfg.CacheRoot = "/var/lib/cifleet/cache"
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 2 * time.Hour
	}
	return &Backend{cfg: cfg, run: execRunner{}}
}
func newWithRunner(cfg Config, runner commandRunner) *Backend {
	b := New(cfg)
	b.run = runner
	return b
}
func (b *Backend) Kind() model.BackendKind { return model.BackendDocker }

func (b *Backend) Capacity(ctx context.Context) (model.Capacity, error) {
	if _, err := b.run.Run(ctx, b.cfg.Binary, "info", "--format", "{{.ServerVersion}}"); err != nil {
		return model.Capacity{}, fmt.Errorf("docker unavailable: %w", err)
	}
	totalMemory, availableMemory, err := memoryMB()
	if err != nil {
		return model.Capacity{}, err
	}
	ids, err := b.managedContainerIDs(ctx, false)
	if err != nil {
		return model.Capacity{}, err
	}
	reservedCPU, reservedMemory := 0, 0
	for _, id := range ids {
		out, err := b.run.Run(ctx, b.cfg.Binary, "inspect", "--format", `{{index .Config.Labels "io.cifleet.cpu"}}|{{index .Config.Labels "io.cifleet.memory_mb"}}`, id)
		if err != nil {
			return model.Capacity{}, fmt.Errorf("inspect managed container %s: %w", id, err)
		}
		parts := strings.SplitN(out, "|", 2)
		if len(parts) == 2 {
			reservedCPU += parseNonNegativeInt(parts[0])
			reservedMemory += parseNonNegativeInt(parts[1])
		}
	}
	totalCPU := runtime.NumCPU()
	freeCPU := max(0, totalCPU-reservedCPU)
	reservationFreeMemory := max(0, totalMemory-reservedMemory)
	freeMemory := min(availableMemory, reservationFreeMemory)
	return model.Capacity{TotalCPU: totalCPU, FreeCPU: freeCPU, TotalMemoryM: totalMemory, FreeMemoryM: freeMemory, RunningJobs: len(ids)}, nil
}

func (b *Backend) Create(ctx context.Context, spec backend.JobSpec) (*backend.Instance, error) {
	if spec.ID == "" || spec.Repository == "" || spec.Image == "" {
		return nil, errors.New("job id, repository and image are required")
	}
	if spec.CPU < 0 || spec.MemoryM < 0 {
		return nil, errors.New("cpu and memory must be non-negative")
	}
	name := "cifleet-" + sanitizeName(spec.ID)
	if name == "cifleet-" {
		return nil, errors.New("job id has no usable characters")
	}
	deadline := time.Now().UTC().Add(spec.EffectiveTimeout(b.cfg.DefaultTimeout))
	args := []string{"run", "-d", "--name", name, "--label", managedLabel + "=true", "--label", "io.cifleet.node=" + b.cfg.NodeID, "--label", "io.cifleet.job_id=" + spec.ID, "--label", "io.cifleet.repository=" + spec.Repository, "--label", "io.cifleet.deadline=" + strconv.FormatInt(deadline.Unix(), 10), "--label", "io.cifleet.cpu=" + strconv.Itoa(spec.CPU), "--label", "io.cifleet.memory_mb=" + strconv.Itoa(spec.MemoryM)}
	if spec.CPU > 0 {
		args = append(args, "--cpus", strconv.Itoa(spec.CPU))
	}
	if spec.MemoryM > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", spec.MemoryM))
	}
	for key, value := range spec.Environment {
		if strings.ContainsRune(key, '=') || key == "" {
			return nil, fmt.Errorf("invalid environment variable name %q", key)
		}
		args = append(args, "--env", key+"="+value)
	}
	if spec.RunnerConfig != "" {
		args = append(args, "--env", "CIFLEET_RUNNER_CONFIG="+spec.RunnerConfig)
	}
	for _, mount := range spec.CacheMounts {
		hostPath, err := b.cachePath(spec.Repository, mount.Name)
		if err != nil {
			return nil, err
		}
		if err := validateCacheTarget(mount.Target); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(hostPath, 0o750); err != nil {
			return nil, fmt.Errorf("create cache directory: %w", err)
		}
		mountArg := "type=bind,src=" + hostPath + ",dst=" + mount.Target
		if mount.ReadOnly {
			mountArg += ",readonly"
		}
		args = append(args, "--mount", mountArg)
	}
	args = append(args, spec.Image)
	args = append(args, spec.Command...)
	id, err := b.run.Run(ctx, b.cfg.Binary, args...)
	if err != nil {
		return nil, err
	}
	return &backend.Instance{ID: strings.TrimSpace(id), Backend: model.BackendDocker, NodeID: b.cfg.NodeID, Deadline: deadline}, nil
}
func (b *Backend) Destroy(ctx context.Context, instanceID string) error {
	if strings.TrimSpace(instanceID) == "" {
		return errors.New("instance id is required")
	}
	_, err := b.run.Run(ctx, b.cfg.Binary, "rm", "-f", instanceID)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "no such container") {
		return nil
	}
	return err
}
func (b *Backend) CleanupExpired(ctx context.Context, now time.Time) (int, error) {
	ids, err := b.managedContainerIDs(ctx, true)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, id := range ids {
		deadlineText, err := b.run.Run(ctx, b.cfg.Binary, "inspect", "--format", `{{index .Config.Labels "io.cifleet.deadline"}}`, id)
		if err != nil {
			continue
		}
		deadlineUnix, err := strconv.ParseInt(strings.TrimSpace(deadlineText), 10, 64)
		if err != nil || deadlineUnix <= 0 || now.Unix() < deadlineUnix {
			continue
		}
		if _, err := b.run.Run(ctx, b.cfg.Binary, "rm", "-f", id); err == nil {
			removed++
		}
	}
	return removed, nil
}
func (b *Backend) managedContainerIDs(ctx context.Context, all bool) ([]string, error) {
	args := []string{"ps"}
	if all {
		args = append(args, "-a")
	}
	args = append(args, "--filter", "label="+managedLabel+"=true", "--format", "{{.ID}}")
	out, err := b.run.Run(ctx, b.cfg.Binary, args...)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	return strings.Fields(out), nil
}
func (b *Backend) cachePath(repository, name string) (string, error) {
	name = sanitizeName(name)
	if name == "" {
		return "", errors.New("cache mount name is required")
	}
	sum := sha256.Sum256([]byte(repository))
	repoKey := fmt.Sprintf("%x", sum[:8])
	return filepath.Join(b.cfg.CacheRoot, repoKey, name), nil
}
func validateCacheTarget(target string) error {
	if !filepath.IsAbs(target) || filepath.Clean(target) == string(filepath.Separator) {
		return fmt.Errorf("cache target %q must be an absolute non-root path", target)
	}
	return nil
}
func sanitizeName(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-._")
}
func parseNonNegativeInt(value string) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return 0
	}
	return n
}
func memoryMB() (total, available int, err error) {
	if runtime.GOOS != "linux" {
		return 0, 0, errors.New("docker capacity memory probe currently requires linux")
	}
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, fmt.Errorf("read /proc/meminfo: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		valueKB, parseErr := strconv.Atoi(fields[1])
		if parseErr != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total = valueKB / 1024
		case "MemAvailable:":
			available = valueKB / 1024
		}
	}
	if total == 0 {
		return 0, 0, errors.New("MemTotal missing from /proc/meminfo")
	}
	return total, available, nil
}
