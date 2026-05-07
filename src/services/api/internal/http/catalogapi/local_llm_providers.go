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
	advancedJSON := map[string]any{
		llmproviders.AvailableCatalogAdvancedKey: map[string]any{
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
	return data.LlmRoute{
		ID:           localRouteUUID(status.ID, model.ID),
		CredentialID: providerUUID,
		Model:        model.ID,
		Priority:     model.Priority,
		IsDefault:    model.Default && !model.Hidden,
		ShowInPicker: !model.Hidden,
		Tags:         []string{},
		WhenJSON:     json.RawMessage("{}"),
		AdvancedJSON: advancedJSON,
		Multiplier:   1,
		CreatedAt:    now,
	}
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
