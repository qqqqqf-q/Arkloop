package channel_telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"arkloop/services/shared/telegrambot"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

type fixedToken struct{ token string }

func (f fixedToken) BotToken(ctx context.Context, channelID uuid.UUID) (string, error) {
	_ = ctx
	_ = channelID
	return f.token, nil
}

func TestCoerceTelegramMessageID(t *testing.T) {
	t.Parallel()
	if _, ok := coerceTelegramMessageID(nil); ok {
		t.Fatal("expected false")
	}
	if s, ok := coerceTelegramMessageID(float64(42)); !ok || s != "42" {
		t.Fatalf("float64: ok=%v s=%q", ok, s)
	}
	if s, ok := coerceTelegramMessageID(" 99 "); !ok || s != "99" {
		t.Fatalf("string: ok=%v s=%q", ok, s)
	}
	if _, ok := coerceTelegramMessageID(float64(0)); ok {
		t.Fatal("expected reject 0")
	}
}

func TestExecutorReact(t *testing.T) {
	var sawMethod, sawToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawMethod = strings.TrimPrefix(r.URL.Path, "/bot")
		idx := strings.Index(sawMethod, "/")
		if idx > 0 {
			sawToken = sawMethod[:idx]
			sawMethod = sawMethod[idx+1:]
		}
		body, _ := io.ReadAll(r.Body)
		var envelope map[string]any
		_ = json.Unmarshal(body, &envelope)
		if sawMethod != "setMessageReaction" {
			t.Fatalf("method: %s", sawMethod)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()

	chID := uuid.New()
	exec := NewExecutor(fixedToken{token: "TEST"}, telegrambot.NewClient(srv.URL, srv.Client()))
	surface := &tools.ChannelToolSurface{
		ChannelID:        chID,
		ChannelType:      "telegram",
		PlatformChatID:   "1001",
		InboundMessageID: "55",
	}
	res := exec.Execute(context.Background(), ToolReact, map[string]any{"emoji": "👍"}, tools.ExecutionContext{
		Channel: surface,
	}, "")
	if res.Error != nil {
		t.Fatalf("react: %v", res.Error)
	}
	if sawToken != "TEST" {
		t.Fatalf("token: %q", sawToken)
	}
}

func TestExecutorReact_UsesReactionKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()

	chID := uuid.New()
	exec := NewExecutor(fixedToken{token: "TEST"}, telegrambot.NewClient(srv.URL, srv.Client()))
	surface := &tools.ChannelToolSurface{
		ChannelID:        chID,
		ChannelType:      "telegram",
		PlatformChatID:   "1001",
		InboundMessageID: "55",
	}
	res := exec.Execute(context.Background(), ToolReact, map[string]any{"reaction": "🔥"}, tools.ExecutionContext{
		Channel: surface,
	}, "")
	if res.Error != nil {
		t.Fatalf("react: %v", res.Error)
	}
}

func TestExecutorReact_UsesNumericMessageIDArg(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()

	chID := uuid.New()
	exec := NewExecutor(fixedToken{token: "TEST"}, telegrambot.NewClient(srv.URL, srv.Client()))
	surface := &tools.ChannelToolSurface{
		ChannelID:        chID,
		ChannelType:      "telegram",
		PlatformChatID:   "1001",
		InboundMessageID: "55",
	}
	res := exec.Execute(context.Background(), ToolReact, map[string]any{
		"emoji":       "❤️",
		"message_id": float64(2002),
	}, tools.ExecutionContext{Channel: surface}, "")
	if res.Error != nil {
		t.Fatalf("react: %v", res.Error)
	}
	if !strings.Contains(string(gotBody), `"message_id":2002`) {
		t.Fatalf("body missing numeric id: %s", gotBody)
	}
}

func TestExecutorReply(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if i := strings.Index(path, "/sendMessage"); i >= 0 {
			methods = append(methods, "sendMessage")
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99,"chat":{"id":1001}}}`))
	}))
	defer srv.Close()

	exec := NewExecutor(fixedToken{token: "T2"}, telegrambot.NewClient(srv.URL, srv.Client()))
	surface := &tools.ChannelToolSurface{
		ChannelID:       uuid.New(),
		ChannelType:     "telegram",
		PlatformChatID:  "1001",
		MessageThreadID: nil,
	}
	res := exec.Execute(context.Background(), ToolReply, map[string]any{
		"text":                  "hello",
		"reply_to_message_id":   "42",
	}, tools.ExecutionContext{Channel: surface}, "")
	if res.Error != nil {
		t.Fatalf("reply: %v", res.Error)
	}
	if len(methods) != 1 || methods[0] != "sendMessage" {
		t.Fatalf("methods: %v", methods)
	}
}

func TestExecutorRejectsNonTelegramSurface(t *testing.T) {
	exec := NewExecutor(fixedToken{token: "T"}, telegrambot.NewClient("", nil))
	res := exec.Execute(context.Background(), ToolReact, map[string]any{"emoji": "x"}, tools.ExecutionContext{
		Channel: &tools.ChannelToolSurface{ChannelType: "slack"},
	}, "")
	if res.Error == nil {
		t.Fatal("expected error")
	}
}
