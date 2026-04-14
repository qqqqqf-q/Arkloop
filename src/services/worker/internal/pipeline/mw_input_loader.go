package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/rollout"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/imageutil"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/stablejson"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type MessageAttachmentStore interface {
	GetWithContentType(ctx context.Context, key string) ([]byte, string, error)
}

type runFirstEventLoader interface {
	FirstEventData(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (string, map[string]any, error)
}

type runRecoveryEventLoader interface {
	runFirstEventLoader
	GetLatestEventType(ctx context.Context, tx pgx.Tx, runID uuid.UUID, types []string) (string, error)
}

type runRecordLoader interface {
	GetRun(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (*data.Run, error)
}

type loadedRunInputs struct {
	InputJSON                 map[string]any
	Messages                  []llm.Message
	ThreadMessageIDs          []uuid.UUID
	HasActiveCompactSnapshot  bool
	ActiveCompactSnapshotText string
}

type LoadedRunInputs = loadedRunInputs

type resumeReplayInsertion struct {
	AnchorKey string
	Messages  []llm.Message
}

type resumeUnavailableError struct {
	reason string
}

func (e *resumeUnavailableError) Error() string {
	if e == nil || strings.TrimSpace(e.reason) == "" {
		return "resume context is unavailable"
	}
	return e.reason
}

const (
	resumeUnavailableErrorClass       = "resume.unavailable"
	interruptedToolErrorClass         = "tool.interrupted"
	interruptedToolErrorMessage       = "tool execution interrupted before result was recorded"
	runStartedThreadTailMessageIDKey  = "thread_tail_message_id"
	runStartedContinuationSourceKey   = "continuation_source"
	runStartedContinuationLoopKey     = "continuation_loop"
	runStartedContinuationResponseKey = "continuation_response"
)

const ResumeUnavailableErrorClass = resumeUnavailableErrorClass

// NewInputLoaderMiddleware 加载 run 的 inputJSON 和线程历史消息到 RunContext。
func NewInputLoaderMiddleware(
	runsRepo data.RunsRepository,
	eventsRepo data.RunEventsRepository,
	messagesRepo data.MessagesRepository,
	attachmentStore MessageAttachmentStore,
	rolloutStore objectstore.BlobStore,
) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		loaded, err := loadRunInputs(ctx, rc.Pool, rc.Run, rc.JobPayload, runsRepo, eventsRepo, messagesRepo, attachmentStore, rolloutStore, rc.ThreadMessageHistoryLimit)
		if err != nil {
			var resumeErr *resumeUnavailableError
			if errors.As(err, &resumeErr) {
				errorClass := resumeUnavailableErrorClass
				eventType := "run.failed"
				message := "resume context is unavailable"
				if IsRuntimeRecoveryJob(rc.JobPayload) {
					errorClass = "worker.recovery_unavailable"
					eventType = "run.interrupted"
					message = "runtime recovery state is unavailable"
				}
				terminal := rc.Emitter.Emit(eventType, map[string]any{
					"error_class": errorClass,
					"message":     message,
				}, nil, StringPtr(errorClass))
				return appendAndCommitSingle(ctx, rc.Pool, rc.Run, runsRepo, eventsRepo, terminal, rc.ReleaseSlot, rc.BroadcastRDB, rc.EventBus)
			}
			return err
		}

		rc.InputJSON = loaded.InputJSON
		rc.Messages, rc.ThreadMessageIDs = sanitizeToolPairs(loaded.Messages, loaded.ThreadMessageIDs)
		rc.HasActiveCompactSnapshot = loaded.HasActiveCompactSnapshot
		rc.ActiveCompactSnapshotText = loaded.ActiveCompactSnapshotText
		emitTraceEvent(rc, "input_loader", "input_loader.loaded", map[string]any{
			"run_kind":      strings.TrimSpace(stringValue(rc.InputJSON["run_kind"])),
			"message_count": len(rc.Messages),
			"history_limit": rc.ThreadMessageHistoryLimit,
		})

		return next(ctx, rc)
	}
}

func stringValue(value any) string {
	if raw, ok := value.(string); ok {
		return raw
	}
	return ""
}

