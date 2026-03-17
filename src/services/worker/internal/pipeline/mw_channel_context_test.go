//go:build !desktop

package pipeline

import (
	"context"
	"testing"

	"arkloop/services/worker/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestParseChannelContextRejectsInvalidChannelID(t *testing.T) {
	_, err := parseChannelContext(map[string]any{
		"channel_id":                 "bad-id",
		"channel_type":               "telegram",
		"platform_chat_id":           "10001",
		"sender_channel_identity_id": uuid.NewString(),
	})
	if err == nil {
		t.Fatal("expected parse error for invalid channel_id")
	}
}

func TestChannelContextMiddlewareOverridesUserIDFromSenderIdentity(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "pipeline_channel_context")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(context.Background(), `
		CREATE TABLE channel_identities (
			id UUID PRIMARY KEY,
			channel_type TEXT NOT NULL,
			external_user_id TEXT NOT NULL,
			display_name TEXT NULL,
			metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			user_id UUID NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		t.Fatalf("create channel_identities: %v", err)
	}

	identityID := uuid.New()
	senderUserID := uuid.New()
	if _, err := pool.Exec(
		context.Background(),
		`INSERT INTO channel_identities (id, channel_type, external_user_id, user_id)
		 VALUES ($1, 'telegram', '10001', $2)`,
		identityID,
		senderUserID,
	); err != nil {
		t.Fatalf("insert channel identity: %v", err)
	}

	originalUserID := uuid.New()
	channelID := uuid.New()
	rc := &RunContext{
		UserID: &originalUserID,
		JobPayload: map[string]any{
			"channel_delivery": map[string]any{
				"channel_id":                 channelID.String(),
				"channel_type":               "telegram",
				"platform_chat_id":           "10001",
				"reply_to_message_id":        "42",
				"sender_channel_identity_id": identityID.String(),
			},
		},
	}

	h := Build([]RunMiddleware{NewChannelContextMiddleware(pool)}, func(_ context.Context, rc *RunContext) error {
		if rc.ChannelContext == nil {
			t.Fatal("expected channel context to be populated")
		}
		if rc.ChannelContext.ChannelID != channelID {
			t.Fatalf("unexpected channel id: %s", rc.ChannelContext.ChannelID)
		}
		if rc.ChannelContext.SenderUserID == nil || *rc.ChannelContext.SenderUserID != senderUserID {
			t.Fatalf("unexpected sender user id: %#v", rc.ChannelContext.SenderUserID)
		}
		if rc.ChannelContext.ReplyToMessageID == nil || *rc.ChannelContext.ReplyToMessageID != "42" {
			t.Fatalf("unexpected reply_to_message_id: %#v", rc.ChannelContext.ReplyToMessageID)
		}
		if rc.UserID == nil || *rc.UserID != senderUserID {
			t.Fatalf("expected rc.UserID to be overridden, got %#v", rc.UserID)
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}
}
