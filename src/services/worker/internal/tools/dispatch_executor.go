package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/skillstore"
	sharedtoolruntime "arkloop/services/shared/toolruntime"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/memory"
	"github.com/google/uuid"
)

const (
	ErrorClassToolNotRegistered   = "tool.not_registered"
	ErrorClassToolExecutionFailed = "tool.execution_failed"
)

type ExecutionContext struct {
	RunID               uuid.UUID
	TraceID             string
	OrgID               *uuid.UUID
	ThreadID            *uuid.UUID
	ProjectID           *uuid.UUID
	UserID              *uuid.UUID
	ProfileRef          string
	WorkspaceRef        string
	EnabledSkills       []skillstore.ResolvedSkill
	AgentID             string
	TimeoutMs           *int
	Budget              map[string]any
	PerToolSoftLimits   PerToolSoftLimits
	Emitter             events.Emitter
	PendingMemoryWrites *memory.PendingWriteBuffer
	RuntimeSnapshot     *sharedtoolruntime.RuntimeSnapshot
}

type ExecutionError struct {
	ErrorClass string
	Message    string
	Details    map[string]any
}

func (e *ExecutionError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e ExecutionError) ToJSON() map[string]any {
	payload := map[string]any{
		"error_class": e.ErrorClass,
		"message":     e.Message,
	}
	if len(e.Details) > 0 {
		payload["details"] = e.Details
	}
	return payload
}

type ExecutionResult struct {
	ResultJSON map[string]any
	Error      *ExecutionError
	DurationMs int
	Usage      map[string]any
	Events     []events.RunEvent
}

type Executor interface {
	Execute(
		ctx context.Context,
		toolName string,
		args map[string]any,
		context ExecutionContext,
		toolCallID string,
	) ExecutionResult
}

type DispatchingExecutor struct {
	registry       *Registry
	policyEnforcer *PolicyEnforcer
	executors      map[string]Executor
	llmNameIndex   map[string]string
}

func NewDispatchingExecutor(registry *Registry, policyEnforcer *PolicyEnforcer) *DispatchingExecutor {
	return &DispatchingExecutor{
		registry:       registry,
		policyEnforcer: policyEnforcer,
		executors:      map[string]Executor{},
		llmNameIndex:   map[string]string{},
	}
}

func (e *DispatchingExecutor) Bind(toolName string, executor Executor) error {
	spec, ok := e.registry.Get(toolName)
	if !ok {
		return fmt.Errorf("tool not registered: %s", toolName)
	}
	e.executors[toolName] = executor

	llmName := strings.TrimSpace(spec.LlmName)
	if llmName == "" {
		llmName = spec.Name
	}
	if llmName != "" {
		if existing, exists := e.llmNameIndex[llmName]; exists && existing != toolName {
			return fmt.Errorf("tool llm name conflict: %s mapped to %s and %s", llmName, existing, toolName)
		}
		e.llmNameIndex[llmName] = toolName
	}
	return nil
}

func (e *DispatchingExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	context ExecutionContext,
	toolCallID string,
) ExecutionResult {
	started := time.Now()

	resolvedName := e.resolveToolName(toolName)

	decision := e.policyEnforcer.RequestToolCall(context.Emitter, resolvedName, args, toolCallID)
	policyEvents := append([]events.RunEvent{}, decision.Events...)

	if !decision.Allowed {
		denyReason := ""
		if len(policyEvents) > 0 {
			last := policyEvents[len(policyEvents)-1]
			if value, ok := last.DataJSON["deny_reason"].(string); ok {
				denyReason = value
			}
		}
		return ExecutionResult{
			Error: &ExecutionError{
				ErrorClass: PolicyDeniedCode,
				Message:    "tool call denied by policy",
				Details: map[string]any{
					"tool_name":    resolvedName,
					"tool_call_id": decision.ToolCallID,
					"deny_reason":  denyReason,
				},
			},
			DurationMs: durationMs(started),
			Events:     policyEvents,
		}
	}

	executor := e.executors[resolvedName]
	if executor == nil {
		return ExecutionResult{
			Error: &ExecutionError{
				ErrorClass: ErrorClassToolNotRegistered,
				Message:    "tool executor not bound",
				Details: map[string]any{
					"tool_name":    resolvedName,
					"tool_call_id": decision.ToolCallID,
				},
			},
			DurationMs: durationMs(started),
			Events:     policyEvents,
		}
	}

	var result ExecutionResult
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				result = ExecutionResult{
					Error: &ExecutionError{
						ErrorClass: ErrorClassToolExecutionFailed,
						Message:    "tool execution failed",
						Details: map[string]any{
							"tool_name":    resolvedName,
							"tool_call_id": decision.ToolCallID,
							"panic":        recovered,
						},
					},
				}
			}
		}()
		result = executor.Execute(ctx, resolvedName, args, context, decision.ToolCallID)
	}()

	result.DurationMs = durationMs(started)
	result.Events = append(policyEvents, result.Events...)
	return result
}

func (e *DispatchingExecutor) resolveToolName(toolName string) string {
	cleaned := strings.TrimSpace(toolName)
	if cleaned == "" {
		return toolName
	}
	if e.executors[cleaned] != nil {
		return cleaned
	}
	if mapped, ok := e.llmNameIndex[cleaned]; ok && strings.TrimSpace(mapped) != "" {
		return mapped
	}
	return cleaned
}

func durationMs(started time.Time) int {
	elapsed := time.Since(started)
	millis := int(elapsed / time.Millisecond)
	if millis < 0 {
		return 0
	}
	return millis
}
