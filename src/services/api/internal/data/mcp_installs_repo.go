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

const (
	MCPSourceKindManualConsole  = "manual_console"
	MCPSourceKindSetupSeed      = "setup_seed"
	MCPSourceKindDesktopFile    = "desktop_file"
	MCPSourceKindProjectImport  = "project_import"
	MCPSourceKindExternalImport = "external_import"

	MCPSyncModeNone                     = "none"
	MCPSyncModeDesktopFileBidirectional = "desktop_file_bidirectional"

	MCPHostRequirementDesktopLocal   = "desktop_local"
	MCPHostRequirementDesktopSidecar = "desktop_sidecar"
	MCPHostRequirementCloudWorker    = "cloud_worker"
	MCPHostRequirementRemoteHTTP     = "remote_http"

	MCPDiscoveryStatusConfigured      = "configured"
	MCPDiscoveryStatusNeedsCheck      = "needs_check"
	MCPDiscoveryStatusReady           = "ready"
	MCPDiscoveryStatusInstallMissing  = "install_missing"
	MCPDiscoveryStatusAuthInvalid     = "auth_invalid"
	MCPDiscoveryStatusConnectFailed   = "connect_failed"
	MCPDiscoveryStatusDiscoveredEmpty = "discovered_empty"
	MCPDiscoveryStatusProtocolError   = "protocol_error"
)

