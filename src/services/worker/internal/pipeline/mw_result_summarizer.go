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

// NewResultSummarizerMiddleware 从系统 summarize persona 读取配置，按需为 ToolExecutor
// 注入 ResultSummarizer（Layer 2 LLM 压缩）。
func NewResultSummarizerMiddleware(
	pool *pgxpool.Pool,
	auxGateway llm.Gateway,
	emitDebugEvents bool,
	llmMaxResponseBytes int,
	loaders ...*routing.ConfigLoader,
) RunMiddleware {
	var configLoader *routing.ConfigLoader
	if len(loaders) > 0 {
		configLoader = loaders[0]
	}
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc.ToolExecutor == nil {
			return next(ctx, rc)
		}
		if rc.ResultSummarizer == nil {
			return next(ctx, rc)
		}

		fallbackGateway := rc.Gateway
		fallbackModel := ""
		if rc.SelectedRoute != nil {
			fallbackModel = rc.SelectedRoute.Route.Model
		}
		accountID := &rc.Run.AccountID

		model := ""
		if rc.SummarizerDefinition != nil && rc.SummarizerDefinition.Model != nil {
			model = strings.TrimSpace(*rc.SummarizerDefinition.Model)
		}
		resolvedFallbackModel := fallbackModel
		if model != "" {
			resolvedFallbackModel = model
		}

		gateway := fallbackGateway
		resolvedModel := resolvedFallbackModel
		if pool != nil {
			gateway, resolvedModel = resolveTitleGateway(
				ctx, pool, accountID,
				fallbackGateway, resolvedFallbackModel,
				auxGateway, emitDebugEvents,
				llmMaxResponseBytes, configLoader,
				rc.RoutingByokEnabled,
			)
			if gateway == nil {
				slog.WarnContext(ctx, "result_summarizer: gateway resolve failed, skipping")
				return next(ctx, rc)
			}
		}
		if resolvedModel == "" {
			resolvedModel = resolvedFallbackModel
		}
		if resolvedModel == "" {
			return next(ctx, rc)
		}

		summarizer := tools.NewResultSummarizer(gateway, resolvedModel, rc.ResultSummarizer.ThresholdBytes, tools.ResultSummarizerConfig{
			Prompt:    rc.ResultSummarizer.Prompt,
			MaxTokens: rc.ResultSummarizer.MaxTokens,
		})
		rc.ToolExecutor.SetSummarizer(summarizer)

		return next(ctx, rc)
	}
}
