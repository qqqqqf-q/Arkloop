package catalogapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"context"
	"encoding/json"
	"fmt"
	nethttp "net/http"
	"regexp"
	"strings"
	"time"

	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/mcpfilesync"
	"arkloop/services/api/internal/observability"
	sharedenvironmentref "arkloop/services/shared/environmentref"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type mcpInstallRequest struct {
	InstallKey      *string           `json:"install_key,omitempty"`
	DisplayName     *string           `json:"display_name,omitempty"`
	SourceKind      *string           `json:"source_kind,omitempty"`
	SourceURI       *string           `json:"source_uri,omitempty"`
	SyncMode        *string           `json:"sync_mode,omitempty"`
	Transport       *string           `json:"transport,omitempty"`
	LaunchSpec      map[string]any    `json:"launch_spec,omitempty"`
	AuthHeaders     map[string]string `json:"auth_headers,omitempty"`
	BearerToken     *string           `json:"bearer_token,omitempty"`
	HostRequirement *string           `json:"host_requirement,omitempty"`
}

type mcpInstallResponse struct {
	ID              string            `json:"id"`
	InstallKey      string            `json:"install_key"`
	AccountID       string            `json:"account_id"`
	ProfileRef      string            `json:"profile_ref"`
	DisplayName     string            `json:"display_name"`
	SourceKind      string            `json:"source_kind"`
	SourceURI       *string           `json:"source_uri,omitempty"`
	SyncMode        string            `json:"sync_mode"`
	Transport       string            `json:"transport"`
	LaunchSpec      map[string]any    `json:"launch_spec"`
	HasAuth         bool              `json:"has_auth"`
	HostRequirement string            `json:"host_requirement"`
	DiscoveryStatus string            `json:"discovery_status"`
	LastErrorCode   *string           `json:"last_error_code,omitempty"`
	LastErrorMsg    *string           `json:"last_error_message,omitempty"`
	LastCheckedAt   *string           `json:"last_checked_at,omitempty"`
	CreatedAt       string            `json:"created_at"`
	UpdatedAt       string            `json:"updated_at"`
	WorkspaceState  *workspaceMCPView `json:"workspace_state,omitempty"`
}

type workspaceMCPView struct {
	WorkspaceRef string  `json:"workspace_ref"`
	Enabled      bool    `json:"enabled"`
	EnabledAt    *string `json:"enabled_at,omitempty"`
}

type workspaceMCPEnablementRequest struct {
	WorkspaceRef string `json:"workspace_ref"`
	InstallKey   string `json:"install_key"`
	Enabled      bool   `json:"enabled"`
}

type mcpDiscoverySourceItem struct {
	SourceURI        string                   `json:"source_uri"`
	SourceKind       string                   `json:"source_kind"`
	Installable      bool                     `json:"installable"`
	ValidationErrors []string                 `json:"validation_errors"`
	HostWarnings     []string                 `json:"host_warnings"`
	ProposedInstalls []mcpDiscoverySourceSpec `json:"proposed_installs"`
}

type mcpDiscoverySourceSpec struct {
	InstallKey      string         `json:"install_key"`
	DisplayName     string         `json:"display_name"`
	Transport       string         `json:"transport"`
	LaunchSpec      map[string]any `json:"launch_spec"`
	HostRequirement string         `json:"host_requirement"`
}

type MCPDiscoverySourceService interface {
	DiscoverSources(ctx context.Context, req mcpfilesync.DiscoveryRequest) (mcpfilesync.DiscoveryResponse, error)
	SyncDesktopMirror(ctx context.Context, accountID uuid.UUID, profileRef string) error
}

var installKeySanitizer = regexp.MustCompile(`[^a-z0-9_]+`)

func mcpInstallsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	installsRepo *data.ProfileMCPInstallsRepository,
	secretsRepo *data.SecretsRepository,
	pool data.DB,
	service MCPDiscoverySourceService,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		switch r.Method {
		case nethttp.MethodGet:
			listMCPInstalls(w, r, traceID, authService, membershipRepo, installsRepo, pool)
		case nethttp.MethodPost:
			createMCPInstall(w, r, traceID, authService, membershipRepo, installsRepo, secretsRepo, pool, service)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func mcpInstallEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	installsRepo *data.ProfileMCPInstallsRepository,
	secretsRepo *data.SecretsRepository,
	pool data.DB,
	service MCPDiscoverySourceService,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		tail := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/mcp-installs/"), "/")
		if tail == "" {
			httpkit.WriteNotFound(w, r)
			return
		}
		if strings.HasSuffix(tail, ":check") {
			id, err := uuid.Parse(strings.TrimSuffix(tail, ":check"))
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
				return
			}
			if r.Method != nethttp.MethodPost {
				httpkit.WriteMethodNotAllowed(w, r)
				return
			}
			checkMCPInstall(w, r, traceID, id, authService, membershipRepo, installsRepo, secretsRepo, pool)
			return
		}
		id, err := uuid.Parse(tail)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
			return
		}
		switch r.Method {
		case nethttp.MethodPatch:
			updateMCPInstall(w, r, traceID, id, authService, membershipRepo, installsRepo, secretsRepo, pool, service)
		case nethttp.MethodDelete:
			deleteMCPInstall(w, r, traceID, id, authService, membershipRepo, installsRepo, pool, service)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func workspaceMCPEnablementsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	installsRepo *data.ProfileMCPInstallsRepository,
	enableRepo *data.WorkspaceMCPEnablementsRepository,
	workspaceRepo *data.WorkspaceRegistriesRepository,
	profileRepo *data.ProfileRegistriesRepository,
	pool data.DB,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		switch r.Method {
		case nethttp.MethodGet:
			listWorkspaceMCPEnablements(w, r, traceID, authService, membershipRepo, enableRepo, profileRepo, workspaceRepo)
		case nethttp.MethodPut:
			setWorkspaceMCPEnablement(w, r, traceID, authService, membershipRepo, installsRepo, enableRepo, profileRepo, workspaceRepo, pool)
		default:
			httpkit.WriteMethodNotAllowed(w, r)
		}
	}
}

func mcpDiscoverySourcesEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	service MCPDiscoverySourceService,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if r.Method != nethttp.MethodGet {
			httpkit.WriteMethodNotAllowed(w, r)
			return
		}
		if _, ok := httpkit.AuthenticateActor(w, r, traceID, authService); !ok {
			return
		}
		if service == nil {
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]any{"items": []mcpDiscoverySourceItem{}})
			return
		}
		paths := r.URL.Query()["path"]
		var workspaceRoot *string
		if value := strings.TrimSpace(r.URL.Query().Get("workspace_root")); value != "" {
			workspaceRoot = &value
		}
		result, err := service.DiscoverSources(r.Context(), mcpfilesync.DiscoveryRequest{
			WorkspaceRoot: workspaceRoot,
			Paths:         paths,
		})
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "mcp.discovery_failed", "failed to discover mcp sources", traceID, nil)
			return
		}
		items := make([]mcpDiscoverySourceItem, 0, len(result.Sources))
		for _, source := range result.Sources {
			proposed := make([]mcpDiscoverySourceSpec, 0, len(source.ProposedInstalls))
			for _, install := range source.ProposedInstalls {
				spec := map[string]any{}
				_ = json.Unmarshal(install.LaunchSpecJSON, &spec)
				proposed = append(proposed, mcpDiscoverySourceSpec{
					InstallKey:      install.InstallKey,
					DisplayName:     install.DisplayName,
					Transport:       install.Transport,
					LaunchSpec:      spec,
					HostRequirement: install.HostRequirement,
				})
			}
			items = append(items, mcpDiscoverySourceItem{
				SourceURI:        source.SourceURI,
				SourceKind:       source.SourceKind,
				Installable:      source.Installable,
				ValidationErrors: source.ValidationErrors,
				HostWarnings:     source.HostWarnings,
				ProposedInstalls: proposed,
			})
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]any{"items": items})
	}
}

