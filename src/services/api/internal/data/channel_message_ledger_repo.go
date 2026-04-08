package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ChannelMessageDirection string

const (
	ChannelMessageDirectionInbound  ChannelMessageDirection = "inbound"
	ChannelMessageDirectionOutbound ChannelMessageDirection = "outbound"
)

type ChannelMessageLedgerEntry struct {
	ID                      uuid.UUID
	ChannelID               uuid.UUID
	ChannelType             string
	Direction               ChannelMessageDirection
	ThreadID                *uuid.UUID
	RunID                   *uuid.UUID
	PlatformConversationID  string
	PlatformMessageID       string
	PlatformParentMessageID *string
	PlatformThreadID        *string
	SenderChannelIdentityID *uuid.UUID
	MessageID               *uuid.UUID
	MetadataJSON            json.RawMessage
	CreatedAt               time.Time
}

type ChannelInboundLedgerEntry struct {
	ChannelID               uuid.UUID
	ChannelType             string
	ID                      uuid.UUID
	ThreadID                *uuid.UUID
	RunID                   *uuid.UUID
	PlatformConversationID  string
	PlatformMessageID       string
	PlatformParentMessageID *string
	PlatformThreadID        *string
	SenderChannelIdentityID *uuid.UUID
	MessageID               *uuid.UUID
	MetadataJSON            json.RawMessage
	CreatedAt               time.Time
}

type ChannelInboundLedgerBatch struct {
	ChannelID      uuid.UUID
	ThreadID       uuid.UUID
	BatchID        string
	DueAt          time.Time
	Entries        []ChannelInboundLedgerEntry
	FirstCreatedAt time.Time
	LastCreatedAt  time.Time
}

func (b ChannelInboundLedgerBatch) MessageCount() int {
	return len(b.Entries)
}

func (b ChannelInboundLedgerBatch) LastEntry() *ChannelInboundLedgerEntry {
	if len(b.Entries) == 0 {
		return nil
	}
	last := b.Entries[len(b.Entries)-1]
	return &last
}

type ChannelMessageLedgerRecordInput struct {
	ChannelID               uuid.UUID
	ChannelType             string
	Direction               ChannelMessageDirection
	ThreadID                *uuid.UUID
	RunID                   *uuid.UUID
	PlatformConversationID  string
	PlatformMessageID       string
	PlatformParentMessageID *string
	PlatformThreadID        *string
	SenderChannelIdentityID *uuid.UUID
	MessageID               *uuid.UUID
	MetadataJSON            json.RawMessage
}

type ChannelMessageLedgerRepository struct {
	db Querier
}

const (
	channelInboundMetadataStateKey   = "ingress_state"
	channelInboundPendingState       = "pending_dispatch"
	channelInboundBurstBatchKey      = "pending_dispatch_batch_id"
	channelInboundBurstDueAtKey      = "pending_dispatch_due_at"
	channelInboundBurstDispatchAfter = "dispatch_after_unix_ms"
	channelInboundDefaultBatchLimit  = 16
	channelInboundDefaultFetchFactor = 64
	channelInboundMinimumFetchRows   = 256
)

func NewChannelMessageLedgerRepository(db Querier) (*ChannelMessageLedgerRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ChannelMessageLedgerRepository{db: db}, nil
}

func (r *ChannelMessageLedgerRepository) WithTx(tx pgx.Tx) *ChannelMessageLedgerRepository {
	return &ChannelMessageLedgerRepository{db: tx}
}

