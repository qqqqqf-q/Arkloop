package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"

	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/security"
)

// NewInjectionScanMiddleware 在 Pipeline 中执行注入扫描。
// composite 为 nil 时整个 middleware 为 no-op。
func NewInjectionScanMiddleware(
	composite *security.CompositeScanner,
	auditor *security.SecurityAuditor,
	configResolver sharedconfig.Resolver,
	eventsRepo data.RunEventsRepository,
) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if composite == nil {
			security.ScanTotal.WithLabelValues("skipped").Inc()
			return next(ctx, rc)
		}

		regexEnabled := resolveEnabled(configResolver, "security.injection_scan.regex_enabled", true)
		semanticEnabled := resolveEnabled(configResolver, "security.injection_scan.semantic_enabled", true)
		blockingEnabled := resolveEnabled(configResolver, "security.injection_scan.blocking_enabled", false)
		toolScanEnabled := resolveEnabled(configResolver, "security.injection_scan.tool_output_scan_enabled", true)

		if !regexEnabled && !semanticEnabled {
			return next(ctx, rc)
		}

		emitRunEvent(ctx, rc, eventsRepo, "security.scan.started", map[string]any{
			"regex_enabled":    regexEnabled,
			"semantic_enabled": semanticEnabled,
		})

		var allDetections []security.ScanResult
		var semanticResult *security.SemanticResult
		injectionDetected := false

		for _, msg := range rc.Messages {
			if msg.Role != "user" {
				continue
			}
			for _, part := range msg.Content {
				text := strings.TrimSpace(part.Text)
				if text == "" {
					continue
				}

				result := composite.Scan(text)

				if regexEnabled {
					for _, r := range result.RegexMatches {
						slog.WarnContext(ctx, "injection pattern detected",
							"run_id", rc.Run.ID,
							"pattern_id", r.PatternID,
							"category", r.Category,
							"severity", r.Severity,
						)
						security.DetectionTotal.WithLabelValues(r.Category).Inc()
					}
					allDetections = append(allDetections, result.RegexMatches...)
				}

				if semanticEnabled && result.SemanticResult != nil {
					if result.SemanticResult.IsInjection {
						slog.WarnContext(ctx, "semantic injection detected",
							"run_id", rc.Run.ID,
							"label", result.SemanticResult.Label,
							"score", result.SemanticResult.Score,
						)
						security.DetectionTotal.WithLabelValues("semantic_"+strings.ToLower(result.SemanticResult.Label)).Inc()
						injectionDetected = true
					}
					semanticResult = result.SemanticResult
				}

				if result.IsInjection {
					injectionDetected = true
				}
			}
		}

		if injectionDetected || len(allDetections) > 0 {
			security.ScanTotal.WithLabelValues("detected").Inc()
			auditor.EmitInjectionDetected(ctx, rc.Run.ID, rc.Run.AccountID, rc.UserID, allDetections)

			patterns := make([]map[string]any, 0, len(allDetections))
			for _, d := range allDetections {
				patterns = append(patterns, map[string]any{
					"pattern_id": d.PatternID,
					"category":   d.Category,
					"severity":   d.Severity,
				})
			}
			eventData := map[string]any{
				"detection_count": len(allDetections),
				"patterns":        patterns,
				"injection":       injectionDetected,
			}
			if semanticResult != nil {
				eventData["semantic"] = map[string]any{
					"label": semanticResult.Label,
					"score": semanticResult.Score,
				}
			}
			emitRunEvent(ctx, rc, eventsRepo, "security.injection.detected", eventData)

			if blockingEnabled {
				return blockRun(ctx, rc, eventsRepo, eventData)
			}
		} else {
			security.ScanTotal.WithLabelValues("clean").Inc()
			emitRunEvent(ctx, rc, eventsRepo, "security.scan.clean", nil)
		}

		// 为 agent loop 注入 tool output 扫描函数
		if toolScanEnabled {
			rc.ToolOutputScanFunc = buildToolOutputScanFunc(composite, regexEnabled, semanticEnabled, auditor, eventsRepo, rc)
		}

		return next(ctx, rc)
	}
}

