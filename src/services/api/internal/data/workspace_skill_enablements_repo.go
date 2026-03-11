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

type WorkspaceSkillEnablement struct {
	WorkspaceRef        string
	OrgID               uuid.UUID
	EnabledByUserID     uuid.UUID
	SkillKey            string
	Version             string
	DisplayName         string
	Description         *string
	InstructionPath     string
	ManifestKey         string
	BundleKey           string
	RegistryProvider    *string
	RegistrySlug        *string
	RegistryOwnerHandle *string
	RegistryVersion     *string
	RegistryDetailURL   *string
	RegistryDownloadURL *string
	RegistrySourceKind  *string
	RegistrySourceURL   *string
	ScanStatus          string
	ScanHasWarnings     bool
	ScanCheckedAt       *time.Time
	ScanEngine          *string
	ScanSummary         *string
	ModerationVerdict   *string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type WorkspaceSkillEnablementsRepository struct {
	db Querier
}

func NewWorkspaceSkillEnablementsRepository(db Querier) (*WorkspaceSkillEnablementsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &WorkspaceSkillEnablementsRepository{db: db}, nil
}

func (r *WorkspaceSkillEnablementsRepository) Replace(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, workspaceRef string, enabledByUserID uuid.UUID, items []WorkspaceSkillEnablement) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if tx == nil {
		return fmt.Errorf("tx must not be nil")
	}
	workspaceRef = strings.TrimSpace(workspaceRef)
	if orgID == uuid.Nil || enabledByUserID == uuid.Nil || workspaceRef == "" {
		return fmt.Errorf("workspace enablement is invalid")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM workspace_skill_enablements WHERE org_id = $1 AND workspace_ref = $2`, orgID, workspaceRef); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO workspace_skill_enablements (workspace_ref, org_id, enabled_by_user_id, skill_key, version)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (workspace_ref, skill_key) DO UPDATE
			 SET version = EXCLUDED.version,
			     enabled_by_user_id = EXCLUDED.enabled_by_user_id,
			     updated_at = now()`,
			workspaceRef,
			orgID,
			enabledByUserID,
			strings.TrimSpace(item.SkillKey),
			strings.TrimSpace(item.Version),
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *WorkspaceSkillEnablementsRepository) ListByWorkspace(ctx context.Context, orgID uuid.UUID, workspaceRef string) ([]WorkspaceSkillEnablement, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT wse.workspace_ref, wse.org_id, wse.enabled_by_user_id, wse.skill_key, wse.version, sp.display_name, sp.description,
		        sp.instruction_path, sp.manifest_key, sp.bundle_key, sp.registry_provider, sp.registry_slug,
		        sp.registry_owner_handle, sp.registry_version, sp.registry_detail_url, sp.registry_download_url,
		        sp.registry_source_kind, sp.registry_source_url, sp.scan_status, sp.scan_has_warnings, sp.scan_checked_at,
		        sp.scan_engine, sp.scan_summary, sp.moderation_verdict,
		        wse.created_at, sp.updated_at
		   FROM workspace_skill_enablements wse
		   JOIN skill_packages sp ON sp.org_id = wse.org_id AND sp.skill_key = wse.skill_key AND sp.version = wse.version
		  WHERE wse.org_id = $1 AND wse.workspace_ref = $2
		  ORDER BY wse.skill_key`,
		orgID,
		strings.TrimSpace(workspaceRef),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]WorkspaceSkillEnablement, 0)
	for rows.Next() {
		var item WorkspaceSkillEnablement
		if err := rows.Scan(&item.WorkspaceRef, &item.OrgID, &item.EnabledByUserID, &item.SkillKey, &item.Version, &item.DisplayName, &item.Description, &item.InstructionPath, &item.ManifestKey, &item.BundleKey, &item.RegistryProvider, &item.RegistrySlug, &item.RegistryOwnerHandle, &item.RegistryVersion, &item.RegistryDetailURL, &item.RegistryDownloadURL, &item.RegistrySourceKind, &item.RegistrySourceURL, &item.ScanStatus, &item.ScanHasWarnings, &item.ScanCheckedAt, &item.ScanEngine, &item.ScanSummary, &item.ModerationVerdict, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