func (r *ChannelMessageLedgerRepository) Record(ctx context.Context, input ChannelMessageLedgerRecordInput) (bool, error) {
	if input.ChannelID == uuid.Nil {
		return false, fmt.Errorf("channel_message_ledger: channel_id must not be empty")
	}
	if strings.TrimSpace(input.ChannelType) == "" {
		return false, fmt.Errorf("channel_message_ledger: channel_type must not be empty")
	}
	if input.Direction != ChannelMessageDirectionInbound && input.Direction != ChannelMessageDirectionOutbound {
		return false, fmt.Errorf("channel_message_ledger: direction must be inbound or outbound")
	}
	if strings.TrimSpace(input.PlatformConversationID) == "" || strings.TrimSpace(input.PlatformMessageID) == "" {
		return false, fmt.Errorf("channel_message_ledger: platform ids must not be empty")
	}
	metadataJSON := input.MetadataJSON
	if len(metadataJSON) == 0 {
		metadataJSON = json.RawMessage(`{}`)
	}
	tag, err := r.db.Exec(
		ctx,
		`INSERT INTO channel_message_ledger (
			channel_id,
			channel_type,
			direction,
			thread_id,
			run_id,
			platform_conversation_id,
			platform_message_id,
			platform_parent_message_id,
			platform_thread_id,
			sender_channel_identity_id,
			message_id,
			metadata_json
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)
		ON CONFLICT (channel_id, direction, platform_conversation_id, platform_message_id) DO NOTHING`,
		input.ChannelID,
		strings.TrimSpace(input.ChannelType),
		string(input.Direction),
		input.ThreadID,
		input.RunID,
		strings.TrimSpace(input.PlatformConversationID),
		strings.TrimSpace(input.PlatformMessageID),
		trimOptionalStringPtr(input.PlatformParentMessageID),
		trimOptionalStringPtr(input.PlatformThreadID),
		input.SenderChannelIdentityID,
		input.MessageID,
		metadataJSON,
	)
	if err != nil {
		return false, fmt.Errorf("channel_message_ledger.Record: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func trimOptionalStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func (r *ChannelMessageLedgerRepository) HasOutboundForRun(ctx context.Context, runID uuid.UUID) (bool, error) {
	if runID == uuid.Nil {
		return false, nil
	}
	var exists bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM channel_message_ledger WHERE run_id = $1 AND direction = 'outbound')`,
		runID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("channel_message_ledger.HasOutboundForRun: %w", err)
	}
	return exists, nil
}

func (r *ChannelMessageLedgerRepository) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM channel_message_ledger WHERE created_at < $1`, cutoff.UTC())
	if err != nil {
		return 0, fmt.Errorf("channel_message_ledger.DeleteOlderThan: %w", err)
	}
	return tag.RowsAffected(), nil
}

// LookupInboundMessage 通过 channel 的 inbound 平台消息 ID 查找对应的 ledger 记录。
func (r *ChannelMessageLedgerRepository) LookupInboundMessage(
	ctx context.Context,
	channelID uuid.UUID,
	platformConversationID string,
	platformMessageID string,
) (*uuid.UUID, *uuid.UUID, error) {
	var messageID, threadID *uuid.UUID
	err := r.db.QueryRow(ctx,
		`SELECT message_id, thread_id FROM channel_message_ledger
		 WHERE channel_id = $1
		   AND direction = 'inbound'
		   AND platform_conversation_id = $2
		   AND platform_message_id = $3`,
		channelID,
		strings.TrimSpace(platformConversationID),
		strings.TrimSpace(platformMessageID),
	).Scan(&messageID, &threadID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("channel_message_ledger.LookupInboundMessage: %w", err)
	}
	return messageID, threadID, nil
}

func (r *ChannelMessageLedgerRepository) GetInboundEntry(
	ctx context.Context,
	channelID uuid.UUID,
	platformConversationID string,
	platformMessageID string,
) (*ChannelInboundLedgerEntry, error) {
	return r.getInboundEntry(ctx, channelID, platformConversationID, platformMessageID, false)
}

func (r *ChannelMessageLedgerRepository) GetInboundEntryForUpdate(
	ctx context.Context,
	channelID uuid.UUID,
	platformConversationID string,
	platformMessageID string,
) (*ChannelInboundLedgerEntry, error) {
	return r.getInboundEntry(ctx, channelID, platformConversationID, platformMessageID, true)
}

