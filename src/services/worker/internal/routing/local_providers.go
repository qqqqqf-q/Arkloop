package routing

import (
	"strings"

	"arkloop/services/shared/localproviders"
)

func AppendLocalProviders(config ProviderRoutingConfig, statuses []localproviders.ProviderStatus) ProviderRoutingConfig {
	if len(statuses) == 0 {
		return config
	}
	for _, status := range statuses {
		credential, routes := localProviderRoutes(status)
		if credential.ID == "" || len(routes) == 0 {
			continue
		}
		if _, exists := config.GetCredential(credential.ID); !exists {
			config.Credentials = append(config.Credentials, credential)
		}
		for _, route := range routes {
			if _, exists := config.GetRoute(route.ID); !exists {
				config.Routes = append(config.Routes, route)
			}
			if config.DefaultRouteID == "" && route.ID != "" {
				config.DefaultRouteID = route.ID
			}
		}
	}
	return config
}

func localProviderRoutes(status localproviders.ProviderStatus) (ProviderCredential, []ProviderRouteRule) {
	kind := ProviderKind(status.Provider)
	if kind != ProviderKindClaudeLocal && kind != ProviderKindCodexLocal {
		return ProviderCredential{}, nil
	}
	credential := ProviderCredential{
		ID:           status.ID,
		Name:         status.DisplayName,
		OwnerKind:    CredentialScopePlatform,
		ProviderKind: kind,
		AdvancedJSON: map[string]any{
			"source":            localproviders.SourceLocal,
			"local_provider_id": status.ID,
			"auth_mode":         status.AuthMode,
			"read_only":         true,
		},
	}
	routes := make([]ProviderRouteRule, 0, len(status.Models))
	for _, model := range status.Models {
		if model.Hidden {
			continue
		}
		routes = append(routes, ProviderRouteRule{
			ID:            localRouteID(status.ID, model.ID),
			Model:         model.ID,
			CredentialID:  credential.ID,
			When:          map[string]any{},
			AdvancedJSON:  localRouteAdvancedJSON(model),
			Multiplier:    1,
			Priority:      model.Priority,
			AccountScoped: false,
		})
	}
	return credential, routes
}

func LocalProviderSelectedRoute(status localproviders.ProviderStatus, model localproviders.Model) (SelectedProviderRoute, bool) {
	credential, routes := localProviderRoutes(localproviders.ProviderStatus{
		ID:          status.ID,
		DisplayName: status.DisplayName,
		Provider:    status.Provider,
		AuthMode:    status.AuthMode,
		Models:      []localproviders.Model{model},
	})
	if credential.ID == "" || len(routes) != 1 {
		return SelectedProviderRoute{}, false
	}
	return SelectedProviderRoute{Credential: credential, Route: routes[0]}, true
}

func localRouteID(providerID string, modelID string) string {
	id := "local-" + strings.ReplaceAll(providerID, "_", "-") + "-" + strings.ReplaceAll(modelID, "_", "-")
	if len(id) <= 64 {
		return id
	}
	return id[:64]
}

func localRouteAdvancedJSON(model localproviders.Model) map[string]any {
	return map[string]any{
		availableCatalogAdvancedKey: map[string]any{
			"id":                  model.ID,
			"name":                model.ID,
			"type":                "chat",
			"context_length":      model.ContextLength,
			"max_output_tokens":   model.MaxOutputTokens,
			"input_modalities":    []string{"text", "image"},
			"output_modalities":   []string{"text"},
			"tool_calling":        model.ToolCalling,
			"reasoning":           model.Reasoning,
			"default_temperature": 1,
		},
	}
}
