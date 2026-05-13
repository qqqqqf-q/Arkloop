package pipeline

import (
	"context"
	"time"

	"arkloop/services/shared/pluginhook"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

const (
	PluginHookSessionStart       = "session_start"
	PluginHookSessionEnd         = "session_end"
	PluginHookBeforeModelCall    = "before_model_call"
	PluginHookAfterModelResponse = "after_model_response"
	PluginHookBeforeToolUse      = "before_tool_use"
	PluginHookAfterToolUse       = "after_tool_use"
)

type PluginHookConfig struct {
	PluginID     string
	HookID       string
	Event        string
	Runtime      string
	LaunchSpec   map[string]any
	HookConfig   pluginhook.HookConfig
	Settings     map[string]any
	RuntimeState map[string]any
	Timeout      time.Duration
}

type PluginHookInvocation struct {
	AccountID     uuid.UUID
	RunID         uuid.UUID
	ThreadID      uuid.UUID
	ProfileRef    string
	WorkspaceRef  string
	Hook          PluginHookConfig
	Event         string
	ToolCall      llm.ToolCall
	ToolResult    tools.ExecutionResult
	ModelRequest  llm.Request
	ModelResponse ModelResponse
	SessionError  string
}

type PluginHookResult struct {
	Decision       string
	Message        string
	ErrorClass     string
	ModifiedArgs   map[string]any
	InjectSegments []PromptSegment
}

type PluginHookRunner interface {
	RunPluginHook(ctx context.Context, invocation PluginHookInvocation) (PluginHookResult, error)
}

type PluginContextLoader func(ctx context.Context, rc *RunContext) ([]PromptSegment, error)

type PluginHooksLoader func(ctx context.Context, rc *RunContext) ([]PluginHookConfig, error)
