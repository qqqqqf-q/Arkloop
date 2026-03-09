package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	FlushStateIdle    = "idle"
	FlushStatePending = "pending"
	FlushStateRunning = "running"
	FlushStateFailed  = "failed"
)

var ErrFlushConflict = errors.New("flush conflict")

type RegistryRecord struct {
	Ref                    string
	OrgID                  uuid.UUID
	OwnerUserID            *uuid.UUID
	ProjectID              *uuid.UUID
	LatestManifestRev      *string
	LeaseHolderID          *string
	LeaseUntil             *time.Time
	DefaultWorkspaceRef    *string
	DefaultShellSessionRef *string
	StoreKey               *string
	FlushState             string
	FlushRetryCount        int
	LastUsedAt             time.Time
	LastFlushFailedAt      *time.Time
	LastFlushSucceededAt   *time.Time
	MetadataJSON           map[string]any
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type ProfileRegistriesRepository struct{}

type WorkspaceRegistriesRepository struct{}

type RegistryLatestManifest struct {
	Ref               string
	LatestManifestRev string
}

func (ProfileRegistriesRepository) Get(ctx context.Context, pool *pgxpool.Pool, profileRef string) (RegistryRecord, error) {
	return getProfileRegistry(ctx, pool, profileRef)
}

func (WorkspaceRegistriesRepository) Get(ctx context.Context, pool *pgxpool.Pool, workspaceRef string) (RegistryRecord, error) {
	return getWorkspaceRegistry(ctx, pool, workspaceRef)
}

func (repo ProfileRegistriesRepository) GetOrCreate(ctx context.Context, pool *pgxpool.Pool, record RegistryRecord) (RegistryRecord, error) {
	if err := repo.UpsertTouch(ctx, pool, record); err != nil {
		return RegistryRecord{}, err
	}
	return repo.Get(ctx, pool, record.Ref)
}

func (repo WorkspaceRegistriesRepository) GetOrCreate(ctx context.Context, pool *pgxpool.Pool, record RegistryRecord) (RegistryRecord, error) {
	if err := repo.UpsertTouch(ctx, pool, record); err != nil {
		return RegistryRecord{}, err
	}
	return repo.Get(ctx, pool, record.Ref)
}

func (ProfileRegistriesRepository) UpsertTouch(ctx context.Context, pool *pgxpool.Pool, record RegistryRecord) error {
	return upsertProfileRegistry(ctx, pool, record)
}

func (WorkspaceRegistriesRepository) UpsertTouch(ctx context.Context, pool *pgxpool.Pool, record RegistryRecord) error {
	return upsertWorkspaceRegistry(ctx, pool, record)
}

func (ProfileRegistriesRepository) MarkFlushPending(ctx context.Context, pool *pgxpool.Pool, profileRef string) error {
	return markRegistryFlushPending(ctx, pool, "profile_registries", "profile_ref", profileRef)
}

func (WorkspaceRegistriesRepository) MarkFlushPending(ctx context.Context, pool *pgxpool.Pool, workspaceRef string) error {
	return markRegistryFlushPending(ctx, pool, "workspace_registries", "workspace_ref", workspaceRef)
}

func (ProfileRegistriesRepository) MarkFlushRunning(ctx context.Context, pool *pgxpool.Pool, profileRef string) error {
	return markRegistryFlushRunning(ctx, pool, "profile_registries", "profile_ref", profileRef)
}

func (WorkspaceRegistriesRepository) MarkFlushRunning(ctx context.Context, pool *pgxpool.Pool, workspaceRef string) error {
	return markRegistryFlushRunning(ctx, pool, "workspace_registries", "workspace_ref", workspaceRef)
}

func (ProfileRegistriesRepository) MarkFlushFailed(ctx context.Context, pool *pgxpool.Pool, profileRef string, failedAt time.Time) error {
	return markRegistryFlushFailed(ctx, pool, "profile_registries", "profile_ref", profileRef, failedAt)
}

func (WorkspaceRegistriesRepository) MarkFlushFailed(ctx context.Context, pool *pgxpool.Pool, workspaceRef string, failedAt time.Time) error {
	return markRegistryFlushFailed(ctx, pool, "workspace_registries", "workspace_ref", workspaceRef, failedAt)
}

func (ProfileRegistriesRepository) MarkFlushSucceeded(ctx context.Context, pool *pgxpool.Pool, profileRef string, revision string, succeededAt time.Time) error {
	return markRegistryFlushSucceeded(ctx, pool, "profile_registries", "profile_ref", profileRef, revision, succeededAt)
}

func (WorkspaceRegistriesRepository) MarkFlushSucceeded(ctx context.Context, pool *pgxpool.Pool, workspaceRef string, revision string, succeededAt time.Time) error {
	return markRegistryFlushSucceeded(ctx, pool, "workspace_registries", "workspace_ref", workspaceRef, revision, succeededAt)
}

func (ProfileRegistriesRepository) AcquireFlushLease(ctx context.Context, pool *pgxpool.Pool, profileRef string, holderID string, expectedBaseRevision string, leaseUntil time.Time) error {
	return acquireRegistryFlushLease(ctx, pool, "profile_registries", "profile_ref", profileRef, holderID, expectedBaseRevision, leaseUntil)
}

func (WorkspaceRegistriesRepository) AcquireFlushLease(ctx context.Context, pool *pgxpool.Pool, workspaceRef string, holderID string, expectedBaseRevision string, leaseUntil time.Time) error {
	return acquireRegistryFlushLease(ctx, pool, "workspace_registries", "workspace_ref", workspaceRef, holderID, expectedBaseRevision, leaseUntil)
}

func (ProfileRegistriesRepository) ReleaseFlushFailure(ctx context.Context, pool *pgxpool.Pool, profileRef string, holderID string, failedAt time.Time) error {
	return releaseRegistryFlushFailure(ctx, pool, "profile_registries", "profile_ref", profileRef, holderID, failedAt)
}

func (WorkspaceRegistriesRepository) ReleaseFlushFailure(ctx context.Context, pool *pgxpool.Pool, workspaceRef string, holderID string, failedAt time.Time) error {
	return releaseRegistryFlushFailure(ctx, pool, "workspace_registries", "workspace_ref", workspaceRef, holderID, failedAt)
}

func (ProfileRegistriesRepository) CommitFlushSuccess(ctx context.Context, pool *pgxpool.Pool, profileRef string, holderID string, expectedBaseRevision string, revision string, succeededAt time.Time) error {
	return commitRegistryFlushSuccess(ctx, pool, "profile_registries", "profile_ref", profileRef, holderID, expectedBaseRevision, revision, succeededAt)
}

func (WorkspaceRegistriesRepository) CommitFlushSuccess(ctx context.Context, pool *pgxpool.Pool, workspaceRef string, holderID string, expectedBaseRevision string, revision string, succeededAt time.Time) error {
	return commitRegistryFlushSuccess(ctx, pool, "workspace_registries", "workspace_ref", workspaceRef, holderID, expectedBaseRevision, revision, succeededAt)
}

func (ProfileRegistriesRepository) ListLatestManifestRevisions(ctx context.Context, pool *pgxpool.Pool) ([]RegistryLatestManifest, error) {
	return listLatestManifestRevisions(ctx, pool, "profile_registries", "profile_ref")
}

func (WorkspaceRegistriesRepository) ListLatestManifestRevisions(ctx context.Context, pool *pgxpool.Pool) ([]RegistryLatestManifest, error) {
	return listLatestManifestRevisions(ctx, pool, "workspace_registries", "workspace_ref")
}

func getProfileRegistry(ctx context.Context, pool *pgxpool.Pool, profileRef string) (RegistryRecord, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return RegistryRecord{}, fmt.Errorf("pool must not be nil")
	}
	profileRef = strings.TrimSpace(profileRef)
	if profileRef == "" {
		return RegistryRecord{}, fmt.Errorf("registry ref must not be empty")
	}

	var record RegistryRecord
	var metadataRaw []byte
	err := pool.QueryRow(
		ctx,
		`SELECT profile_ref,
		        org_id,
		        owner_user_id,
		        latest_manifest_rev,
		        lease_holder_id,
		        lease_until,
		        default_workspace_ref,
		        store_key,
		        flush_state,
		        flush_retry_count,
		        last_used_at,
		        last_flush_failed_at,
		        last_flush_succeeded_at,
		        metadata_json,
		        created_at,
		        updated_at
		   FROM profile_registries
		  WHERE profile_ref = $1`,
		profileRef,
	).Scan(
		&record.Ref,
		&record.OrgID,
		&record.OwnerUserID,
		&record.LatestManifestRev,
		&record.LeaseHolderID,
		&record.LeaseUntil,
		&record.DefaultWorkspaceRef,
		&record.StoreKey,
		&record.FlushState,
		&record.FlushRetryCount,
		&record.LastUsedAt,
		&record.LastFlushFailedAt,
		&record.LastFlushSucceededAt,
		&metadataRaw,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return RegistryRecord{}, err
	}
	return decodeRegistryRecord(record, metadataRaw), nil
}

