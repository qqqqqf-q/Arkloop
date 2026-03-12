package jobs

import (
	"context"
	"fmt"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

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
	logger       *observability.JSONLogger
	timeoutMin   int
}

func NewStaleRunReaper(
	runEventRepo *data.RunEventRepository,
	runLimiter *data.RunLimiter,
	auditRepo *data.AuditLogRepository,
	pool data.Querier,
	logger *observability.JSONLogger,
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
		r.logger.Error("stale run scan failed", observability.LogFields{}, map[string]any{
			"error": err.Error(),
		})
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
			runID := run.ID.String()
			r.logger.Error("force fail run failed", observability.LogFields{RunID: &runID}, map[string]any{
				"error": err.Error(),
			})
			continue
		} else if reaped {
			r.writeAudit(ctx, run)

			runID := run.ID.String()
			accountID := run.AccountID.String()
			r.logger.Info("stale run reaped", observability.LogFields{RunID: &runID, AccountID: &accountID}, nil)
		}
	}

	for accountID := range affectedAccounts {
		if err := r.runLimiter.SyncFromDB(ctx, r.pool, accountID); err != nil {
			aid := accountID.String()
			r.logger.Error("sync run counter failed", observability.LogFields{AccountID: &aid}, map[string]any{
				"error": err.Error(),
			})
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
		AccountID:      &accountID,
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
		r.logger.Error("reaper audit write failed", observability.LogFields{}, map[string]any{
			"error":  err.Error(),
			"run_id": run.ID.String(),
		})
	}
}
