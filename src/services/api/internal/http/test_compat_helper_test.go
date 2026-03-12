package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/mail"
	"path"
	"regexp"
	"strings"
	"time"

	nethttp "net/http"

	catalogfamily "arkloop/services/api/internal/http/catalogapi"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/personas"

	sharedconfig "arkloop/services/shared/config"
	sharedexec "arkloop/services/shared/executionconfig"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// --- auth ---

const (
	refreshTokenCookieName = "arkloop_refresh_token"
	clientAppHeader        = "X-Client-App"
)

type loginResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

type logoutResponse struct {
	OK bool `json:"ok"`
}

type registerResponse struct {
	UserID      string  `json:"user_id"`
	AccessToken string  `json:"access_token"`
	TokenType   string  `json:"token_type"`
	Warning     *string `json:"warning,omitempty"`
}

type meResponse struct {
	ID                        string   `json:"id"`
	Username                  string   `json:"username"`
	Email                     *string  `json:"email,omitempty"`
	EmailVerified             bool     `json:"email_verified"`
	EmailVerificationRequired bool     `json:"email_verification_required"`
	CreatedAt                 string   `json:"created_at"`
	AccountID                 string   `json:"account_id,omitempty"`
	AccountName               string   `json:"account_name,omitempty"`
	Role                      string   `json:"role,omitempty"`
	Permissions               []string `json:"permissions"`
}

func isValidEmail(value string) bool {
	if strings.ContainsAny(value, "\r\n") {
		return false
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil || addr == nil {
		return false
	}
	return addr.Address == trimmed
}

const maxJSONBodySize = 1 << 20

func decodeJSON(r *nethttp.Request, dst any) error {
	reader := nethttp.MaxBytesReader(nil, r.Body, maxJSONBodySize)
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}

func writeJSON(w nethttp.ResponseWriter, traceID string, statusCode int, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = w.Write(raw)
}

// --- admin ---

type executionGovernanceResponse struct {
	Limits               []sharedconfig.SettingInspection `json:"limits"`
	TitleSummarizerModel *string                          `json:"title_summarizer_model,omitempty"`
	Personas             []executionGovernancePersona     `json:"personas"`
}

type executionGovernancePersona struct {
	ID                  string                              `json:"id"`
	Source              string                              `json:"source"`
	PersonaKey          string                              `json:"persona_key"`
	Version             string                              `json:"version"`
	DisplayName         string                              `json:"display_name"`
	PreferredCredential *string                             `json:"preferred_credential,omitempty"`
	Model               *string                             `json:"model,omitempty"`
	ReasoningMode       string                              `json:"reasoning_mode,omitempty"`
	PromptCacheControl  string                              `json:"prompt_cache_control,omitempty"`
	Requested           sharedexec.RequestedBudgets         `json:"requested"`
	Effective           executionGovernancePersonaEffective `json:"effective"`
}

type executionGovernancePersonaEffective struct {
	SystemPrompt           string                       `json:"system_prompt,omitempty"`
	ReasoningIterations    int                          `json:"reasoning_iterations"`
	ToolContinuationBudget int                          `json:"tool_continuation_budget"`
	MaxOutputTokens        *int                         `json:"max_output_tokens,omitempty"`
	Temperature            *float64                     `json:"temperature,omitempty"`
	TopP                   *float64                     `json:"top_p,omitempty"`
	ReasoningMode          string                       `json:"reasoning_mode,omitempty"`
	PerToolSoftLimits      sharedexec.PerToolSoftLimits `json:"per_tool_soft_limits,omitempty"`
}

type broadcastResponse struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	Title       string         `json:"title"`
	Body        string         `json:"body"`
	TargetType  string         `json:"target_type"`
	TargetID    *string        `json:"target_id,omitempty"`
	PayloadJSON map[string]any `json:"payload"`
	Status      string         `json:"status"`
	SentCount   int            `json:"sent_count"`
	CreatedBy   string         `json:"created_by"`
	CreatedAt   string         `json:"created_at"`
}

type adminReportItem struct {
	ID            string   `json:"id"`
	ThreadID      string   `json:"thread_id"`
	ReporterID    string   `json:"reporter_id"`
	ReporterEmail string   `json:"reporter_email"`
	Categories    []string `json:"categories"`
	Feedback      *string  `json:"feedback"`
	CreatedAt     string   `json:"created_at"`
}

