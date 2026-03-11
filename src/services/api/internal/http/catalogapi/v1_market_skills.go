package catalogapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	"context"
	"encoding/json"
	"fmt"
	"io"
	nethttp "net/http"
	"regexp"
	"strings"
	"time"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"
	sharedenvironmentref "arkloop/services/shared/environmentref"
	sharedoutbound "arkloop/services/shared/outboundurl"
	"github.com/google/uuid"
)

const (
	skillsMPAPIKeySetting  = "skills.market.skillsmp_api_key"
	skillsMPBaseURLSetting = "skills.market.skillsmp_base_url"
)

type marketSkillResponse struct {
	SkillKey          string         `json:"skill_key"`
	Version           string         `json:"version,omitempty"`
	DisplayName       string         `json:"display_name"`
	Description       *string        `json:"description,omitempty"`
	Source            string         `json:"source"`
	UpdatedAt         *string        `json:"updated_at,omitempty"`
	DetailURL         *string        `json:"detail_url,omitempty"`
	RepositoryURL     *string        `json:"repository_url,omitempty"`
	Installed         bool           `json:"installed"`
	EnabledByDefault  bool           `json:"enabled_by_default"`
	RegistryProvider  *string        `json:"registry_provider,omitempty"`
	RegistrySlug      *string        `json:"registry_slug,omitempty"`
	OwnerHandle       *string        `json:"owner_handle,omitempty"`
	Stats             map[string]any `json:"stats,omitempty"`
	ScanStatus        string         `json:"scan_status,omitempty"`
	ScanHasWarnings   bool           `json:"scan_has_warnings,omitempty"`
	ScanSummary       *string        `json:"scan_summary,omitempty"`
	ModerationVerdict *string        `json:"moderation_verdict,omitempty"`
}

type marketSkillImportRequest struct {
	Slug          string `json:"slug"`
	Version       string `json:"version"`
	SkillKey      string `json:"skill_key"`
	DetailURL     string `json:"detail_url"`
	RepositoryURL string `json:"repository_url"`
}

type skillStateKeys struct {
	ByPackage  map[string]struct{}
	ByRegistry map[string]struct{}
}

func (s skillStateKeys) Has(item registrySkillSearchItem) bool {
	if item.RegistrySlug != "" && item.Version != "" {
		if _, ok := s.ByRegistry[item.RegistrySlug+"@"+item.Version]; ok {
			return true
		}
	}
	if item.SkillKey != "" && item.Version != "" {
		if _, ok := s.ByPackage[item.SkillKey+"@"+item.Version]; ok {
			return true
		}
	}
	return false
}

func marketSkillsEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	settingsRepo *data.PlatformSettingsRepository,
	installsRepo *data.ProfileSkillInstallsRepository,
	profileRepo *data.ProfileRegistriesRepository,
	enableRepo *data.WorkspaceSkillEnablementsRepository,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if installsRepo == nil || profileRepo == nil || enableRepo == nil {
			httpkit.WriteError(w, nethttp.StatusServiceUnavailable, "skills.market.not_configured", "skills market not configured", traceID, nil)
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
		cfg, err := loadRegistryConfig(r.Context(), settingsRepo)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		provider, err := newRegistryProvider(cfg)
		if err != nil {
			writeSkillsMarketError(w, traceID, err)
			return
		}
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" {
			httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]any{"items": []marketSkillResponse{}})
			return
		}
		items, err := provider.Search(r.Context(), query, r.URL.Query().Get("page"), r.URL.Query().Get("limit"), r.URL.Query().Get("sortBy"))
		if err != nil {
			writeSkillsMarketError(w, traceID, err)
			return
		}
		installed, defaults, err := loadSkillState(r.Context(), actor.OrgID, actor.UserID, installsRepo, profileRepo, enableRepo)
		if err != nil {
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
			return
		}
		responses := make([]marketSkillResponse, 0, len(items))
		for _, item := range items {
			responses = append(responses, marketSkillResponse{
				SkillKey:          item.SkillKey,
				Version:           item.Version,
				DisplayName:       item.DisplayName,
				Description:       stringPtrOrNil(item.Description),
				Source:            "official",
				UpdatedAt:         stringPtrOrNil(item.UpdatedAt),
				DetailURL:         stringPtrOrNil(item.DetailURL),
				RepositoryURL:     stringPtrOrNil(item.RepositoryURL),
				Installed:         installed.Has(item),
				EnabledByDefault:  defaults.Has(item),
				RegistryProvider:  stringPtrOrNil(item.RegistryProvider),
				RegistrySlug:      stringPtrOrNil(item.RegistrySlug),
				OwnerHandle:       stringPtrOrNil(item.OwnerHandle),
				Stats:             item.Stats,
				ScanStatus:        normalizedScanStatus(item.ScanStatus),
				ScanHasWarnings:   item.ScanHasWarnings,
				ScanSummary:       stringPtrOrNil(item.ScanSummary),
				ModerationVerdict: stringPtrOrNil(item.ModerationVerdict),
			})
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusOK, map[string]any{"items": responses})
	}
}

