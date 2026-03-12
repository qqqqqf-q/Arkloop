package entitlement

import (
	"context"
	"crypto/hmac"
	"fmt"
	"strconv"
	"time"

	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/shared/creditpolicy"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	cachePrefix = "arkloop:entitlement:"
	cacheTTL    = 5 * time.Minute
)

// Resolver 提供直接 SQL 的三级权益解析，适合 Worker 等无 DI 容器的服务。
// pool/rdb 均可为 nil：pool 为 nil 时 Resolve 退化为平台默认值；rdb 为 nil 时不使用缓存。
type Resolver struct {
	pool *pgxpool.Pool
	rdb  *redis.Client

	cfgResolver sharedconfig.Resolver
	registry    *sharedconfig.Registry
}

// NewResolver 创建 Resolver。pool/rdb 均可为 nil（fail-open）。
func NewResolver(pool *pgxpool.Pool, rdb *redis.Client) *Resolver {
	registry := sharedconfig.DefaultRegistry()
	var cache sharedconfig.Cache
	cacheTTL := sharedconfig.CacheTTLFromEnv()
	if rdb != nil && cacheTTL > 0 {
		cache = sharedconfig.NewRedisCache(rdb)
	}
	cfgResolver, _ := sharedconfig.NewResolver(registry, sharedconfig.NewPGXStore(pool), cache, cacheTTL)
	return &Resolver{
		pool:        pool,
		rdb:         rdb,
		cfgResolver: cfgResolver,
		registry:    registry,
	}
}

// Resolve 返回权益原始字符串值，优先级：account override > plan entitlement > 平台默认值。
func (r *Resolver) Resolve(ctx context.Context, accountID uuid.UUID, key string) (string, error) {
	if r.rdb != nil {
		if val, ok := fromCache(ctx, r.rdb, accountID, key); ok {
			return val, nil
		}
	}

	val, err := r.resolveFromDB(ctx, accountID, key)
	if err != nil {
		return "", err
	}

	if r.rdb != nil {
		writeCache(ctx, r.rdb, r.registry, accountID, key, val)
	}
	return val, nil
}

// ResolveInt 解析整型权益值。
func (r *Resolver) ResolveInt(ctx context.Context, accountID uuid.UUID, key string) (int64, error) {
	raw, err := r.Resolve(ctx, accountID, key)
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("entitlement: parse int for %q (raw=%q): %w", key, raw, err)
	}
	return n, nil
}

// CountMonthlyRuns 统计指定 account 在给定年月已执行的 run 数量（从 usage_records）。
// 使用时间范围查询，确保索引 idx_usage_records_account_recorded 可被命中。
func (r *Resolver) CountMonthlyRuns(ctx context.Context, accountID uuid.UUID, year, month int) (int64, error) {
	if r.pool == nil {
		return 0, nil
	}
	start, end := monthRange(year, month)
	var count int64
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*)
		 FROM usage_records
		 WHERE account_id = $1 AND recorded_at >= $2 AND recorded_at < $3`,
		accountID, start, end,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("entitlement: count monthly runs: %w", err)
	}
	return count, nil
}

// SumMonthlyTokens 汇总指定 account 在给定年月的 token 消耗总量（从 usage_records）。
// 使用时间范围查询，确保索引 idx_usage_records_account_recorded 可被命中。
func (r *Resolver) SumMonthlyTokens(ctx context.Context, accountID uuid.UUID, year, month int) (int64, error) {
	if r.pool == nil {
		return 0, nil
	}
	start, end := monthRange(year, month)
	var total int64
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(input_tokens + output_tokens), 0)
		 FROM usage_records
		 WHERE account_id = $1 AND recorded_at >= $2 AND recorded_at < $3`,
		accountID, start, end,
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("entitlement: sum monthly tokens: %w", err)
	}
	return total, nil
}

