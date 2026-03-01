package pipeline

import (
"context"
"encoding/json"
"fmt"
"log/slog"
"strings"

"arkloop/services/worker/internal/llm"
"arkloop/services/worker/internal/routing"

"github.com/google/uuid"
"github.com/jackc/pgx/v5/pgxpool"
"github.com/redis/go-redis/v9"
)

const settingTitleSummarizerAgentConfigID = "title_summarizer.agent_config_id"

// NewTitleSummarizerMiddleware 在 thread 无标题时异步调 LLM 生成初始标题。
// 位于 RoutingMiddleware 之后（需要 rc.Gateway），ToolBuildMiddleware 之前。
// goroutine 内部完成 LLM call + DB 写入 + 通知推送，不阻塞主流程。
func NewTitleSummarizerMiddleware(pool *pgxpool.Pool, rdb *redis.Client, stubGateway llm.Gateway, emitDebugEvents bool) RunMiddleware {
return func(ctx context.Context, rc *RunContext, next RunHandler) error {
if rc.TitleSummarizer == nil || pool == nil {
return next(ctx, rc)
}

threadID := rc.Run.ThreadID
firstRun, err := isFirstRunOfThread(ctx, pool, threadID)
if err != nil {
slog.WarnContext(ctx, "title_summarizer: check failed", "err", err.Error())
return next(ctx, rc)
}
if !firstRun {
return next(ctx, rc)
}

// 快照 goroutine 所需的值，避免闭包捕获整个 rc
fallbackGateway := rc.Gateway
fallbackModel := ""
if rc.SelectedRoute != nil {
fallbackModel = rc.SelectedRoute.Route.Model
}
runID := rc.Run.ID
orgID := rc.Run.OrgID
messages := append([]llm.Message{}, rc.Messages...)
prompt := rc.TitleSummarizer.Prompt
maxTokens := rc.TitleSummarizer.MaxTokens

go func() {
bgCtx := context.Background()
gateway, model := resolveTitleGateway(bgCtx, pool, orgID, fallbackGateway, fallbackModel, stubGateway, emitDebugEvents)
if gateway == nil {
return
}
generateTitle(pool, rdb, gateway, runID, threadID, model, messages, prompt, maxTokens)
}()

return next(ctx, rc)
}
}

// resolveTitleGateway 尝试从 platform_settings 读取指定的 agent config，
// 构建独立的 LLM gateway 用于标题生成。找不到配置时回退到 fallback。
func resolveTitleGateway(
ctx context.Context,
pool *pgxpool.Pool,
orgID uuid.UUID,
fallbackGateway llm.Gateway,
fallbackModel string,
stubGateway llm.Gateway,
emitDebugEvents bool,
) (llm.Gateway, string) {
var agentConfigID string
err := pool.QueryRow(ctx,
`SELECT value FROM platform_settings WHERE key = $1`,
settingTitleSummarizerAgentConfigID,
).Scan(&agentConfigID)
agentConfigID = strings.TrimSpace(agentConfigID)

if err != nil || agentConfigID == "" {
return fallbackGateway, fallbackModel
}

// agent_configs.model 字段存储的是 credential name
var credName string
err = pool.QueryRow(ctx,
`SELECT COALESCE(model, '') FROM agent_configs WHERE id = $1`,
agentConfigID,
).Scan(&credName)
if err != nil || strings.TrimSpace(credName) == "" {
slog.Warn("title_summarizer: agent config not found or no model", "id", agentConfigID)
return fallbackGateway, fallbackModel
}
credName = strings.TrimSpace(credName)

routingCfg, err := routing.LoadRoutingConfigFromDB(ctx, pool)
if err != nil {
slog.Warn("title_summarizer: load routing config failed", "err", err.Error())
return fallbackGateway, fallbackModel
}

route, cred, ok := routingCfg.GetHighestPriorityRouteByCredentialName(credName, map[string]any{})
if !ok {
slog.Warn("title_summarizer: credential not found in routing", "credential", credName)
return fallbackGateway, fallbackModel
}

gw, err := gatewayFromCredential(cred, stubGateway, emitDebugEvents)
if err != nil {
slog.Warn("title_summarizer: build gateway failed", "err", err.Error())
return fallbackGateway, fallbackModel
}

return gw, route.Model
}