func loadRunInputs(
	ctx context.Context,
	pool interface {
		BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
	},
	run data.Run,
	jobPayload map[string]any,
	runsRepo runRecordLoader,
	eventsRepo runRecoveryEventLoader,
	messagesRepo data.MessagesRepository,
	attachmentStore MessageAttachmentStore,
	rolloutStore objectstore.BlobStore,
	messageLimit int,
) (*loadedRunInputs, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, dataJSON, err := eventsRepo.FirstEventData(ctx, tx, run.ID)
	if err != nil {
		return nil, err
	}

	inputJSON := map[string]any{
		"account_id": run.AccountID.String(),
		"thread_id":  run.ThreadID.String(),
	}
	if dataJSON != nil {
		if rawRouteID, ok := dataJSON["route_id"].(string); ok && strings.TrimSpace(rawRouteID) != "" {
			inputJSON["route_id"] = strings.TrimSpace(rawRouteID)
		}
		if rawPersonaID, ok := dataJSON["persona_id"].(string); ok && strings.TrimSpace(rawPersonaID) != "" {
			inputJSON["persona_id"] = strings.TrimSpace(rawPersonaID)
		}
		if rawRole, ok := dataJSON["role"].(string); ok && strings.TrimSpace(rawRole) != "" {
			inputJSON["role"] = strings.TrimSpace(rawRole)
		}
		if rawOutputRouteID, ok := dataJSON["output_route_id"].(string); ok && strings.TrimSpace(rawOutputRouteID) != "" {
			inputJSON["output_route_id"] = strings.TrimSpace(rawOutputRouteID)
		}
		if rawModel, ok := dataJSON["model"].(string); ok && strings.TrimSpace(rawModel) != "" {
			inputJSON["model"] = strings.TrimSpace(rawModel)
		}
		if rawWorkDir, ok := dataJSON["work_dir"].(string); ok && strings.TrimSpace(rawWorkDir) != "" {
			inputJSON["work_dir"] = strings.TrimSpace(rawWorkDir)
		}
		if rawReasoningMode, ok := dataJSON["reasoning_mode"].(string); ok && strings.TrimSpace(rawReasoningMode) != "" {
			inputJSON["reasoning_mode"] = strings.TrimSpace(rawReasoningMode)
		}
		if rawContinuationSource, ok := dataJSON[runStartedContinuationSourceKey].(string); ok && strings.TrimSpace(rawContinuationSource) != "" {
			inputJSON[runStartedContinuationSourceKey] = strings.TrimSpace(rawContinuationSource)
		}
		if rawContinuationLoop, ok := dataJSON[runStartedContinuationLoopKey].(bool); ok {
			inputJSON[runStartedContinuationLoopKey] = rawContinuationLoop
		}
		if rawContinuationResponse, ok := dataJSON[runStartedContinuationResponseKey].(bool); ok {
			inputJSON[runStartedContinuationResponseKey] = rawContinuationResponse
		}
		if rawRunKind, ok := dataJSON["run_kind"].(string); ok && strings.TrimSpace(rawRunKind) != "" {
			inputJSON["run_kind"] = strings.TrimSpace(rawRunKind)
		}
		if rawThreadTailID, ok := dataJSON[runStartedThreadTailMessageIDKey].(string); ok && strings.TrimSpace(rawThreadTailID) != "" {
			inputJSON[runStartedThreadTailMessageIDKey] = strings.TrimSpace(rawThreadTailID)
		}
		if rawChannelDelivery, ok := dataJSON["channel_delivery"].(map[string]any); ok && len(rawChannelDelivery) > 0 {
			inputJSON["channel_delivery"] = rawChannelDelivery
		}
	}
	_ = isHeartbeatRun(inputJSON, jobPayload)

	historyUpperBoundID, hasHistoryUpperBound, err := boundedThreadHistoryUpperBound(ctx, tx, inputJSON, jobPayload)
	if err != nil {
		return nil, err
	}
	if hasHistoryUpperBound {
		inputJSON[runStartedThreadTailMessageIDKey] = historyUpperBoundID.String()
	}

	var upperBoundMessageID *uuid.UUID
	if hasHistoryUpperBound {
		upperBoundMessageID = &historyUpperBoundID
	}
	canonicalContext, err := buildCanonicalThreadContext(
		ctx,
		tx,
		run,
		messagesRepo,
		attachmentStore,
		upperBoundMessageID,
		messageLimit,
	)
	if err != nil {
		return nil, err
	}
	messages := canonicalContext.VisibleMessages
	hasActiveSnapshot := canonicalContext.HasLeadingCompactSummary

	replayInsertions := []resumeReplayInsertion(nil)
	if IsRuntimeRecoveryJob(jobPayload) {
		replayInsertions, err = loadRuntimeRecoveryReplay(ctx, tx, run, eventsRepo, rolloutStore, canonicalContext, messages)
		if err != nil {
			return nil, err
		}
	} else if run.ResumeFromRunID != nil {
		replayInsertions, err = loadResumedReplay(ctx, tx, run, runsRepo, eventsRepo, rolloutStore, canonicalContext, messages)
		if err != nil {
			var resumeErr *resumeUnavailableError
			if errors.As(err, &resumeErr) {
				clearContinuationMetadata(inputJSON)
				replayInsertions = nil
			} else {
				return nil, err
			}
		}
	}

	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return nil, err
	}

	replayCount := 0
	replayByAnchor := make(map[string][]resumeReplayInsertion, len(replayInsertions))
	for _, insertion := range replayInsertions {
		replayByAnchor[insertion.AnchorKey] = append(replayByAnchor[insertion.AnchorKey], insertion)
		replayCount += len(insertion.Messages)
	}

	activeSnapshotText := strings.TrimSpace(canonicalContext.LeadingCompactSummary)
	llmMessages := make([]llm.Message, 0, len(canonicalContext.Messages)+replayCount)
	ids := make([]uuid.UUID, 0, len(canonicalContext.ThreadMessageIDs)+replayCount)
	for _, entry := range canonicalContext.Entries {
		llmMessages = append(llmMessages, entry.Message)
		ids = append(ids, entry.ThreadMessageID)
		for _, insertion := range replayByAnchor[entry.AnchorKey] {
			llmMessages = append(llmMessages, insertion.Messages...)
			for range insertion.Messages {
				ids = append(ids, uuid.Nil)
			}
		}
	}

	// 提取最后一条用户消息，供 Lua 脚本通过 context.get("last_user_message") 访问
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" && strings.TrimSpace(messages[i].Content) != "" {
			inputJSON["last_user_message"] = strings.TrimSpace(messages[i].Content)
			break
		}
	}

	return &loadedRunInputs{
		InputJSON:                 inputJSON,
		Messages:                  llmMessages,
		ThreadMessageIDs:          ids,
		HasActiveCompactSnapshot:  hasActiveSnapshot,
		ActiveCompactSnapshotText: activeSnapshotText,
	}, nil
}

