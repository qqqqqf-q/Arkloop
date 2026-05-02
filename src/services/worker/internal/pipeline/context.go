package pipeline

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"arkloop/services/shared/eventbus"
	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/rollout"
	"arkloop/services/shared/skillstore"
	sharedtoolruntime "arkloop/services/shared/toolruntime"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/subagentctl"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// ResolvedAgentConfig 保存继承链解析后的合并配置。
type ResolvedAgentConfig struct {
	SystemPrompt       *string
	Model              *string
	Temperature        *float64
	MaxOutputTokens    *int
	TopP               *float64
	ContextWindowLimit *int
	ToolPolicy         string // "allowlist" | "denylist" | "none"
	ToolAllowlist      []string
	ToolDenylist       []string
	ContentFilterLevel string
	SafetyRulesJSON    map[string]any
	PromptCacheControl string // "none" | "system_prompt"
	ReasoningMode      string // "auto" | "enabled" | "disabled" | "none"
}

// ReadCapabilities 保存当前 run 的统一 read 能力事实来源。
type ReadCapabilities struct {
	NativeImageInput        bool // 当前主模型是否原生支持 image 输入
	ImageBridgeEnabled      bool // 当前 run 是否具备图片桥接读取能力
	ReadImageSourcesVisible bool // 当前 run 的 read schema 是否暴露了图片 source
}

type RunStatusUpdater interface {
	LockRunRow(ctx context.Context, tx pgx.Tx, runID uuid.UUID) error
	UpdateRunTerminalStatus(ctx context.Context, tx pgx.Tx, runID uuid.UUID, u data.TerminalStatusUpdate) error
}

