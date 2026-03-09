package data

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	ShellSessionStateReady  = "ready"
	ShellSessionStateBusy   = "busy"
	ShellSessionStateClosed = "closed"

	ShellShareScopeRun       = "run"
	ShellShareScopeThread    = "thread"
	ShellShareScopeWorkspace = "workspace"
	ShellShareScopeOrg       = "org"
)

type ShellSessionRecord struct {
	SessionRef        string
	OrgID             uuid.UUID
	ProfileRef        string
	WorkspaceRef      string
	ProjectID         *uuid.UUID
	ThreadID          *uuid.UUID
	RunID             *uuid.UUID
	ShareScope        string
	State             string
	LiveSessionID     *string
	LatestRestoreRev  *string
	DefaultBindingKey *string
	LastUsedAt        time.Time
	MetadataJSON      map[string]any
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ShellSessionsRepository struct{}

func (ShellSessionsRepository) GetBySessionRef(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID uuid.UUID,
	sessionRef string,
) (ShellSessionRecord, error) {
	return getShellSession(ctx, pool, orgID, sessionRef)
}

func (ShellSessionsRepository) GetLatestByRun(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID uuid.UUID,
	runID uuid.UUID,
) (ShellSessionRecord, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return ShellSessionRecord{}, fmt.Errorf("pool must not be nil")
	}
	if orgID == uuid.Nil {
		return ShellSessionRecord{}, fmt.Errorf("org_id must not be empty")
	}
	if runID == uuid.Nil {
		return ShellSessionRecord{}, fmt.Errorf("run_id must not be empty")
	}
	return scanShellSession(pool.QueryRow(
		ctx,
		`SELECT session_ref,
		        org_id,
		        profile_ref,
		        workspace_ref,
		        project_id,
		        thread_id,
		        run_id,
		        share_scope,
		        state,
		        live_session_id,
		        latest_restore_rev,
		        default_binding_key,
		        last_used_at,
		        metadata_json,
		        created_at,
		        updated_at
		   FROM shell_sessions
		  WHERE org_id = $1
		    AND run_id = $2
		    AND state <> $3
		  ORDER BY last_used_at DESC, updated_at DESC
		  LIMIT 1`,
		orgID,
		runID,
		ShellSessionStateClosed,
	))
}

func (ShellSessionsRepository) GetByDefaultBindingKey(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID uuid.UUID,
	profileRef string,
	defaultBindingKey string,
) (ShellSessionRecord, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return ShellSessionRecord{}, fmt.Errorf("pool must not be nil")
	}
	if orgID == uuid.Nil {
		return ShellSessionRecord{}, fmt.Errorf("org_id must not be empty")
	}
	profileRef = strings.TrimSpace(profileRef)
	defaultBindingKey = strings.TrimSpace(defaultBindingKey)
	if profileRef == "" {
		return ShellSessionRecord{}, fmt.Errorf("profile_ref must not be empty")
	}
	if defaultBindingKey == "" {
		return ShellSessionRecord{}, fmt.Errorf("default_binding_key must not be empty")
	}
	return scanShellSession(pool.QueryRow(
		ctx,
		`SELECT session_ref,
		        org_id,
		        profile_ref,
		        workspace_ref,
		        project_id,
		        thread_id,
		        run_id,
		        share_scope,
		        state,
		        live_session_id,
		        latest_restore_rev,
		        default_binding_key,
		        last_used_at,
		        metadata_json,
		        created_at,
		        updated_at
		   FROM shell_sessions
		  WHERE org_id = $1
		    AND profile_ref = $2
		    AND default_binding_key = $3
		    AND state <> $4
		  ORDER BY last_used_at DESC, updated_at DESC
		  LIMIT 1`,
		orgID,
		profileRef,
		defaultBindingKey,
		ShellSessionStateClosed,
	))
}

func (ShellSessionsRepository) Upsert(
	ctx context.Context,
	pool *pgxpool.Pool,
	record ShellSessionRecord,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return fmt.Errorf("pool must not be nil")
	}
	normalized, metadataRaw, err := normalizeShellSessionRecord(record)
	if err != nil {
		return err
	}

	_, err = pool.Exec(
		ctx,
		`INSERT INTO shell_sessions (
			session_ref,
			org_id,
			profile_ref,
			workspace_ref,
			project_id,
			thread_id,
			run_id,
			share_scope,
			state,
			live_session_id,
			latest_restore_rev,
			default_binding_key,
			last_used_at,
			metadata_json
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, now(), $13::jsonb
		)
		ON CONFLICT (session_ref) DO UPDATE SET
			profile_ref = EXCLUDED.profile_ref,
			workspace_ref = EXCLUDED.workspace_ref,
			project_id = EXCLUDED.project_id,
			thread_id = EXCLUDED.thread_id,
			run_id = EXCLUDED.run_id,
			share_scope = EXCLUDED.share_scope,
			state = EXCLUDED.state,
			live_session_id = EXCLUDED.live_session_id,
			latest_restore_rev = COALESCE(EXCLUDED.latest_restore_rev, shell_sessions.latest_restore_rev),
			default_binding_key = COALESCE(EXCLUDED.default_binding_key, shell_sessions.default_binding_key),
			last_used_at = now(),
			metadata_json = EXCLUDED.metadata_json,
			updated_at = now()`,
		normalized.SessionRef,
		normalized.OrgID,
		normalized.ProfileRef,
		normalized.WorkspaceRef,
		normalized.ProjectID,
		normalized.ThreadID,
		normalized.RunID,
		normalized.ShareScope,
		normalized.State,
		normalized.LiveSessionID,
		normalized.LatestRestoreRev,
		normalized.DefaultBindingKey,
		string(metadataRaw),
	)
	return err
}

