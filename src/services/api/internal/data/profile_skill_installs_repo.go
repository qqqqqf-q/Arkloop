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

type ProfileSkillInstall struct {
	ProfileRef          string
	AccountID           uuid.UUID
	OwnerUserID         uuid.UUID
	SkillKey            string
	Version             string
	DisplayName         string
	Description         *string
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
	OwnerPluginID       *string
	OwnerPluginVersion  *string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type ProfileSkillInstallsRepository struct {
	db Querier
}

func NewProfileSkillInstallsRepository(db Querier) (*ProfileSkillInstallsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ProfileSkillInstallsRepository{db: db}, nil
}

func (r *ProfileSkillInstallsRepository) WithTx(tx pgx.Tx) *ProfileSkillInstallsRepository {
	return &ProfileSkillInstallsRepository{db: tx}
}

func (r *ProfileSkillInstallsRepository) Install(ctx context.Context, profileRef string, accountID, ownerUserID uuid.UUID, skillKey, version string) error {
	return r.InstallWithOwnerPlugin(ctx, profileRef, accountID, ownerUserID, skillKey, version, nil, nil)
}

func (r *ProfileSkillInstallsRepository) InstallWithOwnerPlugin(ctx context.Context, profileRef string, accountID, ownerUserID uuid.UUID, skillKey, version string, ownerPluginID, ownerPluginVersion *string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	profileRef = strings.TrimSpace(profileRef)
	skillKey = strings.TrimSpace(skillKey)
	version = strings.TrimSpace(version)
	if profileRef == "" || accountID == uuid.Nil || ownerUserID == uuid.Nil || skillKey == "" || version == "" {
		return fmt.Errorf("install relation is invalid")
	}
	_, err := r.db.Exec(
		ctx,
		`INSERT INTO profile_skill_installs (profile_ref, account_id, owner_user_id, skill_key, version, owner_plugin_id, owner_plugin_version)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (profile_ref, skill_key, version) DO UPDATE SET
		     owner_user_id = EXCLUDED.owner_user_id,
		     owner_plugin_id = EXCLUDED.owner_plugin_id,
		     owner_plugin_version = EXCLUDED.owner_plugin_version,
		     updated_at = now()`,
		profileRef,
		accountID,
		ownerUserID,
		skillKey,
		version,
		nullableStringPtr(ownerPluginID),
		nullableStringPtr(ownerPluginVersion),
	)
	return err
}

func (r *ProfileSkillInstallsRepository) Delete(ctx context.Context, profileRef, skillKey, version string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := r.db.Exec(
		ctx,
		`DELETE FROM profile_skill_installs WHERE profile_ref = $1 AND skill_key = $2 AND version = $3`,
		strings.TrimSpace(profileRef),
		strings.TrimSpace(skillKey),
		strings.TrimSpace(version),
	)
	return err
}

func (r *ProfileSkillInstallsRepository) ListByProfile(ctx context.Context, accountID uuid.UUID, profileRef string) ([]ProfileSkillInstall, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT psi.profile_ref, psi.account_id, psi.owner_user_id, psi.skill_key, psi.version,
		        COALESCE(sp.display_name, psi.skill_key), sp.description,
		        sp.registry_provider, sp.registry_slug, sp.registry_owner_handle, sp.registry_version, sp.registry_detail_url,
		        sp.registry_download_url, sp.registry_source_kind, sp.registry_source_url, COALESCE(sp.scan_status, 'unknown'), COALESCE(sp.scan_has_warnings, FALSE),
		        sp.scan_checked_at, sp.scan_engine, sp.scan_summary, sp.moderation_verdict,
		        psi.owner_plugin_id, psi.owner_plugin_version, psi.created_at, COALESCE(sp.updated_at, psi.updated_at)
		   FROM profile_skill_installs psi
		   LEFT JOIN skill_packages sp ON sp.account_id = psi.account_id AND sp.skill_key = psi.skill_key AND sp.version = psi.version
		  WHERE psi.account_id = $1 AND psi.profile_ref = $2
		  ORDER BY psi.skill_key, psi.version`,
		accountID,
		strings.TrimSpace(profileRef),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ProfileSkillInstall, 0)
	for rows.Next() {
		var item ProfileSkillInstall
		if err := rows.Scan(&item.ProfileRef, &item.AccountID, &item.OwnerUserID, &item.SkillKey, &item.Version, &item.DisplayName, &item.Description, &item.RegistryProvider, &item.RegistrySlug, &item.RegistryOwnerHandle, &item.RegistryVersion, &item.RegistryDetailURL, &item.RegistryDownloadURL, &item.RegistrySourceKind, &item.RegistrySourceURL, &item.ScanStatus, &item.ScanHasWarnings, &item.ScanCheckedAt, &item.ScanEngine, &item.ScanSummary, &item.ModerationVerdict, &item.OwnerPluginID, &item.OwnerPluginVersion, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *ProfileSkillInstallsRepository) IsInstalled(ctx context.Context, accountID uuid.UUID, profileRef, skillKey, version string) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var exists bool
	err := r.db.QueryRow(
		ctx,
		`SELECT EXISTS(
		    SELECT 1 FROM profile_skill_installs WHERE account_id = $1 AND profile_ref = $2 AND skill_key = $3 AND version = $4
		 )`,
		accountID,
		strings.TrimSpace(profileRef),
		strings.TrimSpace(skillKey),
		strings.TrimSpace(version),
	).Scan(&exists)
	return exists, err
}

func (r *ProfileSkillInstallsRepository) DeleteByOwnerPlugin(ctx context.Context, accountID uuid.UUID, ownerPluginID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := r.db.Exec(
		ctx,
		`DELETE FROM profile_skill_installs WHERE account_id = $1 AND owner_plugin_id = $2`,
		accountID,
		strings.TrimSpace(ownerPluginID),
	)
	return err
}

func (r *ProfileSkillInstallsRepository) ListByOwnerPlugin(ctx context.Context, accountID uuid.UUID, ownerPluginID string) ([]ProfileSkillInstall, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT profile_ref, account_id, owner_user_id, skill_key, version,
		        owner_plugin_id, owner_plugin_version, created_at, updated_at
		   FROM profile_skill_installs
		  WHERE account_id = $1 AND owner_plugin_id = $2
		  ORDER BY profile_ref, skill_key, version`,
		accountID,
		strings.TrimSpace(ownerPluginID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ProfileSkillInstall, 0)
	for rows.Next() {
		var item ProfileSkillInstall
		if err := rows.Scan(&item.ProfileRef, &item.AccountID, &item.OwnerUserID, &item.SkillKey, &item.Version, &item.OwnerPluginID, &item.OwnerPluginVersion, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.DisplayName = item.SkillKey
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ProfileSkillInstallsRepository) IsInstalledInAnyWorkspaceForOwner(ctx context.Context, accountID, ownerUserID uuid.UUID, skillKey, version string) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var exists bool
	err := r.db.QueryRow(
		ctx,
		`SELECT EXISTS(
		    SELECT 1
		      FROM workspace_skill_enablements wse
		      JOIN workspace_registries wr ON wr.workspace_ref = wse.workspace_ref
		     WHERE wr.account_id = $1
		       AND wr.owner_user_id = $2
		       AND wse.skill_key = $3
		       AND wse.version = $4
		 )`,
		accountID,
		ownerUserID,
		strings.TrimSpace(skillKey),
		strings.TrimSpace(version),
	).Scan(&exists)
	return exists, err
}

func nullableStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cleaned := strings.TrimSpace(*value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}
