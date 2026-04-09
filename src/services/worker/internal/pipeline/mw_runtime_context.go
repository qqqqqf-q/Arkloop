package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
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
	if rc == nil {
		return ""
	}

	lines := make([]string, 0, 3)
	if rc.ChannelContext != nil {
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

		if identity := formatBotIdentity(rc.ChannelContext); identity != "" {
			line += fmt.Sprintf(" | Identity: %s", identity)
		}
		lines = append(lines, line)
	}

	timeZone := runtimeContextTimeZone(ctx, rc)
	localNow := formatRuntimeLocalNow(time.Now().UTC(), timeZone)
	lines = append(lines,
		"User Timezone: "+timeZone,
		"User Local Now: "+localNow,
	)

	return "## Runtime Context\n" + strings.Join(lines, "\n")
}

func formatBotIdentity(cc *ChannelContext) string {
	name := cc.BotDisplayName
	uname := cc.BotUsername
	if name == "" && uname == "" {
		return ""
	}
	if name != "" && uname != "" {
		return fmt.Sprintf("%s (@%s)", name, uname)
	}
	if uname != "" {
		return "@" + uname
	}
	return name
}

func formatRuntimeLocalNow(now time.Time, timeZone string) string {
	loc := loadRuntimeLocation(timeZone)
	local := now.In(loc)
	return local.Format("2006-01-02 15:04:05") + " [" + formatRuntimeUTCOffset(local) + "]"
}

func loadRuntimeLocation(timeZone string) *time.Location {
	cleaned := strings.TrimSpace(timeZone)
	if cleaned == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(cleaned)
	if err != nil {
		return time.UTC
	}
	return loc
}

func formatRuntimeUTCOffset(t time.Time) string {
	_, offsetSeconds := t.Zone()
	sign := "+"
	if offsetSeconds < 0 {
		sign = "-"
		offsetSeconds = -offsetSeconds
	}
	hours := offsetSeconds / 3600
	minutes := (offsetSeconds % 3600) / 60
	if minutes == 0 {
		return fmt.Sprintf("UTC%s%d", sign, hours)
	}
	return fmt.Sprintf("UTC%s%d:%02d", sign, hours, minutes)
}
