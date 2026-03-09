package environment

import (
	"context"
	"fmt"
	"strings"
	"time"

	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/shared/objectstore"
)

func (m *Manager) StartGovernance(ctx context.Context) {
	if m == nil || m.store == nil || m.registry == nil {
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
			sweepCtx, cancel := context.WithTimeout(ctx, flushTimeout)
			err := m.SweepUnreferencedBlobs(sweepCtx)
			cancel()
			if err != nil && m.logger != nil {
				m.logger.Warn("environment_gc", logging.LogFields{}, map[string]any{"flush_result": "failed", "error": err.Error(), "gc_deleted_blob_count": 0})
			}
		}
	}
}

func (m *Manager) SweepUnreferencedBlobs(ctx context.Context) error {
	if m == nil || m.store == nil || m.registry == nil {
		return nil
	}
	totalDeleted := 0
	for _, scope := range []string{ScopeProfile, ScopeWorkspace} {
		bindings, err := m.registry.ListLatestManifestRevisions(ctx, scope)
		if err != nil {
			return err
		}
		for _, binding := range bindings {
			deleted, sweepErr := m.sweepScopeBlobs(ctx, scope, binding)
			if sweepErr != nil {
				return sweepErr
			}
			totalDeleted += deleted
		}
	}
	if m.logger != nil {
		m.logger.Info("environment_gc", logging.LogFields{}, map[string]any{"flush_result": "succeeded", "gc_deleted_blob_count": totalDeleted})
	}
	return nil
}

func (m *Manager) sweepScopeBlobs(ctx context.Context, scope string, binding RegistryManifestBinding) (int, error) {
	binding.Ref = strings.TrimSpace(binding.Ref)
	binding.Revision = strings.TrimSpace(binding.Revision)
	if binding.Ref == "" || binding.Revision == "" {
		return 0, nil
	}
	manifest, err := loadManifest(ctx, m.store, scope, binding.Ref, binding.Revision)
	if err != nil {
		if objectstore.IsNotFound(err) {
			if m.logger != nil {
				m.logger.Warn("environment_gc", logging.LogFields{}, map[string]any{"flush_scope": scope, "flush_ref": binding.Ref, "flush_result": "manifest_missing", "gc_deleted_blob_count": 0})
			}
			return 0, nil
		}
		return 0, fmt.Errorf("load manifest for gc: %w", err)
	}
	live := make(map[string]struct{})
	for _, entry := range manifest.Entries {
		if entry.Type != EntryTypeFile || entry.Deleted || strings.TrimSpace(entry.SHA256) == "" {
			continue
		}
		live[blobKey(scope, binding.Ref, entry.SHA256)] = struct{}{}
	}
	objects, err := m.store.ListPrefix(ctx, blobPrefix(scope, binding.Ref))
	if err != nil {
		return 0, fmt.Errorf("list blobs for gc: %w", err)
	}
	deleted := 0
	for _, object := range objects {
		if _, ok := live[object.Key]; ok {
			continue
		}
		if err := m.store.Delete(ctx, object.Key); err != nil && !objectstore.IsNotFound(err) {
			return deleted, fmt.Errorf("delete blob %s: %w", object.Key, err)
		}
		deleted++
	}
	return deleted, nil
}
