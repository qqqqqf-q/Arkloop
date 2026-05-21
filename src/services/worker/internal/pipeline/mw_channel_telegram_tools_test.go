//go:build !desktop

package pipeline

import (
	"context"
	"strings"
	"testing"

	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

type stubTelegramToken struct{}

func (stubTelegramToken) BotToken(_ context.Context, _ uuid.UUID) (string, error) {
	return "test-token", nil
}

func TestChannelTelegramToolsMiddlewareInjectsTools(t *testing.T) {
	cid := uuid.New()
	ident := uuid.New()
	rc := &RunContext{
		JobPayload: map[string]any{
			"channel_delivery": map[string]any{
				"channel_id":                 cid.String(),
				"channel_type":               "telegram",
				"conversation_ref":           map[string]any{"target": "1"},
				"inbound_message_ref":        map[string]any{"message_id": "10"},
				"sender_channel_identity_id": ident.String(),
			},
		},
		ToolRegistry:  tools.NewRegistry(),
		ToolExecutors: map[string]tools.Executor{},
		AllowlistSet:  map[string]struct{}{},
		ToolSpecs:     nil,
		ToolDenylist:  nil,
	}

	h := Build([]RunMiddleware{
		NewChannelContextMiddleware(nil),
		NewChannelTelegramToolsMiddleware(stubTelegramToken{}, nil),
	}, func(_ context.Context, rc *RunContext) error {
		if _, ok := rc.AllowlistSet["telegram_react"]; !ok {
			t.Fatal("expected telegram_react in allowlist")
		}
		if _, ok := rc.AllowlistSet["telegram_reply"]; !ok {
			t.Fatal("expected telegram_reply in allowlist")
		}
		if rc.ToolExecutors["telegram_react"] == nil || rc.ToolExecutors["telegram_reply"] == nil {
			t.Fatal("expected executors bound")
		}
		if rc.ChannelToolSurface == nil || rc.ChannelToolSurface.PlatformChatID != "1" {
			t.Fatalf("channel surface: %#v", rc.ChannelToolSurface)
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("handler: %v", err)
	}
}

func TestChannelTelegramToolsMiddlewareRespectsDenylist(t *testing.T) {
	cid := uuid.New()
	ident := uuid.New()
	rc := &RunContext{
		JobPayload: map[string]any{
			"channel_delivery": map[string]any{
				"channel_id":                 cid.String(),
				"channel_type":               "telegram",
				"conversation_ref":           map[string]any{"target": "1"},
				"inbound_message_ref":        map[string]any{"message_id": "10"},
				"sender_channel_identity_id": ident.String(),
			},
		},
		ToolRegistry:  tools.NewRegistry(),
		ToolExecutors: map[string]tools.Executor{},
		AllowlistSet:  map[string]struct{}{},
		ToolDenylist:  []string{"telegram_react"},
	}

	h := Build([]RunMiddleware{
		NewChannelContextMiddleware(nil),
		NewChannelTelegramToolsMiddleware(stubTelegramToken{}, nil),
	}, func(_ context.Context, rc *RunContext) error {
		if _, ok := rc.AllowlistSet["telegram_react"]; ok {
			t.Fatal("telegram_react should be denied")
		}
		if _, ok := rc.AllowlistSet["telegram_reply"]; !ok {
			t.Fatal("expected telegram_reply")
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("handler: %v", err)
	}
}

func TestChannelTelegramToolsMiddlewareGroupAlwaysRemovesConversationSearch(t *testing.T) {
	rc := &RunContext{
		ChannelContext: &ChannelContext{
			ChannelType:      "telegram",
			ConversationType: "group",
		},
		ToolRegistry:  tools.NewRegistry(),
		ToolExecutors: map[string]tools.Executor{},
		AllowlistSet: map[string]struct{}{
			"conversation_search":  {},
			"conversation_context": {},
		},
	}

	h := Build([]RunMiddleware{
		NewChannelTelegramToolsMiddleware(nil, nil),
	}, func(_ context.Context, rc *RunContext) error {
		if _, ok := rc.AllowlistSet["conversation_search"]; ok {
			t.Fatal("conversation_search should be removed in group chat")
		}
		if _, ok := rc.AllowlistSet["conversation_context"]; !ok {
			t.Fatal("conversation_context should remain available for current thread")
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("handler: %v", err)
	}
}

func TestDiscussGroupInjectsSpeakAndSkipsTelegramReply(t *testing.T) {
	cid := uuid.New()
	ident := uuid.New()
	rc := &RunContext{
		JobPayload: map[string]any{
			"channel_delivery": map[string]any{
				"channel_id":                 cid.String(),
				"channel_type":               "telegram",
				"conversation_type":          "supergroup",
				"conversation_ref":           map[string]any{"target": "-100"},
				"inbound_message_ref":        map[string]any{"message_id": "10"},
				"sender_channel_identity_id": ident.String(),
			},
		},
		ToolRegistry:      tools.NewRegistry(),
		ToolExecutors:     map[string]tools.Executor{},
		AllowlistSet:      map[string]struct{}{},
		PersonaDefinition: &personas.Definition{CoreTools: []string{"arkloop_help"}},
	}

	h := Build([]RunMiddleware{
		NewChannelContextMiddleware(nil),
		NewDiscussModeMiddleware(),
		NewChannelTelegramToolsMiddleware(stubTelegramToken{}, nil),
	}, func(_ context.Context, rc *RunContext) error {
		if !rc.DiscussRun {
			t.Fatal("expected discuss run")
		}
		if _, ok := rc.AllowlistSet["speak"]; !ok {
			t.Fatal("expected speak in group discuss allowlist")
		}
		if !containsToolName(rc.PersonaDefinition.CoreTools, "speak") {
			t.Fatalf("expected speak in core tools, got %#v", rc.PersonaDefinition.CoreTools)
		}
		if _, ok := rc.AllowlistSet["telegram_reply"]; ok {
			t.Fatal("telegram_reply should not be injected in group discuss")
		}
		if _, ok := rc.AllowlistSet["telegram_react"]; !ok {
			t.Fatal("expected telegram_react to stay injected")
		}
		var discussPrompt string
		for _, segment := range rc.PromptSegments() {
			if segment.Name == "runtime.discuss_mode" {
				discussPrompt = segment.Text
				break
			}
		}
		for _, want := range []string{
			"assistant text is internal monologue",
			"The speak tool is the only way",
			"assistant text without calling speak",
		} {
			if !strings.Contains(discussPrompt, want) {
				t.Fatalf("expected discuss prompt to contain %q, got %q", want, discussPrompt)
			}
		}
		var turnPrompt string
		for _, segment := range rc.PromptSegments() {
			if segment.Name == "runtime.discuss_turn" {
				turnPrompt = segment.Text
				if segment.Target != PromptTargetRuntimeTail || segment.Role != "user" {
					t.Fatalf("unexpected discuss turn segment: %#v", segment)
				}
				break
			}
		}
		for _, want := range []string{
			"If you decide to speak in this turn, call the speak tool",
			"Assistant text without speak is private internal monologue",
		} {
			if !strings.Contains(turnPrompt, want) {
				t.Fatalf("expected discuss turn prompt to contain %q, got %q", want, turnPrompt)
			}
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("handler: %v", err)
	}
}

func TestDiscussGroupMentionAddsSpeakGuidance(t *testing.T) {
	cid := uuid.New()
	ident := uuid.New()
	rc := &RunContext{
		JobPayload: map[string]any{
			"channel_delivery": map[string]any{
				"channel_id":                 cid.String(),
				"channel_type":               "telegram",
				"conversation_type":          "supergroup",
				"conversation_ref":           map[string]any{"target": "-100"},
				"inbound_message_ref":        map[string]any{"message_id": "10"},
				"sender_channel_identity_id": ident.String(),
				"mentions_bot":               true,
			},
		},
		ToolRegistry:  tools.NewRegistry(),
		ToolExecutors: map[string]tools.Executor{},
		AllowlistSet:  map[string]struct{}{},
	}

	h := Build([]RunMiddleware{
		NewChannelContextMiddleware(nil),
		NewDiscussModeMiddleware(),
	}, func(_ context.Context, rc *RunContext) error {
		var discussPrompt string
		for _, segment := range rc.PromptSegments() {
			if segment.Name == "runtime.discuss_mode" {
				discussPrompt = segment.Text
				break
			}
		}
		if !strings.Contains(discussPrompt, "mentioned or replied to") {
			t.Fatalf("expected mention guidance, got %q", discussPrompt)
		}
		if !strings.Contains(discussPrompt, "calling speak now") {
			t.Fatalf("expected explicit speak guidance, got %q", discussPrompt)
		}
		var turnPrompt string
		for _, segment := range rc.PromptSegments() {
			if segment.Name == "runtime.discuss_turn" {
				turnPrompt = segment.Text
				break
			}
		}
		if !strings.Contains(turnPrompt, "directly addressed to you") {
			t.Fatalf("expected mention turn guidance, got %q", turnPrompt)
		}
		if !strings.Contains(turnPrompt, "call speak now") {
			t.Fatalf("expected explicit turn speak guidance, got %q", turnPrompt)
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("handler: %v", err)
	}
}

func TestDiscussGroupWithoutTriggerAddsTurnSpeakReminder(t *testing.T) {
	cid := uuid.New()
	ident := uuid.New()
	rc := &RunContext{
		JobPayload: map[string]any{
			"channel_delivery": map[string]any{
				"channel_id":                 cid.String(),
				"channel_type":               "telegram",
				"conversation_type":          "supergroup",
				"conversation_ref":           map[string]any{"target": "-100"},
				"sender_channel_identity_id": ident.String(),
			},
		},
		ToolRegistry:  tools.NewRegistry(),
		ToolExecutors: map[string]tools.Executor{},
		AllowlistSet:  map[string]struct{}{},
	}

	h := Build([]RunMiddleware{
		NewChannelContextMiddleware(nil),
		NewDiscussModeMiddleware(),
	}, func(_ context.Context, rc *RunContext) error {
		var turnPrompt string
		for _, segment := range rc.PromptSegments() {
			if segment.Name == "runtime.discuss_turn" {
				turnPrompt = segment.Text
				break
			}
		}
		if !strings.Contains(turnPrompt, "No direct mention, reply, or keyword trigger metadata") {
			t.Fatalf("expected no-trigger guidance, got %q", turnPrompt)
		}
		if !strings.Contains(turnPrompt, "call the speak tool") {
			t.Fatalf("expected speak reminder, got %q", turnPrompt)
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("handler: %v", err)
	}
}