func marketSkillsImportEntry(
	authService *auth.Service,
	membershipRepo *data.OrgMembershipRepository,
	apiKeysRepo *data.APIKeysRepository,
	auditWriter *audit.Writer,
	settingsRepo *data.PlatformSettingsRepository,
	packagesRepo *data.SkillPackagesRepository,
	store skillStore,
) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		traceID := observability.TraceIDFromContext(r.Context())
		if packagesRepo == nil || store == nil {
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
		var req marketSkillImportRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpkit.WriteError(w, nethttp.StatusBadRequest, "skills.invalid_request", "invalid JSON body", traceID, nil)
			return
		}
		slug := strings.TrimSpace(req.Slug)
		if slug == "" {
			slug = strings.TrimSpace(req.SkillKey)
		}
		repositoryURL := strings.TrimSpace(req.RepositoryURL)
		if slug != "" {
			cfg, err := loadRegistryConfig(r.Context(), settingsRepo)
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
				return
			}
			provider, err := newRegistryProvider(cfg)
			if err == nil {
				item, candidates, importErr := importSkillFromRegistry(r.Context(), provider, store, packagesRepo, actor.OrgID, slug, strings.TrimSpace(req.Version))
				if importErr == nil {
					httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toSkillPackageResponse(item))
					return
				}
				if repositoryURL == "" {
					writeSkillImportErrorWithCandidates(w, traceID, importErr, candidates)
					return
				}
			}
		}
		if repositoryURL == "" {
			var err error
			repositoryURL, err = resolveRepositoryURLFromDetailPage(r.Context(), strings.TrimSpace(req.DetailURL))
			if err != nil {
				httpkit.WriteError(w, nethttp.StatusBadRequest, "skills.market.repository_missing", "skill repository not found", traceID, nil)
				return
			}
		}
		item, candidates, err := importSkillFromGitHub(r.Context(), store, packagesRepo, actor.OrgID, repositoryURL, "", "", strings.TrimSpace(req.SkillKey))
		if err != nil {
			writeSkillImportErrorWithCandidates(w, traceID, err, candidates)
			return
		}
		_ = packagesRepo.UpdateRegistryMetadata(r.Context(), actor.OrgID, item.SkillKey, item.Version, data.SkillPackageRegistryMetadata{
			RegistrySourceKind: "github",
			RegistrySourceURL:  repositoryURL,
		})
		fresh, getErr := packagesRepo.Get(r.Context(), actor.OrgID, item.SkillKey, item.Version)
		if getErr == nil && fresh != nil {
			item = *fresh
		}
		httpkit.WriteJSON(w, traceID, nethttp.StatusCreated, toSkillPackageResponse(item))
	}
}