type adminReportsResponse struct {
	Data  []adminReportItem `json:"data"`
	Total int               `json:"total"`
}

type adminUserResponse struct {
	ID              string  `json:"id"`
	Login           *string `json:"login,omitempty"`
	Username        string  `json:"username"`
	Email           *string `json:"email"`
	EmailVerifiedAt *string `json:"email_verified_at,omitempty"`
	Status          string  `json:"status"`
	AvatarURL       *string `json:"avatar_url,omitempty"`
	Locale          *string `json:"locale,omitempty"`
	Timezone        *string `json:"timezone,omitempty"`
	LastLoginAt     *string `json:"last_login_at,omitempty"`
	CreatedAt       string  `json:"created_at"`
}

type adminUserDetailResponse struct {
	adminUserResponse
	Accounts []adminUserAccountResponse `json:"accounts"`
}

type adminUserAccountResponse struct {
	AccountID string `json:"account_id"`
	Role      string `json:"role"`
}

// --- billing ---

type creditBalanceResponse struct {
	AccountID string `json:"account_id"`
	Balance   int64  `json:"balance"`
}

type creditTransactionResponse struct {
	ID            string           `json:"id"`
	AccountID     string           `json:"account_id"`
	Amount        int64            `json:"amount"`
	Type          string           `json:"type"`
	ReferenceType *string          `json:"reference_type,omitempty"`
	ReferenceID   *string          `json:"reference_id,omitempty"`
	Note          *string          `json:"note,omitempty"`
	Metadata      *json.RawMessage `json:"metadata,omitempty"`
	ThreadTitle   *string          `json:"thread_title,omitempty"`
	CreatedAt     string           `json:"created_at"`
}

type meCreditsResponse struct {
	Balance      int64                       `json:"balance"`
	Transactions []creditTransactionResponse `json:"transactions"`
}

type redemptionCodeResponse struct {
	ID              string  `json:"id"`
	Code            string  `json:"code"`
	Type            string  `json:"type"`
	Value           string  `json:"value"`
	MaxUses         int     `json:"max_uses"`
	UseCount        int     `json:"use_count"`
	ExpiresAt       *string `json:"expires_at,omitempty"`
	IsActive        bool    `json:"is_active"`
	BatchID         *string `json:"batch_id,omitempty"`
	CreatedByUserID string  `json:"created_by_user_id"`
	CreatedAt       string  `json:"created_at"`
}

// --- catalog ---

type toolDescriptionSource string

const (
	toolDescriptionSourceDefault  toolDescriptionSource = "default"
	toolDescriptionSourcePlatform toolDescriptionSource = "platform"
	toolDescriptionSourceProject toolDescriptionSource = "project"
)

type toolCatalogItem struct {
	Name              string                `json:"name"`
	Label             string                `json:"label"`
	LLMDescription    string                `json:"llm_description"`
	HasOverride       bool                  `json:"has_override"`
	DescriptionSource toolDescriptionSource `json:"description_source"`
	IsDisabled        bool                  `json:"is_disabled"`
}

type toolCatalogGroup struct {
	Group string            `json:"group"`
	Tools []toolCatalogItem `json:"tools"`
}

type toolCatalogResponse struct {
	Groups []toolCatalogGroup `json:"groups"`
}

type llmProviderResponse struct {
	ID            string                     `json:"id"`
	AccountID     string                     `json:"account_id"`
	Provider      string                     `json:"provider"`
	Name          string                     `json:"name"`
	KeyPrefix     *string                    `json:"key_prefix"`
	BaseURL       *string                    `json:"base_url"`
	OpenAIAPIMode *string                    `json:"openai_api_mode"`
	AdvancedJSON  map[string]any             `json:"advanced_json,omitempty"`
	CreatedAt     string                     `json:"created_at"`
	Models        []llmProviderModelResponse `json:"models"`
}

type llmProviderModelResponse struct {
	ID                  string          `json:"id"`
	ProviderID          string          `json:"provider_id"`
	Model               string          `json:"model"`
	Priority            int             `json:"priority"`
	IsDefault           bool            `json:"is_default"`
	Tags                []string        `json:"tags"`
	WhenJSON            json.RawMessage `json:"when"`
	AdvancedJSON        map[string]any  `json:"advanced_json,omitempty"`
	Multiplier          float64         `json:"multiplier"`
	CostPer1kInput      *float64        `json:"cost_per_1k_input,omitempty"`
	CostPer1kOutput     *float64        `json:"cost_per_1k_output,omitempty"`
	CostPer1kCacheWrite *float64        `json:"cost_per_1k_cache_write,omitempty"`
	CostPer1kCacheRead  *float64        `json:"cost_per_1k_cache_read,omitempty"`
}