func listMCPInstalls(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	installsRepo *data.ProfileMCPInstallsRepository,
	pool data.DB,
) {
	if authService == nil || installsRepo == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}
	profileRef := sharedenvironmentref.BuildProfileRef(actor.AccountID, &actor.UserID)
	items, err := installsRepo.ListByProfile(r.Context(), actor.AccountID, profileRef)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	workspaceStateByKey, _ := loadWorkspaceMCPViews(r.Context(), pool, actor.AccountID, actor.UserID, profileRef)
	resp := make([]mcpInstallResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, toMCPInstallResponse(item, workspaceStateByKey[item.InstallKey]))
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, resp)
}

func createMCPInstall(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	installsRepo *data.ProfileMCPInstallsRepository,
	secretsRepo *data.SecretsRepository,
	pool data.DB,
	service MCPDiscoverySourceService,
) {
	if authService == nil || installsRepo == nil || pool == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}
	var req mcpInstallRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}
	install, authSecretID, err := buildMCPInstallFromRequest(actor.AccountID, actor.UserID, req)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
		return
	}
	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context())
	txInstalls := installsRepo.WithTx(tx)
	if authSecretID != nil && secretsRepo != nil {
		secret, err := upsertMCPAuthHeadersSecret(r.Context(), secretsRepo.WithTx(tx), actor.UserID, install.InstallKey, authSecretID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		install.AuthHeadersSecretID = &secret
	}
	created, err := txInstalls.Create(r.Context(), install)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	notifyMCPChanged(r.Context(), pool, actor.AccountID)
	if service != nil {
		_ = service.SyncDesktopMirror(r.Context(), actor.AccountID, created.ProfileRef)
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toMCPInstallResponse(created, nil))
}

func updateMCPInstall(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	id uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	installsRepo *data.ProfileMCPInstallsRepository,
	secretsRepo *data.SecretsRepository,
	pool data.DB,
	service MCPDiscoverySourceService,
) {
	if authService == nil || installsRepo == nil || pool == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}
	current, err := installsRepo.GetByID(r.Context(), actor.AccountID, id)
	if err != nil || current == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "mcp_installs.not_found", "install not found", traceID, nil)
		return
	}
	var req mcpInstallRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}
	patch, authPayload, err := buildMCPInstallPatch(*current, req)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", err.Error(), traceID, nil)
		return
	}
	tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	defer tx.Rollback(r.Context())
	if authPayload != nil && secretsRepo != nil {
		secretID, err := upsertMCPAuthHeadersSecret(r.Context(), secretsRepo.WithTx(tx), actor.UserID, current.InstallKey, authPayload)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		patch.AuthHeadersSecretID = &secretID
	}
	updated, err := installsRepo.WithTx(tx).Patch(r.Context(), actor.AccountID, id, patch)
	if err != nil || updated == nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	notifyMCPChanged(r.Context(), pool, actor.AccountID)
	if service != nil {
		_ = service.SyncDesktopMirror(r.Context(), actor.AccountID, updated.ProfileRef)
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toMCPInstallResponse(*updated, nil))
}

func deleteMCPInstall(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	id uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	installsRepo *data.ProfileMCPInstallsRepository,
	pool data.DB,
	service MCPDiscoverySourceService,
) {
	if authService == nil || installsRepo == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}
	if err := installsRepo.Delete(r.Context(), actor.AccountID, id); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	notifyMCPChanged(r.Context(), pool, actor.AccountID)
	if service != nil {
		profileRef := sharedenvironmentref.BuildProfileRef(actor.AccountID, &actor.UserID)
		_ = service.SyncDesktopMirror(r.Context(), actor.AccountID, profileRef)
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]bool{"ok": true})
}

