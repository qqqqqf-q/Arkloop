package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/rollout"
	"arkloop/services/shared/runresume"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/imageutil"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/stablejson"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type MessageAttachmentStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	GetWithContentType(ctx context.Context, key string) ([]byte, string, error)
}

type MessagePartBuildOptions struct {
	LazyImages bool
}

func resolveMessagePartBuildOptions(opts ...MessagePartBuildOptions) MessagePartBuildOptions {
	if len(opts) == 0 {
		return MessagePartBuildOptions{}
	}
	return opts[0]
}

type runFirstEventLoader interface {
	FirstEventData(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (string, map[string]any, error)
}

type runRecoveryEventLoader interface {
	runFirstEventLoader
	GetLatestEventType(ctx context.Context, tx pgx.Tx, runID uuid.UUID, types []string) (string, error)
}

type runRecordLoader interface {
	GetRun(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (*data.Run, error)
}

type loadedRunInputs struct {
	InputJSON             map[string]any
	Messages              []llm.Message
	ThreadMessageIDs      []uuid.UUID
	ThreadContextFrontier []FrontierNode
	ResumePromptSnapshot  *rollout.PromptSnapshot
}

type LoadedRunInputs = loadedRunInputs

type InputLoaderTraceFunc func(stage string, durationMs int64, fields map[string]any)

type resumeReplayInsertion struct {
	AnchorKey      string
	Messages       []llm.Message
	RunID          uuid.UUID
	PromptSnapshot *rollout.PromptSnapshot
}

func traceInputLoaderStage(trace InputLoaderTraceFunc, stage string, start time.Time, fields map[string]any) {
	if trace == nil {
		return
	}
	trace(stage, time.Since(start).Milliseconds(), fields)
}

type resumeUnavailableError struct {
	reason string
}

func (e *resumeUnavailableError) Error() string {
	if e == nil || strings.TrimSpace(e.reason) == "" {
		return "resume context is unavailable"
	}
	return e.reason
}

const (
	resumeUnavailableErrorClass       = "resume.unavailable"
	replaySyntheticToolErrorClass     = "tool.interrupted"
	runStartedThreadTailMessageIDKey  = "thread_tail_message_id"
	runStartedContinuationSourceKey   = "continuation_source"
	runStartedContinuationLoopKey     = "continuation_loop"
	runStartedContinuationResponseKey = "continuation_response"
)

const ResumeUnavailableErrorClass = resumeUnavailableErrorClass

const (
	CollaborationModeDefault = "default"
	CollaborationModePlan    = "plan"
)

// NewInputLoaderMiddleware 加载 run 的 inputJSON 和线程历史消息到 RunContext。
func NewInputLoaderMiddleware(
	runsRepo data.RunsRepository,
	eventsRepo data.RunEventsRepository,
	messagesRepo data.MessagesRepository,
	attachmentStore MessageAttachmentStore,
	rolloutStore objectstore.BlobStore,
) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		traceStage := func(stage string, durationMs int64, fields map[string]any) {
			if rc == nil || rc.Tracer == nil {
				return
			}
			payload := map[string]any{
				"stage":       strings.TrimSpace(stage),
				"duration_ms": durationMs,
			}
			for key, value := range fields {
				payload[key] = value
			}
			rc.Tracer.Event("input_loader", "input_loader.stage_completed", payload)
		}
		loaded, err := loadRunInputsWithTrace(ctx, rc.Pool, rc.Run, rc.JobPayload, runsRepo, eventsRepo, messagesRepo, attachmentStore, rolloutStore, rc.ThreadMessageHistoryLimit, traceStage)
		if err != nil {
			var resumeErr *resumeUnavailableError
			if errors.As(err, &resumeErr) {
				errorClass := resumeUnavailableErrorClass
				eventType := "run.failed"
				message := "resume context is unavailable"
				if IsRuntimeRecoveryJob(rc.JobPayload) {
					errorClass = "worker.recovery_unavailable"
					eventType = "run.interrupted"
					message = "runtime recovery state is unavailable"
				}
				terminal := rc.Emitter.Emit(eventType, map[string]any{
					"error_class": errorClass,
					"message":     message,
				}, nil, StringPtr(errorClass))
				return appendAndCommitSingle(ctx, rc.Pool, rc.Run, runsRepo, eventsRepo, terminal, rc.ReleaseSlot, rc.BroadcastRDB, rc.EventBus)
			}
			return err
		}

		rc.InputJSON = loaded.InputJSON
		rc.Messages, rc.ThreadMessageIDs = sanitizeToolPairs(loaded.Messages, loaded.ThreadMessageIDs)
		rc.ThreadContextFrontier = append([]FrontierNode(nil), loaded.ThreadContextFrontier...)
		rc.ResumePromptSnapshot = loaded.ResumePromptSnapshot
		if rawWorkDir, ok := loaded.InputJSON["work_dir"].(string); ok {
			rc.WorkDir = strings.TrimSpace(rawWorkDir)
		}
		ApplyCollaborationMode(rc)
		ApplyLearningMode(rc)
		emitTraceEvent(rc, "input_loader", "input_loader.loaded", map[string]any{
			"run_kind":           strings.TrimSpace(stringValue(rc.InputJSON["run_kind"])),
			"message_count":      len(rc.Messages),
			"history_limit":      rc.ThreadMessageHistoryLimit,
			"collaboration_mode": rc.CollaborationMode,
		})

		return next(ctx, rc)
	}
}

func stringValue(value any) string {
	if raw, ok := value.(string); ok {
		return raw
	}
	return ""
}

func shouldLazyLoadChannelImages(inputJSON map[string]any, jobPayload map[string]any) bool {
	for _, payload := range []map[string]any{jobPayload, inputJSON} {
		delivery, ok := payload["channel_delivery"].(map[string]any)
		if !ok || len(delivery) == 0 {
			continue
		}
		conversationType, _ := delivery["conversation_type"].(string)
		if IsTelegramGroupLikeConversation(conversationType) {
			return true
		}
	}
	return false
}

func NormalizeCollaborationMode(value string) (string, bool) {
	switch strings.TrimSpace(value) {
	case "", CollaborationModeDefault:
		return CollaborationModeDefault, true
	case CollaborationModePlan:
		return CollaborationModePlan, true
	default:
		return "", false
	}
}

// ApplyCollaborationMode restores collaboration state from the run snapshot.
func ApplyCollaborationMode(rc *RunContext) {
	if rc == nil {
		return
	}
	mode := CollaborationModeDefault
	if rawMode, ok := rc.InputJSON["collaboration_mode"].(string); ok {
		if normalized, valid := NormalizeCollaborationMode(rawMode); valid {
			mode = normalized
		}
	}
	rc.CollaborationMode = mode
	rc.IsPlanMode = mode == CollaborationModePlan
	rc.PlanModeExitReminder = false
	if rawRevision, ok := rc.InputJSON["collaboration_mode_revision"]; ok {
		rc.CollaborationModeRevision = int64Value(rawRevision)
	}
	if rc.IsPlanMode && rc.PlanFilePath == "" {
		rc.PlanFilePath = planFilePath(rc.Run.ThreadID)
	}
	SyncPlanModePrompt(rc)
}

