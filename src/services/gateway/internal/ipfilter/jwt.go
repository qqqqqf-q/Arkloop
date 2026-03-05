package ipfilter

import (
	"context"

	"arkloop/services/gateway/internal/identity"

	"github.com/redis/go-redis/v9"
)

func extractOrgIDWithRedis(authHeader string, rdb *redis.Client, ctx context.Context, jwtSecret []byte) string {
	return identity.ExtractOrgID(ctx, authHeader, rdb, jwtSecret)
}
