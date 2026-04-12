package accountapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	"arkloop/services/api/internal/observability"
	"arkloop/services/shared/discordbot"
	"arkloop/services/shared/eventbus"
	"arkloop/services/shared/pgnotify"
	"arkloop/services/shared/telegrambot"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var channelInboundBurstRecoveryInterval = 500 * time.Millisecond

const (
	channelInboundBurstScanBatchLimit = 32
)

type ChannelInboundBurstRunnerDeps struct {
	ChannelsRepo             *data.ChannelsRepository
	ChannelIdentitiesRepo    *data.ChannelIdentitiesRepository
	ChannelIdentityLinksRepo *data.ChannelIdentityLinksRepository
	ChannelBindCodesRepo     *data.ChannelBindCodesRepository
	ChannelDMThreadsRepo     *data.ChannelDMThreadsRepository
	ChannelGroupThreadsRepo  *data.ChannelGroupThreadsRepository
	ChannelReceiptsRepo      *data.ChannelMessageReceiptsRepository
	ChannelLedgerRepo        *data.ChannelMessageLedgerRepository
	SecretsRepo              *data.SecretsRepository
	PersonasRepo             *data.PersonasRepository
	UsersRepo                *data.UserRepository
	AccountRepo              *data.AccountRepository
	AccountMembershipRepo    *data.AccountMembershipRepository
	ProjectRepo              *data.ProjectRepository
	ThreadRepo               *data.ThreadRepository
	MessageRepo              *data.MessageRepository
	RunEventRepo             *data.RunEventRepository
	JobRepo                  *data.JobRepository
	CreditsRepo              *data.CreditsRepository
	Pool                     data.DB
	EntitlementService       *entitlement.Service
	TelegramBotClient        *telegrambot.Client
	DiscordBotClient         *discordbot.Client
	MessageAttachmentStore   MessageAttachmentPutStore
	Bus                      eventbus.EventBus
}

type channelInboundBurstRunner struct {
	channelsRepo *data.ChannelsRepository
	personasRepo *data.PersonasRepository
	ledgerRepo   *data.ChannelMessageLedgerRepository
	runEventRepo *data.RunEventRepository
	jobRepo      *data.JobRepository
	messageRepo  *data.MessageRepository
	pool         data.DB
	inputNotify  func(ctx context.Context, runID uuid.UUID)
}

func StartChannelInboundBurstRunner(ctx context.Context, deps ChannelInboundBurstRunnerDeps) {
	if ctx == nil || deps.ChannelLedgerRepo == nil || deps.ChannelsRepo == nil || deps.PersonasRepo == nil ||
		deps.RunEventRepo == nil || deps.JobRepo == nil || deps.MessageRepo == nil || deps.Pool == nil {
		slog.Warn("channel_inbound_burst_runner_skip", "reason", "deps")
		return
	}

	var inputNotify func(ctx context.Context, runID uuid.UUID)
	if deps.Bus != nil {
		bus := deps.Bus
		inputNotify = func(ctx context.Context, runID uuid.UUID) {
			if runID == uuid.Nil {
				return
			}
			_ = bus.Publish(ctx, fmt.Sprintf("run_events:%s", runID.String()), "")
		}
	} else if deps.Pool != nil {
		pool := deps.Pool
		inputNotify = func(ctx context.Context, runID uuid.UUID) {
			if runID == uuid.Nil {
				return
			}
			if _, err := pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgnotify.ChannelRunInput, runID.String()); err != nil {
				slog.Warn("channel_active_run_notify_failed", "run_id", runID.String(), "error", err.Error())
			}
		}
	}

	runner := channelInboundBurstRunner{
		channelsRepo: deps.ChannelsRepo,
		personasRepo: deps.PersonasRepo,
		ledgerRepo:   deps.ChannelLedgerRepo,
		runEventRepo: deps.RunEventRepo,
		jobRepo:      deps.JobRepo,
		messageRepo:  deps.MessageRepo,
		pool:         deps.Pool,
		inputNotify:  inputNotify,
	}
	go runner.run(ctx)
}

