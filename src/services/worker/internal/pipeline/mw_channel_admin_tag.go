package pipeline

import (
	"context"
	"log/slog"
	"strings"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"

	"github.com/google/uuid"
)

// NewChannelAdminTagMiddleware 扫描 rc.Messages 中的 sender-ref，
// 查出关联了 admin 用户的 identity，在 YAML header 中注入 admin: true。
// 必须在 TelegramGroupUserMerge 之前执行。
func NewChannelAdminTagMiddleware(db data.DB) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc == nil || db == nil || rc.ChannelContext == nil {
			return next(ctx, rc)
		}
		if !isTelegramOrDiscordChannel(rc.ChannelContext.ChannelType) {
			return next(ctx, rc)
		}

		adminRefs := resolveAdminIdentityRefs(ctx, db, rc)
		if len(adminRefs) > 0 {
			tagMessagesWithAdmin(rc, adminRefs)
		}

		return next(ctx, rc)
	}
}

func isTelegramOrDiscordChannel(channelType string) bool {
	ct := strings.ToLower(strings.TrimSpace(channelType))
	return ct == "telegram" || ct == "discord"
}

// resolveAdminIdentityRefs 收集消息中所有 sender-ref，批量查出哪些是 admin。
func resolveAdminIdentityRefs(ctx context.Context, db data.DB, rc *RunContext) map[string]struct{} {
	refs := collectSenderRefs(rc.Messages)
	if len(refs) == 0 {
		return nil
	}

	identityIDs := make([]uuid.UUID, 0, len(refs))
	for ref := range refs {
		id, err := uuid.Parse(ref)
		if err != nil {
			continue
		}
		identityIDs = append(identityIDs, id)
	}
	if len(identityIDs) == 0 {
		return nil
	}

	adminSet := queryAdminIdentities(ctx, db, rc.Run.AccountID, identityIDs)
	return adminSet
}

func collectSenderRefs(messages []llm.Message) map[string]struct{} {
	refs := map[string]struct{}{}
	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		for _, part := range msg.Content {
			text := llm.PartPromptText(part)
			if !strings.HasPrefix(text, "---\n") {
				continue
			}
			meta, _, ok := parseTelegramEnvelopeText(text)
			if !ok {
				continue
			}
			ref := strings.TrimSpace(meta["sender-ref"])
			if ref != "" {
				refs[ref] = struct{}{}
			}
		}
	}
	return refs
}

// queryAdminIdentities 批量查 channel_identities JOIN account_memberships，
// 返回属于 admin 的 identity ID 字符串集合。
func queryAdminIdentities(ctx context.Context, db data.DB, accountID uuid.UUID, identityIDs []uuid.UUID) map[string]struct{} {
	if len(identityIDs) == 0 {
		return nil
	}

	result := map[string]struct{}{}
	for _, identityID := range identityIDs {
		var userID *uuid.UUID
		err := db.QueryRow(ctx,
			`SELECT user_id FROM channel_identities WHERE id = $1`,
			identityID,
		).Scan(&userID)
		if err != nil || userID == nil {
			continue
		}

		var role string
		err = db.QueryRow(ctx,
			`SELECT role FROM account_memberships WHERE account_id = $1 AND user_id = $2`,
			accountID, *userID,
		).Scan(&role)
		if err != nil {
			continue
		}
		if role == "account_admin" || role == "platform_admin" {
			result[identityID.String()] = struct{}{}
		}
	}

	if len(result) > 0 {
		slog.InfoContext(ctx, "channel_admin_tag", "admin_count", len(result), "total_refs", len(identityIDs))
	}
	return result
}

// tagMessagesWithAdmin 在匹配的 YAML header 中注入 admin: true。
func tagMessagesWithAdmin(rc *RunContext, adminRefs map[string]struct{}) {
	for i, msg := range rc.Messages {
		if msg.Role != "user" {
			continue
		}
		changed := false
		parts := make([]llm.ContentPart, len(msg.Content))
		copy(parts, msg.Content)

		for j, part := range parts {
			text := llm.PartPromptText(part)
			if !strings.HasPrefix(text, "---\n") {
				continue
			}
			meta, _, ok := parseTelegramEnvelopeText(text)
			if !ok {
				continue
			}
			ref := strings.TrimSpace(meta["sender-ref"])
			if ref == "" {
				continue
			}
			if _, isAdmin := adminRefs[ref]; !isAdmin {
				continue
			}
			if strings.Contains(text, "\nadmin:") {
				continue
			}

			// 在 sender-ref 行后插入 admin: true
			injected := injectAdminField(text)
			if injected != text {
				parts[j] = llm.ContentPart{Type: part.Type, Text: injected}
				changed = true
			}
		}
		if changed {
			rc.Messages[i].Content = parts
		}
	}
}

func injectAdminField(text string) string {
	lines := strings.Split(text, "\n")
	var result []string
	inserted := false
	for _, line := range lines {
		result = append(result, line)
		if !inserted && strings.HasPrefix(strings.TrimSpace(line), "sender-ref:") {
			result = append(result, `admin: true`)
			inserted = true
		}
	}
	if !inserted {
		return text
	}
	return strings.Join(result, "\n")
}