func getWorkspaceRegistry(ctx context.Context, pool *pgxpool.Pool, workspaceRef string) (RegistryRecord, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return RegistryRecord{}, fmt.Errorf("pool must not be nil")
	}
	workspaceRef = strings.TrimSpace(workspaceRef)
	if workspaceRef == "" {
		return RegistryRecord{}, fmt.Errorf("registry ref must not be empty")
	}

	var record RegistryRecord
	var metadataRaw []byte
	err := pool.QueryRow(
		ctx,
		`SELECT workspace_ref,
		        org_id,
		        owner_user_id,
		        project_id,
		        latest_manifest_rev,
		        lease_holder_id,
		        lease_until,
		        default_shell_session_ref,
		        store_key,
		        flush_state,
		        flush_retry_count,
		        last_used_at,
		        last_flush_failed_at,
		        last_flush_succeeded_at,
		        metadata_json,
		        created_at,
		        updated_at
		   FROM workspace_registries
		  WHERE workspace_ref = $1`,
		workspaceRef,
	).Scan(
		&record.Ref,
		&record.OrgID,
		&record.OwnerUserID,
		&record.ProjectID,
		&record.LatestManifestRev,
		&record.LeaseHolderID,
		&record.LeaseUntil,
		&record.DefaultShellSessionRef,
		&record.StoreKey,
		&record.FlushState,
		&record.FlushRetryCount,
		&record.LastUsedAt,
		&record.LastFlushFailedAt,
		&record.LastFlushSucceededAt,
		&metadataRaw,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return RegistryRecord{}, err
	}
	return decodeRegistryRecord(record, metadataRaw), nil
}

