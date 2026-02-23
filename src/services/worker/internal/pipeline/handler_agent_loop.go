package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"arkloop/services/shared/runlimit"
	"arkloop/services/worker/internal/agent"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	eventCommitBatchSize   = 20
	eventCommitMaxInterval = 50 * time.Millisecond
)

var (
	cancelEvtTypes = []string{"run.cancel_requested", "run.cancelled"}
	streamingEventTypes = map[string]struct{}{
		"message.delta":      {},
		"llm.response.chunk": {},
	}
	errStopProcessing = errors.New("stop_processing")
)

// NewAgentLoopHandler 构建 Pipeline 终端 Handler：执行 Agent Loop 并写入事件。
func NewAgentLoopHandler(
	runsRepo data.RunsRepository,
	eventsRepo data.RunEventsRepository,
	messagesRepo data.MessagesRepository,
	runLimiterRDB *redis.Client,
	usageRepo data.UsageRecordsRepository,
	creditsRepo data.CreditsRepository,
) RunHandler {
	return func(ctx context.Context, rc *RunContext) error {
		selected := rc.SelectedRoute

		writer := newEventWriter(
			rc.Pool, rc.Run, rc.TraceID, runLimiterRDB,
			selected.Route.Model, usageRepo, creditsRepo,
			selected.Route.Multiplier, selected.Route.CostPer1kInput, selected.Route.CostPer1kOutput,
		)
		defer writer.Close(ctx)

		routeSelected := rc.Emitter.Emit("run.route.selected", selected.ToRunEventDataJSON(), nil, nil)
		if err := writer.Append(ctx, runsRepo, eventsRepo, rc.Run.ID, routeSelected); err != nil {
			if errors.Is(err, errStopProcessing) {
				return nil
			}
			return err
		}

		toolExecutor := rc.ToolExecutor
		toolSpecs := rc.FinalSpecs

		messages := append([]llm.Message{}, rc.Messages...)
		if strings.TrimSpace(rc.SystemPrompt) != "" {
			messages = append([]llm.Message{
				{
					Role:    "system",
					Content: []llm.TextPart{{Text: rc.SystemPrompt}},
				},
			}, messages...)
		}

		agentRequest := llm.Request{
			Model:           selected.Route.Model,
			Messages:        messages,
			Tools:           append([]llm.ToolSpec{}, toolSpecs...),
			MaxOutputTokens: rc.MaxOutputTokens,
		}

		loop := agent.NewLoop(rc.Gateway, toolExecutor)
		runCtx := agent.RunContext{
			RunID:         rc.Run.ID,
			TraceID:       rc.TraceID,
			InputJSON:     rc.InputJSON,
			MaxIterations: rc.MaxIterations,
			ToolExecutor:  toolExecutor,
			ToolTimeoutMs: rc.ToolTimeoutMs,
			ToolBudget:    rc.ToolBudget,
			CancelSignal: func() bool {
				return ctx.Err() != nil
			},
		}

		err := loop.Run(ctx, runCtx, agentRequest, rc.Emitter, func(ev events.RunEvent) error {
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

		if writer.Completed() {
			if err := writer.InsertAssistantMessage(ctx, messagesRepo, rc.Run.OrgID, rc.Run.ThreadID); err != nil {
				return err
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
	runLimiterRDB *redis.Client
	model         string
	usageRepo     data.UsageRecordsRepository
	creditsRepo   data.CreditsRepository

	multiplier      float64
	costPer1kInput  *float64
	costPer1kOutput *float64

	tx                       pgx.Tx
	pendingEventsSinceCommit int
	lastCommitAt             time.Time
	assistantDeltas          []string
	completed                bool
	hasTerminal              bool

	totalInputTokens  int64
	totalOutputTokens int64
	totalCostUSD      float64
}

func newEventWriter(
	pool *pgxpool.Pool,
	run data.Run,
	traceID string,
	runLimiterRDB *redis.Client,
	model string,
	usageRepo data.UsageRecordsRepository,
	creditsRepo data.CreditsRepository,
	multiplier float64,
	costPer1kInput *float64,
	costPer1kOutput *float64,
) *eventWriter {
	if multiplier <= 0 {
		multiplier = 1.0
	}
	return &eventWriter{
		pool:            pool,
		run:             run,
		traceID:         strings.TrimSpace(traceID),
		lastCommitAt:    time.Now(),
		runLimiterRDB:   runLimiterRDB,
		model:           model,
		usageRepo:       usageRepo,
		creditsRepo:     creditsRepo,
		multiplier:      multiplier,
		costPer1kInput:  costPer1kInput,
		costPer1kOutput: costPer1kOutput,
	}
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
	if err := w.ensureTx(ctx); err != nil {
		return err
	}

	if err := runsRepo.LockRunRow(ctx, w.tx, runID); err != nil {
		return err
	}

	cancelType, err := eventsRepo.GetLatestEventType(ctx, w.tx, runID, cancelEvtTypes)
	if err != nil {
		return err
	}
	if cancelType == "run.cancel_requested" {
		emitter := events.NewEmitter(w.traceID)
		cancelled := emitter.Emit("run.cancelled", map[string]any{}, nil, nil)
		if _, err := eventsRepo.AppendEvent(ctx, w.tx, runID, cancelled.Type, cancelled.DataJSON, cancelled.ToolName, cancelled.ErrorClass); err != nil {
			return err
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
		if err := w.usageRepo.Insert(ctx, w.tx, w.run.OrgID, runID, w.model, w.totalInputTokens, w.totalOutputTokens, w.totalCostUSD); err != nil {
			return err
		}
		if credits := w.calcCreditDeduction(); credits > 0 {
			if err := w.creditsRepo.Deduct(ctx, w.tx, w.run.OrgID, credits, runID); err != nil {
				return err
			}
		}
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

	if _, err := eventsRepo.AppendEvent(ctx, w.tx, runID, ev.Type, ev.DataJSON, ev.ToolName, ev.ErrorClass); err != nil {
		return err
	}
	w.pendingEventsSinceCommit++

	w.accumUsage(ev.DataJSON)

	if ev.Type == "message.delta" {
		if delta := extractAssistantDelta(ev.DataJSON); delta != "" {
			w.assistantDeltas = append(w.assistantDeltas, delta)
		}
	}

	if status, ok := TerminalStatuses[ev.Type]; ok {
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
		if err := w.usageRepo.Insert(ctx, w.tx, w.run.OrgID, runID, w.model, w.totalInputTokens, w.totalOutputTokens, w.totalCostUSD); err != nil {
			return err
		}
		if credits := w.calcCreditDeduction(); credits > 0 {
			if err := w.creditsRepo.Deduct(ctx, w.tx, w.run.OrgID, credits, runID); err != nil {
				return err
			}
		}
		w.hasTerminal = true
		return nil
	}

	if _, ok := streamingEventTypes[ev.Type]; !ok {
		return w.commit(ctx)
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
	if err := w.tx.Commit(ctx); err != nil {
		return err
	}
	w.tx = nil
	w.pendingEventsSinceCommit = 0
	w.lastCommitAt = time.Now()

	channel := fmt.Sprintf("run_events:%s", w.run.ID.String())
	_, _ = w.pool.Exec(ctx, "SELECT pg_notify($1, '')", channel)

	if w.hasTerminal {
		w.hasTerminal = false
		key := runlimit.Key(w.run.OrgID.String())
		runlimit.Release(ctx, w.runLimiterRDB, key)
	}

	return nil
}

func (w *eventWriter) Completed() bool {
	return w.completed
}

func (w *eventWriter) InsertAssistantMessage(
	ctx context.Context,
	repo data.MessagesRepository,
	orgID uuid.UUID,
	threadID uuid.UUID,
) error {
	if err := w.ensureTx(ctx); err != nil {
		return err
	}
	content := strings.Join(w.assistantDeltas, "")
	return repo.InsertAssistantMessage(ctx, w.tx, orgID, threadID, content)
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
	return delta
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
	}
	if cost, ok := dataJSON["cost"].(map[string]any); ok {
		if v, ok := toInt64(cost["amount_micros"]); ok {
			w.totalCostUSD += float64(v) / 1_000_000.0
		}
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

// calcCreditDeduction 按公式计算积分消耗：ceil((input + output) / 1000 * multiplier)，最小 1。
func (w *eventWriter) calcCreditDeduction() int64 {
	totalTokens := w.totalInputTokens + w.totalOutputTokens
	if totalTokens <= 0 {
		return 0
	}
	raw := float64(totalTokens) / 1000.0 * w.multiplier
	credits := int64(math.Ceil(raw))
	if credits < 1 {
		credits = 1
	}
	return credits
}

// calcPlatformCost 按配置的成本费率计算实际成本（USD）。未配置时返回 -1 表示使用 LLM 返回的原始值。
func (w *eventWriter) calcPlatformCost() float64 {
	if w.costPer1kInput == nil && w.costPer1kOutput == nil {
		return -1
	}
	var cost float64
	if w.costPer1kInput != nil {
		cost += float64(w.totalInputTokens) / 1000.0 * *w.costPer1kInput
	}
	if w.costPer1kOutput != nil {
		cost += float64(w.totalOutputTokens) / 1000.0 * *w.costPer1kOutput
	}
	return cost
}
