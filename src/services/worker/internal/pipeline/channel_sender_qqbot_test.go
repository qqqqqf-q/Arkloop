package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"arkloop/services/shared/qqbotclient"
)

func TestQQBotChannelSenderKeepsReplyIDForAllSegments(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_, _ = w.Write([]byte(`{"access_token":"access-1","expires_in":7200}`))
		case "/v2/groups/group-openid/messages":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode send body: %v", err)
			}
			bodies = append(bodies, body)
			_, _ = fmt.Fprintf(w, `{"id":"msg-%d"}`, len(bodies))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := qqbotclient.NewClient(qqbotclient.Credentials{AppID: "app-1", ClientSecret: "secret-1"}, &qqbotclient.ClientOptions{
		TokenURL: srv.URL + "/token",
		APIBase:  srv.URL,
	})
	sender := NewQQBotChannelSender(client, 0)
	ids, err := sender.SendText(context.Background(), ChannelDeliveryTarget{
		Conversation: ChannelConversationRef{Target: "group-openid"},
		ReplyTo:      &ChannelMessageRef{MessageID: "inbound-1"},
		Metadata:     map[string]any{"scope": "group"},
	}, strings.Repeat("a", qqMessageMaxLen+1))
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(ids) != 2 || len(bodies) != 2 {
		t.Fatalf("segments sent ids=%v bodies=%d", ids, len(bodies))
	}
	for idx, body := range bodies {
		if body["msg_id"] != "inbound-1" {
			t.Fatalf("body %d msg_id = %#v", idx, body["msg_id"])
		}
		if _, ok := body["msg_seq"].(float64); !ok {
			t.Fatalf("body %d missing msg_seq: %#v", idx, body)
		}
	}
	if bodies[0]["msg_seq"] == bodies[1]["msg_seq"] {
		t.Fatalf("msg_seq repeated across segments: %#v", bodies)
	}
}
