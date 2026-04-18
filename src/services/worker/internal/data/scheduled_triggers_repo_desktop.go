//go:build desktop

package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/runkind"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrHeartbeatIdentityGone 表示 scheduled_triggers 中的 channel_identity 已不存在，应删除该触发器。
var ErrHeartbeatIdentityGone = errors.New("channel_identity not found, heartbeat trigger is stale")

// ScheduledTriggerRow 是 scheduled_triggers 表的一行。
type ScheduledTriggerRow struct {
	ID                uuid.UUID
	ChannelID         uuid.UUID
	ChannelIdentityID uuid.UUID
	PersonaKey        string
	AccountID         uuid.UUID
	Model             string
	IntervalMin       int
	NextFireAt        time.Time
	TriggerKind       string
	JobID             uuid.UUID
}

// ScheduledTriggersRepository 是 SQLite 实现（desktop）。
type ScheduledTriggersRepository struct{}

func normalizeHeartbeatInterval(intervalMin int) int {
	if intervalMin <= 0 {
		return runkind.DefaultHeartbeatIntervalMinutes
	}
	return intervalMin
}

// HeartbeatThreadContext 保存心跳 run 所需的线程/渠道上下文。
type HeartbeatThreadContext struct {
	ThreadID         uuid.UUID
	ChannelID        string
	ChannelType      string
	PlatformChatID   string
	IdentityID       string
	ConversationType string
	CreatedByUserID  *uuid.UUID
}

type threadTailMessage struct {
	ID   uuid.UUID
	Role string
}

// UpsertHeartbeat 注册或更新某个 channel identity 的 heartbeat 调度。
func (ScheduledTriggersRepository) UpsertHeartbeat(
	ctx context.Context,
	db DesktopDB,
	accountID uuid.UUID,
	channelID uuid.UUID,
	channelIdentityID uuid.UUID,
	personaKey string,
	model string,
	intervalMin int,
) error {
	if channelID == uuid.Nil {
		return fmt.Errorf("channel_id must not be empty")
	}
	if channelIdentityID == uuid.Nil {
		return fmt.Errorf("channel_identity_id must not be empty")
	}
	intervalMin = normalizeHeartbeatInterval(intervalMin)
	now := time.Now().UTC()
	nextFire := now.Add(time.Duration(intervalMin) * time.Minute)
	id := uuid.New()
	_, err := db.Exec(ctx, `
		INSERT INTO scheduled_triggers
		    (id, channel_id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
		ON CONFLICT (channel_id, channel_identity_id) DO UPDATE
		    SET persona_key   = excluded.persona_key,
		        account_id    = excluded.account_id,
		        model         = excluded.model,
		        interval_min  = excluded.interval_min,
		        updated_at    = excluded.updated_at`,
		id, channelID, channelIdentityID, personaKey, accountID, model, intervalMin,
		nextFire.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)
	return err
}

// GetHeartbeat returns the existing trigger for a channel identity.
func (ScheduledTriggersRepository) GetHeartbeat(
	ctx context.Context,
	db DesktopDB,
	channelID uuid.UUID,
	channelIdentityID uuid.UUID,
) (*ScheduledTriggerRow, error) {
	if channelID == uuid.Nil {
		return nil, fmt.Errorf("channel_id must not be empty")
	}
	if channelIdentityID == uuid.Nil {
		return nil, fmt.Errorf("channel_identity_id must not be empty")
	}

	var row ScheduledTriggerRow
	var idStr, channelStr, identityStr, accountStr string
	err := db.QueryRow(ctx, `
		SELECT id, channel_id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at
		  FROM scheduled_triggers
		 WHERE channel_id = $1
		   AND channel_identity_id = $2`,
		channelID.String(),
		channelIdentityID.String(),
	).Scan(&idStr, &channelStr, &identityStr, &row.PersonaKey, &accountStr, &row.Model, &row.IntervalMin, &row.NextFireAt)
	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	row.ID, _ = uuid.Parse(idStr)
	row.ChannelID, _ = uuid.Parse(channelStr)
	row.ChannelIdentityID, _ = uuid.Parse(identityStr)
	row.AccountID, _ = uuid.Parse(accountStr)
	return &row, nil
}

