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
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type defaultSkillsReplaceRequest struct {
	Skills []skillReferenceRequest `json:"skills"`
}

type defaultSkillResponse struct {
	SkillKey            string  `json:"skill_key"`
	Version             string  `json:"version"`
	DisplayName         string  `json:"display_name"`
	Description         *string `json:"description,omitempty"`
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

func profileDefaultSkillsEntry(
	authService *auth.Service,
	membershipRepo *data.AccountMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	packagesRepo *data.SkillPackagesRepository,
	installsRepo *data.ProfileSkillInstallsRepository,
	enableRepo *data.WorkspaceSkillEnablementsRepository,
	profileRepo *data.ProfileRegistriesRepository,
	workspaceRepo *data.WorkspaceRegistriesRepository,
	pool data.DB,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if packagesRepo == nil || installsRepo == nil || enableRepo == nil || profileRepo == nil || workspaceRepo == nil || pool == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "skills.not_configured", "skills not configured", traceID, nil)
			return
		}
		actor, ok := httpkit.ResolveActor(w, r, traceID, authService, membershipRepo, apiKeysRepo, auditWriter)
		if !ok {
			return
		}
		profileRef := sharedenvironmentref.BuildProfileRef(actor.AccountID, &actor.UserID)
		workspaceRef, err := ensureDefaultWorkspaceForProfile(r.Context(), profileRepo, workspaceRepo, actor.AccountID, actor.UserID, profileRef)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
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
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]any{"items": toDefaultSkillResponses(items)})
		case nethttp.MethodPut:
			if !httpkit.RequirePerm(actor, auth.PermDataPersonasManage, w, traceID) {
				return
			}
			var req defaultSkillsReplaceRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				httpkit.WriteError(w, nethttp.StatusBadRequest, "skills.invalid_request", "invalid JSON body", traceID, nil)
				return
			}
			items, err := validateInstalledSkillReferences(r.Context(), actor.AccountID, profileRef, req.Skills, packagesRepo, installsRepo)
			if err != nil {
				writeSkillValidationError(w, traceID, err)
				return
			}
			tx, err := pool.BeginTx(r.Context(), pgx.TxOptions{})
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			defer func() { _ = tx.Rollback(r.Context()) }()
			targetWorkspaceRefs, err := replaceDefaultSkillsAcrossBoundWorkspaces(r.Context(), tx, enableRepo, actor.AccountID, actor.UserID, profileRef, workspaceRef, items)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			if err := tx.Commit(r.Context()); err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			for _, targetWorkspaceRef := range targetWorkspaceRefs {
				if err := syncWorkspaceSkillRefs(r.Context(), enableRepo, workspaceRepo, actor.AccountID, targetWorkspaceRef); err != nil {
					httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
					return
				}
			}
			fresh, err := enableRepo.ListByWorkspace(r.Context(), actor.AccountID, workspaceRef)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]any{"items": toDefaultSkillResponses(fresh)})
		default:
			writeMethodNotAllowedJSON(w, traceID)
		}
	}
}

func replaceDefaultSkillsAcrossBoundWorkspaces(
	ctx context.Context,
	tx pgx.Tx,
	enableRepo *data.WorkspaceSkillEnablementsRepository,
	accountID uuid.UUID,
	userID uuid.UUID,
	profileRef string,
	defaultWorkspaceRef string,
	items []data.WorkspaceSkillEnablement,
) ([]string, error) {
	targetWorkspaceRefs, err := loadProfileBoundWorkspaceRefsTx(ctx, tx, accountID, profileRef, defaultWorkspaceRef)
	if err != nil {
		return nil, err
	}
	for _, targetWorkspaceRef := range targetWorkspaceRefs {
		if err := enableRepo.Replace(ctx, tx, accountID, targetWorkspaceRef, userID, items); err != nil {
			return nil, err
		}
	}
	return targetWorkspaceRefs, nil
}

