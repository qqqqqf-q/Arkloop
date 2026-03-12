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
	accountID uuid.UUID,
	ownerUserID *uuid.UUID,
	profileRef string,
	bindingScope string,
	bindingTargetID uuid.UUID,
	workspaceRef string,
) (string, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if tx == nil {
		return "", false, fmt.Errorf("tx must not be nil")
	}
	if accountID == uuid.Nil {
		return "", false, fmt.Errorf("account_id must not be empty")
	}
	profileRef = strings.TrimSpace(profileRef)
	if profileRef == "" {
		return "", false, fmt.Errorf("profile_ref must not be empty")
	}
	if bindingTargetID == uuid.Nil {
		return "", false, fmt.Errorf("binding_target_id must not be empty")
	}
	if bindingScope != BindingScopeProject && bindingScope != BindingScopeThread {
		return "", false, fmt.Errorf("binding_scope must be project or thread")
	}
	workspaceRef = strings.TrimSpace(workspaceRef)
	if workspaceRef == "" {
		return "", false, fmt.Errorf("workspace_ref must not be empty")
	}

	var existing string
	err := tx.QueryRow(
		ctx,
		`SELECT workspace_ref
		   FROM default_workspace_bindings
		  WHERE account_id = $1
		    AND profile_ref = $2
		    AND binding_scope = $3
		    AND binding_target_id = $4
		  LIMIT 1`,
		accountID,
		profileRef,
		bindingScope,
		bindingTargetID,
	).Scan(&existing)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}

	result, err := tx.Exec(
		ctx,
		`INSERT INTO default_workspace_bindings (
			profile_ref,
			owner_user_id,
			account_id,
			binding_scope,
			binding_target_id,
			workspace_ref
		 ) VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (account_id, profile_ref, binding_scope, binding_target_id) DO NOTHING`,
		profileRef,
		ownerUserID,
		accountID,
		bindingScope,
		bindingTargetID,
		workspaceRef,
	)
	if err != nil {
		return "", false, err
	}
	if result.RowsAffected() > 0 {
		return workspaceRef, true, nil
	}

	err = tx.QueryRow(
		ctx,
		`SELECT workspace_ref
		   FROM default_workspace_bindings
		  WHERE account_id = $1
		    AND profile_ref = $2
		    AND binding_scope = $3
		    AND binding_target_id = $4
		  LIMIT 1`,
		accountID,
		profileRef,
		bindingScope,
		bindingTargetID,
	).Scan(&existing)
	if err != nil {
		return "", false, err
	}
	return existing, false, nil
}