func LoadRunInputs(
	ctx context.Context,
	pool interface {
		BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
	},
	run data.Run,
	jobPayload map[string]any,
	runsRepo runRecordLoader,
	eventsRepo runRecoveryEventLoader,
	messagesRepo data.MessagesRepository,
	attachmentStore MessageAttachmentStore,
	rolloutStore objectstore.BlobStore,
	messageLimit int,
) (*LoadedRunInputs, error) {
	return loadRunInputs(ctx, pool, run, jobPayload, runsRepo, eventsRepo, messagesRepo, attachmentStore, rolloutStore, messageLimit)
}

func IsResumeUnavailableError(err error) bool {
	var resumeErr *resumeUnavailableError
	return errors.As(err, &resumeErr)
}

func clearContinuationMetadata(inputJSON map[string]any) {
	if inputJSON == nil {
		return
	}
	inputJSON[runStartedContinuationSourceKey] = "none"
	inputJSON[runStartedContinuationLoopKey] = false
	delete(inputJSON, runStartedContinuationResponseKey)
}

func boundedThreadHistoryUpperBound(ctx context.Context, tx pgx.Tx, inputJSON map[string]any, jobPayload map[string]any) (uuid.UUID, bool, error) {
	if !isBoundedChannelHistoryRun(inputJSON, jobPayload) {
		return uuid.Nil, false, nil
	}
	if id, ok, err := threadHistoryUpperBoundFromValues(inputJSON, jobPayload); err != nil {
		return uuid.Nil, false, err
	} else if ok {
		return id, true, nil
	}
	if tx == nil {
		return uuid.Nil, false, nil
	}
	return lookupChannelHistoryUpperBoundFromLedger(ctx, tx, inputJSON, jobPayload)
}

func isBoundedChannelHistoryRun(inputJSON map[string]any, jobPayload map[string]any) bool {
	if IsRuntimeRecoveryJob(jobPayload) {
		return false
	}
	if continuationSource, _ := inputJSON[runStartedContinuationSourceKey].(string); strings.TrimSpace(continuationSource) != "" && strings.TrimSpace(continuationSource) != "none" {
		return false
	}
	if isHeartbeatRun(inputJSON, jobPayload) {
		return false
	}
	return hasChannelDeliveryPayload(inputJSON) || hasChannelDeliveryPayload(jobPayload)
}

func hasChannelDeliveryPayload(values map[string]any) bool {
	if len(values) == 0 {
		return false
	}
	raw, ok := values["channel_delivery"].(map[string]any)
	return ok && len(raw) > 0
}

func threadHistoryUpperBoundFromValues(values ...map[string]any) (uuid.UUID, bool, error) {
	for _, value := range values {
		raw, _ := value[runStartedThreadTailMessageIDKey].(string)
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			return uuid.Nil, false, fmt.Errorf("invalid thread history upper bound message id: %w", err)
		}
		return id, true, nil
	}
	return uuid.Nil, false, nil
}