// blockRun 拦截注入请求：写入 blocked 事件 + run.failed，更新 run 状态。
func blockRun(ctx context.Context, rc *RunContext, eventsRepo data.RunEventsRepository, detectionData map[string]any) error {
	emitRunEvent(ctx, rc, eventsRepo, "security.injection.blocked", detectionData)

	failedData := map[string]any{
		"error_class": "security.injection_blocked",
		"message":     "message blocked: injection detected",
	}
	failedEvent := rc.Emitter.Emit("run.failed", failedData, nil, StringPtr("security.injection_blocked"))

	tx, err := rc.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("injection block tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := eventsRepo.AppendEvent(ctx, tx, rc.Run.ID, failedEvent.Type, failedEvent.DataJSON, failedEvent.ToolName, failedEvent.ErrorClass); err != nil {
		return fmt.Errorf("injection block append event: %w", err)
	}

	runsRepo := data.RunsRepository{}
	if err := runsRepo.UpdateRunTerminalStatus(ctx, tx, rc.Run.ID, data.TerminalStatusUpdate{
		Status: "failed",
	}); err != nil {
		return fmt.Errorf("injection block update status: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("injection block commit: %w", err)
	}

	slog.WarnContext(ctx, "run blocked: injection detected", "run_id", rc.Run.ID)
	return nil
}

// buildToolOutputScanFunc 构建 tool output 扫描函数，用于 agent loop 中的间接注入检测。
func buildToolOutputScanFunc(
	composite *security.CompositeScanner,
	regexEnabled, semanticEnabled bool,
	auditor *security.SecurityAuditor,
	eventsRepo data.RunEventsRepository,
	rc *RunContext,
) func(string, string) (string, bool) {
	return func(toolName, text string) (string, bool) {
		result := composite.Scan(text)

		detected := false
		var allDetections []security.ScanResult
		var semanticResult *security.SemanticResult

		if regexEnabled && len(result.RegexMatches) > 0 {
			detected = true
			allDetections = result.RegexMatches
			for _, r := range result.RegexMatches {
				security.DetectionTotal.WithLabelValues("tool_output_" + r.Category).Inc()
			}
		}
		if semanticEnabled && result.SemanticResult != nil {
			semanticResult = result.SemanticResult
			if result.SemanticResult.IsInjection {
				detected = true
				security.DetectionTotal.WithLabelValues("tool_output_semantic_" + strings.ToLower(result.SemanticResult.Label)).Inc()
			}
		}

		// 闭包内无请求级 context，用 Background 做 best-effort 记录
		ctx := context.Background()

		if detected {
			slog.Warn("indirect injection detected in tool output",
				"run_id", rc.Run.ID,
				"tool_name", toolName,
				"regex_matches", len(allDetections),
			)
			security.ScanTotal.WithLabelValues("tool_output_detected").Inc()

			auditor.EmitToolOutputInjectionDetected(ctx, rc.Run.ID, rc.Run.AccountID, rc.UserID, toolName, allDetections, semanticResult)

			eventData := map[string]any{
				"tool_name":       toolName,
				"detection_count": len(allDetections),
			}
			if semanticResult != nil {
				eventData["semantic"] = map[string]any{
					"label": semanticResult.Label,
					"score": semanticResult.Score,
				}
			}
			emitRunEvent(ctx, rc, eventsRepo, "security.tool_output.detected", eventData)

			return "[content filtered: potential injection detected in tool output]", true
		}

		security.ScanTotal.WithLabelValues("tool_output_clean").Inc()
		emitRunEvent(ctx, rc, eventsRepo, "security.tool_output.clean", map[string]any{
			"tool_name": toolName,
		})

		return "", false
	}
}

func emitRunEvent(ctx context.Context, rc *RunContext, eventsRepo data.RunEventsRepository, eventType string, dataJSON map[string]any) {
	ev := rc.Emitter.Emit(eventType, dataJSON, nil, nil)
	tx, err := rc.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		slog.WarnContext(ctx, "injection scan event tx begin failed", "error", err)
		return
	}
	defer tx.Rollback(ctx)
	if _, err := eventsRepo.AppendEvent(ctx, tx, rc.Run.ID, ev.Type, ev.DataJSON, ev.ToolName, ev.ErrorClass); err != nil {
		slog.WarnContext(ctx, "injection scan event append failed", "error", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		slog.WarnContext(ctx, "injection scan event commit failed", "error", err)
	}
}