func (ShellSessionsRepository) Touch(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID uuid.UUID,
	sessionRef string,
) error {
	return (ShellSessionsRepository{}).TouchLastUsed(ctx, pool, orgID, sessionRef)
}

func (ShellSessionsRepository) TouchLastUsed(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID uuid.UUID,
	sessionRef string,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return fmt.Errorf("pool must not be nil")
	}
	sessionRef = strings.TrimSpace(sessionRef)
	if orgID == uuid.Nil {
		return fmt.Errorf("org_id must not be empty")
	}
	if sessionRef == "" {
		return fmt.Errorf("session_ref must not be empty")
	}
	_, err := pool.Exec(
		ctx,
		`UPDATE shell_sessions
		    SET last_used_at = now(),
		        updated_at = now()
		  WHERE org_id = $1
		    AND session_ref = $2`,
		orgID,
		sessionRef,
	)
	return err
}

func (ShellSessionsRepository) UpdateRestoreRevision(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID uuid.UUID,
	sessionRef string,
	revision string,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return fmt.Errorf("pool must not be nil")
	}
	sessionRef = strings.TrimSpace(sessionRef)
	revision = strings.TrimSpace(revision)
	if orgID == uuid.Nil {
		return fmt.Errorf("org_id must not be empty")
	}
	if sessionRef == "" {
		return fmt.Errorf("session_ref must not be empty")
	}
	_, err := pool.Exec(
		ctx,
		`UPDATE shell_sessions
		    SET latest_restore_rev = NULLIF($3, ''),
		        updated_at = now(),
		        last_used_at = now()
		  WHERE org_id = $1
		    AND session_ref = $2`,
		orgID,
		sessionRef,
		revision,
	)
	return err
}

func (ShellSessionsRepository) SetDefaultBindingKey(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID uuid.UUID,
	sessionRef string,
	defaultBindingKey string,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return fmt.Errorf("pool must not be nil")
	}
	if orgID == uuid.Nil {
		return fmt.Errorf("org_id must not be empty")
	}
	sessionRef = strings.TrimSpace(sessionRef)
	defaultBindingKey = strings.TrimSpace(defaultBindingKey)
	if sessionRef == "" {
		return fmt.Errorf("session_ref must not be empty")
	}
	_, err := pool.Exec(
		ctx,
		`UPDATE shell_sessions
		    SET default_binding_key = NULLIF($3, ''),
		        updated_at = now(),
		        last_used_at = now()
		  WHERE org_id = $1
		    AND session_ref = $2`,
		orgID,
		sessionRef,
		defaultBindingKey,
	)
	return err
}

func (ShellSessionsRepository) ClearLiveSession(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID uuid.UUID,
	sessionRef string,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return fmt.Errorf("pool must not be nil")
	}
	if orgID == uuid.Nil {
		return fmt.Errorf("org_id must not be empty")
	}
	sessionRef = strings.TrimSpace(sessionRef)
	if sessionRef == "" {
		return fmt.Errorf("session_ref must not be empty")
	}
	_, err := pool.Exec(
		ctx,
		`UPDATE shell_sessions
		    SET live_session_id = NULL,
		        state = $3,
		        updated_at = now(),
		        last_used_at = now()
		  WHERE org_id = $1
		    AND session_ref = $2`,
		orgID,
		sessionRef,
		ShellSessionStateReady,
	)
	return err
}

func (ShellSessionsRepository) SetState(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID uuid.UUID,
	sessionRef string,
	state string,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return fmt.Errorf("pool must not be nil")
	}
	if orgID == uuid.Nil {
		return fmt.Errorf("org_id must not be empty")
	}
	sessionRef = strings.TrimSpace(sessionRef)
	if sessionRef == "" {
		return fmt.Errorf("session_ref must not be empty")
	}
	_, err := pool.Exec(
		ctx,
		`UPDATE shell_sessions
		    SET state = $3,
		        updated_at = now(),
		        last_used_at = now()
		  WHERE org_id = $1
		    AND session_ref = $2`,
		orgID,
		sessionRef,
		normalizeShellSessionState(state),
	)
	return err
}

func (ShellSessionsRepository) GetLiveSessionRefsByRun(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID uuid.UUID,
	runID uuid.UUID,
) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}
	if orgID == uuid.Nil {
		return nil, fmt.Errorf("org_id must not be empty")
	}
	if runID == uuid.Nil {
		return nil, fmt.Errorf("run_id must not be empty")
	}
	rows, err := pool.Query(
		ctx,
		`SELECT session_ref
		   FROM shell_sessions
		  WHERE org_id = $1
		    AND run_id = $2
		    AND state <> $3`,
		orgID,
		runID,
		ShellSessionStateClosed,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	refs := []string{}
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, err
		}
		refs = append(refs, strings.TrimSpace(ref))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return refs, nil
}

