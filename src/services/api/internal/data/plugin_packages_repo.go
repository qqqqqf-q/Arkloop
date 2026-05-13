package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PluginPackage struct {
	ID                 uuid.UUID
	AccountID          uuid.UUID
	PluginID           string
	Version            string
	DisplayName        string
	Description        *string
	ManifestJSON       json.RawMessage
	SettingsSchemaJSON json.RawMessage
	SourceKind         string
	SourceURI          *string
	IsActive           bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type PluginPackageInput struct {
	AccountID          uuid.UUID
	PluginID           string
	Version            string
	DisplayName        string
	Description        *string
	ManifestJSON       json.RawMessage
	SettingsSchemaJSON json.RawMessage
	SourceKind         string
	SourceURI          *string
}

type PluginPackagesRepository struct {
	db Querier
}

func NewPluginPackagesRepository(db Querier) (*PluginPackagesRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &PluginPackagesRepository{db: db}, nil
}

func (r *PluginPackagesRepository) WithTx(tx pgx.Tx) *PluginPackagesRepository {
	return &PluginPackagesRepository{db: tx}
}

func (r *PluginPackagesRepository) Upsert(ctx context.Context, input PluginPackageInput) (PluginPackage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	input.PluginID = strings.TrimSpace(input.PluginID)
	input.Version = strings.TrimSpace(input.Version)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.SourceKind = strings.TrimSpace(input.SourceKind)
	if input.AccountID == uuid.Nil || input.PluginID == "" || input.Version == "" || input.DisplayName == "" {
		return PluginPackage{}, fmt.Errorf("plugin package is invalid")
	}
	if len(input.ManifestJSON) == 0 || !json.Valid(input.ManifestJSON) {
		return PluginPackage{}, fmt.Errorf("manifest_json is invalid")
	}
	if len(input.SettingsSchemaJSON) == 0 {
		input.SettingsSchemaJSON = json.RawMessage("{}")
	}
	if !json.Valid(input.SettingsSchemaJSON) {
		return PluginPackage{}, fmt.Errorf("settings_schema_json is invalid")
	}
	if input.SourceKind == "" {
		input.SourceKind = "manifest"
	}

	var item PluginPackage
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO plugin_packages (
		    account_id, plugin_id, version, display_name, description, manifest_json,
		    settings_schema_json, source_kind, source_uri
		) VALUES (
		    $1, $2, $3, $4, $5, $6, $7, $8, $9
		)
		ON CONFLICT (account_id, plugin_id, version) DO UPDATE
		SET display_name = EXCLUDED.display_name,
		    description = EXCLUDED.description,
		    manifest_json = EXCLUDED.manifest_json,
		    settings_schema_json = EXCLUDED.settings_schema_json,
		    source_kind = EXCLUDED.source_kind,
		    source_uri = EXCLUDED.source_uri,
		    is_active = TRUE,
		    updated_at = now()
		RETURNING id, account_id, plugin_id, version, display_name, description,
		          manifest_json, settings_schema_json, source_kind, source_uri,
		          is_active, created_at, updated_at`,
		input.AccountID,
		input.PluginID,
		input.Version,
		input.DisplayName,
		input.Description,
		input.ManifestJSON,
		input.SettingsSchemaJSON,
		input.SourceKind,
		input.SourceURI,
	).Scan(
		&item.ID,
		&item.AccountID,
		&item.PluginID,
		&item.Version,
		&item.DisplayName,
		&item.Description,
		&item.ManifestJSON,
		&item.SettingsSchemaJSON,
		&item.SourceKind,
		&item.SourceURI,
		&item.IsActive,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (r *PluginPackagesRepository) ListActive(ctx context.Context, accountID uuid.UUID) ([]PluginPackage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT id, account_id, plugin_id, version, display_name, description,
		        manifest_json, settings_schema_json, source_kind, source_uri,
		        is_active, created_at, updated_at
		   FROM plugin_packages
		  WHERE account_id = $1 AND is_active = TRUE
		  ORDER BY plugin_id, version`,
		accountID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]PluginPackage, 0)
	for rows.Next() {
		item, err := scanPluginPackage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PluginPackagesRepository) GetLatestActive(ctx context.Context, accountID uuid.UUID, pluginID string) (*PluginPackage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	pluginID = strings.TrimSpace(pluginID)
	if accountID == uuid.Nil || pluginID == "" {
		return nil, fmt.Errorf("account_id and plugin_id must not be empty")
	}
	var item PluginPackage
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, plugin_id, version, display_name, description,
		        manifest_json, settings_schema_json, source_kind, source_uri,
		        is_active, created_at, updated_at
		   FROM plugin_packages
		  WHERE account_id = $1 AND plugin_id = $2 AND is_active = TRUE
		  ORDER BY updated_at DESC
		  LIMIT 1`,
		accountID,
		pluginID,
	).Scan(
		&item.ID,
		&item.AccountID,
		&item.PluginID,
		&item.Version,
		&item.DisplayName,
		&item.Description,
		&item.ManifestJSON,
		&item.SettingsSchemaJSON,
		&item.SourceKind,
		&item.SourceURI,
		&item.IsActive,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (r *PluginPackagesRepository) DeactivateOtherVersions(ctx context.Context, accountID uuid.UUID, pluginID string, keepPackageID uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	pluginID = strings.TrimSpace(pluginID)
	if accountID == uuid.Nil || pluginID == "" || keepPackageID == uuid.Nil {
		return fmt.Errorf("account_id, plugin_id and keep_package_id must not be empty")
	}
	_, err := r.db.Exec(
		ctx,
		`UPDATE plugin_packages
		    SET is_active = FALSE, updated_at = now()
		  WHERE account_id = $1 AND plugin_id = $2 AND id <> $3`,
		accountID,
		pluginID,
		keepPackageID,
	)
	return err
}

func (r *PluginPackagesRepository) GetByID(ctx context.Context, accountID, packageID uuid.UUID) (*PluginPackage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var item PluginPackage
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, plugin_id, version, display_name, description,
		        manifest_json, settings_schema_json, source_kind, source_uri,
		        is_active, created_at, updated_at
		   FROM plugin_packages
		  WHERE account_id = $1 AND id = $2`,
		accountID,
		packageID,
	).Scan(
		&item.ID,
		&item.AccountID,
		&item.PluginID,
		&item.Version,
		&item.DisplayName,
		&item.Description,
		&item.ManifestJSON,
		&item.SettingsSchemaJSON,
		&item.SourceKind,
		&item.SourceURI,
		&item.IsActive,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (r *PluginPackagesRepository) DeleteByPluginID(ctx context.Context, accountID uuid.UUID, pluginID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := r.db.Exec(
		ctx,
		`DELETE FROM plugin_packages WHERE account_id = $1 AND plugin_id = $2`,
		accountID,
		strings.TrimSpace(pluginID),
	)
	return err
}

type pluginPackageScanner interface {
	Scan(dest ...any) error
}

func scanPluginPackage(row pluginPackageScanner) (PluginPackage, error) {
	var item PluginPackage
	err := row.Scan(
		&item.ID,
		&item.AccountID,
		&item.PluginID,
		&item.Version,
		&item.DisplayName,
		&item.Description,
		&item.ManifestJSON,
		&item.SettingsSchemaJSON,
		&item.SourceKind,
		&item.SourceURI,
		&item.IsActive,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}
