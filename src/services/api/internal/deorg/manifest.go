package deorg

import (
	"encoding/json"
	"time"
)

const ManifestVersion = "deorg.v1"

type Manifest struct {
	Version                  string                           `json:"version"`
	ExportedAt               time.Time                        `json:"exported_at"`
	LegacyOrgMappings        []LegacyOrgMapping              `json:"legacy_org_mappings"`
	LegacyOrgs               []LegacyOrg                     `json:"legacy_orgs"`
	LegacyMemberships        []LegacyMembership              `json:"legacy_memberships"`
	Users                    []UserRecord                    `json:"users"`
	Projects                 []ProjectRecord                 `json:"projects"`
	Personas                 []PersonaRecord                 `json:"personas"`
	Secrets                  []SecretRecord                  `json:"secrets"`
	LLMCredentials           []LLMCredentialRecord           `json:"llm_credentials"`
	LLMRoutes                []LLMRouteRecord                `json:"llm_routes"`
	ToolProviderConfigs      []ToolProviderConfigRecord      `json:"tool_provider_configs"`
	ToolDescriptionOverrides []ToolDescriptionOverrideRecord `json:"tool_description_overrides"`
	Threads                  []ThreadRecord                  `json:"threads"`
	Messages                 []MessageRecord                 `json:"messages"`
	Runs                     []RunRecord                     `json:"runs"`
	RunEvents                []RunEventRecord                `json:"run_events"`
}

type LegacyOrgMapping struct {
	OrgID            string `json:"legacy_org_id"`
	OwnerUserID      string `json:"owner_user_id"`
	DefaultProjectID string `json:"default_project_id"`
}

type LegacyOrg struct {
	ID           string          `json:"id"`
	Slug         string          `json:"slug"`
	Name         string          `json:"name"`
	Type         string          `json:"type"`
	OwnerUserID  *string         `json:"owner_user_id,omitempty"`
	Status       string          `json:"status"`
	Country      *string         `json:"country,omitempty"`
	Timezone     *string         `json:"timezone,omitempty"`
	LogoURL      *string         `json:"logo_url,omitempty"`
	SettingsJSON json.RawMessage `json:"settings_json"`
	CreatedAt    time.Time       `json:"created_at"`
}

type LegacyMembership struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"legacy_org_id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type UserRecord struct {
	ID                  string     `json:"id"`
	Username            string     `json:"username"`
	Email               *string    `json:"email,omitempty"`
	EmailVerifiedAt     *time.Time `json:"email_verified_at,omitempty"`
	Status              string     `json:"status"`
	DeletedAt           *time.Time `json:"deleted_at,omitempty"`
	AvatarURL           *string    `json:"avatar_url,omitempty"`
	Locale              *string    `json:"locale,omitempty"`
	Timezone            *string    `json:"timezone,omitempty"`
	LastLoginAt         *time.Time `json:"last_login_at,omitempty"`
	TokensInvalidBefore time.Time  `json:"tokens_invalid_before"`
	CreatedAt           time.Time  `json:"created_at"`
	IsPlatformAdmin     bool       `json:"is_platform_admin"`
}

type ProjectRecord struct {
	ID          string     `json:"id"`
	LegacyOrgID *string    `json:"legacy_org_id,omitempty"`
	OwnerUserID *string    `json:"owner_user_id,omitempty"`
	Name        string     `json:"name"`
	Description *string    `json:"description,omitempty"`
	Visibility  string     `json:"visibility"`
	IsDefault   bool       `json:"is_default"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
}

type PersonaRecord struct {
	ID                  string          `json:"id"`
	LegacyOrgID         *string         `json:"legacy_org_id,omitempty"`
	PersonaKey          string          `json:"persona_key"`
	Version             string          `json:"version"`
	DisplayName         string          `json:"display_name"`
	Description         *string         `json:"description,omitempty"`
	SoulMD              string          `json:"soul_md"`
	UserSelectable      bool            `json:"user_selectable"`
	SelectorName        *string         `json:"selector_name,omitempty"`
	SelectorOrder       *int            `json:"selector_order,omitempty"`
	PromptMD            string          `json:"prompt_md"`
	ToolAllowlist       []string        `json:"tool_allowlist"`
	ToolDenylist        []string        `json:"tool_denylist"`
	BudgetsJSON         json.RawMessage `json:"budgets_json"`
	TitleSummarizeJSON  json.RawMessage `json:"title_summarize_json"`
	IsActive            bool            `json:"is_active"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
	PreferredCredential *string         `json:"preferred_credential,omitempty"`
	Model               *string         `json:"model,omitempty"`
	ReasoningMode       string          `json:"reasoning_mode"`
	PromptCacheControl  string          `json:"prompt_cache_control"`
	ExecutorType        string          `json:"executor_type"`
	ExecutorConfigJSON  json.RawMessage `json:"executor_config_json"`
	SyncMode            string          `json:"sync_mode"`
	MirroredFileDir     *string         `json:"mirrored_file_dir,omitempty"`
	LastSyncedAt        *time.Time      `json:"last_synced_at,omitempty"`
}

