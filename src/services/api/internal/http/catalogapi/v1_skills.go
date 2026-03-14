package catalogapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"context"
	"encoding/json"
	"errors"
	nethttp "net/http"
	"strings"
	"time"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	sharedenvironmentref "arkloop/services/shared/environmentref"
	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/skillstore"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type skillPackageResponse struct {
	SkillKey            string   `json:"skill_key"`
	Version             string   `json:"version"`
	DisplayName         string   `json:"display_name"`
	Description         *string  `json:"description,omitempty"`
	InstructionPath     string   `json:"instruction_path"`
	ManifestKey         string   `json:"manifest_key"`
	BundleKey           string   `json:"bundle_key"`
	Platforms           []string `json:"platforms,omitempty"`
	Source              string   `json:"source,omitempty"`
	RegistryProvider    *string  `json:"registry_provider,omitempty"`
	RegistrySlug        *string  `json:"registry_slug,omitempty"`
	RegistryOwnerHandle *string  `json:"registry_owner_handle,omitempty"`
	RegistryVersion     *string  `json:"registry_version,omitempty"`
	RegistryDetailURL   *string  `json:"registry_detail_url,omitempty"`
	RegistryDownloadURL *string  `json:"registry_download_url,omitempty"`
	RegistrySourceKind  *string  `json:"registry_source_kind,omitempty"`
	RegistrySourceURL   *string  `json:"registry_source_url,omitempty"`
	OwnerHandle         *string  `json:"owner_handle,omitempty"`
	DetailURL           *string  `json:"detail_url,omitempty"`
	RepositoryURL       *string  `json:"repository_url,omitempty"`
	ScanStatus          string   `json:"scan_status,omitempty"`
	ScanHasWarnings     bool     `json:"scan_has_warnings,omitempty"`
	ScanCheckedAt       *string  `json:"scan_checked_at,omitempty"`
	ScanEngine          *string  `json:"scan_engine,omitempty"`
	ScanSummary         *string  `json:"scan_summary,omitempty"`
	ModerationVerdict   *string  `json:"moderation_verdict,omitempty"`
	IsActive            bool     `json:"is_active"`
}

type skillPackageRegisterRequest struct {
	SkillKey string `json:"skill_key"`
	Version  string `json:"version"`
}

type skillReferenceRequest struct {
	SkillKey string `json:"skill_key"`
	Version  string `json:"version"`
}

type workspaceSkillsReplaceRequest struct {
	Skills []skillReferenceRequest `json:"skills"`
}

type installedSkillResponse struct {
	SkillKey            string  `json:"skill_key"`
	Version             string  `json:"version"`
	DisplayName         string  `json:"display_name"`
	Description         *string `json:"description,omitempty"`
	CreatedAt           string  `json:"created_at,omitempty"`
	UpdatedAt           string  `json:"updated_at,omitempty"`
	Source              string  `json:"source,omitempty"`
	RegistryProvider    *string `json:"registry_provider,omitempty"`
	RegistrySlug        *string `json:"registry_slug,omitempty"`
	RegistryOwnerHandle *string `json:"registry_owner_handle,omitempty"`
	RegistryVersion     *string `json:"registry_version,omitempty"`
	RegistryDetailURL   *string `json:"registry_detail_url,omitempty"`
	RegistryDownloadURL *string `json:"registry_download_url,omitempty"`
	RegistrySourceKind  *string `json:"registry_source_kind,omitempty"`
	RegistrySourceURL   *string `json:"registry_source_url,omitempty"`
	OwnerHandle         *string `json:"owner_handle,omitempty"`
	DetailURL           *string `json:"detail_url,omitempty"`
	ScanStatus          string  `json:"scan_status,omitempty"`
	ScanHasWarnings     bool    `json:"scan_has_warnings,omitempty"`
	ScanCheckedAt       *string `json:"scan_checked_at,omitempty"`
	ScanEngine          *string `json:"scan_engine,omitempty"`
	ScanSummary         *string `json:"scan_summary,omitempty"`
	ModerationVerdict   *string `json:"moderation_verdict,omitempty"`
}

func skillPackagesEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	repo *data.SkillPackagesRepository,
	_ skillStore,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if repo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "skills.not_configured", "skills not configured", traceID, nil)
			return
		}
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowedJSON(w, traceID)
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataPersonasRead, w, traceID) {
			return
		}
		items, err := repo.ListActive(r.Context(), actor.AccountID)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]any{"items": toSkillPackageResponses(items)})
	}
}

func adminSkillPackagesEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	repo *data.SkillPackagesRepository,
	store skillStore,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if repo == nil || store == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "skills.not_configured", "skills not configured", traceID, nil)
			return
		}
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowedJSON(w, traceID)
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermPlatformAdmin, w, traceID) {
			return
		}
		var req skillPackageRegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "skills.invalid_request", "invalid JSON body", traceID, nil)
			return
		}
		manifest, err := loadSkillPackageManifest(r.Context(), store, req.SkillKey, req.Version)
		if err != nil {
			writeSkillStoreError(w, traceID, err)
			return
		}
		item, err := repo.Create(r.Context(), actor.AccountID, manifest)
		if err != nil {
			var conflict data.SkillPackageConflictError
			if errors.As(err, &conflict) {
				httpkit.WriteError(w, nethttp.StatusConflict, "skills.conflict", "skill package already exists", traceID, nil)
				return
			}
			httpkit.WriteError(w, nethttp.StatusBadRequest, "skills.invalid_manifest", err.Error(), traceID, nil)
			return
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toSkillPackageResponse(item))
	}
}

func skillPackageEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	repo *data.SkillPackagesRepository,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if repo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "skills.not_configured", "skills not configured", traceID, nil)
			return
		}
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowedJSON(w, traceID)
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataPersonasRead, w, traceID) {
			return
		}
		skillKey, version, ok := parseSkillPackagePath(w, traceID, r.URL.Path, "/v1/skill-packages/")
		if !ok {
			return
		}
		item, err := repo.Get(r.Context(), actor.AccountID, skillKey, version)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if item == nil || !item.IsActive {
			httpkit.WriteError(w, nethttp.StatusNotFound, "skills.not_found", "skill package not found", traceID, nil)
			return
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, toSkillPackageResponse(*item))
	}
}

func profileSkillsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	_ *data.SkillPackagesRepository,
	installsRepo *data.ProfileSkillInstallsRepository,
	_ *data.ProfileRegistriesRepository,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if installsRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "skills.not_configured", "skills not configured", traceID, nil)
			return
		}
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if r.Method != nethttp.MethodGet {
			writeMethodNotAllowedJSON(w, traceID)
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataPersonasRead, w, traceID) {
			return
		}
		profileRef := sharedenvironmentref.BuildProfileRef(actor.AccountID, &actor.UserID)
		items, err := installsRepo.ListByProfile(r.Context(), actor.AccountID, profileRef)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]any{"items": toInstalledSkillResponses(items)})
	}
}

func profileSkillsInstallEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	packagesRepo *data.SkillPackagesRepository,
	installsRepo *data.ProfileSkillInstallsRepository,
	profileRepo *data.ProfileRegistriesRepository,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if packagesRepo == nil || installsRepo == nil || profileRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "skills.not_configured", "skills not configured", traceID, nil)
			return
		}
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if r.Method != nethttp.MethodPost {
			writeMethodNotAllowedJSON(w, traceID)
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataPersonasManage, w, traceID) {
			return
		}
		var req skillReferenceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "skills.invalid_request", "invalid JSON body", traceID, nil)
			return
		}
		pkg, err := packagesRepo.Get(r.Context(), actor.AccountID, req.SkillKey, req.Version)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if pkg == nil || !pkg.IsActive {
			httpkit.WriteError(w, nethttp.StatusNotFound, "skills.not_found", "skill package not found", traceID, nil)
			return
		}
		profileRef := sharedenvironmentref.BuildProfileRef(actor.AccountID, &actor.UserID)
		if err := installsRepo.Install(r.Context(), profileRef, actor.AccountID, actor.UserID, req.SkillKey, req.Version); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if err := syncProfileSkillRefs(r.Context(), installsRepo, profileRepo, actor.AccountID, profileRef); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, map[string]any{"skill_key": req.SkillKey, "version": req.Version})
	}
}

func profileSkillEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	installsRepo *data.ProfileSkillInstallsRepository,
	profileRepo *data.ProfileRegistriesRepository,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if installsRepo == nil || profileRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "skills.not_configured", "skills not configured", traceID, nil)
			return
		}
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if r.Method != nethttp.MethodDelete {
			writeMethodNotAllowedJSON(w, traceID)
			return
		}
		if !httpkit.RequirePerm(actor, auth.PermDataPersonasManage, w, traceID) {
			return
		}
		skillKey, version, ok := parseSkillPackagePath(w, traceID, r.URL.Path, "/v1/profiles/me/skills/")
		if !ok {
			return
		}
		profileRef := sharedenvironmentref.BuildProfileRef(actor.AccountID, &actor.UserID)
		used, err := installsRepo.IsInstalledInAnyWorkspaceForOwner(r.Context(), actor.AccountID, actor.UserID, skillKey, version)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if used {
			httpkit.WriteError(w, nethttp.StatusConflict, "skills.in_use", "skill is enabled by a workspace", traceID, nil)
			return
		}
		if err := installsRepo.Delete(r.Context(), profileRef, skillKey, version); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if err := syncProfileSkillRefs(r.Context(), installsRepo, profileRepo, actor.AccountID, profileRef); err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		w.WriteHeader(nethttp.StatusNoContent)
	}
}

func workspaceSkillsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	packagesRepo *data.SkillPackagesRepository,
	installsRepo *data.ProfileSkillInstallsRepository,
	enableRepo *data.WorkspaceSkillEnablementsRepository,
	workspaceRepo *data.WorkspaceRegistriesRepository,
	profileRepo *data.ProfileRegistriesRepository,
	pool data.TxStarter,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if packagesRepo == nil || installsRepo == nil || enableRepo == nil || workspaceRepo == nil || profileRepo == nil || pool == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "skills.not_configured", "skills not configured", traceID, nil)
			return
		}
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		if !strings.HasSuffix(strings.TrimSpace(r.URL.Path), "/skills") {
			writeMethodNotAllowedJSON(w, traceID)
			return
		}
		workspaceRef := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/v1/workspaces/"))
		workspaceRef, _, _ = strings.Cut(workspaceRef, "/skills")
		workspaceRef = strings.TrimSpace(workspaceRef)
		registry, err := workspaceRepo.Get(r.Context(), workspaceRef)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		if registry == nil || registry.AccountID != actor.AccountID || registry.OwnerUserID == nil || *registry.OwnerUserID != actor.UserID {
			httpkit.WriteError(w, nethttp.StatusNotFound, "workspaces.not_found", "workspace not found", traceID, nil)
			return
		}
		profileRef := sharedenvironmentref.BuildProfileRef(actor.AccountID, &actor.UserID)
		switch r.Method {
		case nethttp.MethodGet:
			if !httpkit.RequirePerm(actor, auth.PermDataPersonasRead, w, traceID) {
				return
			}
			items, err := enableRepo.ListByWorkspace(r.Context(), actor.AccountID, workspaceRef)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]any{"items": items})
		case nethttp.MethodPut:
			if !httpkit.RequirePerm(actor, auth.PermDataPersonasManage, w, traceID) {
				return
			}
			var req workspaceSkillsReplaceRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				httpkit.WriteError(w, nethttp.StatusBadRequest, "skills.invalid_request", "invalid JSON body", traceID, nil)
				return
			}
			items := make([]data.WorkspaceSkillEnablement, 0, len(req.Skills))
			for _, item := range req.Skills {
				pkg, err := packagesRepo.Get(r.Context(), actor.AccountID, item.SkillKey, item.Version)
				if err != nil {
					httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
					return
				}
				if pkg == nil || !pkg.IsActive {
					httpkit.WriteError(w, nethttp.StatusNotFound, "skills.not_found", "skill package not found", traceID, nil)
					return
				}
				installed, err := installsRepo.IsInstalled(r.Context(), actor.AccountID, profileRef, item.SkillKey, item.Version)
				if err != nil {
					httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
					return
				}
				if !installed {
					httpkit.WriteError(w, nethttp.StatusBadRequest, "skills.not_installed", "skill is not installed for this profile", traceID, nil)
					return
				}
				items = append(items, data.WorkspaceSkillEnablement{SkillKey: item.SkillKey, Version: item.Version})
			}
			tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			defer tx.Rollback(r.Context())
			if err := enableRepo.Replace(r.Context(), tx, actor.AccountID, workspaceRef, actor.UserID, items); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if err := tx.Commit(r.Context()); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if err := syncWorkspaceSkillRefs(r.Context(), enableRepo, workspaceRepo, actor.AccountID, workspaceRef); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if err := profileRepo.SetDefaultWorkspaceRef(r.Context(), profileRef, workspaceRef); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]any{"items": items})
		default:
			writeMethodNotAllowedJSON(w, traceID)
		}
	}
}

