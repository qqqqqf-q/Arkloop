package pipeline

import (
	"context"
	"log/slog"
	"strings"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/tools"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	settingResultSummarizerEnabled   = "tool_result_summarizer.enabled"
	settingResultSummarizerModel     = "tool_result_summarizer.model"
	defaultResultSummarizerThreshold = 64 * 1024
)

// NewResultSummarizerMiddleware 从 platform_settings 读取配置，按需为 ToolExecutor
// 注入 ResultSummarizer（Layer 2 LLM 压缩）。
func NewResultSummarizerMiddleware(
	pool *pgxpool.Pool,
	stubGateway llm.Gateway,
	emitDebugEvents bool,
	llmMaxResponseBytes int,
	loaders ...*routing.ConfigLoader,
) RunMiddleware {
	var configLoader *routing.ConfigLoader
	if len(loaders) > 0 {
		configLoader = loaders[0]
	}
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if pool == nil || rc.ToolExecutor == nil {
			return next(ctx, rc)
		}

		enabled, model := loadResultSummarizerSettings(ctx, pool)
		if !enabled || model == "" {
			return next(ctx, rc)
		}

		fallbackGateway := rc.Gateway
		fallbackModel := ""
		if rc.SelectedRoute != nil {
			fallbackModel = rc.SelectedRoute.Route.Model
		}
		accountID := &rc.Run.AccountID

		gateway, resolvedModel := resolveTitleGateway(
			ctx, pool, accountID,
			fallbackGateway, fallbackModel,
			stubGateway, emitDebugEvents,
			llmMaxResponseBytes, configLoader,
		)
		if gateway == nil {
			slog.WarnContext(ctx, "result_summarizer: gateway resolve failed, skipping")
			return next(ctx, rc)
		}
		if resolvedModel == "" {
			resolvedModel = model
		}

		summarizer := tools.NewResultSummarizer(gateway, resolvedModel, defaultResultSummarizerThreshold)
		rc.ToolExecutor.SetSummarizer(summarizer)

		return next(ctx, rc)
	}
}

func loadResultSummarizerSettings(ctx context.Context, pool *pgxpool.Pool) (enabled bool, model string) {
	rows, err := pool.Query(ctx,
		`SELECT key, value FROM platform_settings WHERE key = ANY($1)`,
		[]string{settingResultSummarizerEnabled, settingResultSummarizerModel},
	)
	if err != nil {
		return false, ""
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		switch k {
		case settingResultSummarizerEnabled:
			enabled = strings.TrimSpace(v) == "true"
		case settingResultSummarizerModel:
			model = strings.TrimSpace(v)
		}
	}
	return enabled, model
}
