package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"arkloop/services/shared/skillstore"
	sharedtoolruntime "arkloop/services/shared/toolruntime"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"
	"github.com/google/uuid"
)

const (
	ErrorClassToolNotRegistered   = "tool.not_registered"
	ErrorClassToolExecutionFailed = "tool.execution_failed"
)

type ExecutionContext struct {
	RunID                            uuid.UUID
	TraceID                          string
	AccountID                        *uuid.UUID
	ThreadID                         *uuid.UUID
	ProjectID                        *uuid.UUID
	UserID                           *uuid.UUID
	ProfileRef                       string
	WorkspaceRef                     string
	WorkDir                          string
	EnabledSkills                    []skillstore.ResolvedSkill
	ToolAllowlist                    []string
	ToolDenylist                     []string
	ActiveToolProviderConfigsByGroup map[string]sharedtoolruntime.ProviderConfig
	RouteID                          string
	Model                            string
	MemoryScope                      string
	AgentID                          string
	TimeoutMs                        *int
	Budget                           map[string]any
	PerToolSoftLimits                PerToolSoftLimits
	Emitter                          events.Emitter
	PendingMemoryWrites              *memory.PendingWriteBuffer
	RuntimeSnapshot                  *sharedtoolruntime.RuntimeSnapshot
	GenerativeUIReadMeSeen           bool
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

// NotConfiguredChecker is implemented by executors that represent unconfigured tool backends.
type NotConfiguredChecker interface {
	IsNotConfigured() bool
}

// ToolActivator allows dynamic tool activation during agent loop execution.
// search_tools executor calls Activate to mark tools for injection;
// the agent loop calls DrainActivated to collect and append them to request.Tools.
type ToolActivator interface {
	Activate(specs ...llm.ToolSpec)
	DrainActivated() []llm.ToolSpec
}

type DispatchingExecutor struct {
	registry       *Registry
	policyEnforcer *PolicyEnforcer
	executors      map[string]Executor
	llmNameIndex   map[string]string

	// search_tools support: specs not in request.Tools, available via search_tools
	searchableSpecs map[string]llm.ToolSpec
	activatedMu     sync.Mutex
	activatedSpecs  []llm.ToolSpec

	summarizer             *ResultSummarizer
	generativeUIReadMeSeen bool
}

func NewDispatchingExecutor(registry *Registry, policyEnforcer *PolicyEnforcer) *DispatchingExecutor {
	return &DispatchingExecutor{
		registry:        registry,
		policyEnforcer:  policyEnforcer,
		executors:       map[string]Executor{},
		llmNameIndex:    map[string]string{},
		searchableSpecs: map[string]llm.ToolSpec{},
	}
}

// SetSummarizer attaches a ResultSummarizer for Layer 2 LLM-based compression.
func (e *DispatchingExecutor) SetSummarizer(s *ResultSummarizer) {
	e.summarizer = s
}

// SetSearchableSpecs stores tool specs that are not initially visible to the LLM
// but can be loaded via search_tools.
func (e *DispatchingExecutor) SetSearchableSpecs(specs map[string]llm.ToolSpec) {
	e.searchableSpecs = specs
}

// SearchableSpecs returns the current searchable spec map (read-only usage).
func (e *DispatchingExecutor) SearchableSpecs() map[string]llm.ToolSpec {
	return e.searchableSpecs
}

func (e *DispatchingExecutor) Activate(specs ...llm.ToolSpec) {
	e.activatedMu.Lock()
	defer e.activatedMu.Unlock()
	e.activatedSpecs = append(e.activatedSpecs, specs...)
}

func (e *DispatchingExecutor) DrainActivated() []llm.ToolSpec {
	e.activatedMu.Lock()
	defer e.activatedMu.Unlock()
	out := e.activatedSpecs
	e.activatedSpecs = nil
	return out
}

func (e *DispatchingExecutor) ToolCallEvent(
	emitter events.Emitter,
	toolName string,
	args map[string]any,
	toolCallID string,
) events.RunEvent {
	resolvedName := e.resolveToolName(toolName)
	if e.policyEnforcer == nil {
		payload := map[string]any{
			"tool_call_id": strings.TrimSpace(toolCallID),
			"tool_name":    resolvedName,
			"arguments":    args,
		}
		return emitter.Emit("tool.call", payload, stringPtr(resolvedName), nil)
	}
	return e.policyEnforcer.BuildToolCallEvent(emitter, resolvedName, args, toolCallID)
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
		context.GenerativeUIReadMeSeen = e.generativeUIReadMeSeen
		result = executor.Execute(ctx, resolvedName, args, context, decision.ToolCallID)
	}()

	// Layer 1: smart truncation — use CompressTargetBytes as the LLM-facing budget,
	// independent from the executor-level MaxOutputBytes truncation.
	if result.ResultJSON != nil && result.Error == nil && !ShouldBypassResultCompression(resolvedName) {
		result = CompressResult(resolvedName, result, CompressTargetBytes)
	}

	// Layer 2: LLM summarization
	if e.summarizer != nil && result.ResultJSON != nil && result.Error == nil && !ShouldBypassResultSummarization(resolvedName) {
		if raw, _ := json.Marshal(result.ResultJSON); len(raw) > e.summarizer.threshold {
			result = e.summarizer.Summarize(ctx, resolvedName, result)
		}
	}
	if result.Error == nil && IsGenerativeUIBootstrapTool(resolvedName) {
		e.generativeUIReadMeSeen = true
	}

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