// ApplyPlanMode is kept as a narrow compatibility shim for older tests.
func ApplyPlanMode(rc *RunContext) {
	ApplyCollaborationMode(rc)
}

func ApplyLearningMode(rc *RunContext) {
	if rc == nil {
		return
	}
	rc.LearningModeEnabled = boolValue(rc.InputJSON["learning_mode_enabled"])
	SyncLearningModePrompt(rc)
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	default:
		return 0
	}
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "on":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func planFilePath(threadID uuid.UUID) string {
	if threadID == uuid.Nil {
		return ""
	}
	return "plans/" + threadID.String() + ".md"
}

func SyncPlanModePrompt(rc *RunContext) {
	if rc == nil {
		return
	}
	rc.RemovePromptSegment("plan_mode")
	rc.RemovePromptSegment("plan_mode_exit")
	if rc.IsPlanMode {
		if rc.PlanFilePath == "" {
			rc.PlanFilePath = planFilePath(rc.Run.ThreadID)
		}
		rc.UpsertPromptSegment(PromptSegment{
			Name:      "plan_mode",
			Target:    PromptTargetRuntimeTail,
			Role:      "user",
			Stability: PromptStabilityVolatileTail,
			Text: "<system-reminder>\n" +
				"Plan Mode is active for this thread.\n\n" +
				"The user does not want implementation yet. Do not modify ordinary project files, create commits, run mutating commands, or claim implementation is complete.\n\n" +
				"You may read, search, inspect, ask clarifying questions, and maintain only this plan file:\n" +
				rc.PlanFilePath + "\n\n" +
				"Keep the plan file current as you learn. If the plan file does not exist, create it. If it exists, read it first and update or replace it as appropriate for the current request.\n\n" +
				"When the plan is ready, call exit_plan_mode. Do not ask for plan approval in plain text; exit_plan_mode is the approval handoff.\n" +
				"</system-reminder>",
		})
		return
	}
	if rc.PlanModeExitReminder {
		if rc.PlanFilePath == "" {
			rc.PlanFilePath = planFilePath(rc.Run.ThreadID)
		}
		rc.UpsertPromptSegment(PromptSegment{
			Name:      "plan_mode_exit",
			Target:    PromptTargetRuntimeTail,
			Role:      "user",
			Stability: PromptStabilityVolatileTail,
			Text: "<system-reminder>\n" +
				"Plan Mode has ended for this thread.\n\n" +
				"Previous Plan Mode instructions are no longer active. You may now implement changes, run appropriate tools, and continue from the approved plan if it is relevant.\n\n" +
				"The plan file for reference is:\n" +
				rc.PlanFilePath + "\n" +
				"</system-reminder>",
		})
	}
}

func SyncLearningModePrompt(rc *RunContext) {
	if rc == nil {
		return
	}
	rc.RemovePromptSegment("learning_tutor")
	if !rc.LearningModeEnabled {
		return
	}
	rc.UpsertPromptSegment(PromptSegment{
		Name:          "learning_tutor",
		Target:        PromptTargetRuntimeTail,
		Role:          "user",
		Stability:     PromptStabilityVolatileTail,
		CacheEligible: false,
		Text: `<system-reminder>
学习辅导已在当前 thread 启用。这是后端注入的学习辅导语义层，不替换当前 persona，不改变当前模式和工具可用性。你仍然是当前模式下的 Arkloop；当用户在学习、题解、数学、科学、工程、编程基础或概念理解场景中提问时，采用导师式回答。

<learning_tutor_policy>
默认工作语言跟随用户当前输入语言或已知偏好；用户使用中文时使用中文。用户主动指定语言时，以用户指定为准。

对每条学习相关消息先判断意图再行动：
- Directive：用户明确要求“解题、推导、计算、验证、写出答案、给完整过程”时，可以给出完整解答，但必须展示关键思路和必要验证。
- Inquiry：用户问“为什么、怎么理解、原理是什么、哪里不懂”时，先解释概念和思路，不直接倾倒完整答案。
默认按 Inquiry 处理，除非用户明确要求完整解答。

教学遵循 U-S-T：
1. Understand：先理解题目、用户困惑点、已知条件和目标。
2. Solve：内部先独立求解并检查正确性，不把未经验证的推理直接抛给用户。
3. Teach：从直觉解释开始，逐步过渡到形式化表述；必要时给小例子、反例、图示或步骤拆解。

回答方式：
- 像一位严谨、耐心的老师，关注用户是否真正理解，而不是只把答案交出去。
- 不要一次性倾倒所有知识；在自然教学节点停下，但不要机械地以“要继续吗？”结尾。
- 避免“这很简单”之类会压低用户体验的表述。
- 用户表现出焦虑或挫败时，可以适度鼓励，但不要空泛安慰。

数学与表达：
- 数学表达式使用 LaTeX。行内公式使用 \( ... \)，块级公式使用 \[ ... \]。
- 即使只是单个变量或短表达式，也用 LaTeX 包裹，例如 \( x \) 的值为 \( 3 \)。
- 同一行中如果包含 LaTeX，避免混用 Markdown 粗体或斜体造成渲染冲突。

可视化与工具：
- 当概念关系、流程、分类、算法步骤适合图示时，可以使用 Mermaid；节点 ID 使用 ASCII，中文放在节点标签中。
- 只有工具在当前 turn 真实可用时才调用，不伪造工具能力；外部事实、最新数据或引用材料仍按当前 persona 的工具与引用规则处理。

边界：
- 学习辅导只改变学习场景下的解释和教学策略，不创建新的 persona，不覆盖 Normal/Work mode 的基础行为。
- 非学习任务仍按当前 persona 和当前模式规则处理。
</learning_tutor_policy>
</system-reminder>`,
	})
}

func loadRunInputs(
	ctx context.Context,
	pool interface {
		BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
	},
	run data.Run,
	jobPayload map[string]any,
	runsRepo runRecordLoader,
	eventsRepo runRecoveryEventLoader,
	messagesRepo data.MessagesRepository,
	attachmentStore MessageAttachmentStore,
	rolloutStore objectstore.BlobStore,
	messageLimit int,
) (*loadedRunInputs, error) {
	return loadRunInputsWithTrace(ctx, pool, run, jobPayload, runsRepo, eventsRepo, messagesRepo, attachmentStore, rolloutStore, messageLimit, nil)
}

