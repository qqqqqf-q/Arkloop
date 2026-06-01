package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"testing"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/llm"
)

func TestNewChannelGroupContextTrimMiddleware_projectsButSkipsTrimForPrivate(t *testing.T) {
	mw := NewChannelGroupContextTrimMiddleware()
	rc := &RunContext{
		ChannelContext: &ChannelContext{ConversationType: "private"},
		Messages: []llm.Message{{
			Role: "user",
			Content: []llm.ContentPart{{
				Type: "text",
				Text: "---\ndisplay-name: \"Alice\"\nchannel: \"telegram\"\nconversation-type: \"private\"\ntime: \"2026-04-03T10:00:00Z\"\n---\nhello",
			}},
		}},
	}
	called := false
	err := mw(context.Background(), rc, func(context.Context, *RunContext) error {
		called = true
		if len(rc.Messages) != 1 {
			t.Fatalf("messages should not be trimmed for DM")
		}
		text := llm.PartPromptText(rc.Messages[0].Content[0])
		if text == "" || text == "---\ndisplay-name: \"Alice\"\nchannel: \"telegram\"\nconversation-type: \"private\"\ntime: \"2026-04-03T10:00:00Z\"\n---\nhello" {
			t.Fatalf("expected projected envelope text, got %q", text)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("next not invoked")
	}
}

func TestNewChannelGroupContextTrimMiddleware_skipsProjectionWithoutChannelContext(t *testing.T) {
	mw := NewChannelGroupContextTrimMiddleware()
	original := "---\ndisplay-name: \"Alice\"\nchannel: \"telegram\"\nconversation-type: \"private\"\ntime: \"2026-04-03T10:00:00Z\"\n---\nhello"
	rc := &RunContext{
		Messages: []llm.Message{{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: original}}}},
	}

	_ = mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil })

	if got := llm.PartPromptText(rc.Messages[0].Content[0]); got != original {
		t.Fatalf("expected envelope to stay untouched without channel context, got %q", got)
	}
}

func TestNewChannelGroupContextTrimMiddleware_preservesSupergroupHistory(t *testing.T) {
	mw := NewChannelGroupContextTrimMiddleware()
	long := "wwwwwwwwwwwwwwwwwwww"
	msgs := []llm.Message{
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: long}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: long}}},
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "tail"}}},
	}
	rc := &RunContext{
		ChannelContext: &ChannelContext{ConversationType: "supergroup"},
		Messages:       msgs,
	}
	_ = mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil })
	if len(rc.Messages) != len(msgs) {
		t.Fatalf("expected group middleware to preserve history, got %d", len(rc.Messages))
	}
	if rc.Messages[len(rc.Messages)-1].Content[0].Text != "tail" {
		t.Fatalf("unexpected tail content")
	}
}

func TestNewChannelGroupContextTrimMiddleware_preservesReplacementPrefix(t *testing.T) {
	mw := NewChannelGroupContextTrimMiddleware()
	rc := &RunContext{
		ChannelContext: &ChannelContext{ConversationType: "supergroup"},
		Messages: []llm.Message{
			makeThreadContextReplacementMessage("summary"),
			{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "body"}}},
			{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "tail"}}},
		},
	}
	_ = mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil })
	if len(rc.Messages) != 3 {
		t.Fatalf("expected replacement and raw history preserved, got %d", len(rc.Messages))
	}
	if got := rc.Messages[0].Role; got != "system" {
		t.Fatalf("expected replacement to stay as system block, got %q", got)
	}
}

func TestNewChannelGroupContextTrimMiddleware_materializesOnlyKeptLazyImages(t *testing.T) {
	t.Setenv("ARKLOOP_CHANNEL_GROUP_KEEP_IMAGE_TAIL", "1")
	pngData := groupTrimPNG(t)
	store := &groupTrimAttachmentStore{
		data: map[string][]byte{
			"attachments/old.png":    pngData,
			"attachments/mid.png":    pngData,
			"attachments/latest.png": pngData,
		},
		mimeType: "image/png",
	}
	mw := NewChannelGroupContextTrimMiddleware(GroupContextTrimDeps{AttachmentStore: store})
	rc := &RunContext{
		ChannelContext: &ChannelContext{ConversationType: "supergroup"},
		Messages: []llm.Message{
			{Role: "user", Content: []llm.ContentPart{lazyImagePart("attachments/old.png")}},
			{Role: "user", Content: []llm.ContentPart{lazyImagePart("attachments/mid.png")}},
			{Role: "user", Content: []llm.ContentPart{lazyImagePart("attachments/latest.png")}},
		},
	}

	if err := mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(store.keys) != 1 || store.keys[0] != "attachments/latest.png" {
		t.Fatalf("expected only latest image materialized, got %#v", store.keys)
	}
	if got := rc.Messages[0].Content[0].Kind(); got != messagecontent.PartTypeText {
		t.Fatalf("expected old image to be stripped, got %q", got)
	}
	latest := rc.Messages[2].Content[0]
	if latest.Kind() != messagecontent.PartTypeImage || len(latest.Data) == 0 {
		t.Fatalf("expected latest image data materialized, got %#v", latest)
	}
}

func TestNewChannelGroupContextTrimMiddleware_dropsUndecodableImage(t *testing.T) {
	store := &groupTrimAttachmentStore{
		data: map[string][]byte{
			"attachments/good.png": groupTrimPNG(t),
			"attachments/bad.heic": []byte("not a decodable image"),
		},
		mimeType: "image/png",
	}
	mw := NewChannelGroupContextTrimMiddleware(GroupContextTrimDeps{AttachmentStore: store})
	rc := &RunContext{
		ChannelContext: &ChannelContext{ConversationType: "supergroup"},
		Messages: []llm.Message{
			{Role: "user", Content: []llm.ContentPart{
				{Type: messagecontent.PartTypeText, Text: "看这张图"},
				lazyImagePart("attachments/bad.heic"),
			}},
			{Role: "user", Content: []llm.ContentPart{lazyImagePart("attachments/good.png")}},
		},
	}

	nextCalled := false
	err := mw(context.Background(), rc, func(context.Context, *RunContext) error {
		nextCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("undecodable image should be skipped, not abort the run: %v", err)
	}
	if !nextCalled {
		t.Fatal("next not invoked — run aborted instead of proceeding")
	}
	first := rc.Messages[0].Content
	if len(first) != 1 || first[0].Kind() != messagecontent.PartTypeText {
		t.Fatalf("expected bad image dropped and text kept, got %#v", first)
	}
	good := rc.Messages[1].Content[0]
	if good.Kind() != messagecontent.PartTypeImage || len(good.Data) == 0 {
		t.Fatalf("expected good image materialized, got %#v", good)
	}
}

func lazyImagePart(key string) llm.ContentPart {
	return llm.ContentPart{
		Type: messagecontent.PartTypeImage,
		Attachment: &messagecontent.AttachmentRef{
			Key:      key,
			MimeType: "image/png",
		},
	}
}

func groupTrimPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

type groupTrimAttachmentStore struct {
	data     map[string][]byte
	mimeType string
	keys     []string
}

func (s *groupTrimAttachmentStore) Get(ctx context.Context, key string) ([]byte, error) {
	data, _, err := s.GetWithContentType(ctx, key)
	return data, err
}

func (s *groupTrimAttachmentStore) GetWithContentType(_ context.Context, key string) ([]byte, string, error) {
	s.keys = append(s.keys, key)
	data, ok := s.data[key]
	if !ok {
		return nil, "", fmt.Errorf("attachment not found: %s", key)
	}
	return append([]byte(nil), data...), s.mimeType, nil
}
