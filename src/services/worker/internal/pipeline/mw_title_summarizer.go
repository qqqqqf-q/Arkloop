package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"arkloop/services/shared/eventbus"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/redis/go-redis/v9"
)

// TitleSummarizerDB 由 *pgxpool.Pool 与 desktop 的 data.DesktopDB 实现。
type TitleSummarizerDB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

const titleSummarizerTimeout = 30 * time.Second
const titleSummarizerTemperature = 0.2
const titleSummarizerMaterialsBudget = 1200

type TitleGeneratorFunc func(context.Context, TitleSummarizerDB, *redis.Client, eventbus.EventBus, llm.Gateway, uuid.UUID, uuid.UUID, string, []llm.Message, string, int)

var titleGenerator TitleGeneratorFunc = generateTitle

// SetTitleSummarizerGeneratorForTest configures the async title generator for tests.
func SetTitleSummarizerGeneratorForTest(fn TitleGeneratorFunc) {
	titleGenerator = fn
}

// ResetTitleSummarizerGeneratorForTest restores the production generator.
func ResetTitleSummarizerGeneratorForTest() {
	titleGenerator = generateTitle
}

const settingTitleSummarizerModel = "title_summarizer.model"

func titleSummarizerDebugEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("ARKLOOP_DESKTOP_TITLE_DEBUG")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func logTitleSummarizerDebug(ctx context.Context, step string, attrs ...slog.Attr) {
	if !titleSummarizerDebugEnabled() {
		return
	}
	all := append([]slog.Attr{slog.String("step", step)}, attrs...)
	slog.LogAttrs(ctx, slog.LevelInfo, "title_summarizer_debug", all...)
}

func NewTitleSummarizerMiddleware(db TitleSummarizerDB, rdb *redis.Client, auxGateway llm.Gateway, emitDebugEvents bool, loaders ...*routing.ConfigLoader) RunMiddleware {
	var configLoader *routing.ConfigLoader
	if len(loaders) > 0 {
		configLoader = loaders[0]
	}
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		logTitleSummarizerDebug(ctx, "middleware_enter",
			slog.String("run_id", rc.Run.ID.String()),
			slog.String("thread_id", rc.Run.ThreadID.String()),
		)
		if rc.TitleSummarizer == nil || db == nil {
			logTitleSummarizerDebug(ctx, "middleware_skip",
				slog.String("reason", "nil_config_or_db"),
				slog.Bool("title_config_nil", rc.TitleSummarizer == nil),
				slog.Bool("db_nil", db == nil),
				slog.String("run_id", rc.Run.ID.String()),
			)
			return next(ctx, rc)
		}

		threadID := rc.Run.ThreadID
		accountID := &rc.Run.AccountID
		firstRun, err := isFirstRunOfThread(ctx, db, threadID)
		if err != nil {
			slog.WarnContext(ctx, "title_summarizer: check failed", "err", err.Error())
			return next(ctx, rc)
		}
		if !firstRun {
			logTitleSummarizerDebug(ctx, "middleware_skip",
				slog.String("reason", "not_first_thread_run"),
				slog.String("thread_id", threadID.String()),
				slog.String("run_id", rc.Run.ID.String()),
			)
			return next(ctx, rc)
		}
		titleDone, err := hasThreadTitleUpdateEvent(ctx, db, rc.Run.ID)
		if err != nil {
			slog.WarnContext(ctx, "title_summarizer: dedupe check failed", "err", err.Error())
			return next(ctx, rc)
		}
		if titleDone {
			logTitleSummarizerDebug(ctx, "middleware_skip",
				slog.String("reason", "title_already_emitted"),
				slog.String("thread_id", threadID.String()),
				slog.String("run_id", rc.Run.ID.String()),
			)
			return next(ctx, rc)
		}

		fallbackGateway := rc.Gateway
		fallbackModel := ""
		if rc.SummarizerDefinition != nil && rc.SummarizerDefinition.Model != nil {
			fallbackModel = strings.TrimSpace(*rc.SummarizerDefinition.Model)
		}
		if fallbackModel == "" && rc.SelectedRoute != nil {
			fallbackModel = rc.SelectedRoute.Route.Model
		}
		runID := rc.Run.ID
		messages := append([]llm.Message{}, rc.Messages...)
		prompt := rc.TitleSummarizer.Prompt
		maxTokens := rc.TitleSummarizer.MaxTokens
		llmMaxResponseBytes := rc.LlmMaxResponseBytes

		bus := rc.EventBus
		byok := rc.RoutingByokEnabled

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), titleSummarizerTimeout)
			defer cancel()
			logTitleSummarizerDebug(ctx, "async_title_start",
				slog.String("run_id", runID.String()),
				slog.String("thread_id", threadID.String()),
				slog.Bool("fallback_gateway_nil", fallbackGateway == nil),
				slog.String("fallback_model", fallbackModel),
				slog.Int("msg_count", len(messages)),
			)
			gateway, model := resolveTitleGateway(ctx, db, accountID, fallbackGateway, fallbackModel, auxGateway, emitDebugEvents, llmMaxResponseBytes, configLoader, byok)
			if gateway == nil {
				logTitleSummarizerDebug(ctx, "async_title_skip",
					slog.String("reason", "nil_gateway"),
					slog.String("run_id", runID.String()),
					slog.String("thread_id", threadID.String()),
					slog.String("resolved_model", model),
				)
				return
			}
			logTitleSummarizerDebug(ctx, "async_title_llm",
				slog.String("run_id", runID.String()),
				slog.String("model", model),
			)
			titleGenerator(ctx, db, rdb, bus, gateway, runID, threadID, model, messages, prompt, maxTokens)
		}()

		err = next(ctx, rc)
		if err != nil {
			return err
		}

		return err
	}
}