func loadProfileBoundWorkspaceRefsTx(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, profileRef string, defaultWorkspaceRef string) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if tx == nil {
		return nil, errors.New("tx must not be nil")
	}
	if accountID == uuid.Nil {
		return nil, errors.New("account_id must not be empty")
	}
	profileRef = strings.TrimSpace(profileRef)
	if profileRef == "" {
		return nil, errors.New("profile_ref must not be empty")
	}

	refs := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)
	addRef := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		refs = append(refs, value)
	}

	addRef(defaultWorkspaceRef)

	rows, err := tx.Query(
		ctx,
		`SELECT workspace_ref
		   FROM default_workspace_bindings
		  WHERE account_id = $1 AND profile_ref = $2
		  ORDER BY created_at ASC`,
		accountID,
		profileRef,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var workspaceRef string
		if err := rows.Scan(&workspaceRef); err != nil {
			return nil, err
		}
		addRef(workspaceRef)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return refs, nil
}

type skillValidationError struct {
	code string
	msg  string
}

func (e skillValidationError) Error() string { return e.msg }

func validateInstalledSkillReferences(
	ctx context.Context,
	accountID uuid.UUID,
	profileRef string,
	reqs []skillReferenceRequest,
	packagesRepo *data.SkillPackagesRepository,
	installsRepo *data.ProfileSkillInstallsRepository,
) ([]data.WorkspaceSkillEnablement, error) {
	items := make([]data.WorkspaceSkillEnablement, 0, len(reqs))
	for _, item := range reqs {
		skillKey := strings.TrimSpace(item.SkillKey)
		version := strings.TrimSpace(item.Version)
		pkg, err := packagesRepo.Get(ctx, accountID, skillKey, version)
		if err != nil {
			return nil, err
		}
		if pkg == nil || !pkg.IsActive {
			return nil, skillValidationError{code: "skills.not_found", msg: "skill package not found"}
		}
		installed, err := installsRepo.IsInstalled(ctx, accountID, profileRef, skillKey, version)
		if err != nil {
			return nil, err
		}
		if !installed {
			return nil, skillValidationError{code: "skills.not_installed", msg: "skill is not installed for this profile"}
		}
		items = append(items, data.WorkspaceSkillEnablement{SkillKey: skillKey, Version: version})
	}
	return items, nil
}

func writeSkillValidationError(w nethttp.ResponseWriter, traceID string, err error) {
	var validation skillValidationError
	if ok := errorAs(err, &validation); ok {
		status := nethttp.StatusBadRequest
		if validation.code == "skills.not_found" {
			status = nethttp.StatusNotFound
		}
		httpkit.WriteError(w, status, validation.code, validation.msg, traceID, nil)
		return
	}
	httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
}

func ensureDefaultWorkspaceForProfile(ctx context.Context, profileRepo *data.ProfileRegistriesRepository, workspaceRepo *data.WorkspaceRegistriesRepository, accountID, userID uuid.UUID, profileRef string) (string, error) {
	if err := profileRepo.Ensure(ctx, profileRef, accountID, userID); err != nil {
		return "", err
	}
	profile, err := profileRepo.Get(ctx, profileRef)
	if err != nil {
		return "", err
	}
	if profile != nil && profile.DefaultWorkspaceRef != nil && strings.TrimSpace(*profile.DefaultWorkspaceRef) != "" {
		workspaceRef := strings.TrimSpace(*profile.DefaultWorkspaceRef)
		if err := workspaceRepo.Ensure(ctx, workspaceRef, accountID, userID, nil); err != nil {
			return "", err
		}
		return workspaceRef, nil
	}
	workspaceRef := newDefaultWorkspaceRef()
	if err := workspaceRepo.Ensure(ctx, workspaceRef, accountID, userID, nil); err != nil {
		return "", err
	}
	if err := profileRepo.SetDefaultWorkspaceRef(ctx, profileRef, workspaceRef); err != nil {
		return "", err
	}
	return workspaceRef, nil
}

func newDefaultWorkspaceRef() string {
	return "wsref_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func toDefaultSkillResponses(items []data.WorkspaceSkillEnablement) []defaultSkillResponse {
	out := make([]defaultSkillResponse, 0, len(items))
	for _, item := range items {
		out = append(out, defaultSkillResponse{
			SkillKey:            item.SkillKey,
			Version:             item.Version,
			DisplayName:         item.DisplayName,
			Description:         item.Description,
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

func errorAs(err error, target any) bool {
	return errors.As(err, target)
}