func scanShellSession(row pgx.Row) (ShellSessionRecord, error) {
	var record ShellSessionRecord
	var metadataRaw []byte
	err := row.Scan(
		&record.SessionRef,
		&record.OrgID,
		&record.ProfileRef,
		&record.WorkspaceRef,
		&record.ProjectID,
		&record.ThreadID,
		&record.RunID,
		&record.ShareScope,
		&record.State,
		&record.LiveSessionID,
		&record.LatestRestoreRev,
		&record.DefaultBindingKey,
		&record.LastUsedAt,
		&metadataRaw,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return ShellSessionRecord{}, err
	}
	if len(metadataRaw) > 0 {
		_ = json.Unmarshal(metadataRaw, &record.MetadataJSON)
	}
	if record.MetadataJSON == nil {
		record.MetadataJSON = map[string]any{}
	}
	record.ShareScope = normalizeShellShareScope(record.ShareScope)
	record.State = normalizeShellSessionState(record.State)
	record.DefaultBindingKey = normalizeOptionalString(record.DefaultBindingKey)
	return record, nil
}

func getShellSession(ctx context.Context, pool *pgxpool.Pool, orgID uuid.UUID, sessionRef string) (ShellSessionRecord, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return ShellSessionRecord{}, fmt.Errorf("pool must not be nil")
	}
	if orgID == uuid.Nil {
		return ShellSessionRecord{}, fmt.Errorf("org_id must not be empty")
	}
	sessionRef = strings.TrimSpace(sessionRef)
	if sessionRef == "" {
		return ShellSessionRecord{}, fmt.Errorf("session_ref must not be empty")
	}
	return scanShellSession(pool.QueryRow(
		ctx,
		`SELECT session_ref,
		        org_id,
		        profile_ref,
		        workspace_ref,
		        project_id,
		        thread_id,
		        run_id,
		        share_scope,
		        state,
		        live_session_id,
		        latest_restore_rev,
		        default_binding_key,
		        last_used_at,
		        metadata_json,
		        created_at,
		        updated_at
		   FROM shell_sessions
		  WHERE org_id = $1
		    AND session_ref = $2`,
		orgID,
		sessionRef,
	))
}

func normalizeShellSessionRecord(record ShellSessionRecord) (ShellSessionRecord, []byte, error) {
	if record.OrgID == uuid.Nil {
		return ShellSessionRecord{}, nil, fmt.Errorf("org_id must not be empty")
	}
	record.SessionRef = strings.TrimSpace(record.SessionRef)
	if record.SessionRef == "" {
		return ShellSessionRecord{}, nil, fmt.Errorf("session_ref must not be empty")
	}
	record.ProfileRef = strings.TrimSpace(record.ProfileRef)
	if record.ProfileRef == "" {
		return ShellSessionRecord{}, nil, fmt.Errorf("profile_ref must not be empty")
	}
	record.WorkspaceRef = strings.TrimSpace(record.WorkspaceRef)
	if record.WorkspaceRef == "" {
		return ShellSessionRecord{}, nil, fmt.Errorf("workspace_ref must not be empty")
	}
	record.ShareScope = normalizeShellShareScope(record.ShareScope)
	record.State = normalizeShellSessionState(record.State)
	record.DefaultBindingKey = normalizeOptionalString(record.DefaultBindingKey)
	if record.MetadataJSON == nil {
		record.MetadataJSON = map[string]any{}
	}
	metadataRaw, err := json.Marshal(record.MetadataJSON)
	if err != nil {
		return ShellSessionRecord{}, nil, fmt.Errorf("marshal metadata_json: %w", err)
	}
	return record, metadataRaw, nil
}

func normalizeShellShareScope(value string) string {
	switch strings.TrimSpace(value) {
	case ShellShareScopeRun, ShellShareScopeThread, ShellShareScopeWorkspace, ShellShareScopeOrg:
		return strings.TrimSpace(value)
	default:
		return ShellShareScopeThread
	}
}

func normalizeShellSessionState(value string) string {
	switch strings.TrimSpace(value) {
	case ShellSessionStateBusy, ShellSessionStateClosed:
		return strings.TrimSpace(value)
	default:
		return ShellSessionStateReady
	}
}

func ShellDefaultBindingKeyForThread(threadID *uuid.UUID) string {
	if threadID == nil || *threadID == uuid.Nil {
		return ""
	}
	return "thread:" + threadID.String()
}

func ShellDefaultBindingKeyForWorkspace(workspaceRef string) string {
	workspaceRef = strings.TrimSpace(workspaceRef)
	if workspaceRef == "" {
		return ""
	}
	return "workspace:" + workspaceRef
}

func IsShellSessionNotFound(err error) bool {
	return err != nil && err == pgx.ErrNoRows
}
