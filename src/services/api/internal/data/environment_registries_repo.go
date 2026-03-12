package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ProfileRegistry struct {
	ProfileRef          string
	AccountID               uuid.UUID
	OwnerUserID         *uuid.UUID
	DefaultWorkspaceRef *string
	MetadataJSON        map[string]any
}

type WorkspaceRegistry struct {
	WorkspaceRef string
	AccountID        uuid.UUID
	OwnerUserID  *uuid.UUID
	ProjectID    *uuid.UUID
	MetadataJSON map[string]any
}

type ProfileRegistriesRepository struct {
	db Querier
}

type WorkspaceRegistriesRepository struct {
	db Querier
}

func NewProfileRegistriesRepository(db Querier) (*ProfileRegistriesRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ProfileRegistriesRepository{db: db}, nil
}

func NewWorkspaceRegistriesRepository(db Querier) (*WorkspaceRegistriesRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &WorkspaceRegistriesRepository{db: db}, nil
}

func (r *ProfileRegistriesRepository) Get(ctx context.Context, profileRef string) (*ProfileRegistry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var record ProfileRegistry
	var metadataRaw []byte
	err := r.db.QueryRow(
		ctx,
		`SELECT profile_ref, account_id, owner_user_id, default_workspace_ref, metadata_json
		   FROM profile_registries
		  WHERE profile_ref = $1`,
		strings.TrimSpace(profileRef),
	).Scan(&record.ProfileRef, &record.AccountID, &record.OwnerUserID, &record.DefaultWorkspaceRef, &metadataRaw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(metadataRaw, &record.MetadataJSON)
	return &record, nil
}

func (r *WorkspaceRegistriesRepository) Get(ctx context.Context, workspaceRef string) (*WorkspaceRegistry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var record WorkspaceRegistry
	var metadataRaw []byte
	err := r.db.QueryRow(
		ctx,
		`SELECT workspace_ref, account_id, owner_user_id, project_id, metadata_json
		   FROM workspace_registries
		  WHERE workspace_ref = $1`,
		strings.TrimSpace(workspaceRef),
	).Scan(&record.WorkspaceRef, &record.AccountID, &record.OwnerUserID, &record.ProjectID, &metadataRaw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(metadataRaw, &record.MetadataJSON)
	return &record, nil
}

func (r *ProfileRegistriesRepository) UpdateInstalledSkillRefs(ctx context.Context, profileRef string, refs []string) error {
	return updateRegistrySkillRefs(ctx, r.db, "profile_registries", "profile_ref", strings.TrimSpace(profileRef), "installed_skill_refs", refs)
}

func (r *ProfileRegistriesRepository) Ensure(ctx context.Context, profileRef string, accountID uuid.UUID, ownerUserID uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("db must not be nil")
	}
	profileRef = strings.TrimSpace(profileRef)
	if profileRef == "" || accountID == uuid.Nil {
		return fmt.Errorf("profile_ref and account_id must not be empty")
	}
	_, err := r.db.Exec(
		ctx,
		`INSERT INTO profile_registries (profile_ref, account_id, owner_user_id, metadata_json)
		 VALUES ($1, $2, $3, '{}'::jsonb)
		 ON CONFLICT (profile_ref)
		 DO UPDATE SET owner_user_id = COALESCE(profile_registries.owner_user_id, EXCLUDED.owner_user_id),
		               updated_at = now()`,
		profileRef,
		accountID,
		ownerUserID,
	)
	return err
}

func (r *ProfileRegistriesRepository) SetDefaultWorkspaceRef(ctx context.Context, profileRef string, workspaceRef string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("db must not be nil")
	}
	profileRef = strings.TrimSpace(profileRef)
	workspaceRef = strings.TrimSpace(workspaceRef)
	if profileRef == "" || workspaceRef == "" {
		return fmt.Errorf("profile_ref and workspace_ref must not be empty")
	}
	_, err := r.db.Exec(
		ctx,
		`UPDATE profile_registries
		    SET default_workspace_ref = $2,
		        updated_at = now()
		  WHERE profile_ref = $1`,
		profileRef,
		workspaceRef,
	)
	return err
}

func (r *WorkspaceRegistriesRepository) UpdateEnabledSkillRefs(ctx context.Context, workspaceRef string, refs []string) error {
	return updateRegistrySkillRefs(ctx, r.db, "workspace_registries", "workspace_ref", strings.TrimSpace(workspaceRef), "enabled_skill_refs", refs)
}

func (r *WorkspaceRegistriesRepository) Ensure(ctx context.Context, workspaceRef string, accountID uuid.UUID, ownerUserID uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("db must not be nil")
	}
	workspaceRef = strings.TrimSpace(workspaceRef)
	if workspaceRef == "" || accountID == uuid.Nil {
		return fmt.Errorf("workspace_ref and account_id must not be empty")
	}
	_, err := r.db.Exec(
		ctx,
		`INSERT INTO workspace_registries (workspace_ref, account_id, owner_user_id, metadata_json)
		 VALUES ($1, $2, $3, '{}'::jsonb)
		 ON CONFLICT (workspace_ref)
		 DO UPDATE SET owner_user_id = COALESCE(workspace_registries.owner_user_id, EXCLUDED.owner_user_id),
		               updated_at = now()`,
		workspaceRef,
		accountID,
		ownerUserID,
	)
	return err
}

func updateRegistrySkillRefs(ctx context.Context, db Querier, table string, idColumn string, idValue string, field string, refs []string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil {
		return fmt.Errorf("db must not be nil")
	}
	if idValue == "" {
		return fmt.Errorf("registry id must not be empty")
	}
	payload, err := json.Marshal(dedupeSortedStrings(refs))
	if err != nil {
		return err
	}
	_, err = db.Exec(
		ctx,
		fmt.Sprintf(`UPDATE %s
		    SET metadata_json = jsonb_set(COALESCE(metadata_json, '{}'::jsonb), '{%s}', $2::jsonb, true),
		        updated_at = now()
		  WHERE %s = $1`, table, field, idColumn),
		idValue,
		payload,
	)
	return err
}

func dedupeSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		cleaned := strings.TrimSpace(value)
		if cleaned == "" {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	sort.Strings(out)
	return out
}
