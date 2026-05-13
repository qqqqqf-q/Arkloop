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

type PluginEnablement struct {
	ID              uuid.UUID
	AccountID       uuid.UUID
	PackageID       uuid.UUID
	PluginID        string
	PluginVersion   string
	ProfileRef      string
	WorkspaceRef    string
	Enabled         bool
	EnabledByUserID uuid.UUID
	SettingsJSON    json.RawMessage
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type PluginRuntimeState struct {
	AccountID     uuid.UUID
	PackageID     uuid.UUID
	PluginID      string
	PluginVersion string
	ProfileRef    string
	WorkspaceRef  string
	Status        string
	StatusJSON    json.RawMessage
	UpdatedAt     time.Time
}

type PluginEnablementsRepository struct {
	db Querier
}

func NewPluginEnablementsRepository(db Querier) (*PluginEnablementsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &PluginEnablementsRepository{db: db}, nil
}

func (r *PluginEnablementsRepository) WithTx(tx pgx.Tx) *PluginEnablementsRepository {
	return &PluginEnablementsRepository{db: tx}
}

func (r *PluginEnablementsRepository) Upsert(ctx context.Context, item PluginEnablement) (PluginEnablement, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	item.PluginID = strings.TrimSpace(item.PluginID)
	item.PluginVersion = strings.TrimSpace(item.PluginVersion)
	item.ProfileRef = strings.TrimSpace(item.ProfileRef)
	item.WorkspaceRef = strings.TrimSpace(item.WorkspaceRef)
	if item.AccountID == uuid.Nil || item.PackageID == uuid.Nil || item.EnabledByUserID == uuid.Nil || item.PluginID == "" || item.PluginVersion == "" || item.ProfileRef == "" || item.WorkspaceRef == "" {
		return PluginEnablement{}, fmt.Errorf("plugin enablement is invalid")
	}
	if len(item.SettingsJSON) == 0 {
		item.SettingsJSON = json.RawMessage("{}")
	}
	if !json.Valid(item.SettingsJSON) {
		return PluginEnablement{}, fmt.Errorf("settings_json is invalid")
	}
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO plugin_enablements (
		    account_id, package_id, plugin_id, plugin_version, profile_ref,
		    workspace_ref, desired_enabled, enabled_by_user_id, settings_json
		) VALUES (
		    $1, $2, $3, $4, $5,
		    $6, $7, $8, $9
		)
		ON CONFLICT (account_id, package_id, profile_ref, workspace_ref) DO UPDATE
		SET desired_enabled = EXCLUDED.desired_enabled,
		    enabled_by_user_id = EXCLUDED.enabled_by_user_id,
		    settings_json = EXCLUDED.settings_json,
		    updated_at = now()
		RETURNING id, account_id, package_id, plugin_id, plugin_version, profile_ref,
		          workspace_ref, desired_enabled, enabled_by_user_id, settings_json, created_at, updated_at`,
		item.AccountID,
		item.PackageID,
		item.PluginID,
		item.PluginVersion,
		item.ProfileRef,
		item.WorkspaceRef,
		item.Enabled,
		item.EnabledByUserID,
		item.SettingsJSON,
	).Scan(
		&item.ID,
		&item.AccountID,
		&item.PackageID,
		&item.PluginID,
		&item.PluginVersion,
		&item.ProfileRef,
		&item.WorkspaceRef,
		&item.Enabled,
		&item.EnabledByUserID,
		&item.SettingsJSON,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (r *PluginEnablementsRepository) Get(ctx context.Context, accountID, packageID uuid.UUID, profileRef, workspaceRef string) (*PluginEnablement, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var item PluginEnablement
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, package_id, plugin_id, plugin_version, profile_ref,
		        workspace_ref, desired_enabled, enabled_by_user_id, settings_json, created_at, updated_at
		   FROM plugin_enablements
		  WHERE account_id = $1 AND package_id = $2 AND profile_ref = $3 AND workspace_ref = $4`,
		accountID,
		packageID,
		strings.TrimSpace(profileRef),
		strings.TrimSpace(workspaceRef),
	).Scan(
		&item.ID,
		&item.AccountID,
		&item.PackageID,
		&item.PluginID,
		&item.PluginVersion,
		&item.ProfileRef,
		&item.WorkspaceRef,
		&item.Enabled,
		&item.EnabledByUserID,
		&item.SettingsJSON,
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

func (r *PluginEnablementsRepository) ListByPlugin(ctx context.Context, accountID uuid.UUID, pluginID string) ([]PluginEnablement, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT id, account_id, package_id, plugin_id, plugin_version, profile_ref,
		        workspace_ref, desired_enabled, enabled_by_user_id, settings_json, created_at, updated_at
		   FROM plugin_enablements
		  WHERE account_id = $1 AND plugin_id = $2
		  ORDER BY workspace_ref, profile_ref`,
		accountID,
		strings.TrimSpace(pluginID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]PluginEnablement, 0)
	for rows.Next() {
		var item PluginEnablement
		if err := rows.Scan(&item.ID, &item.AccountID, &item.PackageID, &item.PluginID, &item.PluginVersion, &item.ProfileRef, &item.WorkspaceRef, &item.Enabled, &item.EnabledByUserID, &item.SettingsJSON, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PluginEnablementsRepository) DeleteByPlugin(ctx context.Context, accountID uuid.UUID, pluginID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := r.db.Exec(ctx, `DELETE FROM plugin_enablements WHERE account_id = $1 AND plugin_id = $2`, accountID, strings.TrimSpace(pluginID))
	return err
}

func (r *PluginEnablementsRepository) DeleteOtherPackagesForPlugin(ctx context.Context, accountID uuid.UUID, pluginID string, keepPackageID uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	pluginID = strings.TrimSpace(pluginID)
	if accountID == uuid.Nil || pluginID == "" || keepPackageID == uuid.Nil {
		return fmt.Errorf("account_id, plugin_id and keep_package_id must not be empty")
	}
	_, err := r.db.Exec(
		ctx,
		`DELETE FROM plugin_enablements
		  WHERE account_id = $1 AND plugin_id = $2 AND package_id <> $3`,
		accountID,
		pluginID,
		keepPackageID,
	)
	return err
}

type PluginRuntimeStateRepository struct {
	db Querier
}

func NewPluginRuntimeStateRepository(db Querier) (*PluginRuntimeStateRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &PluginRuntimeStateRepository{db: db}, nil
}

func (r *PluginRuntimeStateRepository) WithTx(tx pgx.Tx) *PluginRuntimeStateRepository {
	return &PluginRuntimeStateRepository{db: tx}
}

func (r *PluginRuntimeStateRepository) Upsert(ctx context.Context, item PluginRuntimeState) (PluginRuntimeState, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	item.PluginID = strings.TrimSpace(item.PluginID)
	item.PluginVersion = strings.TrimSpace(item.PluginVersion)
	item.ProfileRef = strings.TrimSpace(item.ProfileRef)
	item.WorkspaceRef = strings.TrimSpace(item.WorkspaceRef)
	item.Status = strings.TrimSpace(item.Status)
	if item.Status == "" {
		item.Status = "not_installed"
	}
	if len(item.StatusJSON) == 0 {
		item.StatusJSON = json.RawMessage("{}")
	}
	if item.AccountID == uuid.Nil || item.PackageID == uuid.Nil || item.PluginID == "" || item.PluginVersion == "" || item.ProfileRef == "" || item.WorkspaceRef == "" || !json.Valid(item.StatusJSON) {
		return PluginRuntimeState{}, fmt.Errorf("plugin runtime state is invalid")
	}
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO plugin_runtime_state (
		    account_id, package_id, plugin_id, plugin_version, profile_ref,
		    workspace_ref, status, status_json
		) VALUES (
		    $1, $2, $3, $4, $5,
		    $6, $7, $8
		)
		ON CONFLICT (account_id, package_id, profile_ref, workspace_ref) DO UPDATE
		SET status = EXCLUDED.status,
		    status_json = EXCLUDED.status_json,
		    updated_at = now()
		RETURNING account_id, package_id, plugin_id, plugin_version, profile_ref,
		          workspace_ref, status, status_json, updated_at`,
		item.AccountID,
		item.PackageID,
		item.PluginID,
		item.PluginVersion,
		item.ProfileRef,
		item.WorkspaceRef,
		item.Status,
		item.StatusJSON,
	).Scan(
		&item.AccountID,
		&item.PackageID,
		&item.PluginID,
		&item.PluginVersion,
		&item.ProfileRef,
		&item.WorkspaceRef,
		&item.Status,
		&item.StatusJSON,
		&item.UpdatedAt,
	)
	return item, err
}

func (r *PluginRuntimeStateRepository) Get(ctx context.Context, accountID, packageID uuid.UUID, profileRef, workspaceRef string) (*PluginRuntimeState, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var item PluginRuntimeState
	err := r.db.QueryRow(
		ctx,
		`SELECT account_id, package_id, plugin_id, plugin_version, profile_ref,
		        workspace_ref, status, status_json, updated_at
		   FROM plugin_runtime_state
		  WHERE account_id = $1 AND package_id = $2 AND profile_ref = $3 AND workspace_ref = $4`,
		accountID,
		packageID,
		strings.TrimSpace(profileRef),
		strings.TrimSpace(workspaceRef),
	).Scan(&item.AccountID, &item.PackageID, &item.PluginID, &item.PluginVersion, &item.ProfileRef, &item.WorkspaceRef, &item.Status, &item.StatusJSON, &item.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (r *PluginRuntimeStateRepository) ListByPlugin(ctx context.Context, accountID uuid.UUID, pluginID string) ([]PluginRuntimeState, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	pluginID = strings.TrimSpace(pluginID)
	if accountID == uuid.Nil || pluginID == "" {
		return nil, fmt.Errorf("account_id and plugin_id must not be empty")
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT account_id, package_id, plugin_id, plugin_version, profile_ref,
		        workspace_ref, status, status_json, updated_at
		   FROM plugin_runtime_state
		  WHERE account_id = $1 AND plugin_id = $2
		  ORDER BY updated_at DESC`,
		accountID,
		pluginID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []PluginRuntimeState
	for rows.Next() {
		var item PluginRuntimeState
		if err := rows.Scan(&item.AccountID, &item.PackageID, &item.PluginID, &item.PluginVersion, &item.ProfileRef, &item.WorkspaceRef, &item.Status, &item.StatusJSON, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PluginRuntimeStateRepository) DeleteOtherPackagesForPlugin(ctx context.Context, accountID uuid.UUID, pluginID string, keepPackageID uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	pluginID = strings.TrimSpace(pluginID)
	if accountID == uuid.Nil || pluginID == "" || keepPackageID == uuid.Nil {
		return fmt.Errorf("account_id, plugin_id and keep_package_id must not be empty")
	}
	_, err := r.db.Exec(
		ctx,
		`DELETE FROM plugin_runtime_state
		  WHERE account_id = $1 AND plugin_id = $2 AND package_id <> $3`,
		accountID,
		pluginID,
		keepPackageID,
	)
	return err
}