// GetCreditBalance 查询 account 的积分余额。无记录时返回 0（不报错，允许 account 尚未初始化积分）。
func (r *Resolver) GetCreditBalance(ctx context.Context, accountID uuid.UUID) (int64, error) {
	if r.pool == nil {
		return 0, nil
	}
	var balance int64
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(balance, 0) FROM credits WHERE account_id = $1`,
		accountID,
	).Scan(&balance)
	if err != nil {
		// 无记录视为余额 0
		return 0, nil
	}
	return balance, nil
}

// monthRange 返回给定年月的 [start, end) UTC 时间范围。
func monthRange(year, month int) (start, end time.Time) {
	start = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end = start.AddDate(0, 1, 0)
	return
}

func (r *Resolver) resolveFromDB(ctx context.Context, accountID uuid.UUID, key string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("entitlement resolver not initialized")
	}
	if r.cfgResolver == nil {
		registry := sharedconfig.DefaultRegistry()
		fallback, _ := sharedconfig.NewResolver(registry, sharedconfig.NewPGXStore(r.pool), nil, 0)
		r.cfgResolver = fallback
		r.registry = registry
	}

	// 1. account override（未过期）
	if r.pool != nil {
		var overrideVal string
		err := r.pool.QueryRow(ctx,
			`SELECT value FROM account_entitlement_overrides
			 WHERE account_id = $1 AND key = $2
			   AND (expires_at IS NULL OR expires_at > now())
			 LIMIT 1`,
			accountID, key,
		).Scan(&overrideVal)
		if err == nil {
			return overrideVal, nil
		}

		// 2. plan entitlement（active subscription → plan）
		var planVal string
		err = r.pool.QueryRow(ctx,
			`SELECT pe.value
			 FROM plan_entitlements pe
			 JOIN subscriptions s ON s.plan_id = pe.plan_id
			 WHERE s.account_id = $1 AND s.status = 'active' AND pe.key = $2
			 LIMIT 1`,
			accountID, key,
		).Scan(&planVal)
		if err == nil {
			return planVal, nil
		}
	}

	// 平台默认值：ENV > platform_settings > registry default
	val, err := r.cfgResolver.Resolve(ctx, key, sharedconfig.Scope{})
	if err != nil {
		return "", fmt.Errorf("entitlement: resolve platform default %q: %w", key, err)
	}
	return val, nil
}

// fromCache 从 Redis 读取缓存值。缓存格式与 API entitlement.Service 一致："type:value"。
func fromCache(ctx context.Context, rdb *redis.Client, accountID uuid.UUID, key string) (string, bool) {
	if rdb == nil {
		return "", false
	}
	if !EntitlementCacheSigningEnabled() {
		return "", false
	}

	cacheKey := cachePrefix + accountID.String() + ":" + key
	sigKey := cacheKey + EntitlementCacheSignatureSuffix
	items, err := rdb.MGet(ctx, cacheKey, sigKey).Result()
	if err != nil {
		return "", false
	}
	if len(items) != 2 {
		return "", false
	}

	raw, ok := items[0].(string)
	sig, sigOK := items[1].(string)
	if !ok || !sigOK || raw == "" || sig == "" {
		_ = rdb.Del(ctx, cacheKey, sigKey).Err()
		return "", false
	}

	expected, ok := ComputeEntitlementCacheSignature(cacheKey, raw)
	if !ok || !hmac.Equal([]byte(sig), []byte(expected)) {
		_ = rdb.Del(ctx, cacheKey, sigKey).Err()
		return "", false
	}

	// 格式 "type:value"，取第一个 ':' 后的内容
	for i := 0; i < len(raw); i++ {
		if raw[i] == ':' {
			return raw[i+1:], true
		}
	}
	_ = rdb.Del(ctx, cacheKey, sigKey).Err()
	return "", false
}

func writeCache(ctx context.Context, rdb *redis.Client, registry *sharedconfig.Registry, accountID uuid.UUID, key, val string) {
	if rdb == nil {
		return
	}
	if !EntitlementCacheSigningEnabled() {
		return
	}

	cacheKey := cachePrefix + accountID.String() + ":" + key
	typ := cacheTypeForKey(key, registry)
	raw := typ + ":" + val
	sig, ok := ComputeEntitlementCacheSignature(cacheKey, raw)
	if !ok {
		return
	}

	pipe := rdb.Pipeline()
	pipe.Set(ctx, cacheKey, raw, cacheTTL)
	pipe.Set(ctx, cacheKey+EntitlementCacheSignatureSuffix, sig, cacheTTL)
	_, _ = pipe.Exec(ctx)
}

// ResolveDeductionPolicy 解析 credit.deduction_policy 权益，fail-open：
// 解析失败或 key 不存在时返回 creditpolicy.DefaultPolicy。
func (r *Resolver) ResolveDeductionPolicy(ctx context.Context, accountID uuid.UUID) (creditpolicy.CreditDeductionPolicy, error) {
	raw, err := r.Resolve(ctx, accountID, "credit.deduction_policy")
	if err != nil {
		return creditpolicy.DefaultPolicy, nil
	}
	return creditpolicy.Parse(raw), nil
}

func cacheTypeForKey(key string, registry *sharedconfig.Registry) string {
	if key == "credit.deduction_policy" {
		return "json"
	}
	if registry == nil {
		registry = sharedconfig.DefaultRegistry()
	}
	if entry, ok := registry.Get(key); ok {
		switch entry.Type {
		case sharedconfig.TypeInt:
			return "int"
		case sharedconfig.TypeBool:
			return "bool"
		default:
			return "string"
		}
	}
	return "string"
}
