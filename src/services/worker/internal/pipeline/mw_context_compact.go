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
	settingContextCompactionModel  = "context.compaction.model"
	contextCompactStreamTimeout    = 60 * time.Second
	contextCompactMaxOut           = 4096
	contextCompactGroupMaxOut      = 8192
	contextCompactPostWriteTimeout = 30 * time.Second
	defaultPersistKeepLastMessages = 40
	// 发往压缩摘要 LLM 的用户块上限（tiktoken 用 HistoryThreadPromptTokens；单条超大时再按 rune 截断）。
	contextCompactMaxLLMInputTokens = 120000
	contextCompactMaxLLMInputRunes  = 400000
	// 快速裁切：已有 snapshot 且待压缩前缀消息不超过此数量时，跳过 LLM 直接复用已有摘要
	fastCompactMaxPrefixMessages = 4
)

const contextCompactSystemPrompt = `You are a context summarization assistant. Your task is to read a conversation between a user and an AI coding assistant, then produce a structured summary following the exact format specified.

Do NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the structured summary.`

const contextCompactInitialPrompt = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

Use this EXACT format:

## Goal
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

const contextCompactUpdatePrompt = `The messages above are NEW conversation messages to incorporate into the existing summary provided in <previous-summary> tags.

Update the existing structured summary with new information. RULES:
- PRESERVE all existing information from the previous summary
- ADD new progress, decisions, and context from the new messages
- UPDATE the Progress section: move items from "In Progress" to "Done" when completed
- UPDATE "Next Steps" based on what was accomplished
- PRESERVE exact file paths, function names, and error messages
- If something is no longer relevant, you may remove it

Use this EXACT format:

## Goal
[Preserve existing goals, add new ones if the task expanded]

## Constraints & Preferences
- [Preserve existing, add new ones discovered]

## Progress
### Done
- [x] [Include previously done items AND newly completed items]

### In Progress
- [ ] [Current work - update based on progress]

### Blocked
- [Current blockers - remove if resolved]

## Key Decisions
- **[Decision]**: [Brief rationale] (preserve all previous, add new)

## Next Steps
1. [Update based on current state]

## Critical Context
- [Preserve important context, add new if needed]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

const contextCompactGroupSystemPrompt = `You are a context summarization assistant for a group chat. Your task is to read a multi-participant group conversation and produce a structured summary following the exact format specified.

Do NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the structured summary.`

const contextCompactGroupInitialPrompt = `The messages above are from a multi-participant group chat to summarize. Create a structured group chat summary that another LLM joining the conversation will use to understand context.

CRITICAL: Write the summary in the SAME language the group chat primarily uses. If the conversation is in Chinese, write the summary in Chinese. If in English, write in English. Match the group's dominant language.

Use this EXACT format:

## Topics
[What topics are being discussed? List each distinct topic or thread of discussion.]

## Participants
- [Name/handle]: [Their role, stance, or typical contributions in this conversation]

## Key Points
- [Important information, opinions, decisions, or events mentioned]
- [Include any shared links, data, or references]

## Mood & Context
[Current atmosphere of the group — casual, heated, collaborative, etc. What direction is the conversation heading?]

## Notable Details
- [Specific facts, numbers, names, or quotes worth preserving]
- [Or "(none)" if not applicable]

