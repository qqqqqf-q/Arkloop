package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	AnchorMessageID uuid.UUID
	Messages        []llm.Message
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
		messageLimit := rc.ThreadMessageHistoryLimit
		if messageLimit <= 0 {
			messageLimit = 200
		}
		loaded, err := loadRunInputs(ctx, rc.Pool, rc.Run, rc.JobPayload, runsRepo, eventsRepo, messagesRepo, attachmentStore, rolloutStore, messageLimit)
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

		return next(ctx, rc)
	}
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
	defer tx.Rollback(ctx)

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
		if rawContinuationSource, ok := dataJSON[runStartedContinuationSourceKey].(string); ok && strings.TrimSpace(rawContinuationSource) != "" {
			inputJSON[runStartedContinuationSourceKey] = strings.TrimSpace(rawContinuationSource)
		}
		if rawContinuationLoop, ok := dataJSON[runStartedContinuationLoopKey].(bool); ok {
			inputJSON[runStartedContinuationLoopKey] = rawContinuationLoop
		}
		if rawContinuationResponse, ok := dataJSON[runStartedContinuationResponseKey].(bool); ok {
			inputJSON[runStartedContinuationResponseKey] = rawContinuationResponse
		}
	}

	messages, err := messagesRepo.ListByThread(ctx, tx, run.AccountID, run.ThreadID, messageLimit)
	if err != nil {
		return nil, err
	}
	activeSnapshot, err := (data.ThreadCompactionSnapshotsRepository{}).GetActiveByThread(ctx, tx, run.AccountID, run.ThreadID)
	if err != nil {
		return nil, err
	}
	hasActiveSnapshot := activeSnapshot != nil && strings.TrimSpace(activeSnapshot.SummaryText) != ""

	replayInsertions := []resumeReplayInsertion(nil)
	if IsRuntimeRecoveryJob(jobPayload) {
		replayInsertions, err = loadRuntimeRecoveryReplay(ctx, tx, run, eventsRepo, rolloutStore, messages, hasActiveSnapshot)
		if err != nil {
			return nil, err
		}
	} else if run.ResumeFromRunID != nil {
		replayInsertions, err = loadResumedReplay(ctx, tx, run, runsRepo, eventsRepo, rolloutStore, messages, hasActiveSnapshot)
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
	replayByAnchor := make(map[uuid.UUID][]resumeReplayInsertion, len(replayInsertions))
	for _, insertion := range replayInsertions {
		replayByAnchor[insertion.AnchorMessageID] = append(replayByAnchor[insertion.AnchorMessageID], insertion)
		replayCount += len(insertion.Messages)
	}

	activeSnapshotText := ""
	llmMessages := make([]llm.Message, 0, len(messages)+replayCount+1)
	ids := make([]uuid.UUID, 0, len(messages)+replayCount+1)
	if hasActiveSnapshot {
		activeSnapshotText = strings.TrimSpace(activeSnapshot.SummaryText)
		llmMessages = append(llmMessages, makeCompactSnapshotMessage(activeSnapshotText))
		ids = append(ids, uuid.Nil)
		for _, insertion := range replayByAnchor[uuid.Nil] {
			llmMessages = append(llmMessages, insertion.Messages...)
			for range insertion.Messages {
				ids = append(ids, uuid.Nil)
			}
		}
	}
	for _, msg := range messages {
		if strings.TrimSpace(msg.Role) == "" {
			continue
		}
		parts, err := BuildMessageParts(ctx, attachmentStore, msg)
		if err != nil {
			return nil, err
		}
		lm := llm.Message{
			Role:         msg.Role,
			Content:      parts,
			OutputTokens: msg.OutputTokens,
		}
		if msg.Role == "assistant" && len(msg.ContentJSON) > 0 {
			lm.ToolCalls = parseToolCallsFromContentJSON(msg.ContentJSON)
		}
		llmMessages = append(llmMessages, lm)
		ids = append(ids, msg.ID)
		for _, insertion := range replayByAnchor[msg.ID] {
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

func loadResumedReplay(
	ctx context.Context,
	tx pgx.Tx,
	run data.Run,
	runsRepo runRecordLoader,
	eventsRepo runFirstEventLoader,
	rolloutStore objectstore.BlobStore,
	threadMessages []data.ThreadMessage,
	hasActiveSnapshot bool,
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
		threadMessages,
		hasActiveSnapshot,
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
	visibleMessages []data.ThreadMessage,
	anchorMessageID uuid.UUID,
	hasActiveSnapshot bool,
	allowVisibleTail bool,
) (uuid.UUID, bool, error) {
	if allowVisibleTail {
		for idx, msg := range visibleMessages {
			if msg.ID == anchorMessageID && idx == len(visibleMessages)-1 {
				return anchorMessageID, true, nil
			}
		}
	}
	if _, ok := trailingResumeUserBlockAfterMessage(visibleMessages, anchorMessageID); ok {
		return anchorMessageID, true, nil
	}
	if !hasActiveSnapshot || tx == nil || accountID == uuid.Nil || threadID == uuid.Nil || anchorMessageID == uuid.Nil {
		return uuid.Nil, false, nil
	}
	var hidden bool
	err := tx.QueryRow(
		ctx,
		`SELECT hidden
		   FROM messages
		  WHERE account_id = $1
		    AND thread_id = $2
		    AND id = $3
		    AND deleted_at IS NULL
		  LIMIT 1`,
		accountID,
		threadID,
		anchorMessageID,
	).Scan(&hidden)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, err
	}
	if hidden {
		return uuid.Nil, true, nil
	}
	return uuid.Nil, false, nil
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
	threadMessages []data.ThreadMessage,
	hasActiveSnapshot bool,
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
			threadMessages,
			hasActiveSnapshot,
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
	insertionAnchorID, ok, err := resumeInsertionAnchor(ctx, tx, accountID, threadID, threadMessages, anchorMessageID, hasActiveSnapshot, false)
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
	if !threadHasAssistantMessageForRun(threadMessages, parentRun.ID) {
		if err := appendVisibleRecoveryDraft(ctx, tx, parentRun.ID, rolloutStore, &replayedMessages); err != nil {
			return nil, err
		}
	}
	return append(insertions, resumeReplayInsertion{
		AnchorMessageID: insertionAnchorID,
		Messages:        replayedMessages,
	}), nil
}

func loadRuntimeRecoveryReplay(
	ctx context.Context,
	tx pgx.Tx,
	run data.Run,
	eventsRepo runRecoveryEventLoader,
	rolloutStore objectstore.BlobStore,
	threadMessages []data.ThreadMessage,
	hasActiveSnapshot bool,
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
	insertionAnchorID, ok, err := resumeInsertionAnchor(ctx, tx, run.AccountID, run.ThreadID, threadMessages, anchorMessageID, hasActiveSnapshot, true)
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
	if !threadHasAssistantMessageForRun(threadMessages, run.ID) {
		if err := appendVisibleRecoveryDraft(ctx, tx, run.ID, rolloutStore, &replayedMessages); err != nil {
			return nil, err
		}
	}
	if len(replayedMessages) == 0 {
		return nil, &resumeUnavailableError{reason: "runtime recovery state is unavailable"}
	}
	return []resumeReplayInsertion{{
		AnchorMessageID: insertionAnchorID,
		Messages:        replayedMessages,
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

func threadHasAssistantMessageForRun(messages []data.ThreadMessage, runID uuid.UUID) bool {
	if runID == uuid.Nil {
		return false
	}
	want := runID.String()
	for _, msg := range messages {
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
		switch msg.Role {
		case "assistant":
			if msg.Assistant == nil {
				continue
			}
			replayed = append(replayed, replayAssistantMessage(*msg.Assistant))
		case "tool":
			if msg.Tool == nil {
				continue
			}
			replayed = append(replayed, replayToolResultMessage(*msg.Tool))
		}
	}
	for _, call := range state.PendingToolCalls {
		replayed = append(replayed, replayToolResultMessage(rollout.ReplayToolResult{
			CallID:    call.CallID,
			Name:      call.Name,
			Error:     interruptedToolErrorMessage,
			Synthetic: true,
		}))
	}
	return replayed, nil
}

func replayAssistantMessage(msg rollout.AssistantMessage) llm.Message {
	var toolCalls []llm.ToolCall
	if len(msg.ToolCalls) > 0 {
		_ = json.Unmarshal(msg.ToolCalls, &toolCalls)
	}
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
	if strings.TrimSpace(result.Name) != "" {
		envelope["tool_name"] = strings.TrimSpace(result.Name)
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
		result = append(result, toolCall)
	}
	return result
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
