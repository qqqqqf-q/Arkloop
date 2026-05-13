package accountapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type DispatchResult struct {
	ThreadID   uuid.UUID
	RunID      uuid.UUID
	FinalState string
	Delivered  bool
}

type InboundDispatchRequest struct {
	TraceID             string
	Channel             data.Channel
	PersonaRef          string
	Identity            data.ChannelIdentity
	Incoming            InboundMessage
	ThreadID            uuid.UUID
	MessageID           uuid.UUID
	InputContent        string
	ThreadTailMessageID string
	Source              string
	JobPayload          map[string]any
	ForceActive         bool
	RunEventRepo        *data.RunEventRepository
	JobRepo             *data.JobRepository
	DeliverToActiveRun  func(context.Context, *data.RunEventRepository, *data.Run, string, string) (bool, error)
}

type InboundPipelinePersistResult struct {
	ThreadID            uuid.UUID
	MessageID           uuid.UUID
	InputContent        string
	ThreadTailMessageID string
	LedgerMetadata      json.RawMessage
}

type InboundImmediatePipelineRequest struct {
	TraceID                string
	Channel                data.Channel
	PersonaRef             string
	Identity               data.ChannelIdentity
	Incoming               InboundMessage
	Source                 string
	ForceActive            bool
	SkipDedup              bool
	LedgerRepo             *data.ChannelMessageLedgerRepository
	ReceiptsRepo           *data.ChannelMessageReceiptsRepository
	RunEventRepo           *data.RunEventRepository
	JobRepo                *data.JobRepository
	ReceivedLedgerMetadata json.RawMessage
	PlatformParentMsgID    *string
	PlatformThreadID       *string
	JobPayload             map[string]any
	ResolveAndPersist      func(context.Context, pgx.Tx) (InboundPipelinePersistResult, error)
	DeliverToActiveRun     func(context.Context, *data.RunEventRepository, *data.Run, string, string) (bool, error)
}

