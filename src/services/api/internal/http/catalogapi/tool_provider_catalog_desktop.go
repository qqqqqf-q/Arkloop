//go:build desktop

package catalogapi

import (
	"context"
	"strings"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
)

var toolProviderCatalog = []toolProviderDefinition{
	{GroupName: "web_search", ProviderName: "web_search.basic"},
	{GroupName: "web_search", ProviderName: "web_search.tavily", RequiresAPIKey: true},
	{GroupName: "web_search", ProviderName: "web_search.exa"},
	{GroupName: "web_search", ProviderName: "web_search.searxng", RequiresBaseURL: true, AllowsInternalHTTP: true, DefaultBaseURL: "http://searxng:8080"},
	{
		GroupName: "x_search", ProviderName: "x_search.xai",
		ConfigFields: []ConfigFieldDef{
			{Key: "model", Label: "Model", Type: "string", Required: false, Default: "grok-4.20-reasoning", Placeholder: "grok-4.20-reasoning"},
		},
	},
	{GroupName: "web_fetch", ProviderName: "web_fetch.jina"},
	{GroupName: "web_fetch", ProviderName: "web_fetch.firecrawl", RequiresBaseURL: true, AllowsInternalHTTP: true, DefaultBaseURL: "http://firecrawl:19012"},
	{GroupName: "web_fetch", ProviderName: "web_fetch.basic"},
	{
		GroupName: "read", ProviderName: "read.minimax",
		RequiresAPIKey: true, DefaultBaseURL: "https://api.minimaxi.com",
		ConfigFields: []ConfigFieldDef{
			{Key: "model", Label: "Model", Type: "string", Required: false, Default: "MiniMax-VL-01", Placeholder: "MiniMax-VL-01"},
		},
	},
	{GroupName: "sandbox", ProviderName: "sandbox.docker", RequiresBaseURL: true, AllowsInternalHTTP: true, DefaultBaseURL: "http://sandbox-docker:19002"},
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
