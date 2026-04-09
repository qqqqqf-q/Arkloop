package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"arkloop/services/shared/creditpolicy"
	sharedent "arkloop/services/shared/entitlement"
	"arkloop/services/shared/eventbus"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/queue"
	"arkloop/services/worker/internal/subagentctl"
	"arkloop/services/worker/internal/tools/builtin/acptool"
	"arkloop/services/worker/internal/tools/builtin/read"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	eventCommitBatchSize   = 20
	eventCommitMaxInterval = 50 * time.Millisecond
	// maxChildRunOutputBytes 限制通过 Redis Pub/Sub 传递的子 Run 输出大小，防止大消息导致延迟或丢失
	maxChildRunOutputBytes = 64 * 1024
)

var (
	cancelEvtTypes      = []string{"run.cancel_requested", "run.cancelled"}
	streamingEventTypes = map[string]struct{}{
		"message.delta":      {},
		"llm.response.chunk": {},
		"run.segment.start":  {},
		"run.segment.end":    {},
		"tool.call.delta":    {},
	}
	errStopProcessing = errors.New("stop_processing")
)

// NewAgentLoopHandler 构建 Pipeline 终端 Handler：执行 Agent Loop 并写入事件。
func NewAgentLoopHandler(
	runsRepo data.RunsRepository,
	eventsRepo data.RunEventsRepository,
	messagesRepo data.MessagesRepository,
	runLimiterRDB *redis.Client,
	jobQueue queue.JobQueue,
	usageRepo data.UsageRecordsRepository,
	creditsRepo data.CreditsRepository,
	resolver *sharedent.Resolver,
) RunHandler {
	return func(ctx context.Context, rc *RunContext) error {
		selected := rc.SelectedRoute

		policy := creditpolicy.DefaultPolicy
		if resolver != nil {
			if p, err := resolver.ResolveDeductionPolicy(ctx, rc.Run.AccountID); err == nil {
				policy = p
			}
		}

		creditsPerUSD := float64(rc.CreditPerUSD)
		if creditsPerUSD <= 0 {
			creditsPerUSD = 1000.0
		}

		personaID := ""
		if rc.PersonaDefinition != nil {
			personaID = rc.PersonaDefinition.ID
		}

		writer := newEventWriter(
			rc.Pool, rc.Run, rc.TraceID, runLimiterRDB,
			rc.EventBus, jobQueue,
			selected.Route.Model, personaID, usageRepo, creditsRepo,
			creditsPerUSD,
			selected.Route.Multiplier, selected.Route.CostPer1kInput, selected.Route.CostPer1kOutput,
			selected.Route.CostPer1kCacheWrite, selected.Route.CostPer1kCacheRead,
			policy,
			rc.ReleaseSlot,
			rc.TelegramToolBoundaryFlush,
			rc.TelegramProgressTracker,
			IsHeartbeatRunContext(rc),
		)
		defer writer.Close(ctx)
		defer func() {
			if writer.terminalRunStatus == "" {
				return
			}
			read.CleanupRunFromExecutors(rc.ToolExecutors, rc.Run.ID.String())
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := acptool.CleanupRunFromExecutors(cleanupCtx, rc.ToolExecutors, rc.Run.ID.String(), writer.terminalRunStatus); err != nil {
				slog.Warn("acp cleanup failed", "run_id", rc.Run.ID.String(), "error", err.Error())
			}
		}()

		routeData := selected.ToRunEventDataJSON()
		if rc.AgentConfig != nil && rc.AgentConfig.Model != nil && strings.TrimSpace(*rc.AgentConfig.Model) != "" {
			routeData["persona_model"] = strings.TrimSpace(*rc.AgentConfig.Model)
		}
		routeSelected := rc.Emitter.Emit("run.route.selected", routeData, nil, nil)
		if err := writer.Append(ctx, runsRepo, eventsRepo, rc.Run.ID, routeSelected); err != nil {
			if errors.Is(err, errStopProcessing) {
				return nil
			}
			return err
		}

		executorType := "agent.simple"
		var executorConfig map[string]any
		if rc.PersonaDefinition != nil {
			if rc.PersonaDefinition.ExecutorType != "" {
				executorType = rc.PersonaDefinition.ExecutorType
			}
			executorConfig = rc.PersonaDefinition.ExecutorConfig
		}

		exec, execBuildErr := rc.ExecutorBuilder.Build(executorType, executorConfig)
		if execBuildErr != nil {
			failed := rc.Emitter.Emit(
				"run.failed",
				map[string]any{
					"error_class": "internal.error",
					"message":     fmt.Sprintf("build executor %q: %s", executorType, execBuildErr.Error()),
				},
				nil,
				StringPtr("internal.error"),
			)
			if err := writer.Append(ctx, runsRepo, eventsRepo, rc.Run.ID, failed); err != nil {
				if errors.Is(err, errStopProcessing) {
					return nil
				}
				return err
			}
			rc.ChannelTerminalNotice = writer.TerminalUserMessage()
			return writer.Flush(ctx)
		}

		err := exec.Execute(ctx, rc, rc.Emitter, func(ev events.RunEvent) error {
			if appendErr := writer.Append(ctx, runsRepo, eventsRepo, rc.Run.ID, ev); appendErr != nil {
				if errors.Is(appendErr, errStopProcessing) {
					return errStopProcessing
				}
				return appendErr
			}
			return nil
		})
		if err != nil && !errors.Is(err, errStopProcessing) {
			return err
		}

		if !writer.Completed() {
			rc.ChannelTerminalNotice = writer.TerminalUserMessage()
		}
		if writer.Completed() {
			if !ShouldSuppressHeartbeatOutput(rc, writer.AssistantOutput()) {
				if writer.terminalRunStatus == "completed" && len(writer.intermediateMessages) > 0 {
					if err := writer.batchInsertIntermediateMessages(ctx, messagesRepo, rc.Run.AccountID, rc.Run.ThreadID, rc.Run.ID); err != nil {
						return err
					}
				}
				if writer.hasStreamedChunks() {
					remainder := writer.telegramStreamRemainder()
					if strings.TrimSpace(remainder) != "" {
						if err := writer.insertStreamRemainder(ctx, messagesRepo, rc.Run.AccountID, rc.Run.ThreadID, remainder); err != nil {
							return err
						}
					}
				} else {
					if _, err := writer.InsertAssistantMessage(ctx, messagesRepo, rc.Run.AccountID, rc.Run.ThreadID, false); err != nil {
						return err
					}
				}
				rc.FinalAssistantOutput = writer.AssistantOutput()
				rc.FinalAssistantOutputs = writer.AssistantOutputs()
				rc.TelegramStreamDeliveryRemainder = writer.telegramStreamRemainder()
				if writer.hasStreamedChunks() {
					rc.FinalAssistantOutputs = writer.telegramUnsentOutputs()
				}
			}
		}
		rc.RunToolCallCount = writer.toolCallCount
		rc.RunIterationCount = writer.iterationCount
		if writer.pendingReplyOverride != "" {
			rc.ChannelReplyOverride = &ChannelMessageRef{
				MessageID: writer.pendingReplyOverride,
			}
		}
		return writer.Flush(ctx)
	}
}

