package catalogapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	nethttp "net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"arkloop/services/api/internal/data"
	sharedoutbound "arkloop/services/shared/outboundurl"
)

const (
	registryProviderSetting   = "skills.registry.provider"
	registryBaseURLSetting    = "skills.registry.base_url"
	registryAPIBaseURLSetting = "skills.registry.api_base_url"
	registryAPIKeySetting     = "skills.registry.api_key"
	defaultRegistryProvider   = "clawhub"
	defaultRegistryBaseURL    = "https://clawhub.ai"
)

var clawHubDownloadBaseURLCache sync.Map

type skillsRegistryConfig struct {
	Provider   string
	BaseURL    string
	APIBaseURL string
	APIKey     string
}

type registryStats struct {
	Comments        int64 `json:"comments,omitempty"`
	Downloads       int64 `json:"downloads,omitempty"`
	InstallsAllTime int64 `json:"installs_all_time,omitempty"`
	InstallsCurrent int64 `json:"installs_current,omitempty"`
	Stars           int64 `json:"stars,omitempty"`
	Versions        int64 `json:"versions,omitempty"`
}

func (s registryStats) toMap() map[string]any {
	return map[string]any{
		"comments":          s.Comments,
		"downloads":         s.Downloads,
		"installs_all_time": s.InstallsAllTime,
		"installs_current":  s.InstallsCurrent,
		"stars":             s.Stars,
		"versions":          s.Versions,
	}
}

type registrySkillSearchItem struct {
	SkillKey          string
	Version           string
	DisplayName       string
	Description       string
	UpdatedAt         string
	DetailURL         string
	RepositoryURL     string
	RegistryProvider  string
	RegistrySlug      string
	OwnerHandle       string
	Stats             map[string]any
	ScanStatus        string
	ScanHasWarnings   bool
	ScanSummary       string
	ModerationVerdict string
}

type marketSkillSearchItem = registrySkillSearchItem

type registrySkillInfo struct {
	Slug               string
	DisplayName        string
	Summary            string
	OwnerHandle        string
	OwnerUserID        string
	LatestVersion      string
	UpdatedAt          string
	DetailURL          string
	RepositoryURL      string
	Stats              map[string]any
	ModerationVerdict  string
	ModerationSummary  string
	ModerationSnapshot map[string]any
}

type registryVersionInfo struct {
	Version         string
	DownloadURL     string
	RepositoryURL   string
	SourceKind      string
	SourceURL       string
	ScanStatus      string
	ScanHasWarnings bool
	ScanCheckedAt   *time.Time
	ScanEngine      string
	ScanSummary     string
	ScanSnapshot    map[string]any
}

type registryProvider interface {
	Search(ctx context.Context, query, page, limit, sortBy string) ([]registrySkillSearchItem, error)
	GetSkill(ctx context.Context, slug string) (registrySkillInfo, error)
	ListVersions(ctx context.Context, slug string) ([]registryVersionInfo, error)
	GetVersion(ctx context.Context, slug, version string) (registryVersionInfo, error)
	DownloadBundle(ctx context.Context, slug, version string) ([]byte, string, error)
}

func loadRegistryConfig(ctx context.Context, settingsRepo *data.PlatformSettingsRepository) (skillsRegistryConfig, error) {
	cfg := skillsRegistryConfig{
		Provider: defaultRegistryProvider,
		BaseURL:  defaultRegistryBaseURL,
	}
	if settingsRepo == nil {
		cfg.APIBaseURL = cfg.BaseURL
		return cfg, nil
	}
	if item, err := settingsRepo.Get(ctx, registryProviderSetting); err != nil {
		return skillsRegistryConfig{}, err
	} else if item != nil && strings.TrimSpace(item.Value) != "" {
		cfg.Provider = strings.TrimSpace(item.Value)
	}
	if item, err := settingsRepo.Get(ctx, registryBaseURLSetting); err != nil {
		return skillsRegistryConfig{}, err
	} else if item != nil && strings.TrimSpace(item.Value) != "" {
		cfg.BaseURL = strings.TrimRight(strings.TrimSpace(item.Value), "/")
	}
	if item, err := settingsRepo.Get(ctx, registryAPIBaseURLSetting); err != nil {
		return skillsRegistryConfig{}, err
	} else if item != nil && strings.TrimSpace(item.Value) != "" {
		cfg.APIBaseURL = strings.TrimRight(strings.TrimSpace(item.Value), "/")
	}
	if item, err := settingsRepo.Get(ctx, registryAPIKeySetting); err != nil {
		return skillsRegistryConfig{}, err
	} else if item != nil && strings.TrimSpace(item.Value) != "" {
		cfg.APIKey = strings.TrimSpace(item.Value)
	}
	if strings.TrimSpace(cfg.APIBaseURL) == "" {
		cfg.APIBaseURL = cfg.BaseURL
	}
	return cfg, nil
}

