package environmentbindings

import (
	"context"
	"errors"
	"strings"
	"time"

	sharedenvironmentref "arkloop/services/shared/environmentref"
	"arkloop/services/worker/internal/data"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func ResolveAndPersistRun(ctx context.Context, pool *pgxpool.Pool, run data.Run) (data.Run, error) {
	profileRef := strings.TrimSpace(derefString(run.ProfileRef))
	if profileRef == "" {
		profileRef = sharedenvironmentref.BuildProfileRef(run.OrgID, run.CreatedByUserID)
	}

	workspaceRef := strings.TrimSpace(derefString(run.WorkspaceRef))
	if workspaceRef != "" {
		run.ProfileRef = stringPtr(profileRef)
		run.WorkspaceRef = stringPtr(workspaceRef)
		if err := syncEnvironmentRegistries(ctx, pool, run.OrgID, run.CreatedByUserID, run.ProjectID, profileRef, workspaceRef, nil); err != nil {
			return run, err
		}
		return run, nil
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return run, err
	}
	defer tx.Rollback(ctx)

	sourceWorkspaceRef, err := loadProfileDefaultWorkspaceRefTx(ctx, tx, run.OrgID, profileRef)
	if err != nil {
		return run, err
	}

	bindingScope := data.BindingScopeThread
	bindingTargetID := run.ThreadID
	if run.ProjectID != nil && *run.ProjectID != uuid.Nil {
		bindingScope = data.BindingScopeProject
		bindingTargetID = *run.ProjectID
	}

	bindingsRepo := data.DefaultWorkspaceBindingsRepository{}
	workspaceRef, created, err := bindingsRepo.GetOrCreate(
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

	inheritedRefs, err := inheritWorkspaceSkillRefs(ctx, tx, run.OrgID, run.CreatedByUserID, sourceWorkspaceRef, workspaceRef, created)
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
	if err := syncEnvironmentRegistries(ctx, pool, run.OrgID, run.CreatedByUserID, run.ProjectID, profileRef, workspaceRef, inheritedRefs); err != nil {
		return run, err
	}

	run.ProfileRef = stringPtr(profileRef)
	run.WorkspaceRef = stringPtr(workspaceRef)
	return run, nil
}

func loadProfileDefaultWorkspaceRefTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, profileRef string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if tx == nil || orgID == uuid.Nil {
		return "", nil
	}
	profileRef = strings.TrimSpace(profileRef)
	if profileRef == "" {
		return "", nil
	}
	var workspaceRef *string
	err := tx.QueryRow(
		ctx,
		`SELECT default_workspace_ref
		   FROM profile_registries
		  WHERE org_id = $1 AND profile_ref = $2`,
		orgID,
		profileRef,
	).Scan(&workspaceRef)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(derefString(workspaceRef)), nil
}

func inheritWorkspaceSkillRefs(
	ctx context.Context,
	tx pgx.Tx,
	orgID uuid.UUID,
	ownerUserID *uuid.UUID,
	sourceWorkspaceRef string,
	targetWorkspaceRef string,
	created bool,
) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !created || tx == nil || ownerUserID == nil || *ownerUserID == uuid.Nil {
		return nil, nil
	}
	sourceWorkspaceRef = strings.TrimSpace(sourceWorkspaceRef)
	targetWorkspaceRef = strings.TrimSpace(targetWorkspaceRef)
	if sourceWorkspaceRef == "" || targetWorkspaceRef == "" || sourceWorkspaceRef == targetWorkspaceRef {
		return nil, nil
	}
	rows, err := tx.Query(
		ctx,
		`SELECT skill_key, version
		   FROM workspace_skill_enablements
		  WHERE org_id = $1 AND workspace_ref = $2
		  ORDER BY skill_key, version`,
		orgID,
		sourceWorkspaceRef,
	)
	if err != nil {
		return nil, err
	}
	type workspaceSkillRef struct {
		skillKey string
		version  string
	}
	items := make([]workspaceSkillRef, 0)
	for rows.Next() {
		var item workspaceSkillRef
		if err := rows.Scan(&item.skillKey, &item.version); err != nil {
			rows.Close()
			return nil, err
		}
		item.skillKey = strings.TrimSpace(item.skillKey)
		item.version = strings.TrimSpace(item.version)
		if item.skillKey == "" || item.version == "" {
			continue
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	refs := make([]string, 0, len(items))
	for _, item := range items {
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO workspace_skill_enablements (workspace_ref, org_id, enabled_by_user_id, skill_key, version)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (workspace_ref, skill_key) DO UPDATE
			 SET version = EXCLUDED.version,
			     enabled_by_user_id = EXCLUDED.enabled_by_user_id,
			     updated_at = now()`,
			targetWorkspaceRef,
			orgID,
			*ownerUserID,
			item.skillKey,
			item.version,
		); err != nil {
			return nil, err
		}
		refs = append(refs, item.skillKey+"@"+item.version)
	}
	if len(refs) == 0 {
		return nil, nil
	}
	return refs, nil
}

func syncEnvironmentRegistries(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID uuid.UUID,
	ownerUserID *uuid.UUID,
	projectID *uuid.UUID,
	profileRef string,
	workspaceRef string,
	enabledSkillRefs []string,
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
	workspaceMetadata := map[string]any{}
	if len(enabledSkillRefs) > 0 {
		workspaceMetadata["enabled_skill_refs"] = enabledSkillRefs
	}
	workspaceRepo := data.WorkspaceRegistriesRepository{}
	return workspaceRepo.UpsertTouch(ctx, pool, data.RegistryRecord{
		Ref:          strings.TrimSpace(workspaceRef),
		OrgID:        orgID,
		OwnerUserID:  ownerUserID,
		ProjectID:    projectID,
		FlushState:   data.FlushStateIdle,
		LastUsedAt:   now,
		MetadataJSON: workspaceMetadata,
	})
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