type llmProviderAvailableModelsResponse struct {
	Models []llmProviderAvailableModel `json:"models"`
}

type llmProviderAvailableModel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Configured bool   `json:"configured"`
}

type personaResponse struct {
	ID                  string          `json:"id"`
	AccountID           *string         `json:"account_id"`
	PersonaKey          string          `json:"persona_key"`
	Version             string          `json:"version"`
	DisplayName         string          `json:"display_name"`
	Description         *string         `json:"description,omitempty"`
	UserSelectable      bool            `json:"user_selectable"`
	SelectorName        *string         `json:"selector_name,omitempty"`
	SelectorOrder       *int            `json:"selector_order,omitempty"`
	PromptMD            string          `json:"prompt_md"`
	ToolAllowlist       []string        `json:"tool_allowlist"`
	ToolDenylist        []string        `json:"tool_denylist"`
	BudgetsJSON         json.RawMessage `json:"budgets"`
	IsActive            bool            `json:"is_active"`
	CreatedAt           string          `json:"created_at"`
	PreferredCredential *string         `json:"preferred_credential,omitempty"`
	Model               *string         `json:"model,omitempty"`
	ReasoningMode       string          `json:"reasoning_mode"`
	PromptCacheControl  string          `json:"prompt_cache_control"`
	ExecutorType        string          `json:"executor_type"`
	ExecutorConfigJSON  json.RawMessage `json:"executor_config"`
	Source              string          `json:"source"`
}

