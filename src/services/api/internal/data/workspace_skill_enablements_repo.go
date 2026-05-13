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
	AccountID           uuid.UUID
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

func (r *WorkspaceSkillEnablementsRepository) WithTx(tx pgx.Tx) *WorkspaceSkillEnablementsRepository {
	return &WorkspaceSkillEnablementsRepository{db: tx}
}

func (r *WorkspaceSkillEnablementsRepository) Replace(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, workspaceRef string, enabledByUserID uuid.UUID, items []WorkspaceSkillEnablement) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if tx == nil {
		return fmt.Errorf("tx must not be nil")
	}
	workspaceRef = strings.TrimSpace(workspaceRef)
	if accountID == uuid.Nil || enabledByUserID == uuid.Nil || workspaceRef == "" {
		return fmt.Errorf("workspace enablement is invalid")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM workspace_skill_enablements WHERE account_id = $1 AND workspace_ref = $2`, accountID, workspaceRef); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO workspace_skill_enablements (workspace_ref, account_id, enabled_by_user_id, skill_key, version)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (workspace_ref, skill_key) DO UPDATE
			 SET version = EXCLUDED.version,
			     enabled_by_user_id = EXCLUDED.enabled_by_user_id,
			     updated_at = now()`,
			workspaceRef,
			accountID,
			enabledByUserID,
			strings.TrimSpace(item.SkillKey),
			strings.TrimSpace(item.Version),
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *WorkspaceSkillEnablementsRepository) Set(ctx context.Context, accountID uuid.UUID, workspaceRef string, enabledByUserID uuid.UUID, skillKey, version string, enabled bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	workspaceRef = strings.TrimSpace(workspaceRef)
	skillKey = strings.TrimSpace(skillKey)
	version = strings.TrimSpace(version)
	if accountID == uuid.Nil || enabledByUserID == uuid.Nil || workspaceRef == "" || skillKey == "" || version == "" {
		return fmt.Errorf("workspace skill enablement is invalid")
	}
	if !enabled {
		_, err := r.db.Exec(
			ctx,
			`DELETE FROM workspace_skill_enablements
			  WHERE account_id = $1 AND workspace_ref = $2 AND skill_key = $3 AND version = $4`,
			accountID,
			workspaceRef,
			skillKey,
			version,
		)
		return err
	}
	_, err := r.db.Exec(
		ctx,
		`INSERT INTO workspace_skill_enablements (workspace_ref, account_id, enabled_by_user_id, skill_key, version)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (workspace_ref, skill_key) DO UPDATE
		 SET version = EXCLUDED.version,
		     enabled_by_user_id = EXCLUDED.enabled_by_user_id,
		     updated_at = now()`,
		workspaceRef,
		accountID,
		enabledByUserID,
		skillKey,
		version,
	)
	return err
}

func (r *WorkspaceSkillEnablementsRepository) DeleteSkill(ctx context.Context, accountID uuid.UUID, skillKey, version string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := r.db.Exec(
		ctx,
		`DELETE FROM workspace_skill_enablements
		  WHERE account_id = $1 AND skill_key = $2 AND version = $3`,
		accountID,
		strings.TrimSpace(skillKey),
		strings.TrimSpace(version),
	)
	return err
}

func (r *WorkspaceSkillEnablementsRepository) ListByWorkspace(ctx context.Context, accountID uuid.UUID, workspaceRef string) ([]WorkspaceSkillEnablement, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT wse.workspace_ref, wse.account_id, wse.enabled_by_user_id, wse.skill_key, wse.version,
		        COALESCE(sp.display_name, wse.skill_key), sp.description,
		        COALESCE(sp.instruction_path, ''), COALESCE(sp.manifest_key, ''), COALESCE(sp.bundle_key, ''), sp.registry_provider, sp.registry_slug,
		        sp.registry_owner_handle, sp.registry_version, sp.registry_detail_url, sp.registry_download_url,
		        sp.registry_source_kind, sp.registry_source_url, COALESCE(sp.scan_status, 'unknown'), COALESCE(sp.scan_has_warnings, FALSE), sp.scan_checked_at,
		        sp.scan_engine, sp.scan_summary, sp.moderation_verdict,
		        wse.created_at, COALESCE(sp.updated_at, wse.updated_at)
		   FROM workspace_skill_enablements wse
		   LEFT JOIN skill_packages sp ON sp.account_id = wse.account_id AND sp.skill_key = wse.skill_key AND sp.version = wse.version
		  WHERE wse.account_id = $1 AND wse.workspace_ref = $2
		  ORDER BY wse.skill_key`,
		accountID,
		strings.TrimSpace(workspaceRef),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]WorkspaceSkillEnablement, 0)
	for rows.Next() {
		var item WorkspaceSkillEnablement
		if err := rows.Scan(&item.WorkspaceRef, &item.AccountID, &item.EnabledByUserID, &item.SkillKey, &item.Version, &item.DisplayName, &item.Description, &item.InstructionPath, &item.ManifestKey, &item.BundleKey, &item.RegistryProvider, &item.RegistrySlug, &item.RegistryOwnerHandle, &item.RegistryVersion, &item.RegistryDetailURL, &item.RegistryDownloadURL, &item.RegistrySourceKind, &item.RegistrySourceURL, &item.ScanStatus, &item.ScanHasWarnings, &item.ScanCheckedAt, &item.ScanEngine, &item.ScanSummary, &item.ModerationVerdict, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