func upsertProfileRegistry(ctx context.Context, pool *pgxpool.Pool, record RegistryRecord) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return fmt.Errorf("pool must not be nil")
	}
	normalized, metadataRaw, err := normalizeRegistryRecord(record)
	if err != nil {
		return err
	}
	_, err = pool.Exec(
		ctx,
		`INSERT INTO profile_registries (
			profile_ref,
			org_id,
			owner_user_id,
			latest_manifest_rev,
			default_workspace_ref,
			store_key,
			flush_state,
			flush_retry_count,
			last_used_at,
			metadata_json
		) VALUES ($1, $2, $3, $4, $5, $6, $7, 0, $8, $9::jsonb)
		ON CONFLICT (profile_ref) DO UPDATE SET
			owner_user_id = COALESCE(EXCLUDED.owner_user_id, profile_registries.owner_user_id),
			default_workspace_ref = COALESCE(EXCLUDED.default_workspace_ref, profile_registries.default_workspace_ref),
			store_key = COALESCE(EXCLUDED.store_key, profile_registries.store_key),
			last_used_at = EXCLUDED.last_used_at,
			metadata_json = EXCLUDED.metadata_json,
			updated_at = now()`,
		normalized.Ref,
		normalized.OrgID,
		normalized.OwnerUserID,
		normalized.LatestManifestRev,
		normalized.DefaultWorkspaceRef,
		normalized.StoreKey,
		normalized.FlushState,
		normalized.LastUsedAt,
		string(metadataRaw),
	)
	return err
}

func upsertWorkspaceRegistry(ctx context.Context, pool *pgxpool.Pool, record RegistryRecord) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return fmt.Errorf("pool must not be nil")
	}
	normalized, metadataRaw, err := normalizeRegistryRecord(record)
	if err != nil {
		return err
	}
	_, err = pool.Exec(
		ctx,
		`INSERT INTO workspace_registries (
			workspace_ref,
			org_id,
			owner_user_id,
			project_id,
			latest_manifest_rev,
			default_shell_session_ref,
			store_key,
			flush_state,
			flush_retry_count,
			last_used_at,
			metadata_json
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 0, $9, $10::jsonb)
		ON CONFLICT (workspace_ref) DO UPDATE SET
			owner_user_id = COALESCE(EXCLUDED.owner_user_id, workspace_registries.owner_user_id),
			project_id = COALESCE(EXCLUDED.project_id, workspace_registries.project_id),
			default_shell_session_ref = COALESCE(EXCLUDED.default_shell_session_ref, workspace_registries.default_shell_session_ref),
			store_key = COALESCE(EXCLUDED.store_key, workspace_registries.store_key),
			last_used_at = EXCLUDED.last_used_at,
			metadata_json = EXCLUDED.metadata_json,
			updated_at = now()`,
		normalized.Ref,
		normalized.OrgID,
		normalized.OwnerUserID,
		normalized.ProjectID,
		normalized.LatestManifestRev,
		normalized.DefaultShellSessionRef,
		normalized.StoreKey,
		normalized.FlushState,
		normalized.LastUsedAt,
		string(metadataRaw),
	)
	return err
}

