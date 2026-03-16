package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"arkloop/services/shared/eventbus"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const titleSummarizerTimeout = 30 * time.Second

const settingTitleSummarizerModel = "title_summarizer.model"

func NewTitleSummarizerMiddleware(pool *pgxpool.Pool, rdb *redis.Client, stubGateway llm.Gateway, emitDebugEvents bool, loaders ...*routing.ConfigLoader) RunMiddleware {
	var configLoader *routing.ConfigLoader
	if len(loaders) > 0 {
		configLoader = loaders[0]
	}
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc.TitleSummarizer == nil || pool == nil {
			return next(ctx, rc)
		}

		threadID := rc.Run.ThreadID
		accountID := &rc.Run.AccountID
		firstRun, err := isFirstRunOfThread(ctx, pool, threadID)
		if err != nil {
			slog.WarnContext(ctx, "title_summarizer: check failed", "err", err.Error())
			return next(ctx, rc)
		}
		if !firstRun {
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
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), titleSummarizerTimeout)
			defer cancel()
			gateway, model := resolveTitleGateway(ctx, pool, accountID, fallbackGateway, fallbackModel, stubGateway, emitDebugEvents, llmMaxResponseBytes, configLoader)
			if gateway == nil {
				return
			}
			generateTitle(ctx, pool, rdb, bus, gateway, runID, threadID, model, messages, prompt, maxTokens)
		}()

		return next(ctx, rc)
	}
}

func resolveTitleGateway(
	ctx context.Context,
	pool *pgxpool.Pool,
	accountID *uuid.UUID,
	fallbackGateway llm.Gateway,
	fallbackModel string,
	stubGateway llm.Gateway,
	emitDebugEvents bool,
	llmMaxResponseBytes int,
	configLoader *routing.ConfigLoader,
) (llm.Gateway, string) {
	var selector string
	err := pool.QueryRow(ctx,
		`SELECT value FROM platform_settings WHERE key = $1`,
		settingTitleSummarizerModel,
	).Scan(&selector)
	selector = strings.TrimSpace(selector)
	if err != nil || selector == "" {
		return fallbackGateway, fallbackModel
	}

	if configLoader == nil {
		return fallbackGateway, fallbackModel
	}
	routingCfg, err := configLoader.Load(ctx, nil)
	if err != nil {
		slog.Warn("title_summarizer: load routing config failed", "err", err.Error())
		return fallbackGateway, fallbackModel
	}

	selected, err := resolveSelectedRouteBySelector(routingCfg, selector, map[string]any{}, true)
	if err != nil || selected == nil {
		if err != nil {
			slog.Warn("title_summarizer: selector resolve failed", "selector", selector, "err", err.Error())
		}
		return fallbackGateway, fallbackModel
	}

	gw, err := gatewayFromSelectedRoute(*selected, stubGateway, emitDebugEvents, llmMaxResponseBytes)
	if err != nil {
		slog.Warn("title_summarizer: build gateway failed", "err", err.Error())
		return fallbackGateway, fallbackModel
	}
	return gw, selected.Route.Model
}

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
	ctx context.Context,
	pool *pgxpool.Pool,
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
		return
	}

	req := llm.Request{
		Model: model,
		Messages: []llm.Message{
			{Role: "system", Content: []llm.TextPart{{Text: buildSummarizeSystem(prompt)}}},
			{Role: "user", Content: []llm.TextPart{{Text: userText}}},
		},
		MaxOutputTokens: &maxTokens,
	}

	var chunks []string
	sentinel := fmt.Errorf("done")

	err := gateway.Stream(ctx, req, func(ev llm.StreamEvent) error {
		switch typed := ev.(type) {
		case llm.StreamMessageDelta:
			if typed.Channel != nil && *typed.Channel == "thinking" {
				return nil
			}
			if typed.ContentDelta != "" {
				chunks = append(chunks, typed.ContentDelta)
			}
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

	emitTitleEvent(ctx, pool, rdb, bus, runID, threadID, title)
}

func emitTitleEvent(
	ctx context.Context,
	pool *pgxpool.Pool,
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
		return
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)

	var seq int64
	if _, err = tx.Exec(ctx, `SELECT 1 FROM runs WHERE id = $1 FOR UPDATE`, runID); err != nil {
		return
	}
	if err = tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM run_events WHERE run_id = $1`,
		runID,
	).Scan(&seq); err != nil {
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

func buildSummarizeSystem(styleHint string) string {
	base := "Generate a concise title for the conversation. Output ONLY the title text — no quotes, no punctuation at the end, no explanation, no prefix like 'Title:'. The title must be in the same language as the user's message."
	if styleHint != "" {
		return base + "\n\n" + styleHint
	}
	return base
}
