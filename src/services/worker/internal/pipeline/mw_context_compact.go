package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pkoukk/tiktoken-go"
)

const (
	settingContextCompactionModel     = "context.compaction.model"
	contextCompactStreamTimeout       = 60 * time.Second
	contextCompactMaxOut              = 2048
	defaultPersistTriggerApproxTokens = 120000
	defaultPersistKeepLastMessages    = 40
)

const contextCompactSystemPrompt = `You compress prior conversation turns for model context. Keep: decisions, constraints, file paths, errors, open tasks. Omit small talk. Reply with summary prose only, no preamble.`

var errContextCompactStreamDone = errors.New("context_compact_stream_done")

// NewContextCompactMiddleware 在 TitleSummarizer 之后运行：可选将头部区间摘要持久化，再按预算裁切消息。
func NewContextCompactMiddleware(
	pool CompactPersistDB,
	messagesRepo data.MessagesRepository,
	eventsRepo CompactRunEventAppender,
	stubGateway llm.Gateway,
	emitDebugEvents bool,
	loaders ...*routing.ConfigLoader,
) RunMiddleware {
	var configLoader *routing.ConfigLoader
	if len(loaders) > 0 {
		configLoader = loaders[0]
	}
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		cfg := rc.ContextCompact
		if !cfg.Enabled && !cfg.PersistEnabled {
			return next(ctx, rc)
		}

		if cfg.Enabled && !ContextCompactHasActiveBudget(cfg) {
			cfg.MaxUserMessageTokens = 20000
			rc.ContextCompact = cfg
		}

		var enc *tiktoken.Tiktoken
		if rc.SelectedRoute != nil {
			if tke, encErr := ResolveTiktokenForRoute(rc.SelectedRoute); encErr != nil {
				slog.WarnContext(ctx, "context_compact", "phase", "tiktoken_route", "err", encErr.Error(), "run_id", rc.Run.ID.String())
			} else {
				enc = tke
			}
		}
		if enc == nil {
			enc, _ = tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
		}

		beforeN := len(rc.Messages)
		msgs := rc.Messages
		ids := rc.ThreadMessageIDs
		persistSplit := 0

		if cfg.PersistEnabled && pool != nil && rc.Gateway != nil && len(msgs) > 1 {
			window := 0
			if rc.SelectedRoute != nil {
				window = routing.RouteContextWindowTokens(rc.SelectedRoute.Route)
			}
			trigger, window := compactPersistTriggerTokens(cfg, window)
			keep := cfg.PersistKeepLastMessages
			if keep <= 0 {
				keep = defaultPersistKeepLastMessages
			}
			// 配置的「保留最近 N 条」在总条数不足时按实际条数折算，否则 N≥len 时永远无法触发 persist。
			tailKeep := keep
			if tailKeep >= len(msgs) {
				tailKeep = len(msgs) - 1
			}
			if tailKeep < 1 {
				tailKeep = 1
			}
			histTok := HistoryThreadPromptTokens(enc, msgs)
			if histTok >= trigger && len(ids) == len(msgs) {
				split := stabilizeCompactStart(msgs, len(msgs)-tailKeep, 0)
				if split > 0 {
					gw, model := resolveCompactionGateway(ctx, pool, rc, stubGateway, emitDebugEvents, configLoader)
					if gw == nil {
						slog.WarnContext(ctx, "context_compact", "phase", "gateway_nil", "run_id", rc.Run.ID.String())
					} else {
						summary, sumErr := runContextCompactLLM(ctx, gw, model, msgs[:split])
						if sumErr != nil {
							slog.WarnContext(ctx, "context_compact", "phase", "llm", "err", sumErr.Error(), "run_id", rc.Run.ID.String())
						} else if strings.TrimSpace(summary) != "" {
							tx, txErr := pool.BeginTx(ctx, pgx.TxOptions{})
							if txErr != nil {
								slog.WarnContext(ctx, "context_compact", "phase", "tx_begin", "err", txErr.Error(), "run_id", rc.Run.ID.String())
							} else {
								prefixIDs := append([]uuid.UUID(nil), ids[:split]...)
								if err := messagesRepo.MarkThreadMessagesCompacted(ctx, tx, rc.Run.AccountID, rc.Run.ThreadID, prefixIDs); err != nil {
									_ = tx.Rollback(ctx)
									slog.WarnContext(ctx, "context_compact", "phase", "mark_compacted", "err", err.Error(), "run_id", rc.Run.ID.String())
								} else {
									meta, _ := json.Marshal(map[string]string{"kind": "compact_summary"})
									summaryID, insErr := messagesRepo.InsertCompactSummaryMessage(ctx, tx, rc.Run.AccountID, rc.Run.ThreadID, strings.TrimSpace(summary), meta)
									if insErr != nil {
										_ = tx.Rollback(ctx)
										slog.WarnContext(ctx, "context_compact", "phase", "insert_summary", "err", insErr.Error(), "run_id", rc.Run.ID.String())
									} else if eventsRepo != nil {
										ev := rc.Emitter.Emit("run.context_compact", map[string]any{
											"op":                     "persist",
											"persist_split":          split,
											"messages_before":        beforeN,
											"thread_tokens_tiktoken": histTok,
											"context_window_tokens":  window,
											"trigger_tokens":         trigger,
											"trigger_context_pct":    cfg.PersistTriggerContextPct,
											"tail_keep_configured":   keep,
											"tail_keep_effective":    tailKeep,
										}, nil, nil)
										if _, evErr := eventsRepo.AppendRunEvent(ctx, tx, rc.Run.ID, ev); evErr != nil {
											_ = tx.Rollback(ctx)
											slog.WarnContext(ctx, "context_compact", "phase", "run_event", "err", evErr.Error(), "run_id", rc.Run.ID.String())
										} else if err := tx.Commit(ctx); err != nil {
											slog.WarnContext(ctx, "context_compact", "phase", "tx_commit", "err", err.Error(), "run_id", rc.Run.ID.String())
										} else {
											persistSplit = split
											summaryMsg := llm.Message{
												Role:    "system",
												Content: []llm.TextPart{{Text: strings.TrimSpace(summary)}},
											}
											tail := make([]llm.Message, len(msgs)-split)
											copy(tail, msgs[split:])
											tailIDs := append([]uuid.UUID{summaryID}, ids[split:]...)
											msgs = append([]llm.Message{summaryMsg}, tail...)
											ids = tailIDs
											rc.Messages = msgs
											rc.ThreadMessageIDs = ids
										}
									} else if err := tx.Commit(ctx); err != nil {
										slog.WarnContext(ctx, "context_compact", "phase", "tx_commit", "err", err.Error(), "run_id", rc.Run.ID.String())
									} else {
										persistSplit = split
										summaryMsg := llm.Message{
											Role:    "system",
											Content: []llm.TextPart{{Text: strings.TrimSpace(summary)}},
										}
										tail := make([]llm.Message, len(msgs)-split)
										copy(tail, msgs[split:])
										tailIDs := append([]uuid.UUID{summaryID}, ids[split:]...)
										msgs = append([]llm.Message{summaryMsg}, tail...)
										ids = tailIDs
										rc.Messages = msgs
										rc.ThreadMessageIDs = ids
									}
								}
							}
						}
					}
				}
			}
		}

		if cfg.Enabled && ContextCompactHasActiveBudget(cfg) {
			beforeTrim := len(rc.Messages)
			beforeTrimTok := HistoryThreadPromptTokens(enc, rc.Messages)
			out, outIDs, dropped := CompactThreadMessages(rc.Messages, rc.ThreadMessageIDs, cfg, enc)
			rc.Messages = out
			rc.ThreadMessageIDs = outIDs
			if dropped > 0 || len(out) != beforeTrim {
				slog.InfoContext(ctx, "context_compact",
					"run_id", rc.Run.ID.String(),
					"thread_id", rc.Run.ThreadID.String(),
					"phase", "trim",
					"dropped_prefix", dropped,
					"after", len(out),
				)
				if eventsRepo != nil && pool != nil {
					tx, txErr := pool.BeginTx(ctx, pgx.TxOptions{})
					if txErr != nil {
						slog.WarnContext(ctx, "context_compact", "phase", "tx_begin_trim", "err", txErr.Error(), "run_id", rc.Run.ID.String())
					} else {
						ev := rc.Emitter.Emit("run.context_compact", map[string]any{
							"op":                     "trim",
							"dropped_prefix":         dropped,
							"messages_before":        beforeTrim,
							"messages_after":         len(out),
							"thread_tokens_tiktoken_before": beforeTrimTok,
							"thread_tokens_tiktoken_after":  HistoryThreadPromptTokens(enc, out),
						}, nil, nil)
						if _, evErr := eventsRepo.AppendRunEvent(ctx, tx, rc.Run.ID, ev); evErr != nil {
							_ = tx.Rollback(ctx)
							slog.WarnContext(ctx, "context_compact", "phase", "run_event_trim", "err", evErr.Error(), "run_id", rc.Run.ID.String())
						} else if err := tx.Commit(ctx); err != nil {
							_ = tx.Rollback(ctx)
							slog.WarnContext(ctx, "context_compact", "phase", "tx_commit_trim", "err", err.Error(), "run_id", rc.Run.ID.String())
						}
					}
				}
			}
		}

		if persistSplit > 0 {
			slog.InfoContext(ctx, "context_compact",
				"run_id", rc.Run.ID.String(),
				"thread_id", rc.Run.ThreadID.String(),
				"phase", "persist",
				"persist_split", persistSplit,
				"before", beforeN,
				"after", len(rc.Messages),
			)
		}

		return next(ctx, rc)
	}
}