type liteAgentResponse struct {
	ID              string          `json:"id"`
	PersonaKey      string          `json:"persona_key"`
	DisplayName     string          `json:"display_name"`
	Description     *string         `json:"description,omitempty"`
	PromptMD        string          `json:"prompt_md"`
	Model           *string         `json:"model,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	MaxOutputTokens *int            `json:"max_output_tokens,omitempty"`
	ReasoningMode   string          `json:"reasoning_mode"`
	ToolPolicy      string          `json:"tool_policy"`
	ToolAllowlist   []string        `json:"tool_allowlist"`
	ToolDenylist    []string        `json:"tool_denylist"`
	IsActive        bool            `json:"is_active"`
	ExecutorType    string          `json:"executor_type"`
	BudgetsJSON     json.RawMessage `json:"budgets"`
	Source          string          `json:"source"`
	CreatedAt       string          `json:"created_at"`
}

func validateAdvancedJSONForProvider(provider string, advancedJSON map[string]any) error {
	if strings.TrimSpace(provider) != "anthropic" || advancedJSON == nil {
		return nil
	}
	return validateAnthropicAdvancedJSON(advancedJSON)
}

const (
	anthropicAdvancedVersionKey      = "anthropic_version"
	anthropicAdvancedExtraHeadersKey = "extra_headers"
	anthropicBetaHeaderName          = "anthropic-beta"
)

func validateAnthropicAdvancedJSON(advancedJSON map[string]any) error {
	if advancedJSON == nil {
		return nil
	}
	if rawVersion, ok := advancedJSON[anthropicAdvancedVersionKey]; ok {
		version, ok := rawVersion.(string)
		if !ok || strings.TrimSpace(version) == "" {
			return errors.New("advanced_json.anthropic_version must be a non-empty string")
		}
	}

	rawHeaders, ok := advancedJSON[anthropicAdvancedExtraHeadersKey]
	if !ok {
		return nil
	}
	headers, ok := rawHeaders.(map[string]any)
	if !ok {
		return errors.New("advanced_json.extra_headers must be an object")
	}
	for key, value := range headers {
		headerName := strings.ToLower(strings.TrimSpace(key))
		if headerName != anthropicBetaHeaderName {
			return errors.New("advanced_json.extra_headers only supports anthropic-beta")
		}
		headerValue, ok := value.(string)
		if !ok || strings.TrimSpace(headerValue) == "" {
			return errors.New("advanced_json.extra_headers.anthropic-beta must be a non-empty string")
		}
	}
	return nil
}

func toLiteAgentFromDB(p data.Persona) liteAgentResponse {
	allowlist := p.ToolAllowlist
	if allowlist == nil {
		allowlist = []string{}
	}
	denylist := p.ToolDenylist
	if denylist == nil {
		denylist = []string{}
	}
	budgets := p.BudgetsJSON
	if len(budgets) == 0 {
		budgets = json.RawMessage("{}")
	}
	executorType := strings.TrimSpace(p.ExecutorType)
	if executorType == "" {
		executorType = "agent.simple"
	}
	temperature, maxOutputTokens := extractLiteAgentBudgetValues(budgets)
	reasoningMode := strings.TrimSpace(p.ReasoningMode)
	if reasoningMode == "" {
		reasoningMode = "auto"
	}
	toolPolicy := "none"
	if len(allowlist) > 0 {
		toolPolicy = "allowlist"
	} else if len(denylist) > 0 {
		toolPolicy = "denylist"
	}
	return liteAgentResponse{
		ID:              p.ID.String(),
		PersonaKey:      p.PersonaKey,
		DisplayName:     p.DisplayName,
		Description:     p.Description,
		PromptMD:        p.PromptMD,
		Model:           optionalLiteTrimmedStringPtr(p.Model),
		Temperature:     temperature,
		MaxOutputTokens: maxOutputTokens,
		ReasoningMode:   reasoningMode,
		ToolPolicy:      toolPolicy,
		ToolAllowlist:   allowlist,
		ToolDenylist:    denylist,
		IsActive:        p.IsActive,
		ExecutorType:    executorType,
		BudgetsJSON:     budgets,
		Source:          "db",
		CreatedAt:       p.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

func toLiteAgentFromRepo(rp personas.RepoPersona) liteAgentResponse {
	allowlist := rp.ToolAllowlist
	if allowlist == nil {
		allowlist = []string{}
	}
	denylist := rp.ToolDenylist
	if denylist == nil {
		denylist = []string{}
	}
	budgets := json.RawMessage("{}")
	if rp.Budgets != nil {
		if b, err := json.Marshal(rp.Budgets); err == nil {
			budgets = b
		}
	}
	executorType := strings.TrimSpace(rp.ExecutorType)
	if executorType == "" {
		executorType = "agent.simple"
	}
	temperature, maxOutputTokens := extractLiteAgentBudgetValues(budgets)
	reasoningMode := strings.TrimSpace(rp.ReasoningMode)
	if reasoningMode == "" {
		reasoningMode = "auto"
	}
	toolPolicy := "none"
	if len(allowlist) > 0 {
		toolPolicy = "allowlist"
	} else if len(denylist) > 0 {
		toolPolicy = "denylist"
	}
	return liteAgentResponse{
		ID:              rp.ID,
		PersonaKey:      rp.ID,
		DisplayName:     rp.Title,
		Description:     optionalLiteTrimmedString(rp.Description),
		PromptMD:        rp.PromptMD,
		Model:           optionalLiteTrimmedString(rp.Model),
		Temperature:     temperature,
		MaxOutputTokens: maxOutputTokens,
		ReasoningMode:   reasoningMode,
		ToolPolicy:      toolPolicy,
		ToolAllowlist:   allowlist,
		ToolDenylist:    denylist,
		IsActive:        true,
		ExecutorType:    executorType,
		BudgetsJSON:     budgets,
		Source:          "repo",
		CreatedAt:       "",
	}
}

func extractLiteAgentBudgetValues(raw json.RawMessage) (*float64, *int) {
	if len(raw) == 0 {
		return nil, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, nil
	}
	var temperature *float64
	if value, ok := payload["temperature"].(float64); ok {
		temperature = &value
	}
	var maxOutputTokens *int
	switch value := payload["max_output_tokens"].(type) {
	case float64:
		converted := int(value)
		maxOutputTokens = &converted
	case int:
		maxOutputTokens = &value
	}
	return temperature, maxOutputTokens
}

func optionalLiteTrimmedStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	return optionalLiteTrimmedString(*value)
}

func optionalLiteTrimmedString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func buildEffectiveToolCatalog(
	ctx context.Context,
	accountID uuid.UUID,
	overridesRepo *data.ToolDescriptionOverridesRepository,
	pool *pgxpool.Pool,
	mcpCache *catalogfamily.EffectiveToolCatalogCache,
	artifactStoreAvailable bool,
) (toolCatalogResponse, error) {
	resp, err := catalogfamily.BuildEffectiveToolCatalogCompat(ctx, accountID, overridesRepo, pool, mcpCache, artifactStoreAvailable)
	if err != nil {
		return toolCatalogResponse{}, err
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		return toolCatalogResponse{}, err
	}
	var converted toolCatalogResponse
	if err := json.Unmarshal(raw, &converted); err != nil {
		return toolCatalogResponse{}, err
	}
	return converted, nil
}

// --- conversation ---

const (
	shareSessionDuration = 24 * time.Hour
	shareSessionSecret   = "arkloop-share-session"
)

type createRunResponse struct {
	RunID   string `json:"run_id"`
	TraceID string `json:"trace_id"`
}

type threadRunResponse struct {
	RunID     string `json:"run_id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type runResponse struct {
	RunID           string   `json:"run_id"`
	AccountID       string   `json:"account_id"`
	ThreadID        string   `json:"thread_id"`
	CreatedByUserID *string  `json:"created_by_user_id"`
	ParentRunID     *string  `json:"parent_run_id,omitempty"`
	ChildRunIDs     []string `json:"child_run_ids,omitempty"`
	Status          string   `json:"status"`
	CreatedAt       string   `json:"created_at"`
	TraceID         string   `json:"trace_id"`
}

