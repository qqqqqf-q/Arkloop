package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ProfileSkillInstall struct {
	ProfileRef          string
	OrgID               uuid.UUID
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

func (r *ProfileSkillInstallsRepository) Install(ctx context.Context, profileRef string, orgID, ownerUserID uuid.UUID, skillKey, version string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	profileRef = strings.TrimSpace(profileRef)
	skillKey = strings.TrimSpace(skillKey)
	version = strings.TrimSpace(version)
	if profileRef == "" || orgID == uuid.Nil || ownerUserID == uuid.Nil || skillKey == "" || version == "" {
		return fmt.Errorf("install relation is invalid")
	}
	_, err := r.db.Exec(
		ctx,
		`INSERT INTO profile_skill_installs (profile_ref, org_id, owner_user_id, skill_key, version)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (profile_ref, skill_key, version) DO UPDATE SET updated_at = now()`,
		profileRef,
		orgID,
		ownerUserID,
		skillKey,
		version,
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

func (r *ProfileSkillInstallsRepository) ListByProfile(ctx context.Context, orgID uuid.UUID, profileRef string) ([]ProfileSkillInstall, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT psi.profile_ref, psi.org_id, psi.owner_user_id, psi.skill_key, psi.version, sp.display_name, sp.description,
		        sp.registry_provider, sp.registry_slug, sp.registry_owner_handle, sp.registry_version, sp.registry_detail_url,
		        sp.registry_download_url, sp.registry_source_kind, sp.registry_source_url, sp.scan_status, sp.scan_has_warnings,
		        sp.scan_checked_at, sp.scan_engine, sp.scan_summary, sp.moderation_verdict, psi.created_at, sp.updated_at
		   FROM profile_skill_installs psi
		   JOIN skill_packages sp ON sp.org_id = psi.org_id AND sp.skill_key = psi.skill_key AND sp.version = psi.version
		  WHERE psi.org_id = $1 AND psi.profile_ref = $2
		  ORDER BY psi.skill_key, psi.version`,
		orgID,
		strings.TrimSpace(profileRef),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ProfileSkillInstall, 0)
	for rows.Next() {
		var item ProfileSkillInstall
		if err := rows.Scan(&item.ProfileRef, &item.OrgID, &item.OwnerUserID, &item.SkillKey, &item.Version, &item.DisplayName, &item.Description, &item.RegistryProvider, &item.RegistrySlug, &item.RegistryOwnerHandle, &item.RegistryVersion, &item.RegistryDetailURL, &item.RegistryDownloadURL, &item.RegistrySourceKind, &item.RegistrySourceURL, &item.ScanStatus, &item.ScanHasWarnings, &item.ScanCheckedAt, &item.ScanEngine, &item.ScanSummary, &item.ModerationVerdict, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *ProfileSkillInstallsRepository) IsInstalled(ctx context.Context, orgID uuid.UUID, profileRef, skillKey, version string) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var exists bool
	err := r.db.QueryRow(
		ctx,
		`SELECT EXISTS(
		    SELECT 1 FROM profile_skill_installs WHERE org_id = $1 AND profile_ref = $2 AND skill_key = $3 AND version = $4
		 )`,
		orgID,
		strings.TrimSpace(profileRef),
		strings.TrimSpace(skillKey),
		strings.TrimSpace(version),
	).Scan(&exists)
	return exists, err
}

func (r *ProfileSkillInstallsRepository) IsInstalledInAnyWorkspaceForOwner(ctx context.Context, orgID, ownerUserID uuid.UUID, skillKey, version string) (bool, error) {
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
		     WHERE wr.org_id = $1
		       AND wr.owner_user_id = $2
		       AND wse.skill_key = $3
		       AND wse.version = $4
		 )`,
		orgID,
		ownerUserID,
		strings.TrimSpace(skillKey),
		strings.TrimSpace(version),
	).Scan(&exists)
	return exists, err
}
