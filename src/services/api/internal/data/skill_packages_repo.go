package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/skillstore"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type SkillPackage struct {
	OrgID               uuid.UUID
	SkillKey            string
	Version             string
	DisplayName         string
	Description         *string
	InstructionPath     string
	ManifestKey         string
	BundleKey           string
	FilesPrefix         string
	Platforms           []string
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
	ScanSnapshotJSON    map[string]any
	IsActive            bool
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type SkillPackageRegistryMetadata struct {
	RegistryProvider    string
	RegistrySlug        string
	RegistryOwnerHandle string
	RegistryVersion     string
	RegistryDetailURL   string
	RegistryDownloadURL string
	RegistrySourceKind  string
	RegistrySourceURL   string
	ScanStatus          string
	ScanHasWarnings     bool
	ScanCheckedAt       *time.Time
	ScanEngine          string
	ScanSummary         string
	ModerationVerdict   string
	ScanSnapshotJSON    map[string]any
}

type SkillPackageConflictError struct {
	SkillKey string
	Version  string
}

func (e SkillPackageConflictError) Error() string {
	return fmt.Sprintf("skill package %q@%q already exists", e.SkillKey, e.Version)
}

type SkillPackagesRepository struct {
	db Querier
}

func NewSkillPackagesRepository(db Querier) (*SkillPackagesRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &SkillPackagesRepository{db: db}, nil
}

func (r *SkillPackagesRepository) Create(ctx context.Context, orgID uuid.UUID, manifest skillstore.PackageManifest) (SkillPackage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return SkillPackage{}, fmt.Errorf("org_id must not be nil")
	}
	normalized, err := skillstore.ValidateManifest(manifest)
	if err != nil {
		return SkillPackage{}, err
	}
	var item SkillPackage
	var scanSnapshotRaw []byte
	err = r.db.QueryRow(
		ctx,
		`INSERT INTO skill_packages
		    (org_id, skill_key, version, display_name, description, instruction_path, manifest_key, bundle_key, files_prefix, platforms)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING org_id, skill_key, version, display_name, description, instruction_path, manifest_key, bundle_key, files_prefix, platforms,
		           registry_provider, registry_slug, registry_owner_handle, registry_version, registry_detail_url, registry_download_url,
		           registry_source_kind, registry_source_url, scan_status, scan_has_warnings, scan_checked_at, scan_engine,
		           scan_summary, moderation_verdict, scan_snapshot_json, is_active, created_at, updated_at`,
		orgID,
		normalized.SkillKey,
		normalized.Version,
		normalized.DisplayName,
		normalized.Description,
		normalized.InstructionPath,
		normalized.ManifestKey,
		normalized.BundleKey,
		normalized.FilesPrefix,
		normalized.Platforms,
	).Scan(
		&item.OrgID,
		&item.SkillKey,
		&item.Version,
		&item.DisplayName,
		&item.Description,
		&item.InstructionPath,
		&item.ManifestKey,
		&item.BundleKey,
		&item.FilesPrefix,
		&item.Platforms,
		&item.RegistryProvider,
		&item.RegistrySlug,
		&item.RegistryOwnerHandle,
		&item.RegistryVersion,
		&item.RegistryDetailURL,
		&item.RegistryDownloadURL,
		&item.RegistrySourceKind,
		&item.RegistrySourceURL,
		&item.ScanStatus,
		&item.ScanHasWarnings,
		&item.ScanCheckedAt,
		&item.ScanEngine,
		&item.ScanSummary,
		&item.ModerationVerdict,
		&scanSnapshotRaw,
		&item.IsActive,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return SkillPackage{}, SkillPackageConflictError{SkillKey: normalized.SkillKey, Version: normalized.Version}
		}
		return SkillPackage{}, err
	}
	if len(scanSnapshotRaw) > 0 {
		_ = json.Unmarshal(scanSnapshotRaw, &item.ScanSnapshotJSON)
	}
	return item, nil
}

