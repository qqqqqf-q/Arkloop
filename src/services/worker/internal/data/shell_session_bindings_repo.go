package data

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	ShellBindingScopeThread    = "thread"
	ShellBindingScopeWorkspace = "workspace"
)

type DefaultShellSessionBindingsRepository struct{}

func (DefaultShellSessionBindingsRepository) Get(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID uuid.UUID,
	profileRef string,
	bindingScope string,
	bindingTarget string,
) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return "", fmt.Errorf("pool must not be nil")
	}
	profileRef = strings.TrimSpace(profileRef)
	bindingScope = normalizeShellBindingScope(bindingScope)
	bindingTarget = strings.TrimSpace(bindingTarget)
	if profileRef == "" {
		return "", fmt.Errorf("profile_ref must not be empty")
	}
	if bindingTarget == "" {
		return "", fmt.Errorf("binding_target must not be empty")
	}
	var sessionRef string
	err := pool.QueryRow(
		ctx,
		`SELECT session_ref
		   FROM default_shell_session_bindings
		  WHERE org_id = $1
		    AND profile_ref = $2
		    AND binding_scope = $3
		    AND binding_target = $4`,
		orgID,
		profileRef,
		bindingScope,
		bindingTarget,
	).Scan(&sessionRef)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(sessionRef), nil
}

func (DefaultShellSessionBindingsRepository) Upsert(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID uuid.UUID,
	profileRef string,
	bindingScope string,
	bindingTarget string,
	sessionRef string,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return fmt.Errorf("pool must not be nil")
	}
	profileRef = strings.TrimSpace(profileRef)
	bindingScope = normalizeShellBindingScope(bindingScope)
	bindingTarget = strings.TrimSpace(bindingTarget)
	sessionRef = strings.TrimSpace(sessionRef)
	if profileRef == "" {
		return fmt.Errorf("profile_ref must not be empty")
	}
	if bindingTarget == "" {
		return fmt.Errorf("binding_target must not be empty")
	}
	if sessionRef == "" {
		return fmt.Errorf("session_ref must not be empty")
	}
	_, err := pool.Exec(
		ctx,
		`INSERT INTO default_shell_session_bindings (
			org_id,
			profile_ref,
			binding_scope,
			binding_target,
			session_ref
		) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (org_id, profile_ref, binding_scope, binding_target) DO UPDATE SET
			session_ref = EXCLUDED.session_ref,
			updated_at = now()`,
		orgID,
		profileRef,
		bindingScope,
		bindingTarget,
		sessionRef,
	)
	return err
}

func normalizeShellBindingScope(value string) string {
	if strings.TrimSpace(value) == ShellBindingScopeWorkspace {
		return ShellBindingScopeWorkspace
	}
	return ShellBindingScopeThread
}

func ShellBindingTargetForThread(threadID *uuid.UUID) string {
	if threadID == nil || *threadID == uuid.Nil {
		return ""
	}
	return threadID.String()
}

func ShellBindingTargetForWorkspace(workspaceRef string) string {
	return strings.TrimSpace(workspaceRef)
}

func IsDefaultShellSessionBindingNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
