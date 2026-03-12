package environment

import (
	"context"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/registryerr"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrFlushConflict = registryerr.ErrFlushConflict

type RegistryManifestBinding struct {
	Ref      string
	Revision string
}

type RegistryWriter interface {
	EnsureProfileRegistry(ctx context.Context, accountID, profileRef string) error
	EnsureBrowserStateRegistry(ctx context.Context, accountID, workspaceRef string) error
	EnsureWorkspaceRegistry(ctx context.Context, accountID, workspaceRef string) error
	GetLatestManifestRevision(ctx context.Context, scope, ref string) (string, error)
	MarkFlushPending(ctx context.Context, scope, ref string) error
	AcquireFlushLease(ctx context.Context, scope, ref, holderID, expectedBaseRevision string, leaseUntil time.Time) error
	CommitFlushSuccess(ctx context.Context, scope, ref, holderID, expectedBaseRevision, revision string, succeededAt time.Time) error
	ReleaseFlushFailure(ctx context.Context, scope, ref, holderID string, failedAt time.Time) error
	ListLatestManifestRevisions(ctx context.Context, scope string) ([]RegistryManifestBinding, error)
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

func (noopRegistryWriter) EnsureBrowserStateRegistry(context.Context, string, string) error {
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

func (noopRegistryWriter) AcquireFlushLease(context.Context, string, string, string, string, time.Time) error {
	return nil
}

func (noopRegistryWriter) CommitFlushSuccess(context.Context, string, string, string, string, string, time.Time) error {
	return nil
}

func (noopRegistryWriter) ReleaseFlushFailure(context.Context, string, string, string, time.Time) error {
	return nil
}

func (noopRegistryWriter) ListLatestManifestRevisions(context.Context, string) ([]RegistryManifestBinding, error) {
	return nil, nil
}

func (w *PGRegistryWriter) EnsureProfileRegistry(ctx context.Context, accountID, profileRef string) error {
	return w.ensureRegistry(ctx, "profile_registries", "profile_ref", accountID, profileRef)
}

func (w *PGRegistryWriter) EnsureBrowserStateRegistry(ctx context.Context, accountID, workspaceRef string) error {
	return w.ensureRegistry(ctx, "browser_state_registries", "workspace_ref", accountID, workspaceRef)
}

func (w *PGRegistryWriter) EnsureWorkspaceRegistry(ctx context.Context, accountID, workspaceRef string) error {
	return w.ensureRegistry(ctx, "workspace_registries", "workspace_ref", accountID, workspaceRef)
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

func (w *PGRegistryWriter) AcquireFlushLease(ctx context.Context, scope, ref, holderID, expectedBaseRevision string, leaseUntil time.Time) error {
	if w == nil || w.pool == nil {
		return nil
	}
	table, keyColumn, err := registryTable(scope)
	if err != nil {
		return err
	}
	ref = strings.TrimSpace(ref)
	holderID = strings.TrimSpace(holderID)
	expectedBaseRevision = strings.TrimSpace(expectedBaseRevision)
	if ref == "" {
		return nil
	}
	if holderID == "" {
		return fmt.Errorf("lease holder_id must not be empty")
	}
	if leaseUntil.IsZero() {
		return fmt.Errorf("lease_until must not be zero")
	}
	commandTag, err := w.pool.Exec(ctx,
		fmt.Sprintf(`UPDATE %s
		    SET lease_holder_id = $2,
		        lease_until = $3,
		        flush_state = 'running',
		        updated_at = now()
		  WHERE %s = $1
		    AND COALESCE(latest_manifest_rev, '') = $4
		    AND (
		        lease_holder_id IS NULL
		        OR lease_until IS NULL
		        OR lease_until <= now()
		        OR lease_holder_id = $2
		    )`, table, keyColumn),
		ref,
		holderID,
		leaseUntil.UTC(),
		expectedBaseRevision,
	)
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() > 0 {
		return nil
	}
	return w.detectFlushConflict(ctx, table, keyColumn, ref, holderID, expectedBaseRevision)
}

func (w *PGRegistryWriter) CommitFlushSuccess(ctx context.Context, scope, ref, holderID, expectedBaseRevision, revision string, succeededAt time.Time) error {
	if w == nil || w.pool == nil {
		return nil
	}
	table, keyColumn, err := registryTable(scope)
	if err != nil {
		return err
	}
	ref = strings.TrimSpace(ref)
	holderID = strings.TrimSpace(holderID)
	expectedBaseRevision = strings.TrimSpace(expectedBaseRevision)
	revision = strings.TrimSpace(revision)
	if ref == "" {
		return nil
	}
	if holderID == "" {
		return fmt.Errorf("lease holder_id must not be empty")
	}
	if revision == "" {
		return fmt.Errorf("latest manifest revision must not be empty")
	}
	if succeededAt.IsZero() {
		succeededAt = time.Now().UTC()
	}
	commandTag, err := w.pool.Exec(ctx,
		fmt.Sprintf(`UPDATE %s
		    SET latest_manifest_rev = $3,
		        lease_holder_id = NULL,
		        lease_until = NULL,
		        flush_state = 'idle',
		        flush_retry_count = 0,
		        last_flush_succeeded_at = $4,
		        updated_at = now()
		  WHERE %s = $1
		    AND lease_holder_id = $2
		    AND COALESCE(latest_manifest_rev, '') = $5`, table, keyColumn),
		ref,
		holderID,
		revision,
		succeededAt.UTC(),
		expectedBaseRevision,
	)
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() > 0 {
		return nil
	}
	return w.detectFlushConflict(ctx, table, keyColumn, ref, holderID, expectedBaseRevision)
}

func (w *PGRegistryWriter) ReleaseFlushFailure(ctx context.Context, scope, ref, holderID string, failedAt time.Time) error {
	if failedAt.IsZero() {
		failedAt = time.Now().UTC()
	}
	return w.updateRegistry(ctx, scope, ref, `
		lease_holder_id = CASE WHEN lease_holder_id = $2 THEN NULL ELSE lease_holder_id END,
		lease_until = CASE WHEN lease_holder_id = $2 THEN NULL ELSE lease_until END,
		flush_state = 'failed',
		flush_retry_count = flush_retry_count + 1,
		last_flush_failed_at = $3,
		updated_at = now()`, strings.TrimSpace(holderID), failedAt.UTC())
}

func (w *PGRegistryWriter) ListLatestManifestRevisions(ctx context.Context, scope string) ([]RegistryManifestBinding, error) {
	if w == nil || w.pool == nil {
		return nil, nil
	}
	table, keyColumn, err := registryTable(scope)
	if err != nil {
		return nil, err
	}
	rows, err := w.pool.Query(ctx,
		fmt.Sprintf(`SELECT %s, latest_manifest_rev
		   FROM %s
		  WHERE latest_manifest_rev IS NOT NULL
		    AND TRIM(latest_manifest_rev) <> ''`, keyColumn, table),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]RegistryManifestBinding, 0)
	for rows.Next() {
		var item RegistryManifestBinding
		if err := rows.Scan(&item.Ref, &item.Revision); err != nil {
			return nil, err
		}
		item.Ref = strings.TrimSpace(item.Ref)
		item.Revision = strings.TrimSpace(item.Revision)
		if item.Ref == "" || item.Revision == "" {
			continue
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (w *PGRegistryWriter) ensureRegistry(ctx context.Context, table, keyColumn, accountID, ref string) error {
	if w == nil || w.pool == nil {
		return nil
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	parsedAccountID, err := uuid.Parse(strings.TrimSpace(accountID))
	if err != nil {
		return nil
	}
	_, err = w.pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (%s, account_id, flush_state, flush_retry_count, metadata_json) VALUES ($1, $2, 'idle', 0, '{}'::jsonb) ON CONFLICT (%s) DO NOTHING`, table, keyColumn, keyColumn), ref, parsedAccountID)
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
	commandTag, err := w.pool.Exec(ctx, fmt.Sprintf(`UPDATE %s SET %s WHERE %s = $1`, table, strings.TrimSpace(setClause), keyColumn), queryArgs...)
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (w *PGRegistryWriter) detectFlushConflict(ctx context.Context, table, keyColumn, ref, holderID, expectedBaseRevision string) error {
	var currentRef string
	var latestManifestRev *string
	var leaseHolderID *string
	var leaseUntil *time.Time
	err := w.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s, latest_manifest_rev, lease_holder_id, lease_until
		   FROM %s
		  WHERE %s = $1`, keyColumn, table, keyColumn),
		ref,
	).Scan(&currentRef, &latestManifestRev, &leaseHolderID, &leaseUntil)
	if err != nil {
		return err
	}
	if strings.TrimSpace(currentRef) == "" {
		return pgx.ErrNoRows
	}
	currentRevision := ""
	if latestManifestRev != nil {
		currentRevision = strings.TrimSpace(*latestManifestRev)
	}
	if currentRevision != strings.TrimSpace(expectedBaseRevision) {
		return ErrFlushConflict
	}
	if leaseHolderID != nil && strings.TrimSpace(*leaseHolderID) != "" && strings.TrimSpace(*leaseHolderID) != holderID {
		if leaseUntil == nil || leaseUntil.After(time.Now().UTC()) {
			return ErrFlushConflict
		}
	}
	return ErrFlushConflict
}

func registryTable(scope string) (string, string, error) {
	switch strings.TrimSpace(scope) {
	case ScopeProfile:
		return "profile_registries", "profile_ref", nil
	case ScopeBrowserState:
		return "browser_state_registries", "workspace_ref", nil
	case ScopeWorkspace:
		return "workspace_registries", "workspace_ref", nil
	default:
		return "", "", fmt.Errorf("unsupported scope: %s", scope)
	}
}