func (r *ChannelMessageLedgerRepository) getInboundEntry(
	ctx context.Context,
	channelID uuid.UUID,
	platformConversationID string,
	platformMessageID string,
	forUpdate bool,
) (*ChannelInboundLedgerEntry, error) {
	var item ChannelInboundLedgerEntry
	query := `SELECT channel_id, channel_type, id, thread_id, run_id, platform_conversation_id, platform_message_id,
	        platform_parent_message_id, platform_thread_id, sender_channel_identity_id,
	        message_id, metadata_json, created_at
	   FROM channel_message_ledger
	  WHERE channel_id = $1
	    AND direction = 'inbound'
	    AND platform_conversation_id = $2
	    AND platform_message_id = $3`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	err := r.db.QueryRow(ctx,
		query,
		channelID,
		strings.TrimSpace(platformConversationID),
		strings.TrimSpace(platformMessageID),
	).Scan(
		&item.ChannelID,
		&item.ChannelType,
		&item.ID,
		&item.ThreadID,
		&item.RunID,
		&item.PlatformConversationID,
		&item.PlatformMessageID,
		&item.PlatformParentMessageID,
		&item.PlatformThreadID,
		&item.SenderChannelIdentityID,
		&item.MessageID,
		&item.MetadataJSON,
		&item.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("channel_message_ledger.GetInboundEntry: %w", err)
	}
	return &item, nil
}

