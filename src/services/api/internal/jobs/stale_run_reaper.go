package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
)

const (
	reaperInterval = 60 * time.Second
)

// StaleRunReaper 定期扫描卡死的 run，强制标记为 failed 并重置 Redis 计数器。
type StaleRunReaper struct {
	runEventRepo *data.RunEventRepository
	runLimiter   *data.RunLimiter
	auditRepo    *data.AuditLogRepository
	pool         data.Querier
	logger       *slog.Logger
	timeoutMin   int
}

func NewStaleRunReaper(
	runEventRepo *data.RunEventRepository,
	runLimiter *data.RunLimiter,
	auditRepo *data.AuditLogRepository,
	pool data.Querier,
	logger *slog.Logger,
	timeoutMinutes int,
) *StaleRunReaper {
	return &StaleRunReaper{
		runEventRepo: runEventRepo,
		runLimiter:   runLimiter,
		auditRepo:    auditRepo,
		pool:         pool,
		logger:       logger,
		timeoutMin:   timeoutMinutes,
	}
}

// Run 启动后台循环，ctx 取消时退出。
func (r *StaleRunReaper) Run(ctx context.Context) {
	r.reap(ctx)

	ticker := time.NewTicker(reaperInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reap(ctx)
		}
	}
}

func (r *StaleRunReaper) reap(ctx context.Context) {
	staleBefore := time.Now().UTC().Add(-time.Duration(r.timeoutMin) * time.Minute)

	staleRuns, err := r.runEventRepo.ListStaleRunning(ctx, staleBefore)
	if err != nil {
		r.logger.Error("stale run scan failed", "error", err.Error())
		return
	}
	if len(staleRuns) == 0 {
		return
	}

	// 收集需要同步计数器的 org
	affectedAccounts := map[uuid.UUID]struct{}{}

	for _, run := range staleRuns {
		// 无论 ForceFailRun 结果如何都纳入 sync，SyncFromDB 能修正任意状态下的计数器
		affectedAccounts[run.AccountID] = struct{}{}

		if reaped, err := r.runEventRepo.ForceFailRun(ctx, run.ID); err != nil {
			r.logger.Error("force fail run failed", "run_id", run.ID.String(), "error", err.Error())
			continue
		} else if reaped {
			r.writeAudit(ctx, run)
			r.logger.Info("stale run reaped", "run_id", run.ID.String(), "account_id", run.AccountID.String())
		}
	}

	for accountID := range affectedAccounts {
		if err := r.runLimiter.SyncFromDB(ctx, r.pool, accountID); err != nil {
			r.logger.Error("sync run counter failed", "account_id", accountID.String(), "error", err.Error())
		}
	}
}

func (r *StaleRunReaper) writeAudit(ctx context.Context, run data.Run) {
	if r.auditRepo == nil {
		return
	}

	traceID := fmt.Sprintf("reaper-%s", run.ID.String())
	targetType := "run"
	targetID := run.ID.String()
	accountID := run.AccountID

	if err := r.auditRepo.Create(ctx, data.AuditLogCreateParams{
		AccountID:  &accountID,
		Action:     "runs.force_expired",
		TargetType: &targetType,
		TargetID:   &targetID,
		TraceID:    traceID,
		Metadata: map[string]any{
			"actor":       "system",
			"error_class": "worker.timeout",
			"timeout_min": r.timeoutMin,
		},
	}); err != nil {
		r.logger.Error("reaper audit write failed", "run_id", run.ID.String(), "error", err.Error())
	}
}
