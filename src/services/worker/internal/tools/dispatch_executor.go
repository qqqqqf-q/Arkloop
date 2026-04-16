package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"arkloop/services/shared/skillstore"
	sharedtoolruntime "arkloop/services/shared/toolruntime"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/subagentctl"
	"github.com/google/uuid"
)

const (
	ErrorClassToolNotRegistered   = "tool.not_registered"
	ErrorClassToolExecutionFailed = "tool.execution_failed"
	ErrorClassToolHardTimeout     = "tool.hard_timeout"
)

type Tracer interface {
	Event(middleware, event string, fields map[string]any)
}

type ExecutionContext struct {
	RunID                            uuid.UUID
	TraceID                          string
	Tracer                           Tracer
	AccountID                        *uuid.UUID
	ThreadID                         *uuid.UUID
	ProjectID                        *uuid.UUID
	UserID                           *uuid.UUID
	ProfileRef                       string
	WorkspaceRef                     string
	WorkDir                          string
	EnabledSkills                    []skillstore.ResolvedSkill
	ExternalSkills                   []skillstore.ExternalSkill
	ToolAllowlist                    []string
	ToolDenylist                     []string
	PersonaID                        string
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
	PromptCacheSnapshot              *subagentctl.PromptCacheSnapshot
	GenerativeUIReadMeSeen           bool
	Channel                          *ChannelToolSurface
	// PipelineRC 由 agent.simple 注入为 *pipeline.RunContext；其它路径为 nil。
	PipelineRC  any
	StreamEvent func(events.RunEvent) error
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

// ContentAttachment 承载工具返回的多模态附件（如图片），由 agent loop 注入 tool result message。
type ContentAttachment struct {
	MimeType      string
	Data          []byte
	AttachmentKey string
}

type ExecutionResult struct {
	ResultJSON   map[string]any
	ContentParts []ContentAttachment // 多模态附件，主模型支持时直接注入 tool result
	Error        *ExecutionError
	DurationMs   int
	Usage        map[string]any
	Events       []events.RunEvent
	Streamed     bool
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
// load_tools executor calls Activate to mark tools for injection;
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

	// load_tools support: specs not in request.Tools, available via load_tools
	searchableSpecs map[string]llm.ToolSpec
	activatedMu     sync.Mutex
	activatedSpecs  []llm.ToolSpec

	summarizer             *ResultSummarizer
	generativeUIReadMeSeen bool
}

type toolIdentity struct {
	LogicalName  string
	ResolvedName string
}

func (e *DispatchingExecutor) ToolCapabilities(toolName string) ToolCapabilities {
	resolved := e.resolveToolIdentity(toolName).ResolvedName
	if e.registry == nil {
		return ToolCapabilities{
			InterruptBehavior: InterruptBehaviorBlock,
			HardTimeoutMode:   HardTimeoutModeEnforced,
		}
	}
	spec, ok := e.registry.Get(resolved)
	if !ok {
		return ToolCapabilities{
			InterruptBehavior: InterruptBehaviorBlock,
			HardTimeoutMode:   HardTimeoutModeEnforced,
		}
	}
	return spec.Capabilities()
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
// but can be loaded via load_tools.
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
	identity := e.resolveToolIdentity(toolName)
	if e.policyEnforcer == nil {
		payload := map[string]any{
			"tool_call_id": strings.TrimSpace(toolCallID),
			"tool_name":    identity.LogicalName,
			"arguments":    args,
		}
		if identity.ResolvedName != "" {
			payload["resolved_tool_name"] = identity.ResolvedName
		}
		return emitter.Emit("tool.call", payload, stringPtr(identity.LogicalName), nil)
	}
	return e.policyEnforcer.BuildToolCallEvent(emitter, identity.LogicalName, args, toolCallID, identity.ResolvedName)
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
	execContext ExecutionContext,
	toolCallID string,
) ExecutionResult {
	started := time.Now()

	identity := e.resolveToolIdentity(toolName)
	logicalName := identity.LogicalName
	resolvedName := identity.ResolvedName

	decision := e.policyEnforcer.RequestToolCall(execContext.Emitter, logicalName, args, toolCallID, resolvedName)
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
					"tool_name":    logicalName,
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
					"tool_name":    logicalName,
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
							"tool_name":    logicalName,
							"tool_call_id": decision.ToolCallID,
							"panic":        recovered,
						},
					},
				}
			}
		}()
		execContext.GenerativeUIReadMeSeen = e.generativeUIReadMeSeen
		execCtx := ctx
		cancelTimeout := func() {}
		capabilities := e.ToolCapabilities(resolvedName)
		if execContext.TimeoutMs != nil && *execContext.TimeoutMs > 0 && capabilities.HardTimeoutMode != HardTimeoutModeIgnored {
			execCtx, cancelTimeout = context.WithTimeout(ctx, time.Duration(*execContext.TimeoutMs)*time.Millisecond)
		}
		defer cancelTimeout()
		result = runExecutorWithHardTimeout(execCtx, executor, logicalName, resolvedName, args, execContext, decision.ToolCallID)
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) && result.Error == nil {
			result = ExecutionResult{
				Error: &ExecutionError{
					ErrorClass: ErrorClassToolHardTimeout,
					Message:    "tool execution reached hard timeout",
					Details: map[string]any{
						"tool_name":    logicalName,
						"tool_call_id": decision.ToolCallID,
						"timeout_ms":   *execContext.TimeoutMs,
					},
				},
			}
		}
	}()

	// Layer 0.5: persist very large outputs to disk for later retrieval.
	var rawResultJSON []byte
	if result.ResultJSON != nil && result.Error == nil && !ShouldBypassResultCompression(logicalName) {
		rawResultJSON, _ = json.Marshal(result.ResultJSON)
		if len(rawResultJSON) > PersistThreshold {
			result = PersistLargeResult(ctx, execContext, decision.ToolCallID, rawResultJSON, result)
		}
	}

	// Layer 1: smart truncation — use CompressTargetBytes as the LLM-facing budget,
	// independent from the executor-level MaxOutputBytes truncation.
	persisted := false
	if p, ok := result.ResultJSON["persisted"].(bool); ok && p {
		persisted = true
	}
	if result.ResultJSON != nil && result.Error == nil && !ShouldBypassResultCompression(logicalName) && !persisted {
		result = CompressResult(logicalName, result, CompressTargetBytes)
	}

	// Layer 2: LLM summarization
	if e.summarizer != nil && result.ResultJSON != nil && result.Error == nil && !ShouldBypassResultSummarization(logicalName) && !persisted {
		if rawResultJSON == nil {
			rawResultJSON, _ = json.Marshal(result.ResultJSON)
		}
		if len(rawResultJSON) > e.summarizer.threshold {
			result = e.summarizer.Summarize(ctx, logicalName, result)
		}
	}
	if result.Error == nil && IsGenerativeUIBootstrapTool(logicalName) {
		e.generativeUIReadMeSeen = true
	}

	result.DurationMs = durationMs(started)
	result.Events = append(policyEvents, result.Events...)
	return result
}

