package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func (m *RunnerManager) loadState() error {
	if m.cfg.StateFile == "" {
		return nil
	}
	data, err := os.ReadFile(m.cfg.StateFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read controller state: %w", err)
	}
	var state persistedJobState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("decode controller state: %w", err)
	}
	if state.Jobs != nil {
		m.jobs = state.Jobs
	}
	return nil
}
func (m *RunnerManager) saveLocked() error {
	if m.cfg.StateFile == "" {
		return nil
	}
	dir := filepath.Dir(m.cfg.StateFile)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create controller state directory: %w", err)
	}
	data, err := json.MarshalIndent(persistedJobState{Jobs: m.jobs}, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.cfg.StateFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write controller state: %w", err)
	}
	if err := os.Rename(tmp, m.cfg.StateFile); err != nil {
		return fmt.Errorf("replace controller state: %w", err)
	}
	return nil
}
