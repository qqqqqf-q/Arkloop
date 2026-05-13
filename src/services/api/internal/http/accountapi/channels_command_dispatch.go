package accountapi

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PreferenceResult carries structured data from /model and /think commands.
// Channels with rich UI (Telegram) use AvailableModels to build inline keyboards.
// Channels with plain text (WeChat) ignore it and just use the text reply.
type PreferenceResult struct {
	AvailableModels []ModelOption
	AllowUserScoped bool
	ThinkingMode    string // current mode for /think keyboard, "off"/"minimal"/"low"/"medium"/"high"/"max"
}

// ModelOption represents a single model choice in the preference UI.
type ModelOption struct {
	Model      string
	IsSelected bool
}

// ChannelCommandResolver provides channel-specific operations needed by DispatchChannelCommand.
type ChannelCommandResolver struct {
	// ResolveThreadID resolves the thread ID for this channel.
	// Takes personaID + projectID, returns threadID.
	ResolveThreadID func(ctx context.Context, tx pgx.Tx, personaID, projectID uuid.UUID, isPrivate bool, platformChatID string) (uuid.UUID, error)

	// ResolveHeartbeatIdentity resolves the identity used for heartbeat config.
	// For group chats, this should be the group identity. For private chats, use the user identity.
	// If nil and in a group chat, the user identity is used as-is.
	ResolveHeartbeatIdentity func(ctx context.Context, tx pgx.Tx) (*data.ChannelIdentity, error)

	// IsGroupAdmin checks if the sender is a group admin (for /new /stop in groups).
	// nil = skip admin check.
	IsGroupAdmin func(ctx context.Context) bool
}