func resolveCompactionGateway(
	ctx context.Context,
	pool CompactPersistDB,
	rc *RunContext,
	stubGateway llm.Gateway,
	emitDebugEvents bool,
	configLoader *routing.ConfigLoader,
) (llm.Gateway, string) {
	fallbackGateway := rc.Gateway
	fallbackModel := ""
	if rc.SelectedRoute != nil {
		fallbackModel = rc.SelectedRoute.Route.Model
	}
	accountID := &rc.Run.AccountID
	if accountID != nil && pool != nil {
		if gw, model, ok := resolveAccountToolGateway(ctx, pool, *accountID, stubGateway, emitDebugEvents, rc.LlmMaxResponseBytes, configLoader); ok {
			fallbackGateway = gw
			fallbackModel = model
		}
	}

	var selector string
	err := pool.QueryRow(ctx,
		`SELECT value FROM platform_settings WHERE key = $1`,
		settingContextCompactionModel,
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
		slog.WarnContext(ctx, "context_compact", "phase", "routing_load", "err", err.Error())
		return fallbackGateway, fallbackModel
	}
	selected, err := resolveSelectedRouteBySelector(routingCfg, selector, map[string]any{}, true)
	if err != nil || selected == nil {
		if err != nil {
			slog.WarnContext(ctx, "context_compact", "phase", "selector", "selector", selector, "err", err.Error())
		}
		return fallbackGateway, fallbackModel
	}
	gw, err := gatewayFromSelectedRoute(*selected, stubGateway, emitDebugEvents, rc.LlmMaxResponseBytes)
	if err != nil {
		slog.WarnContext(ctx, "context_compact", "phase", "gateway_build", "err", err.Error())
		return fallbackGateway, fallbackModel
	}
	return gw, selected.Route.Model
}

