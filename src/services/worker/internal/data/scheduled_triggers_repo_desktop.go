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

// ScheduledTriggerRow 是 scheduled_triggers 表的一行。
type ScheduledTriggerRow struct {
	ID                uuid.UUID
	ChannelIdentityID uuid.UUID
	PersonaKey        string
	AccountID         uuid.UUID
	Model             string
	IntervalMin       int
	NextFireAt        time.Time
}

// ScheduledTriggersRepository 是 SQLite 实现（desktop）。
type ScheduledTriggersRepository struct{}

// UpsertHeartbeat 注册或更新某个 channel identity 的 heartbeat 调度。
func (ScheduledTriggersRepository) UpsertHeartbeat(
	ctx context.Context,
	db DesktopDB,
	accountID uuid.UUID,
	channelIdentityID uuid.UUID,
	personaKey string,
	model string,
	intervalMin int,
) error {
	if channelIdentityID == uuid.Nil {
		return fmt.Errorf("channel_identity_id must not be empty")
	}
	if intervalMin <= 0 {
		intervalMin = runkind.DefaultHeartbeatIntervalMinutes
	}
	now := time.Now().UTC()
	nextFire := now.Add(time.Duration(intervalMin) * time.Minute)
	id := uuid.New()
	_, err := db.Exec(ctx, `
		INSERT INTO scheduled_triggers
		    (id, channel_identity_id, persona_key, account_id, model, interval_min, next_fire_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
		ON CONFLICT (channel_identity_id) DO UPDATE
		    SET persona_key   = excluded.persona_key,
		        account_id    = excluded.account_id,
		        model         = excluded.model,
		        interval_min  = excluded.interval_min,
		        next_fire_at  = excluded.next_fire_at,
		        updated_at    = excluded.updated_at`,
		id, channelIdentityID, personaKey, accountID, model, intervalMin,
		nextFire.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)
	return err
}

// DeleteHeartbeat 删除某个 channel identity 的 heartbeat 调度。
func (ScheduledTriggersRepository) DeleteHeartbeat(
	ctx context.Context,
	db DesktopDB,
	channelIdentityID uuid.UUID,
) error {
	_, err := db.Exec(ctx,
		`DELETE FROM scheduled_triggers WHERE channel_identity_id = $1`,
		channelIdentityID,
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
		SELECT id, channel_identity_id, persona_key, account_id, model, interval_min
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

	var out []ScheduledTriggerRow
	for rows.Next() {
		var r ScheduledTriggerRow
		var idStr, identityStr, accountStr string
		if err := rows.Scan(&idStr, &identityStr, &r.PersonaKey, &accountStr, &r.Model, &r.IntervalMin); err != nil {
			return nil, err
		}
		r.ID, _ = uuid.Parse(idStr)
		r.ChannelIdentityID, _ = uuid.Parse(identityStr)
		r.AccountID, _ = uuid.Parse(accountStr)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 将已取出的行的 next_fire_at 延后，防止重复投递
	for _, r := range out {
		next := now.Add(time.Duration(r.IntervalMin) * time.Minute)
		if _, err := db.Exec(ctx,
			`UPDATE scheduled_triggers SET next_fire_at = $1, updated_at = $2 WHERE id = $3 AND next_fire_at <= $1`,
			next.Format(time.RFC3339Nano),
			now.Format(time.RFC3339Nano),
			r.ID,
		); err != nil {
			return nil, err
		}
	}
	return out, nil
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

// DesktopCreateHeartbeatRun 在 SQLite 中创建心跳 run。
// 通过 channel_identity_id 查 platform_subject_id，
// 再从 channel_group_threads（群会话）或 channel_dm_threads（私聊）找到 thread_id。
func DesktopCreateHeartbeatRun(
	ctx context.Context,
	db DesktopDB,
	row ScheduledTriggerRow,
	model string,
) (uuid.UUID, error) {
	// 从 channel_identities 取 platform_subject_id
	var platformSubjectID string
	err := db.QueryRow(ctx,
		`SELECT platform_subject_id FROM channel_identities WHERE id = $1`,
		row.ChannelIdentityID.String(),
	).Scan(&platformSubjectID)
	if err != nil {
		if isNoRows(err) {
			return uuid.Nil, fmt.Errorf("channel_identity not found: %s", row.ChannelIdentityID)
		}
		return uuid.Nil, fmt.Errorf("query channel_identity: %w", err)
	}

	// 先查 channel_group_threads（群会话）
	var threadIDStr string
	err = db.QueryRow(ctx,
		`SELECT thread_id FROM channel_group_threads WHERE platform_chat_id = $1 LIMIT 1`,
		platformSubjectID,
	).Scan(&threadIDStr)
	if err != nil && !isNoRows(err) {
		return uuid.Nil, fmt.Errorf("query channel_group_threads: %w", err)
	}

	// fallback：查 channel_dm_threads（私聊）
	if isNoRows(err) || strings.TrimSpace(threadIDStr) == "" {
		err = db.QueryRow(ctx,
			`SELECT thread_id FROM channel_dm_threads WHERE channel_identity_id = $1 LIMIT 1`,
			row.ChannelIdentityID.String(),
		).Scan(&threadIDStr)
		if err != nil {
			if isNoRows(err) {
				return uuid.Nil, fmt.Errorf("no thread found for channel_identity_id: %s", row.ChannelIdentityID)
			}
			return uuid.Nil, fmt.Errorf("query channel_dm_threads: %w", err)
		}
	}

	threadID, err := uuid.Parse(threadIDStr)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse thread_id: %w", err)
	}

	// 从 threads 取 created_by_user_id
	var createdByUserID *uuid.UUID
	err = db.QueryRow(ctx,
		`SELECT created_by_user_id FROM threads WHERE id = $1`,
		threadIDStr,
	).Scan(&createdByUserID)
	if err != nil {
		if isNoRows(err) {
			return uuid.Nil, fmt.Errorf("thread not found: %s", threadID)
		}
		return uuid.Nil, fmt.Errorf("query thread: %w", err)
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return uuid.Nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	runID := uuid.New()
	if _, err := tx.Exec(ctx,
		`INSERT INTO runs (id, account_id, thread_id, created_by_user_id, status)
		 VALUES ($1, $2, $3, $4, 'running')`,
		runID, row.AccountID, threadID, createdByUserID,
	); err != nil {
		return uuid.Nil, fmt.Errorf("insert heartbeat run: %w", err)
	}

	repo := DesktopRunEventsRepository{}
	if _, err := repo.AppendEvent(ctx, tx, runID, "run.started",
		map[string]any{"persona_id": row.PersonaKey, "model": model},
		nil, nil,
	); err != nil {
		return uuid.Nil, fmt.Errorf("append run.started: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("commit heartbeat run: %w", err)
	}
	return runID, nil
}

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
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

// GetChannelIdentityHeartbeatConfig 从 channel_identities 读取 heartbeat 配置（desktop）。
func GetChannelIdentityHeartbeatConfig(ctx context.Context, db DesktopDB, identityID uuid.UUID) (*HeartbeatIdentityConfig, error) {
	var enabledInt, interval int
	var model string
	err := db.QueryRow(ctx,
		`SELECT heartbeat_enabled, heartbeat_interval_minutes, heartbeat_model
		   FROM channel_identities WHERE id = $1`,
		identityID.String(),
	).Scan(&enabledInt, &interval, &model)
	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get channel identity heartbeat config: %w", err)
	}
	return &HeartbeatIdentityConfig{
		Enabled:         enabledInt != 0,
		IntervalMinutes: interval,
		Model:           model,
	}, nil
}
