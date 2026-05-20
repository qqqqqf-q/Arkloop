package catalogapi

import (
	"context"
	"encoding/json"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/llmproviders"
	"arkloop/services/shared/localproviders"

	"github.com/google/uuid"
)

type localProviderStatusSource interface {
	ProviderStatuses(ctx context.Context) []localproviders.ProviderStatus
}

func NewLocalProviderListAugmenter(source localProviderStatusSource) LlmProviderListAugmenter {
	return func(ctx context.Context, _ uuid.UUID, scope string, userID uuid.UUID) ([]llmproviders.Provider, error) {
		if source == nil || scope != data.LlmRouteScopeUser {
			return nil, nil
		}
		statuses := source.ProviderStatuses(ctx)
		providers := make([]llmproviders.Provider, 0, len(statuses))
		for _, status := range statuses {
			providers = append(providers, localProviderFromStatus(status, userID))
		}
		return providers, nil
	}
}

func localProviderFromStatus(status localproviders.ProviderStatus, userID uuid.UUID) llmproviders.Provider {
	providerUUID := localProviderUUID(status.ID)
	now := time.Now().UTC()
	credential := data.LlmCredential{
		ID:           providerUUID,
		OwnerKind:    data.LlmRouteScopeUser,
		OwnerUserID:  &userID,
		Provider:     status.Provider,
		Name:         status.DisplayName,
		AdvancedJSON: localProviderAdvancedJSON(status),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	models := make([]data.LlmRoute, 0, len(status.Models))
	for _, model := range status.Models {
		models = append(models, localRouteFromModel(status, providerUUID, model, now))
	}
	return llmproviders.Provider{
		Credential: credential,
		Models:     models,
		Source:     localproviders.SourceLocal,
		ReadOnly:   true,
		AuthMode:   status.AuthMode,
	}
}

func localRouteFromModel(status localproviders.ProviderStatus, providerUUID uuid.UUID, model localproviders.Model, now time.Time) data.LlmRoute {
	advancedJSON := localModelAdvancedJSON(model)
	return data.LlmRoute{
		ID:           localRouteUUID(status.ID, model.ID),
		CredentialID: providerUUID,
		Model:        model.ID,
		Priority:     model.Priority,
		ShowInPicker: !model.Hidden,
		Tags:         copyStringSlice(model.Tags),
		WhenJSON:     json.RawMessage("{}"),
		AdvancedJSON: advancedJSON,
		Multiplier:   1,
		CreatedAt:    now,
	}
}

func localModelAdvancedJSON(model localproviders.Model) map[string]any {
	advancedJSON := copyStringAnyMap(model.AdvancedJSON)
	rawCatalog, _ := advancedJSON[llmproviders.AvailableCatalogAdvancedKey].(map[string]any)
	catalog := copyStringAnyMap(rawCatalog)
	if stringFromAny(catalog["id"]) == "" {
		catalog["id"] = model.ID
	}
	if stringFromAny(catalog["name"]) == "" {
		catalog["name"] = model.ID
	}
	if stringFromAny(catalog["type"]) == "" {
		catalog["type"] = "chat"
	}
	if _, ok := catalog["context_length"]; !ok && model.ContextLength > 0 {
		catalog["context_length"] = model.ContextLength
	}
	if _, ok := catalog["max_output_tokens"]; !ok && model.MaxOutputTokens > 0 {
		catalog["max_output_tokens"] = model.MaxOutputTokens
	}
	if _, ok := catalog["input_modalities"]; !ok {
		catalog["input_modalities"] = []string{"text", "image"}
	}
	if _, ok := catalog["output_modalities"]; !ok {
		catalog["output_modalities"] = []string{"text"}
	}
	if _, ok := catalog["tool_calling"]; !ok {
		catalog["tool_calling"] = model.ToolCalling
	}
	if _, ok := catalog["reasoning"]; !ok {
		catalog["reasoning"] = model.Reasoning
	}
	if _, ok := catalog["default_temperature"]; !ok {
		catalog["default_temperature"] = 1
	}
	advancedJSON[llmproviders.AvailableCatalogAdvancedKey] = catalog
	return advancedJSON
}

func copyStringAnyMap(value map[string]any) map[string]any {
	next := make(map[string]any, len(value))
	for key, item := range value {
		next[key] = item
	}
	return next
}

func stringFromAny(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func copyStringSlice(value []string) []string {
	if value == nil {
		return []string{}
	}
	return append([]string(nil), value...)
}

func localProviderAdvancedJSON(status localproviders.ProviderStatus) map[string]any {
	return map[string]any{
		"source":            localproviders.SourceLocal,
		"local_provider_id": status.ID,
		"auth_mode":         status.AuthMode,
		"read_only":         true,
	}
}

func localProviderUUID(providerID string) uuid.UUID {
	return localproviders.ProviderUUID(providerID)
}

func isLocalProviderUUID(providerID uuid.UUID) bool {
	_, ok := localproviders.ProviderIDFromUUID(providerID)
	return ok
}

func localProviderIDFromUUID(providerID uuid.UUID) (string, bool) {
	return localproviders.ProviderIDFromUUID(providerID)
}

func localRouteUUID(providerID string, modelID string) uuid.UUID {
	return localproviders.RouteUUID(providerID, modelID)
}