func loadRunInputsWithTrace(
	ctx context.Context,
	pool interface {
		BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
	},
	run data.Run,
	jobPayload map[string]any,
	runsRepo runRecordLoader,
	eventsRepo runRecoveryEventLoader,
	messagesRepo data.MessagesRepository,
	attachmentStore MessageAttachmentStore,
	rolloutStore objectstore.BlobStore,
	messageLimit int,
	trace InputLoaderTraceFunc,
) (*loadedRunInputs, error) {
	stageStart := time.Now()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	traceInputLoaderStage(trace, "begin_tx", stageStart, nil)

	stageStart = time.Now()
	_, dataJSON, err := eventsRepo.FirstEventData(ctx, tx, run.ID)
	if err != nil {
		return nil, err
	}
	traceInputLoaderStage(trace, "first_event", stageStart, nil)

	inputJSON := map[string]any{
		"account_id": run.AccountID.String(),
		"thread_id":  run.ThreadID.String(),
	}
	if dataJSON != nil {
		if rawRouteID, ok := dataJSON["route_id"].(string); ok && strings.TrimSpace(rawRouteID) != "" {
			inputJSON["route_id"] = strings.TrimSpace(rawRouteID)
		}
		if rawPersonaID, ok := dataJSON["persona_id"].(string); ok && strings.TrimSpace(rawPersonaID) != "" {
			inputJSON["persona_id"] = strings.TrimSpace(rawPersonaID)
		}
		if rawRole, ok := dataJSON["role"].(string); ok && strings.TrimSpace(rawRole) != "" {
			inputJSON["role"] = strings.TrimSpace(rawRole)
		}
		if rawOutputRouteID, ok := dataJSON["output_route_id"].(string); ok && strings.TrimSpace(rawOutputRouteID) != "" {
			inputJSON["output_route_id"] = strings.TrimSpace(rawOutputRouteID)
		}
		if rawOutputModelKey, ok := dataJSON["output_model_key"].(string); ok && strings.TrimSpace(rawOutputModelKey) != "" {
			inputJSON["output_model_key"] = strings.TrimSpace(rawOutputModelKey)
		}
		if rawModel, ok := dataJSON["model"].(string); ok && strings.TrimSpace(rawModel) != "" {
			inputJSON["model"] = strings.TrimSpace(rawModel)
		}
		if rawWorkDir, ok := dataJSON["work_dir"].(string); ok && strings.TrimSpace(rawWorkDir) != "" {
			inputJSON["work_dir"] = strings.TrimSpace(rawWorkDir)
		}
		if rawReasoningMode, ok := dataJSON["reasoning_mode"].(string); ok && strings.TrimSpace(rawReasoningMode) != "" {
			inputJSON["reasoning_mode"] = strings.TrimSpace(rawReasoningMode)
		}
		if rawCollaborationMode, ok := dataJSON["collaboration_mode"].(string); ok && strings.TrimSpace(rawCollaborationMode) != "" {
			inputJSON["collaboration_mode"] = strings.TrimSpace(rawCollaborationMode)
		}
		if rawRevision, ok := dataJSON["collaboration_mode_revision"]; ok {
			inputJSON["collaboration_mode_revision"] = rawRevision
		}
		if rawLearningMode, ok := dataJSON["learning_mode_enabled"]; ok {
			inputJSON["learning_mode_enabled"] = rawLearningMode
		}
		if rawTimeoutSeconds, ok := dataJSON["timeout_seconds"]; ok {
			switch value := rawTimeoutSeconds.(type) {
			case int:
				if value > 0 {
					inputJSON["timeout_seconds"] = value
				}
			case float64:
				if int(value) > 0 {
					inputJSON["timeout_seconds"] = int(value)
				}
			}
		}
		if rawContinuationSource, ok := dataJSON[runStartedContinuationSourceKey].(string); ok && strings.TrimSpace(rawContinuationSource) != "" {
			inputJSON[runStartedContinuationSourceKey] = strings.TrimSpace(rawContinuationSource)
		}
		if rawContinuationLoop, ok := dataJSON[runStartedContinuationLoopKey].(bool); ok {
			inputJSON[runStartedContinuationLoopKey] = rawContinuationLoop
		}
		if rawContinuationResponse, ok := dataJSON[runStartedContinuationResponseKey].(bool); ok {
			inputJSON[runStartedContinuationResponseKey] = rawContinuationResponse
		}
		if rawRunKind, ok := dataJSON["run_kind"].(string); ok && strings.TrimSpace(rawRunKind) != "" {
			inputJSON["run_kind"] = strings.TrimSpace(rawRunKind)
		}
		if rawStickerID, ok := dataJSON["sticker_id"].(string); ok && strings.TrimSpace(rawStickerID) != "" {
			inputJSON["sticker_id"] = strings.TrimSpace(rawStickerID)
		}
		if rawThreadTailID, ok := dataJSON[runStartedThreadTailMessageIDKey].(string); ok && strings.TrimSpace(rawThreadTailID) != "" {
			inputJSON[runStartedThreadTailMessageIDKey] = strings.TrimSpace(rawThreadTailID)
		}
		if rawChannelDelivery, ok := dataJSON["channel_delivery"].(map[string]any); ok && len(rawChannelDelivery) > 0 {
			inputJSON["channel_delivery"] = rawChannelDelivery
		}
	}
	_ = isHeartbeatRun(inputJSON, jobPayload)

	stageStart = time.Now()
	historyUpperBoundID, hasHistoryUpperBound, err := boundedThreadHistoryUpperBound(ctx, tx, inputJSON, jobPayload)
	if err != nil {
		return nil, err
	}
	if hasHistoryUpperBound {
		inputJSON[runStartedThreadTailMessageIDKey] = historyUpperBoundID.String()
	}
	traceInputLoaderStage(trace, "history_bound", stageStart, map[string]any{
		"has_upper_bound": hasHistoryUpperBound,
	})

	var upperBoundMessageID *uuid.UUID
	if hasHistoryUpperBound {
		upperBoundMessageID = &historyUpperBoundID
	}
	stageStart = time.Now()
	canonicalContext, err := buildCanonicalThreadContextWithTrace(
		ctx,
		tx,
		run,
		messagesRepo,
		attachmentStore,
		upperBoundMessageID,
		messageLimit,
		trace,
		MessagePartBuildOptions{LazyImages: shouldLazyLoadChannelImages(inputJSON, jobPayload)},
	)
	if err != nil {
		return nil, err
	}
	messages := canonicalContext.VisibleMessages
	traceInputLoaderStage(trace, "canonical_context", stageStart, map[string]any{
		"visible_messages":  len(canonicalContext.VisibleMessages),
		"atoms":             len(canonicalContext.Atoms),
		"chunks":            len(canonicalContext.Chunks),
		"frontier":          len(canonicalContext.Frontier),
		"entries":           len(canonicalContext.Entries),
		"rendered_messages": len(canonicalContext.Messages),
	})
	replayInsertions := []resumeReplayInsertion(nil)
	if IsRuntimeRecoveryJob(jobPayload) {
		stageStart = time.Now()
		replayInsertions, err = loadRuntimeRecoveryReplay(ctx, tx, run, eventsRepo, rolloutStore, canonicalContext, messages)
		if err != nil {
			return nil, err
		}
		traceInputLoaderStage(trace, "runtime_recovery_replay", stageStart, map[string]any{
			"insertions": len(replayInsertions),
		})
	} else if run.ResumeFromRunID != nil {
		stageStart = time.Now()
		replayInsertions, err = loadResumedReplay(ctx, tx, run, runsRepo, eventsRepo, rolloutStore, canonicalContext, messages, isExplicitContinueJob(jobPayload))
		if err != nil {
			var resumeErr *resumeUnavailableError
			if errors.As(err, &resumeErr) {
				if isExplicitContinueJob(jobPayload) {
					return nil, err
				}
				clearContinuationMetadata(inputJSON)
				replayInsertions = nil
			} else {
				return nil, err
			}
		}
		traceInputLoaderStage(trace, "resume_replay", stageStart, map[string]any{
			"insertions": len(replayInsertions),
		})
	}

	stageStart = time.Now()
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	traceInputLoaderStage(trace, "commit", stageStart, nil)

	stageStart = time.Now()
	replayCount := 0
	replayByAnchor := make(map[string][]resumeReplayInsertion, len(replayInsertions))
	replayedRunIDs := make(map[uuid.UUID]struct{}, len(replayInsertions))
	for _, insertion := range replayInsertions {
		replayByAnchor[insertion.AnchorKey] = append(replayByAnchor[insertion.AnchorKey], insertion)
		replayCount += len(insertion.Messages)
		if insertion.RunID != uuid.Nil {
			replayedRunIDs[insertion.RunID] = struct{}{}
		}
	}

	llmMessages := make([]llm.Message, 0, len(canonicalContext.Messages)+replayCount)
	ids := make([]uuid.UUID, 0, len(canonicalContext.ThreadMessageIDs)+replayCount)
	messageRunIDs := canonicalAssistantRunIDs(messages)
	for _, entry := range canonicalContext.Entries {
		if entry.ThreadMessageID != uuid.Nil {
			if runID, ok := messageRunIDs[entry.ThreadMessageID]; ok {
				if _, replayed := replayedRunIDs[runID]; replayed {
					continue
				}
			}
		}
		llmMessages = append(llmMessages, entry.Message)
		ids = append(ids, entry.ThreadMessageID)
		for _, insertion := range replayByAnchor[entry.AnchorKey] {
			llmMessages = append(llmMessages, insertion.Messages...)
			for range insertion.Messages {
				ids = append(ids, uuid.Nil)
			}
		}
	}

	// 提取最后一条用户消息，供 Lua 脚本通过 context.get("last_user_message") 访问
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" && strings.TrimSpace(messages[i].Content) != "" {
			inputJSON["last_user_message"] = strings.TrimSpace(messages[i].Content)
			break
		}
	}
	llmMessages, ids = sanitizeToolPairs(llmMessages, ids)
	traceInputLoaderStage(trace, "assemble_output", stageStart, map[string]any{
		"message_count": len(llmMessages),
		"replay_count":  replayCount,
	})

	return &loadedRunInputs{
		InputJSON:             inputJSON,
		Messages:              llmMessages,
		ThreadMessageIDs:      ids,
		ThreadContextFrontier: canonicalContext.Frontier,
		ResumePromptSnapshot:  firstResumePromptSnapshot(replayInsertions),
	}, nil
}

