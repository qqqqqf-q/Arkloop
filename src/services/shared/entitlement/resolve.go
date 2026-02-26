package entitlement

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	cachePrefix = "arkloop:entitlement:"
	cacheTTL    = 5 * time.Minute
)

// 平台默认值，三级均无配置时使用。与 API 侧 platformDefaults 保持同步。
var defaults = map[string]entry{
	"quota.runs_per_month":       {raw: "999999", typ: "int"},
	"quota.tokens_per_month":     {raw: "1000000", typ: "int"},
	"limit.concurrent_runs":      {raw: "10", typ: "int"},
	"limit.team_members":         {raw: "50", typ: "int"},
	"feature.byok_enabled":       {raw: "true", typ: "bool"},
	"feature.mcp_remote_enabled": {raw: "false", typ: "bool"},
	"credit.initial_grant":       {raw: "1000", typ: "int"},
	"credit.invite_reward":       {raw: "500", typ: "int"},
}

type entry struct {
	raw string
	typ string
}

// Resolver 提供直接 SQL 的三级权益解析，适合 Worker 等无 DI 容器的服务。
// pool/rdb 均可为 nil：pool 为 nil 时 Resolve 退化为平台默认值；rdb 为 nil 时不使用缓存。
type Resolver struct {
	pool *pgxpool.Pool
	rdb  *redis.Client
}

// NewResolver 创建 Resolver。pool/rdb 均可为 nil（fail-open）。
func NewResolver(pool *pgxpool.Pool, rdb *redis.Client) *Resolver {
	return &Resolver{pool: pool, rdb: rdb}
}

// Resolve 返回权益原始字符串值，优先级：org override > plan entitlement > 平台默认值。
func (r *Resolver) Resolve(ctx context.Context, orgID uuid.UUID, key string) (string, error) {
	if r.rdb != nil {
		if val, ok := fromCache(ctx, r.rdb, orgID, key); ok {
			return val, nil
		}
	}

	val, err := r.resolveFromDB(ctx, orgID, key)
	if err != nil {
		return "", err
	}

	if r.rdb != nil {
		writeCache(ctx, r.rdb, orgID, key, val)
	}
	return val, nil
}

// ResolveInt 解析整型权益值，无法解析时返回 0。
func (r *Resolver) ResolveInt(ctx context.Context, orgID uuid.UUID, key string) (int64, error) {
	raw, err := r.Resolve(ctx, orgID, key)
	if err != nil {
		return 0, err
	}
	n, _ := strconv.ParseInt(raw, 10, 64)
	return n, nil
}

// CountMonthlyRuns 统计指定 org 在给定年月已执行的 run 数量（从 usage_records）。
// 使用时间范围查询，确保索引 idx_usage_records_org_recorded 可被命中。
func (r *Resolver) CountMonthlyRuns(ctx context.Context, orgID uuid.UUID, year, month int) (int64, error) {
	if r.pool == nil {
		return 0, nil
	}
	start, end := monthRange(year, month)
	var count int64
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*)
		 FROM usage_records
		 WHERE org_id = $1 AND recorded_at >= $2 AND recorded_at < $3`,
		orgID, start, end,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("entitlement: count monthly runs: %w", err)
	}
	return count, nil
}

// SumMonthlyTokens 汇总指定 org 在给定年月的 token 消耗总量（从 usage_records）。
// 使用时间范围查询，确保索引 idx_usage_records_org_recorded 可被命中。
func (r *Resolver) SumMonthlyTokens(ctx context.Context, orgID uuid.UUID, year, month int) (int64, error) {
	if r.pool == nil {
		return 0, nil
	}
	start, end := monthRange(year, month)
	var total int64
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(input_tokens + output_tokens), 0)
		 FROM usage_records
		 WHERE org_id = $1 AND recorded_at >= $2 AND recorded_at < $3`,
		orgID, start, end,
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("entitlement: sum monthly tokens: %w", err)
	}
	return total, nil
}

// GetCreditBalance 查询 org 的积分余额。无记录时返回 0（不报错，允许 org 尚未初始化积分）。
func (r *Resolver) GetCreditBalance(ctx context.Context, orgID uuid.UUID) (int64, error) {
	if r.pool == nil {
		return 0, nil
	}
	var balance int64
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(balance, 0) FROM credits WHERE org_id = $1`,
		orgID,
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

func (r *Resolver) resolveFromDB(ctx context.Context, orgID uuid.UUID, key string) (string, error) {
	if r.pool == nil {
		if def, ok := defaults[key]; ok {
			return def.raw, nil
		}
		return "", fmt.Errorf("entitlement: unknown key %q", key)
	}

	// 1. org override（未过期）
	var overrideVal string
	err := r.pool.QueryRow(ctx,
		`SELECT value FROM org_entitlement_overrides
		 WHERE org_id = $1 AND key = $2
		   AND (expires_at IS NULL OR expires_at > now())
		 LIMIT 1`,
		orgID, key,
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
		 WHERE s.org_id = $1 AND s.status = 'active' AND pe.key = $2
		 LIMIT 1`,
		orgID, key,
	).Scan(&planVal)
	if err == nil {
		return planVal, nil
	}

	// 3. 平台默认值
	if def, ok := defaults[key]; ok {
		return def.raw, nil
	}
	return "", fmt.Errorf("entitlement: unknown key %q", key)
}

// fromCache 从 Redis 读取缓存值。缓存格式与 API entitlement.Service 一致："type:value"。
func fromCache(ctx context.Context, rdb *redis.Client, orgID uuid.UUID, key string) (string, bool) {
	raw, err := rdb.Get(ctx, cachePrefix+orgID.String()+":"+key).Result()
	if err != nil {
		return "", false
	}
	// 格式 "type:value"，取第一个 ':' 后的内容
	for i := 0; i < len(raw); i++ {
		if raw[i] == ':' {
			return raw[i+1:], true
		}
	}
	return "", false
}

func writeCache(ctx context.Context, rdb *redis.Client, orgID uuid.UUID, key, val string) {
	typ := "string"
	if def, ok := defaults[key]; ok {
		typ = def.typ
	}
	_ = rdb.Set(ctx, cachePrefix+orgID.String()+":"+key, typ+":"+val, cacheTTL).Err()
}