func runExecutorWithHardTimeout(
	ctx context.Context,
	executor Executor,
	logicalToolName string,
	resolvedToolName string,
	args map[string]any,
	execContext ExecutionContext,
	toolCallID string,
) ExecutionResult {
	resultCh := make(chan ExecutionResult, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				resultCh <- ExecutionResult{
					Error: &ExecutionError{
						ErrorClass: ErrorClassToolExecutionFailed,
						Message:    "tool execution failed",
						Details: map[string]any{
							"tool_name":    logicalToolName,
							"tool_call_id": toolCallID,
							"panic":        recovered,
						},
					},
				}
			}
		}()
		resultCh <- executor.Execute(ctx, resolvedToolName, args, execContext, toolCallID)
	}()

	select {
	case result := <-resultCh:
		return result
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			timeoutMs := 0
			if execContext.TimeoutMs != nil && *execContext.TimeoutMs > 0 {
				timeoutMs = *execContext.TimeoutMs
			}
			return ExecutionResult{
				Error: &ExecutionError{
					ErrorClass: ErrorClassToolHardTimeout,
					Message:    "tool execution reached hard timeout",
					Details: map[string]any{
						"tool_name":    logicalToolName,
						"tool_call_id": toolCallID,
						"timeout_ms":   timeoutMs,
					},
				},
			}
		}
		return ExecutionResult{
			Error: &ExecutionError{
				ErrorClass: ErrorClassToolExecutionFailed,
				Message:    "tool execution cancelled",
				Details: map[string]any{
					"tool_name":    logicalToolName,
					"tool_call_id": toolCallID,
				},
			},
		}
	}
}

func (e *DispatchingExecutor) resolveToolName(toolName string) string {
	return e.resolveToolIdentity(toolName).ResolvedName
}

func (e *DispatchingExecutor) resolveToolIdentity(toolName string) toolIdentity {
	cleaned := strings.TrimSpace(toolName)
	if cleaned == "" {
		return toolIdentity{}
	}
	if mapped, ok := e.llmNameIndex[cleaned]; ok && strings.TrimSpace(mapped) != "" {
		return toolIdentity{
			LogicalName:  llm.CanonicalToolName(cleaned),
			ResolvedName: strings.TrimSpace(mapped),
		}
	}
	if e.registry != nil {
		if spec, ok := e.registry.Get(cleaned); ok {
			return toolIdentity{
				LogicalName:  toolLogicalName(spec),
				ResolvedName: cleaned,
			}
		}
	}
	canonical := llm.CanonicalToolName(cleaned)
	if canonical == "" {
		canonical = cleaned
	}
	return toolIdentity{
		LogicalName:  canonical,
		ResolvedName: cleaned,
	}
}

func toolLogicalName(spec AgentToolSpec) string {
	logical := strings.TrimSpace(spec.LlmName)
	if logical == "" {
		logical = strings.TrimSpace(spec.Name)
	}
	logical = llm.CanonicalToolName(logical)
	if logical == "" {
		return strings.TrimSpace(spec.Name)
	}
	return logical
}

func durationMs(started time.Time) int {
	elapsed := time.Since(started)
	millis := int(elapsed / time.Millisecond)
	if millis < 0 {
		return 0
	}
	return millis
}