func firstResumePromptSnapshot(insertions []resumeReplayInsertion) *rollout.PromptSnapshot {
	for _, insertion := range insertions {
		if insertion.PromptSnapshot != nil {
			copy := *insertion.PromptSnapshot
			return &copy
		}
	}
	return nil
}

func LoadRunInputs(
	ctx context.Context,
	pool interface {
		BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
	},
	run data.Run,
	jobPayload map[string]any,
	runsRepo runRecordLoader,
	eventsRepo runRecoveryEventLoader,
	messagesRepo data.MessagesRepository,
	attachmentStore MessageAttachmentStore,
	rolloutStore objectstore.BlobStore,
	messageLimit int,
) (*LoadedRunInputs, error) {
	return loadRunInputs(ctx, pool, run, jobPayload, runsRepo, eventsRepo, messagesRepo, attachmentStore, rolloutStore, messageLimit)
}

func LoadRunInputsWithTrace(
	ctx context.Context,
	pool interface {
		BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
	},
	run data.Run,
	jobPayload map[string]any,
	runsRepo runRecordLoader,
	eventsRepo runRecoveryEventLoader,
	messagesRepo data.MessagesRepository,
	attachmentStore MessageAttachmentStore,
	rolloutStore objectstore.BlobStore,
	messageLimit int,
	trace InputLoaderTraceFunc,
) (*LoadedRunInputs, error) {
	return loadRunInputsWithTrace(ctx, pool, run, jobPayload, runsRepo, eventsRepo, messagesRepo, attachmentStore, rolloutStore, messageLimit, trace)
}

func IsResumeUnavailableError(err error) bool {
	var resumeErr *resumeUnavailableError
	return errors.As(err, &resumeErr)
}

func clearContinuationMetadata(inputJSON map[string]any) {
	if inputJSON == nil {
		return
	}
	inputJSON[runStartedContinuationSourceKey] = "none"
	inputJSON[runStartedContinuationLoopKey] = false
	delete(inputJSON, runStartedContinuationResponseKey)
}

func isExplicitContinueJob(jobPayload map[string]any) bool {
	if len(jobPayload) == 0 {
		return false
	}
	raw, _ := jobPayload["source"].(string)
	return strings.TrimSpace(raw) == "continue"
}

func canonicalAssistantRunIDs(messages []data.ThreadMessage) map[uuid.UUID]uuid.UUID {
	out := make(map[uuid.UUID]uuid.UUID, len(messages))
	for _, msg := range messages {
		if msg.ID == uuid.Nil || msg.Role != "assistant" || len(msg.MetadataJSON) == 0 {
			continue
		}
		var metadata map[string]any
		if err := json.Unmarshal(msg.MetadataJSON, &metadata); err != nil {
			continue
		}
		rawRunID, _ := metadata["run_id"].(string)
		runID, err := uuid.Parse(strings.TrimSpace(rawRunID))
		if err != nil {
			continue
		}
		out[msg.ID] = runID
	}
	return out
}

func boundedThreadHistoryUpperBound(ctx context.Context, tx pgx.Tx, inputJSON map[string]any, jobPayload map[string]any) (uuid.UUID, bool, error) {
	if !isBoundedChannelHistoryRun(inputJSON, jobPayload) {
		return uuid.Nil, false, nil
	}
	if id, ok, err := threadHistoryUpperBoundFromValues(inputJSON, jobPayload); err != nil {
		return uuid.Nil, false, err
	} else if ok {
		return id, true, nil
	}
	if tx == nil {
		return uuid.Nil, false, nil
	}
	return lookupChannelHistoryUpperBoundFromLedger(ctx, tx, inputJSON, jobPayload)
}

