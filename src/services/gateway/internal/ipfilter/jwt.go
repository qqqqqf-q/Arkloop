package ipfilter

import (
	"context"

	"arkloop/services/gateway/internal/identity"

	"github.com/redis/go-redis/v9"
)

func extractAccountIDWithRedis(authHeader string, rdb *redis.Client, ctx context.Context, jwtSecret []byte) string {
	return identity.ExtractAccountID(ctx, authHeader, rdb, jwtSecret)
}
