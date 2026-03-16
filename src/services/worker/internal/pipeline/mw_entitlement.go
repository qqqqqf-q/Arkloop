package pipeline

import (
	"context"
	"fmt"
	"time"

	sharedent "arkloop/services/shared/entitlement"
	"arkloop/services/worker/internal/data"
)

const errorClassQuotaExceeded = "entitlement.quota_exceeded"

// NewEntitlementMiddleware 在 run 开始前检查月度配额（runs_per_month、tokens_per_month）。
// 超限时写 run.failed 事件并短路 Pipeline。
// resolver 为 nil 时跳过检查（fail-open）。
func NewEntitlementMiddleware(
	resolver *sharedent.Resolver,
	runsRepo data.RunsRepository,
	eventsRepo data.RunEventsRepository,
	releaseSlot func(ctx context.Context, run data.Run),
) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if resolver == nil {
			return next(ctx, rc)
		}

		now := time.Now().UTC()
		year, month := now.Year(), int(now.Month())
		accountID := rc.Run.AccountID

		var releaseFn func()
		if rc.ReleaseSlot != nil {
			releaseFn = rc.ReleaseSlot
		} else if releaseSlot != nil {
			run := rc.Run
			releaseFn = func() { releaseSlot(ctx, run) }
		}

		// 检查 quota.runs_per_month
		runsLimit, err := resolver.ResolveInt(ctx, accountID, "quota.runs_per_month")
		if err != nil {
			return fmt.Errorf("entitlement middleware: resolve runs_per_month: %w", err)
		}
		if runsLimit > 0 {
			monthlyRuns, err := resolver.CountMonthlyRuns(ctx, accountID, year, month)
			if err != nil {
				return fmt.Errorf("entitlement middleware: count monthly runs: %w", err)
			}
			if monthlyRuns >= runsLimit {
				failed := rc.Emitter.Emit(
					"run.failed",
					map[string]any{
						"error_class": errorClassQuotaExceeded,
						"code":        "entitlement.runs_per_month_exceeded",
						"message":     fmt.Sprintf("monthly run limit reached (%d/%d)", monthlyRuns, runsLimit),
					},
					nil,
					StringPtr(errorClassQuotaExceeded),
				)
				return appendAndCommitSingle(ctx, rc.Pool, rc.Run, runsRepo, eventsRepo, failed, releaseFn, rc.BroadcastRDB, rc.EventBus)
			}
		}

		// 检查 quota.tokens_per_month
		tokensLimit, err := resolver.ResolveInt(ctx, accountID, "quota.tokens_per_month")
		if err != nil {
			return fmt.Errorf("entitlement middleware: resolve tokens_per_month: %w", err)
		}
		if tokensLimit > 0 {
			monthlyTokens, err := resolver.SumMonthlyTokens(ctx, accountID, year, month)
			if err != nil {
				return fmt.Errorf("entitlement middleware: sum monthly tokens: %w", err)
			}
			if monthlyTokens >= tokensLimit {
				failed := rc.Emitter.Emit(
					"run.failed",
					map[string]any{
						"error_class": errorClassQuotaExceeded,
						"code":        "entitlement.tokens_per_month_exceeded",
						"message":     fmt.Sprintf("monthly token limit reached (%d/%d)", monthlyTokens, tokensLimit),
					},
					nil,
					StringPtr(errorClassQuotaExceeded),
				)
				return appendAndCommitSingle(ctx, rc.Pool, rc.Run, runsRepo, eventsRepo, failed, releaseFn, rc.BroadcastRDB, rc.EventBus)
			}
		}

		// 检查积分余额
		creditBalance, err := resolver.GetCreditBalance(ctx, accountID)
		if err != nil {
			return fmt.Errorf("entitlement middleware: get credit balance: %w", err)
		}
		if creditBalance <= 0 {
			failed := rc.Emitter.Emit(
				"run.failed",
				map[string]any{
					"error_class": errorClassQuotaExceeded,
					"code":        "entitlement.credits_exhausted",
					"message":     "credit balance exhausted",
				},
				nil,
				StringPtr(errorClassQuotaExceeded),
			)
			return appendAndCommitSingle(ctx, rc.Pool, rc.Run, runsRepo, eventsRepo, failed, releaseFn, rc.BroadcastRDB, rc.EventBus)
		}

		return next(ctx, rc)
	}
}
