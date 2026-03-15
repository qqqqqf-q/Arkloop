package catalogapi

import (
	"context"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ToolCatalogItem = toolCatalogItem

type ToolCatalogGroup = toolCatalogGroup

type ToolCatalogResponse = toolCatalogResponse

type PersonaResponse = personaResponse

type LLMProviderAvailableModelsResponse = llmProviderAvailableModelsResponse

func BuildEffectiveToolCatalogCompat(
	ctx context.Context,
	accountID uuid.UUID,
	projectID uuid.UUID,
	overridesRepo *data.ToolDescriptionOverridesRepository,
	pool *pgxpool.Pool,
	mcpCache *EffectiveToolCatalogCache,
	artifactStoreAvailable bool,
) (ToolCatalogResponse, error) {
	return buildEffectiveToolCatalog(ctx, accountID, projectID, overridesRepo, pool, mcpCache, artifactStoreAvailable)
}