func isBoundedChannelHistoryRun(inputJSON map[string]any, jobPayload map[string]any) bool {
	if IsRuntimeRecoveryJob(jobPayload) {
		return false
	}
	if continuationSource, _ := inputJSON[runStartedContinuationSourceKey].(string); strings.TrimSpace(continuationSource) != "" && strings.TrimSpace(continuationSource) != "none" {
		return false
	}
	if isHeartbeatRun(inputJSON, jobPayload) {
		return false
	}
	return hasChannelDeliveryPayload(inputJSON) || hasChannelDeliveryPayload(jobPayload)
}

func hasChannelDeliveryPayload(values map[string]any) bool {
	if len(values) == 0 {
		return false
	}
	raw, ok := values["channel_delivery"].(map[string]any)
	return ok && len(raw) > 0
}

func threadHistoryUpperBoundFromValues(values ...map[string]any) (uuid.UUID, bool, error) {
	for _, value := range values {
		raw, _ := value[runStartedThreadTailMessageIDKey].(string)
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			return uuid.Nil, false, fmt.Errorf("invalid thread history upper bound message id: %w", err)
		}
		return id, true, nil
	}
	return uuid.Nil, false, nil
}

func lookupChannelHistoryUpperBoundFromLedger(ctx context.Context, tx pgx.Tx, inputJSON map[string]any, jobPayload map[string]any) (uuid.UUID, bool, error) {
	channelDelivery := channelDeliveryPayload(inputJSON, jobPayload)
	if len(channelDelivery) == 0 {
		return uuid.Nil, false, nil
	}
	channelID, err := requiredUUIDValue(channelDelivery, "channel_id")
	if err != nil {
		return uuid.Nil, false, nil
	}
	conversationRef, err := parseConversationRef(channelDelivery)
	if err != nil || strings.TrimSpace(conversationRef.Target) == "" {
		return uuid.Nil, false, nil
	}
	candidates := []string{}
	if triggerRef, err := parseOptionalMessageRef(channelDelivery, "trigger_message_ref", "reply_to_message_id"); err == nil && triggerRef != nil && strings.TrimSpace(triggerRef.MessageID) != "" {
		candidates = append(candidates, strings.TrimSpace(triggerRef.MessageID))
	}
	if inboundRef, err := parseOptionalInboundMessageRef(channelDelivery); err == nil && strings.TrimSpace(inboundRef.MessageID) != "" {
		candidates = append(candidates, strings.TrimSpace(inboundRef.MessageID))
	}
	seen := map[string]struct{}{}
	for _, platformMessageID := range candidates {
		if _, ok := seen[platformMessageID]; ok {
			continue
		}
		seen[platformMessageID] = struct{}{}
		var messageID *uuid.UUID
		err := tx.QueryRow(
			ctx,
			`SELECT message_id
			   FROM channel_message_ledger
			  WHERE channel_id = $1
			    AND direction = 'inbound'
			    AND platform_conversation_id = $2
			    AND platform_message_id = $3
			    AND message_id IS NOT NULL
			  ORDER BY created_at DESC
			  LIMIT 1`,
			channelID,
			strings.TrimSpace(conversationRef.Target),
			platformMessageID,
		).Scan(&messageID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return uuid.Nil, false, fmt.Errorf("lookup bounded channel history upper bound: %w", err)
		}
		if messageID != nil && *messageID != uuid.Nil {
			return *messageID, true, nil
		}
	}
	return uuid.Nil, false, nil
}

func channelDeliveryPayload(values ...map[string]any) map[string]any {
	for _, value := range values {
		raw, ok := value["channel_delivery"].(map[string]any)
		if ok && len(raw) > 0 {
			return raw
		}
	}
	return nil
}

func loadResumedReplay(
	ctx context.Context,
	tx pgx.Tx,
	run data.Run,
	runsRepo runRecordLoader,
	eventsRepo runFirstEventLoader,
	rolloutStore objectstore.BlobStore,
	canonicalContext *canonicalThreadContext,
	threadMessages []data.ThreadMessage,
	requirePromptSnapshot bool,
) ([]resumeReplayInsertion, error) {
	if run.ResumeFromRunID == nil {
		return nil, nil
	}
	if runsRepo == nil {
		return nil, &resumeUnavailableError{reason: "resume run repository is unavailable"}
	}
	if eventsRepo == nil {
		return nil, &resumeUnavailableError{reason: "resume event repository is unavailable"}
	}

	insertions, err := collectResumeReplayInsertions(
		ctx,
		tx,
		run.AccountID,
		run.ThreadID,
		*run.ResumeFromRunID,
		runsRepo,
		eventsRepo,
		rolloutStore,
		canonicalContext,
		threadMessages,
		requirePromptSnapshot,
		map[uuid.UUID]struct{}{},
	)
	if err != nil {
		return nil, err
	}
	return insertions, nil
}

func IsRuntimeRecoveryJob(jobPayload map[string]any) bool {
	if len(jobPayload) == 0 {
		return false
	}
	if raw, _ := jobPayload["recovery_source"].(string); strings.TrimSpace(raw) == "runtime_recovery" {
		return true
	}
	if raw, _ := jobPayload["source"].(string); strings.TrimSpace(raw) == "desktop_recovery" {
		return true
	}
	return false
}

func resumeAnchorMessageID(dataJSON map[string]any) (uuid.UUID, error) {
	if dataJSON == nil {
		return uuid.Nil, &resumeUnavailableError{reason: "resume source run has no thread tail anchor"}
	}
	raw, _ := dataJSON[runStartedThreadTailMessageIDKey].(string)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return uuid.Nil, &resumeUnavailableError{reason: "resume source run has no thread tail anchor"}
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, &resumeUnavailableError{reason: "resume source run has invalid thread tail anchor"}
	}
	return id, nil
}

func trailingResumeUserBlockAfterMessage(messages []data.ThreadMessage, anchorMessageID uuid.UUID) (int, bool) {
	if len(messages) == 0 {
		return 0, false
	}
	anchorIndex := -1
	for i, msg := range messages {
		if msg.ID == anchorMessageID {
			anchorIndex = i
			break
		}
	}
	if anchorIndex < 0 || anchorIndex == len(messages)-1 {
		return 0, false
	}
	for i := anchorIndex + 1; i < len(messages); i++ {
		if messages[i].Role != "user" {
			return 0, false
		}
	}
	return anchorIndex + 1, true
}