func normalizeRegistryRecord(record RegistryRecord) (RegistryRecord, []byte, error) {
	if record.OrgID == uuid.Nil {
		return RegistryRecord{}, nil, fmt.Errorf("org_id must not be empty")
	}
	record.Ref = strings.TrimSpace(record.Ref)
	if record.Ref == "" {
		return RegistryRecord{}, nil, fmt.Errorf("registry ref must not be empty")
	}
	record.FlushState = normalizeFlushState(record.FlushState)
	record.DefaultWorkspaceRef = normalizeOptionalString(record.DefaultWorkspaceRef)
	record.DefaultShellSessionRef = normalizeOptionalString(record.DefaultShellSessionRef)
	record.StoreKey = normalizeOptionalString(record.StoreKey)
	if record.MetadataJSON == nil {
		record.MetadataJSON = map[string]any{}
	}
	if record.LastUsedAt.IsZero() {
		record.LastUsedAt = time.Now().UTC()
	} else {
		record.LastUsedAt = record.LastUsedAt.UTC()
	}
	metadataRaw, err := json.Marshal(record.MetadataJSON)
	if err != nil {
		return RegistryRecord{}, nil, fmt.Errorf("marshal metadata_json: %w", err)
	}
	return record, metadataRaw, nil
}

func decodeRegistryRecord(record RegistryRecord, metadataRaw []byte) RegistryRecord {
	if len(metadataRaw) > 0 {
		_ = json.Unmarshal(metadataRaw, &record.MetadataJSON)
	}
	if record.MetadataJSON == nil {
		record.MetadataJSON = map[string]any{}
	}
	record.FlushState = normalizeFlushState(record.FlushState)
	return record
}

func markRegistryFlushPending(ctx context.Context, pool *pgxpool.Pool, table string, keyColumn string, ref string) error {
	return updateRegistryState(ctx, pool, table, keyColumn, ref, `
		flush_state = 'pending',
		updated_at = now()`)
}

func markRegistryFlushRunning(ctx context.Context, pool *pgxpool.Pool, table string, keyColumn string, ref string) error {
	return updateRegistryState(ctx, pool, table, keyColumn, ref, `
		flush_state = 'running',
		updated_at = now()`)
}

func markRegistryFlushFailed(ctx context.Context, pool *pgxpool.Pool, table string, keyColumn string, ref string, failedAt time.Time) error {
	if failedAt.IsZero() {
		failedAt = time.Now().UTC()
	}
	return updateRegistryState(ctx, pool, table, keyColumn, ref, `
		flush_state = 'failed',
		flush_retry_count = flush_retry_count + 1,
		last_flush_failed_at = $2,
		updated_at = now()`, failedAt.UTC())
}

func markRegistryFlushSucceeded(ctx context.Context, pool *pgxpool.Pool, table string, keyColumn string, ref string, revision string, succeededAt time.Time) error {
	if succeededAt.IsZero() {
		succeededAt = time.Now().UTC()
	}
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return fmt.Errorf("latest manifest revision must not be empty")
	}
	return updateRegistryState(ctx, pool, table, keyColumn, ref, `
		latest_manifest_rev = $2,
		lease_holder_id = NULL,
		lease_until = NULL,
		flush_state = 'idle',
		flush_retry_count = 0,
		last_flush_succeeded_at = $3,
		updated_at = now()`, revision, succeededAt.UTC())
}

