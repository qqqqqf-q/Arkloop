package pipeline

import (
	"context"
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

		if !regexEnabled && !semanticEnabled {
			return next(ctx, rc)
		}

		emitRunEvent(ctx, rc, eventsRepo, "security.scan.started", map[string]any{
			"regex_enabled":    regexEnabled,
			"semantic_enabled": semanticEnabled,
		})

		var allDetections []security.ScanResult
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

				if semanticEnabled && result.SemanticResult != nil && result.SemanticResult.IsInjection {
					slog.WarnContext(ctx, "semantic injection detected",
						"run_id", rc.Run.ID,
						"label", result.SemanticResult.Label,
						"score", result.SemanticResult.Score,
					)
					security.DetectionTotal.WithLabelValues("semantic_"+strings.ToLower(result.SemanticResult.Label)).Inc()
					injectionDetected = true
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
			emitRunEvent(ctx, rc, eventsRepo, "security.injection.detected", map[string]any{
				"detection_count": len(allDetections),
				"patterns":        patterns,
				"injection":       injectionDetected,
			})
		} else {
			security.ScanTotal.WithLabelValues("clean").Inc()
			emitRunEvent(ctx, rc, eventsRepo, "security.scan.clean", nil)
		}

		return next(ctx, rc)
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
