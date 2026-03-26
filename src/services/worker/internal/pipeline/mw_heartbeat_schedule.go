package pipeline

import (
	"context"
	"log/slog"
	"strings"

	"arkloop/services/shared/pgnotify"
	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
)

// NewHeartbeatScheduleMiddleware 在 run 结束后 upsert scheduled_triggers。
// 以群的 channel identity（platform_chat_id 对应）为唯一键，
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
			slog.DebugContext(ctx, "heartbeat_schedule: skip heartbeat run")
			return nil
		}
		if rc.ChannelContext == nil || !IsTelegramGroupLikeConversation(rc.ChannelContext.ConversationType) {
			slog.DebugContext(ctx, "heartbeat_schedule: not a telegram group conversation")
			return nil
		}

		// 用群的 platform_chat_id 查群 identity（heartbeat 配置挂在群上，不是 sender）
		platformChatID := strings.TrimSpace(rc.ChannelContext.Conversation.Target)
		if platformChatID == "" {
			slog.WarnContext(ctx, "heartbeat_schedule: no platform_chat_id, skipping")
			return nil
		}
		channelType := strings.TrimSpace(rc.ChannelContext.ChannelType)
		if channelType == "" {
			channelType = "telegram"
		}

		identityID, cfg, cfgErr := data.GetGroupHeartbeatConfig(ctx, db, channelType, platformChatID)
		if cfgErr != nil {
			slog.WarnContext(ctx, "heartbeat_schedule: get group heartbeat config failed", "error", cfgErr)
			return nil
		}

		def := rc.PersonaDefinition
		if def == nil || !def.HeartbeatEnabled {
			if identityID != uuid.Nil {
				if deleteErr := repo.DeleteHeartbeat(ctx, db, identityID); deleteErr != nil {
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
				if deleteErr := repo.DeleteHeartbeat(ctx, db, identityID); deleteErr != nil {
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
				"platform_chat_id", platformChatID,
				"channel_type", channelType,
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

		existing, getErr := repo.GetHeartbeat(ctx, db, identityID)
		if getErr != nil {
			slog.WarnContext(ctx, "heartbeat_schedule: get trigger failed", "identity_id", identityID, "error", getErr)
			return nil
		}

		if existing == nil {
			if upsertErr := repo.UpsertHeartbeat(ctx, db, rc.Run.AccountID, identityID, def.ID, model, iv); upsertErr != nil {
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
			if upsertErr := repo.UpsertHeartbeat(ctx, db, rc.Run.AccountID, identityID, def.ID, model, iv); upsertErr != nil {
				slog.WarnContext(ctx, "heartbeat_schedule: update trigger metadata failed", "identity_id", identityID, "error", upsertErr)
				return nil
			}
			if intervalChanged {
				nextFire, resetErr := repo.ResetHeartbeatNextFire(ctx, db, identityID, iv)
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
