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

const (
	DefaultWorkspaceBindingScopeProject = "project"
	DefaultWorkspaceBindingScopeThread  = "thread"
)

type DefaultWorkspaceBinding struct {
	ProfileRef      string
	OwnerUserID     *uuid.UUID
	OrgID           uuid.UUID
	BindingScope    string
	BindingTargetID uuid.UUID
	WorkspaceRef    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type DefaultWorkspaceBindingsRepository struct {
	db Querier
}

func NewDefaultWorkspaceBindingsRepository(db Querier) (*DefaultWorkspaceBindingsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &DefaultWorkspaceBindingsRepository{db: db}, nil
}

func (r *DefaultWorkspaceBindingsRepository) Get(
	ctx context.Context,
	orgID uuid.UUID,
	profileRef string,
	bindingScope string,
	bindingTargetID uuid.UUID,
) (*DefaultWorkspaceBinding, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	profileRef = strings.TrimSpace(profileRef)
	bindingScope = strings.TrimSpace(bindingScope)
	if orgID == uuid.Nil || profileRef == "" || bindingTargetID == uuid.Nil {
		return nil, fmt.Errorf("org_id, profile_ref and binding_target_id must not be empty")
	}
	if bindingScope != DefaultWorkspaceBindingScopeProject && bindingScope != DefaultWorkspaceBindingScopeThread {
		return nil, fmt.Errorf("binding_scope must be project or thread")
	}

	var record DefaultWorkspaceBinding
	err := r.db.QueryRow(
		ctx,
		`SELECT profile_ref,
		        owner_user_id,
		        org_id,
		        binding_scope,
		        binding_target_id,
		        workspace_ref,
		        created_at,
		        updated_at
		   FROM default_workspace_bindings
		  WHERE org_id = $1
		    AND profile_ref = $2
		    AND binding_scope = $3
		    AND binding_target_id = $4`,
		orgID,
		profileRef,
		bindingScope,
		bindingTargetID,
	).Scan(
		&record.ProfileRef,
		&record.OwnerUserID,
		&record.OrgID,
		&record.BindingScope,
		&record.BindingTargetID,
		&record.WorkspaceRef,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (r *DefaultWorkspaceBindingsRepository) GetOrCreate(
	ctx context.Context,
	tx pgx.Tx,
	orgID uuid.UUID,
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
	if orgID == uuid.Nil {
		return "", false, fmt.Errorf("org_id must not be empty")
	}
	profileRef = strings.TrimSpace(profileRef)
	bindingScope = strings.TrimSpace(bindingScope)
	workspaceRef = strings.TrimSpace(workspaceRef)
	if profileRef == "" || bindingTargetID == uuid.Nil || workspaceRef == "" {
		return "", false, fmt.Errorf("profile_ref, binding_target_id and workspace_ref must not be empty")
	}
	if bindingScope != DefaultWorkspaceBindingScopeProject && bindingScope != DefaultWorkspaceBindingScopeThread {
		return "", false, fmt.Errorf("binding_scope must be project or thread")
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
		return "", false, err
	}
	if result.RowsAffected() > 0 {
		return workspaceRef, true, nil
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
		return "", false, err
	}
	return existing, false, nil
}