func newRegistryProvider(cfg skillsRegistryConfig) (registryProvider, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "", defaultRegistryProvider:
		return clawHubProvider{cfg: cfg}, nil
	default:
		return nil, fmt.Errorf("unsupported registry provider")
	}
}

type clawHubProvider struct {
	cfg skillsRegistryConfig
}

type clawHubSearchPayload struct {
	Results []struct {
		Slug        string  `json:"slug"`
		DisplayName string  `json:"displayName"`
		Summary     string  `json:"summary"`
		Version     *string `json:"version"`
		UpdatedAt   *int64  `json:"updatedAt"`
	} `json:"results"`
}

type clawHubSkillPayload struct {
	Skill *struct {
		Slug        string  `json:"slug"`
		DisplayName string  `json:"displayName"`
		Summary     *string `json:"summary"`
		Stats       *struct {
			Comments        int64 `json:"comments"`
			Downloads       int64 `json:"downloads"`
			InstallsAllTime int64 `json:"installsAllTime"`
			InstallsCurrent int64 `json:"installsCurrent"`
			Stars           int64 `json:"stars"`
			Versions        int64 `json:"versions"`
		} `json:"stats"`
		CreatedAt int64 `json:"createdAt"`
		UpdatedAt int64 `json:"updatedAt"`
	} `json:"skill"`
	LatestVersion *struct {
		Version   string `json:"version"`
		CreatedAt int64  `json:"createdAt"`
		Changelog string `json:"changelog"`
	} `json:"latestVersion"`
	Owner *struct {
		Handle string `json:"handle"`
		UserID string `json:"userId"`
	} `json:"owner"`
	Moderation map[string]any `json:"moderation"`
}

type clawHubVersionsPayload struct {
	Items []struct {
		Version   string `json:"version"`
		CreatedAt int64  `json:"createdAt"`
		Changelog string `json:"changelog"`
	} `json:"items"`
}

type clawHubVersionPayload struct {
	Skill *struct {
		Slug        string `json:"slug"`
		DisplayName string `json:"displayName"`
	} `json:"skill"`
	Version *struct {
		Version   string `json:"version"`
		CreatedAt int64  `json:"createdAt"`
		Changelog string `json:"changelog"`
		Source    *struct {
			Kind string `json:"kind"`
			URL  string `json:"url"`
		} `json:"source"`
		Security *struct {
			Status      string  `json:"status"`
			HasWarnings bool    `json:"hasWarnings"`
			CheckedAt   *int64  `json:"checkedAt"`
			Model       *string `json:"model"`
		} `json:"security"`
	} `json:"version"`
}

func (p clawHubProvider) Search(ctx context.Context, query, page, limit, sortBy string) ([]registrySkillSearchItem, error) {
	params := url.Values{}
	params.Set("q", strings.TrimSpace(query))
	requestedLimit := 12
	if parsed, err := strconv.Atoi(strings.TrimSpace(limit)); err == nil && parsed > 0 {
		requestedLimit = parsed
	}
	if requestedLimit > 20 {
		requestedLimit = 20
	}
	params.Set("limit", strconv.Itoa(requestedLimit))
	requestURL := strings.TrimRight(p.cfg.APIBaseURL, "/") + "/api/v1/search?" + params.Encode()
	var payload clawHubSearchPayload
	if err := p.loadJSON(ctx, requestURL, &payload); err != nil {
		requestURL = strings.TrimRight(p.cfg.APIBaseURL, "/") + "/api/search?" + params.Encode()
		if loadErr := p.loadJSON(ctx, requestURL, &payload); loadErr != nil {
			return nil, loadErr
		}
	}
	items := make([]registrySkillSearchItem, 0, len(payload.Results))
	for _, result := range payload.Results {
		slug := strings.TrimSpace(result.Slug)
		if slug == "" {
			continue
		}
		items = append(items, registrySkillSearchItem{
			SkillKey:         slug,
			Version:          strings.TrimSpace(stringValue(result.Version)),
			DisplayName:      firstNonEmpty(strings.TrimSpace(result.DisplayName), slug),
			Description:      strings.TrimSpace(result.Summary),
			UpdatedAt:        msToRFC3339(result.UpdatedAt),
			DetailURL:        strings.TrimRight(p.cfg.BaseURL, "/") + "/skills?focus=search&q=" + url.QueryEscape(slug),
			RegistryProvider: defaultRegistryProvider,
			RegistrySlug:     slug,
			ScanStatus:       "unknown",
		})
	}
	p.enrichSearchItems(ctx, items)
	return items, nil
}

