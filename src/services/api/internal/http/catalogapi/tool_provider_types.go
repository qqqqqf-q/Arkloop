package catalogapi

import "encoding/json"

type toolProviderDefinition struct {
	GroupName          string
	ProviderName       string
	RequiresAPIKey     bool
	RequiresBaseURL    bool
	AllowsInternalHTTP bool
	ConfigFields       []ConfigFieldDef `json:"config_fields,omitempty"`
	DefaultBaseURL     string
	DefaultAPIKey      string
}

type ConfigFieldDef struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Default     string   `json:"default,omitempty"`
	Options     []string `json:"options,omitempty"`
	Group       string   `json:"group,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
}

type toolProvidersResponse struct {
	Groups []toolProviderGroupResponse `json:"groups"`
}

type toolProviderGroupResponse struct {
	GroupName string                     `json:"group_name"`
	Providers []toolProviderItemResponse `json:"providers"`
}

type toolProviderItemResponse struct {
	GroupName       string           `json:"group_name"`
	ProviderName    string           `json:"provider_name"`
	IsActive        bool             `json:"is_active"`
	KeyPrefix       *string          `json:"key_prefix,omitempty"`
	BaseURL         *string          `json:"base_url,omitempty"`
	RequiresAPIKey  bool             `json:"requires_api_key"`
	RequiresBaseURL bool             `json:"requires_base_url"`
	Configured      bool             `json:"configured"`
	RuntimeState    string           `json:"runtime_state"`
	RuntimeReason   string           `json:"runtime_reason,omitempty"`
	RuntimeStatus   string           `json:"runtime_status,omitempty"`
	RuntimeSource   string           `json:"runtime_source,omitempty"`
	ConfigStatus    string           `json:"config_status,omitempty"`
	ConfigReason    string           `json:"config_reason,omitempty"`
	ConfigJSON      json.RawMessage  `json:"config_json,omitempty"`
	ConfigFields    []ConfigFieldDef `json:"config_fields,omitempty"`
	DefaultBaseURL  string           `json:"default_base_url,omitempty"`
}

type upsertToolProviderCredentialRequest struct {
	APIKey            *string `json:"api_key"`
	BaseURL           *string `json:"base_url"`
	AllowInternalHTTP *bool   `json:"allow_internal_http"`
}