func resolveTitleGateway(
	ctx context.Context,
	pool TitleSummarizerDB,
	accountID *uuid.UUID,
	fallbackGateway llm.Gateway,
	fallbackModel string,
	auxGateway llm.Gateway,
	emitDebugEvents bool,
	llmMaxResponseBytes int,
	configLoader *routing.ConfigLoader,
	byokEnabled bool,
) (llm.Gateway, string) {
	// account-level override takes precedence over platform setting
	if accountID != nil {
		if gw, model, ok := resolveAccountToolGateway(ctx, pool, *accountID, auxGateway, emitDebugEvents, llmMaxResponseBytes, configLoader, byokEnabled); ok {
			logTitleSummarizerDebug(ctx, "resolve_gateway",
				slog.String("source", "account_tool"),
				slog.String("model", model),
			)
			return gw, model
		}
	}

	var selector string
	err := pool.QueryRow(ctx,
		`SELECT value FROM platform_settings WHERE key = $1`,
		settingTitleSummarizerModel,
	).Scan(&selector)
	selector = strings.TrimSpace(selector)
	if err != nil || selector == "" {
		logTitleSummarizerDebug(ctx, "resolve_gateway",
			slog.String("source", "fallback"),
			slog.String("cause", "platform_setting_missing"),
			slog.Bool("query_err", err != nil),
			slog.Bool("selector_empty", selector == ""),
		)
		return fallbackGateway, fallbackModel
	}

	if configLoader == nil {
		logTitleSummarizerDebug(ctx, "resolve_gateway",
			slog.String("source", "fallback"),
			slog.String("cause", "config_loader_nil"),
			slog.String("selector", selector),
		)
		return fallbackGateway, fallbackModel
	}
	routingCfg, err := configLoader.Load(ctx, accountID)
	if err != nil {
		slog.Warn("title_summarizer: load routing config failed", "err", err.Error())
		logTitleSummarizerDebug(ctx, "resolve_gateway",
			slog.String("source", "fallback"),
			slog.String("cause", "routing_load_err"),
			slog.String("selector", selector),
		)
		return fallbackGateway, fallbackModel
	}

	platformCfg := routingCfg.PlatformOnly()
	selected, err := resolveSelectedRouteBySelector(platformCfg, selector, map[string]any{}, byokEnabled)
	if err != nil || selected == nil {
		if err != nil {
			slog.Warn("title_summarizer: selector resolve failed", "selector", selector, "err", err.Error())
		}
		logTitleSummarizerDebug(ctx, "resolve_gateway",
			slog.String("source", "fallback"),
			slog.String("cause", "selector_unresolved"),
			slog.String("selector", selector),
			slog.Bool("resolve_err", err != nil),
			slog.Bool("selected_nil", selected == nil),
		)
		return fallbackGateway, fallbackModel
	}

	gw, err := gatewayFromSelectedRoute(*selected, auxGateway, emitDebugEvents, llmMaxResponseBytes)
	if err != nil {
		slog.Warn("title_summarizer: build gateway failed", "err", err.Error())
		logTitleSummarizerDebug(ctx, "resolve_gateway",
			slog.String("source", "fallback"),
			slog.String("cause", "build_gateway_err"),
			slog.String("selector", selector),
		)
		return fallbackGateway, fallbackModel
	}
	logTitleSummarizerDebug(ctx, "resolve_gateway",
		slog.String("source", "platform_route"),
		slog.String("model", selected.Route.Model),
		slog.String("selector", selector),
	)
	return gw, selected.Route.Model
}