func resumeInsertionAnchor(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	threadID uuid.UUID,
	canonicalContext *canonicalThreadContext,
	visibleMessages []data.ThreadMessage,
	anchorMessageID uuid.UUID,
	allowVisibleTail bool,
) (string, bool, error) {
	renderedAnchorKey := renderedMessageAnchorKey(canonicalContext.Entries, anchorMessageID)
	if allowVisibleTail {
		if renderedAnchorKey != "" && isLastRenderedMessage(canonicalContext.Entries, anchorMessageID) {
			return renderedAnchorKey, true, nil
		}
	}
	if _, ok := trailingResumeUserBlockAfterMessage(visibleMessages, anchorMessageID); ok {
		if renderedAnchorKey != "" {
			return renderedAnchorKey, true, nil
		}
	}
	if tx == nil || accountID == uuid.Nil || threadID == uuid.Nil || anchorMessageID == uuid.Nil {
		return "", false, nil
	}
	var threadSeq int64
	err := tx.QueryRow(
		ctx,
		`SELECT thread_seq
		   FROM messages
		  WHERE account_id = $1
		    AND thread_id = $2
		    AND id = $3
		    AND deleted_at IS NULL
		  LIMIT 1`,
		accountID,
		threadID,
		anchorMessageID,
	).Scan(&threadSeq)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	if replacementAnchor := replacementAnchorKeyForThreadSeq(canonicalContext.Entries, threadSeq); replacementAnchor != "" {
		return replacementAnchor, true, nil
	}
	if renderedAnchorKey != "" {
		return renderedAnchorKey, true, nil
	}
	return "", false, nil
}

func collectResumeReplayInsertions(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	threadID uuid.UUID,
	runID uuid.UUID,
	runsRepo runRecordLoader,
	eventsRepo runFirstEventLoader,
	rolloutStore objectstore.BlobStore,
	canonicalContext *canonicalThreadContext,
	threadMessages []data.ThreadMessage,
	requirePromptSnapshot bool,
	visited map[uuid.UUID]struct{},
) ([]resumeReplayInsertion, error) {
	if _, ok := visited[runID]; ok {
		return nil, &resumeUnavailableError{reason: "resume source run chain has a cycle"}
	}
	visited[runID] = struct{}{}

	parentRun, err := runsRepo.GetRun(ctx, tx, runID)
	if err != nil {
		return nil, err
	}
	if parentRun == nil {
		return nil, &resumeUnavailableError{reason: "resume source run does not exist"}
	}
	if parentRun.ThreadID != threadID {
		return nil, &resumeUnavailableError{reason: "resume source run does not belong to the thread"}
	}
	if parentRun.Status != "interrupted" && parentRun.Status != "cancelled" && parentRun.Status != "failed" {
		return nil, &resumeUnavailableError{reason: "resume source run is not resumable"}
	}

	insertions := []resumeReplayInsertion(nil)
	if parentRun.ResumeFromRunID != nil {
		insertions, err = collectResumeReplayInsertions(
			ctx,
			tx,
			accountID,
			threadID,
			*parentRun.ResumeFromRunID,
			runsRepo,
			eventsRepo,
			rolloutStore,
			canonicalContext,
			threadMessages,
			requirePromptSnapshot,
			visited,
		)
		if err != nil {
			return nil, err
		}
	}

	_, parentStartedData, err := eventsRepo.FirstEventData(ctx, tx, parentRun.ID)
	if err != nil {
		return nil, err
	}
	anchorMessageID, err := resumeAnchorMessageID(parentStartedData)
	if err != nil {
		return nil, err
	}
	insertionAnchorKey, ok, err := resumeInsertionAnchor(ctx, tx, accountID, threadID, canonicalContext, threadMessages, anchorMessageID, false)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &resumeUnavailableError{reason: "resume input block is missing"}
	}

	canonicalHasAssistant := canonicalThreadHasAssistantMessageForRun(canonicalContext, threadMessages, parentRun.ID)
	if rolloutStore == nil {
		return nil, &resumeUnavailableError{reason: "resume rollout store is unavailable"}
	}

	items, err := rollout.NewReader(rolloutStore).ReadRollout(ctx, parentRun.ID)
	if err != nil {
		return nil, &resumeUnavailableError{reason: "resume rollout is unavailable"}
	}
	state := rollout.NewReader(rolloutStore).Reconstruct(items)
	if requirePromptSnapshot && state.PromptSnapshot == nil {
		return nil, &resumeUnavailableError{reason: "resume prompt snapshot is unavailable"}
	}
	if len(state.PendingToolCalls) > 0 {
		return nil, &resumeUnavailableError{reason: "resume source run has unfinished tool calls"}
	}
	replayedMessages, err := buildReplayMessages(state)
	if err != nil {
		return nil, err
	}
	if !canonicalHasAssistant {
		if err := appendVisibleRecoveryDraft(ctx, tx, parentRun.ID, rolloutStore, &replayedMessages); err != nil {
			return nil, err
		}
	}
	if len(replayedMessages) == 0 {
		return nil, &resumeUnavailableError{reason: "resume rollout is unavailable"}
	}
	return append(insertions, resumeReplayInsertion{
		AnchorKey:      insertionAnchorKey,
		Messages:       replayedMessages,
		RunID:          parentRun.ID,
		PromptSnapshot: state.PromptSnapshot,
	}), nil
}

func loadRuntimeRecoveryReplay(
	ctx context.Context,
	tx pgx.Tx,
	run data.Run,
	eventsRepo runRecoveryEventLoader,
	rolloutStore objectstore.BlobStore,
	canonicalContext *canonicalThreadContext,
	threadMessages []data.ThreadMessage,
) ([]resumeReplayInsertion, error) {
	hasRecoverableOutput, err := runtimeRecoveryHasRecoverableOutput(ctx, tx, eventsRepo, run.ID)
	if err != nil {
		return nil, err
	}
	if !hasRecoverableOutput {
		return nil, nil
	}
	if rolloutStore == nil {
		return nil, &resumeUnavailableError{reason: "runtime recovery store is unavailable"}
	}
	_, startedData, err := eventsRepo.FirstEventData(ctx, tx, run.ID)
	if err != nil {
		return nil, err
	}
	anchorMessageID, err := resumeAnchorMessageID(startedData)
	if err != nil {
		return nil, err
	}
	insertionAnchorKey, ok, err := resumeInsertionAnchor(ctx, tx, run.AccountID, run.ThreadID, canonicalContext, threadMessages, anchorMessageID, true)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &resumeUnavailableError{reason: "runtime recovery input block is missing"}
	}
	items, err := rollout.NewReader(rolloutStore).ReadRollout(ctx, run.ID)
	if err != nil && !objectstore.IsNotFound(err) {
		return nil, &resumeUnavailableError{reason: "runtime recovery rollout is unavailable"}
	}
	state := rollout.NewReader(rolloutStore).Reconstruct(items)
	if len(state.PendingToolCalls) > 0 {
		return nil, &resumeUnavailableError{reason: "runtime recovery has unfinished tool calls"}
	}
	replayedMessages, err := buildReplayMessages(state)
	if err != nil {
		return nil, err
	}
	if !canonicalThreadHasAssistantMessageForRun(canonicalContext, threadMessages, run.ID) {
		if err := appendVisibleRecoveryDraft(ctx, tx, run.ID, rolloutStore, &replayedMessages); err != nil {
			return nil, err
		}
	}
	if len(replayedMessages) == 0 {
		return nil, &resumeUnavailableError{reason: "runtime recovery state is unavailable"}
	}
	return []resumeReplayInsertion{{
		AnchorKey: insertionAnchorKey,
		Messages:  replayedMessages,
	}}, nil
}