// RunContext 承载单次 Execute 调用的全部运行时状态，在 Pipeline 各中间件间共享。
type RunContext struct {
	// -- 初始化时写入 --
	Run         data.Run
	DB          data.DB
	RunStatusDB RunStatusUpdater
	Pool        *pgxpool.Pool
	// MemoryServiceDB 供 memory run_events / usage 与快照刷新；桌面为 SQLite，服务端可与 Pool 相同。
	MemoryServiceDB data.MemoryMiddlewareDB
	// MemorySnapshotStore 由 Execute 注入，与 NewMemoryMiddleware 共用同一快照语义。
	MemorySnapshotStore MemorySnapshotStore
	DirectPool          *pgxpool.Pool     // LISTEN/NOTIFY 专用直连，不走 PgBouncer；由 Execute 保证非 nil
	BroadcastRDB        *redis.Client     // 跨实例 SSE 广播，nil 时仅走 pg_notify
	EventBus            eventbus.EventBus // 进程内 SSE 通知（Desktop 模式替代 pg_notify + Redis）
	TraceID             string
	Tracer              Tracer
	Emitter             events.Emitter
	Router              *routing.ProviderRouter
	Runtime             *sharedtoolruntime.RuntimeSnapshot
	HookRuntime         *HookRuntime
	HookRegistry        *HookRegistry

	// -- EngineV1.Execute 从 Run.CreatedByUserID 注入；nil 时 MemoryMiddleware 跳过写入 --
	// agent_id 约定：默认取 PersonaDefinition.ID，字符集 [a-zA-Z0-9_-]，adapter 层 sanitize
	UserID *uuid.UUID
	// 长期环境绑定，由 EngineV1.Execute 在 run 启动时解析并注入。
	ProfileRef     string
	WorkspaceRef   string
	WorkDir        string // 用户选定的工作目录（Claw 模式），空字符串时由后端 fallback
	EnabledSkills  []skillstore.ResolvedSkill
	ExternalSkills []skillstore.ExternalSkill

	// -- AgentLoopHandler 写入：run 完成后的 assistant 最终拼接文本，供 MemoryMiddleware 写入 --
	FinalAssistantOutput string
	// -- AgentLoopHandler 写入：按 turn 保留的 assistant 输出，供 Channel 逐条投递 --
	FinalAssistantOutputs []string
	// -- Sticker delivery 预处理后写入：按原始输出顺序保留 text/sticker 片段 --
	ChannelDeliverySegments []data.OutboxSegment
	// -- AgentLoopHandler / desktop agent loop 写入：Arkloop 自身线程写入完成，可执行 thread persist hooks --
	ThreadPersistReady bool
	// -- Channel tool / desktop writer 写入：正文已由具副作用的渠道工具直接送达，middleware 不应再次外发 --
	ChannelOutputDelivered bool
	// -- telegram_reply 工具写入：为 delivery 层设置显式 reply 引用 --
	ChannelReplyOverride *ChannelMessageRef

	// -- AgentLoopHandler 写入：本次 run 的 tool call 总数和 LLM 迭代轮数，供 MemoryMiddleware 判断提炼条件 --
	RunToolCallCount  int
	RunIterationCount int

	// -- CancelGuardMiddleware 写入 --
	CancelFunc context.CancelFunc // 释放 LISTEN 连接
	ListenDone <-chan struct{}    // LISTEN goroutine 完成信号

	// -- EngineV1.Execute 注入 --
	JobPayload map[string]any

	// -- InputLoaderMiddleware 写入 --
	InputJSON map[string]any
	Messages  []llm.Message
	// ThreadMessageIDs 与 Messages 对齐；synthetic snapshot/replay 条目使用 uuid.Nil 占位。
	ThreadMessageIDs         []uuid.UUID
	ThreadContextFrontier    []FrontierNode
	PendingSubAgentCallbacks []data.ThreadSubAgentCallbackRecord

	// -- EngineV1.Execute 注入：context compact（[middleware 内可能改写 Messages]） --
	ContextCompact ContextCompactSettings
	// 当前 run 已知的最近一次真实 request prompt 锚点，供 compact 压力判断复用。
	HasContextCompactAnchor          bool
	LastRealPromptTokens             int
	LastRequestContextEstimateTokens int

	// -- PersonaResolutionMiddleware 写入 --
	AgentConfig     *ResolvedAgentConfig
	AgentConfigID   *uuid.UUID
	AgentConfigName string

	// -- PersonaResolutionMiddleware 写入 --
	PromptAssembly               PromptAssembly
	SystemPrompt                 string
	RuntimePrompt                string
	InheritedPromptCacheSnapshot *subagentctl.PromptCacheSnapshot
	PersonaDefinition            *personas.Definition
	MaxOutputTokens              *int
	Temperature                  *float64
	TopP                         *float64
	ToolTimeoutMs                *int
	ToolBudget                   map[string]any
	PerToolSoftLimits            tools.PerToolSoftLimits
	MaxCostMicros                *int64
	MaxTotalOutputTokens         *int64
	PreferredCredentialName      string // Persona.PreferredCredential 解析结果，供 RoutingMiddleware 使用
	ReasoningMode                string // "auto" | "enabled" | "disabled" | "none"
	StreamThinking               bool   // persona.stream_thinking，默认 true
	ToolChoice                   *llm.ToolChoice

	// -- 初始化时写入 base 值，MCPDiscovery/ToolBuild 覆盖 --
	ToolSpecs     []llm.ToolSpec
	ToolExecutors map[string]tools.Executor
	AllowlistSet  map[string]struct{}
	ToolDenylist  []string
	ToolRegistry  *tools.Registry
	// group_name -> provider_name
	ActiveToolProviderByGroup map[string]string
	// group_name -> active provider config
	ActiveToolProviderConfigsByGroup map[string]sharedtoolruntime.ProviderConfig

	// -- RoutingMiddleware 写入 --
	Gateway       llm.Gateway
	SelectedRoute *routing.SelectedProviderRoute
	// ContextWindowTokens 保存当前主路由解析出的 context window；0 表示未提供，由后续走 fallback。
	ContextWindowTokens int
	// RoutingByokEnabled 与 RoutingMiddleware 中 feature.byok_enabled 一致，供后续按 selector 解析路由使用。
	RoutingByokEnabled bool
	// ResolveGatewayForRouteID 按 route_id 构建目标 Gateway，用于同一 run 内切换输出模型。
	// route_id 为空时应回退当前主路由；返回 error 时由上层决定是否降级。
	ResolveGatewayForRouteID func(ctx context.Context, routeID string) (llm.Gateway, *routing.SelectedProviderRoute, error)
	// ResolveGatewayForAgentName 按 Agent 配置名称构建目标 Gateway，用于 Lua 中直接按 agent 名称切换输出模型。
	ResolveGatewayForAgentName func(ctx context.Context, agentName string) (llm.Gateway, *routing.SelectedProviderRoute, error)
	// EstimateProviderRequestBytes 按当前主路由的最终 provider payload 形态估算 request bytes。
	EstimateProviderRequestBytes func(req llm.Request) (int, error)
	// -- ToolBuildMiddleware 写入：统一 read 能力事实（提示注入/占位引导仅依赖此对象） --
	ReadCapabilities ReadCapabilities

	// -- ToolBuildMiddleware 写入 --
	ToolExecutor *tools.DispatchingExecutor
	FinalSpecs   []llm.ToolSpec

	// -- ToolLoopDetectionMiddleware 写入 --
	ToolLoopDetector *ToolLoopDetector

	// -- ChannelContextMiddleware 写入 --
	ChannelContext *ChannelContext
	// ChannelToolSurface 与 ChannelContext 同步，供工具 ExecutionContext 使用（避免 tools 依赖 pipeline.ChannelContext）
	ChannelToolSurface *tools.ChannelToolSurface
	// SenderIsAdmin 由 RuntimeContextMiddleware 写入：发送人是否持有 admin 角色
	SenderIsAdmin bool
	// TelegramToolBoundaryFlush 由 Channel 投递中间件注入：每个 tool.call 前发送尚未投递的 assistant 正文（nil 表示终态一次性投递）。
	TelegramToolBoundaryFlush func(ctx context.Context, text string) error
	// TelegramProgressTracker 由 Channel 投递中间件注入：按 segment 发送/编辑 Telegram 进度消息（nil 表示不启用）。
	TelegramProgressTracker *TelegramProgressTracker
	// TelegramStreamDeliveryRemainder 由 AgentLoopHandler 写入：分段投递模式下终态只发此尾段（已 TrimSpace）。
	TelegramStreamDeliveryRemainder string
	// ChannelTerminalNotice 由 AgentLoopHandler 在非 completed 终局时写入，供 Channel 在无任何助手正文时仍向用户说明原因。
	ChannelTerminalNotice string

	// -- EngineV1.Execute 注入：平台限制 --
	ThreadMessageHistoryLimit     int
	AgentReasoningIterationsLimit int
	ToolContinuationBudgetLimit   int
	MaxParallelTasks              int
	RunWallClockTimeout           time.Duration
	PausedInputTimeout            time.Duration
	IdleHeartbeatInterval         time.Duration
	CreditPerUSD                  int
	LlmMaxResponseBytes           int

	// -- 默认来自平台限制，PersonaResolution 可缩小 --
	ReasoningIterations    int
	ToolContinuationBudget int

	// -- EngineV1.Execute 注入 --
	ExecutorBuilder AgentExecutorBuilder

	// -- MemoryProvider，由 EngineV1.Execute 注入；nil 时 Lua binding 返回空结果 --
	MemoryProvider memory.MemoryProvider
	// -- 当前 run 内显式 memory_write 的待刷写缓冲区 --
	PendingMemoryWrites *memory.PendingWriteBuffer
	// -- 当前 run 内新增的人类输入，供 memory distill 构造增量归档内容 --
	baseUserMessages    []memory.MemoryMessage
	runtimeUserMessages []memory.MemoryMessage

	// -- LLM 重试，由 EngineV1.Execute 注入 --
	LlmRetryMaxAttempts int
	LlmRetryBaseDelayMs int

	// -- Human-in-the-loop 钩子，均为 nil 时 Executor 不触发 --
	// WaitForInput 非 nil 时，Executor 在 CheckInAt 边界调用此函数阻塞等待用户输入。
	// 返回 ("", false) 表示超时或不注入；返回 (text, true) 则将 text 作为 user message 注入。
	WaitForInput func(ctx context.Context) (string, bool)
	// CheckInAt 判断当前迭代 iter 是否为 check-in 边界，仅当 WaitForInput 非 nil 时有效。
	CheckInAt func(iter int) bool
	// PollSteeringInput 非阻塞轮询用户 steering 消息，由 CancelGuardMiddleware 注入。
	// 返回 (text, true) 表示有新消息；返回 ("", false) 表示无新消息。nil 时不触发。
	PollSteeringInput func(ctx context.Context) (string, bool)

	// -- Sub-agent 控制面（由 EngineV1.Execute 注入，nil 时表示未启用）--
	SubAgentControl subagentctl.Control

	// -- PersonaResolutionMiddleware 写入，Summarizer middlewares 读取 --
	SummarizerDefinition *personas.Definition
	TitleSummarizer      *personas.TitleSummarizerConfig
	ResultSummarizer     *personas.ResultSummarizerConfig

	// -- InjectionScanMiddleware 写入 --
	// InjectionScanUserTexts 由 Telegram 群组合并 burst 时写入：仅最后一条物理 user 的扫描文本。
	// 注入拦截/语义判定只应对「本轮触发输入」生效，避免合并后的历史消息里残留的测试 payload 误杀后续 run。
	InjectionScanUserTexts []string
	// UserPromptScanFunc 对运行中新增的人类输入执行同样的 prompt injection 检测。
	// phase 例如 "ask_user" / "interactive_checkin"。
	UserPromptScanFunc func(ctx context.Context, text string, phase string) error
	// ToolOutputScanFunc 扫描 tool output，检测间接注入。
	// 返回 (sanitized, true) 表示检测到注入；返回 ("", false) 表示安全。
	ToolOutputScanFunc func(toolName, text string) (string, bool)
	// -- EngineV1.Execute 注入：并发槽释放（idempotent，多次调用安全）--
	// 通过 sync.Once 保证底层 runlimit.Release 只执行一次。
	// nil 时表示未启用（测试上下文或 sub-agent）。
	ReleaseSlot func()

	// -- Heartbeat --
	HeartbeatRun         bool
	ScheduledJobRun      bool
	HeartbeatSilent      bool // 由 heartbeat_decision 工具执行时设置，AgentLoop 只读
	HeartbeatToolOutcome *HeartbeatDecisionOutcome

	// -- end_reply --
	EndReplyRequested bool // set by end_reply tool, agent loop reads to terminate run

	// -- plan mode --
	CollaborationMode         string
	CollaborationModeRevision int64
	IsPlanMode                bool
	LearningModeEnabled       bool
	PlanFilePath              string
	PlanModeExitReminder      bool

	// -- Impression --
	ImpressionRun bool
	// -- Sticker register --
	StickerRegisterRun bool

	// -- Rollout --
	// RolloutRecorder 用于写入 rollout 日志，为 nil 时不记录
	RolloutRecorder *rollout.Recorder
	// ResumePromptSnapshot 是 continue 从 parent rollout 恢复的 prompt/materialized context。
	ResumePromptSnapshot *rollout.PromptSnapshot
	// ResponseDraftStore 用于保存未完成正文草稿
	ResponseDraftStore objectstore.BlobStore

	// -- Prompt cache 调试开关（账号级，默认 false） --
	PromptCacheDebugEnabled bool
	// LastInheritedReuseResult 由 inherited prompt cache 复用判定写入，供 debug 事件读取。
	// nil 表示无 inherited 上下文（非 subagent 路径或 mode=incremental）。
	LastInheritedReuseResult *InheritedReuseResult
}

