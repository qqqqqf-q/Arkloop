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
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"

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
			selected.Route.CostPer1kCacheWrite, selected.Route.CostPer1kCacheRead,
		)
		defer writer.Close(ctx)

		routeData := selected.ToRunEventDataJSON()
		if rc.AgentConfigName != "" {
			routeData["agent_config_name"] = rc.AgentConfigName
		}
		if rc.AgentConfigID != nil {
			routeData["agent_config_id"] = rc.AgentConfigID.String()
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
		if rc.SkillDefinition != nil {
			if rc.SkillDefinition.ExecutorType != "" {
				executorType = rc.SkillDefinition.ExecutorType
			}
			executorConfig = rc.SkillDefinition.ExecutorConfig
		}

		exec, execBuildErr := rc.ExecutorBuilder.Build(executorType, executorConfig)
		if execBuildErr != nil {
			return fmt.Errorf("build executor %q: %w", executorType, execBuildErr)
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
	runLimiterRDB *redis.Client // 双职责：并发槽释放（runlimit.Release）+ 跨实例 SSE 广播（Publish）
	model         string
	usageRepo     data.UsageRecordsRepository
	creditsRepo   data.CreditsRepository

	multiplier          float64
	costPer1kInput      *float64
	costPer1kOutput     *float64
	costPer1kCacheWrite *float64
	costPer1kCacheRead  *float64

	tx                       pgx.Tx
	pendingEventsSinceCommit int
	lastCommitAt             time.Time
	assistantDeltas          []string
	completed                bool
	hasTerminal              bool

	totalInputTokens         int64
	totalOutputTokens        int64
	totalCacheCreationTokens int64
	totalCacheReadTokens     int64
	totalCachedTokens        int64
	totalCostUSD             float64
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
	costPer1kCacheWrite *float64,
	costPer1kCacheRead *float64,
) *eventWriter {
	if multiplier <= 0 {
		multiplier = 1.0
	}
	return &eventWriter{
		pool:                pool,
		run:                 run,
		traceID:             strings.TrimSpace(traceID),
		lastCommitAt:        time.Now(),
		runLimiterRDB:       runLimiterRDB,
		model:               model,
		usageRepo:           usageRepo,
		creditsRepo:         creditsRepo,
		multiplier:          multiplier,
		costPer1kInput:      costPer1kInput,
		costPer1kOutput:     costPer1kOutput,
		costPer1kCacheWrite: costPer1kCacheWrite,
		costPer1kCacheRead:  costPer1kCacheRead,
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
		if err := w.usageRepo.Insert(ctx, w.tx, w.run.OrgID, runID, w.model,
			w.totalInputTokens, w.totalOutputTokens,
			w.totalCacheCreationTokens, w.totalCacheReadTokens, w.totalCachedTokens,
			w.totalCostUSD); err != nil {
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
		if err := w.usageRepo.Insert(ctx, w.tx, w.run.OrgID, runID, w.model,
			w.totalInputTokens, w.totalOutputTokens,
			w.totalCacheCreationTokens, w.totalCacheReadTokens, w.totalCachedTokens,
			w.totalCostUSD); err != nil {
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

	if w.runLimiterRDB != nil {
		redisChannel := fmt.Sprintf("arkloop:sse:run_events:%s", w.run.ID.String())
		_, _ = w.runLimiterRDB.Publish(ctx, redisChannel, "").Result()
	}

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

// calcCreditDeduction 按实际 cost（USD）计算积分消耗。
// 汇率：1 积分 = $0.0001（CREDITS_PER_USD = 10000）× multiplier。
// totalCostUSD 为 0 时退回按 token 计算的兜底值。
func (w *eventWriter) calcCreditDeduction() int64 {
	const creditsPerUSD = 1000.0 // 1 credit = $0.001

	if w.totalCostUSD > 0 {
		raw := w.totalCostUSD * creditsPerUSD * w.multiplier
		credits := int64(math.Ceil(raw))
		if credits < 1 {
			credits = 1
		}
		return credits
	}

	// 兜底：无 cost 数据时按加权 token 计算
	if w.totalInputTokens <= 0 && w.totalOutputTokens <= 0 {
		return 0
	}
	nonCached := w.totalInputTokens - w.totalCacheReadTokens - w.totalCachedTokens
	if nonCached < 0 {
		nonCached = 0
	}
	effective := float64(nonCached)*1.0 +
		float64(w.totalCacheCreationTokens)*1.25 +
		float64(w.totalCacheReadTokens)*0.1 +
		float64(w.totalCachedTokens)*0.5 +
		float64(w.totalOutputTokens)*1.0
	raw := effective / 1000.0 * w.multiplier
	credits := int64(math.Ceil(raw))
	if credits < 1 {
		credits = 1
	}
	return credits
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

	// Anthropic cache tokens
	if w.totalCacheCreationTokens > 0 || w.totalCacheReadTokens > 0 {
		// Anthropic input_tokens = total context (非 cached 部分单独计费)
		// non-cached input at full rate
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
	} else if w.totalCachedTokens > 0 {
		cacheRate := inputRate * 0.50
		if w.costPer1kCacheRead != nil {
			cacheRate = *w.costPer1kCacheRead
		}
		uncached := w.totalInputTokens - w.totalCachedTokens
		if uncached < 0 {
			uncached = 0
		}
		cost += float64(uncached)/1000.0*inputRate + float64(w.totalCachedTokens)/1000.0*cacheRate
	} else {
		// no cache
		cost += float64(w.totalInputTokens) / 1000.0 * inputRate
	}

	return cost
}