func lookupChannelHistoryUpperBoundFromLedger(ctx context.Context, tx pgx.Tx, inputJSON map[string]any, jobPayload map[string]any) (uuid.UUID, bool, error) {
	channelDelivery := channelDeliveryPayload(inputJSON, jobPayload)
	if len(channelDelivery) == 0 {
		return uuid.Nil, false, nil
	}
	channelID, err := requiredUUIDValue(channelDelivery, "channel_id")
	if err != nil {
		return uuid.Nil, false, nil
	}
	conversationRef, err := parseConversationRef(channelDelivery)
	if err != nil || strings.TrimSpace(conversationRef.Target) == "" {
		return uuid.Nil, false, nil
	}
	candidates := []string{}
	if triggerRef, err := parseOptionalMessageRef(channelDelivery, "trigger_message_ref", "reply_to_message_id"); err == nil && triggerRef != nil && strings.TrimSpace(triggerRef.MessageID) != "" {
		candidates = append(candidates, strings.TrimSpace(triggerRef.MessageID))
	}
	if inboundRef, err := parseOptionalInboundMessageRef(channelDelivery); err == nil && strings.TrimSpace(inboundRef.MessageID) != "" {
		candidates = append(candidates, strings.TrimSpace(inboundRef.MessageID))
	}
	seen := map[string]struct{}{}
	for _, platformMessageID := range candidates {
		if _, ok := seen[platformMessageID]; ok {
			continue
		}
		seen[platformMessageID] = struct{}{}
		var messageID *uuid.UUID
		err := tx.QueryRow(
			ctx,
			`SELECT message_id
			   FROM channel_message_ledger
			  WHERE channel_id = $1
			    AND direction = 'inbound'
			    AND platform_conversation_id = $2
			    AND platform_message_id = $3
			    AND message_id IS NOT NULL
			  ORDER BY created_at DESC
			  LIMIT 1`,
			channelID,
			strings.TrimSpace(conversationRef.Target),
			platformMessageID,
		).Scan(&messageID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return uuid.Nil, false, fmt.Errorf("lookup bounded channel history upper bound: %w", err)
		}
		if messageID != nil && *messageID != uuid.Nil {
			return *messageID, true, nil
		}
	}
	return uuid.Nil, false, nil
}

func channelDeliveryPayload(values ...map[string]any) map[string]any {
	for _, value := range values {
		raw, ok := value["channel_delivery"].(map[string]any)
		if ok && len(raw) > 0 {
			return raw
		}
	}
	return nil
}

func loadResumedReplay(
	ctx context.Context,
	tx pgx.Tx,
	run data.Run,
	runsRepo runRecordLoader,
	eventsRepo runFirstEventLoader,
	rolloutStore objectstore.BlobStore,
	canonicalContext *canonicalThreadContext,
	threadMessages []data.ThreadMessage,
) ([]resumeReplayInsertion, error) {
	if run.ResumeFromRunID == nil {
		return nil, nil
	}
	if rolloutStore == nil {
		return nil, &resumeUnavailableError{reason: "resume rollout store is unavailable"}
	}
	if runsRepo == nil {
		return nil, &resumeUnavailableError{reason: "resume run repository is unavailable"}
	}
	if eventsRepo == nil {
		return nil, &resumeUnavailableError{reason: "resume event repository is unavailable"}
	}

	insertions, err := collectResumeReplayInsertions(
		ctx,
		tx,
		run.AccountID,
		run.ThreadID,
		*run.ResumeFromRunID,
		runsRepo,
		eventsRepo,
		rolloutStore,
		canonicalContext,
		threadMessages,
		map[uuid.UUID]struct{}{},
	)
	if err != nil {
		return nil, err
	}
	return insertions, nil
}

func IsRuntimeRecoveryJob(jobPayload map[string]any) bool {
	if len(jobPayload) == 0 {
		return false
	}
	if raw, _ := jobPayload["recovery_source"].(string); strings.TrimSpace(raw) == "runtime_recovery" {
		return true
	}
	if raw, _ := jobPayload["source"].(string); strings.TrimSpace(raw) == "desktop_recovery" {
		return true
	}
	return false
}

func resumeAnchorMessageID(dataJSON map[string]any) (uuid.UUID, error) {
	if dataJSON == nil {
		return uuid.Nil, &resumeUnavailableError{reason: "resume source run has no thread tail anchor"}
	}
	raw, _ := dataJSON[runStartedThreadTailMessageIDKey].(string)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return uuid.Nil, &resumeUnavailableError{reason: "resume source run has no thread tail anchor"}
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, &resumeUnavailableError{reason: "resume source run has invalid thread tail anchor"}
	}
	return id, nil
}

func trailingResumeUserBlockAfterMessage(messages []data.ThreadMessage, anchorMessageID uuid.UUID) (int, bool) {
	if len(messages) == 0 {
		return 0, false
	}
	anchorIndex := -1
	for i, msg := range messages {
		if msg.ID == anchorMessageID {
			anchorIndex = i
			break
		}
	}
	if anchorIndex < 0 || anchorIndex == len(messages)-1 {
		return 0, false
	}
	for i := anchorIndex + 1; i < len(messages); i++ {
		if messages[i].Role != "user" {
			return 0, false
		}
	}
	return anchorIndex + 1, true
}

