package accountapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/shared/messagecontent"
	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/telegrambot"

	"github.com/google/uuid"
)

type fakeAttachmentPutStore struct {
	keys  []string
	blobs map[string][]byte
}

func (f *fakeAttachmentPutStore) PutObject(ctx context.Context, key string, data []byte, options objectstore.PutOptions) error {
	_ = ctx
	_ = options
	f.keys = append(f.keys, key)
	if f.blobs == nil {
		f.blobs = make(map[string][]byte)
	}
	f.blobs[key] = data
	return nil
}

func TestBuildTelegramStructuredMessageWithMedia_Photo(t *testing.T) {
	t.Parallel()
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getFile") && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"photos/a.png"}}`))
		case strings.HasPrefix(r.URL.Path, "/file/bot"):
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client := telegrambot.NewClient(srv.URL, srv.Client())
	store := &fakeAttachmentPutStore{}
	identity := data.ChannelIdentity{ID: uuid.New()}
	incoming := telegramIncomingMessage{
		ChannelID:        uuid.New(),
		PlatformChatID:   "1",
		PlatformMsgID:    "9",
		PlatformUserID:   "42",
		ChatType:         "private",
		Text:             "hello",
		MediaAttachments: []telegramInboundAttachment{{Type: "image", FileID: "fid"}},
	}

	accID := uuid.New()
	threadID := uuid.New()
	userID := uuid.New()
	proj, raw, _, err := buildTelegramStructuredMessageWithMedia(
		context.Background(),
		client,
		store,
		"TOK",
		accID,
		threadID,
		&userID,
		identity,
		incoming,
		buildInboundTimeContext(time.Unix(1710000300, 0).UTC(), "Asia/Shanghai"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(proj, "hello") {
		t.Fatalf("projection: %q", proj)
	}
	if len(store.keys) != 1 || !strings.HasPrefix(store.keys[0], "attachments/"+accID.String()+"/") {
		t.Fatalf("keys: %v", store.keys)
	}
	var c messagecontent.Content
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	var foundImage bool
	for _, p := range c.Parts {
		if p.Type == messagecontent.PartTypeImage && p.Attachment != nil && p.Attachment.Key == store.keys[0] {
			foundImage = true
			break
		}
	}
	if !foundImage {
		t.Fatalf("content: %+v", c)
	}
}

func TestBuildTelegramStructuredMessageWithMedia_NilStoreFallback(t *testing.T) {
	t.Parallel()
	identity := data.ChannelIdentity{ID: uuid.New()}
	incoming := telegramIncomingMessage{
		ChannelID:        uuid.New(),
		PlatformChatID:   "1",
		PlatformMsgID:    "9",
		PlatformUserID:   "42",
		ChatType:         "private",
		Text:             "hi",
		MediaAttachments: []telegramInboundAttachment{{Type: "image", FileID: "fid"}},
	}
	var nilUser *uuid.UUID
	proj, _, _, err := buildTelegramStructuredMessageWithMedia(
		context.Background(),
		nil,
		nil,
		"TOK",
		uuid.New(),
		uuid.New(),
		nilUser,
		identity,
		incoming,
		buildInboundTimeContext(time.Unix(1710000300, 0).UTC(), "Asia/Shanghai"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(proj, "[图片:") {
		t.Fatalf("expected placeholder, got %q", proj)
	}
}
