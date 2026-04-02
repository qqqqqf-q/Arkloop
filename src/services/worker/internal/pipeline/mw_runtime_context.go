package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