func runtimeRecoveryHasRecoverableOutput(
	ctx context.Context,
	tx pgx.Tx,
	eventsRepo runRecoveryEventLoader,
	runID uuid.UUID,
) (bool, error) {
	if eventsRepo == nil || runID == uuid.Nil {
		return false, nil
	}
	eventType, err := eventsRepo.GetLatestEventType(ctx, tx, runID, runresume.RecoverableEventTypeNames())
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(eventType) != "", nil
}

func canonicalThreadHasAssistantMessageForRun(
	canonicalContext *canonicalThreadContext,
	messages []data.ThreadMessage,
	runID uuid.UUID,
) bool {
	if canonicalContext == nil || runID == uuid.Nil {
		return false
	}
	rendered := make(map[uuid.UUID]struct{}, len(canonicalContext.ThreadMessageIDs))
	for _, messageID := range canonicalContext.ThreadMessageIDs {
		if messageID == uuid.Nil {
			continue
		}
		rendered[messageID] = struct{}{}
	}
	want := runID.String()
	for _, msg := range messages {
		if _, ok := rendered[msg.ID]; !ok {
			continue
		}
		if msg.Role != "assistant" || len(msg.MetadataJSON) == 0 {
			continue
		}
		var metadata map[string]any
		if err := json.Unmarshal(msg.MetadataJSON, &metadata); err != nil {
			continue
		}
		rawRunID, _ := metadata["run_id"].(string)
		if strings.TrimSpace(rawRunID) == want {
			return true
		}
	}
	return false
}

func buildReplayMessages(state *rollout.ReconstructedState) ([]llm.Message, error) {
	if state == nil {
		return nil, nil
	}
	replayed := make([]llm.Message, 0, len(state.ReplayMessages)+len(state.PendingToolCalls))
	for _, msg := range state.ReplayMessages {
		var rebuilt llm.Message
		var err error
		var keep bool
		switch msg.Role {
		case "assistant":
			if msg.Assistant == nil {
				continue
			}
			rebuilt, err = replayAssistantMessage(*msg.Assistant)
			if err != nil {
				return nil, err
			}
		case "tool":
			if msg.Tool == nil {
				continue
			}
			rebuilt = replayToolResultMessage(*msg.Tool)
		default:
			continue
		}
		rebuilt, keep = filterLongTermHeartbeatDecision(rebuilt)
		if keep {
			replayed = append(replayed, rebuilt)
		}
	}
	return replayed, nil
}

func replayAssistantMessage(msg rollout.AssistantMessage) (llm.Message, error) {
	raw := map[string]any{
		"role": "assistant",
	}
	if len(msg.ContentParts) > 0 {
		var content any
		if err := json.Unmarshal(msg.ContentParts, &content); err != nil {
			return llm.Message{}, err
		}
		raw["content"] = content
	} else if text := sanitizeStoredAssistantText(msg.Content); strings.TrimSpace(text) != "" {
		raw["content"] = []any{map[string]any{"type": messagecontent.PartTypeText, "text": text}}
	}
	if len(msg.ToolCalls) > 0 {
		var toolCalls []llm.ToolCall
		if err := json.Unmarshal(msg.ToolCalls, &toolCalls); err != nil {
			return llm.Message{}, err
		}
		encodedToolCalls := make([]any, 0, len(toolCalls))
		for _, call := range toolCalls {
			encodedCall := map[string]any{
				"tool_call_id": call.ToolCallID,
				"tool_name":    call.ToolName,
				"arguments":    call.ArgumentsJSON,
			}
			if displayDescription := strings.TrimSpace(call.DisplayDescription); displayDescription != "" {
				encodedCall["display_description"] = displayDescription
			}
			encodedToolCalls = append(encodedToolCalls, encodedCall)
		}
		raw["tool_calls"] = encodedToolCalls
	}
	parsed, err := llm.MessageFromJSONMap(raw)
	if err != nil {
		return llm.Message{}, err
	}
	parsed.ToolCalls = llm.CanonicalToolCalls(parsed.ToolCalls)
	return parsed, nil
}

func replayToolResultMessage(result rollout.ReplayToolResult) llm.Message {
	envelope := map[string]any{
		"tool_call_id": result.CallID,
	}
	if toolName := llm.CanonicalToolName(result.Name); toolName != "" {
		envelope["tool_name"] = toolName
	}
	if len(result.Output) > 0 {
		var output any
		if err := json.Unmarshal(result.Output, &output); err == nil {
			envelope["result"] = output
		}
	}
	if strings.TrimSpace(result.Error) != "" {
		errorClass := replaySyntheticToolErrorClass
		if !result.Synthetic {
			errorClass = "tool.error"
		}
		envelope["error"] = map[string]any{
			"error_class": errorClass,
			"message":     strings.TrimSpace(result.Error),
		}
	}
	text, err := stablejson.Encode(envelope)
	if err != nil {
		encoded, _ := json.Marshal(envelope)
		text = string(encoded)
	}
	return llm.Message{
		Role:    "tool",
		Content: []llm.TextPart{{Text: text, TrustSource: "tool"}},
	}
}

