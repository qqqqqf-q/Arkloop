package environmentbindings

import (
	"context"
	"fmt"
	"strings"

	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func ResolveAndPersistRun(ctx context.Context, pool *pgxpool.Pool, run data.Run) (data.Run, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if pool == nil {
		return data.Run{}, fmt.Errorf("pool must not be nil")
	}
	if run.ID == uuid.Nil {
		return data.Run{}, fmt.Errorf("run_id must not be empty")
	}
	if run.OrgID == uuid.Nil {
		return data.Run{}, fmt.Errorf("org_id must not be empty")
	}
	if run.ThreadID == uuid.Nil {
		return data.Run{}, fmt.Errorf("thread_id must not be empty")
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return data.Run{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	runsRepo := data.RunsRepository{}
	if err := runsRepo.LockRunRow(ctx, tx, run.ID); err != nil {
		return data.Run{}, err
	}
	stored, err := runsRepo.GetRun(ctx, tx, run.ID)
	if err != nil {
		return data.Run{}, err
	}
	if stored == nil {
		return data.Run{}, fmt.Errorf("run not found: %s", run.ID)
	}

	resolved := *stored
	if resolved.ProfileRef != nil && resolved.WorkspaceRef != nil {
		if err := tx.Commit(ctx); err != nil {
			return data.Run{}, err
		}
		return resolved, nil
	}

	profileRef := strings.TrimSpace(deref(resolved.ProfileRef))
	if profileRef == "" {
		profileRef = makeProfileRef(resolved.OrgID, resolved.CreatedByUserID, resolved.ThreadID)
	}

	profileRepo := data.ProfileRegistriesRepository{}
	profileRecord, err := profileRepo.GetOrCreate(ctx, pool, data.RegistryRecord{
		Ref:         profileRef,
		OrgID:       resolved.OrgID,
		OwnerUserID: resolved.CreatedByUserID,
	})
	if err != nil {
		return data.Run{}, err
	}

	bindingScope, bindingTargetID := bindingKey(resolved)
	workspaceRef := strings.TrimSpace(deref(resolved.WorkspaceRef))
	workspaceCreated := false
	if workspaceRef == "" {
		candidate := makeWorkspaceRef(resolved.OrgID, profileRef, bindingScope, bindingTargetID)
		workspaceRef, workspaceCreated, err = data.DefaultWorkspaceBindingsRepository{}.GetOrCreate(
			ctx,
			tx,
			resolved.OrgID,
			resolved.CreatedByUserID,
			profileRef,
			bindingScope,
			bindingTargetID,
			candidate,
		)
		if err != nil {
			return data.Run{}, err
		}
	}

	metadata := map[string]any{}
	defaultWorkspaceRef := strings.TrimSpace(deref(profileRecord.DefaultWorkspaceRef))
	if workspaceCreated && defaultWorkspaceRef != "" && defaultWorkspaceRef != workspaceRef {
		skillRefs, err := inheritWorkspaceSkills(ctx, tx, resolved.OrgID, resolved.CreatedByUserID, defaultWorkspaceRef, workspaceRef)
		if err != nil {
			return data.Run{}, err
		}
		if len(skillRefs) > 0 {
			metadata["skill_refs"] = skillRefs
		}
	}

	workspaceRepo := data.WorkspaceRegistriesRepository{}
	if _, err := workspaceRepo.GetOrCreate(ctx, pool, data.RegistryRecord{
		Ref:          workspaceRef,
		OrgID:        resolved.OrgID,
		OwnerUserID:  resolved.CreatedByUserID,
		ProjectID:    resolved.ProjectID,
		MetadataJSON: metadata,
	}); err != nil {
		return data.Run{}, err
	}

	if _, err := profileRepo.GetOrCreate(ctx, pool, data.RegistryRecord{
		Ref:                 profileRef,
		OrgID:               resolved.OrgID,
		OwnerUserID:         resolved.CreatedByUserID,
		DefaultWorkspaceRef: &workspaceRef,
	}); err != nil {
		return data.Run{}, err
	}

	if err := runsRepo.UpdateEnvironmentBindings(ctx, tx, resolved.ID, profileRef, workspaceRef); err != nil {
		return data.Run{}, err
	}
	resolved.ProfileRef = &profileRef
	resolved.WorkspaceRef = &workspaceRef

	if err := tx.Commit(ctx); err != nil {
		return data.Run{}, err
	}
	return resolved, nil
}

func bindingKey(run data.Run) (string, uuid.UUID) {
	if run.ProjectID != nil && *run.ProjectID != uuid.Nil {
		return data.BindingScopeProject, *run.ProjectID
	}
	return data.BindingScopeThread, run.ThreadID
}

func makeProfileRef(orgID uuid.UUID, ownerUserID *uuid.UUID, threadID uuid.UUID) string {
	seed := orgID.String() + ":"
	if ownerUserID != nil && *ownerUserID != uuid.Nil {
		seed += ownerUserID.String()
	} else {
		seed += threadID.String()
	}
	return "pref_" + uuid.NewSHA1(uuid.NameSpaceOID, []byte(seed)).String()
}

func makeWorkspaceRef(orgID uuid.UUID, profileRef string, bindingScope string, bindingTargetID uuid.UUID) string {
	seed := strings.Join([]string{orgID.String(), profileRef, bindingScope, bindingTargetID.String()}, ":")
	return "wsref_" + uuid.NewSHA1(uuid.NameSpaceOID, []byte(seed)).String()
}

func inheritWorkspaceSkills(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, enabledByUserID *uuid.UUID, fromWorkspaceRef, toWorkspaceRef string) ([]string, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT skill_key, version
		   FROM workspace_skill_enablements
		  WHERE org_id = $1
		    AND workspace_ref = $2
		  ORDER BY skill_key, version`,
		orgID,
		fromWorkspaceRef,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type skillRef struct {
		skillKey string
		version  string
	}
	items := make([]skillRef, 0)
	for rows.Next() {
		var item skillRef
		if err := rows.Scan(&item.skillKey, &item.version); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	refs := make([]string, 0, len(items))
	for _, item := range items {
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO workspace_skill_enablements (workspace_ref, org_id, enabled_by_user_id, skill_key, version)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (workspace_ref, skill_key) DO NOTHING`,
			toWorkspaceRef,
			orgID,
			enabledByUserID,
			item.skillKey,
			item.version,
		); err != nil {
			return nil, err
		}
		refs = append(refs, item.skillKey+"@"+item.version)
	}
	return refs, nil
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