type cancelRunResponse struct {
	OK bool `json:"ok"`
}

type globalRunResponse struct {
	RunID             string   `json:"run_id"`
	AccountID         string   `json:"account_id"`
	ThreadID          string   `json:"thread_id"`
	Status            string   `json:"status"`
	Model             *string  `json:"model,omitempty"`
	PersonaID         *string  `json:"persona_id,omitempty"`
	ParentRunID       *string  `json:"parent_run_id,omitempty"`
	TotalInputTokens  *int64   `json:"total_input_tokens,omitempty"`
	TotalOutputTokens *int64   `json:"total_output_tokens,omitempty"`
	TotalCostUSD      *float64 `json:"total_cost_usd,omitempty"`
	DurationMs        *int64   `json:"duration_ms,omitempty"`
	CacheHitRate      *float64 `json:"cache_hit_rate,omitempty"`
	CreditsUsed       *int64   `json:"credits_used,omitempty"`
	CreatedAt         string   `json:"created_at"`
	CompletedAt       *string  `json:"completed_at,omitempty"`
	FailedAt          *string  `json:"failed_at,omitempty"`
	CreatedByUserID   *string  `json:"created_by_user_id,omitempty"`
	CreatedByUserName *string  `json:"created_by_user_name,omitempty"`
	CreatedByEmail    *string  `json:"created_by_email,omitempty"`
}

type threadResponse struct {
	ID              string  `json:"id"`
	AccountID       string  `json:"account_id"`
	CreatedByUserID *string `json:"created_by_user_id"`
	Title           *string `json:"title"`
	ProjectID       *string `json:"project_id,omitempty"`
	CreatedAt       string  `json:"created_at"`
	ActiveRunID     *string `json:"active_run_id"`
	IsPrivate       bool    `json:"is_private"`
	ParentThreadID  *string `json:"parent_thread_id,omitempty"`
}

type messageResponse struct {
	ID              string          `json:"id"`
	AccountID       string          `json:"account_id"`
	ThreadID        string          `json:"thread_id"`
	CreatedByUserID *string         `json:"created_by_user_id"`
	RunID           *string         `json:"run_id,omitempty"`
	Role            string          `json:"role"`
	Content         string          `json:"content"`
	ContentJSON     json.RawMessage `json:"content_json,omitempty"`
	CreatedAt       string          `json:"created_at"`
}

type messageAttachmentUploadResponse struct {
	Key           string `json:"key"`
	Filename      string `json:"filename"`
	MimeType      string `json:"mime_type"`
	Size          int64  `json:"size"`
	Kind          string `json:"kind"`
	ExtractedText string `json:"extracted_text,omitempty"`
}

type reportResponse struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
}

func generateShareSession(share *data.ThreadShare) string {
	expiry := time.Now().Add(shareSessionDuration).Unix()
	payload := fmt.Sprintf("%s:%d", share.Token, expiry)
	key := []byte(shareSessionSecret + share.ID.String())
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%s:%d", sig, expiry)
}