// InheritedReuseResult 描述子代理 prompt cache 快照复用判定结果。
type InheritedReuseResult struct {
	Reused        bool
	FailureReason string
}

// HeartbeatDecisionOutcome 保存 heartbeat_decision 工具的调用结果。
type HeartbeatDecisionOutcome struct {
	Reply bool
}

// SetHeartbeatDecisionOutcome implements tools/builtin/heartbeat_decision.PipelineBinding.
func (rc *RunContext) SetHeartbeatDecisionOutcome(reply bool, _ []string) {
	if rc == nil {
		return
	}
	rc.HeartbeatToolOutcome = &HeartbeatDecisionOutcome{
		Reply: reply,
	}
	rc.HeartbeatSilent = !reply
}

// SetEndReplyRequested implements tools/builtin/end_reply.PipelineBinding.
func (rc *RunContext) SetEndReplyRequested(requested bool) {
	if rc == nil {
		return
	}
	rc.EndReplyRequested = requested
}

// SetIsPlanMode implements tools/builtin/enter_plan_mode.PipelineBinding.
func (rc *RunContext) SetIsPlanMode(active bool) {
	if rc == nil {
		return
	}
	if !active && rc.IsPlanMode {
		rc.PlanModeExitReminder = true
	}
	rc.IsPlanMode = active
	if active {
		rc.CollaborationMode = CollaborationModePlan
		if rc.PlanFilePath == "" {
			rc.PlanFilePath = "plans/" + rc.Run.ThreadID.String() + ".md"
		}
	} else {
		rc.CollaborationMode = CollaborationModeDefault
	}
	SyncPlanModePrompt(rc)
}

