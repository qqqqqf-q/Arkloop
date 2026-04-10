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

const contextCompactSystemPrompt = `You are a dialogue compression assistant.

Compress the conversation faithfully so another model can continue with minimal loss.

Rules:
- This is compression, not analysis.
- Do NOT infer goals, plans, blockers, or "next steps" unless they were explicitly said.
- Preserve concrete facts, chronology, unresolved questions, decisions actually stated, file paths, function names, commands, URLs, numbers, errors, IDs, and quoted wording when important.
- Remove filler, repetition, greetings, and other low-information chatter.
- Keep the output in the dominant language of the conversation.
- Output only the compressed conversation text.`

const contextCompactInitialPrompt = `Rewrite the conversation above into a shorter faithful version.

Output rules:
- Keep chronological order.
- Use short bullet points.
- Mention the speaker only when it helps disambiguate.
- Preserve concrete details exactly when they matter.
- Do not turn the conversation into a project report or task analysis.
- Do not add headings such as Goal, Progress, Next Steps, or Decisions unless those words were part of the original conversation.
- Do not answer the conversation.`

const contextCompactUpdatePrompt = `Update the existing compressed conversation in <previous-summary> using the new messages in <conversation>.

Rules:
- Preserve earlier compressed content unless the new messages clearly replace or resolve it.
- Keep chronological order.
- Continue to compress faithfully rather than analyze.
- Keep concrete details exact when they matter.
- Remove filler and repeated phrasing.
- Output only the updated compressed conversation as short bullet points.`

const contextCompactGroupSystemPrompt = `You are a multi-participant dialogue compression assistant.

Compress the conversation faithfully so another model can continue with minimal loss.

Rules:
- This is compression, not analysis.
- Preserve who said what when speaker identity matters.
- Do NOT infer goals, plans, moods, or conclusions unless they were explicitly stated.
- Preserve usernames, links, numbers, commands, IDs, errors, and notable quoted wording when important.
- Remove filler, repetition, greetings, and other low-information chatter.
- Keep the output in the dominant language of the conversation.
- Output only the compressed conversation text.`

const contextCompactGroupInitialPrompt = `Rewrite the group conversation above into a shorter faithful version.

Output rules:
- Keep chronological order.
- Use short bullet points.
- Prefix each bullet with the participant name only when needed.
- Preserve concrete facts and speaker attribution exactly when they matter.
- Do not turn the conversation into topics / mood / participant analysis.
- Do not answer the conversation.`