func importSkillFromRegistry(ctx context.Context, provider registryProvider, store skillStore, packagesRepo *data.SkillPackagesRepository, orgID uuid.UUID, slug, version string) (data.SkillPackage, []skillImportCandidate, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return data.SkillPackage{}, nil, skillImportError{status: nethttp.StatusBadRequest, code: "skills.invalid_request", msg: "slug is required"}
	}
	skillInfo, err := provider.GetSkill(ctx, slug)
	if err != nil {
		return data.SkillPackage{}, nil, skillImportError{status: nethttp.StatusBadGateway, code: "skills.market_unavailable", msg: "skills market unavailable"}
	}
	targetVersion := strings.TrimSpace(version)
	if targetVersion == "" {
		targetVersion = strings.TrimSpace(skillInfo.LatestVersion)
	}
	if targetVersion == "" {
		versions, versionsErr := provider.ListVersions(ctx, slug)
		if versionsErr == nil && len(versions) > 0 {
			targetVersion = strings.TrimSpace(versions[0].Version)
		}
	}
	if targetVersion == "" {
		return data.SkillPackage{}, nil, skillImportError{status: nethttp.StatusNotFound, code: "skills.import_not_found", msg: "skill package not found"}
	}
	versionInfo, err := provider.GetVersion(ctx, slug, targetVersion)
	if err != nil {
		return data.SkillPackage{}, nil, skillImportError{status: nethttp.StatusBadGateway, code: "skills.market_unavailable", msg: "skills market unavailable"}
	}
	payload, downloadURL, err := provider.DownloadBundle(ctx, slug, targetVersion)
	if err != nil {
		if strings.EqualFold(strings.TrimSpace(versionInfo.SourceKind), "github") && strings.TrimSpace(versionInfo.SourceURL) != "" {
			item, candidates, importErr := importSkillFromGitHub(ctx, store, packagesRepo, orgID, strings.TrimSpace(versionInfo.SourceURL), "", "", "")
			if importErr != nil {
				return data.SkillPackage{}, candidates, importErr
			}
			metadata := registryMetadataFromRegistry(skillInfo, versionInfo, downloadURL)
			if updateErr := packagesRepo.UpdateRegistryMetadata(ctx, orgID, item.SkillKey, item.Version, metadata); updateErr != nil {
				return data.SkillPackage{}, nil, updateErr
			}
			if fresh, getErr := packagesRepo.Get(ctx, orgID, item.SkillKey, item.Version); getErr == nil && fresh != nil {
				item = *fresh
			}
			return item, nil, nil
		}
		return data.SkillPackage{}, nil, skillImportError{status: nethttp.StatusBadGateway, code: "skills.market_download_failed", msg: "skill bundle download failed"}
	}
	item, candidates, err := importSkillFromUploadData(ctx, store, packagesRepo, orgID, slug+"-"+targetVersion+".zip", payload)
	if err != nil {
		return data.SkillPackage{}, candidates, err
	}
	metadata := registryMetadataFromRegistry(skillInfo, versionInfo, downloadURL)
	if updateErr := packagesRepo.UpdateRegistryMetadata(ctx, orgID, item.SkillKey, item.Version, metadata); updateErr != nil {
		return data.SkillPackage{}, nil, updateErr
	}
	fresh, getErr := packagesRepo.Get(ctx, orgID, item.SkillKey, item.Version)
	if getErr == nil && fresh != nil {
		item = *fresh
	}
	return item, nil, nil
}

func registryMetadataFromRegistry(skillInfo registrySkillInfo, versionInfo registryVersionInfo, downloadURL string) data.SkillPackageRegistryMetadata {
	snapshot := map[string]any{
		"provider":     defaultRegistryProvider,
		"slug":         skillInfo.Slug,
		"owner_handle": skillInfo.OwnerHandle,
		"version":      versionInfo.Version,
		"download_url": downloadURL,
		"source_kind":  versionInfo.SourceKind,
		"source_url":   versionInfo.SourceURL,
		"scan":         versionInfo.ScanSnapshot,
		"moderation":   skillInfo.ModerationSnapshot,
	}
	return data.SkillPackageRegistryMetadata{
		RegistryProvider:    defaultRegistryProvider,
		RegistrySlug:        skillInfo.Slug,
		RegistryOwnerHandle: skillInfo.OwnerHandle,
		RegistryVersion:     versionInfo.Version,
		RegistryDetailURL:   skillInfo.DetailURL,
		RegistryDownloadURL: downloadURL,
		RegistrySourceKind:  versionInfo.SourceKind,
		RegistrySourceURL:   firstNonEmpty(versionInfo.SourceURL, versionInfo.RepositoryURL, skillInfo.RepositoryURL),
		ScanStatus:          normalizedScanStatus(versionInfo.ScanStatus),
		ScanHasWarnings:     versionInfo.ScanHasWarnings,
		ScanCheckedAt:       versionInfo.ScanCheckedAt,
		ScanEngine:          versionInfo.ScanEngine,
		ScanSummary:         firstNonEmpty(versionInfo.ScanSummary, skillInfo.ModerationSummary),
		ModerationVerdict:   skillInfo.ModerationVerdict,
		ScanSnapshotJSON:    snapshot,
	}
}