func isFirstRunOfThread(ctx context.Context, pool TitleSummarizerDB, threadID uuid.UUID) (bool, error) {
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

func hasThreadTitleUpdateEvent(ctx context.Context, pool TitleSummarizerDB, runID uuid.UUID) (bool, error) {
	var count int
	err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM run_events WHERE run_id = $1 AND type = 'thread.title.updated'`,
		runID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func generateTitle(
	ctx context.Context,
	pool TitleSummarizerDB,
	rdb *redis.Client,
	bus eventbus.EventBus,
	gateway llm.Gateway,
	runID uuid.UUID,
	threadID uuid.UUID,
	model string,
	messages []llm.Message,
	prompt string,
	maxTokens int,
) {

	titleInput := buildTitleInput(messages)
	if titleInput == "" {
		logTitleSummarizerDebug(ctx, "generate_skip",
			slog.String("reason", "empty_title_input"),
			slog.String("run_id", runID.String()),
			slog.String("thread_id", threadID.String()),
		)
		return
	}
	logTitleSummarizerDebug(ctx, "generate_title_input",
		slog.String("run_id", runID.String()),
		slog.Int("title_input_runes", len([]rune(titleInput))),
	)

	req := llm.Request{
		Model: model,
		Messages: []llm.Message{
			{Role: "system", Content: []llm.TextPart{{Text: buildSummarizeSystem(prompt)}}},
			{Role: "user", Content: []llm.TextPart{{Text: titleInput}}},
		},
		Temperature:     floatPtr(titleSummarizerTemperature),
		MaxOutputTokens: &maxTokens,
		ReasoningMode:   "disabled",
	}

	var chunks []string
	var thinkingParts int
	sentinel := fmt.Errorf("done")

	err := gateway.Stream(ctx, req, func(ev llm.StreamEvent) error {
		switch typed := ev.(type) {
		case llm.StreamMessageDelta:
			if typed.ContentDelta == "" {
				return nil
			}
			if typed.Channel != nil && *typed.Channel == "thinking" {
				thinkingParts++
				return nil
			}
			chunks = append(chunks, typed.ContentDelta)
		case llm.StreamRunCompleted, llm.StreamRunFailed:
			return sentinel
		}
		return nil
	})
	if err != nil && err != sentinel {
		if ctx.Err() == context.DeadlineExceeded {
			slog.Warn("title_summarizer: timeout exceeded", "timeout", titleSummarizerTimeout)
		} else {
			slog.Warn("title_summarizer: llm call failed", "err", err.Error())
		}
		logTitleSummarizerDebug(ctx, "generate_llm_err",
			slog.String("run_id", runID.String()),
			slog.Bool("deadline", ctx.Err() == context.DeadlineExceeded),
		)
		return
	}

	title := normalizeGeneratedTitle(strings.Join(chunks, ""))
	if title == "" {
		logTitleSummarizerDebug(ctx, "generate_skip",
			slog.String("reason", "empty_model_title"),
			slog.String("run_id", runID.String()),
			slog.Int("chunks", len(chunks)),
			slog.Int("thinking_parts", thinkingParts),
		)
		return
	}
	if len([]rune(title)) > 50 {
		title = string([]rune(title)[:50])
	}
	seq, emitted, err := writeThreadTitleAndEventOnce(ctx, pool, runID, threadID, title)
	if err != nil {
		slog.Warn("title_summarizer: db write failed", "err", err.Error())
		logTitleSummarizerDebug(ctx, "thread_title_write_err",
			slog.String("run_id", runID.String()),
			slog.String("thread_id", threadID.String()),
		)
		return
	}
	if !emitted {
		logTitleSummarizerDebug(ctx, "generate_skip",
			slog.String("reason", "title_already_emitted"),
			slog.String("run_id", runID.String()),
			slog.String("thread_id", threadID.String()),
		)
		return
	}
	logTitleSummarizerDebug(ctx, "thread_title_set",
		slog.String("run_id", runID.String()),
		slog.String("thread_id", threadID.String()),
		slog.Int("title_runes", len([]rune(title))),
	)

	notifyTitleEvent(ctx, pool, rdb, bus, runID, seq)
}

func writeThreadTitleAndEventOnce(
	ctx context.Context,
	pool TitleSummarizerDB,
	runID uuid.UUID,
	threadID uuid.UUID,
	title string,
) (int64, bool, error) {
	dataJSON := map[string]any{
		"thread_id": threadID.String(),
		"title":     title,
	}
	encoded, err := json.Marshal(dataJSON)
	if err != nil {
		logTitleSummarizerDebug(ctx, "thread_title_write_err",
			slog.String("phase", "marshal"),
			slog.String("run_id", runID.String()),
			slog.String("err", err.Error()),
		)
		return 0, false, err
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		logTitleSummarizerDebug(ctx, "thread_title_write_err",
			slog.String("phase", "begin_tx"),
			slog.String("run_id", runID.String()),
			slog.String("err", err.Error()),
		)
		return 0, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err = tx.Exec(ctx, `SELECT 1 FROM runs WHERE id = $1 FOR UPDATE`, runID); err != nil {
		logTitleSummarizerDebug(ctx, "thread_title_write_err",
			slog.String("phase", "lock_run"),
			slog.String("run_id", runID.String()),
			slog.String("err", err.Error()),
		)
		return 0, false, err
	}

	var alreadyEmitted bool
	if err = tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM run_events WHERE run_id = $1 AND type = 'thread.title.updated')`,
		runID,
	).Scan(&alreadyEmitted); err != nil {
		logTitleSummarizerDebug(ctx, "thread_title_write_err",
			slog.String("phase", "dedupe"),
			slog.String("run_id", runID.String()),
			slog.String("err", err.Error()),
		)
		return 0, false, err
	}
	if alreadyEmitted {
		return 0, false, nil
	}

	if _, err = tx.Exec(ctx,
		`UPDATE threads SET title = $1 WHERE id = $2 AND deleted_at IS NULL AND title_locked = false`,
		title, threadID,
	); err != nil {
		logTitleSummarizerDebug(ctx, "thread_title_write_err",
			slog.String("phase", "update_title"),
			slog.String("run_id", runID.String()),
			slog.String("thread_id", threadID.String()),
			slog.String("err", err.Error()),
		)
		return 0, false, err
	}

	var seq int64
	if err = tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM run_events WHERE run_id = $1`,
		runID,
	).Scan(&seq); err != nil {
		logTitleSummarizerDebug(ctx, "thread_title_write_err",
			slog.String("phase", "next_seq"),
			slog.String("run_id", runID.String()),
			slog.String("err", err.Error()),
		)
		return 0, false, err
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, $2, $3, $4::jsonb)`,
		runID, seq, "thread.title.updated", string(encoded),
	)
	if err != nil {
		logTitleSummarizerDebug(ctx, "thread_title_write_err",
			slog.String("phase", "insert"),
			slog.String("run_id", runID.String()),
			slog.String("err", err.Error()),
		)
		return 0, false, err
	}

	if err = tx.Commit(ctx); err != nil {
		logTitleSummarizerDebug(ctx, "thread_title_write_err",
			slog.String("phase", "commit"),
			slog.String("run_id", runID.String()),
			slog.String("err", err.Error()),
		)
		return 0, false, err
	}

	logTitleSummarizerDebug(ctx, "thread_title_write_ok",
		slog.String("run_id", runID.String()),
		slog.String("thread_id", threadID.String()),
		slog.Int64("seq", seq),
	)
	return seq, true, nil
}