func DispatchInboundImmediate(ctx context.Context, tx pgx.Tx, req InboundImmediatePipelineRequest) (DispatchResult, bool, error) {
	if tx == nil {
		return DispatchResult{}, false, fmt.Errorf("inbound dispatch requires transaction")
	}
	if strings.TrimSpace(req.Incoming.PlatformChatID) == "" || strings.TrimSpace(req.Incoming.PlatformMsgID) == "" {
		return DispatchResult{}, false, fmt.Errorf("inbound dispatch requires platform ids")
	}
	if req.ResolveAndPersist == nil {
		return DispatchResult{}, false, fmt.Errorf("inbound dispatch requires resolve/persist hook")
	}
	ledgerMeta := req.ReceivedLedgerMetadata
	if len(ledgerMeta) == 0 {
		ledgerMeta = inboundLedgerMetadata(map[string]any{
			inboundLedgerKeySource:           firstNonEmptySelector(req.Source, req.Channel.ChannelType),
			inboundLedgerKeyConversationType: req.Incoming.ConversationType,
			inboundLedgerKeyMentionsBot:      req.Incoming.MentionsBot,
			inboundLedgerKeyIsReplyToBot:     req.Incoming.IsReplyToBot,
			inboundLedgerKeyMatchesKeyword:   req.Incoming.MatchesKeyword,
		}, inboundStateReceived)
	}
	if req.SkipDedup {
		// Caller has already recorded this inbound message, usually because
		// command handling must run after the duplicate check.
	} else if req.LedgerRepo != nil {
		accepted, err := req.LedgerRepo.WithTx(tx).Record(ctx, data.ChannelMessageLedgerRecordInput{
			ChannelID:               req.Channel.ID,
			ChannelType:             req.Channel.ChannelType,
			Direction:               data.ChannelMessageDirectionInbound,
			PlatformConversationID:  req.Incoming.PlatformChatID,
			PlatformMessageID:       req.Incoming.PlatformMsgID,
			PlatformParentMessageID: req.PlatformParentMsgID,
			PlatformThreadID:        req.PlatformThreadID,
			SenderChannelIdentityID: &req.Identity.ID,
			MetadataJSON:            ledgerMeta,
		})
		if err != nil {
			return DispatchResult{}, false, err
		}
		if !accepted {
			return DispatchResult{}, false, nil
		}
	} else if req.ReceiptsRepo != nil {
		accepted, err := req.ReceiptsRepo.WithTx(tx).Record(ctx, req.Channel.ID, req.Incoming.PlatformChatID, req.Incoming.PlatformMsgID)
		if err != nil {
			return DispatchResult{}, false, err
		}
		if !accepted {
			return DispatchResult{}, false, nil
		}
	}

	persisted, err := req.ResolveAndPersist(ctx, tx)
	if err != nil {
		return DispatchResult{}, true, err
	}
	if persisted.ThreadID == uuid.Nil || persisted.MessageID == uuid.Nil {
		return DispatchResult{}, true, fmt.Errorf("inbound dispatch resolve/persist returned empty ids")
	}
	pendingMetadata := persisted.LedgerMetadata
	if len(pendingMetadata) == 0 {
		pendingMetadata = applyInboundLedgerState(ledgerMeta, inboundStatePendingDispatch)
	}
	if req.LedgerRepo != nil {
		if _, err := req.LedgerRepo.WithTx(tx).UpdateInboundEntry(ctx, req.Channel.ID, req.Incoming.PlatformChatID, req.Incoming.PlatformMsgID, &persisted.ThreadID, nil, &persisted.MessageID, pendingMetadata); err != nil {
			return DispatchResult{}, true, err
		}
	}

	dispatchResult, err := DispatchInbound(ctx, tx, InboundDispatchRequest{
		TraceID:             req.TraceID,
		Channel:             req.Channel,
		PersonaRef:          req.PersonaRef,
		Identity:            req.Identity,
		Incoming:            req.Incoming,
		ThreadID:            persisted.ThreadID,
		MessageID:           persisted.MessageID,
		InputContent:        persisted.InputContent,
		ThreadTailMessageID: persisted.ThreadTailMessageID,
		Source:              req.Source,
		JobPayload:          req.JobPayload,
		ForceActive:         req.ForceActive,
		RunEventRepo:        req.RunEventRepo,
		JobRepo:             req.JobRepo,
		DeliverToActiveRun:  req.DeliverToActiveRun,
	})
	if err != nil {
		return DispatchResult{}, true, err
	}
	if req.LedgerRepo != nil {
		var runID *uuid.UUID
		if dispatchResult.RunID != uuid.Nil {
			runID = &dispatchResult.RunID
		}
		if _, err := req.LedgerRepo.WithTx(tx).UpdateInboundEntry(ctx, req.Channel.ID, req.Incoming.PlatformChatID, req.Incoming.PlatformMsgID, &persisted.ThreadID, runID, &persisted.MessageID, applyInboundLedgerState(pendingMetadata, dispatchResult.FinalState)); err != nil {
			return DispatchResult{}, true, err
		}
	}
	return dispatchResult, true, nil
}