func acquireRegistryFlushLease(ctx context.Context, pool *pgxpool.Pool, table string, keyColumn string, ref string, holderID string, expectedBaseRevision string, leaseUntil time.Time) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return fmt.Errorf("pool must not be nil")
	}
	ref = strings.TrimSpace(ref)
	holderID = strings.TrimSpace(holderID)
	expectedBaseRevision = strings.TrimSpace(expectedBaseRevision)
	if ref == "" {
		return fmt.Errorf("registry ref must not be empty")
	}
	if holderID == "" {
		return fmt.Errorf("lease holder_id must not be empty")
	}
	if leaseUntil.IsZero() {
		return fmt.Errorf("lease_until must not be zero")
	}
	commandTag, err := pool.Exec(ctx,
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
	return detectRegistryFlushConflict(ctx, pool, table, keyColumn, ref, holderID, expectedBaseRevision)
}

func releaseRegistryFlushFailure(ctx context.Context, pool *pgxpool.Pool, table string, keyColumn string, ref string, holderID string, failedAt time.Time) error {
	if failedAt.IsZero() {
		failedAt = time.Now().UTC()
	}
	return updateRegistryState(ctx, pool, table, keyColumn, ref, `
		lease_holder_id = CASE WHEN lease_holder_id = $2 THEN NULL ELSE lease_holder_id END,
		lease_until = CASE WHEN lease_holder_id = $2 THEN NULL ELSE lease_until END,
		flush_state = 'failed',
		flush_retry_count = flush_retry_count + 1,
		last_flush_failed_at = $3,
		updated_at = now()`, strings.TrimSpace(holderID), failedAt.UTC())
}

func commitRegistryFlushSuccess(ctx context.Context, pool *pgxpool.Pool, table string, keyColumn string, ref string, holderID string, expectedBaseRevision string, revision string, succeededAt time.Time) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return fmt.Errorf("pool must not be nil")
	}
	ref = strings.TrimSpace(ref)
	holderID = strings.TrimSpace(holderID)
	expectedBaseRevision = strings.TrimSpace(expectedBaseRevision)
	revision = strings.TrimSpace(revision)
	if ref == "" {
		return fmt.Errorf("registry ref must not be empty")
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
	commandTag, err := pool.Exec(ctx,
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
	return detectRegistryFlushConflict(ctx, pool, table, keyColumn, ref, holderID, expectedBaseRevision)
}

func listLatestManifestRevisions(ctx context.Context, pool *pgxpool.Pool, table string, keyColumn string) ([]RegistryLatestManifest, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}
	rows, err := pool.Query(ctx,
		fmt.Sprintf(`SELECT %s, latest_manifest_rev
		   FROM %s
		  WHERE latest_manifest_rev IS NOT NULL
		    AND TRIM(latest_manifest_rev) <> ''`, keyColumn, table),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]RegistryLatestManifest, 0)
	for rows.Next() {
		var item RegistryLatestManifest
		if err := rows.Scan(&item.Ref, &item.LatestManifestRev); err != nil {
			return nil, err
		}
		item.Ref = strings.TrimSpace(item.Ref)
		item.LatestManifestRev = strings.TrimSpace(item.LatestManifestRev)
		if item.Ref == "" || item.LatestManifestRev == "" {
			continue
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func detectRegistryFlushConflict(ctx context.Context, pool *pgxpool.Pool, table string, keyColumn string, ref string, holderID string, expectedBaseRevision string) error {
	var currentRef string
	var latestManifestRev *string
	var leaseHolderID *string
	var leaseUntil *time.Time
	err := pool.QueryRow(ctx,
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

func updateRegistryState(ctx context.Context, pool *pgxpool.Pool, table string, keyColumn string, ref string, setClause string, args ...any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return fmt.Errorf("pool must not be nil")
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("registry ref must not be empty")
	}
	queryArgs := make([]any, 0, len(args)+1)
	queryArgs = append(queryArgs, ref)
	queryArgs = append(queryArgs, args...)
	commandTag, err := pool.Exec(
		ctx,
		fmt.Sprintf(`UPDATE %s
		    SET %s
		  WHERE %s = $1`, table, strings.TrimSpace(setClause), keyColumn),
		queryArgs...,
	)
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func normalizeFlushState(value string) string {
	switch strings.TrimSpace(value) {
	case FlushStatePending, FlushStateRunning, FlushStateFailed:
		return strings.TrimSpace(value)
	default:
		return FlushStateIdle
	}
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	cleaned := strings.TrimSpace(*value)
	if cleaned == "" {
		return nil
	}
	copied := cleaned
	return &copied
}

func IsRegistryNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