func (r channelInboundBurstRunner) run(ctx context.Context) {
	ticker := time.NewTicker(channelInboundBurstRecoveryInterval)
	defer ticker.Stop()

	for {
		if err := r.scan(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("channel_inbound_burst_scan_failed", "error", err.Error())
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r channelInboundBurstRunner) scan(ctx context.Context) error {
	channels, err := r.listActiveInboundChannels(ctx)
	if err != nil {
		return err
	}
	for _, ch := range channels {
		if err := r.recoverChannel(ctx, ch); err != nil && ctx.Err() == nil {
			slog.Warn("channel_inbound_burst_channel_recovery_failed",
				"channel_id", ch.ID.String(),
				"channel_type", ch.ChannelType,
				"error", err.Error(),
			)
		}
	}
	return nil
}

func (r channelInboundBurstRunner) listActiveInboundChannels(ctx context.Context) ([]data.Channel, error) {
	typeSet := []string{"telegram", "discord"}
	out := make([]data.Channel, 0, 8)
	seen := make(map[uuid.UUID]struct{}, 8)
	for _, channelType := range typeSet {
		items, err := r.channelsRepo.ListActiveByType(ctx, channelType)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
			out = append(out, item)
		}
	}
	return out, nil
}

func (r channelInboundBurstRunner) recoverChannel(ctx context.Context, ch data.Channel) error {
	batches, err := r.ledgerRepo.ListDueInboundBatchesByChannel(ctx, ch.ID, time.Now().UTC(), channelInboundBurstScanBatchLimit)
	if err != nil {
		return err
	}
	for _, batch := range batches {
		if err := r.recoverBatch(ctx, ch, batch); err != nil && ctx.Err() == nil {
			slog.Warn("channel_inbound_burst_batch_recovery_failed",
				"channel_id", ch.ID.String(),
				"channel_type", ch.ChannelType,
				"thread_id", batch.ThreadID.String(),
				"batch_id", batch.BatchID,
				"error", err.Error(),
			)
		}
	}
	return nil
}

func (r channelInboundBurstRunner) recoverBatch(
	ctx context.Context,
	ch data.Channel,
	candidate data.ChannelInboundLedgerBatch,
) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	ledgerRepoTx := r.ledgerRepo.WithTx(tx)
	openBatch, err := ledgerRepoTx.GetOpenInboundBatchByThread(ctx, ch.ID, candidate.ThreadID)
	if err != nil {
		return err
	}
	if openBatch == nil {
		return nil
	}
	if candidate.BatchID != "" && openBatch.BatchID != candidate.BatchID {
		return nil
	}
	if openBatch.DueAt.After(time.Now().UTC()) {
		return nil
	}
	entries := openBatch.Entries
	if len(entries) == 0 {
		return nil
	}

	runRepoTx := r.runEventRepo.WithTx(tx)
	if err := runRepoTx.LockThreadRow(ctx, openBatch.ThreadID); err != nil {
		return err
	}

	activeRun, err := runRepoTx.GetActiveRootRunForThread(ctx, openBatch.ThreadID)
	if err != nil {
		return err
	}
	if activeRun != nil {
		state, delivered, err := deliverPendingBatchToActiveRunTx(
			ctx,
			ch,
			runRepoTx,
			r.messageRepo,
			ledgerRepoTx,
			activeRun,
			entries,
			observability.NewTraceID(),
		)
		if err != nil {
			return err
		}
		if state != "" {
			if err := markPendingBatchStateTx(ctx, ledgerRepoTx, ch.ID, entries, &activeRun.ID, state); err != nil {
				return err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		if delivered {
			r.notifyActiveRunInput(ctx, activeRun.ID)
		}
		return nil
	}

	personaRef, err := resolveInboundBurstPersonaRef(ctx, r.personasRepo, ch)
	if err != nil {
		return err
	}
	latestEntry, err := latestPendingBatchEntry(entries)
	if err != nil {
		return err
	}
	if latestEntry.MessageID == nil || *latestEntry.MessageID == uuid.Nil {
		return fmt.Errorf("pending batch latest entry missing message_id")
	}
	message, err := r.messageRepo.GetByID(ctx, ch.AccountID, openBatch.ThreadID, *latestEntry.MessageID)
	if err != nil {
		return err
	}
	if message == nil {
		return fmt.Errorf("pending batch latest message not found")
	}
	jobPayload, err := buildChannelBurstJobPayload(ch, latestEntry)
	if err != nil {
		return err
	}
	runStartedData := buildChannelBurstRunStartedData(
		personaRef,
		resolveChannelBurstDefaultModel(ch.ConfigJSON),
		jobPayload,
	)
	runStartedData["thread_tail_message_id"] = latestEntry.MessageID.String()
	run, _, err := runRepoTx.CreateRunWithStartedEvent(
		ctx,
		ch.AccountID,
		openBatch.ThreadID,
		message.CreatedByUserID,
		"run.started",
		runStartedData,
	)
	if err != nil {
		return err
	}
	if _, err := r.jobRepo.WithTx(tx).EnqueueRun(
		ctx,
		ch.AccountID,
		run.ID,
		observability.NewTraceID(),
		data.RunExecuteJobType,
		jobPayload,
		nil,
	); err != nil {
		return err
	}
	if err := markPendingBatchStateTx(ctx, ledgerRepoTx, ch.ID, entries, &run.ID, inboundStateEnqueuedNewRun); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r channelInboundBurstRunner) notifyActiveRunInput(ctx context.Context, runID uuid.UUID) {
	if r.inputNotify == nil || runID == uuid.Nil {
		return
	}
	r.inputNotify(ctx, runID)
}

func deliverPendingBatchToActiveRunTx(
	ctx context.Context,
	ch data.Channel,
	runRepo *data.RunEventRepository,
	messageRepo *data.MessageRepository,
	ledgerRepo *data.ChannelMessageLedgerRepository,
	activeRun *data.Run,
	entries []data.ChannelInboundLedgerEntry,
	traceID string,
) (string, bool, error) {
	if runRepo == nil || messageRepo == nil || activeRun == nil || len(entries) == 0 {
		return "", false, nil
	}

	switch ch.ChannelType {
	case "telegram":
		connector := telegramConnector{channelLedgerRepo: ledgerRepo}
		for _, entry := range entries {
			if entry.ThreadID == nil || entry.MessageID == nil {
				return "", false, fmt.Errorf("telegram inbound ledger missing thread_id or message_id")
			}
			msg, err := messageRepo.GetByID(ctx, ch.AccountID, *entry.ThreadID, *entry.MessageID)
			if err != nil {
				return "", false, err
			}
			if msg == nil {
				return "", false, fmt.Errorf("telegram inbound message missing")
			}
			preTail, _ := inboundLedgerString(entry.MetadataJSON, inboundMetadataPreTailKey)
			delivered, heartbeatAbsorbed, err := connector.deliverTelegramMessageToActiveRun(
				ctx,
				runRepo,
				activeRun,
				buildTelegramIncomingFromLedger(entry),
				msg.Content,
				traceID,
				preTail,
			)
			if err != nil {
				return "", false, err
			}
			if heartbeatAbsorbed {
				return inboundStateAbsorbedHeartbeat, false, nil
			}
			if !delivered {
				return "", false, nil
			}
		}
		return inboundStateDeliveredToRun, true, nil
	default:
		for _, entry := range entries {
			if entry.ThreadID == nil || entry.MessageID == nil {
				return "", false, fmt.Errorf("channel inbound ledger missing thread_id or message_id")
			}
			msg, err := messageRepo.GetByID(ctx, ch.AccountID, *entry.ThreadID, *entry.MessageID)
			if err != nil {
				return "", false, err
			}
			if msg == nil {
				return "", false, fmt.Errorf("channel inbound message missing")
			}
			if _, err := runRepo.ProvideInputWithKey(
				ctx,
				activeRun.ID,
				msg.Content,
				traceID,
				channelInboundInputKey(ch.ChannelType, entry.PlatformConversationID, entry.PlatformMessageID),
			); err != nil {
				var notActive data.RunNotActiveError
				if errors.As(err, &notActive) {
					return "", false, nil
				}
				return "", false, err
			}
		}
		return inboundStateDeliveredToRun, true, nil
	}
}

func channelInboundInputKey(channelType, platformConversationID, platformMessageID string) string {
	channelType = strings.TrimSpace(channelType)
	platformConversationID = strings.TrimSpace(platformConversationID)
	platformMessageID = strings.TrimSpace(platformMessageID)
	if channelType == "" || platformConversationID == "" || platformMessageID == "" {
		return ""
	}
	return channelType + ":" + platformConversationID + ":" + platformMessageID
}

func resolveInboundBurstPersonaRef(ctx context.Context, personasRepo *data.PersonasRepository, ch data.Channel) (string, error) {
	if personasRepo == nil {
		return "", fmt.Errorf("personas repo not configured")
	}
	if ch.PersonaID == nil || *ch.PersonaID == uuid.Nil {
		return "", fmt.Errorf("channel %s missing persona_id", ch.ID.String())
	}
	persona, err := personasRepo.GetByIDForAccount(ctx, ch.AccountID, *ch.PersonaID)
	if err != nil {
		return "", err
	}
	if persona == nil || !persona.IsActive {
		return "", fmt.Errorf("persona not found or inactive")
	}
	if persona.ProjectID == nil || *persona.ProjectID == uuid.Nil {
		return "", fmt.Errorf("channel persona must belong to a project")
	}
	return buildPersonaRef(*persona), nil
}

func buildChannelBurstRunStartedData(personaRef string, defaultModel string, jobPayload map[string]any) map[string]any {
	channelDelivery, _ := jobPayload["channel_delivery"].(map[string]any)
	return buildChannelRunStartedData(personaRef, defaultModel, channelDelivery)
}

func resolveChannelBurstDefaultModel(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	model, _ := payload["default_model"].(string)
	return strings.TrimSpace(model)
}

func buildChannelBurstJobPayload(ch data.Channel, entry data.ChannelInboundLedgerEntry) (map[string]any, error) {
	if entry.SenderChannelIdentityID == nil || *entry.SenderChannelIdentityID == uuid.Nil {
		return nil, fmt.Errorf("pending batch entry missing sender_channel_identity_id")
	}
	platformChatID := strings.TrimSpace(entry.PlatformConversationID)
	platformMessageID := strings.TrimSpace(entry.PlatformMessageID)
	if platformChatID == "" || platformMessageID == "" {
		return nil, fmt.Errorf("pending batch entry missing platform ids")
	}

	conversationRef := map[string]any{"target": platformChatID}
	if entry.PlatformThreadID != nil && strings.TrimSpace(*entry.PlatformThreadID) != "" {
		threadID := strings.TrimSpace(*entry.PlatformThreadID)
		conversationRef["thread_id"] = threadID
	}

	payload := map[string]any{
		"source": "channel_burst_recovery",
		"channel_delivery": map[string]any{
			"channel_id":   ch.ID.String(),
			"channel_type": ch.ChannelType,
			"conversation_ref": map[string]any{
				"target": conversationRef["target"],
			},
			"inbound_message_ref": map[string]any{
				"message_id": platformMessageID,
			},
			"trigger_message_ref": map[string]any{
				"message_id": platformMessageID,
			},
			"platform_chat_id":           platformChatID,
			"platform_message_id":        platformMessageID,
			"sender_channel_identity_id": entry.SenderChannelIdentityID.String(),
		},
	}
	delivery := payload["channel_delivery"].(map[string]any)
	if threadID, ok := conversationRef["thread_id"].(string); ok && strings.TrimSpace(threadID) != "" {
		delivery["conversation_ref"].(map[string]any)["thread_id"] = threadID
		delivery["message_thread_id"] = threadID
	}
	if entry.PlatformParentMessageID != nil && strings.TrimSpace(*entry.PlatformParentMessageID) != "" {
		delivery["inbound_reply_to_message_id"] = strings.TrimSpace(*entry.PlatformParentMessageID)
	}
	if conversationType, ok := inboundLedgerString(entry.MetadataJSON, "conversation_type"); ok {
		delivery["conversation_type"] = conversationType
	} else {
		delivery["conversation_type"] = "private"
	}
	if mentionsBot, ok := inboundLedgerBool(entry.MetadataJSON, "mentions_bot"); ok {
		delivery["mentions_bot"] = mentionsBot
	}
	if replyToBot, ok := inboundLedgerBool(entry.MetadataJSON, "is_reply_to_bot"); ok {
		delivery["is_reply_to_bot"] = replyToBot
	}
	return payload, nil
}

func extendPendingInboundBurstWindowTx(
	ctx context.Context,
	repo *data.ChannelMessageLedgerRepository,
	channelID uuid.UUID,
	threadID uuid.UUID,
	now time.Time,
) error {
	entries, err := repo.ListInboundEntriesByThreadState(ctx, channelID, threadID, inboundStatePendingDispatch, true)
	if err != nil {
		return err
	}
	dispatchAfterUnixMs := nextInboundBurstDispatchAfter(now)
	for _, entry := range entries {
		metadata := applyInboundBurstMetadata(entry.MetadataJSON, dispatchAfterUnixMs)
		if _, err := repo.UpdateInboundEntry(
			ctx,
			channelID,
			entry.PlatformConversationID,
			entry.PlatformMessageID,
			entry.ThreadID,
			nil,
			entry.MessageID,
			metadata,
		); err != nil {
			return err
		}
	}
	return nil
}

func shouldMergePassiveInboundIntoPendingBatchTx(
	ctx context.Context,
	repo *data.ChannelMessageLedgerRepository,
	channelID uuid.UUID,
	threadID uuid.UUID,
	now time.Time,
) (bool, error) {
	if repo == nil || channelID == uuid.Nil || threadID == uuid.Nil {
		return false, nil
	}
	pendingEntries, err := listPendingInboundBatchTx(ctx, repo, channelID, threadID)
	if err != nil {
		return false, err
	}
	if len(pendingEntries) == 0 {
		return false, nil
	}
	return !pendingBatchReady(pendingEntries, now.UTC()), nil
}

func promoteRecentPassiveInboundToPendingTx(
	ctx context.Context,
	repo *data.ChannelMessageLedgerRepository,
	channelID uuid.UUID,
	threadID uuid.UUID,
	now time.Time,
) error {
	if repo == nil || channelID == uuid.Nil || threadID == uuid.Nil {
		return nil
	}
	now = now.UTC()
	cutoff := now.Add(-channelInboundBurstWindow)
	dispatchAfterUnixMs := nextInboundBurstDispatchAfter(now)

	passiveEntries, err := repo.ListInboundEntriesByThreadState(ctx, channelID, threadID, inboundStatePassivePersisted, true)
	if err != nil {
		return err
	}
	for _, entry := range passiveEntries {
		if entry.CreatedAt.UTC().Before(cutoff) {
			continue
		}
		metadata := applyInboundBurstMetadata(applyInboundLedgerState(entry.MetadataJSON, inboundStatePendingDispatch), dispatchAfterUnixMs)
		if _, err := repo.UpdateInboundEntry(
			ctx,
			channelID,
			entry.PlatformConversationID,
			entry.PlatformMessageID,
			entry.ThreadID,
			nil,
			entry.MessageID,
			metadata,
		); err != nil {
			return err
		}
	}
	return nil
}

func latestPendingBatchEntry(entries []data.ChannelInboundLedgerEntry) (data.ChannelInboundLedgerEntry, error) {
	if len(entries) == 0 {
		return data.ChannelInboundLedgerEntry{}, fmt.Errorf("pending inbound batch is empty")
	}
	return entries[len(entries)-1], nil
}

func markPendingBatchStateTx(
	ctx context.Context,
	repo *data.ChannelMessageLedgerRepository,
	channelID uuid.UUID,
	entries []data.ChannelInboundLedgerEntry,
	runID *uuid.UUID,
	state string,
) error {
	for _, entry := range entries {
		if _, err := repo.UpdateInboundEntry(
			ctx,
			channelID,
			entry.PlatformConversationID,
			entry.PlatformMessageID,
			entry.ThreadID,
			runID,
			entry.MessageID,
			applyInboundLedgerState(entry.MetadataJSON, state),
		); err != nil {
			return err
		}
	}
	return nil
}
