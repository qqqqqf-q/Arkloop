package pipeline

import (
	"context"
	"log/slog"
	"strings"

	"arkloop/services/worker/internal/data"
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
			return nil
		}
		def := rc.PersonaDefinition
		if def == nil || !def.HeartbeatEnabled {
			return nil
		}
		if rc.ChannelContext == nil || !IsTelegramGroupLikeConversation(rc.ChannelContext.ConversationType) {
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
		if cfg == nil || !cfg.Enabled {
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

		if upsertErr := repo.UpsertHeartbeat(ctx, db, rc.Run.AccountID, identityID, def.ID, model, iv); upsertErr != nil {
			slog.WarnContext(ctx, "heartbeat schedule upsert failed", "identity_id", identityID, "error", upsertErr)
		} else {
			slog.InfoContext(ctx, "heartbeat schedule upserted", "identity_id", identityID, "interval_min", iv, "model", model)
		}
		return nil
	}
}