func resumeInsertionAnchor(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	threadID uuid.UUID,
	canonicalContext *canonicalThreadContext,
	visibleMessages []data.ThreadMessage,
	anchorMessageID uuid.UUID,
	allowVisibleTail bool,
) (string, bool, error) {
	renderedAnchorKey := renderedMessageAnchorKey(canonicalContext.Entries, anchorMessageID)
	if allowVisibleTail {
		if renderedAnchorKey != "" && isLastRenderedMessage(canonicalContext.Entries, anchorMessageID) {
			return renderedAnchorKey, true, nil
		}
	}
	if _, ok := trailingResumeUserBlockAfterMessage(visibleMessages, anchorMessageID); ok {
		if renderedAnchorKey != "" {
			return renderedAnchorKey, true, nil
		}
	}
	if tx == nil || accountID == uuid.Nil || threadID == uuid.Nil || anchorMessageID == uuid.Nil {
		return "", false, nil
	}
	var threadSeq int64
	err := tx.QueryRow(
		ctx,
		`SELECT thread_seq
		   FROM messages
		  WHERE account_id = $1
		    AND thread_id = $2
		    AND id = $3
		    AND deleted_at IS NULL
		  LIMIT 1`,
		accountID,
		threadID,
		anchorMessageID,
	).Scan(&threadSeq)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	if replacementAnchor := replacementAnchorKeyForThreadSeq(canonicalContext.Entries, threadSeq); replacementAnchor != "" {
		return replacementAnchor, true, nil
	}
	if renderedAnchorKey != "" {
		return renderedAnchorKey, true, nil
	}
	return "", false, nil
}

func collectResumeReplayInsertions(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	threadID uuid.UUID,
	runID uuid.UUID,
	runsRepo runRecordLoader,
	eventsRepo runFirstEventLoader,
	rolloutStore objectstore.BlobStore,
	canonicalContext *canonicalThreadContext,
	threadMessages []data.ThreadMessage,
	visited map[uuid.UUID]struct{},
) ([]resumeReplayInsertion, error) {
	if _, ok := visited[runID]; ok {
		return nil, &resumeUnavailableError{reason: "resume source run chain has a cycle"}
	}
	visited[runID] = struct{}{}

	parentRun, err := runsRepo.GetRun(ctx, tx, runID)
	if err != nil {
		return nil, err
	}
	if parentRun == nil {
		return nil, &resumeUnavailableError{reason: "resume source run does not exist"}
	}
	if parentRun.ThreadID != threadID {
		return nil, &resumeUnavailableError{reason: "resume source run does not belong to the thread"}
	}
	if parentRun.Status != "interrupted" && parentRun.Status != "cancelled" {
		return nil, &resumeUnavailableError{reason: "resume source run is not resumable"}
	}

	insertions := []resumeReplayInsertion(nil)
	if parentRun.ResumeFromRunID != nil {
		insertions, err = collectResumeReplayInsertions(
			ctx,
			tx,
			accountID,
			threadID,
			*parentRun.ResumeFromRunID,
			runsRepo,
			eventsRepo,
			rolloutStore,
			canonicalContext,
			threadMessages,
			visited,
		)
		if err != nil {
			return nil, err
		}
	}

	_, parentStartedData, err := eventsRepo.FirstEventData(ctx, tx, parentRun.ID)
	if err != nil {
		return nil, err
	}
	anchorMessageID, err := resumeAnchorMessageID(parentStartedData)
	if err != nil {
		return nil, err
	}
	insertionAnchorKey, ok, err := resumeInsertionAnchor(ctx, tx, accountID, threadID, canonicalContext, threadMessages, anchorMessageID, false)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &resumeUnavailableError{reason: "resume input block is missing"}
	}

	items, err := rollout.NewReader(rolloutStore).ReadRollout(ctx, parentRun.ID)
	if err != nil {
		return nil, &resumeUnavailableError{reason: "resume rollout is unavailable"}
	}
	state := rollout.NewReader(rolloutStore).Reconstruct(items)
	replayedMessages, err := buildReplayMessages(state)
	if err != nil {
		return nil, err
	}
	if !canonicalThreadHasAssistantMessageForRun(canonicalContext, threadMessages, parentRun.ID) {
		if err := appendVisibleRecoveryDraft(ctx, tx, parentRun.ID, rolloutStore, &replayedMessages); err != nil {
			return nil, err
		}
	}
	return append(insertions, resumeReplayInsertion{
		AnchorKey: insertionAnchorKey,
		Messages:  replayedMessages,
	}), nil
}

