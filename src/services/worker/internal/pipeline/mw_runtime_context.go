package pipeline

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"
)

func NewRuntimeContextMiddleware() RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc.ChannelContext != nil {
			rc.UpsertPromptSegment(PromptSegment{
				Name:          "runtime.channel_output_behavior",
				Target:        PromptTargetSystemPrefix,
				Role:          "system",
				Text:          buildChannelOutputBehaviorBlock(),
				Stability:     PromptStabilityStablePrefix,
				CacheEligible: true,
			})
			isAdmin := checkSenderIsAdmin(ctx, rc)
			rc.SenderIsAdmin = isAdmin
		}
		rc.UpsertPromptSegment(PromptSegment{
			Name:          "runtime.context",
			Target:        PromptTargetSystemPrefix,
			Role:          "system",
			Text:          buildRuntimeContextBlock(ctx, rc),
			Stability:     PromptStabilitySessionPrefix,
			CacheEligible: true,
		})
		return next(ctx, rc)
	}
}

func buildChannelOutputBehaviorBlock() string {
	return `<channel_output_behavior>
Your text outputs are delivered to the chat platform in real-time as separate messages.
When you call tools mid-reply, text before and after the tool call becomes distinct messages visible to the user.
Avoid repeating content that was already sent. If you have nothing new to add after a tool call, use end_reply.
</channel_output_behavior>`
}

func buildRuntimeContextBlock(ctx context.Context, rc *RunContext) string {
	if rc == nil {
		return ""
	}

	timeZone := runtimeContextTimeZone(ctx, rc)
	loc := loadRuntimeLocation(timeZone)
	localDate := time.Now().UTC().In(loc).Format("2006-01-02")

	var sb strings.Builder
	sb.WriteString("User Timezone: " + timeZone + "\n")
	sb.WriteString("User Local Date: " + localDate + "\n")
	sb.WriteString("Host Mode: " + hostMode + "\n")
	sb.WriteString("Platform: " + runtime.GOOS + "/" + runtime.GOARCH)
	if hostMode == "desktop" {
		sb.WriteString("\nExecution Environment: local machine (commands run directly on the user's device, not in a cloud sandbox)")
	}
	return sb.String()
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