// isFirstRunOfThread 检查当前是否是 thread 的第一个 run。
func isFirstRunOfThread(ctx context.Context, pool *pgxpool.Pool, threadID uuid.UUID) (bool, error) {
var count int
err := pool.QueryRow(ctx,
`SELECT COUNT(*) FROM runs WHERE thread_id = $1 AND deleted_at IS NULL`,
threadID,
).Scan(&count)
if err != nil {
return false, err
}
return count <= 1, nil
}

func generateTitle(
pool *pgxpool.Pool,
rdb *redis.Client,
gateway llm.Gateway,
runID uuid.UUID,
threadID uuid.UUID,
model string,
messages []llm.Message,
prompt string,
maxTokens int,
) {
ctx := context.Background()

userText := extractUserText(messages)
if userText == "" {
return
}

req := llm.Request{
Model: model,
Messages: []llm.Message{
{
Role:    "system",
Content: []llm.TextPart{{Text: buildSummarizeSystem(prompt)}},
},
{
Role:    "user",
Content: []llm.TextPart{{Text: userText}},
},
},
MaxOutputTokens: &maxTokens,
}

	var chunks []string
	sentinel := fmt.Errorf("done")

	err := gateway.Stream(ctx, req, func(ev llm.StreamEvent) error {
		switch typed := ev.(type) {
		case llm.StreamMessageDelta:
			// 跳过 thinking channel，只采集主输出
			if typed.Channel != nil && *typed.Channel == "thinking" {
				return nil
			}
			if typed.ContentDelta != "" {
				chunks = append(chunks, typed.ContentDelta)
			}
		case llm.StreamRunCompleted:
			return sentinel
		case llm.StreamRunFailed:
			return sentinel
		}
		return nil
	})
	if err != nil && err != sentinel {
		slog.Warn("title_summarizer: llm call failed", "err", err.Error())
		return
	}

title := strings.TrimSpace(strings.Join(chunks, ""))
if title == "" {
return
}
if len([]rune(title)) > 50 {
title = string([]rune(title)[:50])
}

_, err = pool.Exec(ctx,
`UPDATE threads SET title = $1 WHERE id = $2 AND deleted_at IS NULL AND title_locked = false`,
title, threadID,
)
if err != nil {
slog.Warn("title_summarizer: db update failed", "err", err.Error())
return
}

emitTitleEvent(ctx, pool, rdb, runID, threadID, title)
}

// emitTitleEvent 写入 thread.title.updated 事件到 run_events 表，触发 SSE 推送。
func emitTitleEvent(
ctx context.Context,
pool *pgxpool.Pool,
rdb *redis.Client,
runID uuid.UUID,
threadID uuid.UUID,
title string,
) {
dataJSON := map[string]any{
"thread_id": threadID.String(),
"title":     title,
}
encoded, err := json.Marshal(dataJSON)
if err != nil {
return
}

tx, err := pool.Begin(ctx)
if err != nil {
return
}
defer tx.Rollback(ctx)

var seq int64
if err = tx.QueryRow(ctx, `SELECT nextval('run_events_seq_global')`).Scan(&seq); err != nil {
return
}

_, err = tx.Exec(ctx,
`INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, $2, $3, $4::jsonb)`,
runID, seq, "thread.title.updated", string(encoded),
)
if err != nil {
return
}

if err = tx.Commit(ctx); err != nil {
return
}

pgChannel := fmt.Sprintf(`"run_events:%s"`, runID.String())
_, _ = pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgChannel, "ping")
if rdb != nil {
rdbChannel := fmt.Sprintf("arkloop:sse:run_events:%s", runID.String())
_, _ = rdb.Publish(ctx, rdbChannel, "ping").Result()
}
}

func extractUserText(messages []llm.Message) string {
var parts []string
for _, msg := range messages {
if msg.Role != "user" {
continue
}
for _, part := range msg.Content {
if t := strings.TrimSpace(part.Text); t != "" {
parts = append(parts, t)
}
}
}
joined := strings.Join(parts, "\n")
if len([]rune(joined)) > 500 {
joined = string([]rune(joined)[:500])
}
return joined
}

// buildSummarizeSystem 构建标题生成的 system prompt。
// 外层固定指令确保 LLM 只输出纯标题，persona 的 prompt 作为风格说明追加。
func buildSummarizeSystem(styleHint string) string {
base := "Generate a concise title for the conversation. Output ONLY the title text — no quotes, no punctuation at the end, no explanation, no prefix like 'Title:'. The title must be in the same language as the user's message."
if styleHint != "" {
return base + "\n\n" + styleHint
}
return base
}
