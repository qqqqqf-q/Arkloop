package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"

	"arkloop/services/worker/internal/data"
)

func NewRuntimeContextMiddleware() RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		block := buildRuntimeContextBlock(ctx, rc)
		if block != "" {
			rc.SystemPrompt += "\n\n" + block
		}
		return next(ctx, rc)
	}
}

func buildRuntimeContextBlock(ctx context.Context, rc *RunContext) string {
	if rc.ChannelContext == nil {
		return ""
	}

	channelType := rc.ChannelContext.ChannelType
	convType := rc.ChannelContext.ConversationType
	host := hostMode

	isAdmin := checkSenderIsAdmin(ctx, rc)
	rc.SenderIsAdmin = isAdmin

	line := fmt.Sprintf("Channel: %s | Type: %s | Host: %s | Admin: %v",
		channelType, convType, host, isAdmin)

	if rc.ChannelContext.SenderUserID != nil {
		h := sha256.Sum256([]byte(rc.ChannelContext.SenderUserID.String()))
		senderHash := hex.EncodeToString(h[:])[:12]
		line += fmt.Sprintf(" | Sender: %s", senderHash)
	}

	return "## Runtime Context\n" + line
}

func checkSenderIsAdmin(ctx context.Context, rc *RunContext) bool {
	if rc.ChannelContext == nil || rc.ChannelContext.SenderUserID == nil {
		return false
	}
	if rc.Pool == nil {
		return false
	}

	repo := data.AccountMembershipsRepository{}
	membership, err := repo.GetByAccountAndUser(ctx, rc.Pool, rc.Run.AccountID, *rc.ChannelContext.SenderUserID)
	if err != nil {
		slog.WarnContext(ctx, "runtime_context: failed to query sender membership", "error", err)
		return false
	}
	if membership == nil {
		return false
	}

	return membership.Role == "account_admin" || membership.Role == "platform_admin"
}
