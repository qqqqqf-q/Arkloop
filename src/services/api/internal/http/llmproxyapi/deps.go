package llmproxyapi

import (
	"arkloop/services/api/internal/data"
	"arkloop/services/shared/acptoken"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Deps struct {
	TokenValidator *acptoken.Validator
	LlmCredRepo    *data.LlmCredentialsRepository
	LlmRoutesRepo  *data.LlmRoutesRepository
	SecretsRepo    *data.SecretsRepository
	Pool           *pgxpool.Pool
	RedisClient    *redis.Client
	RunEventRepo   *data.RunEventRepository
}
