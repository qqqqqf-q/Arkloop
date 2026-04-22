package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"arkloop/services/shared/pgnotify"
	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
)

// NewHeartbeatScheduleMiddleware 在 run 结束后 upsert scheduled_triggers。
// 群聊以群 identity（platform_chat_id 对应）为唯一键，私聊以 sender identity 为唯一键，
// interval/model 从 channel_identities 的 heartbeat_* 列读取（由 /heartbeat 命令写入）。
// heartbeat run 本身不执行（避免无限循环）。
func NewHeartbeatScheduleMiddleware(db data.DB) RunMiddleware {
	repo := data.ScheduledTriggersRepository{}
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		err := next(ctx, rc)

		if err != nil || rc == nil || db == nil {
			return err
		}
		if rc.HeartbeatRun {
			return updateHeartbeatCooldown(ctx, db, rc, repo)
		}
		if rc.ChannelContext == nil {
			slog.DebugContext(ctx, "heartbeat_schedule: no channel context")
			return nil
		}
		channelID, identityID, cfg, targetKind, lookupKey := resolveHeartbeatIdentityConfig(ctx, db, rc)
		if identityID == uuid.Nil && cfg == nil {
			slog.DebugContext(ctx, "heartbeat_schedule: no heartbeat target", "conversation_type", strings.TrimSpace(rc.ChannelContext.ConversationType))
			return nil
		}
		if cfg == nil {
			slog.DebugContext(ctx, "heartbeat_schedule: target identity has no heartbeat config", "identity_id", identityID, "target_kind", targetKind)
			return nil
		}

		def := rc.PersonaDefinition
		if def == nil || !def.HeartbeatEnabled {
			if identityID != uuid.Nil {
				if deleteErr := repo.DeleteHeartbeat(ctx, db, channelID, identityID); deleteErr != nil {
					slog.WarnContext(ctx, "heartbeat_schedule: delete persona-disabled trigger failed", "identity_id", identityID, "error", deleteErr)
				} else {
					notifyHeartbeatScheduler(ctx, rc)
					slog.InfoContext(ctx, "heartbeat_schedule: deleted persona-disabled trigger", "identity_id", identityID)
				}
			}
			slog.DebugContext(ctx, "heartbeat_schedule: persona heartbeat disabled", "persona", func() string {
				if def == nil {
					return "<nil>"
				}
				return def.ID
			}())
			return nil
		}

		if cfg == nil || !cfg.Enabled {
			if identityID != uuid.Nil {
				if deleteErr := repo.DeleteHeartbeat(ctx, db, channelID, identityID); deleteErr != nil {
					slog.WarnContext(ctx, "heartbeat_schedule: delete disabled trigger failed", "identity_id", identityID, "error", deleteErr)
				} else {
					notifyHeartbeatScheduler(ctx, rc)
					slog.InfoContext(ctx, "heartbeat_schedule: deleted disabled trigger", "identity_id", identityID)
				}
			}
			slog.DebugContext(ctx, "heartbeat_schedule: channel heartbeat disabled",
				"identity_id", identityID,
				"cfg_nil", cfg == nil,
				"enabled", func() bool {
					if cfg == nil {
						return false
					}
					return cfg.Enabled
				}(),
				"lookup_key", lookupKey,
				"target_kind", targetKind,
			)
			return nil
		}

		iv := cfg.IntervalMinutes
		if iv <= 0 {
			iv = 30
		}
		model := strings.TrimSpace(cfg.Model)

		// model fallback：InputJSON → PersonaDefinition
		if model == "" {
			if m, ok := rc.InputJSON["model"].(string); ok && strings.TrimSpace(m) != "" {
				model = strings.TrimSpace(m)
			}
		}
		if model == "" && def.Model != nil {
			model = strings.TrimSpace(*def.Model)
		}

		existing, getErr := repo.GetHeartbeat(ctx, db, channelID, identityID)
		if getErr != nil {
			slog.WarnContext(ctx, "heartbeat_schedule: get trigger failed", "identity_id", identityID, "error", getErr)
			return nil
		}

		if existing == nil {
			if upsertErr := repo.UpsertHeartbeat(ctx, db, rc.Run.AccountID, channelID, identityID, def.ID, model, iv); upsertErr != nil {
				slog.WarnContext(ctx, "heartbeat_schedule: create trigger failed", "identity_id", identityID, "error", upsertErr)
				return nil
			}
			notifyHeartbeatScheduler(ctx, rc)
			slog.InfoContext(ctx, "heartbeat_schedule: created trigger", "identity_id", identityID, "interval_min", iv, "model", model)
			return nil
		}

		intervalChanged := existing.IntervalMin != iv
		modelChanged := strings.TrimSpace(existing.Model) != model
		personaChanged := strings.TrimSpace(existing.PersonaKey) != def.ID
		accountChanged := existing.AccountID != rc.Run.AccountID
		if intervalChanged || modelChanged || personaChanged || accountChanged {
			if upsertErr := repo.UpsertHeartbeat(ctx, db, rc.Run.AccountID, channelID, identityID, def.ID, model, iv); upsertErr != nil {
				slog.WarnContext(ctx, "heartbeat_schedule: update trigger metadata failed", "identity_id", identityID, "error", upsertErr)
				return nil
			}
			if intervalChanged {
				nextFire, resetErr := repo.ResetHeartbeatNextFire(ctx, db, channelID, identityID, iv)
				if resetErr != nil {
					slog.WarnContext(ctx, "heartbeat_schedule: reschedule trigger failed", "identity_id", identityID, "error", resetErr)
					return nil
				}
				notifyHeartbeatScheduler(ctx, rc)
				slog.InfoContext(ctx, "heartbeat_schedule: rescheduled trigger", "identity_id", identityID, "interval_min", iv, "model", model, "next_fire_at", nextFire)
				return nil
			}
			slog.InfoContext(ctx, "heartbeat_schedule: updated trigger metadata", "identity_id", identityID, "interval_min", iv, "model", model)
			return nil
		}
		slog.DebugContext(ctx, "heartbeat_schedule: trigger unchanged", "identity_id", identityID)
		return nil
	}
}