func DispatchInbound(ctx context.Context, tx pgx.Tx, req InboundDispatchRequest) (DispatchResult, error) {
	if tx == nil {
		return DispatchResult{}, fmt.Errorf("inbound dispatch requires transaction")
	}
	if req.RunEventRepo == nil || req.JobRepo == nil {
		return DispatchResult{}, fmt.Errorf("inbound dispatch requires run and job repositories")
	}
	if req.ThreadID == uuid.Nil {
		return DispatchResult{}, fmt.Errorf("inbound dispatch requires thread_id")
	}
	if strings.TrimSpace(req.PersonaRef) == "" {
		return DispatchResult{}, fmt.Errorf("inbound dispatch requires persona_ref")
	}
	dispatchMode := channelDispatchMode(req.Channel.ChannelType)
	result := DispatchResult{ThreadID: req.ThreadID}
	runRepoTx := req.RunEventRepo.WithTx(tx)
	if err := runRepoTx.LockThreadRow(ctx, req.ThreadID); err != nil {
		return result, err
	}
	activeRun, err := runRepoTx.GetActiveRootRunForThread(ctx, req.ThreadID)
	if err != nil {
		return result, err
	}
	if activeRun != nil && req.DeliverToActiveRun != nil {
		delivered, err := req.DeliverToActiveRun(ctx, runRepoTx, activeRun, req.InputContent, req.TraceID)
		if err != nil {
			return result, err
		}
		if delivered {
			result.RunID = activeRun.ID
			result.FinalState = inboundStateDeliveredToRun
			result.Delivered = true
			return result, nil
		}
	}
	if !req.ForceActive && !req.Incoming.ShouldCreateRun() {
		result.FinalState = inboundStatePassivePersisted
		return result, nil
	}
	if !channelAgentTriggerConsume(req.Channel.ID) {
		result.FinalState = inboundStateThrottledNoRun
		return result, nil
	}
	channelDelivery := BuildChannelDeliveryPayload(req.Incoming, req.Identity.ID)
	runData := map[string]any{
		"persona_id":          strings.TrimSpace(req.PersonaRef),
		"continuation_source": "none",
		"continuation_loop":   false,
		"channel_delivery":    channelDelivery,
		"dispatch_mode":       dispatchMode,
	}
	if req.ThreadTailMessageID != "" {
		runData["thread_tail_message_id"] = req.ThreadTailMessageID
	}
	run, _, err := runRepoTx.CreateRunWithStartedEvent(ctx, req.Channel.AccountID, req.ThreadID, channelOwnerUserID(req.Channel), "run.started", runData)
	if err != nil {
		return result, err
	}
	jobPayload := map[string]any{
		"source":           firstNonEmptySelector(req.Source, req.Channel.ChannelType),
		"channel_delivery": channelDelivery,
		"dispatch_mode":    dispatchMode,
	}
	for key, value := range req.JobPayload {
		jobPayload[key] = value
	}
	if _, err := req.JobRepo.WithTx(tx).EnqueueRun(ctx, req.Channel.AccountID, run.ID, req.TraceID, data.RunExecuteJobType, jobPayload, nil); err != nil {
		return result, err
	}
	result.RunID = run.ID
	result.FinalState = inboundStateEnqueuedNewRun
	return result, nil
}

type inboundThreadConfig struct {
	DefaultModel            string `json:"default_model,omitempty"`
	ReasoningMode           string `json:"reasoning_mode,omitempty"`
	HeartbeatEnabled        *bool  `json:"heartbeat_enabled,omitempty"`
	HeartbeatIntervalMinute int    `json:"heartbeat_interval_minutes,omitempty"`
	HeartbeatModel          string `json:"heartbeat_model,omitempty"`
}

func ensureInboundThreadDefaultModel(ctx context.Context, db data.Querier, threadID uuid.UUID, defaultModel string) error {
	if db == nil || threadID == uuid.Nil {
		return nil
	}
	model := strings.TrimSpace(defaultModel)
	if model == "" {
		return nil
	}

	config, err := readInboundThreadConfigMap(ctx, db, threadID)
	if err != nil {
		return err
	}
	if existing, _ := config["default_model"].(string); strings.TrimSpace(existing) != "" {
		return nil
	}
	config["default_model"] = model
	return writeInboundThreadConfigMap(ctx, db, threadID, config)
}

func readInboundThreadConfig(ctx context.Context, db data.Querier, threadID uuid.UUID) (inboundThreadConfig, bool, error) {
	configMap, err := readInboundThreadConfigMap(ctx, db, threadID)
	if err != nil {
		return inboundThreadConfig{}, false, err
	}
	if configMap == nil {
		return inboundThreadConfig{}, false, nil
	}
	encoded, err := json.Marshal(configMap)
	if err != nil {
		return inboundThreadConfig{}, false, err
	}
	var cfg inboundThreadConfig
	if err := json.Unmarshal(encoded, &cfg); err != nil {
		return inboundThreadConfig{}, false, err
	}
	return cfg, true, nil
}

