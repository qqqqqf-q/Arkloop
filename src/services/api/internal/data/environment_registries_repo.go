package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
"arkloop/services/shared/database"
)

type ProfileRegistry struct {
	ProfileRef          string
	OrgID               uuid.UUID
	OwnerUserID         *uuid.UUID
	DefaultWorkspaceRef *string
	LastUsedAt          time.Time
	MetadataJSON        map[string]any
}

type WorkspaceRegistry struct {
	WorkspaceRef           string
	OrgID                  uuid.UUID
	OwnerUserID            *uuid.UUID
	ProjectID              *uuid.UUID
	LatestManifestRev      *string
	DefaultShellSessionRef *string
	LastUsedAt             time.Time
	MetadataJSON           map[string]any
}

type ProfileRegistriesRepository struct {
	db      Querier
	dialect database.DialectHelper
}

type WorkspaceRegistriesRepository struct {
	db      Querier
	dialect database.DialectHelper
}

func NewProfileRegistriesRepository(db Querier, dialect ...database.DialectHelper) (*ProfileRegistriesRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	d := database.DialectHelper(database.PostgresDialect{})
	if len(dialect) > 0 && dialect[0] != nil {
		d = dialect[0]
	}
	return &ProfileRegistriesRepository{db: db, dialect: d}, nil
}

func NewWorkspaceRegistriesRepository(db Querier, dialect ...database.DialectHelper) (*WorkspaceRegistriesRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	d := database.DialectHelper(database.PostgresDialect{})
	if len(dialect) > 0 && dialect[0] != nil {
		d = dialect[0]
	}
	return &WorkspaceRegistriesRepository{db: db, dialect: d}, nil
}

func (r *ProfileRegistriesRepository) Get(ctx context.Context, profileRef string) (*ProfileRegistry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var record ProfileRegistry
	var metadataRaw []byte
	err := r.db.QueryRow(
		ctx,
		`SELECT profile_ref, org_id, owner_user_id, default_workspace_ref, last_used_at, metadata_json
		   FROM profile_registries
		  WHERE profile_ref = $1`,
		strings.TrimSpace(profileRef),
	).Scan(&record.ProfileRef, &record.OrgID, &record.OwnerUserID, &record.DefaultWorkspaceRef, &record.LastUsedAt, &metadataRaw)
	if err != nil {
		if errors.Is(err, database.ErrNoRows) {
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
		`SELECT workspace_ref, org_id, owner_user_id, project_id, latest_manifest_rev, default_shell_session_ref, last_used_at, metadata_json
		   FROM workspace_registries
		  WHERE workspace_ref = $1`,
		strings.TrimSpace(workspaceRef),
	).Scan(&record.WorkspaceRef, &record.OrgID, &record.OwnerUserID, &record.ProjectID, &record.LatestManifestRev, &record.DefaultShellSessionRef, &record.LastUsedAt, &metadataRaw)
	if err != nil {
		if errors.Is(err, database.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(metadataRaw, &record.MetadataJSON)
	return &record, nil
}

func (r *ProfileRegistriesRepository) UpdateInstalledSkillRefs(ctx context.Context, profileRef string, refs []string) error {
	return updateRegistrySkillRefs(ctx, r.db, r.dialect, "profile_registries", "profile_ref", strings.TrimSpace(profileRef), "installed_skill_refs", refs)
}

func (r *ProfileRegistriesRepository) Ensure(ctx context.Context, profileRef string, orgID uuid.UUID, ownerUserID uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("db must not be nil")
	}
	profileRef = strings.TrimSpace(profileRef)
	if profileRef == "" || orgID == uuid.Nil {
		return fmt.Errorf("profile_ref and org_id must not be empty")
	}
	_, err := r.db.Exec(
		ctx,
		`INSERT INTO profile_registries (profile_ref, org_id, owner_user_id, last_used_at, metadata_json)
		 VALUES ($1, $2, $3, now(), `+r.dialect.JSONCast("'{}'")+`)
		 ON CONFLICT (profile_ref)
		 DO UPDATE SET owner_user_id = COALESCE(profile_registries.owner_user_id, EXCLUDED.owner_user_id),
		               last_used_at = now(),
		               updated_at = now()`,
		profileRef,
		orgID,
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
		        last_used_at = now(),
		        updated_at = now()
		  WHERE profile_ref = $1`,
		profileRef,
		workspaceRef,
	)
	return err
}

func (r *WorkspaceRegistriesRepository) UpdateEnabledSkillRefs(ctx context.Context, workspaceRef string, refs []string) error {
	return updateRegistrySkillRefs(ctx, r.db, r.dialect, "workspace_registries", "workspace_ref", strings.TrimSpace(workspaceRef), "enabled_skill_refs", refs)
}

func (r *WorkspaceRegistriesRepository) Ensure(ctx context.Context, workspaceRef string, orgID uuid.UUID, ownerUserID uuid.UUID, projectID *uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("db must not be nil")
	}
	workspaceRef = strings.TrimSpace(workspaceRef)
	if workspaceRef == "" || orgID == uuid.Nil {
		return fmt.Errorf("workspace_ref and org_id must not be empty")
	}
	_, err := r.db.Exec(
		ctx,
		`INSERT INTO workspace_registries (workspace_ref, org_id, owner_user_id, project_id, last_used_at, metadata_json)
		 VALUES ($1, $2, $3, $4, now(), `+r.dialect.JSONCast("'{}'")+`)
		 ON CONFLICT (workspace_ref)
		 DO UPDATE SET owner_user_id = COALESCE(workspace_registries.owner_user_id, EXCLUDED.owner_user_id),
		               project_id = COALESCE(workspace_registries.project_id, EXCLUDED.project_id),
		               last_used_at = now(),
		               updated_at = now()`,
		workspaceRef,
		orgID,
		ownerUserID,
		projectID,
	)
	return err
}

func updateRegistrySkillRefs(ctx context.Context, db Querier, dialect database.DialectHelper, table string, idColumn string, idValue string, field string, refs []string) error {
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
	// jsonb_set is PG-specific; SQLite uses json_set with slightly different syntax.
	// Both receive the same arguments here so we use jsonb_set for PG and json_set for SQLite.
	jsonSetFn := "jsonb_set"
	if dialect.Name() == database.DialectSQLite {
		jsonSetFn = "json_set"
	}
	emptyObj := dialect.JSONCast("'{}'")
	valCast := dialect.JSONCast("$2")
	_, err = db.Exec(
		ctx,
		fmt.Sprintf(`UPDATE %s
		    SET metadata_json = %s(COALESCE(metadata_json, %s), '{%s}', %s, true),
		        updated_at = now()
		  WHERE %s = $1`, table, jsonSetFn, emptyObj, field, valCast, idColumn),
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