func (p clawHubProvider) enrichSearchItems(ctx context.Context, items []registrySkillSearchItem) {
	if len(items) == 0 {
		return
	}
	enrichCount := len(items)
	if enrichCount > 6 {
		enrichCount = 6
	}
	enrichCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	type searchResult struct {
		index int
		item  registrySkillSearchItem
	}
	results := make(chan searchResult, enrichCount)
	semaphore := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for index := 0; index < enrichCount; index++ {
		item := items[index]
		wg.Add(1)
		go func(index int, item registrySkillSearchItem) {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
			case <-enrichCtx.Done():
				return
			}
			defer func() { <-semaphore }()
			enriched := p.enrichSearchItem(enrichCtx, item)
			select {
			case results <- searchResult{index: index, item: enriched}:
			case <-enrichCtx.Done():
			}
		}(index, item)
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	for result := range results {
		items[result.index] = result.item
	}
}

func (p clawHubProvider) enrichSearchItem(ctx context.Context, item registrySkillSearchItem) registrySkillSearchItem {
	slug := strings.TrimSpace(item.RegistrySlug)
	if slug == "" {
		slug = strings.TrimSpace(item.SkillKey)
	}
	if slug == "" {
		return item
	}
	skillInfo, err := p.GetSkill(ctx, slug)
	if err == nil {
		item.DisplayName = firstNonEmpty(skillInfo.DisplayName, item.DisplayName)
		item.Description = firstNonEmpty(skillInfo.Summary, item.Description)
		item.DetailURL = firstNonEmpty(skillInfo.DetailURL, item.DetailURL)
		item.OwnerHandle = firstNonEmpty(skillInfo.OwnerHandle, item.OwnerHandle)
		item.Stats = skillInfo.Stats
		item.ModerationVerdict = firstNonEmpty(skillInfo.ModerationVerdict, item.ModerationVerdict)
		if strings.TrimSpace(item.UpdatedAt) == "" {
			item.UpdatedAt = skillInfo.UpdatedAt
		}
		if strings.TrimSpace(item.Version) == "" {
			item.Version = skillInfo.LatestVersion
		}
	}
	if strings.TrimSpace(item.Version) == "" {
		return item
	}
	versionInfo, err := p.GetVersion(ctx, slug, item.Version)
	if err != nil {
		return item
	}
	item.ScanStatus = normalizedScanStatus(firstNonEmpty(versionInfo.ScanStatus, item.ScanStatus))
	item.ScanHasWarnings = versionInfo.ScanHasWarnings
	item.ScanSummary = firstNonEmpty(versionInfo.ScanSummary, skillInfo.ModerationSummary, item.ScanSummary)
	return item
}