func loadRuntimeRecoveryReplay(
	ctx context.Context,
	tx pgx.Tx,
	run data.Run,
	eventsRepo runRecoveryEventLoader,
	rolloutStore objectstore.BlobStore,
	canonicalContext *canonicalThreadContext,
	threadMessages []data.ThreadMessage,
) ([]resumeReplayInsertion, error) {
	hasRecoverableOutput, err := runtimeRecoveryHasRecoverableOutput(ctx, tx, eventsRepo, run.ID)
	if err != nil {
		return nil, err
	}
	if !hasRecoverableOutput {
		return nil, nil
	}
	if rolloutStore == nil {
		return nil, &resumeUnavailableError{reason: "runtime recovery store is unavailable"}
	}
	_, startedData, err := eventsRepo.FirstEventData(ctx, tx, run.ID)
	if err != nil {
		return nil, err
	}
	anchorMessageID, err := resumeAnchorMessageID(startedData)
	if err != nil {
		return nil, err
	}
	insertionAnchorKey, ok, err := resumeInsertionAnchor(ctx, tx, run.AccountID, run.ThreadID, canonicalContext, threadMessages, anchorMessageID, true)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &resumeUnavailableError{reason: "runtime recovery input block is missing"}
	}
	items, err := rollout.NewReader(rolloutStore).ReadRollout(ctx, run.ID)
	if err != nil && !objectstore.IsNotFound(err) {
		return nil, &resumeUnavailableError{reason: "runtime recovery rollout is unavailable"}
	}
	state := rollout.NewReader(rolloutStore).Reconstruct(items)
	replayedMessages, err := buildReplayMessages(state)
	if err != nil {
		return nil, err
	}
	if !canonicalThreadHasAssistantMessageForRun(canonicalContext, threadMessages, run.ID) {
		if err := appendVisibleRecoveryDraft(ctx, tx, run.ID, rolloutStore, &replayedMessages); err != nil {
			return nil, err
		}
	}
	if len(replayedMessages) == 0 {
		return nil, &resumeUnavailableError{reason: "runtime recovery state is unavailable"}
	}
	return []resumeReplayInsertion{{
		AnchorKey: insertionAnchorKey,
		Messages:  replayedMessages,
	}}, nil
}

func runtimeRecoveryHasRecoverableOutput(
	ctx context.Context,
	tx pgx.Tx,
	eventsRepo runRecoveryEventLoader,
	runID uuid.UUID,
) (bool, error) {
	if eventsRepo == nil || runID == uuid.Nil {
		return false, nil
	}
	eventType, err := eventsRepo.GetLatestEventType(ctx, tx, runID, []string{
		"message.delta",
		"tool.call",
		"tool.result",
	})
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(eventType) != "", nil
}

func canonicalThreadHasAssistantMessageForRun(
	canonicalContext *canonicalThreadContext,
	messages []data.ThreadMessage,
	runID uuid.UUID,
) bool {
	if canonicalContext == nil || runID == uuid.Nil {
		return false
	}
	rendered := make(map[uuid.UUID]struct{}, len(canonicalContext.ThreadMessageIDs))
	for _, messageID := range canonicalContext.ThreadMessageIDs {
		if messageID == uuid.Nil {
			continue
		}
		rendered[messageID] = struct{}{}
	}
	want := runID.String()
	for _, msg := range messages {
		if _, ok := rendered[msg.ID]; !ok {
			continue
		}
		if msg.Role != "assistant" || len(msg.MetadataJSON) == 0 {
			continue
		}
		var metadata map[string]any
		if err := json.Unmarshal(msg.MetadataJSON, &metadata); err != nil {
			continue
		}
		rawRunID, _ := metadata["run_id"].(string)
		if strings.TrimSpace(rawRunID) == want {
			return true
		}
	}
	return false
}

func buildReplayMessages(state *rollout.ReconstructedState) ([]llm.Message, error) {
	if state == nil {
		return nil, nil
	}
	replayed := make([]llm.Message, 0, len(state.ReplayMessages)+len(state.PendingToolCalls))
	for _, msg := range state.ReplayMessages {
		var rebuilt llm.Message
		var keep bool
		switch msg.Role {
		case "assistant":
			if msg.Assistant == nil {
				continue
			}
			rebuilt = replayAssistantMessage(*msg.Assistant)
		case "tool":
			if msg.Tool == nil {
				continue
			}
			rebuilt = replayToolResultMessage(*msg.Tool)
		default:
			continue
		}
		rebuilt, keep = filterLongTermHeartbeatDecision(rebuilt)
		if keep {
			replayed = append(replayed, rebuilt)
		}
	}
	for _, call := range state.PendingToolCalls {
		rebuilt := replayToolResultMessage(rollout.ReplayToolResult{
			CallID:    call.CallID,
			Name:      call.Name,
			Error:     interruptedToolErrorMessage,
			Synthetic: true,
		})
		rebuilt, keep := filterLongTermHeartbeatDecision(rebuilt)
		if keep {
			replayed = append(replayed, rebuilt)
		}
	}
	return replayed, nil
}

