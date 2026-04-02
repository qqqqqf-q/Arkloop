//go:build desktop

package pipeline

import (
	"context"
	"log/slog"

	"arkloop/services/worker/internal/data"
)

func checkSenderIsAdmin(ctx context.Context, rc *RunContext) bool {
	if rc.ChannelContext == nil || rc.ChannelContext.SenderUserID == nil {
		return false
	}
	if rc.DB == nil {
		return false
	}

	repo := data.AccountMembershipsRepository{}
	membership, err := repo.GetByAccountAndUser(ctx, rc.DB, rc.Run.AccountID, *rc.ChannelContext.SenderUserID)
	if err != nil {
		slog.WarnContext(ctx, "runtime_context: failed to query sender membership", "error", err)
		return false
	}
	if membership == nil {
		return false
	}

	return membership.Role == "account_admin" || membership.Role == "platform_admin"
}