func (p clawHubProvider) GetSkill(ctx context.Context, slug string) (registrySkillInfo, error) {
	requestURL := strings.TrimRight(p.cfg.APIBaseURL, "/") + "/api/v1/skills/" + url.PathEscape(strings.TrimSpace(slug))
	var payload clawHubSkillPayload
	if err := p.loadJSON(ctx, requestURL, &payload); err != nil {
		return registrySkillInfo{}, err
	}
	if payload.Skill == nil {
		return registrySkillInfo{}, fmt.Errorf("registry skill not found")
	}
	info := registrySkillInfo{
		Slug:        strings.TrimSpace(payload.Skill.Slug),
		DisplayName: strings.TrimSpace(payload.Skill.DisplayName),
		Summary:     strings.TrimSpace(stringValue(payload.Skill.Summary)),
		LatestVersion: strings.TrimSpace(stringValue(func() *string {
			if payload.LatestVersion == nil {
				return nil
			}
			return &payload.LatestVersion.Version
		}())),
		UpdatedAt: msToRFC3339(&payload.Skill.UpdatedAt),
	}
	if payload.Owner != nil {
		info.OwnerHandle = strings.TrimSpace(payload.Owner.Handle)
		info.OwnerUserID = strings.TrimSpace(payload.Owner.UserID)
	}
	info.DetailURL = p.buildDetailURL(info.OwnerHandle, info.Slug)
	if payload.Skill.Stats != nil {
		info.Stats = registryStats{
			Comments:        payload.Skill.Stats.Comments,
			Downloads:       payload.Skill.Stats.Downloads,
			InstallsAllTime: payload.Skill.Stats.InstallsAllTime,
			InstallsCurrent: payload.Skill.Stats.InstallsCurrent,
			Stars:           payload.Skill.Stats.Stars,
			Versions:        payload.Skill.Stats.Versions,
		}.toMap()
	}
	if len(payload.Moderation) > 0 {
		info.ModerationSnapshot = payload.Moderation
		info.ModerationVerdict = strings.TrimSpace(firstString(payload.Moderation, "verdict"))
		info.ModerationSummary = strings.TrimSpace(firstString(payload.Moderation, "summary", "legacyReason"))
	}
	return info, nil
}

func (p clawHubProvider) ListVersions(ctx context.Context, slug string) ([]registryVersionInfo, error) {
	requestURL := strings.TrimRight(p.cfg.APIBaseURL, "/") + "/api/v1/skills/" + url.PathEscape(strings.TrimSpace(slug)) + "/versions"
	var payload clawHubVersionsPayload
	if err := p.loadJSON(ctx, requestURL, &payload); err != nil {
		return nil, err
	}
	items := make([]registryVersionInfo, 0, len(payload.Items))
	for _, item := range payload.Items {
		items = append(items, registryVersionInfo{
			Version:     strings.TrimSpace(item.Version),
			DownloadURL: p.buildDownloadURL(slug, item.Version),
		})
	}
	return items, nil
}

func (p clawHubProvider) GetVersion(ctx context.Context, slug, version string) (registryVersionInfo, error) {
	requestURL := strings.TrimRight(p.cfg.APIBaseURL, "/") + "/api/v1/skills/" + url.PathEscape(strings.TrimSpace(slug)) + "/versions/" + url.PathEscape(strings.TrimSpace(version))
	var payload clawHubVersionPayload
	if err := p.loadJSON(ctx, requestURL, &payload); err != nil {
		return registryVersionInfo{}, err
	}
	if payload.Version == nil {
		return registryVersionInfo{}, fmt.Errorf("registry version not found")
	}
	info := registryVersionInfo{
		Version:     strings.TrimSpace(payload.Version.Version),
		DownloadURL: p.buildDownloadURL(slug, payload.Version.Version),
		ScanStatus:  "unknown",
	}
	if payload.Version.Source != nil {
		info.SourceKind = strings.TrimSpace(payload.Version.Source.Kind)
		info.SourceURL = strings.TrimSpace(payload.Version.Source.URL)
		if strings.EqualFold(info.SourceKind, "github") {
			info.RepositoryURL = info.SourceURL
		}
	}
	if payload.Version.Security != nil {
		info.ScanStatus = normalizedScanStatus(payload.Version.Security.Status)
		info.ScanHasWarnings = payload.Version.Security.HasWarnings
		info.ScanCheckedAt = msToTime(payload.Version.Security.CheckedAt)
		info.ScanEngine = strings.TrimSpace(stringValue(payload.Version.Security.Model))
		info.ScanSnapshot = map[string]any{
			"status":      info.ScanStatus,
			"hasWarnings": info.ScanHasWarnings,
			"checkedAt":   payload.Version.Security.CheckedAt,
			"model":       info.ScanEngine,
		}
	}
	return info, nil
}

func (p clawHubProvider) DownloadBundle(ctx context.Context, slug, version string) ([]byte, string, error) {
	requestURL := p.buildDownloadURL(slug, version)
	data, err := p.loadBytes(ctx, requestURL)
	if err == nil {
		return data, requestURL, nil
	}
	fallbackURL, resolveErr := p.resolveDownloadURLFromDetailPage(ctx, slug, version)
	if resolveErr != nil {
		return nil, requestURL, err
	}
	data, fallbackErr := p.loadBytes(ctx, fallbackURL)
	if fallbackErr != nil {
		return nil, fallbackURL, fallbackErr
	}
	return data, fallbackURL, nil
}

