package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ShellSessionRepository struct {
	db Querier
}

type ShellSession struct {
	SessionRef    string
	SessionType   string
	AccountID     uuid.UUID
	ProfileRef    string
	WorkspaceRef  string
	State         string
	LiveSessionID *string
	LastUsedAt    time.Time
}

func NewShellSessionRepository(db Querier) (*ShellSessionRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ShellSessionRepository{db: db}, nil
}

func (r *ShellSessionRepository) GetRunIDBySessionRef(ctx context.Context, accountID uuid.UUID, sessionRef string) (*uuid.UUID, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return nil, fmt.Errorf("account_id must not be empty")
	}
	sessionRef = strings.TrimSpace(sessionRef)
	if sessionRef == "" {
		return nil, fmt.Errorf("session_ref must not be empty")
	}

	var runID *uuid.UUID
	err := r.db.QueryRow(
		ctx,
		`SELECT run_id
		   FROM shell_sessions
		  WHERE account_id = $1
		    AND session_ref = $2`,
		accountID,
		sessionRef,
	).Scan(&runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return runID, nil
}

func (r *ShellSessionRepository) GetBySessionRef(ctx context.Context, accountID uuid.UUID, sessionRef string) (*ShellSession, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	if accountID == uuid.Nil {
		return nil, fmt.Errorf("account_id must not be empty")
	}
	sessionRef = strings.TrimSpace(sessionRef)
	if sessionRef == "" {
		return nil, fmt.Errorf("session_ref must not be empty")
	}

	var session ShellSession
	err := r.db.QueryRow(
		ctx,
		`SELECT session_ref, session_type, account_id, profile_ref, workspace_ref, state, live_session_id, last_used_at
		   FROM shell_sessions
		  WHERE account_id = $1
		    AND session_ref = $2`,
		accountID,
		sessionRef,
	).Scan(
		&session.SessionRef, &session.SessionType, &session.AccountID,
		&session.ProfileRef, &session.WorkspaceRef, &session.State,
		&session.LiveSessionID, &session.LastUsedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *ShellSessionRepository) GetLatestLiveByWorkspaceRef(ctx context.Context, accountID uuid.UUID, workspaceRef string) (*ShellSession, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	if accountID == uuid.Nil {
		return nil, fmt.Errorf("account_id must not be empty")
	}
	workspaceRef = strings.TrimSpace(workspaceRef)
	if workspaceRef == "" {
		return nil, fmt.Errorf("workspace_ref must not be empty")
	}

	var session ShellSession
	err := r.db.QueryRow(
		ctx,
		`SELECT session_ref, session_type, account_id, profile_ref, workspace_ref, state, live_session_id, last_used_at
		   FROM shell_sessions
		  WHERE account_id = $1
		    AND workspace_ref = $2
		    AND state <> 'closed'
		    AND live_session_id IS NOT NULL
		    AND TRIM(live_session_id) <> ''
		  ORDER BY last_used_at DESC, updated_at DESC
		  LIMIT 1`,
		accountID,
		workspaceRef,
	).Scan(
		&session.SessionRef, &session.SessionType, &session.AccountID,
		&session.ProfileRef, &session.WorkspaceRef, &session.State,
		&session.LiveSessionID, &session.LastUsedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &session, nil
}