## Assistant Voice
[Quote the assistant's 2-3 most recent replies VERBATIM (the full message text). If the assistant has not spoken in this segment, write "(no assistant messages in this segment)".]

Keep each section concise. Preserve usernames, links, and specific data exactly as stated.`

const contextCompactGroupUpdatePrompt = `The messages above are NEW group chat messages to incorporate into the existing summary provided in <previous-summary> tags.

Update the existing structured summary with new information. RULES:
- PRESERVE key information from the previous summary
- CONDENSE older information: merge related topics, remove concluded discussions, shorten participant descriptions for inactive participants
- ADD new topics, participants, and key points from the new messages
- UPDATE participant descriptions if their stance or activity changed
- UPDATE "Mood & Context" to reflect the latest conversation direction
- MERGE or remove topics that have concluded or are no longer relevant
- PRESERVE usernames, links, and specific data exactly as stated
- UPDATE "Assistant Voice" with the assistant's latest verbatim replies from the new messages; keep only the 2-3 most recent
- Write the summary in the SAME language the group chat primarily uses
- The summary MUST NOT exceed approximately 8000 tokens (~32000 characters). If approaching this limit, aggressively condense older sections to make room for new information

Use this EXACT format:

## Topics
[Preserve ongoing topics, add new ones, mark concluded ones if needed]

## Participants
- [Preserve existing, add new participants, update descriptions]

## Key Points
- [Preserve existing important points, add new ones]

## Mood & Context
[Update to reflect the latest group atmosphere and conversation direction]

## Notable Details
- [Preserve important details, add new ones]

## Assistant Voice
[Quote the assistant's 2-3 most recent replies VERBATIM. Replace older quotes with newer ones from the new messages. If no new assistant messages, preserve the previous quotes.]

Keep each section concise. Preserve usernames, links, and specific data exactly as stated.`

var errContextCompactStreamDone = errors.New("context_compact_stream_done")

// NewContextCompactMiddleware 在 TitleSummarizer 之后运行：可选将头部区间摘要持久化，再按预算裁切消息。
func NewContextCompactMiddleware(
	pool CompactPersistDB,
	messagesRepo data.MessagesRepository,
	eventsRepo CompactRunEventAppender,
	auxGateway llm.Gateway,
	emitDebugEvents bool,
	loaders ...*routing.ConfigLoader,
) RunMiddleware {
	var configLoader *routing.ConfigLoader
	if len(loaders) > 0 {
		configLoader = loaders[0]
	}
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		beforeMsgs := append([]llm.Message(nil), rc.Messages...)
		cfg := rc.ContextCompact
		if !cfg.Enabled && !cfg.PersistEnabled {
			beforeTokens := traceContextCompactTokens(nil, rc.SystemPrompt, beforeMsgs)
			emitTraceEvent(rc, "context_compact", "context_compact.completed", map[string]any{
				"compacted":     false,
				"tokens_before": beforeTokens,
				"tokens_after":  beforeTokens,
			})
			return next(ctx, rc)
		}

		// 群聊 compact 已在 GroupContextTrim 中独立处理，此处 skip persist 避免重复。
		isGroupChat := rc.ChannelContext != nil && IsTelegramGroupLikeConversation(rc.ChannelContext.ConversationType)

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
		beforeTokens := traceContextCompactTokens(enc, rc.SystemPrompt, beforeMsgs)

		if cfg.MicrocompactKeepRecentTools > 0 {
			rc.Messages = microcompactToolResults(rc.Messages, cfg.MicrocompactKeepRecentTools)
		}

		beforeN := len(rc.Messages)
		msgs := rc.Messages
		ids := rc.ThreadMessageIDs
		previousSummary := strings.TrimSpace(rc.ActiveCompactSnapshotText)
		fixedPrefixCount := 0
		if rc.HasActiveCompactSnapshot && len(msgs) > 0 {
			fixedPrefixCount = 1
		}
		persistSplit := 0
		persistFastPath := false
		var persistPrefixIDs []uuid.UUID
		var persistSummary string
		var persistStartedEvent map[string]any
		var persistFailedEvent map[string]any
		var persistCompletedEvent map[string]any

		if cfg.PersistEnabled && !isGroupChat && pool != nil && rc.Gateway != nil && len(msgs[fixedPrefixCount:]) > 1 {
			window := 0
			if rc.SelectedRoute != nil {
				window = routing.RouteContextWindowTokens(rc.SelectedRoute.Route)
			}
			trigger, window := compactPersistTriggerTokens(cfg, window)
			keep := cfg.PersistKeepLastMessages
			if keep <= 0 {
				keep = defaultPersistKeepLastMessages
			}
			requestEstimate := HistoryThreadPromptTokens(enc, contextCompactRequestMessages(rc.SystemPrompt, msgs))
			anchor, anchored := resolveContextCompactPressureAnchor(ctx, pool, rc)
			if anchored {
				rc.SetContextCompactPressureAnchor(anchor.LastRealPromptTokens, anchor.LastRequestContextEstimateTokens)
			}
			pressure := ComputeContextCompactPressure(requestEstimate, func() *ContextCompactPressureAnchor {
				if !anchored {
					return nil
				}
				return &anchor
			}())
			if pressure.ContextPressureTokens >= trigger && len(ids) == len(msgs) {
				// 断路器：连续失败过多则跳过 persist
				if pool != nil && compactConsecutiveFailures(ctx, pool, rc.Run.AccountID, rc.Run.ThreadID) >= maxConsecutiveCompactFailures {
					slog.WarnContext(ctx, "context_compact", "phase", "circuit_breaker", "run_id", rc.Run.ID.String(), "thread_id", rc.Run.ThreadID.String())
					persistStartedEvent = map[string]any{
						"op":    "persist",
						"phase": "circuit_breaker",
					}
					ApplyContextCompactPressureFields(persistStartedEvent, pressure)
				} else {
					compactBase := msgs[fixedPrefixCount:]
					compactBaseIDs := ids[fixedPrefixCount:]
					var tailKeep int
					tailPct := cfg.PersistKeepTailPct
					if tailPct > 100 {
						tailPct = 100
					}
					if tailPct > 0 && window > 0 {
						tailTokenBudget := window * tailPct / 100
						tailKeep = computeTailKeepByTokenBudget(enc, compactBase, tailTokenBudget, keep)
					} else {
						tailKeep = keep
					}
					if tailKeep >= len(compactBase) {
						tailKeep = len(compactBase) - 1
					}
					if tailKeep < 1 {
						tailKeep = 1
					}
					split := stabilizeCompactStart(compactBase, len(compactBase)-tailKeep, 0)
					split = ensureToolPairIntegrity(compactBase, split)
					if split > 0 {
						if rc.HasActiveCompactSnapshot && split <= fastCompactMaxPrefixMessages && previousSummary != "" {
							// 快速裁切：前缀少且已有 snapshot，跳过 LLM 复用已有摘要
							persistFastPath = true
							persistStartedEvent = map[string]any{
								"op":                    "persist",
								"phase":                 "started",
								"persist_split":         split,
								"trigger_tokens":        trigger,
								"context_window_tokens": window,
								"trigger_context_pct":   cfg.PersistTriggerContextPct,
								"tail_keep_effective":   tailKeep,
								"fast_path":             true,
							}
							ApplyContextCompactPressureFields(persistStartedEvent, pressure)
							persistSplit = split
							persistSummary = previousSummary
							persistPrefixIDs = append([]uuid.UUID(nil), filterNonNilUUIDs(compactBaseIDs[:split])...)
							persistCompletedEvent = map[string]any{
								"op":                    "persist",
								"phase":                 "completed",
								"persist_split":         split,
								"messages_before":       beforeN,
								"context_window_tokens": window,
								"fast_path":             true,
								"tail_keep_effective":   tailKeep,
							}
							ApplyContextCompactPressureFields(persistCompletedEvent, pressure)
							tail := make([]llm.Message, len(compactBase)-split)
							copy(tail, compactBase[split:])
							tail = truncateLargeTailMessages(enc, tail)
							msgs = append([]llm.Message{makeCompactSnapshotMessage(persistSummary)}, tail...)
							ids = append([]uuid.UUID{uuid.Nil}, compactBaseIDs[split:]...)
							rc.Messages = msgs
							rc.ThreadMessageIDs = ids
							rc.HasActiveCompactSnapshot = true
							rc.ActiveCompactSnapshotText = persistSummary
						} else {
							gw, model := resolveCompactionGateway(ctx, pool, rc, auxGateway, emitDebugEvents, configLoader)
							if gw == nil {
								slog.WarnContext(ctx, "context_compact", "phase", "gateway_nil", "run_id", rc.Run.ID.String())
							} else {
								persistStartedEvent = map[string]any{
									"op":                    "persist",
									"phase":                 "started",
									"persist_split":         split,
									"trigger_tokens":        trigger,
									"context_window_tokens": window,
									"trigger_context_pct":   cfg.PersistTriggerContextPct,
									"tail_keep_effective":   tailKeep,
								}
								ApplyContextCompactPressureFields(persistStartedEvent, pressure)

								// Acquire file lock BEFORE LLM call to prevent concurrent compacts on Desktop.
								// For PostgreSQL, advisory lock inside tx provides DB-level protection,
								// but file lock ensures LLM call (expensive) is not duplicated.
								var fileLockCleanup func()
								var fileLockErr error
								if pool != nil {
									fileLockCleanup, fileLockErr = CompactThreadCompactionLock(ctx, rc.Run.ThreadID)
									if fileLockErr != nil {
										slog.WarnContext(ctx, "context_compact", "phase", "file_lock", "err", fileLockErr.Error(), "run_id", rc.Run.ID.String())
									}
									if fileLockCleanup != nil {
										defer fileLockCleanup()
									}
								}

								summary, sumErr := runContextCompactLLM(ctx, gw, model, compactBase[:split], enc, previousSummary)
								if sumErr != nil {
									slog.WarnContext(ctx, "context_compact", "phase", "llm", "err", sumErr.Error(), "run_id", rc.Run.ID.String())
									persistFailedEvent = map[string]any{
										"op":             "persist",
										"phase":          "llm_failed",
										"persist_split":  split,
										"llm_error":      sumErr.Error(),
										"trigger_tokens": trigger,
									}
									ApplyContextCompactPressureFields(persistFailedEvent, pressure)
								} else if strings.TrimSpace(summary) != "" {
									persistSplit = split
									persistSummary = strings.TrimSpace(summary)
									persistPrefixIDs = append([]uuid.UUID(nil), filterNonNilUUIDs(compactBaseIDs[:split])...)
									persistCompletedEvent = map[string]any{
										"op":                    "persist",
										"phase":                 "completed",
										"persist_split":         split,
										"messages_before":       beforeN,
										"context_window_tokens": window,
										"trigger_tokens":        trigger,
										"trigger_context_pct":   cfg.PersistTriggerContextPct,
										"tail_keep_configured":  keep,
										"tail_keep_effective":   tailKeep,
									}
									ApplyContextCompactPressureFields(persistCompletedEvent, pressure)
									tail := make([]llm.Message, len(compactBase)-split)
									copy(tail, compactBase[split:])
									tail = truncateLargeTailMessages(enc, tail)
									msgs = append([]llm.Message{makeCompactSnapshotMessage(persistSummary)}, tail...)
									ids = append([]uuid.UUID{uuid.Nil}, compactBaseIDs[split:]...)
									rc.Messages = msgs
									rc.ThreadMessageIDs = ids
									rc.HasActiveCompactSnapshot = true
									rc.ActiveCompactSnapshotText = persistSummary
								}
							}
						}
					}
				}
			}
		}

		var trimEvent map[string]any
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
				trimEvent = map[string]any{
					"op":                            "trim",
					"phase":                         "completed",
					"dropped_prefix":                dropped,
					"messages_before":               beforeTrim,
					"messages_after":                len(out),
					"thread_tokens_tiktoken_before": beforeTrimTok,
					"thread_tokens_tiktoken_after":  HistoryThreadPromptTokens(enc, out),
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
				"fast_path", persistFastPath,
			)
		}

		nextErr := next(ctx, rc)

		postCtx, cancel := context.WithTimeout(context.Background(), contextCompactPostWriteTimeout)
		defer cancel()

		if persistStartedEvent != nil {
			if err := appendContextCompactRunEvent(postCtx, pool, eventsRepo, rc, persistStartedEvent); err != nil {
				slog.WarnContext(ctx, "context_compact", "phase", "run_event_started", "err", err.Error(), "run_id", rc.Run.ID.String())
			}
		}
		if persistFailedEvent != nil {
			if err := appendContextCompactRunEvent(postCtx, pool, eventsRepo, rc, persistFailedEvent); err != nil {
				slog.WarnContext(ctx, "context_compact", "phase", "run_event_llm_failed", "err", err.Error(), "run_id", rc.Run.ID.String())
			}
		}

		if persistSplit > 0 && persistSummary != "" && pool != nil {
			tx, txErr := pool.BeginTx(postCtx, pgx.TxOptions{})
			if txErr != nil {
				slog.WarnContext(ctx, "context_compact", "phase", "tx_begin", "err", txErr.Error(), "run_id", rc.Run.ID.String())
			} else {
				if lockErr := compactThreadCompactionAdvisoryXactLock(postCtx, tx, rc.Run.ThreadID); lockErr != nil {
					_ = tx.Rollback(postCtx)
					emitContextCompactFailure(ctx, postCtx, pool, eventsRepo, rc, "persist", "advisory_lock", lockErr)
					slog.WarnContext(ctx, "context_compact", "phase", "advisory_lock", "err", lockErr.Error(), "run_id", rc.Run.ID.String())
				} else {
					still, chkErr := compactPrefixMessagesStillUncompacted(postCtx, tx, rc.Run.AccountID, rc.Run.ThreadID, persistPrefixIDs)
					if chkErr != nil {
						_ = tx.Rollback(postCtx)
						emitContextCompactFailure(ctx, postCtx, pool, eventsRepo, rc, "persist", "prefix_precheck", chkErr)
						slog.WarnContext(ctx, "context_compact", "phase", "prefix_precheck", "err", chkErr.Error(), "run_id", rc.Run.ID.String())
					} else if !still {
						_ = tx.Rollback(postCtx)
					} else if len(persistPrefixIDs) > 0 {
						if err := messagesRepo.MarkThreadMessagesCompacted(postCtx, tx, rc.Run.AccountID, rc.Run.ThreadID, persistPrefixIDs); err != nil {
							_ = tx.Rollback(postCtx)
							emitContextCompactFailure(ctx, postCtx, pool, eventsRepo, rc, "persist", "mark_compacted", err)
							slog.WarnContext(ctx, "context_compact", "phase", "mark_compacted", "err", err.Error(), "run_id", rc.Run.ID.String())
						} else {
							meta, _ := json.Marshal(map[string]string{"kind": "context_compact"})
							snapshot, insErr := (data.ThreadCompactionSnapshotsRepository{}).ReplaceActive(postCtx, tx, rc.Run.AccountID, rc.Run.ThreadID, persistSummary, meta)
							if insErr != nil {
								_ = tx.Rollback(postCtx)
								emitContextCompactFailure(ctx, postCtx, pool, eventsRepo, rc, "persist", "replace_snapshot", insErr)
								slog.WarnContext(ctx, "context_compact", "phase", "replace_snapshot", "err", insErr.Error(), "run_id", rc.Run.ID.String())
							} else {
								evOk := true
								if persistCompletedEvent != nil && eventsRepo != nil {
									ev := rc.Emitter.Emit("run.context_compact", persistCompletedEvent, nil, nil)
									if _, evErr := eventsRepo.AppendRunEvent(postCtx, tx, rc.Run.ID, ev); evErr != nil {
										_ = tx.Rollback(postCtx)
										evOk = false
										slog.WarnContext(ctx, "context_compact", "phase", "run_event", "err", evErr.Error(), "run_id", rc.Run.ID.String())
									}
								}
								if evOk {
									if err := tx.Commit(postCtx); err != nil {
										slog.WarnContext(ctx, "context_compact", "phase", "tx_commit", "err", err.Error(), "run_id", rc.Run.ID.String())
									} else {
										rc.HasActiveCompactSnapshot = true
										rc.ActiveCompactSnapshotText = snapshot.SummaryText
									}
								}
							}
						}
					} else {
						meta, _ := json.Marshal(map[string]string{"kind": "context_compact"})
						snapshot, insErr := (data.ThreadCompactionSnapshotsRepository{}).ReplaceActive(postCtx, tx, rc.Run.AccountID, rc.Run.ThreadID, persistSummary, meta)
						if insErr != nil {
							_ = tx.Rollback(postCtx)
							emitContextCompactFailure(ctx, postCtx, pool, eventsRepo, rc, "persist", "replace_snapshot", insErr)
							slog.WarnContext(ctx, "context_compact", "phase", "replace_snapshot", "err", insErr.Error(), "run_id", rc.Run.ID.String())
						} else {
							evOk := true
							if persistCompletedEvent != nil && eventsRepo != nil {
								ev := rc.Emitter.Emit("run.context_compact", persistCompletedEvent, nil, nil)
								if _, evErr := eventsRepo.AppendRunEvent(postCtx, tx, rc.Run.ID, ev); evErr != nil {
									_ = tx.Rollback(postCtx)
									evOk = false
									slog.WarnContext(ctx, "context_compact", "phase", "run_event", "err", evErr.Error(), "run_id", rc.Run.ID.String())
								}
							}
							if evOk {
								if err := tx.Commit(postCtx); err != nil {
									slog.WarnContext(ctx, "context_compact", "phase", "tx_commit", "err", err.Error(), "run_id", rc.Run.ID.String())
								} else {
									rc.HasActiveCompactSnapshot = true
									rc.ActiveCompactSnapshotText = snapshot.SummaryText
								}
							}
						}
					}
				}
			}
		}

		if trimEvent != nil {
			if err := appendContextCompactRunEvent(postCtx, pool, eventsRepo, rc, trimEvent); err != nil {
				slog.WarnContext(ctx, "context_compact", "phase", "run_event_trim", "err", err.Error(), "run_id", rc.Run.ID.String())
			}
		}
		afterTokens := traceContextCompactTokens(enc, rc.SystemPrompt, rc.Messages)
		emitTraceEvent(rc, "context_compact", "context_compact.completed", map[string]any{
			"compacted":     beforeTokens != afterTokens || len(beforeMsgs) != len(rc.Messages),
			"tokens_before": beforeTokens,
			"tokens_after":  afterTokens,
		})

		return nextErr
	}
}

func traceContextCompactTokens(enc *tiktoken.Tiktoken, systemPrompt string, msgs []llm.Message) int {
	if enc == nil {
		enc, _ = tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	}
	return HistoryThreadPromptTokens(enc, contextCompactRequestMessages(systemPrompt, msgs))
}

func filterNonNilUUIDs(ids []uuid.UUID) []uuid.UUID {
	if len(ids) == 0 {
		return nil
	}
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id != uuid.Nil {
			out = append(out, id)
		}
	}
	return out
}

// compactPrefixMessagesStillUncompacted 事务内校验：待标记的 id 仍全部存在且未 compact，避免并发 persist 重复写。
func compactPrefixMessagesStillUncompacted(ctx context.Context, tx pgx.Tx, accountID, threadID uuid.UUID, prefixIDs []uuid.UUID) (bool, error) {
	if len(prefixIDs) == 0 {
		return true, nil
	}
	var n int
	err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM messages WHERE account_id = $1 AND thread_id = $2 AND id = ANY($3::uuid[]) AND deleted_at IS NULL AND compacted = false`,
		accountID, threadID, prefixIDs,
	).Scan(&n)
	if err != nil {
		return false, err
	}
	return n == len(prefixIDs), nil
}