func checkMCPInstall(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	id uuid.UUID,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	installsRepo *data.ProfileMCPInstallsRepository,
	secretsRepo *data.SecretsRepository,
	pool data.DB,
) {
	if authService == nil || installsRepo == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}
	item, err := installsRepo.GetByID(r.Context(), actor.AccountID, id)
	if err != nil || item == nil {
		httpkit.WriteError(w, nethttp.StatusNotFound, "mcp_installs.not_found", "install not found", traceID, nil)
		return
	}
	headers, err := loadMCPAuthHeaders(r.Context(), secretsRepo, item.AuthHeadersSecretID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	status, code, msg := runMCPInstallCheck(r.Context(), *item, headers)
	now := time.Now().UTC()
	updated, err := installsRepo.Patch(r.Context(), actor.AccountID, item.ID, data.ProfileMCPInstallPatch{
		DiscoveryStatus:  &status,
		LastErrorCode:    &code,
		LastErrorMessage: &msg,
		LastCheckedAt:    &now,
	})
	if err != nil || updated == nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toMCPInstallResponse(*updated, nil))
}

func listWorkspaceMCPEnablements(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	enableRepo *data.WorkspaceMCPEnablementsRepository,
	profileRepo *data.ProfileRegistriesRepository,
	workspaceRepo *data.WorkspaceRegistriesRepository,
) {
	if authService == nil || enableRepo == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}
	profileRef := sharedenvironmentref.BuildProfileRef(actor.AccountID, &actor.UserID)
	workspaceRef := strings.TrimSpace(r.URL.Query().Get("workspace_ref"))
	if workspaceRef == "" {
		var err error
		workspaceRef, err = ensureDefaultWorkspaceForProfile(r.Context(), profileRepo, workspaceRepo, actor.AccountID, actor.UserID, profileRef)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
	}
	items, err := enableRepo.ListByWorkspace(r.Context(), actor.AccountID, workspaceRef)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]any{"items": items})
}

func setWorkspaceMCPEnablement(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	installsRepo *data.ProfileMCPInstallsRepository,
	enableRepo *data.WorkspaceMCPEnablementsRepository,
	profileRepo *data.ProfileRegistriesRepository,
	workspaceRepo *data.WorkspaceRegistriesRepository,
	pool data.DB,
) {
	if authService == nil || installsRepo == nil || enableRepo == nil {
		httpkit.WriteAuthNotConfigured(w, traceID)
		return
	}
	actor, ok := httpkit.AuthenticateActor(w, r, traceID, authService)
	if !ok {
		return
	}
	var req workspaceMCPEnablementRequest
	if err := httpkit.DecodeJSON(r, &req); err != nil {
		httpkit.WriteError(w, nethttp.StatusUnprocessableEntity, "validation.error", "request validation failed", traceID, nil)
		return
	}
	profileRef := sharedenvironmentref.BuildProfileRef(actor.AccountID, &actor.UserID)
	workspaceRef := strings.TrimSpace(req.WorkspaceRef)
	if workspaceRef == "" {
		var err error
		workspaceRef, err = ensureDefaultWorkspaceForProfile(r.Context(), profileRepo, workspaceRepo, actor.AccountID, actor.UserID, profileRef)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
	}
	items, err := installsRepo.ListByProfile(r.Context(), actor.AccountID, profileRef)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	found := false
	for _, item := range items {
		if item.InstallKey == strings.TrimSpace(req.InstallKey) {
			found = true
			break
		}
	}
	if !found {
		httpkit.WriteError(w, nethttp.StatusNotFound, "mcp_installs.not_found", "install not found", traceID, nil)
		return
	}
	if err := enableRepo.Set(r.Context(), actor.AccountID, workspaceRef, req.InstallKey, &actor.UserID, req.Enabled); err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	notifyMCPChanged(r.Context(), pool, actor.AccountID)
	itemsResp, err := enableRepo.ListByWorkspace(r.Context(), actor.AccountID, workspaceRef)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]any{"items": itemsResp})
}