func resolveHeartbeatIdentityConfig(ctx context.Context, db data.DB, rc *RunContext) (uuid.UUID, uuid.UUID, *data.HeartbeatIdentityConfig, string, string) {
	if rc == nil || rc.ChannelContext == nil || db == nil {
		return uuid.Nil, uuid.Nil, nil, "", ""
	}
	channelType := strings.TrimSpace(rc.ChannelContext.ChannelType)
	if channelType == "" {
		channelType = "telegram"
	}
	if IsTelegramGroupLikeConversation(rc.ChannelContext.ConversationType) {
		platformChatID := strings.TrimSpace(rc.ChannelContext.Conversation.Target)
		if platformChatID == "" {
			slog.WarnContext(ctx, "heartbeat_schedule: no platform_chat_id for group conversation")
			return uuid.Nil, uuid.Nil, nil, "group", ""
		}
		identityID, cfg, err := data.GetGroupHeartbeatConfig(ctx, db, channelType, platformChatID)
		if err != nil {
			slog.WarnContext(ctx, "heartbeat_schedule: get group heartbeat config failed", "error", err)
			return uuid.Nil, uuid.Nil, nil, "group", platformChatID
		}
		return rc.ChannelContext.ChannelID, identityID, cfg, "group", platformChatID
	}
	if isPrivateChannelConversation(rc.ChannelContext.ConversationType) {
		channelID := rc.ChannelContext.ChannelID
		identityID := rc.ChannelContext.SenderChannelIdentityID
		if identityID == uuid.Nil {
			slog.WarnContext(ctx, "heartbeat_schedule: no sender identity for private conversation")
			return channelID, uuid.Nil, nil, "direct", ""
		}
		cfg, err := data.GetDMBindingHeartbeatConfig(ctx, db, channelID, identityID)
		if err != nil {
			slog.WarnContext(ctx, "heartbeat_schedule: get direct heartbeat config failed", "identity_id", identityID, "error", err)
			return channelID, uuid.Nil, nil, "direct", identityID.String()
		}
		return channelID, identityID, cfg, "direct", identityID.String()
	}
	return uuid.Nil, uuid.Nil, nil, "", ""
}

func isPrivateChannelConversation(ct string) bool {
	switch strings.ToLower(strings.TrimSpace(ct)) {
	case "private", "dm":
		return true
	default:
		return false
	}
}

func updateHeartbeatCooldown(ctx context.Context, db data.DB, rc *RunContext, repo data.ScheduledTriggersRepository) error {
	channelID, identityID, _, _, _ := resolveHeartbeatIdentityConfig(ctx, db, rc)
	if identityID == uuid.Nil {
		return nil
	}

	existing, err := repo.GetHeartbeat(ctx, db, channelID, identityID)
	if err != nil || existing == nil {
		return nil
	}

	snapshotLastUserMsg := existing.LastUserMsgAt

	now := time.Now().UTC()
	var newLevel int
	var nextFire time.Time

	if rc.HeartbeatToolOutcome != nil && rc.HeartbeatToolOutcome.Reply {
		newLevel = 0
		nextFire = now.Add(1 * time.Minute)
	} else {
		newLevel = existing.CooldownLevel + 1
		if newLevel > 2 {
			newLevel = 2
		}
		nextFire = now.Add(idleIntervalForLevel(newLevel))
	}

	if err := repo.UpdateCooldownAfterHeartbeat(ctx, db, channelID, identityID, newLevel, nextFire, snapshotLastUserMsg); err != nil {
		if errors.Is(err, data.ErrHeartbeatSnapshotStale) {
			slog.DebugContext(ctx, "heartbeat_schedule: skip cooldown update due to stale snapshot", "channel_id", channelID, "identity_id", identityID)
			return nil
		}
		slog.WarnContext(ctx, "heartbeat_schedule: update cooldown failed", "error", err)
		return nil
	}

	notifyHeartbeatScheduler(ctx, rc)
	return nil
}

func idleIntervalForLevel(level int) time.Duration {
	switch level {
	case 0:
		return 1 * time.Minute
	case 1:
		return 15 * time.Minute
	case 2:
		return 60 * time.Minute
	default:
		return 60 * time.Minute
	}
}

func notifyHeartbeatScheduler(ctx context.Context, rc *RunContext) {
	if rc == nil {
		return
	}
	if rc.EventBus != nil {
		_ = rc.EventBus.Publish(ctx, pgnotify.ChannelHeartbeat, "")
	}
	if rc.DirectPool != nil {
		_, _ = rc.DirectPool.Exec(ctx, "SELECT pg_notify($1, '')", pgnotify.ChannelHeartbeat)
		return
	}
	if rc.Pool != nil {
		_, _ = rc.Pool.Exec(ctx, "SELECT pg_notify($1, '')", pgnotify.ChannelHeartbeat)
	}
}