type SecretRecord struct {
	ID             string     `json:"id"`
	LegacyOrgID    *string    `json:"legacy_org_id,omitempty"`
	LegacyScope    string     `json:"legacy_scope"`
	Name           string     `json:"name"`
	EncryptedValue string     `json:"encrypted_value"`
	KeyVersion     int        `json:"key_version"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	RotatedAt      *time.Time `json:"rotated_at,omitempty"`
}

type LLMCredentialRecord struct {
	ID             string          `json:"id"`
	LegacyOrgID    *string         `json:"legacy_org_id,omitempty"`
	LegacyScope    string          `json:"legacy_scope"`
	Provider       string          `json:"provider"`
	Name           string          `json:"name"`
	SecretID       *string         `json:"secret_id,omitempty"`
	KeyPrefix      *string         `json:"key_prefix,omitempty"`
	BaseURL        *string         `json:"base_url,omitempty"`
	OpenAIAPIMode  *string         `json:"openai_api_mode,omitempty"`
	AdvancedJSON   json.RawMessage `json:"advanced_json"`
	RevokedAt      *time.Time      `json:"revoked_at,omitempty"`
	LastUsedAt     *time.Time      `json:"last_used_at,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type LLMRouteRecord struct {
	ID                  string          `json:"id"`
	RouteKey            string          `json:"route_key"`
	LegacyOrgID         *string         `json:"legacy_org_id,omitempty"`
	CredentialID        string          `json:"credential_id"`
	Model               string          `json:"model"`
	Priority            int             `json:"priority"`
	IsDefault           bool            `json:"is_default"`
	Tags                []string        `json:"tags"`
	WhenJSON            json.RawMessage `json:"when_json"`
	AdvancedJSON        json.RawMessage `json:"advanced_json"`
	Multiplier          float64         `json:"multiplier"`
	CostPer1kInput      *float64        `json:"cost_per_1k_input,omitempty"`
	CostPer1kOutput     *float64        `json:"cost_per_1k_output,omitempty"`
	CostPer1kCacheWrite *float64        `json:"cost_per_1k_cache_write,omitempty"`
	CostPer1kCacheRead  *float64        `json:"cost_per_1k_cache_read,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
}

type ToolProviderConfigRecord struct {
	ID           string          `json:"id"`
	LegacyOrgID  *string         `json:"legacy_org_id,omitempty"`
	LegacyScope  string          `json:"legacy_scope"`
	GroupName    string          `json:"group_name"`
	ProviderName string          `json:"provider_name"`
	IsActive     bool            `json:"is_active"`
	SecretID     *string         `json:"secret_id,omitempty"`
	KeyPrefix    *string         `json:"key_prefix,omitempty"`
	BaseURL      *string         `json:"base_url,omitempty"`
	ConfigJSON   json.RawMessage `json:"config_json"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type ToolDescriptionOverrideRecord struct {
	LegacyOrgID *string    `json:"legacy_org_id,omitempty"`
	LegacyScope string     `json:"legacy_scope"`
	ToolName    string     `json:"tool_name"`
	Description string     `json:"description"`
	IsDisabled  bool       `json:"is_disabled"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type ThreadRecord struct {
	ID                    string     `json:"id"`
	LegacyOrgID           string     `json:"legacy_org_id"`
	ProjectID             string     `json:"project_id"`
	CreatedByUserID       *string    `json:"created_by_user_id,omitempty"`
	Title                 *string    `json:"title,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	DeletedAt             *time.Time `json:"deleted_at,omitempty"`
	IsPrivate             bool       `json:"is_private"`
	ExpiresAt             *time.Time `json:"expires_at,omitempty"`
	ParentThreadID        *string    `json:"parent_thread_id,omitempty"`
	BranchedFromMessageID *string    `json:"branched_from_message_id,omitempty"`
	TitleLocked           bool       `json:"title_locked"`
}

type MessageRecord struct {
	ID              string          `json:"id"`
	LegacyOrgID     string          `json:"legacy_org_id"`
	ThreadID        string          `json:"thread_id"`
	CreatedByUserID *string         `json:"created_by_user_id,omitempty"`
	Role            string          `json:"role"`
	Content         string          `json:"content"`
	ContentJSON     json.RawMessage `json:"content_json"`
	MetadataJSON    json.RawMessage `json:"metadata_json"`
	TokenCount      *int32          `json:"token_count,omitempty"`
	DeletedAt       *time.Time      `json:"deleted_at,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	Hidden          bool            `json:"hidden"`
}

type RunRecord struct {
	ID                string          `json:"id"`
	LegacyOrgID       string          `json:"legacy_org_id"`
	ProjectID         string          `json:"project_id"`
	ThreadID          string          `json:"thread_id"`
	CreatedByUserID   *string         `json:"created_by_user_id,omitempty"`
	Status            string          `json:"status"`
	CreatedAt         time.Time       `json:"created_at"`
	ParentRunID       *string         `json:"parent_run_id,omitempty"`
	StatusUpdatedAt   *time.Time      `json:"status_updated_at,omitempty"`
	CompletedAt       *time.Time      `json:"completed_at,omitempty"`
	FailedAt          *time.Time      `json:"failed_at,omitempty"`
	DurationMs        *int64          `json:"duration_ms,omitempty"`
	TotalInputTokens  *int64          `json:"total_input_tokens,omitempty"`
	TotalOutputTokens *int64          `json:"total_output_tokens,omitempty"`
	TotalCostUSD      *float64        `json:"total_cost_usd,omitempty"`
	Model             *string         `json:"model,omitempty"`
	PersonaID         *string         `json:"persona_id,omitempty"`
	ProfileRef        *string         `json:"profile_ref,omitempty"`
	WorkspaceRef      *string         `json:"workspace_ref,omitempty"`
	DeletedAt         *time.Time      `json:"deleted_at,omitempty"`
	EnvironmentJSON   json.RawMessage `json:"environment_json"`
}

type RunEventRecord struct {
	EventID    string          `json:"event_id"`
	RunID      string          `json:"run_id"`
	Seq        int64           `json:"seq"`
	TS         time.Time       `json:"ts"`
	Type       string          `json:"type"`
	DataJSON   json.RawMessage `json:"data_json"`
	ToolName   *string         `json:"tool_name,omitempty"`
	ErrorClass *string         `json:"error_class,omitempty"`
}
