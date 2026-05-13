package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"arkloop/services/shared/pgnotify"
	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
)

// NewHeartbeatScheduleMiddleware 在 run 结束后按 thread upsert scheduled_triggers。
// interval/model 优先从 threads.config_json 读取；缺失时回退旧 identity/binding 配置。
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
		channelID, identityID, cfg, targetKind, lookupKey := resolveHeartbeatThreadConfig(ctx, db, rc)
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
				if deleteErr := deleteHeartbeatSchedule(ctx, db, repo, rc, channelID, identityID); deleteErr != nil {
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
				if deleteErr := deleteHeartbeatSchedule(ctx, db, repo, rc, channelID, identityID); deleteErr != nil {
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

		// If heartbeat follows the conversation, use the current channel default.
		if model == "" {
			model = currentChannelDefaultModel(ctx, db, channelID)
		}
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
			if upsertErr := upsertHeartbeatSchedule(ctx, db, repo, rc, channelID, identityID, def.ID, model, iv); upsertErr != nil {
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
			if upsertErr := upsertHeartbeatSchedule(ctx, db, repo, rc, channelID, identityID, def.ID, model, iv); upsertErr != nil {
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

func upsertHeartbeatSchedule(ctx context.Context, db data.DB, repo data.ScheduledTriggersRepository, rc *RunContext, channelID, identityID uuid.UUID, personaKey, model string, intervalMin int) error {
	if rc != nil && rc.Run.ThreadID != uuid.Nil {
		return repo.UpsertHeartbeatForThread(ctx, db, rc.Run.AccountID, channelID, identityID, rc.Run.ThreadID, personaKey, model, intervalMin)
	}
	return nil
}

func deleteHeartbeatSchedule(ctx context.Context, db data.DB, repo data.ScheduledTriggersRepository, rc *RunContext, channelID, identityID uuid.UUID) error {
	if rc != nil && rc.Run.ThreadID != uuid.Nil {
		return repo.DeleteHeartbeatForThread(ctx, db, rc.Run.ThreadID)
	}
	return repo.DeleteHeartbeat(ctx, db, channelID, identityID)
}

func currentChannelDefaultModel(ctx context.Context, db data.DB, channelID uuid.UUID) string {
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil || channelID == uuid.Nil {
		return ""
	}
	var raw json.RawMessage
	if err := db.QueryRow(ctx, `SELECT config_json FROM channels WHERE id = $1`, channelID).Scan(&raw); err != nil {
		return ""
	}
	var payload struct {
		DefaultModel string `json:"default_model"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.DefaultModel)
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

func resolveHeartbeatThreadConfig(ctx context.Context, db data.DB, rc *RunContext) (uuid.UUID, uuid.UUID, *data.HeartbeatIdentityConfig, string, string) {
	channelID, identityID, legacyCfg, targetKind, lookupKey := resolveHeartbeatIdentityConfig(ctx, db, rc)
	if rc == nil || rc.Run.ThreadID == uuid.Nil || db == nil {
		return channelID, identityID, legacyCfg, targetKind, lookupKey
	}
	cfg, err := getThreadHeartbeatConfig(ctx, db, rc.Run.ThreadID)
	if err != nil {
		slog.WarnContext(ctx, "heartbeat_schedule: get thread heartbeat config failed", "thread_id", rc.Run.ThreadID, "error", err)
		return channelID, identityID, legacyCfg, targetKind, lookupKey
	}
	if cfg != nil {
		return channelID, identityID, cfg, targetKind, lookupKey
	}
	if legacyCfg != nil {
		slog.WarnContext(ctx, "heartbeat_schedule: using deprecated identity heartbeat config", "thread_id", rc.Run.ThreadID, "identity_id", identityID)
	}
	return channelID, identityID, legacyCfg, targetKind, lookupKey
}

func getThreadHeartbeatConfig(ctx context.Context, db data.DB, threadID uuid.UUID) (*data.HeartbeatIdentityConfig, error) {
	var raw json.RawMessage
	if err := db.QueryRow(ctx, `SELECT COALESCE(config_json, '{}') FROM threads WHERE id = $1`, threadID).Scan(&raw); err != nil {
		return nil, err
	}
	var cfg data.ThreadConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.HeartbeatEnabled == nil {
		return nil, nil
	}
	return &data.HeartbeatIdentityConfig{
		Enabled:         *cfg.HeartbeatEnabled,
		IntervalMinutes: cfg.HeartbeatIntervalMinute,
		Model:           strings.TrimSpace(cfg.HeartbeatModel),
	}, nil
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
	if identityID == uuid.Nil && (rc == nil || rc.Run.ThreadID == uuid.Nil) {
		return nil
	}

	var existing *data.ScheduledTriggerRow
	var err error
	if rc != nil && rc.Run.ThreadID != uuid.Nil {
		existing, err = repo.GetHeartbeatForThread(ctx, db, rc.Run.ThreadID)
	} else {
		existing, err = repo.GetHeartbeat(ctx, db, channelID, identityID)
	}
	if err != nil || existing == nil {
		return nil
	}

	snapshotLastUserMsg := existing.LastUserMsgAt

	now := time.Now().UTC()
	var newLevel int
	var nextFire time.Time

	if existing.CooldownLevel == 0 {
		newLevel = existing.CooldownLevel + 1
		nextFire = now.Add(time.Minute)
	} else {
		newLevel = existing.CooldownLevel + 1
		nextFire = suspendHeartbeatUntilNextMessage(now)
	}

	var updateErr error
	if rc != nil && rc.Run.ThreadID != uuid.Nil {
		updateErr = repo.UpdateCooldownAfterHeartbeatForThread(ctx, db, rc.Run.ThreadID, newLevel, nextFire, snapshotLastUserMsg)
	} else {
		updateErr = repo.UpdateCooldownAfterHeartbeat(ctx, db, channelID, identityID, newLevel, nextFire, snapshotLastUserMsg)
	}
	if updateErr != nil {
		if errors.Is(updateErr, data.ErrHeartbeatSnapshotStale) {
			slog.DebugContext(ctx, "heartbeat_schedule: skip cooldown update due to stale snapshot", "channel_id", channelID, "identity_id", identityID)
			return nil
		}
		slog.WarnContext(ctx, "heartbeat_schedule: update cooldown failed", "error", updateErr)
		return nil
	}

	notifyHeartbeatScheduler(ctx, rc)
	return nil
}

func suspendHeartbeatUntilNextMessage(now time.Time) time.Time {
	return now.AddDate(1, 0, 0)
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