// ResetHeartbeatNextFire sets next_fire_at to now + interval_min for the provided channel identity.
func (ScheduledTriggersRepository) ResetHeartbeatNextFire(
	ctx context.Context,
	db DesktopDB,
	channelID uuid.UUID,
	channelIdentityID uuid.UUID,
	intervalMin int,
) (time.Time, error) {
	if channelID == uuid.Nil {
		return time.Time{}, fmt.Errorf("channel_id must not be empty")
	}
	if channelIdentityID == uuid.Nil {
		return time.Time{}, fmt.Errorf("channel_identity_id must not be empty")
	}
	intervalMin = normalizeHeartbeatInterval(intervalMin)
	nextFire := time.Now().UTC().Add(time.Duration(intervalMin) * time.Minute)
	tag, err := db.Exec(ctx, `
		UPDATE scheduled_triggers
		   SET interval_min = $1,
		       next_fire_at = $2,
		       updated_at = $3
		 WHERE channel_id = $4
		   AND channel_identity_id = $5`,
		intervalMin,
		nextFire.Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano),
		channelID.String(),
		channelIdentityID.String(),
	)
	if err != nil {
		return time.Time{}, err
	}
	if tag.RowsAffected() == 0 {
		return time.Time{}, fmt.Errorf("reset heartbeat next fire: channel_identity_id %s not found", channelIdentityID)
	}
	return nextFire, nil
}

// RescheduleHeartbeatNextFireAt forces next_fire_at to the provided timestamp.
func (ScheduledTriggersRepository) RescheduleHeartbeatNextFireAt(
	ctx context.Context,
	db DesktopDB,
	id uuid.UUID,
	nextFireAt time.Time,
) error {
	if id == uuid.Nil {
		return fmt.Errorf("id must not be empty")
	}
	if nextFireAt.IsZero() {
		return fmt.Errorf("next_fire_at must not be zero")
	}
	ts := nextFireAt.UTC().Format(time.RFC3339Nano)
	tag, err := db.Exec(ctx, `
		UPDATE scheduled_triggers
		   SET next_fire_at = $1,
		       updated_at = $2
		 WHERE id = $3`,
		ts,
		time.Now().UTC().Format(time.RFC3339Nano),
		id,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("reschedule heartbeat: id %s not found", id)
	}
	return nil
}

// DeleteHeartbeat 删除某个 channel identity 的 heartbeat 调度。
func (ScheduledTriggersRepository) DeleteHeartbeat(
	ctx context.Context,
	db DesktopDB,
	channelID uuid.UUID,
	channelIdentityID uuid.UUID,
) error {
	if channelID == uuid.Nil {
		return fmt.Errorf("channel_id must not be empty")
	}
	_, err := db.Exec(ctx,
		`DELETE FROM scheduled_triggers WHERE channel_id = $1 AND channel_identity_id = $2`,
		channelID.String(),
		channelIdentityID.String(),
	)
	return err
}

