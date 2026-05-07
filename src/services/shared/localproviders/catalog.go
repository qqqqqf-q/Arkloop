package localproviders

import "github.com/google/uuid"

const (
	SourceLocal       = "local"
	OwnershipReadOnly = "read_only"

	AuthModeAPIKey = "api_key"
	AuthModeOAuth  = "oauth"

	ClaudeCodeProviderID   = "claude_code_local"
	ClaudeCodeDisplayName  = "Claude Code (Local)"
	ClaudeCodeProviderKind = "claude_code_local"

	CodexProviderID   = "codex_local"
	CodexDisplayName  = "Codex (Local)"
	CodexProviderKind = "codex_local"
)

type Model struct {
	ID              string
	ContextLength   int
	MaxOutputTokens int
	ToolCalling     bool
	Reasoning       bool
	Default         bool
	Hidden          bool
	Priority        int
}

type ProviderStatus struct {
	ID          string
	DisplayName string
	Provider    string
	AuthMode    string
	Models      []Model
}

func ClaudeCodeModels() []Model {
	return []Model{
		{
			ID:              "claude-opus-4-6",
			ContextLength:   200000,
			MaxOutputTokens: 8192,
			ToolCalling:     true,
			Reasoning:       true,
			Default:         true,
			Priority:        900,
		},
		{
			ID:              "claude-opus-4-6-fast",
			ContextLength:   200000,
			MaxOutputTokens: 8192,
			ToolCalling:     true,
			Reasoning:       true,
			Priority:        880,
		},
		{
			ID:              "claude-opus-4-5",
			ContextLength:   200000,
			MaxOutputTokens: 8192,
			ToolCalling:     true,
			Reasoning:       true,
			Priority:        860,
		},
		{
			ID:              "claude-opus-4-1",
			ContextLength:   200000,
			MaxOutputTokens: 8192,
			ToolCalling:     true,
			Reasoning:       true,
			Priority:        840,
		},
		{
			ID:              "claude-sonnet-4-6",
			ContextLength:   200000,
			MaxOutputTokens: 8192,
			ToolCalling:     true,
			Reasoning:       true,
			Priority:        820,
		},
		{
			ID:              "claude-sonnet-4-5",
			ContextLength:   200000,
			MaxOutputTokens: 8192,
			ToolCalling:     true,
			Reasoning:       true,
			Priority:        800,
		},
		{
			ID:              "claude-sonnet-4-20250514",
			ContextLength:   200000,
			MaxOutputTokens: 8192,
			ToolCalling:     true,
			Reasoning:       true,
			Priority:        780,
		},
		{
			ID:              "claude-3-7-sonnet-20250219",
			ContextLength:   200000,
			MaxOutputTokens: 8192,
			ToolCalling:     true,
			Reasoning:       true,
			Priority:        760,
		},
		{
			ID:              "claude-haiku-4-5",
			ContextLength:   200000,
			MaxOutputTokens: 8192,
			ToolCalling:     true,
			Reasoning:       true,
			Priority:        740,
		},
		{
			ID:              "claude-3-5-haiku-20241022",
			ContextLength:   200000,
			MaxOutputTokens: 8192,
			ToolCalling:     true,
			Reasoning:       false,
			Priority:        720,
		},
	}
}

func CodexModels() []Model {
	return []Model{
		{
			ID:            "gpt-5.4",
			ContextLength: 272000,
			ToolCalling:   true,
			Reasoning:     true,
			Default:       true,
			Priority:      900,
		},
		{
			ID:            "gpt-5.5",
			ContextLength: 272000,
			ToolCalling:   true,
			Reasoning:     true,
			Priority:      880,
		},
		{
			ID:            "gpt-5.4-mini",
			ContextLength: 272000,
			ToolCalling:   true,
			Reasoning:     true,
			Priority:      860,
		},
		{
			ID:            "gpt-5.3-codex",
			ContextLength: 272000,
			ToolCalling:   true,
			Reasoning:     true,
			Priority:      840,
		},
		{
			ID:            "gpt-5.3-codex-spark",
			ContextLength: 128000,
			ToolCalling:   true,
			Reasoning:     true,
			Priority:      820,
		},
		{
			ID:            "gpt-5.2",
			ContextLength: 272000,
			ToolCalling:   true,
			Reasoning:     true,
			Priority:      700,
		},
	}
}

func modelByID(providerID string, modelID string) (Model, bool) {
	var models []Model
	switch providerID {
	case ClaudeCodeProviderID:
		models = ClaudeCodeModels()
	case CodexProviderID:
		models = CodexModels()
	default:
		return Model{}, false
	}
	for _, model := range models {
		if model.ID == modelID {
			return model, true
		}
	}
	return Model{}, false
}

func ProviderUUID(providerID string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("arkloop:local-provider:"+providerID))
}

func ProviderIDFromUUID(providerUUID uuid.UUID) (string, bool) {
	switch providerUUID {
	case ProviderUUID(ClaudeCodeProviderID):
		return ClaudeCodeProviderID, true
	case ProviderUUID(CodexProviderID):
		return CodexProviderID, true
	default:
		return "", false
	}
}

func RouteUUID(providerID string, modelID string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("arkloop:local-provider:"+providerID+":model:"+modelID))
}