func (p clawHubProvider) buildDetailURL(ownerHandle, slug string) string {
	baseURL := strings.TrimRight(p.cfg.BaseURL, "/")
	if strings.TrimSpace(ownerHandle) == "" {
		return baseURL + "/skills?focus=search&q=" + url.QueryEscape(strings.TrimSpace(slug))
	}
	return baseURL + "/" + path.Clean(strings.Trim(strings.TrimSpace(ownerHandle), "/")) + "/" + url.PathEscape(strings.TrimSpace(slug))
}

func (p clawHubProvider) buildDownloadURL(slug, version string) string {
	params := url.Values{}
	params.Set("slug", strings.TrimSpace(slug))
	if strings.TrimSpace(version) != "" {
		params.Set("version", strings.TrimSpace(version))
	}
	return strings.TrimRight(p.cfg.APIBaseURL, "/") + "/api/v1/download?" + params.Encode()
}

func (p clawHubProvider) resolveDownloadURLFromDetailPage(ctx context.Context, slug, version string) (string, error) {
	skillInfo, err := p.GetSkill(ctx, slug)
	if err != nil {
		return "", err
	}
	detailURL := p.buildDetailURL(skillInfo.OwnerHandle, slug)
	body, err := p.loadBytes(ctx, detailURL)
	if err != nil {
		return "", err
	}
	raw := strings.ReplaceAll(string(body), "\u0026", "&")
	raw = strings.ReplaceAll(raw, "&amp;", "&")
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`https://[^"'\s]+/api/v1/download\?slug=` + regexp.QuoteMeta(strings.TrimSpace(slug)) + `(?:&version=[^"'\s]+)?`),
		regexp.MustCompile(`/api/v1/download\?slug=` + regexp.QuoteMeta(strings.TrimSpace(slug)) + `(?:&version=[^"'\s]+)?`),
	}
	for _, pattern := range patterns {
		match := pattern.FindString(raw)
		if strings.TrimSpace(match) == "" {
			continue
		}
		if strings.HasPrefix(match, "/") {
			match = strings.TrimRight(p.cfg.BaseURL, "/") + match
		}
		parsed, parseErr := url.Parse(match)
		if parseErr != nil {
			continue
		}
		queryValues := parsed.Query()
		if strings.TrimSpace(queryValues.Get("version")) == "" && strings.TrimSpace(version) != "" {
			queryValues.Set("version", strings.TrimSpace(version))
			parsed.RawQuery = queryValues.Encode()
		}
		return parsed.String(), nil
	}
	if resolved := p.cachedExternalDownloadURL(slug, version); resolved != "" {
		return resolved, nil
	}
	for _, assetURL := range extractClawHubAssetURLs(raw, p.cfg.BaseURL) {
		assetBody, assetErr := p.loadBytes(ctx, assetURL)
		if assetErr != nil {
			continue
		}
		downloadBaseURL := extractClawHubDownloadBaseURL(string(assetBody))
		if strings.TrimSpace(downloadBaseURL) == "" {
			continue
		}
		clawHubDownloadBaseURLCache.Store(strings.TrimRight(p.cfg.BaseURL, "/"), downloadBaseURL)
		return buildExternalDownloadURL(downloadBaseURL, slug, version), nil
	}
	return "", fmt.Errorf("registry download url not found")
}

func (p clawHubProvider) cachedExternalDownloadURL(slug, version string) string {
	raw, ok := clawHubDownloadBaseURLCache.Load(strings.TrimRight(p.cfg.BaseURL, "/"))
	if !ok {
		return ""
	}
	baseURL, ok := raw.(string)
	if !ok || strings.TrimSpace(baseURL) == "" {
		return ""
	}
	return buildExternalDownloadURL(baseURL, slug, version)
}

func buildExternalDownloadURL(baseURL, slug, version string) string {
	params := url.Values{}
	params.Set("slug", strings.TrimSpace(slug))
	if strings.TrimSpace(version) != "" {
		params.Set("version", strings.TrimSpace(version))
	}
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/api/v1/download?" + params.Encode()
}

func extractClawHubAssetURLs(rawHTML, baseURL string) []string {
	pattern := regexp.MustCompile(`/assets/[A-Za-z0-9._-]+\.js`)
	matches := pattern.FindAllString(rawHTML, -1)
	urls := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if _, ok := seen[match]; ok {
			continue
		}
		seen[match] = struct{}{}
		if strings.HasPrefix(match, "/") {
			urls = append(urls, strings.TrimRight(baseURL, "/")+match)
			continue
		}
		urls = append(urls, match)
	}
	return urls
}