// eventWriter 批提交事件并在终态时更新 runs.status + DECR 并发计数 + 写入 usage_records。
type eventWriter struct {
	pool          *pgxpool.Pool
	run           data.Run
	traceID       string
	runLimiterRDB *redis.Client // SSE 广播（Publish）; slot release via releaseSlot closure
	eventBus      eventbus.EventBus
	jobQueue      queue.JobQueue
	projector     *subagentctl.SubAgentStateProjector
	model         string
	personaID     string
	runsRepo      data.RunsRepository
	usageRepo     data.UsageRecordsRepository
	creditsRepo   data.CreditsRepository
	releaseSlot   func() // idempotent per-run slot release (from RunContext)

	multiplier          float64
	costPer1kInput      *float64
	costPer1kOutput     *float64
	costPer1kCacheWrite *float64
	costPer1kCacheRead  *float64
	policy              creditpolicy.CreditDeductionPolicy
	creditsPerUSD       float64

	tx                       pgx.Tx
	pendingEventsSinceCommit int
	lastCommitAt             time.Time
	assistantDeltas          []string
	assistantMessage         *llm.Message
	assistantMessageFresh    bool
	assistantOutputs         []string
	lastTurnDeltaCount       int
	toolCallCount            int
	iterationCount           int
	completed                bool
	hasTerminal              bool

	totalInputTokens         int64
	totalOutputTokens        int64
	totalCacheCreationTokens int64
	totalCacheReadTokens     int64
	totalCachedTokens        int64
	totalCostUSD             float64

	telegramToolBoundaryFlush func(context.Context, string) error
	telegramSentOutputCount   int
	telegramProgressTracker   *TelegramProgressTracker
	pendingReplyOverride      string
	heartbeatRun              bool

	// 子 Run 完成通知：commit 时将终态状态发布到 run.child.{runID}.done
	terminalRunStatus    string
	terminalMessage      string
	pendingEnqueueRunIDs []uuid.UUID

	pendingToolCalls     []llm.ToolCall
	pendingToolResults   []intermediateMessage
	intermediateMessages []intermediateMessage
}

type intermediateMessage struct {
	Role        string
	Content     string
	ContentJSON json.RawMessage
	ToolCallID  string // tool result only
}

type pendingTelegramProgressToolCall struct {
	CallID   string
	ToolName string
	ArgsJSON string
}

func newEventWriter(
	pool *pgxpool.Pool,
	run data.Run,
	traceID string,
	runLimiterRDB *redis.Client,
	bus eventbus.EventBus,
	jobQueue queue.JobQueue,
	model string,
	personaID string,
	usageRepo data.UsageRecordsRepository,
	creditsRepo data.CreditsRepository,
	creditsPerUSD float64,
	multiplier float64,
	costPer1kInput *float64,
	costPer1kOutput *float64,
	costPer1kCacheWrite *float64,
	costPer1kCacheRead *float64,
	policy creditpolicy.CreditDeductionPolicy,
	releaseSlot func(),
	telegramToolBoundaryFlush func(context.Context, string) error,
	telegramProgressTracker *TelegramProgressTracker,
	heartbeatRun bool,
) *eventWriter {
	if creditsPerUSD <= 0 {
		creditsPerUSD = 1000.0
	}
	if multiplier <= 0 {
		multiplier = 1.0
	}
	return &eventWriter{
		pool:                      pool,
		run:                       run,
		traceID:                   strings.TrimSpace(traceID),
		lastCommitAt:              time.Now(),
		runLimiterRDB:             runLimiterRDB,
		eventBus:                  bus,
		jobQueue:                  jobQueue,
		projector:                 subagentctl.NewSubAgentStateProjector(pool, runLimiterRDB, jobQueue),
		model:                     model,
		personaID:                 strings.TrimSpace(personaID),
		usageRepo:                 usageRepo,
		creditsRepo:               creditsRepo,
		creditsPerUSD:             creditsPerUSD,
		multiplier:                multiplier,
		costPer1kInput:            costPer1kInput,
		costPer1kOutput:           costPer1kOutput,
		costPer1kCacheWrite:       costPer1kCacheWrite,
		costPer1kCacheRead:        costPer1kCacheRead,
		policy:                    policy,
		releaseSlot:               releaseSlot,
		telegramToolBoundaryFlush: telegramToolBoundaryFlush,
		telegramProgressTracker:   telegramProgressTracker,
		heartbeatRun:              heartbeatRun,
	}
}