func buildMCPInstallFromRequest(accountID, userID uuid.UUID, req mcpInstallRequest) (data.ProfileMCPInstall, map[string]string, error) {
	displayName := strings.TrimSpace(derefReqString(req.DisplayName))
	transport := strings.TrimSpace(derefReqString(req.Transport))
	if displayName == "" || transport == "" {
		return data.ProfileMCPInstall{}, nil, fmt.Errorf("display_name and transport are required")
	}
	profileRef := sharedenvironmentref.BuildProfileRef(accountID, &userID)
	installKey := strings.TrimSpace(derefReqString(req.InstallKey))
	if installKey == "" {
		installKey = normalizeInstallKey(displayName)
	}
	sourceKind := strings.TrimSpace(derefReqString(req.SourceKind))
	if sourceKind == "" {
		sourceKind = "manual_console"
	}
	syncMode := strings.TrimSpace(derefReqString(req.SyncMode))
	if syncMode == "" {
		syncMode = "none"
	}
	hostRequirement := strings.TrimSpace(derefReqString(req.HostRequirement))
	if hostRequirement == "" {
		if transport == "stdio" {
			hostRequirement = "cloud_worker"
		} else {
			hostRequirement = "remote_http"
		}
	}
	launchSpecJSON, err := json.Marshal(req.LaunchSpec)
	if err != nil {
		return data.ProfileMCPInstall{}, nil, fmt.Errorf("launch_spec is invalid")
	}
	authHeaders := cloneStringMap(req.AuthHeaders)
	if token := strings.TrimSpace(derefReqString(req.BearerToken)); token != "" {
		authHeaders["Authorization"] = "Bearer " + token
	}
	return data.ProfileMCPInstall{
		InstallKey:      installKey,
		AccountID:       accountID,
		ProfileRef:      profileRef,
		DisplayName:     displayName,
		SourceKind:      sourceKind,
		SourceURI:       cleanStringPtr(req.SourceURI),
		SyncMode:        syncMode,
		Transport:       transport,
		LaunchSpecJSON:  launchSpecJSON,
		HostRequirement: hostRequirement,
		DiscoveryStatus: "needs_check",
	}, authHeaders, nil
}

func buildMCPInstallPatch(current data.ProfileMCPInstall, req mcpInstallRequest) (data.ProfileMCPInstallPatch, map[string]string, error) {
	var patch data.ProfileMCPInstallPatch
	if req.DisplayName != nil {
		patch.DisplayName = cleanStringPtr(req.DisplayName)
	}
	if req.SourceKind != nil {
		patch.SourceKind = cleanStringPtr(req.SourceKind)
	}
	if req.SourceURI != nil {
		patch.SourceURI = cleanStringPtr(req.SourceURI)
	}
	if req.SyncMode != nil {
		patch.SyncMode = cleanStringPtr(req.SyncMode)
	}
	if req.Transport != nil {
		patch.Transport = cleanStringPtr(req.Transport)
	}
	if req.LaunchSpec != nil {
		payload, err := json.Marshal(req.LaunchSpec)
		if err != nil {
			return patch, nil, fmt.Errorf("launch_spec is invalid")
		}
		raw := json.RawMessage(payload)
		patch.LaunchSpecJSON = &raw
		status := "needs_check"
		patch.DiscoveryStatus = &status
	}
	if req.HostRequirement != nil {
		patch.HostRequirement = cleanStringPtr(req.HostRequirement)
	}
	authHeaders := cloneStringMap(req.AuthHeaders)
	if token := strings.TrimSpace(derefReqString(req.BearerToken)); token != "" {
		authHeaders["Authorization"] = "Bearer " + token
	}
	return patch, authHeaders, nil
}

