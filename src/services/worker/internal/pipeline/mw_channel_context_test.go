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
		"conversation_ref":           map[string]any{"target": "10001"},
		"inbound_message_ref":        map[string]any{"message_id": "42"},
		"sender_channel_identity_id": uuid.NewString(),
	})
	if err == nil {
		t.Fatal("expected parse error for invalid channel_id")
	}
}

func TestParseChannelContextAllowsHeartbeatWithoutInboundMessage(t *testing.T) {
	channelID := uuid.New()
	senderID := uuid.New()
	got, err := parseChannelContext(map[string]any{
		"channel_id":   channelID.String(),
		"channel_type": "telegram",
		"conversation_ref": map[string]any{
			"target": "10001",
		},
		"sender_channel_identity_id": senderID.String(),
		"conversation_type":          "supergroup",
	})
	if err != nil {
		t.Fatalf("parseChannelContext: %v", err)
	}
	if got.ChannelID != channelID {
		t.Fatalf("unexpected channel id: %s", got.ChannelID)
	}
	if got.InboundMessage.MessageID != "" {
		t.Fatalf("expected empty inbound message id, got %q", got.InboundMessage.MessageID)
	}
	if got.SenderChannelIdentityID != senderID {
		t.Fatalf("unexpected sender channel identity id: %s", got.SenderChannelIdentityID)
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
				"channel_id":   channelID.String(),
				"channel_type": "telegram",
				"conversation_ref": map[string]any{
					"target":    "10001",
					"thread_id": "thread-7",
				},
				"inbound_message_ref": map[string]any{
					"message_id": "99",
				},
				"trigger_message_ref": map[string]any{
					"message_id": "42",
				},
				"inbound_reply_to_message_id": "13",
				"conversation_type":           "private",
				"mentions_bot":                true,
				"sender_channel_identity_id":  identityID.String(),
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
		if rc.ChannelContext.TriggerMessage == nil || rc.ChannelContext.TriggerMessage.MessageID != "42" {
			t.Fatalf("unexpected trigger message: %#v", rc.ChannelContext.TriggerMessage)
		}
		if rc.ChannelContext.InboundMessage.MessageID != "99" {
			t.Fatalf("unexpected inbound message id: %q", rc.ChannelContext.InboundMessage.MessageID)
		}
		if rc.ChannelContext.Conversation.Target != "10001" {
			t.Fatalf("unexpected conversation target: %q", rc.ChannelContext.Conversation.Target)
		}
		if rc.ChannelContext.Conversation.ThreadID == nil || *rc.ChannelContext.Conversation.ThreadID != "thread-7" {
			t.Fatalf("unexpected conversation thread: %#v", rc.ChannelContext.Conversation.ThreadID)
		}
		if rc.ChannelContext.ConversationType != "private" || !rc.ChannelContext.MentionsBot {
			t.Fatalf("unexpected conversation flags: %#v", rc.ChannelContext)
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

func TestChannelContextMiddlewareFallsBackToChannelOwnerWhenIdentityMissingUserID(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "pipeline_channel_context_owner_fallback")
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
	if _, err := pool.Exec(context.Background(), `
		CREATE TABLE channels (
			id UUID PRIMARY KEY,
			owner_user_id UUID NULL
		)`); err != nil {
		t.Fatalf("create channels: %v", err)
	}

	identityID := uuid.New()
	channelID := uuid.New()
	ownerUserID := uuid.New()
	if _, err := pool.Exec(
		context.Background(),
		`INSERT INTO channel_identities (id, channel_type, external_user_id, metadata_json)
		 VALUES ($1, 'telegram', '10001', '{}'::jsonb)`,
		identityID,
	); err != nil {
		t.Fatalf("insert channel identity: %v", err)
	}
	if _, err := pool.Exec(
		context.Background(),
		`INSERT INTO channels (id, owner_user_id) VALUES ($1, $2)`,
		channelID,
		ownerUserID,
	); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	originalUserID := uuid.New()
	rc := &RunContext{
		UserID: &originalUserID,
		JobPayload: map[string]any{
			"channel_delivery": map[string]any{
				"channel_id":                 channelID.String(),
				"channel_type":               "telegram",
				"conversation_ref":           map[string]any{"target": "10001"},
				"sender_channel_identity_id": identityID.String(),
			},
		},
	}

	h := Build([]RunMiddleware{NewChannelContextMiddleware(pool)}, func(_ context.Context, rc *RunContext) error {
		if rc.ChannelContext == nil {
			t.Fatal("expected channel context to be populated")
		}
		if rc.ChannelContext.SenderUserID == nil || *rc.ChannelContext.SenderUserID != ownerUserID {
			t.Fatalf("unexpected sender user id: %#v", rc.ChannelContext.SenderUserID)
		}
		if rc.UserID == nil || *rc.UserID != ownerUserID {
			t.Fatalf("unexpected rc.UserID: %#v", rc.UserID)
		}
		if *rc.UserID == originalUserID {
			t.Fatalf("expected rc.UserID to be overridden, got %#v", rc.UserID)
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}
}
