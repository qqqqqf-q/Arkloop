package runengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"arkloop/services/worker/internal/data"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func resolveAndPersistEnvironmentBindings(ctx context.Context, pool *pgxpool.Pool, run data.Run) (data.Run, error) {
	profileRef := strings.TrimSpace(derefString(run.ProfileRef))
	if profileRef == "" {
		profileRef = buildProfileRef(run.OrgID, run.CreatedByUserID)
	}

	workspaceRef := strings.TrimSpace(derefString(run.WorkspaceRef))
	if workspaceRef != "" {
		run.ProfileRef = stringPtr(profileRef)
		run.WorkspaceRef = stringPtr(workspaceRef)
		if err := syncEnvironmentRegistries(ctx, pool, run.OrgID, run.CreatedByUserID, run.ProjectID, profileRef, workspaceRef); err != nil {
			return run, err
		}
		return run, nil
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return run, err
	}
	defer tx.Rollback(ctx)

	bindingScope := data.BindingScopeThread
	bindingTargetID := run.ThreadID
	if run.ProjectID != nil && *run.ProjectID != uuid.Nil {
		bindingScope = data.BindingScopeProject
		bindingTargetID = *run.ProjectID
	}

	bindingsRepo := data.DefaultWorkspaceBindingsRepository{}
	workspaceRef, err = bindingsRepo.GetOrCreate(
		ctx,
		tx,
		run.OrgID,
		run.CreatedByUserID,
		profileRef,
		bindingScope,
		bindingTargetID,
		newWorkspaceRef(),
	)
	if err != nil {
		return run, err
	}

	runsRepo := data.RunsRepository{}
	if err := runsRepo.UpdateEnvironmentBindings(ctx, tx, run.ID, profileRef, workspaceRef); err != nil {
		return run, err
	}
	if err := tx.Commit(ctx); err != nil {
		return run, err
	}
	if err := syncEnvironmentRegistries(ctx, pool, run.OrgID, run.CreatedByUserID, run.ProjectID, profileRef, workspaceRef); err != nil {
		return run, err
	}

	run.ProfileRef = stringPtr(profileRef)
	run.WorkspaceRef = stringPtr(workspaceRef)
	return run, nil
}

func syncEnvironmentRegistries(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID uuid.UUID,
	ownerUserID *uuid.UUID,
	projectID *uuid.UUID,
	profileRef string,
	workspaceRef string,
) error {
	if pool == nil {
		return nil
	}
	now := time.Now().UTC()
	profileRepo := data.ProfileRegistriesRepository{}
	if err := profileRepo.UpsertTouch(ctx, pool, data.RegistryRecord{
		Ref:                 strings.TrimSpace(profileRef),
		OrgID:               orgID,
		OwnerUserID:         ownerUserID,
		DefaultWorkspaceRef: stringPtr(workspaceRef),
		FlushState:          data.FlushStateIdle,
		LastUsedAt:          now,
		MetadataJSON:        map[string]any{},
	}); err != nil {
		return err
	}
	workspaceRepo := data.WorkspaceRegistriesRepository{}
	return workspaceRepo.UpsertTouch(ctx, pool, data.RegistryRecord{
		Ref:          strings.TrimSpace(workspaceRef),
		OrgID:        orgID,
		OwnerUserID:  ownerUserID,
		ProjectID:    projectID,
		FlushState:   data.FlushStateIdle,
		LastUsedAt:   now,
		MetadataJSON: map[string]any{},
	})
}

func buildProfileRef(orgID uuid.UUID, userID *uuid.UUID) string {
	userKey := "system"
	if userID != nil && *userID != uuid.Nil {
		userKey = userID.String()
	}
	raw := "profile|" + orgID.String() + "|" + userKey
	sum := sha256.Sum256([]byte(raw))
	return "pref_" + hex.EncodeToString(sum[:16])
}

func newWorkspaceRef() string {
	return "wsref_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringPtr(value string) *string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return nil
	}
	copied := cleaned
	return &copied
}
