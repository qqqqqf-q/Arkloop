package channel_telegram

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/telegrambot"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

type fixedToken struct{ token string }

type testArtifactStore struct {
	data        []byte
	contentType string
}

func (f fixedToken) BotToken(ctx context.Context, channelID uuid.UUID) (string, error) {
	_ = ctx
	_ = channelID
	return f.token, nil
}

func (s testArtifactStore) PutObject(context.Context, string, []byte, objectstore.PutOptions) error {
	return nil
}

func (s testArtifactStore) Put(context.Context, string, []byte) error { return nil }

func (s testArtifactStore) Get(_ context.Context, _ string) ([]byte, error) {
	return append([]byte(nil), s.data...), nil
}

func (s testArtifactStore) GetWithContentType(_ context.Context, _ string) ([]byte, string, error) {
	return append([]byte(nil), s.data...), s.contentType, nil
}

func (s testArtifactStore) Head(_ context.Context, _ string) (objectstore.ObjectInfo, error) {
	return objectstore.ObjectInfo{ContentType: s.contentType, Size: int64(len(s.data))}, nil
}

func (s testArtifactStore) Delete(context.Context, string) error { return nil }

func (s testArtifactStore) ListPrefix(context.Context, string) ([]objectstore.ObjectInfo, error) {
	return nil, nil
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
	exec := NewExecutor(fixedToken{token: "TEST"}, telegrambot.NewClient(srv.URL, srv.Client()), nil)
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
	exec := NewExecutor(fixedToken{token: "TEST"}, telegrambot.NewClient(srv.URL, srv.Client()), nil)
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
	exec := NewExecutor(fixedToken{token: "TEST"}, telegrambot.NewClient(srv.URL, srv.Client()), nil)
	surface := &tools.ChannelToolSurface{
		ChannelID:        chID,
		ChannelType:      "telegram",
		PlatformChatID:   "1001",
		InboundMessageID: "55",
	}
	res := exec.Execute(context.Background(), ToolReact, map[string]any{
		"emoji":      "❤️",
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// react/sendFile 仍需要 server，reply 不应到达此处
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()

	exec := NewExecutor(fixedToken{token: "T2"}, telegrambot.NewClient(srv.URL, srv.Client()), nil)
	surface := &tools.ChannelToolSurface{
		ChannelID:       uuid.New(),
		ChannelType:     "telegram",
		PlatformChatID:  "1001",
		MessageThreadID: nil,
	}
	res := exec.Execute(context.Background(), ToolReply, map[string]any{
		"reply_to_message_id": "42",
	}, tools.ExecutionContext{Channel: surface}, "")
	if res.Error != nil {
		t.Fatalf("reply: %v", res.Error)
	}
	result := res.ResultJSON
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result["reply_to_set"] != true {
		t.Fatalf("expected reply_to_set=true, got %v", result["reply_to_set"])
	}
	if result["reply_to_message_id"] != "42" {
		t.Fatalf("expected reply_to_message_id=42, got %v", result["reply_to_message_id"])
	}
}

func TestExecutorReply_RejectsEmptyMessageID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()

	exec := NewExecutor(fixedToken{token: "T2"}, telegrambot.NewClient(srv.URL, srv.Client()), nil)
	surface := &tools.ChannelToolSurface{
		ChannelID:      uuid.New(),
		ChannelType:    "telegram",
		PlatformChatID: "1001",
	}
	res := exec.Execute(context.Background(), ToolReply, map[string]any{}, tools.ExecutionContext{Channel: surface}, "")
	if res.Error == nil {
		t.Fatal("expected error for missing reply_to_message_id")
	}
}

func TestExecutorReply_NumericMessageID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()

	exec := NewExecutor(fixedToken{token: "T2"}, telegrambot.NewClient(srv.URL, srv.Client()), nil)
	surface := &tools.ChannelToolSurface{
		ChannelID:      uuid.New(),
		ChannelType:    "telegram",
		PlatformChatID: "1001",
	}
	res := exec.Execute(context.Background(), ToolReply, map[string]any{
		"reply_to_message_id": float64(6687),
	}, tools.ExecutionContext{Channel: surface}, "")
	if res.Error != nil {
		t.Fatalf("reply: %v", res.Error)
	}
	if res.ResultJSON["reply_to_message_id"] != "6687" {
		t.Fatalf("expected 6687, got %v", res.ResultJSON["reply_to_message_id"])
	}
}

