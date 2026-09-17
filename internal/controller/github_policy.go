package controller

import (
	"errors"
	"fmt"
	"github.com/fangbm/cifleet/internal/githubapp"
	"github.com/fangbm/cifleet/internal/model"
	"strconv"
	"strings"
)

func trustedWorkflowRun(run githubapp.WorkflowRun, expectedRepository string) (bool, string) {
	if run.Repository.FullName != "" && !strings.EqualFold(run.Repository.FullName, expectedRepository) {
		return false, "workflow run repository does not match webhook repository"
	}
	if run.Repository.Private {
		return true, "private repository"
	}
	switch run.Event {
	case "pull_request_target":
		return false, "pull_request_target is denied on public repositories"
	case "pull_request":
		if run.HeadRepository == nil || !strings.EqualFold(run.HeadRepository.FullName, expectedRepository) {
			return false, "fork pull request is denied on public repositories"
		}
	}
	return true, "trusted event"
}

func requirementsFromLabels(repository string, labels []string, cpu, memory int) (model.JobRequirements, error) {
	lower := make(map[string]bool, len(labels))
	for _, label := range labels {
		lower[strings.ToLower(strings.TrimSpace(label))] = true
	}
	req := model.JobRequirements{Repository: repository, Isolation: model.IsolationContainer, CPU: cpu, MemoryM: memory}
	switch {
	case lower["linux-x64"] || (lower["linux"] && lower["x64"]):
		req.OS = model.OSLinux
		req.Arch = model.ArchAMD64
	case lower["linux-arm64"] || (lower["linux"] && lower["arm64"]):
		req.OS = model.OSLinux
		req.Arch = model.ArchARM64
	case lower["windows-x64"] || (lower["windows"] && lower["x64"]):
		req.OS = model.OSWindows
		req.Arch = model.ArchAMD64
		req.Isolation = model.IsolationVM
	case lower["macos-arm64"] || (lower["macos"] && lower["arm64"]):
		req.OS = model.OSMacOS
		req.Arch = model.ArchARM64
		req.Isolation = model.IsolationVM
	default:
		return model.JobRequirements{}, errors.New("CIFleet job needs a supported platform label such as linux-x64")
	}
	if lower["cifleet-vm"] {
		req.Isolation = model.IsolationVM
	}
	for label := range lower {
		if strings.HasPrefix(label, "cifleet-cap-") {
			capability := strings.TrimPrefix(label, "cifleet-cap-")
			if capability != "" {
				req.Capabilities = append(req.Capabilities, capability)
			}
		}
	}
	return req, nil
}

func repoParts(repo RepositoryInfo) (string, string, error) {
	owner, name := strings.TrimSpace(repo.Owner), strings.TrimSpace(repo.Name)
	if owner != "" && name != "" {
		return owner, name, nil
	}
	parts := strings.SplitN(repo.FullName, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repository name %q", repo.FullName)
	}
	return parts[0], parts[1], nil
}
func hasLabel(labels []string, wanted string) bool {
	for _, label := range labels {
		if strings.EqualFold(strings.TrimSpace(label), strings.TrimSpace(wanted)) {
			return true
		}
	}
	return false
}
func runnerName(nodeID string, jobID int64, attempt int) string {
	return fmt.Sprintf("cifleet-%s-%d-a%d", safeName(nodeID), jobID, attempt)
}
func containerJobID(jobID int64) string { return "gh-" + strconv.FormatInt(jobID, 10) }
func containerName(jobID int64) string  { return "cifleet-" + containerJobID(jobID) }
func safeName(value string) string {
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
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