func toSkillPackageResponses(items []data.SkillPackage) []skillPackageResponse {
	out := make([]skillPackageResponse, 0, len(items))
	for _, item := range items {
		out = append(out, toSkillPackageResponse(item))
	}
	return out
}

func toInstalledSkillResponses(items []data.ProfileSkillInstall) []installedSkillResponse {
	out := make([]installedSkillResponse, 0, len(items))
	for _, item := range items {
		out = append(out, installedSkillResponse{
			SkillKey:            item.SkillKey,
			Version:             item.Version,
			DisplayName:         item.DisplayName,
			Description:         item.Description,
			CreatedAt:           item.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:           item.UpdatedAt.UTC().Format(time.RFC3339),
			Source:              skillSource(item.RegistryProvider, item.RegistrySourceKind),
			RegistryProvider:    copyOptionalString(item.RegistryProvider),
			RegistrySlug:        copyOptionalString(item.RegistrySlug),
			RegistryOwnerHandle: copyOptionalString(item.RegistryOwnerHandle),
			RegistryVersion:     copyOptionalString(item.RegistryVersion),
			RegistryDetailURL:   copyOptionalString(item.RegistryDetailURL),
			RegistryDownloadURL: copyOptionalString(item.RegistryDownloadURL),
			RegistrySourceKind:  copyOptionalString(item.RegistrySourceKind),
			RegistrySourceURL:   copyOptionalString(item.RegistrySourceURL),
			OwnerHandle:         copyOptionalString(item.RegistryOwnerHandle),
			DetailURL:           copyOptionalString(item.RegistryDetailURL),
			ScanStatus:          normalizedScanStatus(item.ScanStatus),
			ScanHasWarnings:     item.ScanHasWarnings,
			ScanCheckedAt:       copyOptionalTime(item.ScanCheckedAt),
			ScanEngine:          copyOptionalString(item.ScanEngine),
			ScanSummary:         copyOptionalString(item.ScanSummary),
			ModerationVerdict:   copyOptionalString(item.ModerationVerdict),
		})
	}
	return out
}

func toSkillPackageResponse(item data.SkillPackage) skillPackageResponse {
	return skillPackageResponse{
		SkillKey:            item.SkillKey,
		Version:             item.Version,
		DisplayName:         item.DisplayName,
		Description:         item.Description,
		InstructionPath:     item.InstructionPath,
		ManifestKey:         item.ManifestKey,
		BundleKey:           item.BundleKey,
		Platforms:           append([]string(nil), item.Platforms...),
		Source:              skillSource(item.RegistryProvider, item.RegistrySourceKind),
		RegistryProvider:    copyOptionalString(item.RegistryProvider),
		RegistrySlug:        copyOptionalString(item.RegistrySlug),
		RegistryOwnerHandle: copyOptionalString(item.RegistryOwnerHandle),
		RegistryVersion:     copyOptionalString(item.RegistryVersion),
		RegistryDetailURL:   copyOptionalString(item.RegistryDetailURL),
		RegistryDownloadURL: copyOptionalString(item.RegistryDownloadURL),
		RegistrySourceKind:  copyOptionalString(item.RegistrySourceKind),
		RegistrySourceURL:   copyOptionalString(item.RegistrySourceURL),
		OwnerHandle:         copyOptionalString(item.RegistryOwnerHandle),
		DetailURL:           copyOptionalString(item.RegistryDetailURL),
		RepositoryURL:       copyOptionalString(item.RegistrySourceURL),
		ScanStatus:          normalizedScanStatus(item.ScanStatus),
		ScanHasWarnings:     item.ScanHasWarnings,
		ScanCheckedAt:       copyOptionalTime(item.ScanCheckedAt),
		ScanEngine:          copyOptionalString(item.ScanEngine),
		ScanSummary:         copyOptionalString(item.ScanSummary),
		ModerationVerdict:   copyOptionalString(item.ModerationVerdict),
		IsActive:            item.IsActive,
	}
}

