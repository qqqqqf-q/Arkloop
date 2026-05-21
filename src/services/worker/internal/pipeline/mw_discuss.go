package pipeline

import (
	"context"
	"strings"

	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/speak"
)

func IsDiscussRunContext(rc *RunContext) bool {
	return rc != nil && rc.DiscussRun
}

func IsSpeakToolName(toolName string) bool {
	return strings.EqualFold(strings.TrimSpace(toolName), speak.ToolName)
}

func NewDiscussModeMiddleware() RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc == nil || rc.ChannelContext == nil || !isGroupChannelConversation(rc.ChannelContext.ConversationType) {
			return next(ctx, rc)
		}

		rc.DiscussRun = true
		if rc.ToolExecutors == nil {
			rc.ToolExecutors = map[string]tools.Executor{}
		}
		if rc.AllowlistSet == nil {
			rc.AllowlistSet = map[string]struct{}{}
		}

		deny := make(map[string]struct{})
		for _, n := range rc.ToolDenylist {
			if c := strings.TrimSpace(n); c != "" {
				deny[c] = struct{}{}
			}
		}
		if _, blocked := deny[speak.ToolName]; !blocked {
			if rc.PersonaDefinition != nil && len(rc.PersonaDefinition.CoreTools) > 0 && !containsToolName(rc.PersonaDefinition.CoreTools, speak.ToolName) {
				rc.PersonaDefinition.CoreTools = append(rc.PersonaDefinition.CoreTools, speak.ToolName)
			}
			rc.ToolExecutors[speak.ToolName] = speak.New()
			rc.AllowlistSet[speak.ToolName] = struct{}{}
			if !containsToolSpecName(rc.ToolSpecs, speak.ToolName) {
				rc.ToolSpecs = append(rc.ToolSpecs, speak.Spec)
			}
			rc.ToolRegistry = ForkRegistry(rc.ToolRegistry, []tools.AgentToolSpec{speak.AgentSpec})
		}

		rc.UpsertPromptSegment(PromptSegment{
			Name:          "runtime.discuss_mode",
			Target:        PromptTargetSystemPrefix,
			Role:          "system",
			Text:          buildDiscussModeBlock(rc.ChannelContext),
			Stability:     PromptStabilityStablePrefix,
			CacheEligible: true,
		})
		rc.UpsertPromptSegment(PromptSegment{
			Name:          "runtime.discuss_turn",
			Target:        PromptTargetRuntimeTail,
			Role:          "user",
			Text:          buildDiscussTurnBlock(rc.ChannelContext),
			Stability:     PromptStabilityVolatileTail,
			CacheEligible: false,
		})

		return next(ctx, rc)
	}
}

func isGroupChannelConversation(conversationType string) bool {
	return IsTelegramGroupLikeConversation(conversationType)
}

func buildDiscussModeBlock(cc *ChannelContext) string {
	var sb strings.Builder
	sb.WriteString(`<discuss_mode>
You are in group discuss mode. Your direct assistant text is internal monologue: no one in the chat can see it.
The speak tool is the only way to deliver your assistant text to the group.
If you want to say anything to the group, you must call speak first, then write the actual message as normal assistant text.
If you write assistant text without calling speak, that text is absolute silence outside the system.

How to use speak:
- Call speak with no arguments to send your following assistant text to the current group conversation.
- Call speak with reply_to_message_id to send your following assistant text as a platform reply to that message.
- The speak tool does not contain message text. Put the message text in assistant output after the tool call.
- To stay silent, do not call speak. If you have nothing useful to add, use end_reply or stop.
`)
	if cc != nil && (cc.MentionsBot || cc.IsReplyToBot) {
		sb.WriteString("\nYou were mentioned or replied to. You should respond by calling speak now before writing visible text.\n")
	} else if cc != nil && cc.MatchesKeyword {
		sb.WriteString("\nThis run was triggered by a configured group keyword. If you respond, call speak before writing visible text.\n")
	}
	sb.WriteString("</discuss_mode>")
	return sb.String()
}

func buildDiscussTurnBlock(cc *ChannelContext) string {
	var sb strings.Builder
	sb.WriteString(`<discuss_turn>
IMPORTANT: You are in group discuss mode. If you decide to speak in this turn, call the speak tool before writing visible assistant text.
Assistant text without speak is private internal monologue and will not be delivered to the group.
If you do not need to speak, stay silent or use end_reply.
`)
	if cc != nil {
		switch {
		case cc.MentionsBot || cc.IsReplyToBot:
			sb.WriteString("This turn was directly addressed to you. If you respond, call speak now.\n")
		case cc.MatchesKeyword:
			sb.WriteString("This turn was created because a configured group keyword matched. If you respond, call speak now.\n")
		default:
			sb.WriteString("No direct mention, reply, or keyword trigger metadata is attached to this run. Speak only if the recent group context genuinely needs your response.\n")
		}
	}
	sb.WriteString("</discuss_turn>")
	return sb.String()
}
