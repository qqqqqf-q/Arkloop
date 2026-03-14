package security

import (
	"context"
	"time"

	"arkloop/services/shared/plugin"

	"github.com/google/uuid"
)

// SecurityAuditor 将安全事件写入审计日志。
type SecurityAuditor struct {
	sink plugin.AuditSink
}

// NewSecurityAuditor 创建 SecurityAuditor。sink 为 nil 时所有方法为 no-op。
func NewSecurityAuditor(sink plugin.AuditSink) *SecurityAuditor {
	return &SecurityAuditor{sink: sink}
}

// EmitInjectionDetected 记录注入检测事件到审计日志。
func (a *SecurityAuditor) EmitInjectionDetected(
	ctx context.Context,
	runID uuid.UUID,
	accountID uuid.UUID,
	actorUserID *uuid.UUID,
	results []ScanResult,
) {
	if a == nil || a.sink == nil || len(results) == 0 {
		return
	}

	patterns := make([]map[string]string, 0, len(results))
	for _, r := range results {
		patterns = append(patterns, map[string]string{
			"pattern_id": r.PatternID,
			"category":   r.Category,
			"severity":   r.Severity,
		})
	}

	var actor uuid.UUID
	if actorUserID != nil {
		actor = *actorUserID
	}

	targetType := "run"
	targetID := runID.String()

	_ = a.sink.Emit(ctx, plugin.AuditEvent{
		Timestamp:  time.Now().UTC(),
		ActorID:    actor,
		AccountID:  accountID,
		Action:     "security.injection_detected",
		Resource:   targetType,
		ResourceID: targetID,
		Detail: map[string]any{
			"detection_count": len(results),
			"patterns":        patterns,
		},
	})
}

// EmitToolOutputInjectionDetected 记录工具输出间接注入检测事件到审计日志。
func (a *SecurityAuditor) EmitToolOutputInjectionDetected(
	ctx context.Context,
	runID uuid.UUID,
	accountID uuid.UUID,
	actorUserID *uuid.UUID,
	toolName string,
	results []ScanResult,
	semantic *SemanticResult,
) {
	if a == nil || a.sink == nil {
		return
	}

	var actor uuid.UUID
	if actorUserID != nil {
		actor = *actorUserID
	}

	detail := map[string]any{
		"tool_name":       toolName,
		"detection_count": len(results),
	}

	if len(results) > 0 {
		patterns := make([]map[string]string, 0, len(results))
		for _, r := range results {
			patterns = append(patterns, map[string]string{
				"pattern_id": r.PatternID,
				"category":   r.Category,
				"severity":   r.Severity,
			})
		}
		detail["patterns"] = patterns
	}

	if semantic != nil {
		detail["semantic"] = map[string]any{
			"label": semantic.Label,
			"score": semantic.Score,
		}
	}

	_ = a.sink.Emit(ctx, plugin.AuditEvent{
		Timestamp:  time.Now().UTC(),
		ActorID:    actor,
		AccountID:  accountID,
		Action:     "security.tool_output_injection_detected",
		Resource:   "run",
		ResourceID: runID.String(),
		Detail:     detail,
	})
}