func loadSkillState(ctx context.Context, orgID, userID uuid.UUID, installsRepo *data.ProfileSkillInstallsRepository, profileRepo *data.ProfileRegistriesRepository, enableRepo *data.WorkspaceSkillEnablementsRepository) (skillStateKeys, skillStateKeys, error) {
	profileRef := sharedenvironmentref.BuildProfileRef(orgID, &userID)
	installedItems, err := installsRepo.ListByProfile(ctx, orgID, profileRef)
	if err != nil {
		return skillStateKeys{}, skillStateKeys{}, err
	}
	installed := skillStateKeys{ByPackage: map[string]struct{}{}, ByRegistry: map[string]struct{}{}}
	for _, item := range installedItems {
		installed.ByPackage[item.SkillKey+"@"+item.Version] = struct{}{}
		if item.RegistrySlug != nil && item.RegistryVersion != nil && strings.TrimSpace(*item.RegistrySlug) != "" && strings.TrimSpace(*item.RegistryVersion) != "" {
			installed.ByRegistry[strings.TrimSpace(*item.RegistrySlug)+"@"+strings.TrimSpace(*item.RegistryVersion)] = struct{}{}
		}
	}
	defaults := skillStateKeys{ByPackage: map[string]struct{}{}, ByRegistry: map[string]struct{}{}}
	profile, err := profileRepo.Get(ctx, profileRef)
	if err != nil {
		return skillStateKeys{}, skillStateKeys{}, err
	}
	if profile != nil && profile.DefaultWorkspaceRef != nil && strings.TrimSpace(*profile.DefaultWorkspaceRef) != "" {
		defaultItems, err := enableRepo.ListByWorkspace(ctx, orgID, strings.TrimSpace(*profile.DefaultWorkspaceRef))
		if err != nil {
			return skillStateKeys{}, skillStateKeys{}, err
		}
		for _, item := range defaultItems {
			defaults.ByPackage[item.SkillKey+"@"+item.Version] = struct{}{}
			if item.RegistrySlug != nil && item.RegistryVersion != nil && strings.TrimSpace(*item.RegistrySlug) != "" && strings.TrimSpace(*item.RegistryVersion) != "" {
				defaults.ByRegistry[strings.TrimSpace(*item.RegistrySlug)+"@"+strings.TrimSpace(*item.RegistryVersion)] = struct{}{}
			}
		}
	}
	return installed, defaults, nil
}

func resolveRepositoryURLFromDetailPage(ctx context.Context, detailURL string) (string, error) {
	detailURL = strings.TrimSpace(detailURL)
	if detailURL == "" {
		return "", fmt.Errorf("detail url is required")
	}
	if err := sharedoutbound.DefaultPolicy().ValidateRequestURL(detailURL); err != nil {
		return "", err
	}
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, detailURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Arkloop/skills-market")
	resp, err := sharedoutbound.DefaultPolicy().NewHTTPClient(20 * time.Second).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`"repository"\s*:\s*"([^"]+)"`),
		regexp.MustCompile(`"repository_url"\s*:\s*"([^"]+)"`),
		regexp.MustCompile(`https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+`),
	}
	for _, pattern := range patterns {
		matches := pattern.FindSubmatch(body)
		if len(matches) > 1 {
			return strings.TrimSpace(string(matches[1])), nil
		}
		if match := pattern.Find(body); len(match) > 0 {
			return strings.TrimSpace(string(match)), nil
		}
	}
	return "", fmt.Errorf("repository not found")
}

func writeSkillsMarketError(w nethttp.ResponseWriter, traceID string, err error) {
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(message, "403") || strings.Contains(message, "401") {
		httpkit.WriteError(w, nethttp.StatusBadGateway, "skills.market_auth_failed", "skills market auth failed", traceID, nil)
		return
	}
	httpkit.WriteError(w, nethttp.StatusBadGateway, "skills.market_unavailable", "skills market unavailable", traceID, nil)
}

func stringPtrOrNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	copied := trimmed
	return &copied
}