func notifyTitleEvent(
	ctx context.Context,
	pool TitleSummarizerDB,
	rdb *redis.Client,
	bus eventbus.EventBus,
	runID uuid.UUID,
	seq int64,
) {
	logTitleSummarizerDebug(ctx, "notify_title_event",
		slog.String("run_id", runID.String()),
		slog.Int64("seq", seq),
		slog.Bool("bus_nil", bus == nil),
		slog.Bool("rdb_nil", rdb == nil),
	)

	channel := fmt.Sprintf("run_events:%s", runID.String())
	if bus != nil {
		if err := bus.Publish(ctx, channel, ""); err != nil {
			slog.WarnContext(ctx, "title_event_bus_publish_failed", "channel", channel, "err", err)
		}
	} else {
		pgChannel := fmt.Sprintf(`"%s"`, channel)
		if _, err := pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgChannel, "ping"); err != nil {
			slog.WarnContext(ctx, "title_pg_notify_failed", "channel", channel, "err", err)
		}
	}
	if rdb != nil {
		rdbChannel := fmt.Sprintf("arkloop:sse:run_events:%s", runID.String())
		if _, err := rdb.Publish(ctx, rdbChannel, "ping").Result(); err != nil {
			slog.WarnContext(ctx, "title_redis_publish_failed", "channel", rdbChannel, "err", err)
		}
	}
}

