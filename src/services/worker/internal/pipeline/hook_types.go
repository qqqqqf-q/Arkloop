package pipeline

import (
	"context"
	"strings"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

type HookName string

const (
	HookBeforePromptAssemble HookName = "before_prompt_assemble"
	HookAfterPromptAssemble  HookName = "after_prompt_assemble"
	HookBeforeModelCall      HookName = "before_model_call"
	HookAfterModelResponse   HookName = "after_model_response"
	HookAfterToolCall        HookName = "after_tool_call"
	HookBeforeCompact        HookName = "before_compact"
	HookAfterCompact         HookName = "after_compact"
	HookBeforeThreadPersist  HookName = "before_thread_persist"
	HookAfterThreadPersist   HookName = "after_thread_persist"
)

var allHookNames = []HookName{
	HookBeforePromptAssemble,
	HookAfterPromptAssemble,
	HookBeforeModelCall,
	HookAfterModelResponse,
	HookAfterToolCall,
	HookBeforeCompact,
	HookAfterCompact,
	HookBeforeThreadPersist,
	HookAfterThreadPersist,
}

type HookProvider interface {
	HookProviderName() string
}

type PromptFragment struct {
	Key      string
	XMLTag   string
	Content  string
	Source   string
	Priority int
}

type PromptFragments []PromptFragment

type ModelCallHint struct {
	Key      string
	Value    string
	Source   string
	Priority int
}

type ModelCallHints []ModelCallHint

type PostResponseAction struct {
	Key      string
	Value    string
	Source   string
	Priority int
}

type PostResponseActions []PostResponseAction

type PostToolAction struct {
	Key      string
	Value    string
	Source   string
	Priority int
}

type PostToolActions []PostToolAction

type CompactHint struct {
	Content  string
	Source   string
	Priority int
}

type CompactHints []CompactHint

type PostCompactAction struct {
	Key      string
	Value    string
	Source   string
	Priority int
}

type PostCompactActions []PostCompactAction

type ThreadPersistHint struct {
	Key      string
	Value    string
	Source   string
	Priority int
}

type ThreadPersistHints []ThreadPersistHint

type PersistObserver struct {
	Key      string
	Value    string
	Source   string
	Priority int
}

type PersistObservers []PersistObserver

type ModelResponse struct {
	AssistantText string
	ToolCalls     []llm.ToolCall
	ToolResults   []llm.StreamToolResult
	Completed     map[string]any
	Terminal      bool
	Cancelled     bool
}

type CompactInput struct {
	SystemPrompt string
	Messages     []llm.Message
}

type CompactOutput struct {
	SystemPrompt string
	Messages     []llm.Message
	Summary      string
	Changed      bool
}

type ThreadDeltaMessage struct {
	Role    string
	Content string
}

type ThreadDelta struct {
	RunID           uuid.UUID
	ThreadID        uuid.UUID
	AccountID       uuid.UUID
	UserID          uuid.UUID
	AgentID         string
	Messages        []ThreadDeltaMessage
	AssistantOutput string
	ToolCallCount   int
	IterationCount  int
	TraceID         string
}

type ThreadPersistResult struct {
	Handled          bool
	Provider         string
	ExternalThreadID string
	AppendedMessages int
	Committed        bool
	Err              error
}

type BeforePromptAssembleHook interface {
	HookProvider
	BeforePromptAssemble(ctx context.Context, rc *RunContext) (PromptFragments, error)
}

type AfterPromptAssembleHook interface {
	HookProvider
	AfterPromptAssemble(ctx context.Context, rc *RunContext, assembledPrompt string) (PromptFragments, error)
}

type BeforeModelCallHook interface {
	HookProvider
	BeforeModelCall(ctx context.Context, rc *RunContext, request llm.Request) (ModelCallHints, error)
}

type AfterModelResponseHook interface {
	HookProvider
	AfterModelResponse(ctx context.Context, rc *RunContext, response ModelResponse) (PostResponseActions, error)
}

type AfterToolCallHook interface {
	HookProvider
	AfterToolCall(ctx context.Context, rc *RunContext, toolCall llm.ToolCall, toolResult tools.ExecutionResult) (PostToolActions, error)
}

type BeforeCompactHook interface {
	HookProvider
	BeforeCompact(ctx context.Context, rc *RunContext, input CompactInput) (CompactHints, error)
}

type AfterCompactHook interface {
	HookProvider
	AfterCompact(ctx context.Context, rc *RunContext, output CompactOutput) (PostCompactActions, error)
}

type BeforeThreadPersistHook interface {
	HookProvider
	BeforeThreadPersist(ctx context.Context, rc *RunContext, delta ThreadDelta) (ThreadPersistHints, error)
}

type AfterThreadPersistHook interface {
	HookProvider
	AfterThreadPersist(ctx context.Context, rc *RunContext, delta ThreadDelta, result ThreadPersistResult) (PersistObservers, error)
}

type ContextContributor interface {
	BeforePromptAssembleHook
	AfterPromptAssembleHook
}

type CompactionAdvisor interface {
	BeforeCompactHook
	AfterCompactHook
}

type ThreadPersistenceProvider interface {
	HookProvider
	PersistThread(ctx context.Context, rc *RunContext, delta ThreadDelta, hints ThreadPersistHints) ThreadPersistResult
}

type ModelLifecycleHook interface {
	BeforeModelCallHook
	AfterModelResponseHook
	AfterToolCallHook
}

func providerName(provider HookProvider) string {
	if provider == nil {
		return "unknown"
	}
	name := strings.TrimSpace(provider.HookProviderName())
	if name == "" {
		return "unknown"
	}
	return name
}
