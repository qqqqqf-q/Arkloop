package data

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	BindingScopeProject = "project"
	BindingScopeThread  = "thread"
)

type DefaultWorkspaceBindingsRepository struct{}

func (DefaultWorkspaceBindingsRepository) GetOrCreate(
	ctx context.Context,
	tx pgx.Tx,
	orgID uuid.UUID,
	ownerUserID *uuid.UUID,
	profileRef string,
	bindingScope string,
	bindingTargetID uuid.UUID,
	workspaceRef string,
) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if tx == nil {
		return "", fmt.Errorf("tx must not be nil")
	}
	if orgID == uuid.Nil {
		return "", fmt.Errorf("org_id must not be empty")
	}
	profileRef = strings.TrimSpace(profileRef)
	if profileRef == "" {
		return "", fmt.Errorf("profile_ref must not be empty")
	}
	if bindingTargetID == uuid.Nil {
		return "", fmt.Errorf("binding_target_id must not be empty")
	}
	if bindingScope != BindingScopeProject && bindingScope != BindingScopeThread {
		return "", fmt.Errorf("binding_scope must be project or thread")
	}
	workspaceRef = strings.TrimSpace(workspaceRef)
	if workspaceRef == "" {
		return "", fmt.Errorf("workspace_ref must not be empty")
	}

	var existing string
	err := tx.QueryRow(
		ctx,
		`SELECT workspace_ref
		   FROM default_workspace_bindings
		  WHERE org_id = $1
		    AND profile_ref = $2
		    AND binding_scope = $3
		    AND binding_target_id = $4
		  LIMIT 1`,
		orgID,
		profileRef,
		bindingScope,
		bindingTargetID,
	).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	result, err := tx.Exec(
		ctx,
		`INSERT INTO default_workspace_bindings (
			profile_ref,
			owner_user_id,
			org_id,
			binding_scope,
			binding_target_id,
			workspace_ref
		 ) VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (org_id, profile_ref, binding_scope, binding_target_id) DO NOTHING`,
		profileRef,
		ownerUserID,
		orgID,
		bindingScope,
		bindingTargetID,
		workspaceRef,
	)
	if err != nil {
		return "", err
	}
	if result.RowsAffected() > 0 {
		return workspaceRef, nil
	}

	err = tx.QueryRow(
		ctx,
		`SELECT workspace_ref
		   FROM default_workspace_bindings
		  WHERE org_id = $1
		    AND profile_ref = $2
		    AND binding_scope = $3
		    AND binding_target_id = $4
		  LIMIT 1`,
		orgID,
		profileRef,
		bindingScope,
		bindingTargetID,
	).Scan(&existing)
	if err != nil {
		return "", err
	}
	return existing, nil
}