func loadWorkspaceMCPViews(ctx context.Context, pool data.DB, accountID, userID uuid.UUID, profileRef string) (map[string]*workspaceMCPView, error) {
	if pool == nil {
		return map[string]*workspaceMCPView{}, nil
	}
	workspaceRepo, _ := data.NewWorkspaceRegistriesRepository(pool)
	profileRepo, _ := data.NewProfileRegistriesRepository(pool)
	enableRepo, _ := data.NewWorkspaceMCPEnablementsRepository(pool)
	workspaceRef, err := ensureDefaultWorkspaceForProfile(ctx, profileRepo, workspaceRepo, accountID, userID, profileRef)
	if err != nil {
		return nil, err
	}
	items, err := enableRepo.ListByWorkspace(ctx, accountID, workspaceRef)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*workspaceMCPView, len(items))
	for _, item := range items {
		var enabledAt *string
		if item.EnabledAt != nil {
			value := item.EnabledAt.UTC().Format(time.RFC3339)
			enabledAt = &value
		}
		out[item.InstallKey] = &workspaceMCPView{WorkspaceRef: item.WorkspaceRef, Enabled: item.Enabled, EnabledAt: enabledAt}
	}
	return out, nil
}

func toMCPInstallResponse(item data.ProfileMCPInstall, workspaceState *workspaceMCPView) mcpInstallResponse {
	spec := map[string]any{}
	if len(item.LaunchSpecJSON) > 0 {
		_ = json.Unmarshal(item.LaunchSpecJSON, &spec)
	}
	var lastCheckedAt *string
	if item.LastCheckedAt != nil {
		value := item.LastCheckedAt.UTC().Format(time.RFC3339)
		lastCheckedAt = &value
	}
	return mcpInstallResponse{
		ID:              item.ID.String(),
		InstallKey:      item.InstallKey,
		AccountID:       item.AccountID.String(),
		ProfileRef:      item.ProfileRef,
		DisplayName:     item.DisplayName,
		SourceKind:      item.SourceKind,
		SourceURI:       item.SourceURI,
		SyncMode:        item.SyncMode,
		Transport:       item.Transport,
		LaunchSpec:      spec,
		HasAuth:         item.AuthHeadersSecretID != nil,
		HostRequirement: item.HostRequirement,
		DiscoveryStatus: item.DiscoveryStatus,
		LastErrorCode:   item.LastErrorCode,
		LastErrorMsg:    item.LastErrorMessage,
		LastCheckedAt:   lastCheckedAt,
		CreatedAt:       item.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:       item.UpdatedAt.UTC().Format(time.RFC3339),
		WorkspaceState:  workspaceState,
	}
}

func normalizeInstallKey(value string) string {
	key := strings.ToLower(strings.TrimSpace(value))
	key = installKeySanitizer.ReplaceAllString(key, "_")
	key = strings.Trim(key, "_")
	if key == "" {
		return "mcp_install"
	}
	return key
}

func derefReqString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func cleanStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cleaned := strings.TrimSpace(*value)
	return &cleaned
}

func cloneStringMap(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]string, len(value))
	for key, item := range value {
		out[strings.TrimSpace(key)] = item
	}
	return out
}

func upsertMCPAuthHeadersSecret(ctx context.Context, repo *data.SecretsRepository, userID uuid.UUID, installKey string, payload map[string]string) (uuid.UUID, error) {
	if repo == nil || len(payload) == 0 {
		return uuid.Nil, nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, err
	}
	secret, err := repo.Upsert(ctx, userID, "mcp_auth_headers:"+strings.TrimSpace(installKey), string(encoded))
	if err != nil {
		return uuid.Nil, err
	}
	return secret.ID, nil
}

func loadMCPAuthHeaders(ctx context.Context, repo *data.SecretsRepository, secretID *uuid.UUID) (map[string]string, error) {
	if repo == nil || secretID == nil || *secretID == uuid.Nil {
		return nil, nil
	}
	plain, err := repo.DecryptByID(ctx, *secretID)
	if err != nil || plain == nil {
		return nil, err
	}
	headers := map[string]string{}
	if err := json.Unmarshal([]byte(*plain), &headers); err != nil {
		return nil, err
	}
	return headers, nil
}

func notifyMCPChanged(ctx context.Context, pool data.DB, accountID uuid.UUID) {
	if pool == nil || accountID == uuid.Nil {
		return
	}
	_, _ = pool.Exec(ctx, "SELECT pg_notify('mcp_config_changed', $1)", accountID.String())
}