func buildTitleInput(messages []llm.Message) string {
	userPrompt := buildTitleUserPrompt(messages)
	materials := buildTitleMaterials(messages, titleSummarizerMaterialsBudget)
	if userPrompt == "" && materials == "" {
		return ""
	}

	if materials == "" {
		materials = "(none)"
	}

	return "User prompt:\n" + userPrompt + "\n\nMaterials:\n" + materials
}

func buildTitleUserPrompt(messages []llm.Message) string {
	var parts []string
	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		for _, part := range msg.Content {
			if part.Kind() != "text" {
				continue
			}
			if part.Text != "" {
				parts = append(parts, part.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func buildTitleMaterials(messages []llm.Message, budget int) string {
	if budget <= 0 {
		return ""
	}

	var blocks []string
	remaining := budget
	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		for _, part := range msg.Content {
			block := titleMaterialBlock(part)
			if block == "" {
				continue
			}
			if len(blocks) > 0 {
				separator := "\n\n"
				if utf8RuneCount(separator) >= remaining {
					return strings.Join(blocks, "\n\n")
				}
				remaining -= utf8RuneCount(separator)
			}
			block = truncateRunes(block, remaining)
			if block == "" {
				return strings.Join(blocks, "\n\n")
			}
			blocks = append(blocks, block)
			remaining -= utf8RuneCount(block)
			if remaining <= 0 {
				return strings.Join(blocks, "\n\n")
			}
		}
	}
	return strings.Join(blocks, "\n\n")
}

func titleMaterialBlock(part llm.ContentPart) string {
	switch part.Kind() {
	case "file":
		name := titleAttachmentName(part, "file")
		if strings.TrimSpace(part.ExtractedText) == "" {
			return "[附件: " + name + "]"
		}
		return "[附件: " + name + "]\n" + part.ExtractedText
	case "image":
		return "[图片: " + titleAttachmentName(part, "image") + "]"
	default:
		return ""
	}
}

func titleAttachmentName(part llm.ContentPart, fallback string) string {
	if part.Attachment != nil {
		if name := strings.TrimSpace(part.Attachment.Filename); name != "" {
			return name
		}
		if mime := strings.TrimSpace(part.Attachment.MimeType); mime != "" {
			return mime
		}
	}
	return fallback
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func utf8RuneCount(value string) int {
	return len([]rune(value))
}

func normalizeGeneratedTitle(raw string) string {
	title := strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n"))
	if title == "" {
		return ""
	}
	for _, line := range strings.Split(title, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			title = line
			break
		}
	}
	for _, marker := range []string{"标题：", "标题:", "Title:", "title:"} {
		if idx := strings.LastIndex(title, marker); idx >= 0 {
			title = strings.TrimSpace(title[idx+len(marker):])
		}
	}
	title = strings.TrimSpace(strings.Trim(title, "\"'`“”‘’「」『』【】[]()<>"))
	title = strings.Join(strings.Fields(title), " ")
	title = strings.TrimRight(title, "。！？!?.,;:：；、")
	if len([]rune(title)) > 50 {
		title = string([]rune(title)[:50])
	}
	return strings.TrimSpace(title)
}

func floatPtr(v float64) *float64 {
	return &v
}

// resolveAccountToolGateway 查询账户级 spawn.profile.tool override，若存在则构建对应 gateway。
func resolveAccountToolGateway(
	ctx context.Context,
	pool CompactPersistDB,
	accountID uuid.UUID,
	auxGateway llm.Gateway,
	emitDebugEvents bool,
	llmMaxResponseBytes int,
	configLoader *routing.ConfigLoader,
	byokEnabled bool,
) (llm.Gateway, string, bool) {
	resolution, ok := resolveAccountToolRoute(ctx, pool, accountID, auxGateway, emitDebugEvents, llmMaxResponseBytes, configLoader, byokEnabled)
	if !ok {
		return nil, "", false
	}
	return resolution.Gateway, resolution.Selected.Route.Model, true
}

type accountToolRouteResolution struct {
	Selected *routing.SelectedProviderRoute
	Gateway  llm.Gateway
}

func resolveAccountToolRoute(
	ctx context.Context,
	pool CompactPersistDB,
	accountID uuid.UUID,
	auxGateway llm.Gateway,
	emitDebugEvents bool,
	llmMaxResponseBytes int,
	configLoader *routing.ConfigLoader,
	byokEnabled bool,
) (*accountToolRouteResolution, bool) {
	var selector string
	err := pool.QueryRow(ctx,
		`SELECT value FROM account_entitlement_overrides
		  WHERE account_id = $1 AND key = 'spawn.profile.tool'
		    AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
		  LIMIT 1`,
		accountID,
	).Scan(&selector)
	selector = strings.TrimSpace(selector)
	if err != nil || selector == "" {
		return nil, false
	}

	if configLoader == nil {
		return nil, false
	}
	aid := accountID
	routingCfg, err := configLoader.Load(ctx, &aid)
	if err != nil {
		slog.Warn("title_summarizer: load routing config for tool profile failed", "err", err.Error())
		return nil, false
	}

	selected, err := resolveSelectedRouteBySelector(routingCfg, selector, map[string]any{}, byokEnabled)
	if err != nil || selected == nil {
		// 精确 route 不存在时，按 credential name 查找并构造临时 route
		credName, modelName, exact := splitModelSelector(selector)
		if exact {
			if baseRoute, cred, ok := routingCfg.GetHighestPriorityRouteByCredentialName(credName, map[string]any{}); ok {
				selected = &routing.SelectedProviderRoute{
					Route:      baseRoute,
					Credential: cred,
				}
				selected.Route.Model = modelName
			}
		}
		if selected == nil {
			return nil, false
		}
	}

	gw, err := gatewayFromSelectedRoute(*selected, auxGateway, emitDebugEvents, llmMaxResponseBytes)
	if err != nil {
		slog.Warn("tool_gateway: build failed", "selector", selector, "err", err.Error())
		return nil, false
	}
	return &accountToolRouteResolution{
		Selected: selected,
		Gateway:  gw,
	}, true
}

func buildSummarizeSystem(styleHint string) string {
	base := "Generate a very short conversation title from the provided User prompt and Materials.\n" +
		"Hard rules:\n" +
		"- Output exactly one title and nothing else.\n" +
		"- Do not answer the user.\n" +
		"- Do not describe what you will do.\n" +
		"- No quotes, backticks, markdown, labels, prefixes, or trailing punctuation.\n" +
		"- Use the same language as the user's message.\n" +
		"- If the user's message is mainly Chinese, keep the title under 10 Chinese characters.\n" +
		"- Otherwise keep the title under 6 words.\n" +
		"- Prefer the concrete task or question the user asked for.\n" +
		"- Use material details when they make the title more specific.\n" +
		"- Do not ignore the Materials section.\n" +
		"- Do not turn the title into only a raw file name or domain label.\n" +
		"- Ignore formatting noise, boilerplate, and repeated content.\n" +
		"- Avoid naming only the business domain or deliverable topic when the user asked for an action."
	if styleHint != "" {
		return base + "\n\n" + styleHint
	}
	return base
}