func compactPersistTriggerTokens(cfg ContextCompactSettings, windowFromRoute int) (trigger int, window int) {
	window = windowFromRoute
	if window <= 0 {
		window = cfg.FallbackContextWindowTokens
	}
	pct := cfg.PersistTriggerContextPct
	if pct > 100 {
		pct = 100
	}
	if pct > 0 && window > 0 {
		trigger = window * pct / 100
		if trigger < 1 {
			trigger = 1
		}
		return trigger, window
	}
	trigger = cfg.PersistTriggerApproxTokens
	if trigger <= 0 {
		trigger = defaultPersistTriggerApproxTokens
	}
	return trigger, window
}

func runContextCompactLLM(ctx context.Context, gateway llm.Gateway, model string, prefix []llm.Message) (string, error) {
	if gateway == nil || strings.TrimSpace(model) == "" {
		return "", fmt.Errorf("gateway or model missing")
	}
	var sb strings.Builder
	for _, m := range prefix {
		sb.WriteString(m.Role)
		sb.WriteString(":\n")
		sb.WriteString(messageText(m))
		sb.WriteString("\n\n")
	}
	userBlock := strings.TrimSpace(sb.String())
	if userBlock == "" {
		return "", nil
	}
	maxTok := contextCompactMaxOut
	req := llm.Request{
		Model: model,
		Messages: []llm.Message{
			{Role: "system", Content: []llm.TextPart{{Text: contextCompactSystemPrompt}}},
			{Role: "user", Content: []llm.TextPart{{Text: userBlock}}},
		},
		MaxOutputTokens: &maxTok,
	}
	streamCtx, cancel := context.WithTimeout(ctx, contextCompactStreamTimeout)
	defer cancel()

	var chunks []string
	err := gateway.Stream(streamCtx, req, func(ev llm.StreamEvent) error {
		switch typed := ev.(type) {
		case llm.StreamMessageDelta:
			if typed.Channel != nil && *typed.Channel == "thinking" {
				return nil
			}
			if typed.ContentDelta != "" {
				chunks = append(chunks, typed.ContentDelta)
			}
		case llm.StreamRunCompleted:
			return errContextCompactStreamDone
		case llm.StreamRunFailed:
			return fmt.Errorf("stream failed: %s", typed.Error.Message)
		}
		return nil
	})
	if err != nil && !errors.Is(err, errContextCompactStreamDone) {
		return "", err
	}
	return strings.TrimSpace(strings.Join(chunks, "")), nil
}