const contextCompactGroupUpdatePrompt = `Update the existing compressed group conversation in <previous-summary> using the new messages in <conversation>.

Rules:
- Preserve earlier compressed content unless the new messages clearly replace or resolve it.
- Keep chronological order.
- Preserve speaker attribution when it matters.
- Continue to compress faithfully rather than analyze.
- Keep concrete details exact when they matter.
- Remove filler and repeated phrasing.
- Output only the updated compressed conversation as short bullet points.`

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
		if rewritten, stripped := stripOlderImagePartsKeepingTail(rc.Messages, resolveContextKeepImageTail()); stripped > 0 {
			rc.Messages = rewritten
		}
		if !cfg.Enabled && !cfg.PersistEnabled {
			beforeTokens := traceContextCompactTokens(nil, rc.SystemPrompt, beforeMsgs)
			afterTokens := traceContextCompactTokens(nil, rc.SystemPrompt, rc.Messages)
			emitTraceEvent(rc, "context_compact", "context_compact.completed", map[string]any{
				"compacted":     beforeTokens != afterTokens || len(beforeMsgs) != len(rc.Messages),
				"tokens_before": beforeTokens,
				"tokens_after":  afterTokens,
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
		persistSplit := 0
		var persistWindowMsgs []llm.Message
		var persistWindowIDs []uuid.UUID
		persistWindowActiveSnapshotText := strings.TrimSpace(rc.ActiveCompactSnapshotText)
		var persistPrefixIDs []uuid.UUID
		var persistSummary string
		var persistStartedEvent map[string]any
		var persistFailedEvent map[string]any
		var persistCompletedEvent map[string]any

		if cfg.PersistEnabled && !isGroupChat && pool != nil && rc.Gateway != nil && len(msgs) > 1 {
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
					compactBase := msgs
					compactBaseIDs := ids
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
						split = clampPersistSplitBeforeSyntheticTail(compactBase, compactBaseIDs, split)
					}
					if split > 0 {
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

							summaryInputMsgs, summaryInputDropped := prepareCompactSummaryInput(enc, compactBase[:split])
							summaryInputIDs := compactBaseIDs[summaryInputDropped:split]
							summary, sumErr := runContextCompactLLM(ctx, gw, model, summaryInputMsgs, enc, persistWindowActiveSnapshotText)
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
								persistWindowMsgs = append([]llm.Message(nil), summaryInputMsgs...)
								persistWindowIDs = append([]uuid.UUID(nil), summaryInputIDs...)
								persistWindowActiveSnapshotText = strings.TrimSpace(rc.ActiveCompactSnapshotText)
								persistSummary = strings.TrimSpace(summary)
								persistPrefixIDs = append([]uuid.UUID(nil), filterNonNilUUIDs(summaryInputIDs)...)
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
								unsummarizedHead := make([]llm.Message, summaryInputDropped)
								copy(unsummarizedHead, compactBase[:summaryInputDropped])
								tail := make([]llm.Message, len(compactBase)-split)
								copy(tail, compactBase[split:])
								tail = truncateLargeTailMessages(enc, tail)
								msgs = append(unsummarizedHead, makeCompactSnapshotMessage(persistSummary))
								msgs = append(msgs, tail...)
								ids = append([]uuid.UUID(nil), compactBaseIDs[:summaryInputDropped]...)
								ids = append(ids, uuid.Nil)
								ids = append(ids, compactBaseIDs[split:]...)
								rc.Messages = msgs
								rc.ThreadMessageIDs = ids
								rc.HasActiveCompactSnapshot = true
								rc.ActiveCompactSnapshotText = firstCompactSummaryText(msgs, ids)
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
					still, chkErr := compactPrefixMessagesStillAvailable(postCtx, tx, rc.Run.AccountID, rc.Run.ThreadID, persistPrefixIDs)
					if chkErr != nil {
						_ = tx.Rollback(postCtx)
						emitContextCompactFailure(ctx, postCtx, pool, eventsRepo, rc, "persist", "prefix_precheck", chkErr)
						slog.WarnContext(ctx, "context_compact", "phase", "prefix_precheck", "err", chkErr.Error(), "run_id", rc.Run.ID.String())
					} else if !still {
						_ = tx.Rollback(postCtx)
					} else {
						startThreadSeq, endThreadSeq, layer, ok, rangeErr := resolvePersistReplacementRange(
							postCtx,
							tx,
							messagesRepo,
							rc.Run.AccountID,
							rc.Run.ThreadID,
							persistWindowMsgs,
							persistWindowIDs,
							persistWindowActiveSnapshotText,
						)
						if rangeErr != nil {
							_ = tx.Rollback(postCtx)
							emitContextCompactFailure(ctx, postCtx, pool, eventsRepo, rc, "persist", "range_resolve", rangeErr)
							slog.WarnContext(ctx, "context_compact", "phase", "range_resolve", "err", rangeErr.Error(), "run_id", rc.Run.ID.String())
						} else if !ok {
							_ = tx.Rollback(postCtx)
						} else {
							replacementsRepo := data.ThreadContextReplacementsRepository{}
							replacement, insErr := replacementsRepo.Insert(postCtx, tx, data.ThreadContextReplacementInsertInput{
								AccountID:      rc.Run.AccountID,
								ThreadID:       rc.Run.ThreadID,
								StartThreadSeq: startThreadSeq,
								EndThreadSeq:   endThreadSeq,
								SummaryText:    persistSummary,
								Layer:          layer,
								MetadataJSON:   compactReplacementMetadata("context_compact"),
							})
							if insErr != nil {
								_ = tx.Rollback(postCtx)
								emitContextCompactFailure(ctx, postCtx, pool, eventsRepo, rc, "persist", "insert_replacement", insErr)
								slog.WarnContext(ctx, "context_compact", "phase", "insert_replacement", "err", insErr.Error(), "run_id", rc.Run.ID.String())
							} else if supErr := replacementsRepo.SupersedeActiveOverlaps(postCtx, tx, rc.Run.AccountID, rc.Run.ThreadID, replacement.StartThreadSeq, replacement.EndThreadSeq, replacement.ID); supErr != nil {
								_ = tx.Rollback(postCtx)
								emitContextCompactFailure(ctx, postCtx, pool, eventsRepo, rc, "persist", "supersede_replacements", supErr)
								slog.WarnContext(ctx, "context_compact", "phase", "supersede_replacements", "err", supErr.Error(), "run_id", rc.Run.ID.String())
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
										rc.ActiveCompactSnapshotText = firstCompactSummaryText(rc.Messages, rc.ThreadMessageIDs)
									}
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

func clampPersistSplitBeforeSyntheticTail(msgs []llm.Message, ids []uuid.UUID, split int) int {
	if split <= 0 || len(ids) != len(msgs) {
		return split
	}
	leadingPrefix := leadingCompactPrefixMessageCount(msgs, ids)
	for i := leadingPrefix; i < split; i++ {
		if ids[i] == uuid.Nil {
			return i
		}
	}
	return split
}

func resolvePersistReplacementRange(
	ctx context.Context,
	tx pgx.Tx,
	messagesRepo data.MessagesRepository,
	accountID uuid.UUID,
	threadID uuid.UUID,
	prefixMsgs []llm.Message,
	prefixIDs []uuid.UUID,
	activeSnapshotText string,
) (int64, int64, int, bool, error) {
	if tx == nil {
		return 0, 0, 0, false, fmt.Errorf("tx must not be nil")
	}
	var (
		startThreadSeq int64
		endThreadSeq   int64
		layer          = 1
	)
	mergeRange := func(start, end int64) {
		if start <= 0 || end <= 0 || start > end {
			return
		}
		if startThreadSeq == 0 || start < startThreadSeq {
			startThreadSeq = start
		}
		if end > endThreadSeq {
			endThreadSeq = end
		}
	}

	rawIDs := filterNonNilUUIDs(prefixIDs)
	rawStart := int64(0)
	if len(rawIDs) > 0 {
		start, end, err := messagesRepo.GetThreadSeqRangeForMessageIDs(ctx, tx, accountID, threadID, rawIDs)
		if err != nil {
			return 0, 0, 0, false, err
		}
		rawStart = start
		mergeRange(start, end)
	}

	compactedLeadingCount := leadingCompactPrefixMessageCount(prefixMsgs, prefixIDs)
	if compactedLeadingCount > 0 {
		replacementsRepo := data.ThreadContextReplacementsRepository{}
		items, err := replacementsRepo.ListActiveByThreadUpToSeq(ctx, tx, accountID, threadID, nil)
		if err != nil {
			return 0, 0, 0, false, err
		}
		selected := selectRenderableReplacements(items)
		included := compactedLeadingCount
		if included > len(selected) {
			included = len(selected)
		}
		for i := 0; i < included; i++ {
			mergeRange(selected[i].StartThreadSeq, selected[i].EndThreadSeq)
			if selected[i].Layer+1 > layer {
				layer = selected[i].Layer + 1
			}
		}
		if compactedLeadingCount > included && strings.TrimSpace(activeSnapshotText) != "" {
			legacyEnd := int64(0)
			if len(selected) > 0 && selected[0].StartThreadSeq > 1 {
				legacyEnd = selected[0].StartThreadSeq - 1
			} else if rawStart > 1 {
				legacyEnd = rawStart - 1
			} else if endThreadSeq > 0 {
				legacyEnd = endThreadSeq
			}
			if legacyEnd > 0 {
				mergeRange(1, legacyEnd)
				if layer < 2 {
					layer = 2
				}
			}
		}
	}

	if startThreadSeq <= 0 || endThreadSeq <= 0 || startThreadSeq > endThreadSeq {
		return 0, 0, 0, false, nil
	}
	return startThreadSeq, endThreadSeq, layer, true, nil
}

func leadingCompactPrefixMessageCount(msgs []llm.Message, ids []uuid.UUID) int {
	if len(msgs) == 0 {
		return 0
	}
	alignedIDs := len(ids) == len(msgs)
	count := 0
	for i := range msgs {
		if alignedIDs && ids[i] != uuid.Nil {
			break
		}
		if msgs[i].Role != "user" || len(msgs[i].Content) == 0 {
			break
		}
		if !strings.HasPrefix(msgs[i].Content[0].Text, compactSnapshotHeader) {
			break
		}
		count++
	}
	return count
}

func firstCompactSummaryText(msgs []llm.Message, ids []uuid.UUID) string {
	count := leadingCompactPrefixMessageCount(msgs, ids)
	if count == 0 || len(msgs[0].Content) == 0 {
		return ""
	}
	text := msgs[0].Content[0].Text
	start := strings.Index(text, "<state_snapshot>")
	end := strings.Index(text, "</state_snapshot>")
	if start < 0 || end < 0 || end <= start {
		return strings.TrimSpace(text)
	}
	start += len("<state_snapshot>")
	return strings.TrimSpace(text[start:end])
}

func leadingCompactSnapshotPrefixCount(msgs []llm.Message, ids []uuid.UUID) int {
	if len(msgs) == 0 {
		return 0
	}
	aligned := len(ids) == len(msgs)
	n := 0
	for i := 0; i < len(msgs); i++ {
		if aligned && ids[i] != uuid.Nil {
			break
		}
		m := msgs[i]
		if strings.TrimSpace(m.Role) != "user" || len(m.Content) == 0 {
			break
		}
		// snapshot message uses a stable header; avoid treating replay/resume synthetic messages as snapshots.
		if !strings.HasPrefix(strings.TrimSpace(m.Content[0].Text), compactSnapshotHeader) {
			break
		}
		n++
	}
	return n
}

// compactPrefixMessagesStillAvailable 事务内校验：待折叠的前缀消息仍全部存在，避免并发 persist 重复写 replacement。
func compactPrefixMessagesStillAvailable(ctx context.Context, tx pgx.Tx, accountID, threadID uuid.UUID, prefixIDs []uuid.UUID) (bool, error) {
	if len(prefixIDs) == 0 {
		return true, nil
	}
	var n int
	err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM messages WHERE account_id = $1 AND thread_id = $2 AND id = ANY($3::uuid[]) AND deleted_at IS NULL`,
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
	if len(msgs) <= 1 {
		return msgs, stats, false, nil
	}
	compactBase := append([]llm.Message(nil), msgs...)
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
	summaryInputMsgs, summaryInputDropped := prepareCompactSummaryInput(enc, compactBase[:split])
	summary, err := runContextCompactLLM(ctx, rc.Gateway, rc.SelectedRoute.Route.Model, summaryInputMsgs, enc, rc.ActiveCompactSnapshotText)
	if err != nil {
		return msgs, stats, false, err
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return msgs, stats, false, nil
	}
	unsummarizedHead := make([]llm.Message, summaryInputDropped)
	copy(unsummarizedHead, compactBase[:summaryInputDropped])
	tail := make([]llm.Message, len(compactBase)-split)
	copy(tail, compactBase[split:])
	tail = truncateLargeTailMessages(enc, tail)
	compactedBase := append([]llm.Message(nil), unsummarizedHead...)
	compactedBase = append(compactedBase, makeCompactSnapshotMessage(summary))
	compactedBase = append(compactedBase, tail...)
	return compactedBase, stats, true, nil
}

func runContextCompactLLM(ctx context.Context, gateway llm.Gateway, model string, prefix []llm.Message, enc *tiktoken.Tiktoken, previousSummary string) (string, error) {
	if gateway == nil || strings.TrimSpace(model) == "" {
		return "", fmt.Errorf("gateway or model missing")
	}
	_ = enc
	if strings.TrimSpace(previousSummary) != "" {
		prefix = trimLeadingCompactSnapshotMessages(prefix)
	}
	conversationText := serializeMessagesForCompact(prefix)
	if strings.TrimSpace(conversationText) == "" && strings.TrimSpace(previousSummary) != "" {
		return strings.TrimSpace(previousSummary), nil
	}
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

func trimLeadingCompactSnapshotMessages(msgs []llm.Message) []llm.Message {
	start := 0
	for start < len(msgs) {
		msg := msgs[start]
		if strings.TrimSpace(msg.Role) != "user" || len(msg.Content) == 0 {
			break
		}
		if !strings.HasPrefix(strings.TrimSpace(msg.Content[0].Text), compactSnapshotHeader) {
			break
		}
		start++
	}
	if start == 0 {
		return msgs
	}
	out := make([]llm.Message, len(msgs)-start)
	copy(out, msgs[start:])
	return out
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
	_ = enc
	if strings.TrimSpace(previousSummary) != "" {
		prefix = trimLeadingCompactSnapshotMessages(prefix)
	}
	conversationText := serializeMessagesForCompact(prefix)
	if strings.TrimSpace(conversationText) == "" && strings.TrimSpace(previousSummary) != "" {
		return strings.TrimSpace(previousSummary), nil
	}
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