func replayAssistantMessage(msg rollout.AssistantMessage) llm.Message {
	var toolCalls []llm.ToolCall
	if len(msg.ToolCalls) > 0 {
		if err := json.Unmarshal(msg.ToolCalls, &toolCalls); err != nil {
			slog.Warn("rollout: failed to parse assistant tool_calls", "err", err)
		}
	}
	toolCalls = llm.CanonicalToolCalls(toolCalls)
	content := []llm.ContentPart(nil)
	text := sanitizeStoredAssistantText(msg.Content)
	if strings.TrimSpace(text) != "" {
		content = []llm.ContentPart{{Type: messagecontent.PartTypeText, Text: text}}
	}
	return llm.Message{
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
	}
}

func replayToolResultMessage(result rollout.ReplayToolResult) llm.Message {
	envelope := map[string]any{
		"tool_call_id": result.CallID,
	}
	if toolName := llm.CanonicalToolName(result.Name); toolName != "" {
		envelope["tool_name"] = toolName
	}
	if len(result.Output) > 0 {
		var output any
		if err := json.Unmarshal(result.Output, &output); err == nil {
			envelope["result"] = output
		}
	}
	if strings.TrimSpace(result.Error) != "" {
		errorClass := interruptedToolErrorClass
		if !result.Synthetic {
			errorClass = "tool.error"
		}
		envelope["error"] = map[string]any{
			"error_class": errorClass,
			"message":     strings.TrimSpace(result.Error),
		}
	}
	text, err := stablejson.Encode(envelope)
	if err != nil {
		encoded, _ := json.Marshal(envelope)
		text = string(encoded)
	}
	return llm.Message{
		Role:    "tool",
		Content: []llm.TextPart{{Text: text, TrustSource: "tool"}},
	}
}