func extractClawHubDownloadBaseURL(rawJS string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile("https://[^\"'\\s`]+/api/v1/download\\?slug=\\$\\{[^}]+\\}(?:&version=\\$\\{[^}]+\\})?"),
		regexp.MustCompile("https://[^\"'\\s`]+/api/v1/download\\?slug=[^\"'\\s`]+"),
	}
	for _, pattern := range patterns {
		match := pattern.FindString(rawJS)
		if strings.TrimSpace(match) == "" {
			continue
		}
		parsed, err := url.Parse(match)
		if err != nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
			continue
		}
		return parsed.Scheme + "://" + parsed.Host
	}
	return ""
}

func (p clawHubProvider) loadJSON(ctx context.Context, requestURL string, target any) error {
	payload, err := p.loadBytes(ctx, requestURL)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return err
	}
	return nil
}

func (p clawHubProvider) loadBytes(ctx context.Context, requestURL string) ([]byte, error) {
	if err := sharedoutbound.DefaultPolicy().ValidateRequestURL(requestURL); err != nil {
		return nil, err
	}
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, application/zip;q=0.9, */*;q=0.5")
	req.Header.Set("User-Agent", "Arkloop/skills-registry")
	if strings.TrimSpace(p.cfg.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(p.cfg.APIKey))
	}
	resp, err := sharedoutbound.DefaultPolicy().NewHTTPClient(20 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return nil, fmt.Errorf("registry request failed: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

func normalizedScanStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "clean", "suspicious", "malicious", "pending", "error":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "unknown"
	}
}

func normalizeSkillsMarketPayload(payload any) []marketSkillSearchItem {
	records := collectSkillsMarketRecords(payload)
	items := make([]marketSkillSearchItem, 0, len(records))
	for _, record := range records {
		skillKey := firstString(record, "skill_key", "key", "slug", "name", "id")
		if skillKey == "" {
			continue
		}
		displayName := firstString(record, "display_name", "title", "name")
		if displayName == "" {
			displayName = skillKey
		}
		detailURL := firstString(record, "detail_url", "url", "detailUrl", "html_url")
		if detailURL == "" {
			if id := firstString(record, "id"); id != "" {
				detailURL = strings.TrimRight(defaultRegistryBaseURL, "/") + "/skills/" + strings.TrimSpace(id)
			}
		}
		items = append(items, marketSkillSearchItem{
			SkillKey:      skillKey,
			Version:       firstString(record, "version", "latest_version", "branch"),
			DisplayName:   displayName,
			Description:   firstString(record, "description", "summary"),
			UpdatedAt:     firstString(record, "updated_at", "updatedAt"),
			DetailURL:     detailURL,
			RepositoryURL: firstString(record, "repository_url", "repository", "repo_url", "githubUrl", "github_url"),
		})
	}
	return items
}

func collectSkillsMarketRecords(payload any) []map[string]any {
	records := make([]map[string]any, 0)
	switch typed := payload.(type) {
	case map[string]any:
		for _, key := range []string{"items", "skills", "results"} {
			if values, ok := typed[key].([]any); ok {
				for _, value := range values {
					if item, ok := value.(map[string]any); ok {
						records = append(records, item)
					}
				}
			}
		}
		if nested, ok := typed["data"].(map[string]any); ok {
			records = append(records, collectSkillsMarketRecords(nested)...)
		}
	case []any:
		for _, value := range typed {
			if item, ok := value.(map[string]any); ok {
				records = append(records, item)
			}
		}
	}
	return records
}

func msToTime(value *int64) *time.Time {
	if value == nil || *value <= 0 {
		return nil
	}
	t := time.UnixMilli(*value).UTC()
	return &t
}

func msToRFC3339(value *int64) string {
	if parsed := msToTime(value); parsed != nil {
		return parsed.Format(time.RFC3339)
	}
	return ""
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstString(item map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := item[key]
		if !ok {
			continue
		}
		switch value := raw.(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		case fmt.Stringer:
			text := strings.TrimSpace(value.String())
			if text != "" {
				return text
			}
		case float64:
			return strconv.FormatFloat(value, 'f', -1, 64)
		case int64:
			return strconv.FormatInt(value, 10)
		}
	}
	return ""
}