func (r *SkillPackagesRepository) UpdateRegistryMetadata(ctx context.Context, orgID uuid.UUID, skillKey, version string, metadata SkillPackageRegistryMetadata) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return fmt.Errorf("org_id must not be nil")
	}
	skillKey = strings.TrimSpace(skillKey)
	version = strings.TrimSpace(version)
	if skillKey == "" || version == "" {
		return fmt.Errorf("skill_key and version must not be empty")
	}
	scanStatus := strings.TrimSpace(metadata.ScanStatus)
	if scanStatus == "" {
		scanStatus = "unknown"
	}
	snapshot := metadata.ScanSnapshotJSON
	if snapshot == nil {
		snapshot = map[string]any{}
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(
		ctx,
		`UPDATE skill_packages
		    SET registry_provider = NULLIF($4, ''),
		        registry_slug = NULLIF($5, ''),
		        registry_owner_handle = NULLIF($6, ''),
		        registry_version = NULLIF($7, ''),
		        registry_detail_url = NULLIF($8, ''),
		        registry_download_url = NULLIF($9, ''),
		        registry_source_kind = NULLIF($10, ''),
		        registry_source_url = NULLIF($11, ''),
		        scan_status = $12,
		        scan_has_warnings = $13,
		        scan_checked_at = $14,
		        scan_engine = NULLIF($15, ''),
		        scan_summary = NULLIF($16, ''),
		        moderation_verdict = NULLIF($17, ''),
		        scan_snapshot_json = $18::jsonb,
		        updated_at = now()
		  WHERE org_id = $1 AND skill_key = $2 AND version = $3`,
		orgID,
		skillKey,
		version,
		strings.TrimSpace(metadata.RegistryProvider),
		strings.TrimSpace(metadata.RegistrySlug),
		strings.TrimSpace(metadata.RegistryOwnerHandle),
		strings.TrimSpace(metadata.RegistryVersion),
		strings.TrimSpace(metadata.RegistryDetailURL),
		strings.TrimSpace(metadata.RegistryDownloadURL),
		strings.TrimSpace(metadata.RegistrySourceKind),
		strings.TrimSpace(metadata.RegistrySourceURL),
		scanStatus,
		metadata.ScanHasWarnings,
		metadata.ScanCheckedAt,
		strings.TrimSpace(metadata.ScanEngine),
		strings.TrimSpace(metadata.ScanSummary),
		strings.TrimSpace(metadata.ModerationVerdict),
		payload,
	)
	return err
}

func (r *SkillPackagesRepository) ListActive(ctx context.Context, orgID uuid.UUID) ([]SkillPackage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT org_id, skill_key, version, display_name, description, instruction_path, manifest_key, bundle_key, files_prefix, platforms,
		        registry_provider, registry_slug, registry_owner_handle, registry_version, registry_detail_url, registry_download_url,
		        registry_source_kind, registry_source_url, scan_status, scan_has_warnings, scan_checked_at, scan_engine,
		        scan_summary, moderation_verdict, scan_snapshot_json, is_active, created_at, updated_at
		   FROM skill_packages
		  WHERE org_id = $1
		    AND is_active = TRUE
		  ORDER BY skill_key, version`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SkillPackage, 0)
	for rows.Next() {
		var item SkillPackage
		var scanSnapshotRaw []byte
		if err := rows.Scan(&item.OrgID, &item.SkillKey, &item.Version, &item.DisplayName, &item.Description, &item.InstructionPath, &item.ManifestKey, &item.BundleKey, &item.FilesPrefix, &item.Platforms, &item.RegistryProvider, &item.RegistrySlug, &item.RegistryOwnerHandle, &item.RegistryVersion, &item.RegistryDetailURL, &item.RegistryDownloadURL, &item.RegistrySourceKind, &item.RegistrySourceURL, &item.ScanStatus, &item.ScanHasWarnings, &item.ScanCheckedAt, &item.ScanEngine, &item.ScanSummary, &item.ModerationVerdict, &scanSnapshotRaw, &item.IsActive, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if len(scanSnapshotRaw) > 0 {
			_ = json.Unmarshal(scanSnapshotRaw, &item.ScanSnapshotJSON)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *SkillPackagesRepository) Get(ctx context.Context, orgID uuid.UUID, skillKey, version string) (*SkillPackage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	skillKey = strings.TrimSpace(skillKey)
	version = strings.TrimSpace(version)
	if orgID == uuid.Nil || skillKey == "" || version == "" {
		return nil, fmt.Errorf("org_id, skill_key and version must not be empty")
	}
	var item SkillPackage
	var scanSnapshotRaw []byte
	err := r.db.QueryRow(
		ctx,
		`SELECT org_id, skill_key, version, display_name, description, instruction_path, manifest_key, bundle_key, files_prefix, platforms,
		        registry_provider, registry_slug, registry_owner_handle, registry_version, registry_detail_url, registry_download_url,
		        registry_source_kind, registry_source_url, scan_status, scan_has_warnings, scan_checked_at, scan_engine,
		        scan_summary, moderation_verdict, scan_snapshot_json, is_active, created_at, updated_at
		   FROM skill_packages
		  WHERE org_id = $1 AND skill_key = $2 AND version = $3`,
		orgID,
		skillKey,
		version,
	).Scan(&item.OrgID, &item.SkillKey, &item.Version, &item.DisplayName, &item.Description, &item.InstructionPath, &item.ManifestKey, &item.BundleKey, &item.FilesPrefix, &item.Platforms, &item.RegistryProvider, &item.RegistrySlug, &item.RegistryOwnerHandle, &item.RegistryVersion, &item.RegistryDetailURL, &item.RegistryDownloadURL, &item.RegistrySourceKind, &item.RegistrySourceURL, &item.ScanStatus, &item.ScanHasWarnings, &item.ScanCheckedAt, &item.ScanEngine, &item.ScanSummary, &item.ModerationVerdict, &scanSnapshotRaw, &item.IsActive, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if len(scanSnapshotRaw) > 0 {
		_ = json.Unmarshal(scanSnapshotRaw, &item.ScanSnapshotJSON)
	}
	return &item, nil
}
