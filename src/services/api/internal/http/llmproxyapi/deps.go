package llmproxyapi

import (
	"arkloop/services/api/internal/data"
	"arkloop/services/shared/acptoken"

	"github.com/redis/go-redis/v9"
)

type Deps struct {
	TokenValidator *acptoken.Validator
	LlmCredRepo    *data.LlmCredentialsRepository
	LlmRoutesRepo  *data.LlmRoutesRepository
	SecretsRepo    *data.SecretsRepository
	Pool           data.DB
	RedisClient    *redis.Client
	RunEventRepo   *data.RunEventRepository
}