type ProfileMCPInstall struct {
	ID                  uuid.UUID
	AccountID           uuid.UUID
	ProfileRef          string
	InstallKey          string
	DisplayName         string
	SourceKind          string
	SourceURI           *string
	SyncMode            string
	Transport           string
	LaunchSpecJSON      json.RawMessage
	AuthHeadersSecretID *uuid.UUID
	HostRequirement     string
	DiscoveryStatus     string
	LastErrorCode       *string
	LastErrorMessage    *string
	LastCheckedAt       *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type MCPInstallPatch struct {
	DisplayName         *string
	SourceKind          *string
	SourceURI           *string
	SyncMode            *string
	Transport           *string
	LaunchSpecJSON      *json.RawMessage
	AuthHeadersSecretID *uuid.UUID
	ClearAuthHeaders    bool
	HostRequirement     *string
	DiscoveryStatus     *string
	LastErrorCode       *string
	LastErrorMessage    *string
	LastCheckedAt       *time.Time
}

type ProfileMCPInstallPatch = MCPInstallPatch

type ProfileMCPInstallsRepository struct {
	db Querier
}

func NewProfileMCPInstallsRepository(db Querier) (*ProfileMCPInstallsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ProfileMCPInstallsRepository{db: db}, nil
}

func (r *ProfileMCPInstallsRepository) WithTx(tx pgx.Tx) *ProfileMCPInstallsRepository {
	return &ProfileMCPInstallsRepository{db: tx}
}

func (r *ProfileMCPInstallsRepository) Create(ctx context.Context, install ProfileMCPInstall) (ProfileMCPInstall, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	install.ProfileRef = strings.TrimSpace(install.ProfileRef)
	install.InstallKey = strings.TrimSpace(install.InstallKey)
	install.DisplayName = strings.TrimSpace(install.DisplayName)
	install.SourceKind = strings.TrimSpace(install.SourceKind)
	install.SyncMode = strings.TrimSpace(install.SyncMode)
	install.Transport = strings.TrimSpace(install.Transport)
	install.HostRequirement = strings.TrimSpace(install.HostRequirement)
	install.DiscoveryStatus = strings.TrimSpace(install.DiscoveryStatus)
	if install.AccountID == uuid.Nil || install.ProfileRef == "" || install.InstallKey == "" || install.DisplayName == "" {
		return ProfileMCPInstall{}, fmt.Errorf("profile mcp install is invalid")
	}
	if len(install.LaunchSpecJSON) == 0 {
		install.LaunchSpecJSON = json.RawMessage("{}")
	}
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO profile_mcp_installs (
		    account_id, profile_ref, install_key, display_name, source_kind, source_uri,
		    sync_mode, transport, launch_spec_json, auth_headers_secret_id, host_requirement,
		    discovery_status, last_error_code, last_error_message, last_checked_at
		) VALUES (
		    $1, $2, $3, $4, $5, $6,
		    $7, $8, $9, $10, $11,
		    $12, $13, $14, $15
		)
		RETURNING id, account_id, profile_ref, install_key, display_name, source_kind, source_uri,
		          sync_mode, transport, launch_spec_json, auth_headers_secret_id, host_requirement,
		          discovery_status, last_error_code, last_error_message, last_checked_at, created_at, updated_at`,
		install.AccountID, install.ProfileRef, install.InstallKey, install.DisplayName, install.SourceKind, install.SourceURI,
		install.SyncMode, install.Transport, install.LaunchSpecJSON, install.AuthHeadersSecretID, install.HostRequirement,
		install.DiscoveryStatus, install.LastErrorCode, install.LastErrorMessage, install.LastCheckedAt,
	).Scan(
		&install.ID, &install.AccountID, &install.ProfileRef, &install.InstallKey, &install.DisplayName, &install.SourceKind, &install.SourceURI,
		&install.SyncMode, &install.Transport, &install.LaunchSpecJSON, &install.AuthHeadersSecretID, &install.HostRequirement,
		&install.DiscoveryStatus, &install.LastErrorCode, &install.LastErrorMessage, &install.LastCheckedAt, &install.CreatedAt, &install.UpdatedAt,
	)
	return install, err
}

func (r *ProfileMCPInstallsRepository) ListByProfile(ctx context.Context, accountID uuid.UUID, profileRef string) ([]ProfileMCPInstall, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT id, account_id, profile_ref, install_key, display_name, source_kind, source_uri,
		        sync_mode, transport, launch_spec_json, auth_headers_secret_id, host_requirement,
		        discovery_status, last_error_code, last_error_message, last_checked_at, created_at, updated_at
		   FROM profile_mcp_installs
		  WHERE account_id = $1 AND profile_ref = $2
		  ORDER BY display_name, install_key`,
		accountID,
		strings.TrimSpace(profileRef),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ProfileMCPInstall{}
	for rows.Next() {
		var item ProfileMCPInstall
		if err := rows.Scan(
			&item.ID, &item.AccountID, &item.ProfileRef, &item.InstallKey, &item.DisplayName, &item.SourceKind, &item.SourceURI,
			&item.SyncMode, &item.Transport, &item.LaunchSpecJSON, &item.AuthHeadersSecretID, &item.HostRequirement,
			&item.DiscoveryStatus, &item.LastErrorCode, &item.LastErrorMessage, &item.LastCheckedAt, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ProfileMCPInstallsRepository) GetByID(ctx context.Context, accountID, id uuid.UUID) (*ProfileMCPInstall, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var item ProfileMCPInstall
	err := r.db.QueryRow(
		ctx,
		`SELECT id, account_id, profile_ref, install_key, display_name, source_kind, source_uri,
		        sync_mode, transport, launch_spec_json, auth_headers_secret_id, host_requirement,
		        discovery_status, last_error_code, last_error_message, last_checked_at, created_at, updated_at
		   FROM profile_mcp_installs
		  WHERE account_id = $1 AND id = $2`,
		accountID, id,
	).Scan(
		&item.ID, &item.AccountID, &item.ProfileRef, &item.InstallKey, &item.DisplayName, &item.SourceKind, &item.SourceURI,
		&item.SyncMode, &item.Transport, &item.LaunchSpecJSON, &item.AuthHeadersSecretID, &item.HostRequirement,
		&item.DiscoveryStatus, &item.LastErrorCode, &item.LastErrorMessage, &item.LastCheckedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (r *ProfileMCPInstallsRepository) Patch(ctx context.Context, accountID, id uuid.UUID, patch MCPInstallPatch) (*ProfileMCPInstall, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	setClauses := []string{"updated_at = now()"}
	args := []any{}
	argIdx := 1
	if patch.DisplayName != nil {
		setClauses = append(setClauses, fmt.Sprintf("display_name = $%d", argIdx))
		args = append(args, strings.TrimSpace(*patch.DisplayName))
		argIdx++
	}
	if patch.SourceKind != nil {
		setClauses = append(setClauses, fmt.Sprintf("source_kind = $%d", argIdx))
		args = append(args, strings.TrimSpace(*patch.SourceKind))
		argIdx++
	}
	if patch.SourceURI != nil {
		setClauses = append(setClauses, fmt.Sprintf("source_uri = $%d", argIdx))
		args = append(args, strings.TrimSpace(*patch.SourceURI))
		argIdx++
	}
	if patch.SyncMode != nil {
		setClauses = append(setClauses, fmt.Sprintf("sync_mode = $%d", argIdx))
		args = append(args, strings.TrimSpace(*patch.SyncMode))
		argIdx++
	}
	if patch.Transport != nil {
		setClauses = append(setClauses, fmt.Sprintf("transport = $%d", argIdx))
		args = append(args, strings.TrimSpace(*patch.Transport))
		argIdx++
	}
	if patch.LaunchSpecJSON != nil && len(*patch.LaunchSpecJSON) > 0 {
		setClauses = append(setClauses, fmt.Sprintf("launch_spec_json = $%d", argIdx))
		args = append(args, *patch.LaunchSpecJSON)
		argIdx++
	}
	if patch.ClearAuthHeaders {
		setClauses = append(setClauses, "auth_headers_secret_id = NULL")
	} else if patch.AuthHeadersSecretID != nil {
		setClauses = append(setClauses, fmt.Sprintf("auth_headers_secret_id = $%d", argIdx))
		args = append(args, *patch.AuthHeadersSecretID)
		argIdx++
	}
	if patch.HostRequirement != nil {
		setClauses = append(setClauses, fmt.Sprintf("host_requirement = $%d", argIdx))
		args = append(args, strings.TrimSpace(*patch.HostRequirement))
		argIdx++
	}
	if patch.DiscoveryStatus != nil {
		setClauses = append(setClauses, fmt.Sprintf("discovery_status = $%d", argIdx))
		args = append(args, strings.TrimSpace(*patch.DiscoveryStatus))
		argIdx++
	}
	if patch.LastErrorCode != nil {
		setClauses = append(setClauses, fmt.Sprintf("last_error_code = $%d", argIdx))
		args = append(args, nullableTrimmed(*patch.LastErrorCode))
		argIdx++
	}
	if patch.LastErrorMessage != nil {
		setClauses = append(setClauses, fmt.Sprintf("last_error_message = $%d", argIdx))
		args = append(args, nullableTrimmed(*patch.LastErrorMessage))
		argIdx++
	}
	if patch.LastCheckedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("last_checked_at = $%d", argIdx))
		args = append(args, *patch.LastCheckedAt)
		argIdx++
	}
	args = append(args, id, accountID)

	var item ProfileMCPInstall
	err := r.db.QueryRow(
		ctx,
		fmt.Sprintf(`UPDATE profile_mcp_installs
		    SET %s
		  WHERE id = $%d AND account_id = $%d
		  RETURNING id, account_id, profile_ref, install_key, display_name, source_kind, source_uri,
		            sync_mode, transport, launch_spec_json, auth_headers_secret_id, host_requirement,
		            discovery_status, last_error_code, last_error_message, last_checked_at, created_at, updated_at`,
			strings.Join(setClauses, ", "), argIdx, argIdx+1,
		),
		args...,
	).Scan(
		&item.ID, &item.AccountID, &item.ProfileRef, &item.InstallKey, &item.DisplayName, &item.SourceKind, &item.SourceURI,
		&item.SyncMode, &item.Transport, &item.LaunchSpecJSON, &item.AuthHeadersSecretID, &item.HostRequirement,
		&item.DiscoveryStatus, &item.LastErrorCode, &item.LastErrorMessage, &item.LastCheckedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (r *ProfileMCPInstallsRepository) Delete(ctx context.Context, accountID, id uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := r.db.Exec(ctx, `DELETE FROM profile_mcp_installs WHERE account_id = $1 AND id = $2`, accountID, id)
	return err
}

type WorkspaceMCPEnablement struct {
	WorkspaceRef     string
	AccountID        uuid.UUID
	InstallID        uuid.UUID
	InstallKey       string
	EnabledByUserID  uuid.UUID
	Enabled          bool
	EnabledAt        *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DisplayName      string
	ProfileRef       string
	SourceKind       string
	SourceURI        *string
	SyncMode         string
	Transport        string
	HostRequirement  string
	DiscoveryStatus  string
	LastErrorCode    *string
	LastErrorMessage *string
	LastCheckedAt    *time.Time
	LaunchSpecJSON   json.RawMessage
}

type WorkspaceMCPEnablementsRepository struct {
	db Querier
}

func NewWorkspaceMCPEnablementsRepository(db Querier) (*WorkspaceMCPEnablementsRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &WorkspaceMCPEnablementsRepository{db: db}, nil
}

func (r *WorkspaceMCPEnablementsRepository) WithTx(tx pgx.Tx) *WorkspaceMCPEnablementsRepository {
	return &WorkspaceMCPEnablementsRepository{db: tx}
}

func (r *WorkspaceMCPEnablementsRepository) Replace(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, workspaceRef string, enabledByUserID uuid.UUID, installIDs []uuid.UUID, installsByID map[uuid.UUID]ProfileMCPInstall) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if tx == nil {
		return fmt.Errorf("tx must not be nil")
	}
	workspaceRef = strings.TrimSpace(workspaceRef)
	if workspaceRef == "" || accountID == uuid.Nil || enabledByUserID == uuid.Nil {
		return fmt.Errorf("workspace mcp enablement is invalid")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM workspace_mcp_enablements WHERE account_id = $1 AND workspace_ref = $2`, accountID, workspaceRef); err != nil {
		return err
	}
	for _, installID := range installIDs {
		install, ok := installsByID[installID]
		if !ok {
			return fmt.Errorf("install %s not found", installID)
		}
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO workspace_mcp_enablements (workspace_ref, account_id, install_id, install_key, enabled_by_user_id, enabled)
			 VALUES ($1, $2, $3, $4, $5, TRUE)
			 ON CONFLICT (workspace_ref, install_id) DO UPDATE
			 SET install_key = EXCLUDED.install_key,
			     enabled_by_user_id = EXCLUDED.enabled_by_user_id,
			     enabled = TRUE,
			     enabled_at = now(),
			     updated_at = now()`,
			workspaceRef, accountID, installID, install.InstallKey, enabledByUserID,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *WorkspaceMCPEnablementsRepository) ListByWorkspace(ctx context.Context, accountID uuid.UUID, workspaceRef string) ([]WorkspaceMCPEnablement, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT w.workspace_ref, w.account_id, w.install_id, w.install_key, w.enabled_by_user_id,
		        w.enabled, w.enabled_at, w.created_at, w.updated_at,
		        i.display_name, i.profile_ref, i.source_kind, i.source_uri, i.sync_mode,
		        i.transport, i.host_requirement, i.discovery_status, i.last_error_code,
		        i.last_error_message, i.last_checked_at, i.launch_spec_json
		   FROM workspace_mcp_enablements w
		   JOIN profile_mcp_installs i ON i.id = w.install_id
		  WHERE w.account_id = $1 AND w.workspace_ref = $2
		  ORDER BY i.display_name, i.install_key`,
		accountID, strings.TrimSpace(workspaceRef),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []WorkspaceMCPEnablement{}
	for rows.Next() {
		var item WorkspaceMCPEnablement
		if err := rows.Scan(
			&item.WorkspaceRef, &item.AccountID, &item.InstallID, &item.InstallKey, &item.EnabledByUserID,
			&item.Enabled, &item.EnabledAt, &item.CreatedAt, &item.UpdatedAt,
			&item.DisplayName, &item.ProfileRef, &item.SourceKind, &item.SourceURI, &item.SyncMode,
			&item.Transport, &item.HostRequirement, &item.DiscoveryStatus, &item.LastErrorCode,
			&item.LastErrorMessage, &item.LastCheckedAt, &item.LaunchSpecJSON,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *WorkspaceMCPEnablementsRepository) Set(ctx context.Context, accountID uuid.UUID, profileRef, workspaceRef string, installID uuid.UUID, enabledByUserID *uuid.UUID, enabled bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID == uuid.Nil {
		return fmt.Errorf("account_id must not be empty")
	}
	profileRef = strings.TrimSpace(profileRef)
	workspaceRef = strings.TrimSpace(workspaceRef)
	if profileRef == "" || workspaceRef == "" || installID == uuid.Nil {
		return fmt.Errorf("profile_ref, workspace_ref and install_id must not be empty")
	}
	if enabled {
		if enabledByUserID == nil || *enabledByUserID == uuid.Nil {
			return fmt.Errorf("enabled_by_user_id must not be empty")
		}
		_, err := r.db.Exec(
			ctx,
			`INSERT INTO workspace_mcp_enablements (workspace_ref, account_id, install_id, install_key, enabled_by_user_id, enabled, enabled_at)
				 SELECT $1, i.account_id, i.id, i.install_key, $5, TRUE, now()
				   FROM profile_mcp_installs i
				  WHERE i.account_id = $2 AND i.profile_ref = $3 AND i.id = $4
				 ON CONFLICT (workspace_ref, install_id) DO UPDATE
				     SET install_key = EXCLUDED.install_key,
				         enabled_by_user_id = EXCLUDED.enabled_by_user_id,
				         enabled = TRUE,
				         enabled_at = now(),
				         updated_at = now()`,
			workspaceRef, accountID, profileRef, installID, *enabledByUserID,
		)
		return err
	}
	_, err := r.db.Exec(
		ctx,
		`DELETE FROM workspace_mcp_enablements
		  WHERE account_id = $1 AND workspace_ref = $2 AND install_id = $3`,
		accountID, workspaceRef, installID,
	)
	return err
}

func nullableTrimmed(value string) *string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}