// SetPlanFilePath implements tools/builtin/enter_plan_mode.PipelineBinding.
func (rc *RunContext) SetPlanFilePath(path string) {
	if rc == nil {
		return
	}
	rc.PlanFilePath = path
	SyncPlanModePrompt(rc)
}

// PlanFilePathValue implements tools/builtin/exit_plan_mode.PipelineBinding.
func (rc *RunContext) PlanFilePathValue() string {
	if rc == nil {
		return ""
	}
	return rc.PlanFilePath
}

// IsPlanModeActive is consumed by write tools to enforce plan-mode read-only constraint.
func (rc *RunContext) IsPlanModeActive() bool {
	return rc != nil && rc.IsPlanMode
}

// PlanModeWritePathAllowed allows plan-mode maintenance only for the current thread plan file.
func (rc *RunContext) PlanModeWritePathAllowed(path string) bool {
	if rc == nil || rc.Run.ThreadID == uuid.Nil {
		return false
	}
	raw := strings.TrimSpace(path)
	if raw == "" || filepath.IsAbs(raw) {
		return false
	}
	cleaned := filepath.ToSlash(filepath.Clean(raw))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return false
	}
	target := strings.TrimSpace(rc.PlanFilePath)
	if target == "" {
		target = "plans/" + rc.Run.ThreadID.String() + ".md"
	}
	target = filepath.ToSlash(filepath.Clean(target))
	return cleaned == target
}

