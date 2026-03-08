package environment

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RegistryWriter interface {
	EnsureProfileRegistry(ctx context.Context, orgID, profileRef string) error
	EnsureWorkspaceRegistry(ctx context.Context, orgID, workspaceRef string) error
	GetLatestManifestRevision(ctx context.Context, scope, ref string) (string, error)
	MarkFlushPending(ctx context.Context, scope, ref string) error
	MarkFlushRunning(ctx context.Context, scope, ref string) error
	MarkFlushFailed(ctx context.Context, scope, ref string, failedAt time.Time) error
	MarkFlushSucceeded(ctx context.Context, scope, ref, revision string, succeededAt time.Time) error
}

type noopRegistryWriter struct{}

type PGRegistryWriter struct {
	pool *pgxpool.Pool
}

func NewNoopRegistryWriter() RegistryWriter {
	return noopRegistryWriter{}
}

func NewPGRegistryWriter(pool *pgxpool.Pool) RegistryWriter {
	if pool == nil {
		return NewNoopRegistryWriter()
	}
	return &PGRegistryWriter{pool: pool}
}

func (noopRegistryWriter) EnsureProfileRegistry(context.Context, string, string) error {
	return nil
}

func (noopRegistryWriter) EnsureWorkspaceRegistry(context.Context, string, string) error {
	return nil
}

func (noopRegistryWriter) GetLatestManifestRevision(context.Context, string, string) (string, error) {
	return "", nil
}

func (noopRegistryWriter) MarkFlushPending(context.Context, string, string) error {
	return nil
}

func (noopRegistryWriter) MarkFlushRunning(context.Context, string, string) error {
	return nil
}

func (noopRegistryWriter) MarkFlushFailed(context.Context, string, string, time.Time) error {
	return nil
}

func (noopRegistryWriter) MarkFlushSucceeded(context.Context, string, string, string, time.Time) error {
	return nil
}

func (w *PGRegistryWriter) EnsureProfileRegistry(ctx context.Context, orgID, profileRef string) error {
	return w.ensureRegistry(ctx, "profile_registries", "profile_ref", orgID, profileRef)
}

func (w *PGRegistryWriter) EnsureWorkspaceRegistry(ctx context.Context, orgID, workspaceRef string) error {
	return w.ensureRegistry(ctx, "workspace_registries", "workspace_ref", orgID, workspaceRef)
}

func (w *PGRegistryWriter) GetLatestManifestRevision(ctx context.Context, scope, ref string) (string, error) {
	table, keyColumn, err := registryTable(scope)
	if err != nil {
		return "", err
	}
	var revision *string
	if err := w.pool.QueryRow(ctx, fmt.Sprintf(`SELECT latest_manifest_rev FROM %s WHERE %s = $1`, table, keyColumn), strings.TrimSpace(ref)).Scan(&revision); err != nil {
		return "", err
	}
	if revision == nil {
		return "", nil
	}
	return strings.TrimSpace(*revision), nil
}

func (w *PGRegistryWriter) MarkFlushPending(ctx context.Context, scope, ref string) error {
	return w.updateRegistry(ctx, scope, ref, `flush_state = 'pending', updated_at = now()`)
}

func (w *PGRegistryWriter) MarkFlushRunning(ctx context.Context, scope, ref string) error {
	return w.updateRegistry(ctx, scope, ref, `flush_state = 'running', updated_at = now()`)
}

func (w *PGRegistryWriter) MarkFlushFailed(ctx context.Context, scope, ref string, failedAt time.Time) error {
	if failedAt.IsZero() {
		failedAt = time.Now().UTC()
	}
	return w.updateRegistry(ctx, scope, ref, `flush_state = 'failed', flush_retry_count = flush_retry_count + 1, last_flush_failed_at = $2, updated_at = now()`, failedAt.UTC())
}

func (w *PGRegistryWriter) MarkFlushSucceeded(ctx context.Context, scope, ref, revision string, succeededAt time.Time) error {
	if succeededAt.IsZero() {
		succeededAt = time.Now().UTC()
	}
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return fmt.Errorf("latest manifest revision must not be empty")
	}
	return w.updateRegistry(ctx, scope, ref, `latest_manifest_rev = $2, flush_state = 'idle', flush_retry_count = 0, last_flush_succeeded_at = $3, updated_at = now()`, revision, succeededAt.UTC())
}

func (w *PGRegistryWriter) ensureRegistry(ctx context.Context, table, keyColumn, orgID, ref string) error {
	if w == nil || w.pool == nil {
		return nil
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	parsedOrgID, err := uuid.Parse(strings.TrimSpace(orgID))
	if err != nil {
		return nil
	}
	_, err = w.pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (%s, org_id, flush_state, flush_retry_count, metadata_json) VALUES ($1, $2, 'idle', 0, '{}'::jsonb) ON CONFLICT (%s) DO NOTHING`, table, keyColumn, keyColumn), ref, parsedOrgID)
	return err
}

func (w *PGRegistryWriter) updateRegistry(ctx context.Context, scope, ref, setClause string, args ...any) error {
	if w == nil || w.pool == nil {
		return nil
	}
	table, keyColumn, err := registryTable(scope)
	if err != nil {
		return err
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	queryArgs := make([]any, 0, len(args)+1)
	queryArgs = append(queryArgs, ref)
	queryArgs = append(queryArgs, args...)
	_, err = w.pool.Exec(ctx, fmt.Sprintf(`UPDATE %s SET %s WHERE %s = $1`, table, strings.TrimSpace(setClause), keyColumn), queryArgs...)
	return err
}

func registryTable(scope string) (string, string, error) {
	switch strings.TrimSpace(scope) {
	case ScopeProfile:
		return "profile_registries", "profile_ref", nil
	case ScopeWorkspace:
		return "workspace_registries", "workspace_ref", nil
	default:
		return "", "", fmt.Errorf("unsupported scope: %s", scope)
	}
}