// DispatchChannelCommand handles command dispatch for all text-based IM channels.
// It detects the command from commandText, resolves projectID/threadID, and dispatches
// to the appropriate handler.
//
// The caller is responsible for:
//   - Starting and committing the transaction
//   - Sending the reply via channel-specific mechanism
//   - Any channel-specific text preprocessing (e.g., stripLeadingMention)
func DispatchChannelCommand(
	ctx context.Context,
	tx pgx.Tx,
	ch data.Channel,
	persona data.Persona,
	identity data.ChannelIdentity,
	commandText string,
	isPrivate bool,
	platformChatID string,
	defaultModel string,
	entSvc *entitlement.Service,
	resolver ChannelCommandResolver,
	channelIdentitiesRepo *data.ChannelIdentitiesRepository,
	channelDMThreadsRepo *data.ChannelDMThreadsRepository,
	channelGroupThreadsRepo *data.ChannelGroupThreadsRepository,
	personasRepo *data.PersonasRepository,
	runEventRepo *data.RunEventRepository,
) (handled bool, replyText string, prefResult *PreferenceResult, cancelRunID uuid.UUID, err error) {
	cmd, ok := telegramCommandBase(strings.TrimSpace(commandText), "")
	if !ok {
		return false, "", nil, uuid.Nil, nil
	}

	// Resolve projectID
	threadProjectID := derefUUID(persona.ProjectID)
	if threadProjectID == uuid.Nil {
		ownerUserID := uuid.Nil
		if ch.OwnerUserID != nil {
			ownerUserID = *ch.OwnerUserID
		}
		if ownerUserID == uuid.Nil && identity.UserID != nil {
			ownerUserID = *identity.UserID
		}
		if ownerUserID != uuid.Nil {
			if pid, err := personasRepo.GetOrCreateDefaultProjectIDByOwner(ctx, ch.AccountID, ownerUserID); err == nil {
				threadProjectID = pid
			}
		}
	}

	// resolveThreadID is a helper for commands that need a thread
	resolveThreadID := func() (uuid.UUID, error) {
		if threadProjectID == uuid.Nil {
			return uuid.Nil, fmt.Errorf("cannot resolve project for persona %s", persona.ID)
		}
		if resolver.ResolveThreadID == nil {
			return uuid.Nil, fmt.Errorf("thread resolution not configured")
		}
		return resolver.ResolveThreadID(ctx, tx, persona.ID, threadProjectID, isPrivate, platformChatID)
	}

	switch {
	case cmd == "/model" || strings.HasPrefix(cmd, "/think"):
		threadID, err := resolveThreadID()
		if err != nil {
			return true, "", nil, uuid.Nil, err
		}
		replyText, prefResult, err = handleTelegramPreferenceCommand(ctx, tx, ch.AccountID, threadID, strings.TrimSpace(commandText), entSvc)
		return true, replyText, prefResult, uuid.Nil, err

	case strings.HasPrefix(cmd, "/heartbeat"):
		threadID, err := resolveThreadID()
		if err != nil {
			return true, "", nil, uuid.Nil, err
		}
		heartbeatIdentity := identity
		if !isPrivate && resolver.ResolveHeartbeatIdentity != nil {
			gi, err := resolver.ResolveHeartbeatIdentity(ctx, tx)
			if err != nil {
				return true, "", nil, uuid.Nil, err
			}
			if gi != nil {
				heartbeatIdentity = *gi
			}
		}
		replyText, err = handleTelegramHeartbeatCommand(
			ctx, tx,
			ch.ID, ch.AccountID, ch.PersonaID,
			defaultModel,
			threadID,
			heartbeatIdentity,
			strings.TrimSpace(commandText),
			channelIdentitiesRepo,
			personasRepo,
			entSvc,
		)
		return true, replyText, nil, uuid.Nil, err

	case cmd == "/new":
		if ch.PersonaID == nil || *ch.PersonaID == uuid.Nil {
			return true, "当前会话未配置 persona。", nil, uuid.Nil, nil
		}
		if !isPrivate && resolver.IsGroupAdmin != nil && !resolver.IsGroupAdmin(ctx) {
			return true, "无权限。", nil, uuid.Nil, nil
		}
		if isPrivate {
			if channelDMThreadsRepo != nil {
				if err := channelDMThreadsRepo.WithTx(tx).DeleteByBinding(ctx, ch.ID, identity.ID, *ch.PersonaID, ""); err != nil {
					slog.WarnContext(ctx, "channel_command_new_delete_dm_failed", "error", err, "channel_id", ch.ID, "identity_id", identity.ID)
					return true, "操作失败。", nil, uuid.Nil, nil
				}
			}
		} else {
			if channelGroupThreadsRepo != nil {
				if err := channelGroupThreadsRepo.WithTx(tx).DeleteByBinding(ctx, ch.ID, platformChatID, *ch.PersonaID); err != nil {
					slog.WarnContext(ctx, "channel_command_new_delete_group_failed", "error", err, "channel_id", ch.ID, "platform_chat_id", platformChatID)
					return true, "操作失败。", nil, uuid.Nil, nil
				}
			}
		}
		return true, "已开启新会话。", nil, uuid.Nil, nil

	case cmd == "/stop":
		if ch.PersonaID == nil || *ch.PersonaID == uuid.Nil {
			return true, "当前没有运行中的任务。", nil, uuid.Nil, nil
		}
		if !isPrivate && resolver.IsGroupAdmin != nil && !resolver.IsGroupAdmin(ctx) {
			return true, "无权限。", nil, uuid.Nil, nil
		}
		threadID, err := resolveThreadID()
		if err != nil {
			return true, "当前没有运行中的任务。", nil, uuid.Nil, err
		}
		activeRun, _ := runEventRepo.WithTx(tx).GetActiveRootRunForThread(ctx, threadID)
		if activeRun == nil {
			return true, "当前没有运行中的任务。", nil, uuid.Nil, nil
		}
		if _, err := runEventRepo.WithTx(tx).RequestCancel(ctx, activeRun.ID, nil, "", 0, nil); err != nil {
			return true, "", nil, uuid.Nil, err
		}
		return true, "已请求停止当前任务。", nil, activeRun.ID, nil

	default:
		return false, "", nil, uuid.Nil, nil
	}
}
