package sandbox

import (
	"context"
	"log/slog"
	"math"

	"arkloop/services/shared/creditpolicy"
	sharedent "arkloop/services/shared/entitlement"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/tools"

	"github.com/jackc/pgx/v5"
)

// BillingConfig 存储 sandbox 计费参数。
type BillingConfig struct {
	BaseFee       int64   // 每次调用固定积分
	RatePerSecond float64 // 每秒执行积分费率
}

// CalcCredits 计算 sandbox 调用应扣减的积分，并返回计算明细。
// durationMs 为 sandbox 服务返回的实际执行时长。
func CalcCredits(cfg BillingConfig, durationMs int64, policy creditpolicy.CreditDeductionPolicy) (int64, map[string]any) {
	if durationMs < 0 {
		durationMs = 0
	}
	durationS := float64(durationMs) / 1000.0
	raw := float64(cfg.BaseFee) + durationS*cfg.RatePerSecond

	multiplier := policy.MultiplierFor(math.MaxInt64, math.MaxFloat64)
	if multiplier == 0 {
		return 0, nil
	}

	rawCredits := raw * multiplier
	credits := int64(math.Ceil(rawCredits))
	if credits < 1 {
		credits = 1
	}

	meta := map[string]any{
		"type":              "sandbox",
		"duration_ms":       durationMs,
		"base_fee":          cfg.BaseFee,
		"rate_per_second":   cfg.RatePerSecond,
		"policy_multiplier": multiplier,
		"raw_credits":       rawCredits,
		"credits":           credits,
	}
	return credits, meta
}

// CalcBaseOnlyCredits 计算失败调用的积分（仅 base_fee），并返回计算明细。
func CalcBaseOnlyCredits(cfg BillingConfig, policy creditpolicy.CreditDeductionPolicy) (int64, map[string]any) {
	multiplier := policy.MultiplierFor(math.MaxInt64, math.MaxFloat64)
	if multiplier == 0 {
		return 0, nil
	}
	rawCredits := float64(cfg.BaseFee) * multiplier
	credits := int64(math.Ceil(rawCredits))
	if credits < 1 {
		credits = 1
	}

	meta := map[string]any{
		"type":              "sandbox",
		"failed":            true,
		"base_fee":          cfg.BaseFee,
		"policy_multiplier": multiplier,
		"raw_credits":       rawCredits,
		"credits":           credits,
	}
	return credits, meta
}

// TxBeginner 抽象 pgxpool.Pool 的 Begin 方法，便于测试。
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// BillingExecutor 装饰原始 sandbox executor，在调用完成后立即扣减积分。
type BillingExecutor struct {
	inner       tools.Executor
	pool        TxBeginner
	creditsRepo data.CreditsRepository
	resolver    *sharedent.Resolver
	cfg         BillingConfig
}

// NewBillingExecutor 创建计费装饰器。pool 和 resolver 不应为 nil。
func NewBillingExecutor(
	inner tools.Executor,
	pool TxBeginner,
	resolver *sharedent.Resolver,
	cfg BillingConfig,
) *BillingExecutor {
	return &BillingExecutor{
		inner:    inner,
		pool:     pool,
		resolver: resolver,
		cfg:      cfg,
	}
}

func (b *BillingExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
) tools.ExecutionResult {
	result := b.inner.Execute(ctx, toolName, args, execCtx, toolCallID)

	if execCtx.OrgID == nil {
		return result
	}

	policy := creditpolicy.DefaultPolicy
	if b.resolver != nil {
		if p, err := b.resolver.ResolveDeductionPolicy(ctx, *execCtx.OrgID); err == nil {
			policy = p
		}
	}

	var credits int64
	var meta map[string]any
	if result.Error != nil {
		credits, meta = CalcBaseOnlyCredits(b.cfg, policy)
	} else {
		durationMs := extractDurationMs(result.ResultJSON)
		credits, meta = CalcCredits(b.cfg, durationMs, policy)
	}

	if credits <= 0 {
		return result
	}

	err := b.creditsRepo.DeductStandalone(ctx, b.pool, *execCtx.OrgID, credits, execCtx.RunID, "sandbox", meta)
	if err != nil {
		slog.WarnContext(ctx, "sandbox billing: deduct failed",
			"org_id", execCtx.OrgID,
			"run_id", execCtx.RunID,
			"credits", credits,
			"error", err,
		)
	}

	return result
}

func extractDurationMs(resultJSON map[string]any) int64 {
	if resultJSON == nil {
		return 0
	}
	switch v := resultJSON["duration_ms"].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	default:
		return 0
	}
}