func copyOptionalString(value *string) *string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func copyOptionalTime(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

func skillSource(registryProvider *string, sourceKind *string) string {
	if registryProvider != nil && strings.TrimSpace(*registryProvider) != "" {
		return "official"
	}
	if sourceKind != nil && strings.EqualFold(strings.TrimSpace(*sourceKind), "github") {
		return "github"
	}
	return "custom"
}

func loadSkillPackageManifest(ctx context.Context, store skillStore, skillKey, version string) (skillstore.PackageManifest, error) {
	manifestKey := skillstore.DerivedManifestKey(skillKey, version)
	bundleKey := skillstore.DerivedBundleKey(skillKey, version)
	if _, err := store.Head(ctx, manifestKey); err != nil {
		return skillstore.PackageManifest{}, err
	}
	if _, err := store.Head(ctx, bundleKey); err != nil {
		return skillstore.PackageManifest{}, err
	}
	manifestBytes, err := store.Get(ctx, manifestKey)
	if err != nil {
		return skillstore.PackageManifest{}, err
	}
	manifest, err := skillstore.DecodeManifest(manifestBytes)
	if err != nil {
		return skillstore.PackageManifest{}, err
	}
	bundleBytes, err := store.Get(ctx, bundleKey)
	if err != nil {
		return skillstore.PackageManifest{}, err
	}
	bundle, err := skillstore.DecodeBundle(bundleBytes)
	if err != nil {
		return skillstore.PackageManifest{}, err
	}
	if err := skillstore.ValidateBundleAgainstManifest(manifest, bundle); err != nil {
		return skillstore.PackageManifest{}, err
	}
	return manifest, nil
}

func parseSkillPackagePath(w nethttp.ResponseWriter, traceID string, raw string, prefix string) (string, string, bool) {
	tail := strings.TrimSpace(strings.TrimPrefix(raw, prefix))
	parts := strings.Split(tail, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		httpkit.WriteError(w, nethttp.StatusBadRequest, "skills.invalid_path", "invalid skill path", traceID, nil)
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

func syncProfileSkillRefs(ctx context.Context, installsRepo *data.ProfileSkillInstallsRepository, profileRepo *data.ProfileRegistriesRepository, accountID uuid.UUID, profileRef string) error {
	items, err := installsRepo.ListByProfile(ctx, accountID, profileRef)
	if err != nil {
		return err
	}
	refs := make([]string, 0, len(items))
	for _, item := range items {
		refs = append(refs, item.SkillKey+"@"+item.Version)
	}
	return profileRepo.UpdateInstalledSkillRefs(ctx, profileRef, refs)
}

func syncWorkspaceSkillRefs(ctx context.Context, repo *data.WorkspaceSkillEnablementsRepository, workspaceRepo *data.WorkspaceRegistriesRepository, accountID uuid.UUID, workspaceRef string) error {
	items, err := repo.ListByWorkspace(ctx, accountID, workspaceRef)
	if err != nil {
		return err
	}
	refs := make([]string, 0, len(items))
	for _, item := range items {
		refs = append(refs, item.SkillKey+"@"+item.Version)
	}
	return workspaceRepo.UpdateEnabledSkillRefs(ctx, workspaceRef, refs)
}

func writeSkillStoreError(w nethttp.ResponseWriter, traceID string, err error) {
	if objectstore.IsNotFound(err) {
		httpkit.WriteError(w, nethttp.StatusNotFound, "skills.bundle_not_found", "skill package artifacts not found", traceID, nil)
		return
	}
	httpkit.WriteError(w, nethttp.StatusBadRequest, "skills.invalid_manifest", err.Error(), traceID, nil)
}

func writeMethodNotAllowedJSON(w nethttp.ResponseWriter, traceID string) {
	httpkit.WriteError(w, nethttp.StatusMethodNotAllowed, "http.method_not_allowed", "method not allowed", traceID, nil)
}