func resolveCompactionGateway(
	ctx context.Context,
	pool CompactPersistDB,
	rc *RunContext,
	auxGateway llm.Gateway,
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
		if gw, model, ok := resolveAccountToolGateway(ctx, pool, *accountID, auxGateway, emitDebugEvents, rc.LlmMaxResponseBytes, configLoader, rc.RoutingByokEnabled); ok {
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
	aid := rc.Run.AccountID
	routingCfg, err := configLoader.Load(ctx, &aid)
	if err != nil {
		slog.WarnContext(ctx, "context_compact", "phase", "routing_load", "err", err.Error())
		return fallbackGateway, fallbackModel
	}
	selected, err := resolveSelectedRouteBySelector(routingCfg, selector, map[string]any{}, rc.RoutingByokEnabled)
	if err != nil || selected == nil {
		if err != nil {
			slog.WarnContext(ctx, "context_compact", "phase", "selector", "selector", selector, "err", err.Error())
		}
		return fallbackGateway, fallbackModel
	}
	gw, err := gatewayFromSelectedRoute(*selected, auxGateway, emitDebugEvents, rc.LlmMaxResponseBytes)
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
	return trigger, window
}

func MaybeInlineCompactMessages(
	ctx context.Context,
	rc *RunContext,
	msgs []llm.Message,
	anchor *ContextCompactPressureAnchor,
) ([]llm.Message, ContextCompactPressureStats, bool, error) {
	if rc == nil {
		return msgs, ContextCompactPressureStats{}, false, nil
	}
	cfg := rc.ContextCompact
	if !cfg.PersistEnabled || rc.Gateway == nil || rc.SelectedRoute == nil || len(msgs) <= 1 {
		estimate := HistoryThreadPromptTokensForRoute(rc.SelectedRoute, msgs)
		stats := ComputeContextCompactPressure(estimate, anchor)
		return msgs, stats, false, nil
	}
	enc, err := ResolveTiktokenForRoute(rc.SelectedRoute)
	if err != nil || enc == nil {
		enc, _ = tiktoken.GetEncoding(tiktoken.MODEL_O200K_BASE)
	}
	window := routing.RouteContextWindowTokens(rc.SelectedRoute.Route)
	trigger, window := compactPersistTriggerTokens(cfg, window)
	estimate := HistoryThreadPromptTokens(enc, msgs)
	stats := ComputeContextCompactPressure(estimate, anchor)
	if stats.ContextPressureTokens < trigger {
		return msgs, stats, false, nil
	}
	fixedPrefixCount := 0
	previousSummary := ""
	if rc.HasActiveCompactSnapshot && len(msgs) > 0 {
		fixedPrefixCount = 1
		previousSummary = strings.TrimSpace(rc.ActiveCompactSnapshotText)
	}
	if len(msgs)-fixedPrefixCount <= 1 {
		return msgs, stats, false, nil
	}
	compactBase := append([]llm.Message(nil), msgs[fixedPrefixCount:]...)
	keep := cfg.PersistKeepLastMessages
	if keep <= 0 {
		keep = defaultPersistKeepLastMessages
	}
	var tailKeep int
	tailPct := cfg.PersistKeepTailPct
	if tailPct > 100 {
		tailPct = 100
	}
	if tailPct > 0 && window > 0 {
		tailTokenBudget := window * tailPct / 100
		tailKeep = computeTailKeepByTokenBudget(enc, compactBase, tailTokenBudget, keep)
	} else {
		tailKeep = keep
	}
	if tailKeep >= len(compactBase) {
		tailKeep = len(compactBase) - 1
	}
	if tailKeep < 1 {
		tailKeep = 1
	}
	split := stabilizeCompactStart(compactBase, len(compactBase)-tailKeep, 0)
	split = ensureToolPairIntegrity(compactBase, split)
	if split <= 0 {
		return msgs, stats, false, nil
	}
	summary, err := runContextCompactLLM(ctx, rc.Gateway, rc.SelectedRoute.Route.Model, compactBase[:split], enc, previousSummary)
	if err != nil {
		return msgs, stats, false, err
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return msgs, stats, false, nil
	}
	tail := make([]llm.Message, len(compactBase)-split)
	copy(tail, compactBase[split:])
	tail = truncateLargeTailMessages(enc, tail)
	compactedBase := append([]llm.Message{makeCompactSnapshotMessage(summary)}, tail...)
	return compactedBase, stats, true, nil
}

func runContextCompactLLM(ctx context.Context, gateway llm.Gateway, model string, prefix []llm.Message, enc *tiktoken.Tiktoken, previousSummary string) (string, error) {
	if gateway == nil || strings.TrimSpace(model) == "" {
		return "", fmt.Errorf("gateway or model missing")
	}
	prefix = TrimPrefixMessagesForCompactLLM(enc, prefix, contextCompactMaxLLMInputTokens)
	conversationText := serializeMessagesForCompact(prefix)
	if strings.TrimSpace(conversationText) == "" {
		return "", nil
	}
	runes := []rune(conversationText)
	if len(runes) > contextCompactMaxLLMInputRunes {
		conversationText = string(runes[len(runes)-contextCompactMaxLLMInputRunes:])
	}

	// 按 pi 的结构组装：<conversation> 块 + 可选 <previous-summary> 块 + 格式指令
	var userBlock strings.Builder
	userBlock.WriteString("<conversation>\n")
	userBlock.WriteString(conversationText)
	userBlock.WriteString("\n</conversation>\n\n")
	if previousSummary != "" {
		userBlock.WriteString("<previous-summary>\n")
		userBlock.WriteString(previousSummary)
		userBlock.WriteString("\n</previous-summary>\n\n")
		userBlock.WriteString(contextCompactUpdatePrompt)
	} else {
		userBlock.WriteString(contextCompactInitialPrompt)
	}

	maxTok := contextCompactMaxOut
	req := llm.Request{
		Model: model,
		Messages: []llm.Message{
			{Role: "system", Content: []llm.TextPart{{Text: contextCompactSystemPrompt}}},
			{Role: "user", Content: []llm.TextPart{{Text: userBlock.String()}}},
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

func appendContextCompactRunEvent(
	ctx context.Context,
	pool CompactPersistDB,
	eventsRepo CompactRunEventAppender,
	rc *RunContext,
	data map[string]any,
) error {
	if eventsRepo == nil || pool == nil {
		return nil
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	ev := rc.Emitter.Emit("run.context_compact", data, nil, nil)
	if _, err := eventsRepo.AppendRunEvent(ctx, tx, rc.Run.ID, ev); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func emitContextCompactFailure(
	ctx context.Context,
	postCtx context.Context,
	pool CompactPersistDB,
	eventsRepo CompactRunEventAppender,
	rc *RunContext,
	op string,
	phase string,
	err error,
) {
	if err == nil {
		return
	}
	payload := map[string]any{
		"op":    op,
		"phase": phase,
		"error": err.Error(),
	}
	if appendErr := appendContextCompactRunEvent(postCtx, pool, eventsRepo, rc, payload); appendErr != nil {
		slog.WarnContext(ctx, "context_compact", "phase", "run_event_failure", "err", appendErr.Error(), "run_id", rc.Run.ID.String())
	}
}

// serializeMessagesForCompact 将消息列表序列化为摘要 LLM 可读的纯文本。
// active snapshot 通过 previousSummary 单独传递；这里仅处理真实对话与 replay 内容。
// tool result 只提取 result/error 核心内容，tool calls 展开参数，避免噪声。
func serializeMessagesForCompact(msgs []llm.Message) string {
	parts := make([]string, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "user":
			if text := strings.TrimSpace(messageText(m)); text != "" {
				parts = append(parts, "[User]: "+text)
			}
		case "assistant":
			if text := strings.TrimSpace(messageText(m)); text != "" {
				parts = append(parts, "[Assistant]: "+text)
			}
			if len(m.ToolCalls) > 0 {
				calls := make([]string, 0, len(m.ToolCalls))
				for _, tc := range m.ToolCalls {
					call := tc.ToolName
					if len(tc.ArgumentsJSON) > 0 {
						if args, err := json.Marshal(tc.ArgumentsJSON); err == nil {
							call += "(" + string(args) + ")"
						}
					}
					calls = append(calls, call)
				}
				parts = append(parts, "[Assistant tool calls]: "+strings.Join(calls, "; "))
			}
		case "tool":
			// tool result Content 是 JSON envelope {tool_call_id, tool_name, result?, error?}
			// 只取 tool_name + result/error，丢弃 tool_call_id 等无关字段
			if text := strings.TrimSpace(messageText(m)); text != "" {
				label := "[Tool result]"
				content := text
				var envelope map[string]any
				if err := json.Unmarshal([]byte(text), &envelope); err == nil {
					if name, _ := envelope["tool_name"].(string); name != "" {
						label = "[Tool result: " + name + "]"
					}
					// 优先取 error，其次取 result
					if errVal := envelope["error"]; errVal != nil {
						if b, err := json.Marshal(errVal); err == nil {
							content = string(b)
						}
					} else if resVal := envelope["result"]; resVal != nil {
						if b, err := json.Marshal(resVal); err == nil {
							content = string(b)
						}
					}
				}
				parts = append(parts, label+": "+content)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

// runGroupCompactLLM 群聊专用的 compact LLM 调用，使用群聊 prompt 模板。
func runGroupCompactLLM(ctx context.Context, gateway llm.Gateway, model string, prefix []llm.Message, enc *tiktoken.Tiktoken, previousSummary string) (string, error) {
	if gateway == nil || strings.TrimSpace(model) == "" {
		return "", fmt.Errorf("gateway or model missing")
	}
	prefix = TrimPrefixMessagesForCompactLLM(enc, prefix, contextCompactMaxLLMInputTokens)
	conversationText := serializeMessagesForCompact(prefix)
	if strings.TrimSpace(conversationText) == "" {
		return "", nil
	}
	runes := []rune(conversationText)
	if len(runes) > contextCompactMaxLLMInputRunes {
		conversationText = string(runes[len(runes)-contextCompactMaxLLMInputRunes:])
	}

	var userBlock strings.Builder
	userBlock.WriteString("<conversation>\n")
	userBlock.WriteString(conversationText)
	userBlock.WriteString("\n</conversation>\n\n")
	if previousSummary != "" {
		userBlock.WriteString("<previous-summary>\n")
		userBlock.WriteString(previousSummary)
		userBlock.WriteString("\n</previous-summary>\n\n")
		userBlock.WriteString(contextCompactGroupUpdatePrompt)
	} else {
		userBlock.WriteString(contextCompactGroupInitialPrompt)
	}

	maxTok := contextCompactGroupMaxOut
	req := llm.Request{
		Model: model,
		Messages: []llm.Message{
			{Role: "system", Content: []llm.TextPart{{Text: contextCompactGroupSystemPrompt}}},
			{Role: "user", Content: []llm.TextPart{{Text: userBlock.String()}}},
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
