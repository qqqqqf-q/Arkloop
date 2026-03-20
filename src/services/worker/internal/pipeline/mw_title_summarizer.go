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

// titleSummarizerDB 由 *pgxpool.Pool 与 desktop 的 data.DesktopDB 实现。
type titleSummarizerDB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

const titleSummarizerTimeout = 30 * time.Second

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

func NewTitleSummarizerMiddleware(db titleSummarizerDB, rdb *redis.Client, stubGateway llm.Gateway, emitDebugEvents bool, loaders ...*routing.ConfigLoader) RunMiddleware {
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

		fallbackGateway := rc.Gateway
		fallbackModel := ""
		if rc.SelectedRoute != nil {
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
			gateway, model := resolveTitleGateway(ctx, db, accountID, fallbackGateway, fallbackModel, stubGateway, emitDebugEvents, llmMaxResponseBytes, configLoader, byok)
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
			generateTitle(ctx, db, rdb, bus, gateway, runID, threadID, model, messages, prompt, maxTokens)
		}()

		return next(ctx, rc)
	}
}

func resolveTitleGateway(
	ctx context.Context,
	pool titleSummarizerDB,
	accountID *uuid.UUID,
	fallbackGateway llm.Gateway,
	fallbackModel string,
	stubGateway llm.Gateway,
	emitDebugEvents bool,
	llmMaxResponseBytes int,
	configLoader *routing.ConfigLoader,
	byokEnabled bool,
) (llm.Gateway, string) {
	// account-level override takes precedence over platform setting
	if accountID != nil {
		if gw, model, ok := resolveAccountToolGateway(ctx, pool, *accountID, stubGateway, emitDebugEvents, llmMaxResponseBytes, configLoader, byokEnabled); ok {
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

	gw, err := gatewayFromSelectedRoute(*selected, stubGateway, emitDebugEvents, llmMaxResponseBytes)
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

func isFirstRunOfThread(ctx context.Context, pool titleSummarizerDB, threadID uuid.UUID) (bool, error) {
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
	ctx context.Context,
	pool titleSummarizerDB,
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

	userText := extractUserText(messages)
	if userText == "" {
		logTitleSummarizerDebug(ctx, "generate_skip",
			slog.String("reason", "empty_user_text"),
			slog.String("run_id", runID.String()),
			slog.String("thread_id", threadID.String()),
		)
		return
	}
	logTitleSummarizerDebug(ctx, "generate_user_text",
		slog.String("run_id", runID.String()),
		slog.Int("user_text_runes", len([]rune(userText))),
	)

	req := llm.Request{
		Model: model,
		Messages: []llm.Message{
			{Role: "system", Content: []llm.TextPart{{Text: buildSummarizeSystem(prompt)}}},
			{Role: "user", Content: []llm.TextPart{{Text: userText}}},
		},
		MaxOutputTokens: &maxTokens,
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

	title := strings.TrimSpace(strings.Join(chunks, ""))
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

	_, err = pool.Exec(ctx,
		`UPDATE threads SET title = $1 WHERE id = $2 AND deleted_at IS NULL AND title_locked = false`,
		title, threadID,
	)
	if err != nil {
		slog.Warn("title_summarizer: db update failed", "err", err.Error())
		logTitleSummarizerDebug(ctx, "thread_update_err",
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

	emitTitleEvent(ctx, pool, rdb, bus, runID, threadID, title)
}

func emitTitleEvent(
	ctx context.Context,
	pool titleSummarizerDB,
	rdb *redis.Client,
	bus eventbus.EventBus,
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
		logTitleSummarizerDebug(ctx, "emit_title_event_err",
			slog.String("phase", "marshal"),
			slog.String("run_id", runID.String()),
			slog.String("err", err.Error()),
		)
		return
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		logTitleSummarizerDebug(ctx, "emit_title_event_err",
			slog.String("phase", "begin_tx"),
			slog.String("run_id", runID.String()),
			slog.String("err", err.Error()),
		)
		return
	}
	defer tx.Rollback(ctx)

	var seq int64
	if _, err = tx.Exec(ctx, `SELECT 1 FROM runs WHERE id = $1 FOR UPDATE`, runID); err != nil {
		logTitleSummarizerDebug(ctx, "emit_title_event_err",
			slog.String("phase", "lock_run"),
			slog.String("run_id", runID.String()),
			slog.String("err", err.Error()),
		)
		return
	}
	if err = tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM run_events WHERE run_id = $1`,
		runID,
	).Scan(&seq); err != nil {
		logTitleSummarizerDebug(ctx, "emit_title_event_err",
			slog.String("phase", "next_seq"),
			slog.String("run_id", runID.String()),
			slog.String("err", err.Error()),
		)
		return
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO run_events (run_id, seq, type, data_json) VALUES ($1, $2, $3, $4::jsonb)`,
		runID, seq, "thread.title.updated", string(encoded),
	)
	if err != nil {
		logTitleSummarizerDebug(ctx, "emit_title_event_err",
			slog.String("phase", "insert"),
			slog.String("run_id", runID.String()),
			slog.String("err", err.Error()),
		)
		return
	}

	if err = tx.Commit(ctx); err != nil {
		logTitleSummarizerDebug(ctx, "emit_title_event_err",
			slog.String("phase", "commit"),
			slog.String("run_id", runID.String()),
			slog.String("err", err.Error()),
		)
		return
	}

	logTitleSummarizerDebug(ctx, "emit_title_event_ok",
		slog.String("run_id", runID.String()),
		slog.String("thread_id", threadID.String()),
		slog.Int64("seq", seq),
		slog.Bool("bus_nil", bus == nil),
		slog.Bool("rdb_nil", rdb == nil),
	)

	channel := fmt.Sprintf("run_events:%s", runID.String())
	if bus != nil {
		_ = bus.Publish(ctx, channel, "")
	} else {
		pgChannel := fmt.Sprintf(`"%s"`, channel)
		_, _ = pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgChannel, "ping")
	}
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

// resolveAccountToolGateway 查询账户级 spawn.profile.tool override，若存在则构建对应 gateway。
func resolveAccountToolGateway(
	ctx context.Context,
	pool CompactPersistDB,
	accountID uuid.UUID,
	stubGateway llm.Gateway,
	emitDebugEvents bool,
	llmMaxResponseBytes int,
	configLoader *routing.ConfigLoader,
	byokEnabled bool,
) (llm.Gateway, string, bool) {
	var selector string
	err := pool.QueryRow(ctx,
		`SELECT value FROM account_entitlement_overrides
		  WHERE account_id = $1 AND key = 'spawn.profile.tool'
		    AND (expires_at IS NULL OR expires_at > NOW())
		  LIMIT 1`,
		accountID,
	).Scan(&selector)
	selector = strings.TrimSpace(selector)
	if err != nil || selector == "" {
		return nil, "", false
	}

	if configLoader == nil {
		return nil, "", false
	}
	aid := accountID
	routingCfg, err := configLoader.Load(ctx, &aid)
	if err != nil {
		slog.Warn("title_summarizer: load routing config for tool profile failed", "err", err.Error())
		return nil, "", false
	}

	selected, err := resolveSelectedRouteBySelector(routingCfg, selector, map[string]any{}, byokEnabled)
	if err != nil || selected == nil {
		if err != nil {
			slog.Warn("title_summarizer: tool profile selector resolve failed", "selector", selector, "err", err.Error())
		}
		return nil, "", false
	}

	gw, err := gatewayFromSelectedRoute(*selected, stubGateway, emitDebugEvents, llmMaxResponseBytes)
	if err != nil {
		slog.Warn("title_summarizer: tool profile build gateway failed", "err", err.Error())
		return nil, "", false
	}
	return gw, selected.Route.Model, true
}

func buildSummarizeSystem(styleHint string) string {
	base := "Summarize the user's request: what they want you to do in this chat (the ask), not the body of pasted specs, code, HTML, or long quotations, and not the thematic subject of a deliverable when they asked you to build or write something.\n" +
		"Generate a concise title for the conversation. Output ONLY the title text — no quotes, no punctuation at the end, no explanation, no prefix like 'Title:'. The title must be in the same language as the user's message."
	if styleHint != "" {
		return base + "\n\n" + styleHint
	}
	return base
}