// IsHeartbeatRun implements tools/builtin/heartbeat_decision.PipelineBinding.
func (rc *RunContext) IsHeartbeatRun() bool {
	return rc != nil && rc.HeartbeatRun
}

// AppendRuntimeUserMessage records a real user input added during the current run.
func (rc *RunContext) AppendRuntimeUserMessage(text string) {
	if rc == nil {
		return
	}
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return
	}
	rc.runtimeUserMessages = append(rc.runtimeUserMessages, memory.MemoryMessage{
		Role:    "user",
		Content: cleaned,
	})
}

func (rc *RunContext) SetBaseUserMessages(messages []memory.MemoryMessage) {
	if rc == nil {
		return
	}
	rc.baseUserMessages = append([]memory.MemoryMessage(nil), messages...)
}

func (rc *RunContext) BaseUserMessages() []memory.MemoryMessage {
	if rc == nil || len(rc.baseUserMessages) == 0 {
		return nil
	}
	return append([]memory.MemoryMessage(nil), rc.baseUserMessages...)
}

func (rc *RunContext) RuntimeUserMessages() []memory.MemoryMessage {
	if rc == nil || len(rc.runtimeUserMessages) == 0 {
		return nil
	}
	return append([]memory.MemoryMessage(nil), rc.runtimeUserMessages...)
}

