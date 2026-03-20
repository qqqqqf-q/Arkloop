//go:build desktop

package catalogapi

import (
	"context"
	"strings"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
)

var toolProviderCatalog = []toolProviderDefinition{
	{GroupName: "web_search", ProviderName: "web_search.tavily", RequiresAPIKey: true},
	{GroupName: "web_search", ProviderName: "web_search.searxng", RequiresBaseURL: true, AllowsInternalHTTP: true, DefaultBaseURL: "http://searxng:8080"},
	{GroupName: "web_fetch", ProviderName: "web_fetch.jina", RequiresAPIKey: true},
	{GroupName: "web_fetch", ProviderName: "web_fetch.firecrawl", RequiresBaseURL: true, AllowsInternalHTTP: true, DefaultBaseURL: "http://firecrawl:19012"},
	{GroupName: "web_fetch", ProviderName: "web_fetch.basic"},
	{
		GroupName: "acp", ProviderName: "acp.opencode",
		ConfigFields: []ConfigFieldDef{
			{Key: "host_kind", Label: "Host Kind", Type: "select", Required: false, Default: "local", Options: []string{"local", "sandbox"}},
			{Key: "auth_strategy", Label: "Auth Strategy", Type: "select", Required: false, Default: "provider_native", Options: []string{"provider_native", "arkloop_proxy"}},
			{Key: "command", Label: "Command", Type: "string", Required: false, Placeholder: "opencode"},
			{Key: "extra_args", Label: "Arguments", Type: "string", Required: false, Placeholder: "--verbose"},
			{Key: "delegate_model_selector", Label: "Model", Type: "string", Required: false, Placeholder: "credentialId^model"},
			{Key: "cwd", Label: "Working Directory", Type: "string", Required: false, Placeholder: "."},
			{Key: "env_overrides", Label: "Env Overrides", Type: "string", Required: false, Placeholder: "{\"KEY\":\"value\"}"},
		},
	},
	{GroupName: "sandbox", ProviderName: "sandbox.docker", RequiresBaseURL: true, AllowsInternalHTTP: true, DefaultBaseURL: "http://sandbox-docker:19002"},
	{GroupName: "sandbox", ProviderName: "sandbox.firecracker", RequiresBaseURL: true, AllowsInternalHTTP: true, DefaultBaseURL: "http://sandbox:19002"},
	{
		GroupName: "memory", ProviderName: "memory.openviking",
		RequiresBaseURL: true, RequiresAPIKey: true, AllowsInternalHTTP: true,
		DefaultBaseURL: "http://openviking:1933",
		ConfigFields: []ConfigFieldDef{
			{Key: "embedding.provider", Label: "Embedding Provider", Type: "select", Required: true, Default: "volcengine", Options: []string{"openai", "volcengine", "vikingdb", "jina"}, Group: "embedding"},
			{Key: "embedding.model", Label: "Embedding Model", Type: "string", Required: true, Default: "doubao-embedding-vision-250615", Group: "embedding", Placeholder: "e.g. text-embedding-3-small"},
			{Key: "embedding.api_key", Label: "Embedding API Key", Type: "password", Required: true, Group: "embedding"},
			{Key: "embedding.api_base", Label: "Embedding API Base", Type: "string", Required: true, Default: "https://ark.cn-beijing.volces.com/api/v3", Group: "embedding", Placeholder: "https://api.openai.com/v1"},
			{Key: "embedding.dimension", Label: "Embedding Dimension", Type: "number", Required: true, Default: "1024", Group: "embedding"},
			{Key: "vlm.provider", Label: "VLM Provider", Type: "select", Required: true, Default: "litellm", Options: []string{"volcengine", "openai", "litellm"}, Group: "vlm"},
			{Key: "vlm.model", Label: "VLM Model", Type: "string", Required: true, Default: "doubao-seed-1-8-251228", Group: "vlm", Placeholder: "e.g. gpt-4o"},
			{Key: "vlm.api_key", Label: "VLM API Key", Type: "password", Required: true, Group: "vlm"},
			{Key: "vlm.api_base", Label: "VLM API Base", Type: "string", Required: true, Default: "https://ark.cn-beijing.volces.com/api/v3", Group: "vlm", Placeholder: "https://api.openai.com/v1"},
			{Key: "cost_per_commit", Label: "Cost per Commit", Type: "number", Required: false, Default: "0", Group: "billing"},
		},
	},
}

func findProviderDef(groupName string, providerName string) (toolProviderDefinition, bool) {
	group := strings.TrimSpace(groupName)
	provider := strings.TrimSpace(providerName)
	for _, def := range toolProviderCatalog {
		if def.GroupName == group && def.ProviderName == provider {
			return def, true
		}
	}
	return toolProviderDefinition{}, false
}

func applyProviderDefaults(
	ctx context.Context,
	repo *data.ToolProviderConfigsRepository,
	ownerKind string,
	ownerUserID *uuid.UUID,
	groupName string,
	providerName string,
) {
	def, ok := findProviderDef(groupName, providerName)
	if !ok || def.DefaultBaseURL == "" {
		return
	}
	baseURL := def.DefaultBaseURL
	var apiKey *string
	if def.DefaultAPIKey != "" {
		apiKey = &def.DefaultAPIKey
	}
	_, _ = repo.UpsertConfig(ctx, ownerKind, ownerUserID, groupName, providerName, nil, nil, &baseURL, nil)
	_ = apiKey
}
