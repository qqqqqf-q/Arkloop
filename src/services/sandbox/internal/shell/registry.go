package shell

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SessionRestoreRegistry interface {
	GetLatestRestoreRevision(ctx context.Context, orgID, sessionID string) (string, error)
	BindLatestRestoreRevision(ctx context.Context, orgID, sessionID, revision string) error
}

type memorySessionRestoreRegistry struct {
	mu        sync.Mutex
	revisions map[string]string
}

type PGSessionRestoreRegistry struct {
	pool *pgxpool.Pool
}

func NewMemorySessionRestoreRegistry() SessionRestoreRegistry {
	return &memorySessionRestoreRegistry{revisions: map[string]string{}}
}

func NewPGSessionRestoreRegistry(pool *pgxpool.Pool) SessionRestoreRegistry {
	if pool == nil {
		return NewMemorySessionRestoreRegistry()
	}
	return &PGSessionRestoreRegistry{pool: pool}
}

func (r *memorySessionRestoreRegistry) GetLatestRestoreRevision(_ context.Context, orgID, sessionID string) (string, error) {
	if r == nil {
		return "", os.ErrNotExist
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	revision := strings.TrimSpace(r.revisions[restoreRegistryKey(orgID, sessionID)])
	if revision == "" {
		return "", os.ErrNotExist
	}
	return revision, nil
}

func (r *memorySessionRestoreRegistry) BindLatestRestoreRevision(_ context.Context, orgID, sessionID, revision string) error {
	if r == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	revision = strings.TrimSpace(revision)
	if sessionID == "" || revision == "" {
		return fmt.Errorf("session_ref and revision must not be empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.revisions[restoreRegistryKey(orgID, sessionID)] = revision
	return nil
}

func (r *PGSessionRestoreRegistry) GetLatestRestoreRevision(ctx context.Context, orgID, sessionID string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil || r.pool == nil {
		return "", os.ErrNotExist
	}
	parsedOrgID, err := parseRestoreOrgID(orgID)
	if err != nil {
		return "", err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", os.ErrNotExist
	}
	var revision *string
	if err := r.pool.QueryRow(ctx, `SELECT latest_restore_rev FROM shell_sessions WHERE org_id = $1 AND session_ref = $2`, parsedOrgID, sessionID).Scan(&revision); err != nil {
		if err == pgx.ErrNoRows {
			return "", os.ErrNotExist
		}
		return "", err
	}
	if revision == nil || strings.TrimSpace(*revision) == "" {
		return "", os.ErrNotExist
	}
	return strings.TrimSpace(*revision), nil
}

func (r *PGSessionRestoreRegistry) BindLatestRestoreRevision(ctx context.Context, orgID, sessionID, revision string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil || r.pool == nil {
		return nil
	}
	parsedOrgID, err := parseRestoreOrgID(orgID)
	if err != nil {
		return err
	}
	sessionID = strings.TrimSpace(sessionID)
	revision = strings.TrimSpace(revision)
	if sessionID == "" || revision == "" {
		return fmt.Errorf("session_ref and revision must not be empty")
	}
	result, err := r.pool.Exec(ctx, `UPDATE shell_sessions
		SET latest_restore_rev = $3,
		    updated_at = now(),
		    last_used_at = now()
		WHERE org_id = $1
		  AND session_ref = $2`, parsedOrgID, sessionID, revision)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return os.ErrNotExist
	}
	return nil
}

func restoreRegistryKey(orgID, sessionID string) string {
	return strings.TrimSpace(orgID) + "|" + strings.TrimSpace(sessionID)
}

func parseRestoreOrgID(value string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse org_id: %w", err)
	}
	return parsed, nil
}