func (rc *RunContext) ResetPromptAssembly() {
	if rc == nil {
		return
	}
	rc.PromptAssembly.Reset()
	rc.syncLegacyPromptViews()
}

func (rc *RunContext) ReplacePromptAssembly(assembly PromptAssembly) {
	if rc == nil {
		return
	}
	rc.PromptAssembly = assembly.Clone()
	rc.syncLegacyPromptViews()
}

func (rc *RunContext) PromptSegments() []PromptSegment {
	if rc == nil || len(rc.PromptAssembly.Segments) == 0 {
		return nil
	}
	out := make([]PromptSegment, len(rc.PromptAssembly.Segments))
	copy(out, rc.PromptAssembly.Segments)
	return out
}

func (rc *RunContext) UpsertPromptSegment(segment PromptSegment) {
	if rc == nil {
		return
	}
	rc.PromptAssembly.Upsert(segment)
	rc.syncLegacyPromptViews()
}

func (rc *RunContext) AppendPromptSegment(segment PromptSegment) {
	if rc == nil {
		return
	}
	rc.PromptAssembly.Append(segment)
	rc.syncLegacyPromptViews()
}

func (rc *RunContext) RemovePromptSegment(name string) {
	if rc == nil {
		return
	}
	rc.PromptAssembly.Remove(name)
	rc.syncLegacyPromptViews()
}

func (rc *RunContext) RemovePromptSegmentsByPrefix(prefix string) {
	if rc == nil {
		return
	}
	rc.PromptAssembly.RemoveByPrefix(prefix)
	rc.syncLegacyPromptViews()
}

func (rc *RunContext) MaterializedSystemPrompt() string {
	if rc == nil {
		return ""
	}
	return rc.PromptAssembly.MaterializeSystemPrompt()
}

func (rc *RunContext) MaterializedRuntimePrompt() string {
	if rc == nil {
		return ""
	}
	return rc.PromptAssembly.MaterializeRuntimePrompt()
}

func (rc *RunContext) ApplyResumePromptSnapshot() {
	if rc == nil || rc.ResumePromptSnapshot == nil {
		return
	}
	segments := make([]PromptSegment, 0, len(rc.ResumePromptSnapshot.Segments))
	for _, segment := range rc.ResumePromptSnapshot.Segments {
		segments = append(segments, PromptSegment{
			Name:          segment.Name,
			Target:        PromptSegmentTarget(segment.Target),
			Role:          segment.Role,
			Text:          segment.Text,
			Stability:     PromptSegmentStability(segment.Stability),
			CacheEligible: segment.CacheEligible,
		})
	}
	rc.ReplacePromptAssembly(PromptAssembly{Segments: segments})
}

func (rc *RunContext) syncLegacyPromptViews() {
	if rc == nil {
		return
	}
	rc.SystemPrompt = rc.PromptAssembly.MaterializeSystemPrompt()
	rc.RuntimePrompt = rc.PromptAssembly.MaterializeRuntimePrompt()
}

// SetContextCompactPressureAnchor 记录当前 run 内最近一次真实 request 的 compact 压力锚点。
func (rc *RunContext) SetContextCompactPressureAnchor(lastRealPromptTokens, lastRequestContextEstimateTokens int) {
	if rc == nil || lastRealPromptTokens <= 0 || lastRequestContextEstimateTokens <= 0 {
		return
	}
	rc.HasContextCompactAnchor = true
	rc.LastRealPromptTokens = lastRealPromptTokens
	rc.LastRequestContextEstimateTokens = lastRequestContextEstimateTokens
}

// ReadToolMessages exposes a stable read-only message snapshot for tools that
// need access to already loaded conversation parts (e.g. image attachments).
func (rc *RunContext) ReadToolMessages() []llm.Message {
	if rc == nil || len(rc.Messages) == 0 {
		return nil
	}
	out := make([]llm.Message, len(rc.Messages))
	copy(out, rc.Messages)
	return out
}