// ClaimDueHeartbeats 获取 next_fire_at 不晚于当前时间的记录（最多 limit 条），
// 并将 next_fire_at 延后 interval_min 分钟后返回（AT MOST ONCE 投递）。
func (ScheduledTriggersRepository) ClaimDueHeartbeats(
	ctx context.Context,
	db DesktopDB,
	limit int,
) ([]ScheduledTriggerRow, error) {
	if limit <= 0 {
		limit = 8
	}
	now := time.Now().UTC()
	rows, err := db.Query(ctx, `
		SELECT id, channel_id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at, next_fire_at, trigger_kind, job_id
		  FROM scheduled_triggers
		 WHERE next_fire_at <= $1
		 ORDER BY next_fire_at ASC
		 LIMIT $2`,
		now.Format(time.RFC3339Nano), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pending []ScheduledTriggerRow
	var pendingRaw []string
	for rows.Next() {
		var r ScheduledTriggerRow
		var idStr, channelStr, identityStr, accountStr, nextFireRaw string
		var triggerKind string
		var jobIDStr *string
		if err := rows.Scan(&idStr, &channelStr, &identityStr, &r.PersonaKey, &accountStr, &r.Model, &r.IntervalMin, &r.NextFireAt, &nextFireRaw, &triggerKind, &jobIDStr); err != nil {
			return nil, err
		}
		r.ID, _ = uuid.Parse(idStr)
		r.ChannelID, _ = uuid.Parse(channelStr)
		r.ChannelIdentityID, _ = uuid.Parse(identityStr)
		r.AccountID, _ = uuid.Parse(accountStr)
		r.TriggerKind = triggerKind
		if jobIDStr != nil {
			r.JobID, _ = uuid.Parse(*jobIDStr)
		}
		pending = append(pending, r)
		pendingRaw = append(pendingRaw, nextFireRaw)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	var out []ScheduledTriggerRow
	for i, r := range pending {
		next := advanceHeartbeatNextFireAt(r.NextFireAt, now, r.IntervalMin)
		tag, err := db.Exec(ctx,
			`UPDATE scheduled_triggers
			    SET next_fire_at = $1,
			        updated_at = $2
			  WHERE id = $3
			    AND next_fire_at = $4
			    AND interval_min = $5
			    AND model = $6`,
			next.Format(time.RFC3339Nano),
			now.Format(time.RFC3339Nano),
			r.ID,
			pendingRaw[i],
			r.IntervalMin,
			r.Model,
		)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		r.NextFireAt = next
		out = append(out, r)
	}
	return out, nil
}

// GetEarliestHeartbeatDue returns the earliest scheduled next_fire_at.
func (ScheduledTriggersRepository) GetEarliestHeartbeatDue(
	ctx context.Context,
	db DesktopDB,
) (*time.Time, error) {
	var next time.Time
	err := db.QueryRow(ctx,
		`SELECT next_fire_at
		   FROM scheduled_triggers
		  ORDER BY next_fire_at ASC
		  LIMIT 1`,
	).Scan(&next)
	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &next, nil
}

// PostponeHeartbeat 将指定 ID 的 next_fire_at 延后 delay（出错时用于退避重试）。
func (ScheduledTriggersRepository) PostponeHeartbeat(
	ctx context.Context,
	db DesktopDB,
	id uuid.UUID,
	delay time.Duration,
) error {
	next := time.Now().UTC().Add(delay)
	_, err := db.Exec(ctx,
		`UPDATE scheduled_triggers SET next_fire_at = $1 WHERE id = $2`,
		next.Format(time.RFC3339Nano), id,
	)
	return err
}

// DeleteTriggerByJobID 删除指定 job_id 的 trigger。
func (ScheduledTriggersRepository) DeleteTriggerByJobID(
	ctx context.Context,
	db DesktopDB,
	jobID uuid.UUID,
) error {
	_, err := db.Exec(ctx,
		`DELETE FROM scheduled_triggers WHERE job_id = $1`,
		jobID.String(),
	)
	return err
}

// UpdateTriggerNextFire 更新指定 trigger 的 next_fire_at。
func (ScheduledTriggersRepository) UpdateTriggerNextFire(
	ctx context.Context,
	db DesktopDB,
	id uuid.UUID,
	nextFireAt time.Time,
) error {
	_, err := db.Exec(ctx,
		`UPDATE scheduled_triggers SET next_fire_at = $1, updated_at = $2 WHERE id = $3`,
		nextFireAt.UTC().Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano),
		id.String(),
	)
	return err
}

func advanceHeartbeatNextFireAt(oldNextFireAt, now time.Time, intervalMin int) time.Time {
	intervalMin = normalizeHeartbeatInterval(intervalMin)
	step := time.Duration(intervalMin) * time.Minute
	next := oldNextFireAt.UTC()
	if next.IsZero() {
		next = now.UTC()
	}
	for !next.After(now) {
		next = next.Add(step)
	}
	return next
}

func (ScheduledTriggersRepository) ResolveHeartbeatThread(
	ctx context.Context,
	db DesktopDB,
	row ScheduledTriggerRow,
) (*HeartbeatThreadContext, error) {
	var platformSubjectID, channelType string
	err := db.QueryRow(ctx,
		`SELECT platform_subject_id, channel_type FROM channel_identities WHERE id = $1`,
		row.ChannelIdentityID.String(),
	).Scan(&platformSubjectID, &channelType)
	if err != nil {
		if isNoRows(err) {
			return nil, ErrHeartbeatIdentityGone
		}
		return nil, fmt.Errorf("query channel_identity: %w", err)
	}

	var personaIDStr string
	if strings.TrimSpace(row.PersonaKey) != "" {
		if err := db.QueryRow(ctx,
			`SELECT id FROM personas WHERE account_id = $1 AND persona_key = $2 ORDER BY created_at DESC LIMIT 1`,
			row.AccountID.String(),
			row.PersonaKey,
		).Scan(&personaIDStr); err != nil && !isNoRows(err) {
			return nil, fmt.Errorf("query persona for heartbeat trigger: %w", err)
		}
	}

	var (
		threadIDStr    string
		channelID      string
		conversationTy = "private"
		groupQuery     strings.Builder
		groupArgs      = []any{platformSubjectID, row.ChannelID.String(), row.AccountID.String()}
		groupFound     = false
	)

	groupQuery.WriteString(`
SELECT cgt.thread_id, cgt.channel_id
  FROM channel_group_threads cgt
  JOIN threads t ON t.id = cgt.thread_id
 WHERE cgt.platform_chat_id = $1
   AND cgt.channel_id = $2
   AND t.account_id = $3
   AND t.deleted_at IS NULL`)
	if personaIDStr != "" {
		groupQuery.WriteString(" AND cgt.persona_id = $4")
		groupArgs = append(groupArgs, personaIDStr)
	}
	groupQuery.WriteString(" ORDER BY cgt.created_at DESC LIMIT 1")

	if personaIDStr != "" {
		err = db.QueryRow(ctx, groupQuery.String(), groupArgs...).Scan(&threadIDStr, &channelID)
	} else {
		err = db.QueryRow(ctx, groupQuery.String(), groupArgs...).Scan(&threadIDStr, &channelID)
	}
	if err == nil && strings.TrimSpace(threadIDStr) != "" {
		conversationTy = resolveGroupConversationType(channelType)
		groupFound = true
	}
	if err != nil && !isNoRows(err) {
		return nil, fmt.Errorf("query channel_group_threads: %w", err)
	}

	if !groupFound {
		var dmChannelID string
		err = db.QueryRow(ctx,
			`SELECT cdt.thread_id, cdt.channel_id
			   FROM channel_dm_threads cdt
			   JOIN threads t ON t.id = cdt.thread_id
			  WHERE cdt.channel_id = $1
			    AND cdt.channel_identity_id = $2
			    AND t.account_id = $3
			    AND t.deleted_at IS NULL
			  LIMIT 1`,
			row.ChannelID.String(),
			row.ChannelIdentityID.String(),
			row.AccountID.String(),
		).Scan(&threadIDStr, &dmChannelID)
		if err != nil {
			if isNoRows(err) {
				return nil, fmt.Errorf("no thread found for channel_identity_id: %s", row.ChannelIdentityID)
			}
			return nil, fmt.Errorf("query channel_dm_threads: %w", err)
		}
		channelID = dmChannelID
		conversationTy = "private"
		platformConversationID := strings.TrimSpace(platformSubjectID)
		if err := db.QueryRow(ctx,
			`SELECT platform_conversation_id
			   FROM channel_message_ledger
			  WHERE channel_id = $1
			    AND sender_channel_identity_id = $2
			    AND platform_conversation_id <> ''
			  ORDER BY created_at DESC
			  LIMIT 1`,
			dmChannelID,
			row.ChannelIdentityID.String(),
		).Scan(&platformConversationID); err != nil && !isNoRows(err) {
			return nil, fmt.Errorf("query channel_message_ledger: %w", err)
		}
		platformSubjectID = strings.TrimSpace(platformConversationID)
	}

	threadID, err := uuid.Parse(threadIDStr)
	if err != nil {
		return nil, fmt.Errorf("parse thread_id: %w", err)
	}

	var creator uuid.NullUUID
	err = db.QueryRow(ctx,
		`SELECT created_by_user_id FROM threads WHERE id = $1`,
		threadIDStr,
	).Scan(&creator)
	if err != nil {
		if isNoRows(err) {
			return nil, fmt.Errorf("thread not found: %s", threadID)
		}
		return nil, fmt.Errorf("query thread: %w", err)
	}
	var createdBy *uuid.UUID
	if creator.Valid {
		createdBy = &creator.UUID
	}

	return &HeartbeatThreadContext{
		ThreadID:         threadID,
		ChannelID:        channelID,
		ChannelType:      channelType,
		PlatformChatID:   platformSubjectID,
		IdentityID:       row.ChannelIdentityID.String(),
		ConversationType: conversationTy,
		CreatedByUserID:  createdBy,
	}, nil
}

func (ScheduledTriggersRepository) HasActiveRootRun(
	ctx context.Context,
	db DesktopDB,
	threadID uuid.UUID,
) (bool, error) {
	if threadID == uuid.Nil {
		return false, fmt.Errorf("thread_id must not be empty")
	}
	var exists int
	err := db.QueryRow(ctx,
		`SELECT 1 FROM runs
		 WHERE thread_id = $1
		   AND parent_run_id IS NULL
		   AND status IN ('running', 'cancelling')
		 LIMIT 1`,
		threadID.String(),
	).Scan(&exists)
	if err != nil {
		if isNoRows(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// HeartbeatRunResult 是 DesktopCreateHeartbeatRun 的返回值，包含 run ID 和 channel delivery 上下文。
type HeartbeatRunResult struct {
	RunID            uuid.UUID
	ChannelID        string // channel_group_threads.channel_id
	ChannelType      string // channel_identities.channel_type
	PlatformChatID   string // 群的 platform_chat_id（即 platform_subject_id）
	IdentityID       string // scheduled_triggers.channel_identity_id
	ConversationType string // "supergroup" | "group" | "private"
}

// DesktopCreateHeartbeatRun 在 SQLite 中创建心跳 run。
// 通过 channel_identity_id 查 platform_subject_id，
// 再从 channel_group_threads（群会话）或 channel_dm_threads（私聊）找到 thread_id。
func DesktopCreateHeartbeatRun(
	ctx context.Context,
	db DesktopDB,
	row ScheduledTriggerRow,
	model string,
) (HeartbeatRunResult, error) {
	repo := ScheduledTriggersRepository{}
	ctxData, err := repo.ResolveHeartbeatThread(ctx, db, row)
	if err != nil {
		return HeartbeatRunResult{}, err
	}
	return DesktopCreateHeartbeatRunWithContext(ctx, db, row, model, ctxData)
}

// DesktopCreateHeartbeatRunWithContext 使用预先解析的线程上下文创建 run。
func (ScheduledTriggersRepository) InsertHeartbeatRunInTx(
	ctx context.Context,
	tx pgx.Tx,
	row ScheduledTriggerRow,
	ctxData *HeartbeatThreadContext,
	model string,
) (HeartbeatRunResult, error) {
	if ctxData == nil {
		return HeartbeatRunResult{}, fmt.Errorf("heartbeat thread context is nil")
	}
	var exists int
	err := tx.QueryRow(ctx,
		`SELECT 1 FROM runs
		 WHERE thread_id = $1
		   AND parent_run_id IS NULL
		   AND status IN ('running', 'cancelling')
		 LIMIT 1`,
		ctxData.ThreadID.String(),
	).Scan(&exists)
	if err != nil && !isNoRows(err) {
		return HeartbeatRunResult{}, fmt.Errorf("check active heartbeat run: %w", err)
	}
	if exists == 1 {
		return HeartbeatRunResult{}, ErrThreadBusy
	}

	runID := uuid.New()
	if _, err := tx.Exec(ctx,
		`INSERT INTO runs (id, account_id, thread_id, created_by_user_id, status)
		 VALUES ($1, $2, $3, $4, 'running')`,
		runID, row.AccountID, ctxData.ThreadID, ctxData.CreatedByUserID,
	); err != nil {
		return HeartbeatRunResult{}, fmt.Errorf("insert heartbeat run: %w", err)
	}

	result := HeartbeatRunResult{
		RunID:            runID,
		ChannelID:        ctxData.ChannelID,
		ChannelType:      ctxData.ChannelType,
		PlatformChatID:   ctxData.PlatformChatID,
		IdentityID:       ctxData.IdentityID,
		ConversationType: ctxData.ConversationType,
	}

	startedData, err := buildDesktopHeartbeatStartedData(ctx, tx, ctxData.ThreadID, row.PersonaKey, model, result)
	if err != nil {
		return HeartbeatRunResult{}, fmt.Errorf("build heartbeat run.started data: %w", err)
	}

	repo := DesktopRunEventsRepository{}
	if _, err := repo.AppendEvent(ctx, tx, runID, "run.started",
		startedData,
		nil, nil,
	); err != nil {
		return HeartbeatRunResult{}, fmt.Errorf("append run.started: %w", err)
	}

	return result, nil
}

func DesktopCreateHeartbeatRunWithContext(
	ctx context.Context,
	db DesktopDB,
	row ScheduledTriggerRow,
	model string,
	ctxData *HeartbeatThreadContext,
) (HeartbeatRunResult, error) {
	if ctxData == nil {
		return HeartbeatRunResult{}, fmt.Errorf("heartbeat thread context is nil")
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return HeartbeatRunResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck

	result, err := ScheduledTriggersRepository{}.InsertHeartbeatRunInTx(ctx, tx, row, ctxData, model)
	if err != nil {
		return HeartbeatRunResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return HeartbeatRunResult{}, fmt.Errorf("commit heartbeat run: %w", err)
	}
	return result, nil
}

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// resolveGroupConversationType 根据 channel_type 确定群聊的 conversation_type。
func resolveGroupConversationType(channelType string) string {
	switch strings.TrimSpace(channelType) {
	case "qq":
		return "group"
	default:
		return "supergroup"
	}
}

func buildDesktopHeartbeatStartedData(
	ctx context.Context,
	tx pgx.Tx,
	threadID uuid.UUID,
	personaKey string,
	model string,
	result HeartbeatRunResult,
) (map[string]any, error) {
	data := map[string]any{
		"persona_id":          personaKey,
		"model":               model,
		"run_kind":            runkind.Heartbeat,
		"continuation_source": "none",
		"continuation_loop":   false,
		"channel_delivery": map[string]any{
			"channel_id":                 result.ChannelID,
			"channel_type":               result.ChannelType,
			"sender_channel_identity_id": result.IdentityID,
			"conversation_type":          result.ConversationType,
			"conversation_ref": map[string]any{
				"target": result.PlatformChatID,
			},
		},
	}
	msg, err := getLatestDesktopThreadMessage(ctx, tx, threadID)
	if err != nil {
		return nil, err
	}
	if msg != nil {
		data["thread_tail_message_id"] = msg.ID.String()
	}
	return data, nil
}

func getLatestDesktopThreadMessage(ctx context.Context, tx pgx.Tx, threadID uuid.UUID) (*threadTailMessage, error) {
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("thread_id must not be empty")
	}
	var msg threadTailMessage
	err := tx.QueryRow(ctx,
		`SELECT id, role
		   FROM messages
		  WHERE thread_id = $1
		    AND hidden = FALSE
		    AND deleted_at IS NULL
		  ORDER BY thread_seq DESC
		  LIMIT 1`,
		threadID,
	).Scan(&msg.ID, &msg.Role)
	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	msg.Role = strings.TrimSpace(msg.Role)
	return &msg, nil
}

// HeartbeatIdentityConfig 是从 channel_identities 读到的 heartbeat 配置。
type HeartbeatIdentityConfig struct {
	Enabled         bool
	IntervalMinutes int
	Model           string
}

// GetGroupHeartbeatConfig 通过 channel_type + platform_subject_id 查群 identity 的 heartbeat 配置（desktop）。
// 返回 identityID 供 UpsertHeartbeat 使用。
func GetGroupHeartbeatConfig(ctx context.Context, db DesktopDB, channelType, platformSubjectID string) (uuid.UUID, *HeartbeatIdentityConfig, error) {
	var enabledInt, interval int
	var model, idStr string
	err := db.QueryRow(ctx,
		`SELECT id, heartbeat_enabled, heartbeat_interval_minutes, heartbeat_model
		   FROM channel_identities
		  WHERE channel_type = $1 AND platform_subject_id = $2`,
		channelType, platformSubjectID,
	).Scan(&idStr, &enabledInt, &interval, &model)
	if err != nil {
		if isNoRows(err) {
			return uuid.Nil, nil, nil
		}
		return uuid.Nil, nil, fmt.Errorf("get group heartbeat config: %w", err)
	}
	identityID, _ := uuid.Parse(idStr)
	return identityID, &HeartbeatIdentityConfig{
		Enabled:         enabledInt != 0,
		IntervalMinutes: interval,
		Model:           model,
	}, nil
}

// GetDMBindingHeartbeatConfig 从 channel_identity_links 读取私聊 binding 的 heartbeat 配置（desktop）。
func GetDMBindingHeartbeatConfig(ctx context.Context, db DesktopDB, channelID uuid.UUID, identityID uuid.UUID) (*HeartbeatIdentityConfig, error) {
	var enabledInt, interval int
	var model string
	err := db.QueryRow(ctx,
		`SELECT heartbeat_enabled, heartbeat_interval_minutes, heartbeat_model
		   FROM channel_identity_links
		  WHERE channel_id = $1 AND channel_identity_id = $2`,
		channelID.String(),
		identityID.String(),
	).Scan(&enabledInt, &interval, &model)
	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get dm binding heartbeat config: %w", err)
	}
	return &HeartbeatIdentityConfig{
		Enabled:         enabledInt != 0,
		IntervalMinutes: interval,
		Model:           model,
	}, nil
}
