package shell

import (
	"context"
	"time"

	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/shared/objectstore"
)

func (m *Manager) StartGovernance(ctx context.Context) {
	if m == nil || m.stateStore == nil || m.restoreRegistry == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	go m.runGovernanceLoop(ctx)
}

func (m *Manager) runGovernanceLoop(ctx context.Context) {
	ticker := time.NewTicker(m.config.GovernanceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepCtx, cancel := context.WithTimeout(ctx, time.Minute)
			err := m.SweepExpiredRestoreStates(sweepCtx)
			cancel()
			if err != nil && m.logger != nil {
				m.logger.Warn("session_restore_gc", logging.LogFields{}, map[string]any{"flush_result": "failed", "error": err.Error(), "gc_deleted_blob_count": 0})
			}
		}
	}
}

func (m *Manager) SweepExpiredRestoreStates(ctx context.Context) error {
	if m == nil || m.stateStore == nil || m.restoreRegistry == nil {
		return nil
	}
	bindings, err := m.restoreRegistry.ListLatestRestoreBindings(ctx)
	if err != nil {
		return err
	}
	deleted := 0
	for _, binding := range bindings {
		state, loadErr := loadRestoreStateByRevision(ctx, m.stateStore, m.restoreRegistry, binding.AccountID, binding.SessionID, binding.Revision)
		if loadErr == nil {
			if !expiredRestoreState(*state, time.Now().UTC()) {
				continue
			}
			cleanupExpiredRestoreState(ctx, m.stateStore, m.restoreRegistry, binding.AccountID, binding.SessionID, binding.Revision)
			deleted++
			continue
		}
		if objectstore.IsNotFound(loadErr) {
			_ = m.restoreRegistry.ClearLatestRestoreRevision(ctx, binding.AccountID, binding.SessionID, binding.Revision)
			deleted++
			continue
		}
		return loadErr
	}
	if m.logger != nil {
		m.logger.Info("session_restore_gc", logging.LogFields{}, map[string]any{"flush_result": "succeeded", "gc_deleted_blob_count": deleted})
	}
	return nil
}