func TestExecutorSendFile_ArtifactRefUploadsMultipart(t *testing.T) {
	accountID := uuid.New()
	var (
		gotMethod   string
		gotFilename string
		gotBytes    []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = strings.TrimPrefix(r.URL.Path, "/botTEST/")
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("parse media type: %v", err)
		}
		if mediaType != "multipart/form-data" {
			t.Fatalf("unexpected content type: %s", mediaType)
		}
		reader := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("next part: %v", err)
			}
			if part.FormName() == "photo" {
				gotFilename = part.FileName()
				gotBytes, _ = io.ReadAll(part)
			}
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":42}}`))
	}))
	defer srv.Close()

	exec := NewExecutor(
		fixedToken{token: "TEST"},
		telegrambot.NewClient(srv.URL, srv.Client()),
		testArtifactStore{
			data:        []byte("png-bytes"),
			contentType: "image/png",
		},
	)
	surface := &tools.ChannelToolSurface{
		ChannelID:      uuid.New(),
		ChannelType:    "telegram",
		PlatformChatID: "1001",
	}
	res := exec.Execute(context.Background(), ToolSendFile, map[string]any{
		"file_url": "artifact:" + accountID.String() + "/run/generated-image.png",
		"kind":     "photo",
	}, tools.ExecutionContext{
		AccountID: &accountID,
		Channel:   surface,
	}, "")
	if res.Error != nil {
		t.Fatalf("send file: %v", res.Error)
	}
	if gotMethod != "sendPhoto" {
		t.Fatalf("method: %s", gotMethod)
	}
	if gotFilename != "generated-image.png" {
		t.Fatalf("filename: %s", gotFilename)
	}
	if string(gotBytes) != "png-bytes" {
		t.Fatalf("unexpected bytes: %q", gotBytes)
	}
}

func TestExecutorSendFile_RejectsCrossAccountArtifact(t *testing.T) {
	accountID := uuid.New()
	otherAccountID := uuid.New()
	exec := NewExecutor(
		fixedToken{token: "TEST"},
		telegrambot.NewClient("https://example.invalid", nil),
		testArtifactStore{
			data:        []byte("png-bytes"),
			contentType: "image/png",
		},
	)
	surface := &tools.ChannelToolSurface{
		ChannelID:      uuid.New(),
		ChannelType:    "telegram",
		PlatformChatID: "1001",
	}
	res := exec.Execute(context.Background(), ToolSendFile, map[string]any{
		"file_url": "artifact:" + otherAccountID.String() + "/run/generated-image.png",
		"kind":     "photo",
	}, tools.ExecutionContext{
		AccountID: &accountID,
		Channel:   surface,
	}, "")
	if res.Error == nil {
		t.Fatal("expected error")
	}
	if res.Error.Message != "artifact is outside the current account" {
		t.Fatalf("unexpected error: %#v", res.Error)
	}
}

func TestExecutorRejectsNonTelegramSurface(t *testing.T) {
	exec := NewExecutor(fixedToken{token: "T"}, telegrambot.NewClient("", nil), nil)
	res := exec.Execute(context.Background(), ToolReact, map[string]any{"emoji": "x"}, tools.ExecutionContext{
		Channel: &tools.ChannelToolSurface{ChannelType: "slack"},
	}, "")
	if res.Error == nil {
		t.Fatal("expected error")
	}
}