func (w *eventWriter) telegramStreamRemainder() string {
	if w.telegramToolBoundaryFlush == nil {
		return ""
	}
	unsent := w.assistantOutputs[w.telegramSentOutputCount:]
	if len(unsent) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(unsent, "\n"))
}

func (w *eventWriter) hasStreamedChunks() bool {
	return w.telegramToolBoundaryFlush != nil && w.telegramSentOutputCount > 0
}

func (w *eventWriter) pendingTelegramFlushChunk() string {
	if w.telegramToolBoundaryFlush == nil {
		return ""
	}
	unsent := w.assistantOutputs[w.telegramSentOutputCount:]
	if len(unsent) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(unsent, "\n"))
}

func (w *eventWriter) telegramUnsentOutputs() []string {
	if w.telegramSentOutputCount >= len(w.assistantOutputs) {
		return nil
	}
	out := make([]string, 0, len(w.assistantOutputs)-w.telegramSentOutputCount)
	for _, item := range w.assistantOutputs[w.telegramSentOutputCount:] {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func (w *eventWriter) flushTelegramBoundaryAndProgress(
	ctx context.Context,
	flushChunk string,
	progressCall *pendingTelegramProgressToolCall,
) error {
	if flushChunk != "" && w.telegramToolBoundaryFlush != nil {
		if err := w.telegramToolBoundaryFlush(ctx, flushChunk); err != nil {
			return err
		}
		w.telegramSentOutputCount = len(w.assistantOutputs)
	}
	if progressCall != nil && w.telegramProgressTracker != nil {
		w.telegramProgressTracker.OnToolCall(ctx, progressCall.CallID, progressCall.ToolName, progressCall.ArgsJSON)
	}
	return nil
}

func (w *eventWriter) insertStreamRemainder(
	ctx context.Context,
	repo data.MessagesRepository,
	accountID uuid.UUID,
	threadID uuid.UUID,
	content string,
) error {
	if err := w.ensureTx(ctx); err != nil {
		return err
	}
	messageID, err := repo.InsertAssistantMessageWithMetadata(
		ctx, w.tx, accountID, threadID, w.run.ID,
		content, nil, false,
		map[string]any{"stream_chunk": true},
	)
	if err != nil {
		return err
	}
	if messageID != uuid.Nil {
		if err := (data.SubAgentRepository{}).SetLastOutputRefByLastCompletedRunID(ctx, w.tx, w.run.ID, "message:"+messageID.String()); err != nil {
			return err
		}
	}
	return nil
}

func (w *eventWriter) ensureTx(ctx context.Context) error {
	if w.tx != nil {
		return nil
	}
	tx, err := w.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	w.tx = tx
	w.lastCommitAt = time.Now()
	return nil
}

func (w *eventWriter) Append(
	ctx context.Context,
	runsRepo data.RunsRepository,
	eventsRepo data.RunEventsRepository,
	runID uuid.UUID,
	ev events.RunEvent,
) error {
	w.runsRepo = runsRepo
	if err := w.ensureTx(ctx); err != nil {
		return err
	}

	if err := runsRepo.LockRunRow(ctx, w.tx, runID); err != nil {
		return err
	}

	if ev.Type == "run.route.selected" {
		if err := runsRepo.UpdateRunMetadata(ctx, w.tx, runID, w.model, w.personaID); err != nil {
			return err
		}
	}

	cancelType, err := eventsRepo.GetLatestEventType(ctx, w.tx, runID, cancelEvtTypes)
	if err != nil {
		return err
	}
	if cancelType == "run.cancel_requested" {
		emitter := events.NewEmitter(w.traceID)
		cancelled := emitter.Emit("run.cancelled", map[string]any{}, nil, nil)
		if _, err := eventsRepo.AppendRunEvent(ctx, w.tx, runID, cancelled); err != nil {
			return err
		}
		if w.projector != nil {
			nextRunID, err := w.projector.ProjectRunTerminal(ctx, w.tx, w.run, data.SubAgentStatusCancelled, map[string]any{"run_id": runID.String()}, nil)
			if err != nil {
				return err
			}
			if nextRunID != nil {
				w.pendingEnqueueRunIDs = append(w.pendingEnqueueRunIDs, *nextRunID)
			}
		}
		// 如果配置了平台成本费率，覆盖 LLM 返回的原始 cost
		if platformCost := w.calcPlatformCost(); platformCost >= 0 {
			w.totalCostUSD = platformCost
		}
		if err := runsRepo.UpdateRunTerminalStatus(ctx, w.tx, runID, data.TerminalStatusUpdate{
			Status:            "cancelled",
			TotalInputTokens:  w.totalInputTokens,
			TotalOutputTokens: w.totalOutputTokens,
			TotalCostUSD:      w.totalCostUSD,
		}); err != nil {
			return err
		}
		if err := w.usageRepo.Insert(ctx, w.tx, w.run.AccountID, runID, w.model,
			w.totalInputTokens, w.totalOutputTokens,
			w.totalCacheCreationTokens, w.totalCacheReadTokens, w.totalCachedTokens,
			w.totalCostUSD); err != nil {
			return err
		}
		if r := w.calcCreditDeduction(); r.Credits > 0 {
			if err := w.creditsRepo.Deduct(ctx, w.tx, w.run.AccountID, r.Credits, runID, r.Metadata); err != nil {
				return err
			}
		}
		w.terminalRunStatus = "cancelled"
		w.hasTerminal = true
		if err := w.commit(ctx); err != nil {
			return err
		}
		return errStopProcessing
	}
	if cancelType == "run.cancelled" {
		if err := w.commit(ctx); err != nil {
			return err
		}
		return errStopProcessing
	}
	if _, err := eventsRepo.AppendRunEvent(ctx, w.tx, runID, ev); err != nil {
		return err
	}
	w.pendingEventsSinceCommit++
	if assistantMessage, ok := assistantMessageFromEventData(ev.DataJSON); ok {
		w.assistantMessage = &assistantMessage
		w.assistantMessageFresh = true
	}
	flushChunk := ""
	var pendingProgressCall *pendingTelegramProgressToolCall
	if ev.Type == "llm.turn.completed" {
		w.captureAssistantTurnOutput()
		flushChunk = w.pendingTelegramFlushChunk()
	}

	if shouldAccumulateUsageForEvent(ev.Type) {
		w.accumUsage(ev.DataJSON)
	}

	if ev.Type == "run.segment.start" && w.telegramProgressTracker != nil {
		segmentID, kind, mode, label := extractProgressSegmentStart(ev.DataJSON)
		w.telegramProgressTracker.OnRunSegmentStart(ctx, segmentID, kind, mode, label)
	}

	if ev.Type == "run.segment.end" && w.telegramProgressTracker != nil {
		segmentID, _ := ev.DataJSON["segment_id"].(string)
		w.telegramProgressTracker.OnRunSegmentEnd(ctx, segmentID)
	}

	if ev.Type == "tool.call" {
		flushChunk = w.pendingTelegramFlushChunk()
		w.captureReplyOverride(ev.DataJSON)
		w.toolCallCount++
		w.collectToolCall(ev.DataJSON)
		if w.telegramProgressTracker != nil {
			callID, _ := ev.DataJSON["tool_call_id"].(string)
			toolName, _ := ev.DataJSON["tool_name"].(string)
			toolName = llm.CanonicalToolName(toolName)
			argsRaw, _ := json.Marshal(ev.DataJSON["arguments"])
			pendingProgressCall = &pendingTelegramProgressToolCall{
				CallID:   callID,
				ToolName: toolName,
				ArgsJSON: string(argsRaw),
			}
		}
	}
	if ev.Type == "llm.request" {
		w.flushPendingToolCalls()
		w.iterationCount++
	}

	if ev.Type == "tool.result" {
		w.collectToolResult(ev.DataJSON)
		if w.telegramProgressTracker != nil {
			callID, _ := ev.DataJSON["tool_call_id"].(string)
			toolName, _ := ev.DataJSON["tool_name"].(string)
			toolName = llm.CanonicalToolName(toolName)
			errorClass := ""
			if ev.ErrorClass != nil {
				errorClass = *ev.ErrorClass
			}
			w.telegramProgressTracker.OnToolResult(ctx, callID, toolName, errorClass)
		}
	}

	if ev.Type == "message.delta" {
		if w.telegramProgressTracker != nil {
			role, _ := ev.DataJSON["role"].(string)
			channel, _ := ev.DataJSON["channel"].(string)
			if delta := extractAssistantDelta(ev.DataJSON); delta != "" {
				w.telegramProgressTracker.OnMessageDelta(ctx, role, channel, delta)
			}
		}
		// 只累积主内容，thinking channel 不计入最终消息文本
		if channel, _ := ev.DataJSON["channel"].(string); channel == "" {
			if delta := extractAssistantDelta(ev.DataJSON); delta != "" {
				w.assistantDeltas = append(w.assistantDeltas, delta)
			}
		}
	}

	if status, ok := TerminalStatuses[ev.Type]; ok {
		w.flushPendingToolCalls()
		if status == "completed" {
			w.completed = true
		}
		// 如果配置了平台成本费率，覆盖 LLM 返回的原始 cost
		if platformCost := w.calcPlatformCost(); platformCost >= 0 {
			w.totalCostUSD = platformCost
		}
		if err := runsRepo.UpdateRunTerminalStatus(ctx, w.tx, runID, data.TerminalStatusUpdate{
			Status:            status,
			TotalInputTokens:  w.totalInputTokens,
			TotalOutputTokens: w.totalOutputTokens,
			TotalCostUSD:      w.totalCostUSD,
		}); err != nil {
			return err
		}
		if w.projector != nil {
			nextRunID, err := w.projector.ProjectRunTerminal(ctx, w.tx, w.run, status, ev.DataJSON, ev.ErrorClass)
			if err != nil {
				return err
			}
			if nextRunID != nil {
				w.pendingEnqueueRunIDs = append(w.pendingEnqueueRunIDs, *nextRunID)
			}
		}
		if err := w.usageRepo.Insert(ctx, w.tx, w.run.AccountID, runID, w.model,
			w.totalInputTokens, w.totalOutputTokens,
			w.totalCacheCreationTokens, w.totalCacheReadTokens, w.totalCachedTokens,
			w.totalCostUSD); err != nil {
			return err
		}
		if r := w.calcCreditDeduction(); r.Credits > 0 {
			if err := w.creditsRepo.Deduct(ctx, w.tx, w.run.AccountID, r.Credits, runID, r.Metadata); err != nil {
				return err
			}
		}
		w.terminalRunStatus = status
		if status != "completed" {
			w.terminalMessage = TerminalStatusMessage(ev.DataJSON)
		}
		w.hasTerminal = true
		return nil
	}

	if _, ok := streamingEventTypes[ev.Type]; !ok {
		if err := w.commit(ctx); err != nil {
			return err
		}
		if err := w.flushTelegramBoundaryAndProgress(ctx, flushChunk, pendingProgressCall); err != nil {
			return err
		}
		return nil
	}

	now := time.Now()
	if w.pendingEventsSinceCommit >= eventCommitBatchSize || now.Sub(w.lastCommitAt) >= eventCommitMaxInterval {
		return w.commit(ctx)
	}
	return nil
}

func (w *eventWriter) commit(ctx context.Context) error {
	if w.tx == nil {
		return nil
	}
	if w.pendingEventsSinceCommit > 0 && !w.hasTerminal {
		if err := w.runsRepo.TouchRunActivity(ctx, w.tx, w.run.ID); err != nil {
			return err
		}
	}
	if err := w.tx.Commit(ctx); err != nil {
		return err
	}
	w.tx = nil
	w.pendingEventsSinceCommit = 0
	w.lastCommitAt = time.Now()

	channel := fmt.Sprintf("run_events:%s", w.run.ID.String())
	if w.eventBus != nil {
		if err := w.eventBus.Publish(ctx, channel, ""); err != nil {
			slog.Warn("event_bus_publish_failed", "channel", channel, "err", err)
		}
	} else {
		if _, err := w.pool.Exec(ctx, "SELECT pg_notify($1, '')", channel); err != nil {
			slog.Warn("pg_notify_failed", "channel", channel, "err", err)
		}
	}

	if w.runLimiterRDB != nil {
		redisChannel := fmt.Sprintf("arkloop:sse:run_events:%s", w.run.ID.String())
		if _, err := w.runLimiterRDB.Publish(ctx, redisChannel, "").Result(); err != nil {
			slog.Warn("redis_publish_failed", "channel", redisChannel, "err", err)
		}
	}

	if w.hasTerminal {
		for _, nextRunID := range w.pendingEnqueueRunIDs {
			if w.projector == nil {
				continue
			}
			if err := w.projector.EnqueueRun(ctx, w.run.AccountID, nextRunID, w.traceID, nil, nil); err != nil {
				if markErr := w.projector.MarkRunFailed(context.Background(), nextRunID, "failed to enqueue child run job"); markErr != nil {
					slog.Error("mark_child_run_failed",
						"run_id", nextRunID.String(),
						"enqueue_error", err.Error(),
						"mark_error", markErr.Error(),
					)
				}
			}
		}
		w.pendingEnqueueRunIDs = nil
		if w.runLimiterRDB != nil && w.terminalRunStatus != "" {
			// 通知可能正在等待的父 Run（无父 Run 时此 publish 为空操作）
			output := ""
			if w.terminalRunStatus == "completed" {
				output = truncateChildRunPayload(strings.Join(w.assistantDeltas, ""))
			} else {
				output = truncateChildRunPayload(w.terminalMessage)
			}
			ch := fmt.Sprintf("run.child.%s.done", w.run.ID.String())
			_, _ = w.runLimiterRDB.Publish(ctx, ch, w.terminalRunStatus+"\n"+output).Result()
		}
		w.hasTerminal = false
		w.terminalMessage = ""
		if w.releaseSlot != nil {
			w.releaseSlot()
		}
	}

	return nil
}

func (w *eventWriter) Completed() bool {
	return w.completed
}

// TerminalUserMessage 返回终局失败/取消时用于对外的摘要（Flush 前可读，commit 后会清空内部缓存）。
func (w *eventWriter) TerminalUserMessage() string {
	return strings.TrimSpace(w.terminalMessage)
}

// AssistantOutput 返回本次 run 的 assistant 最终拼接文本，供调用方写回 RunContext。
func (w *eventWriter) AssistantOutput() string {
	if w.assistantMessage != nil {
		return llm.VisibleMessageText(*w.assistantMessage)
	}
	return strings.Join(w.assistantDeltas, "")
}

func (w *eventWriter) AssistantOutputs() []string {
	if len(w.assistantOutputs) == 0 {
		output := strings.TrimSpace(w.AssistantOutput())
		if output == "" {
			return nil
		}
		return []string{output}
	}
	out := make([]string, 0, len(w.assistantOutputs))
	for _, item := range w.assistantOutputs {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (w *eventWriter) InsertAssistantMessage(
	ctx context.Context,
	repo data.MessagesRepository,
	accountID uuid.UUID,
	threadID uuid.UUID,
	hidden bool,
) (uuid.UUID, error) {
	if err := w.ensureTx(ctx); err != nil {
		return uuid.Nil, err
	}
	message := w.finalAssistantMessage()
	content := llm.VisibleMessageText(message)
	contentJSON, err := llm.BuildAssistantThreadContentJSON(message)
	if err != nil {
		return uuid.Nil, err
	}
	messageID, err := repo.InsertAssistantMessage(ctx, w.tx, accountID, threadID, w.run.ID, content, contentJSON, hidden)
	if err != nil {
		return uuid.Nil, err
	}
	if messageID != uuid.Nil {
		if err := (data.SubAgentRepository{}).SetLastOutputRefByLastCompletedRunID(ctx, w.tx, w.run.ID, "message:"+messageID.String()); err != nil {
			return uuid.Nil, err
		}
	}
	return messageID, nil
}

func (w *eventWriter) finalAssistantMessage() llm.Message {
	if w.assistantMessage != nil {
		return *w.assistantMessage
	}
	content := strings.Join(w.assistantDeltas, "")
	if strings.TrimSpace(content) == "" {
		return llm.Message{Role: "assistant"}
	}
	return llm.Message{
		Role:    "assistant",
		Content: []llm.TextPart{{Text: content}},
	}
}

func (w *eventWriter) captureAssistantTurnOutput() {
	text := ""
	if w.assistantMessageFresh && w.assistantMessage != nil {
		text = llm.VisibleMessageText(*w.assistantMessage)
	} else if w.lastTurnDeltaCount < len(w.assistantDeltas) {
		text = strings.Join(w.assistantDeltas[w.lastTurnDeltaCount:], "")
	}
	w.lastTurnDeltaCount = len(w.assistantDeltas)
	w.assistantMessageFresh = false
	if trimmed := strings.TrimSpace(text); trimmed != "" {
		w.assistantOutputs = append(w.assistantOutputs, trimmed)
	}
}

func (w *eventWriter) captureReplyOverride(dataJSON map[string]any) {
	if dataJSON == nil {
		return
	}
	toolName, _ := dataJSON["tool_name"].(string)
	if strings.TrimSpace(toolName) != "telegram_reply" {
		return
	}
	args, _ := dataJSON["arguments"].(map[string]any)
	if args == nil {
		return
	}
	if mid, _ := args["reply_to_message_id"].(string); strings.TrimSpace(mid) != "" {
		w.pendingReplyOverride = strings.TrimSpace(mid)
	}
}

func (w *eventWriter) collectToolCall(dataJSON map[string]any) {
	callID, _ := dataJSON["tool_call_id"].(string)
	toolName, _ := dataJSON["tool_name"].(string)
	toolName = llm.CanonicalToolName(toolName)
	if callID == "" || toolName == "" {
		return
	}
	args, _ := dataJSON["arguments"].(map[string]any)
	w.pendingToolCalls = append(w.pendingToolCalls, llm.ToolCall{
		ToolCallID:    callID,
		ToolName:      toolName,
		ArgumentsJSON: args,
	})
}

func (w *eventWriter) flushPendingToolCalls() {
	if len(w.pendingToolCalls) == 0 {
		w.pendingToolResults = w.pendingToolResults[:0]
		return
	}

	// 只保留已有对应 result 的 call，确保 tool_use/tool_result 配对写入
	resolved := make(map[string]struct{}, len(w.pendingToolResults))
	for _, r := range w.pendingToolResults {
		resolved[r.ToolCallID] = struct{}{}
	}
	filteredCalls := make([]llm.ToolCall, 0, len(w.pendingToolCalls))
	keptCallIDs := make(map[string]struct{}, len(w.pendingToolCalls))
	for _, call := range w.pendingToolCalls {
		if _, ok := resolved[call.ToolCallID]; ok {
			if w.heartbeatRun && IsHeartbeatDecisionToolName(call.ToolName) {
				continue
			}
			filteredCalls = append(filteredCalls, call)
			keptCallIDs[call.ToolCallID] = struct{}{}
		}
	}

	w.pendingToolCalls = w.pendingToolCalls[:0]
	results := w.pendingToolResults
	w.pendingToolResults = w.pendingToolResults[:0]
	filteredResults := make([]intermediateMessage, 0, len(results))
	for _, result := range results {
		if _, ok := keptCallIDs[result.ToolCallID]; ok {
			filteredResults = append(filteredResults, result)
		}
	}

	msg := w.assistantMessage
	hasVisibleParts := msg != nil && len(llm.VisibleContentParts(msg.Content)) > 0
	if len(filteredCalls) == 0 && !hasVisibleParts {
		// 所有 call 均无结果（suppressed 或被黑名单移除），且无可见内容可保留
		return
	}

	if msg == nil {
		msg = &llm.Message{Role: "assistant"}
	}
	contentJSON, err := llm.BuildIntermediateAssistantContentJSON(*msg, filteredCalls)
	if err != nil {
		return
	}
	w.intermediateMessages = append(w.intermediateMessages, intermediateMessage{
		Role:        "assistant",
		Content:     llm.VisibleMessageText(*msg),
		ContentJSON: contentJSON,
	})
	w.intermediateMessages = append(w.intermediateMessages, filteredResults...)
}

func (w *eventWriter) collectToolResult(dataJSON map[string]any) {
	toolName, _ := dataJSON["tool_name"].(string)
	toolName = llm.CanonicalToolName(toolName)
	envelope := map[string]any{
		"tool_call_id": dataJSON["tool_call_id"],
		"tool_name":    toolName,
	}
	if v, ok := dataJSON["result"]; ok {
		envelope["result"] = v
	}
	if v, ok := dataJSON["error"]; ok {
		envelope["error"] = v
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return
	}
	callID, _ := dataJSON["tool_call_id"].(string)
	w.pendingToolResults = append(w.pendingToolResults, intermediateMessage{
		Role:       "tool",
		Content:    string(raw),
		ToolCallID: callID,
	})
}

func (w *eventWriter) batchInsertIntermediateMessages(
	ctx context.Context,
	repo data.MessagesRepository,
	accountID, threadID, runID uuid.UUID,
) error {
	if err := w.ensureTx(ctx); err != nil {
		return err
	}
	baseTime := w.run.CreatedAt.UTC().Add(time.Millisecond).Round(0)
	for i, msg := range w.intermediateMessages {
		createdAt := baseTime.Add(time.Duration(i) * time.Microsecond)
		meta := map[string]any{
			"intermediate": "true",
			"run_id":       runID.String(),
		}
		if msg.ToolCallID != "" {
			meta["tool_call_id"] = msg.ToolCallID
		}
		metadataJSON, _ := json.Marshal(meta)
		var contentJSON json.RawMessage
		if msg.Role != "tool" {
			contentJSON = msg.ContentJSON
		}
		if _, err := repo.InsertIntermediateMessage(ctx, w.tx, accountID, threadID, msg.Role, msg.Content, contentJSON, metadataJSON, createdAt); err != nil {
			return err
		}
	}
	return nil
}

func (w *eventWriter) Flush(ctx context.Context) error {
	return w.commit(ctx)
}

func (w *eventWriter) Close(ctx context.Context) {
	if w.tx != nil {
		_ = w.tx.Rollback(ctx)
		w.tx = nil
	}
}

func extractAssistantDelta(dataJSON map[string]any) string {
	role, ok := dataJSON["role"]
	if ok && role != nil && role != "assistant" {
		return ""
	}
	delta, _ := dataJSON["content_delta"].(string)
	if delta == "" {
		return ""
	}
	// 过滤 MiniMax 等模型在工具调用后输出的终止 token
	if strings.TrimSpace(delta) == "<end_turn>" {
		return ""
	}
	return delta
}

func extractProgressSegmentStart(dataJSON map[string]any) (segmentID, kind, mode, label string) {
	if dataJSON == nil {
		return "", "", "", ""
	}
	segmentID, _ = dataJSON["segment_id"].(string)
	kind, _ = dataJSON["kind"].(string)
	display, _ := dataJSON["display"].(map[string]any)
	if display == nil {
		return strings.TrimSpace(segmentID), strings.TrimSpace(kind), "", ""
	}
	mode, _ = display["mode"].(string)
	label, _ = display["label"].(string)
	return strings.TrimSpace(segmentID), strings.TrimSpace(kind), strings.TrimSpace(mode), strings.TrimSpace(label)
}

func assistantMessageFromEventData(dataJSON map[string]any) (llm.Message, bool) {
	if dataJSON == nil {
		return llm.Message{}, false
	}
	raw, ok := dataJSON["assistant_message"].(map[string]any)
	if !ok || raw == nil {
		return llm.Message{}, false
	}
	message, err := llm.MessageFromJSONMap(raw)
	if err != nil {
		return llm.Message{}, false
	}
	return message, true
}

// ShouldSuppressHeartbeatOutput 判断 heartbeat 终态是否应跳过写 thread / 外发渠道。
// 原则：工具未调用 → 抑制；reply=false → 抑制；reply=true → 不抑制。
func ShouldSuppressHeartbeatOutput(rc *RunContext, output string) bool {
	if rc == nil || !rc.HeartbeatRun {
		return false
	}
	trimmed := strings.TrimSpace(output)
	if trimmed == "" || trimmed == "HEARTBEAT_OK" || trimmed == "[No substantive content to send]" {
		return true
	}
	if rc.HeartbeatToolOutcome != nil {
		return !rc.HeartbeatToolOutcome.Reply
	}
	return true
}

func (w *eventWriter) accumUsage(dataJSON map[string]any) {
	if dataJSON == nil {
		return
	}
	if usage, ok := dataJSON["usage"].(map[string]any); ok {
		if v, ok := toInt64(usage["input_tokens"]); ok {
			w.totalInputTokens += v
		}
		if v, ok := toInt64(usage["output_tokens"]); ok {
			w.totalOutputTokens += v
		}
		if v, ok := toInt64(usage["cache_creation_input_tokens"]); ok {
			w.totalCacheCreationTokens += v
		}
		if v, ok := toInt64(usage["cache_read_input_tokens"]); ok {
			w.totalCacheReadTokens += v
		}
		if v, ok := toInt64(usage["cached_tokens"]); ok {
			w.totalCachedTokens += v
		}
	}
	if cost, ok := dataJSON["cost"].(map[string]any); ok {
		if v, ok := toInt64(cost["amount_micros"]); ok {
			w.totalCostUSD += float64(v) / 1_000_000.0
		}
	}
}

func shouldAccumulateUsageForEvent(eventType string) bool {
	switch eventType {
	case "run.completed", "run.failed", "run.cancelled", "run.interrupted":
		return false
	default:
		return true
	}
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}

// creditDeductionResult 封装积分计算结果和明细。
type creditDeductionResult struct {
	Credits  int64
	Metadata map[string]any
}

// calcCreditDeduction 按实际 cost（USD）计算积分消耗，并返回计算明细。
// 汇率：creditsPerUSD（credit.per_usd）* multiplier。
// totalCostUSD 为 0 时退回按 token 计算的兜底值。
func (w *eventWriter) calcCreditDeduction() creditDeductionResult {
	totalTokens := w.totalInputTokens + w.totalOutputTokens
	policyMultiplier := w.policy.MultiplierFor(totalTokens, w.totalCostUSD)
	if policyMultiplier == 0 {
		return creditDeductionResult{}
	}

	meta := map[string]any{
		"type":              "llm",
		"model":             w.model,
		"input_tokens":      w.totalInputTokens,
		"output_tokens":     w.totalOutputTokens,
		"cost_usd":          w.totalCostUSD,
		"credits_per_usd":   w.creditsPerUSD,
		"multiplier":        w.multiplier,
		"policy_multiplier": policyMultiplier,
	}

	if w.totalCostUSD > 0 {
		raw := w.totalCostUSD * w.creditsPerUSD * w.multiplier * policyMultiplier
		credits := int64(math.Ceil(raw))
		if credits < 1 {
			credits = 1
		}
		meta["method"] = "cost_usd"
		meta["raw_credits"] = raw
		meta["credits"] = credits
		return creditDeductionResult{Credits: credits, Metadata: meta}
	}

	// 兜底：无 cost 数据时按加权 token 计算
	if w.totalInputTokens <= 0 && w.totalOutputTokens <= 0 {
		return creditDeductionResult{}
	}
	hasAnthropicCache := w.totalCacheCreationTokens > 0 || w.totalCacheReadTokens > 0
	hasOpenAICache := w.totalCachedTokens > 0

	effective := 0.0
	switch {
	case hasAnthropicCache && !hasOpenAICache:
		effective = float64(w.totalInputTokens)*1.0 +
			float64(w.totalCacheCreationTokens)*1.25 +
			float64(w.totalCacheReadTokens)*0.1 +
			float64(w.totalOutputTokens)*1.0
	case hasOpenAICache && !hasAnthropicCache:
		nonCached := w.totalInputTokens - w.totalCachedTokens
		if nonCached < 0 {
			nonCached = 0
		}
		effective = float64(nonCached)*1.0 +
			float64(w.totalCachedTokens)*0.5 +
			float64(w.totalOutputTokens)*1.0
	case hasAnthropicCache && hasOpenAICache:
		nonCached := w.totalInputTokens - w.totalCacheReadTokens - w.totalCachedTokens
		if nonCached < 0 {
			nonCached = 0
		}
		effective = float64(nonCached)*1.0 +
			float64(w.totalCacheCreationTokens)*1.25 +
			float64(w.totalCacheReadTokens)*0.1 +
			float64(w.totalCachedTokens)*0.5 +
			float64(w.totalOutputTokens)*1.0
	default:
		effective = float64(w.totalInputTokens)*1.0 + float64(w.totalOutputTokens)*1.0
	}
	raw := effective / 1000.0 * w.multiplier * policyMultiplier
	credits := int64(math.Ceil(raw))
	if credits < 1 {
		credits = 1
	}
	meta["method"] = "token_fallback"
	meta["effective_tokens"] = effective
	meta["cache_creation_tokens"] = w.totalCacheCreationTokens
	meta["cache_read_tokens"] = w.totalCacheReadTokens
	meta["cached_tokens"] = w.totalCachedTokens
	meta["raw_credits"] = raw
	meta["credits"] = credits
	return creditDeductionResult{Credits: credits, Metadata: meta}
}

// calcPlatformCost 分段计算实际成本（USD）。
// 未配置任何 input/output 费率时返回 -1，表示使用 LLM 返回的原始值。
// Cache 定价：
//   - 未配置 costPer1kCacheWrite/Read 时，使用 input 费率乘以行业默认比例
//   - Anthropic cache_creation: 1.25× input；cache_read: 0.10× input
//   - OpenAI cached_tokens: 0.50× input（未命中部分 = totalInput - cachedTokens）
func (w *eventWriter) calcPlatformCost() float64 {
	if w.costPer1kInput == nil && w.costPer1kOutput == nil {
		return -1
	}

	var cost float64

	// output tokens（不受缓存影响）
	if w.costPer1kOutput != nil {
		cost += float64(w.totalOutputTokens) / 1000.0 * *w.costPer1kOutput
	}

	inputRate := 0.0
	if w.costPer1kInput != nil {
		inputRate = *w.costPer1kInput
	}

	hasAnthropicCache := w.totalCacheCreationTokens > 0 || w.totalCacheReadTokens > 0
	hasOpenAICache := w.totalCachedTokens > 0

	switch {
	case hasAnthropicCache && !hasOpenAICache:
		// Anthropic: input_tokens 为非缓存输入，cache_write/cache_read 单独计费
		cost += float64(w.totalInputTokens) / 1000.0 * inputRate
		if w.totalCacheCreationTokens > 0 {
			rate := inputRate * 1.25
			if w.costPer1kCacheWrite != nil {
				rate = *w.costPer1kCacheWrite
			}
			cost += float64(w.totalCacheCreationTokens) / 1000.0 * rate
		}
		if w.totalCacheReadTokens > 0 {
			rate := inputRate * 0.10
			if w.costPer1kCacheRead != nil {
				rate = *w.costPer1kCacheRead
			}
			cost += float64(w.totalCacheReadTokens) / 1000.0 * rate
		}
	case hasOpenAICache && !hasAnthropicCache:
		// OpenAI: input_tokens 含 cached_tokens，命中部分按 cache_read 费率
		cacheRate := inputRate * 0.50
		if w.costPer1kCacheRead != nil {
			cacheRate = *w.costPer1kCacheRead
		}
		uncached := w.totalInputTokens - w.totalCachedTokens
		if uncached < 0 {
			uncached = 0
		}
		cost += float64(uncached)/1000.0*inputRate + float64(w.totalCachedTokens)/1000.0*cacheRate
	case hasAnthropicCache && hasOpenAICache:
		// TODO: 混合 provider 缓存口径统一后再替换；当前保留旧逻辑避免行为突变。
		nonCachedInput := w.totalInputTokens - w.totalCacheReadTokens
		if nonCachedInput > 0 {
			cost += float64(nonCachedInput) / 1000.0 * inputRate
		}
		if w.totalCacheCreationTokens > 0 {
			rate := inputRate * 1.25
			if w.costPer1kCacheWrite != nil {
				rate = *w.costPer1kCacheWrite
			}
			cost += float64(w.totalCacheCreationTokens) / 1000.0 * rate
		}
		if w.totalCacheReadTokens > 0 {
			rate := inputRate * 0.10
			if w.costPer1kCacheRead != nil {
				rate = *w.costPer1kCacheRead
			}
			cost += float64(w.totalCacheReadTokens) / 1000.0 * rate
		}
	default:
		// no cache
		cost += float64(w.totalInputTokens) / 1000.0 * inputRate
	}

	return cost
}