func parseToolCallsFromContentJSON(raw json.RawMessage) []llm.ToolCall {
	var parsed struct {
		ToolCalls []map[string]any `json:"tool_calls"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.ToolCalls) == 0 {
		return nil
	}
	result := make([]llm.ToolCall, 0, len(parsed.ToolCalls))
	for _, tc := range parsed.ToolCalls {
		toolCall, err := llm.ToolCallFromJSONMap(tc)
		if err != nil {
			continue
		}
		result = append(result, llm.CanonicalToolCall(toolCall))
	}
	return result
}

func filterLongTermHeartbeatDecision(msg llm.Message) (llm.Message, bool) {
	if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
		filtered := make([]llm.ToolCall, 0, len(msg.ToolCalls))
		for _, call := range msg.ToolCalls {
			if IsHeartbeatDecisionToolName(call.ToolName) {
				continue
			}
			filtered = append(filtered, call)
		}
		msg.ToolCalls = filtered
	}
	if msg.Role == "tool" && toolMessageIsHeartbeatDecision(msg) {
		return llm.Message{}, false
	}
	if msg.Role == "assistant" && len(msg.ToolCalls) == 0 && len(msg.Content) == 0 {
		return llm.Message{}, false
	}
	return msg, true
}

func toolMessageIsHeartbeatDecision(msg llm.Message) bool {
	if msg.Role != "tool" || len(msg.Content) == 0 {
		return false
	}
	var envelope struct {
		ToolName string `json:"tool_name"`
	}
	if json.Unmarshal([]byte(msg.Content[0].Text), &envelope) != nil {
		return false
	}
	return IsHeartbeatDecisionToolName(envelope.ToolName)
}

func canonicalizeToolMessageParts(parts []llm.ContentPart) []llm.ContentPart {
	if len(parts) == 0 {
		return nil
	}
	out := append([]llm.ContentPart(nil), parts...)
	for i := range out {
		if out[i].Kind() != messagecontent.PartTypeText {
			continue
		}
		out[i].Text = llm.CanonicalizeToolEnvelopeText(out[i].Text)
	}
	return out
}

func BuildMessageParts(ctx context.Context, store MessageAttachmentStore, msg data.ThreadMessage) ([]llm.ContentPart, error) {
	fallbackContent := msg.Content
	if msg.Role == "assistant" {
		fallbackContent = sanitizeStoredAssistantText(fallbackContent)
	}
	if len(msg.ContentJSON) == 0 {
		return fallbackTextParts(fallbackContent), nil
	}
	parsed, err := messagecontent.Parse(msg.ContentJSON)
	if err != nil {
		return fallbackTextParts(fallbackContent), nil
	}
	content, err := messagecontent.Normalize(parsed.Parts)
	if err != nil {
		return fallbackTextParts(fallbackContent), nil
	}
	parts := make([]llm.ContentPart, 0, len(content.Parts))
	for _, part := range content.Parts {
		switch part.Type {
		case messagecontent.PartTypeText:
			text := part.Text
			if msg.Role == "assistant" {
				text = sanitizeStoredAssistantText(text)
			}
			if strings.TrimSpace(text) == "" {
				continue
			}
			parts = append(parts, llm.ContentPart{Type: messagecontent.PartTypeText, Text: text})
		case messagecontent.PartTypeFile:
			parts = append(parts, llm.ContentPart{
				Type:          messagecontent.PartTypeFile,
				Attachment:    part.Attachment,
				ExtractedText: part.ExtractedText,
			})
		case messagecontent.PartTypeImage:
			if part.Attachment == nil {
				return nil, fmt.Errorf("message image attachment is required")
			}
			if store == nil {
				return nil, fmt.Errorf("message attachment store not configured")
			}
			dataBytes, contentType, err := store.GetWithContentType(ctx, part.Attachment.Key)
			if err != nil {
				if objectstore.IsNotFound(err) {
					return nil, fmt.Errorf("message attachment not found")
				}
				return nil, err
			}
			attachment := *part.Attachment
			if strings.TrimSpace(contentType) != "" {
				attachment.MimeType = strings.TrimSpace(contentType)
			}
			dataBytes, attachment.MimeType = imageutil.ProcessImage(dataBytes, attachment.MimeType)
			parts = append(parts, llm.ContentPart{
				Type:       messagecontent.PartTypeImage,
				Attachment: &attachment,
				Data:       dataBytes,
			})
		}
	}
	if len(parts) == 0 {
		return fallbackTextParts(fallbackContent), nil
	}
	return parts, nil
}

func fallbackTextParts(content string) []llm.ContentPart {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	return []llm.ContentPart{{Type: messagecontent.PartTypeText, Text: content}}
}

func sanitizeStoredAssistantText(text string) string {
	return strings.ReplaceAll(text, "<end_turn>", "")
}

func loadVisibleSeqCutoff(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (int64, bool, error) {
	var raw []byte
	err := tx.QueryRow(ctx,
		`SELECT data_json FROM run_events
		 WHERE run_id = $1 AND type = 'run.cancel_requested'
		 ORDER BY seq DESC LIMIT 1`,
		runID,
	).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, err
	}
	if len(raw) == 0 {
		return 0, true, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0, true, nil
	}
	switch value := payload["visible_seq_cutoff"].(type) {
	case float64:
		return int64(value), true, nil
	case json.Number:
		i, err := value.Int64()
		if err != nil {
			return 0, true, nil
		}
		return i, true, nil
	case int64:
		return value, true, nil
	case int:
		return int64(value), true, nil
	default:
		return 0, true, nil
	}
}

func loadVisibleAssistantOutput(ctx context.Context, tx pgx.Tx, runID uuid.UUID, cutoff int64) (string, error) {
	if cutoff <= 0 {
		return "", nil
	}
	query := `
		SELECT data_json FROM run_events
		WHERE run_id = $1 AND type = 'message.delta' AND seq <= $2
		ORDER BY seq ASC
	`
	rows, err := tx.Query(ctx, query, runID, cutoff)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var builder strings.Builder
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return "", err
		}
		if len(raw) == 0 {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			continue
		}
		if delta := extractVisibleAssistantDelta(payload); delta != "" {
			builder.WriteString(delta)
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return builder.String(), nil
}

func extractVisibleAssistantDelta(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	channel, _ := payload["channel"].(string)
	if strings.TrimSpace(channel) != "" {
		return ""
	}
	if role, _ := payload["role"].(string); role != "" && role != "assistant" {
		return ""
	}
	delta, _ := payload["content_delta"].(string)
	if strings.TrimSpace(delta) == "" || strings.TrimSpace(delta) == "<end_turn>" {
		return ""
	}
	return delta
}

func appendVisibleRecoveryDraft(
	ctx context.Context,
	tx pgx.Tx,
	runID uuid.UUID,
	rolloutStore objectstore.BlobStore,
	messages *[]llm.Message,
) error {
	cutoff, hasCutoff, err := loadVisibleSeqCutoff(ctx, tx, runID)
	if err != nil {
		return err
	}
	if hasCutoff {
		visibleContent, err := loadVisibleAssistantOutput(ctx, tx, runID, cutoff)
		if err != nil {
			return err
		}
		if visibleContent != "" {
			*messages = append(*messages, responseDraftMessage(visibleContent))
		}
		return nil
	}
	if rolloutStore == nil {
		return &resumeUnavailableError{reason: "response draft is unavailable"}
	}
	draft, err := readResponseDraft(ctx, rolloutStore, runID)
	if err != nil {
		return &resumeUnavailableError{reason: "response draft is unavailable"}
	}
	if draft != nil {
		*messages = append(*messages, responseDraftMessage(draft.Content))
	}
	return nil
}