// --- helpers ---

var uuidPrefixRegex = regexp.MustCompile(`^[0-9a-fA-F-]{1,36}$`)

func calcCacheHitRate(inputTokens, cacheRead, cacheCreation, cachedTokens *int64) *float64 {
	hasAnthropic := (cacheRead != nil && *cacheRead > 0) || (cacheCreation != nil && *cacheCreation > 0)
	hasOpenAI := cachedTokens != nil && *cachedTokens > 0

	if hasAnthropic && hasOpenAI {
		return nil
	}
	if hasAnthropic {
		total := 0.0
		if inputTokens != nil {
			total += float64(*inputTokens)
		}
		if cacheRead != nil {
			total += float64(*cacheRead)
		}
		if cacheCreation != nil {
			total += float64(*cacheCreation)
		}
		if total <= 0 {
			return nil
		}
		read := 0.0
		if cacheRead != nil {
			read = float64(*cacheRead)
		}
		r := read / total
		return &r
	}
	if hasOpenAI && inputTokens != nil && *inputTokens > 0 {
		r := float64(*cachedTokens) / float64(*inputTokens)
		return &r
	}
	return nil
}

// --- org ---

type apiKeyResponse struct {
	ID         string   `json:"id"`
	AccountID  string   `json:"account_id"`
	UserID     string   `json:"user_id"`
	Name       string   `json:"name"`
	KeyPrefix  string   `json:"key_prefix"`
	Scopes     []string `json:"scopes"`
	RevokedAt  *string  `json:"revoked_at,omitempty"`
	LastUsedAt *string  `json:"last_used_at,omitempty"`
	CreatedAt  string   `json:"created_at"`
}

type webhookEndpointResponse struct {
	ID        string   `json:"id"`
	AccountID string   `json:"account_id"`
	URL       string   `json:"url"`
	Events    []string `json:"events"`
	Enabled   bool     `json:"enabled"`
	CreatedAt string   `json:"created_at"`
}

func workspaceManifestKey(workspaceRef, revision string) string {
	return "workspaces/" + workspaceRef + "/manifests/" + revision + ".json"
}

func workspaceBlobKey(workspaceRef, sha256Hash string) string {
	return "workspaces/" + workspaceRef + "/blobs/" + sha256Hash
}

type workspaceManifest struct {
	Entries []workspaceManifestEntry `json:"entries,omitempty"`
}

type workspaceManifestEntry struct {
	Path    string `json:"path"`
	Type    string `json:"type"`
	SHA256  string `json:"sha256,omitempty"`
	Deleted bool   `json:"deleted,omitempty"`
}

const workspaceEntryTypeFile = "file"

func detectWorkspaceContentType(relativePath string, content []byte) string {
	if ext := strings.ToLower(path.Ext(relativePath)); ext != "" {
		if guessed := mime.TypeByExtension(ext); strings.TrimSpace(guessed) != "" {
			return guessed
		}
	}
	return nethttp.DetectContentType(content)
}

// --- platform ---

const maskedSensitiveValue = "******"

type notificationResponse struct {
	ID          string         `json:"id"`
	UserID      string         `json:"user_id"`
	AccountID   string         `json:"account_id"`
	Type        string         `json:"type"`
	Title       string         `json:"title"`
	Body        string         `json:"body"`
	PayloadJSON map[string]any `json:"payload"`
	ReadAt      *string        `json:"read_at,omitempty"`
	CreatedAt   string         `json:"created_at"`
}

func maskIfSensitive(key, value string, registry *sharedconfig.Registry) string {
	if registry == nil {
		registry = sharedconfig.DefaultRegistry()
	}
	entry, ok := registry.Get(key)
	if !ok || !entry.Sensitive {
		return value
	}
	if strings.TrimSpace(value) == "" {
		return value
	}
	return maskedSensitiveValue
}

func filterSchemaEntries(entries []sharedconfig.Entry, isPlatformAdmin bool) []sharedconfig.Entry {
	if isPlatformAdmin {
		return entries
	}

	out := make([]sharedconfig.Entry, 0, len(entries))
	for _, e := range entries {
		if e.Scope == sharedconfig.ScopeProject || e.Scope == sharedconfig.ScopeBoth {
			out = append(out, e)
		}
	}
	return out
}