func readInboundThreadConfigMap(ctx context.Context, db data.Querier, threadID uuid.UUID) (map[string]any, error) {
	if db == nil || threadID == uuid.Nil {
		return nil, nil
	}
	var raw json.RawMessage
	if err := db.QueryRow(ctx, `
		SELECT COALESCE(config_json, '{}'::jsonb)
		  FROM threads
		 WHERE id = $1
		   AND deleted_at IS NULL`,
		threadID,
	).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	config := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, err
		}
	}
	return config, nil
}

func writeInboundThreadConfigMap(ctx context.Context, db data.Querier, threadID uuid.UUID, config map[string]any) error {
	if db == nil || threadID == uuid.Nil {
		return nil
	}
	if config == nil {
		config = map[string]any{}
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `
		UPDATE threads
		   SET config_json = $2::jsonb,
		       updated_at = now()
		 WHERE id = $1
		   AND deleted_at IS NULL`,
		threadID,
		encoded,
	)
	return err
}

func getInboundThreadModelPreference(ctx context.Context, db data.Querier, threadID uuid.UUID) (string, string, bool, error) {
	cfg, ok, err := readInboundThreadConfig(ctx, db, threadID)
	if err != nil || !ok {
		return "", "", ok, err
	}
	return strings.TrimSpace(cfg.DefaultModel), strings.TrimSpace(cfg.ReasoningMode), true, nil
}

func updateInboundThreadModelPreference(ctx context.Context, db data.Querier, threadID uuid.UUID, model string, reasoningMode string) error {
	config, err := readInboundThreadConfigMap(ctx, db, threadID)
	if err != nil || config == nil {
		return err
	}
	if strings.TrimSpace(model) == "" {
		delete(config, "default_model")
	} else {
		config["default_model"] = strings.TrimSpace(model)
	}
	if strings.TrimSpace(reasoningMode) == "" || strings.EqualFold(strings.TrimSpace(reasoningMode), "off") {
		delete(config, "reasoning_mode")
	} else {
		config["reasoning_mode"] = strings.TrimSpace(reasoningMode)
	}
	return writeInboundThreadConfigMap(ctx, db, threadID, config)
}

func getInboundThreadHeartbeatConfig(ctx context.Context, db data.Querier, threadID uuid.UUID) (enabled bool, intervalMin int, model string, ok bool, err error) {
	cfg, ok, err := readInboundThreadConfig(ctx, db, threadID)
	if err != nil || !ok {
		return false, 0, "", ok, err
	}
	if cfg.HeartbeatEnabled == nil {
		return false, cfg.HeartbeatIntervalMinute, strings.TrimSpace(cfg.HeartbeatModel), false, nil
	}
	return *cfg.HeartbeatEnabled, cfg.HeartbeatIntervalMinute, strings.TrimSpace(cfg.HeartbeatModel), true, nil
}

func updateInboundThreadHeartbeatConfig(ctx context.Context, db data.Querier, threadID uuid.UUID, enabled bool, intervalMin int, model string) error {
	config, err := readInboundThreadConfigMap(ctx, db, threadID)
	if err != nil || config == nil {
		return err
	}
	config["heartbeat_enabled"] = enabled
	if intervalMin > 0 {
		config["heartbeat_interval_minutes"] = intervalMin
	} else {
		delete(config, "heartbeat_interval_minutes")
	}
	if strings.TrimSpace(model) == "" {
		delete(config, "heartbeat_model")
	} else {
		config["heartbeat_model"] = strings.TrimSpace(model)
	}
	return writeInboundThreadConfigMap(ctx, db, threadID, config)
}
