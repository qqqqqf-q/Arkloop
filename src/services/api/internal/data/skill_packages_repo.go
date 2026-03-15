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
	AccountID               uuid.UUID
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

func (r *SkillPackagesRepository) Create(ctx context.Context, accountID uuid.UUID, manifest skillstore.PackageManifest) (SkillPackage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return SkillPackage{}, fmt.Errorf("account_id must not be nil")
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
		    (account_id, skill_key, version, display_name, description, instruction_path, manifest_key, bundle_key, files_prefix, platforms)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING account_id, skill_key, version, display_name, description, instruction_path, manifest_key, bundle_key, files_prefix, platforms,
		           registry_provider, registry_slug, registry_owner_handle, registry_version, registry_detail_url, registry_download_url,
		           registry_source_kind, registry_source_url, scan_status, scan_has_warnings, scan_checked_at, scan_engine,
		           scan_summary, moderation_verdict, scan_snapshot_json, is_active, created_at, updated_at`,
		accountID,
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
		&item.AccountID,
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

func (r *SkillPackagesRepository) UpdateRegistryMetadata(ctx context.Context, accountID uuid.UUID, skillKey, version string, metadata SkillPackageRegistryMetadata) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return fmt.Errorf("account_id must not be nil")
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
		  WHERE account_id = $1 AND skill_key = $2 AND version = $3`,
		accountID,
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

func (r *SkillPackagesRepository) ListActive(ctx context.Context, accountID uuid.UUID) ([]SkillPackage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT account_id, skill_key, version, display_name, description, instruction_path, manifest_key, bundle_key, files_prefix, platforms,
		        registry_provider, registry_slug, registry_owner_handle, registry_version, registry_detail_url, registry_download_url,
		        registry_source_kind, registry_source_url, scan_status, scan_has_warnings, scan_checked_at, scan_engine,
		        scan_summary, moderation_verdict, scan_snapshot_json, is_active, created_at, updated_at
		   FROM skill_packages
		  WHERE account_id = $1
		    AND is_active = TRUE
		  ORDER BY skill_key, version`,
		accountID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SkillPackage, 0)
	for rows.Next() {
		var item SkillPackage
		var scanSnapshotRaw []byte
		if err := rows.Scan(&item.AccountID, &item.SkillKey, &item.Version, &item.DisplayName, &item.Description, &item.InstructionPath, &item.ManifestKey, &item.BundleKey, &item.FilesPrefix, &item.Platforms, &item.RegistryProvider, &item.RegistrySlug, &item.RegistryOwnerHandle, &item.RegistryVersion, &item.RegistryDetailURL, &item.RegistryDownloadURL, &item.RegistrySourceKind, &item.RegistrySourceURL, &item.ScanStatus, &item.ScanHasWarnings, &item.ScanCheckedAt, &item.ScanEngine, &item.ScanSummary, &item.ModerationVerdict, &scanSnapshotRaw, &item.IsActive, &item.CreatedAt, &item.UpdatedAt); err != nil {
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

func (r *SkillPackagesRepository) Get(ctx context.Context, accountID uuid.UUID, skillKey, version string) (*SkillPackage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	skillKey = strings.TrimSpace(skillKey)
	version = strings.TrimSpace(version)
	if accountID == uuid.Nil || skillKey == "" || version == "" {
		return nil, fmt.Errorf("account_id, skill_key and version must not be empty")
	}
	var item SkillPackage
	var scanSnapshotRaw []byte
	err := r.db.QueryRow(
		ctx,
		`SELECT account_id, skill_key, version, display_name, description, instruction_path, manifest_key, bundle_key, files_prefix, platforms,
		        registry_provider, registry_slug, registry_owner_handle, registry_version, registry_detail_url, registry_download_url,
		        registry_source_kind, registry_source_url, scan_status, scan_has_warnings, scan_checked_at, scan_engine,
		        scan_summary, moderation_verdict, scan_snapshot_json, is_active, created_at, updated_at
		   FROM skill_packages
		  WHERE account_id = $1 AND skill_key = $2 AND version = $3`,
		accountID,
		skillKey,
		version,
	).Scan(&item.AccountID, &item.SkillKey, &item.Version, &item.DisplayName, &item.Description, &item.InstructionPath, &item.ManifestKey, &item.BundleKey, &item.FilesPrefix, &item.Platforms, &item.RegistryProvider, &item.RegistrySlug, &item.RegistryOwnerHandle, &item.RegistryVersion, &item.RegistryDetailURL, &item.RegistryDownloadURL, &item.RegistrySourceKind, &item.RegistrySourceURL, &item.ScanStatus, &item.ScanHasWarnings, &item.ScanCheckedAt, &item.ScanEngine, &item.ScanSummary, &item.ModerationVerdict, &scanSnapshotRaw, &item.IsActive, &item.CreatedAt, &item.UpdatedAt)
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

func (r *SkillPackagesRepository) UpsertPlatformSkill(ctx context.Context, manifest skillstore.PackageManifest, contentHash string) (SkillPackage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	normalized, err := skillstore.ValidateManifest(manifest)
	if err != nil {
		return SkillPackage{}, err
	}
	var item SkillPackage
	var scanSnapshotRaw []byte
	var accountID *uuid.UUID
	err = r.db.QueryRow(
		ctx,
		`INSERT INTO skill_packages
		    (account_id, skill_key, version, display_name, description, instruction_path,
		     manifest_key, bundle_key, files_prefix, platforms, sync_mode, content_hash, scan_status)
		 VALUES (NULL, $1, $2, $3, $4, $5, $6, $7, $8, $9, 'platform_skill', $10, 'clean')
		 ON CONFLICT (account_id, skill_key, version) WHERE account_id IS NULL
		 DO UPDATE SET
		    display_name = EXCLUDED.display_name,
		    description = EXCLUDED.description,
		    instruction_path = EXCLUDED.instruction_path,
		    manifest_key = EXCLUDED.manifest_key,
		    bundle_key = EXCLUDED.bundle_key,
		    files_prefix = EXCLUDED.files_prefix,
		    platforms = EXCLUDED.platforms,
		    content_hash = EXCLUDED.content_hash,
		    is_active = true,
		    updated_at = now()
		 RETURNING account_id, skill_key, version, display_name, description, instruction_path, manifest_key, bundle_key, files_prefix, platforms,
		           registry_provider, registry_slug, registry_owner_handle, registry_version, registry_detail_url, registry_download_url,
		           registry_source_kind, registry_source_url, scan_status, scan_has_warnings, scan_checked_at, scan_engine,
		           scan_summary, moderation_verdict, scan_snapshot_json, is_active, created_at, updated_at`,
		normalized.SkillKey,
		normalized.Version,
		normalized.DisplayName,
		normalized.Description,
		normalized.InstructionPath,
		normalized.ManifestKey,
		normalized.BundleKey,
		normalized.FilesPrefix,
		normalized.Platforms,
		contentHash,
	).Scan(
		&accountID,
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
		return SkillPackage{}, err
	}
	if accountID != nil {
		item.AccountID = *accountID
	}
	if len(scanSnapshotRaw) > 0 {
		_ = json.Unmarshal(scanSnapshotRaw, &item.ScanSnapshotJSON)
	}
	return item, nil
}

func (r *SkillPackagesRepository) ListPlatformSkills(ctx context.Context) ([]SkillPackage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT account_id, skill_key, version, display_name, description, instruction_path, manifest_key, bundle_key, files_prefix, platforms,
		        registry_provider, registry_slug, registry_owner_handle, registry_version, registry_detail_url, registry_download_url,
		        registry_source_kind, registry_source_url, scan_status, scan_has_warnings, scan_checked_at, scan_engine,
		        scan_summary, moderation_verdict, scan_snapshot_json, is_active, created_at, updated_at
		   FROM skill_packages
		  WHERE account_id IS NULL
		    AND sync_mode = 'platform_skill'
		  ORDER BY skill_key, version`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SkillPackage, 0)
	for rows.Next() {
		var item SkillPackage
		var scanSnapshotRaw []byte
		var accountID *uuid.UUID
		if err := rows.Scan(&accountID, &item.SkillKey, &item.Version, &item.DisplayName, &item.Description, &item.InstructionPath, &item.ManifestKey, &item.BundleKey, &item.FilesPrefix, &item.Platforms, &item.RegistryProvider, &item.RegistrySlug, &item.RegistryOwnerHandle, &item.RegistryVersion, &item.RegistryDetailURL, &item.RegistryDownloadURL, &item.RegistrySourceKind, &item.RegistrySourceURL, &item.ScanStatus, &item.ScanHasWarnings, &item.ScanCheckedAt, &item.ScanEngine, &item.ScanSummary, &item.ModerationVerdict, &scanSnapshotRaw, &item.IsActive, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if accountID != nil {
			item.AccountID = *accountID
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

func (r *SkillPackagesRepository) DeactivatePlatformSkill(ctx context.Context, skillKey string) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	skillKey = strings.TrimSpace(skillKey)
	if skillKey == "" {
		return 0, fmt.Errorf("skill_key must not be empty")
	}
	tag, err := r.db.Exec(
		ctx,
		`UPDATE skill_packages
		    SET is_active = false, updated_at = now()
		  WHERE account_id IS NULL
		    AND skill_key = $1
		    AND sync_mode = 'platform_skill'`,
		skillKey,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
