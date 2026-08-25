package scheduled_job_manage

import (
	"context"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/tools"

	"github.com/jackc/pgx/v5/pgxpool"
)

type desktopExecutor struct {
	common *executorCommon
}

// New 返回 desktop 下的 scheduled_job_manage executor。
func New(_ *pgxpool.Pool) tools.Executor {
	return &desktopExecutor{}
}

// SetDesktopDB 注入 DesktopDB（desktop 模式下由 composition 调用）。
func (e *desktopExecutor) SetDesktopDB(db data.DesktopDB) {
	e.common = &executorCommon{
		db:   db,
		repo: data.DesktopScheduledJobsRepository{},
	}
}

func (e *desktopExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	rawInput string,
) tools.ExecutionResult {
	return e.common.Execute(ctx, toolName, args, execCtx, rawInput)
}