func parseToolCallsFromContentJSON(raw json.RawMessage) []llm.ToolCall {
	var parsed struct {
		ToolCalls []map[string]any `json:"tool_calls"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.ToolCalls) == 0 {
		return nil
	}
	result := make([]llm.ToolCall, 0, len(parsed.ToolCalls))
	for _, tc := range parsed.ToolCalls {
		toolCall, err := llm.ToolCallFromJSONMap(tc)
		if err != nil {
			continue
		}
		result = append(result, llm.CanonicalToolCall(toolCall))
	}
	return result
}

func filterLongTermHeartbeatDecision(msg llm.Message) (llm.Message, bool) {
	if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
		filtered := make([]llm.ToolCall, 0, len(msg.ToolCalls))
		for _, call := range msg.ToolCalls {
			if IsHeartbeatDecisionToolName(call.ToolName) {
				continue
			}
			filtered = append(filtered, call)
		}
		msg.ToolCalls = filtered
	}
	if msg.Role == "tool" && toolMessageIsHeartbeatDecision(msg) {
		return llm.Message{}, false
	}
	if msg.Role == "assistant" && len(msg.ToolCalls) == 0 && len(msg.Content) == 0 {
		return llm.Message{}, false
	}
	return msg, true
}

func toolMessageIsHeartbeatDecision(msg llm.Message) bool {
	if msg.Role != "tool" || len(msg.Content) == 0 {
		return false
	}
	var envelope struct {
		ToolName string `json:"tool_name"`
	}
	if json.Unmarshal([]byte(msg.Content[0].Text), &envelope) != nil {
		return false
	}
	return IsHeartbeatDecisionToolName(envelope.ToolName)
}

func canonicalizeToolMessageParts(parts []llm.ContentPart) []llm.ContentPart {
	if len(parts) == 0 {
		return nil
	}
	out := append([]llm.ContentPart(nil), parts...)
	for i := range out {
		if out[i].Kind() != messagecontent.PartTypeText {
			continue
		}
		out[i].Text = llm.CanonicalizeToolEnvelopeText(out[i].Text)
	}
	return out
}

func BuildMessageParts(ctx context.Context, store MessageAttachmentStore, msg data.ThreadMessage) ([]llm.ContentPart, error) {
	return BuildMessagePartsWithOptions(ctx, store, msg, MessagePartBuildOptions{})
}

func BuildMessagePartsWithOptions(ctx context.Context, store MessageAttachmentStore, msg data.ThreadMessage, opts MessagePartBuildOptions) ([]llm.ContentPart, error) {
	fallbackContent := msg.Content
	if msg.Role == "assistant" {
		if restored, err := llm.AssistantMessageFromThreadContentJSON(msg.ContentJSON); err == nil && restored != nil {
			return restored.Content, nil
		}
		fallbackContent = sanitizeStoredAssistantText(fallbackContent)
	}
	if len(msg.ContentJSON) == 0 {
		return fallbackTextParts(fallbackContent), nil
	}
	parsed, err := messagecontent.Parse(msg.ContentJSON)
	if err != nil {
		return fallbackTextParts(fallbackContent), nil
	}
	content, err := messagecontent.Normalize(parsed.Parts)
	if err != nil {
		return fallbackTextParts(fallbackContent), nil
	}
	parts := make([]llm.ContentPart, 0, len(content.Parts))
	for _, part := range content.Parts {
		switch part.Type {
		case messagecontent.PartTypeText:
			text := part.Text
			if msg.Role == "assistant" {
				text = sanitizeStoredAssistantText(text)
			}
			if strings.TrimSpace(text) == "" {
				continue
			}
			parts = append(parts, llm.ContentPart{Type: messagecontent.PartTypeText, Text: text})
		case messagecontent.PartTypeFile:
			parts = append(parts, llm.ContentPart{
				Type:          messagecontent.PartTypeFile,
				Attachment:    part.Attachment,
				ExtractedText: part.ExtractedText,
			})
		case messagecontent.PartTypeImage:
			if part.Attachment == nil {
				return nil, fmt.Errorf("message image attachment is required")
			}
			if opts.LazyImages {
				attachment := *part.Attachment
				parts = append(parts, llm.ContentPart{
					Type:       messagecontent.PartTypeImage,
					Attachment: &attachment,
				})
				continue
			}
			if store == nil {
				return nil, fmt.Errorf("message attachment store not configured")
			}
			dataBytes, contentType, err := store.GetWithContentType(ctx, part.Attachment.Key)
			if err != nil {
				if objectstore.IsNotFound(err) {
					return nil, fmt.Errorf("message attachment not found")
				}
				return nil, err
			}
			attachment := *part.Attachment
			if strings.TrimSpace(contentType) != "" {
				attachment.MimeType = strings.TrimSpace(contentType)
			}
			dataBytes, attachment.MimeType = imageutil.ProcessImage(dataBytes, attachment.MimeType)
			parts = append(parts, llm.ContentPart{
				Type:       messagecontent.PartTypeImage,
				Attachment: &attachment,
				Data:       dataBytes,
			})
		}
	}
	if len(parts) == 0 {
		return fallbackTextParts(fallbackContent), nil
	}
	return parts, nil
}

func fallbackTextParts(content string) []llm.ContentPart {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	return []llm.ContentPart{{Type: messagecontent.PartTypeText, Text: content}}
}

func sanitizeStoredAssistantText(text string) string {
	return strings.ReplaceAll(text, "<end_turn>", "")
}

func loadVisibleSeqCutoff(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (int64, bool, error) {
	var raw []byte
	err := tx.QueryRow(ctx,
		`SELECT data_json FROM run_events
		 WHERE run_id = $1 AND type = 'run.cancel_requested'
		 ORDER BY seq DESC LIMIT 1`,
		runID,
	).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, err
	}
	if len(raw) == 0 {
		return 0, true, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0, true, nil
	}
	switch value := payload["visible_seq_cutoff"].(type) {
	case float64:
		return int64(value), true, nil
	case json.Number:
		i, err := value.Int64()
		if err != nil {
			return 0, true, nil
		}
		return i, true, nil
	case int64:
		return value, true, nil
	case int:
		return int64(value), true, nil
	default:
		return 0, true, nil
	}
}

func loadVisibleAssistantOutput(ctx context.Context, tx pgx.Tx, runID uuid.UUID, cutoff int64) (string, error) {
	if cutoff <= 0 {
		return "", nil
	}
	query := `
		SELECT data_json FROM run_events
		WHERE run_id = $1 AND type = 'message.delta' AND seq <= $2
		ORDER BY seq ASC
	`
	rows, err := tx.Query(ctx, query, runID, cutoff)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var builder strings.Builder
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return "", err
		}
		if len(raw) == 0 {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			continue
		}
		if delta := extractVisibleAssistantDelta(payload); delta != "" {
			builder.WriteString(delta)
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return builder.String(), nil
}

func extractVisibleAssistantDelta(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	channel, _ := payload["channel"].(string)
	if strings.TrimSpace(channel) != "" {
		return ""
	}
	if role, _ := payload["role"].(string); role != "" && role != "assistant" {
		return ""
	}
	delta, _ := payload["content_delta"].(string)
	if strings.TrimSpace(delta) == "" || strings.TrimSpace(delta) == "<end_turn>" {
		return ""
	}
	return delta
}

func appendVisibleRecoveryDraft(
	ctx context.Context,
	tx pgx.Tx,
	runID uuid.UUID,
	rolloutStore objectstore.BlobStore,
	messages *[]llm.Message,
) error {
	cutoff, hasCutoff, err := loadVisibleSeqCutoff(ctx, tx, runID)
	if err != nil {
		return err
	}
	if hasCutoff {
		visibleContent, err := loadVisibleAssistantOutput(ctx, tx, runID, cutoff)
		if err != nil {
			return err
		}
		if visibleContent != "" {
			*messages = append(*messages, responseDraftMessage(visibleContent))
		}
		return nil
	}
	if rolloutStore == nil {
		return &resumeUnavailableError{reason: "response draft is unavailable"}
	}
	draft, err := readResponseDraft(ctx, rolloutStore, runID)
	if err != nil {
		return &resumeUnavailableError{reason: "response draft is unavailable"}
	}
	if draft != nil {
		*messages = append(*messages, responseDraftMessage(draft.Content))
	}
	return nil
}