func (r *ChannelMessageLedgerRepository) ListInboundEntriesByState(
	ctx context.Context,
	channelID uuid.UUID,
	state string,
	limit int,
) ([]ChannelInboundLedgerEntry, error) {
	if channelID == uuid.Nil {
		return nil, fmt.Errorf("channel_message_ledger: channel_id must not be empty")
	}
	if strings.TrimSpace(state) == "" {
		return nil, fmt.Errorf("channel_message_ledger: state must not be empty")
	}
	if limit <= 0 {
		limit = 16
	}
	rows, err := r.db.Query(ctx,
		`SELECT channel_id, channel_type, id, thread_id, run_id, platform_conversation_id, platform_message_id,
		        platform_parent_message_id, platform_thread_id, sender_channel_identity_id,
		        message_id, metadata_json, created_at
		   FROM channel_message_ledger
		  WHERE channel_id = $1
		    AND direction = 'inbound'
		    AND metadata_json->>'`+channelInboundMetadataStateKey+`' = $2
		  ORDER BY created_at ASC
		  LIMIT $3`,
		channelID,
		strings.TrimSpace(state),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("channel_message_ledger.ListInboundEntriesByState: %w", err)
	}
	defer rows.Close()

	items := make([]ChannelInboundLedgerEntry, 0, limit)
	for rows.Next() {
		var item ChannelInboundLedgerEntry
		if err := rows.Scan(
			&item.ChannelID,
			&item.ChannelType,
			&item.ID,
			&item.ThreadID,
			&item.RunID,
			&item.PlatformConversationID,
			&item.PlatformMessageID,
			&item.PlatformParentMessageID,
			&item.PlatformThreadID,
			&item.SenderChannelIdentityID,
			&item.MessageID,
			&item.MetadataJSON,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("channel_message_ledger.ListInboundEntriesByState scan: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("channel_message_ledger.ListInboundEntriesByState rows: %w", err)
	}
	return items, nil
}

func (r *ChannelMessageLedgerRepository) ListInboundEntriesByStateGlobal(
	ctx context.Context,
	state string,
	limit int,
) ([]ChannelInboundLedgerEntry, error) {
	if strings.TrimSpace(state) == "" {
		return nil, fmt.Errorf("channel_message_ledger: state must not be empty")
	}
	if limit <= 0 {
		limit = 64
	}
	rows, err := r.db.Query(ctx,
		`SELECT channel_id, channel_type, id, thread_id, run_id, platform_conversation_id, platform_message_id,
		        platform_parent_message_id, platform_thread_id, sender_channel_identity_id,
		        message_id, metadata_json, created_at
		   FROM channel_message_ledger
		  WHERE direction = 'inbound'
		    AND run_id IS NULL
		    AND metadata_json->>'`+channelInboundMetadataStateKey+`' = $1
		  ORDER BY created_at ASC
		  LIMIT $2`,
		strings.TrimSpace(state),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("channel_message_ledger.ListInboundEntriesByStateGlobal: %w", err)
	}
	defer rows.Close()

	items := make([]ChannelInboundLedgerEntry, 0, limit)
	for rows.Next() {
		var item ChannelInboundLedgerEntry
		if err := rows.Scan(
			&item.ChannelID,
			&item.ChannelType,
			&item.ID,
			&item.ThreadID,
			&item.RunID,
			&item.PlatformConversationID,
			&item.PlatformMessageID,
			&item.PlatformParentMessageID,
			&item.PlatformThreadID,
			&item.SenderChannelIdentityID,
			&item.MessageID,
			&item.MetadataJSON,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("channel_message_ledger.ListInboundEntriesByStateGlobal scan: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("channel_message_ledger.ListInboundEntriesByStateGlobal rows: %w", err)
	}
	return items, nil
}

func (r *ChannelMessageLedgerRepository) ListInboundEntriesByThreadState(
	ctx context.Context,
	channelID uuid.UUID,
	threadID uuid.UUID,
	state string,
	forUpdate bool,
) ([]ChannelInboundLedgerEntry, error) {
	if channelID == uuid.Nil {
		return nil, fmt.Errorf("channel_message_ledger: channel_id must not be empty")
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("channel_message_ledger: thread_id must not be empty")
	}
	if strings.TrimSpace(state) == "" {
		return nil, fmt.Errorf("channel_message_ledger: state must not be empty")
	}
	query := `SELECT channel_id, channel_type, id, thread_id, run_id, platform_conversation_id, platform_message_id,
	        platform_parent_message_id, platform_thread_id, sender_channel_identity_id,
	        message_id, metadata_json, created_at
	   FROM channel_message_ledger
	  WHERE channel_id = $1
	    AND direction = 'inbound'
	    AND thread_id = $2
	    AND run_id IS NULL
	    AND metadata_json->>'` + channelInboundMetadataStateKey + `' = $3
	  ORDER BY created_at ASC`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	rows, err := r.db.Query(ctx, query, channelID, threadID, strings.TrimSpace(state))
	if err != nil {
		return nil, fmt.Errorf("channel_message_ledger.ListInboundEntriesByThreadState: %w", err)
	}
	defer rows.Close()

	items := make([]ChannelInboundLedgerEntry, 0, 8)
	for rows.Next() {
		var item ChannelInboundLedgerEntry
		if err := rows.Scan(
			&item.ChannelID,
			&item.ChannelType,
			&item.ID,
			&item.ThreadID,
			&item.RunID,
			&item.PlatformConversationID,
			&item.PlatformMessageID,
			&item.PlatformParentMessageID,
			&item.PlatformThreadID,
			&item.SenderChannelIdentityID,
			&item.MessageID,
			&item.MetadataJSON,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("channel_message_ledger.ListInboundEntriesByThreadState scan: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("channel_message_ledger.ListInboundEntriesByThreadState rows: %w", err)
	}
	return items, nil
}

func (r *ChannelMessageLedgerRepository) UpdateInboundEntry(
	ctx context.Context,
	channelID uuid.UUID,
	platformConversationID string,
	platformMessageID string,
	threadID *uuid.UUID,
	runID *uuid.UUID,
	messageID *uuid.UUID,
	metadataJSON json.RawMessage,
) (bool, error) {
	if channelID == uuid.Nil {
		return false, fmt.Errorf("channel_message_ledger: channel_id must not be empty")
	}
	if strings.TrimSpace(platformConversationID) == "" || strings.TrimSpace(platformMessageID) == "" {
		return false, fmt.Errorf("channel_message_ledger: platform ids must not be empty")
	}
	if len(metadataJSON) == 0 {
		metadataJSON = json.RawMessage(`{}`)
	}
	tag, err := r.db.Exec(ctx,
		`UPDATE channel_message_ledger
		    SET thread_id = COALESCE($4, thread_id),
		        run_id = COALESCE($5, run_id),
		        message_id = COALESCE($6, message_id),
		        metadata_json = $7::jsonb
		  WHERE channel_id = $1
		    AND direction = 'inbound'
		    AND platform_conversation_id = $2
		    AND platform_message_id = $3`,
		channelID,
		strings.TrimSpace(platformConversationID),
		strings.TrimSpace(platformMessageID),
		threadID,
		runID,
		messageID,
		metadataJSON,
	)
	if err != nil {
		return false, fmt.Errorf("channel_message_ledger.UpdateInboundEntry: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *ChannelMessageLedgerRepository) UpdateInboundMetadata(
	ctx context.Context,
	channelID uuid.UUID,
	platformConversationID string,
	platformMessageID string,
	metadataJSON json.RawMessage,
) (bool, error) {
	if channelID == uuid.Nil {
		return false, fmt.Errorf("channel_message_ledger: channel_id must not be empty")
	}
	if strings.TrimSpace(platformConversationID) == "" || strings.TrimSpace(platformMessageID) == "" {
		return false, fmt.Errorf("channel_message_ledger: platform ids must not be empty")
	}
	if len(metadataJSON) == 0 {
		metadataJSON = json.RawMessage(`{}`)
	}
	tag, err := r.db.Exec(
		ctx,
		`UPDATE channel_message_ledger
		    SET metadata_json = $4::jsonb
		  WHERE channel_id = $1
		    AND direction = 'inbound'
		    AND platform_conversation_id = $2
		    AND platform_message_id = $3`,
		channelID,
		strings.TrimSpace(platformConversationID),
		strings.TrimSpace(platformMessageID),
		metadataJSON,
	)
	if err != nil {
		return false, fmt.Errorf("channel_message_ledger.UpdateInboundMetadata: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *ChannelMessageLedgerRepository) GetOpenInboundBatchByThread(
	ctx context.Context,
	channelID uuid.UUID,
	threadID uuid.UUID,
) (*ChannelInboundLedgerBatch, error) {
	items, err := r.ListInboundEntriesByThreadState(ctx, channelID, threadID, channelInboundPendingState, false)
	if err != nil {
		return nil, err
	}
	batches := groupInboundBatches(channelID, items)
	if len(batches) == 0 {
		return nil, nil
	}
	sortInboundBatches(batches)
	return &batches[0], nil
}

func (r *ChannelMessageLedgerRepository) ListDueInboundBatchesByChannel(
	ctx context.Context,
	channelID uuid.UUID,
	dueBefore time.Time,
	limit int,
) ([]ChannelInboundLedgerBatch, error) {
	if channelID == uuid.Nil {
		return nil, fmt.Errorf("channel_message_ledger: channel_id must not be empty")
	}
	if limit <= 0 {
		limit = channelInboundDefaultBatchLimit
	}
	fetchRows := limit * channelInboundDefaultFetchFactor
	if fetchRows < channelInboundMinimumFetchRows {
		fetchRows = channelInboundMinimumFetchRows
	}
	entries, err := r.ListInboundEntriesByState(ctx, channelID, channelInboundPendingState, fetchRows)
	if err != nil {
		return nil, err
	}
	batches := groupInboundBatches(channelID, entries)
	if len(batches) == 0 {
		return nil, nil
	}
	sortInboundBatches(batches)
	cutoff := dueBefore.UTC()
	result := make([]ChannelInboundLedgerBatch, 0, limit)
	for _, batch := range batches {
		if batch.DueAt.After(cutoff) {
			continue
		}
		result = append(result, batch)
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}

func groupInboundBatches(channelID uuid.UUID, items []ChannelInboundLedgerEntry) []ChannelInboundLedgerBatch {
	if len(items) == 0 {
		return nil
	}
	grouped := make(map[string]*ChannelInboundLedgerBatch, len(items))
	order := make([]string, 0, len(items))
	for _, item := range items {
		if item.ThreadID == nil || *item.ThreadID == uuid.Nil {
			continue
		}
		batchID := inboundBatchIDFromMetadata(item.MetadataJSON, item.ThreadID.String())
		key := item.ThreadID.String() + "|" + batchID
		batch, ok := grouped[key]
		if !ok {
			dueAt := inboundBurstDueAtFromMetadata(item.MetadataJSON, item.CreatedAt)
			batch = &ChannelInboundLedgerBatch{
				ChannelID:      channelID,
				ThreadID:       *item.ThreadID,
				BatchID:        batchID,
				DueAt:          dueAt,
				Entries:        make([]ChannelInboundLedgerEntry, 0, 4),
				FirstCreatedAt: item.CreatedAt.UTC(),
				LastCreatedAt:  item.CreatedAt.UTC(),
			}
			grouped[key] = batch
			order = append(order, key)
		}
		batch.Entries = append(batch.Entries, item)
		createdAt := item.CreatedAt.UTC()
		if createdAt.Before(batch.FirstCreatedAt) {
			batch.FirstCreatedAt = createdAt
		}
		if createdAt.After(batch.LastCreatedAt) {
			batch.LastCreatedAt = createdAt
		}
		if dueAt := inboundBurstDueAtFromMetadata(item.MetadataJSON, item.CreatedAt); dueAt.After(batch.DueAt) {
			batch.DueAt = dueAt
		}
	}
	result := make([]ChannelInboundLedgerBatch, 0, len(order))
	for _, key := range order {
		batch := grouped[key]
		if batch == nil {
			continue
		}
		sort.Slice(batch.Entries, func(i, j int) bool {
			return batch.Entries[i].CreatedAt.Before(batch.Entries[j].CreatedAt)
		})
		result = append(result, *batch)
	}
	return result
}

func sortInboundBatches(items []ChannelInboundLedgerBatch) {
	sort.Slice(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		if !left.DueAt.Equal(right.DueAt) {
			return left.DueAt.Before(right.DueAt)
		}
		if !left.FirstCreatedAt.Equal(right.FirstCreatedAt) {
			return left.FirstCreatedAt.Before(right.FirstCreatedAt)
		}
		return strings.Compare(left.BatchID, right.BatchID) < 0
	})
}

func inboundBatchIDFromMetadata(raw json.RawMessage, fallback string) string {
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		fallback = uuid.NewString()
	}
	value, ok := inboundMetadataString(raw, channelInboundBurstBatchKey)
	if !ok {
		return fallback
	}
	return value
}

func inboundBurstDueAtFromMetadata(raw json.RawMessage, fallback time.Time) time.Time {
	base := fallback.UTC()
	metadata, ok := inboundMetadataObject(raw)
	if !ok {
		return base
	}
	value, ok := metadata[channelInboundBurstDueAtKey]
	if !ok || value == nil {
		value, ok = metadata[channelInboundBurstDispatchAfter]
	}
	if !ok || value == nil {
		return base
	}
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return base
		}
		if ts, err := time.Parse(time.RFC3339Nano, trimmed); err == nil {
			return ts.UTC()
		}
		if ts, err := time.Parse(time.RFC3339, trimmed); err == nil {
			return ts.UTC()
		}
	case float64:
		return unixValueToTime(int64(typed), base)
	case int64:
		return unixValueToTime(typed, base)
	case int:
		return unixValueToTime(int64(typed), base)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return unixValueToTime(parsed, base)
		}
	default:
		if text := strings.TrimSpace(fmt.Sprintf("%v", typed)); text != "" {
			if parsed, err := strconv.ParseInt(text, 10, 64); err == nil {
				return unixValueToTime(parsed, base)
			}
		}
	}
	return base
}

func unixValueToTime(raw int64, fallback time.Time) time.Time {
	if raw <= 0 {
		return fallback
	}
	if raw > 1_000_000_000_000 {
		return time.UnixMilli(raw).UTC()
	}
	return time.Unix(raw, 0).UTC()
}

func inboundMetadataString(raw json.RawMessage, key string) (string, bool) {
	metadata, ok := inboundMetadataObject(raw)
	if !ok {
		return "", false
	}
	value, _ := metadata[key].(string)
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}

func inboundMetadataObject(raw json.RawMessage) (map[string]any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil || len(payload) == 0 {
		return nil, false
	}
	return payload, true
}
