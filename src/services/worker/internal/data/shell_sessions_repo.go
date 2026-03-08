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
	ShellSessionStateReady = "ready"
	ShellSessionStateBusy  = "busy"
	ShellSessionStateClosed = "closed"

	ShellShareScopeRun       = "run"
	ShellShareScopeThread    = "thread"
	ShellShareScopeWorkspace = "workspace"
	ShellShareScopeOrg       = "org"
)

type ShellSessionRecord struct {
	SessionRef          string
	OrgID               uuid.UUID
	ProfileRef          string
	WorkspaceRef        string
	ProjectID           *uuid.UUID
	ThreadID            *uuid.UUID
	RunID               *uuid.UUID
	ShareScope          string
	State               string
	LiveSessionID       *string
	LatestCheckpointRev *string
	LastUsedAt          time.Time
	MetadataJSON        map[string]any
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type ShellSessionsRepository struct{}

func (ShellSessionsRepository) GetBySessionRef(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID uuid.UUID,
	sessionRef string,
) (ShellSessionRecord, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return ShellSessionRecord{}, fmt.Errorf("pool must not be nil")
	}
	sessionRef = strings.TrimSpace(sessionRef)
	if sessionRef == "" {
		return ShellSessionRecord{}, fmt.Errorf("session_ref must not be empty")
	}

	var record ShellSessionRecord
	var metadataRaw []byte
	err := pool.QueryRow(
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
		        latest_checkpoint_rev,
		        last_used_at,
		        metadata_json,
		        created_at,
		        updated_at
		   FROM shell_sessions
		  WHERE org_id = $1
		    AND session_ref = $2`,
		orgID,
		sessionRef,
	).Scan(
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
		&record.LatestCheckpointRev,
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
	return record, nil
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
	if record.OrgID == uuid.Nil {
		return fmt.Errorf("org_id must not be empty")
	}
	record.SessionRef = strings.TrimSpace(record.SessionRef)
	if record.SessionRef == "" {
		return fmt.Errorf("session_ref must not be empty")
	}
	record.ProfileRef = strings.TrimSpace(record.ProfileRef)
	if record.ProfileRef == "" {
		return fmt.Errorf("profile_ref must not be empty")
	}
	record.WorkspaceRef = strings.TrimSpace(record.WorkspaceRef)
	if record.WorkspaceRef == "" {
		return fmt.Errorf("workspace_ref must not be empty")
	}
	record.ShareScope = normalizeShellShareScope(record.ShareScope)
	record.State = normalizeShellSessionState(record.State)
	if record.MetadataJSON == nil {
		record.MetadataJSON = map[string]any{}
	}
	metadataRaw, err := json.Marshal(record.MetadataJSON)
	if err != nil {
		return fmt.Errorf("marshal metadata_json: %w", err)
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
			latest_checkpoint_rev,
			last_used_at,
			metadata_json
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now(), $12::jsonb
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
			latest_checkpoint_rev = COALESCE(EXCLUDED.latest_checkpoint_rev, shell_sessions.latest_checkpoint_rev),
			last_used_at = now(),
			metadata_json = EXCLUDED.metadata_json,
			updated_at = now()`,
		record.SessionRef,
		record.OrgID,
		record.ProfileRef,
		record.WorkspaceRef,
		record.ProjectID,
		record.ThreadID,
		record.RunID,
		record.ShareScope,
		record.State,
		record.LiveSessionID,
		record.LatestCheckpointRev,
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
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return fmt.Errorf("pool must not be nil")
	}
	sessionRef = strings.TrimSpace(sessionRef)
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

func (ShellSessionsRepository) UpdateCheckpointRevision(
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
	if sessionRef == "" {
		return fmt.Errorf("session_ref must not be empty")
	}
	_, err := pool.Exec(
		ctx,
		`UPDATE shell_sessions
		    SET latest_checkpoint_rev = NULLIF($3, ''),
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

func IsShellSessionNotFound(err error) bool {
	return err != nil && err == pgx.ErrNoRows
}
