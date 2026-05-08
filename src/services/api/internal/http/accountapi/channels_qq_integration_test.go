//go:build !desktop

package accountapi

import (
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"arkloop/services/shared/onebotclient"
	"arkloop/services/shared/telegrambot"
)

func TestQQGroupHeartbeatRequiresBoundSender(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []onebotclient.SendMsgRequest
	)
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.URL.Path == "/send_group_msg" {
			var req onebotclient.SendMsgRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
				mu.Lock()
				requests = append(requests, req)
				mu.Unlock()
			}
		}
		_, _ = io.WriteString(w, `{"status":"ok","retcode":0,"data":{"message_id":1}}`)
	}))
	defer server.Close()

	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient("https://api.telegram.org", nil))

	createResp := doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/channels",
		map[string]any{
			"channel_type": "qq",
			"bot_token":    "",
			"persona_id":   env.personaID.String(),
			"config_json": map[string]any{
				"onebot_http_url": server.URL,
			},
		},
		authHeader(env.accessToken),
	)
	if createResp.Code != nethttp.StatusCreated {
		t.Fatalf("create qq channel: %d %s", createResp.Code, createResp.Body.String())
	}
	var channel channelResponse
	if err := json.Unmarshal(createResp.Body.Bytes(), &channel); err != nil {
		t.Fatalf("decode channel: %v", err)
	}

	updateResp := doJSONAccount(
		env.handler,
		nethttp.MethodPatch,
		"/v1/channels/"+channel.ID,
		map[string]any{"is_active": true},
		authHeader(env.accessToken),
	)
	if updateResp.Code != nethttp.StatusOK {
		t.Fatalf("activate qq channel: %d %s", updateResp.Code, updateResp.Body.String())
	}

	callbackResp := doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/napcat/onebot-callback",
		map[string]any{
			"time":         1710000000,
			"self_id":      998877,
			"post_type":    "message",
			"message_type": "group",
			"message_id":   201,
			"user_id":      10001,
			"group_id":     20001,
			"raw_message":  "/heartbeat on",
			"message": []map[string]any{
				{"type": "text", "data": map[string]any{"text": "/heartbeat on"}},
			},
			"sender": map[string]any{
				"user_id":  10001,
				"nickname": "Alice",
				"card":     "Alice",
				"role":     "owner",
			},
		},
		nil,
	)
	if callbackResp.Code != nethttp.StatusOK {
		t.Fatalf("qq callback: %d %s", callbackResp.Code, callbackResp.Body.String())
	}

	var linkCount int
	if err := env.pool.QueryRow(
		context.Background(),
		`SELECT COUNT(*)
		   FROM channel_identity_links cil
		   JOIN channel_identities ci ON ci.id = cil.channel_identity_id
		  WHERE cil.channel_id = $1
		    AND ci.platform_subject_id = $2`,
		channel.ID,
		"20001",
	).Scan(&linkCount); err != nil {
		t.Fatalf("query group heartbeat links: %v", err)
	}
	if linkCount != 0 {
		t.Fatalf("expected unbound sender to leave no heartbeat link, got %d", linkCount)
	}

	mu.Lock()
	gotRequests := append([]onebotclient.SendMsgRequest(nil), requests...)
	mu.Unlock()
	if len(gotRequests) != 1 {
		t.Fatalf("expected one group reply, got %#v", gotRequests)
	}
	var text onebotclient.TextData
	if len(gotRequests[0].Message) != 1 || gotRequests[0].Message[0].Type != "text" {
		t.Fatalf("unexpected reply message: %#v", gotRequests[0].Message)
	}
	if err := json.Unmarshal(gotRequests[0].Message[0].Data, &text); err != nil {
		t.Fatalf("decode reply text: %v", err)
	}
	if !strings.Contains(text.Text, "/bind") {
		t.Fatalf("unexpected reply text: %q", text.Text)
	}
}

func TestQQGroupBindCommandCreatesBindingAndReplies(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []onebotclient.SendMsgRequest
	)
	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.URL.Path == "/send_group_msg" {
			var req onebotclient.SendMsgRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
				mu.Lock()
				requests = append(requests, req)
				mu.Unlock()
			}
		}
		_, _ = io.WriteString(w, `{"status":"ok","retcode":0,"data":{"message_id":1}}`)
	}))
	defer server.Close()

	env := setupTelegramChannelsTestEnv(t, telegrambot.NewClient("https://api.telegram.org", nil))

	createResp := doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/channels",
		map[string]any{
			"channel_type": "qq",
			"bot_token":    "",
			"persona_id":   env.personaID.String(),
			"config_json": map[string]any{
				"onebot_http_url": server.URL,
			},
		},
		authHeader(env.accessToken),
	)
	if createResp.Code != nethttp.StatusCreated {
		t.Fatalf("create qq channel: %d %s", createResp.Code, createResp.Body.String())
	}
	var channel channelResponse
	if err := json.Unmarshal(createResp.Body.Bytes(), &channel); err != nil {
		t.Fatalf("decode channel: %v", err)
	}

	updateResp := doJSONAccount(
		env.handler,
		nethttp.MethodPatch,
		"/v1/channels/"+channel.ID,
		map[string]any{"is_active": true},
		authHeader(env.accessToken),
	)
	if updateResp.Code != nethttp.StatusOK {
		t.Fatalf("activate qq channel: %d %s", updateResp.Code, updateResp.Body.String())
	}

	bindCreate := doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/me/channel-binds",
		map[string]any{"channel_type": "qq"},
		authHeader(env.accessToken),
	)
	if bindCreate.Code != nethttp.StatusCreated {
		t.Fatalf("create bind code: %d %s", bindCreate.Code, bindCreate.Body.String())
	}
	var bindBody struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(bindCreate.Body.Bytes(), &bindBody); err != nil {
		t.Fatalf("decode bind code: %v", err)
	}
	if strings.TrimSpace(bindBody.Token) == "" {
		t.Fatal("empty bind token")
	}

	callbackResp := doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/napcat/onebot-callback",
		map[string]any{
			"time":         1710000000,
			"self_id":      998877,
			"post_type":    "message",
			"message_type": "group",
			"message_id":   101,
			"user_id":      10001,
			"group_id":     20001,
			"raw_message":  "/bind " + bindBody.Token,
			"message": []map[string]any{
				{"type": "text", "data": map[string]any{"text": "/bind " + bindBody.Token}},
			},
			"sender": map[string]any{
				"user_id":  10001,
				"nickname": "Alice",
				"card":     "Alice",
				"role":     "owner",
			},
		},
		nil,
	)
	if callbackResp.Code != nethttp.StatusOK {
		t.Fatalf("qq callback: %d %s", callbackResp.Code, callbackResp.Body.String())
	}

	listResp := doJSONAccount(
		env.handler,
		nethttp.MethodGet,
		"/v1/channels/"+channel.ID+"/bindings",
		nil,
		authHeader(env.accessToken),
	)
	if listResp.Code != nethttp.StatusOK {
		t.Fatalf("list bindings: %d %s", listResp.Code, listResp.Body.String())
	}
	var bindings []channelBindingResponse
	if err := json.Unmarshal(listResp.Body.Bytes(), &bindings); err != nil {
		t.Fatalf("decode bindings: %v", err)
	}
	if len(bindings) != 1 || bindings[0].PlatformSubjectID != "10001" {
		t.Fatalf("unexpected bindings: %#v", bindings)
	}

	heartbeatResp := doJSONAccount(
		env.handler,
		nethttp.MethodPost,
		"/v1/napcat/onebot-callback",
		map[string]any{
			"time":         1710000001,
			"self_id":      998877,
			"post_type":    "message",
			"message_type": "group",
			"message_id":   102,
			"user_id":      10001,
			"group_id":     20001,
			"raw_message":  "/heartbeat on",
			"message": []map[string]any{
				{"type": "text", "data": map[string]any{"text": "/heartbeat on"}},
			},
			"sender": map[string]any{
				"user_id":  10001,
				"nickname": "Alice",
				"card":     "Alice",
				"role":     "owner",
			},
		},
		nil,
	)
	if heartbeatResp.Code != nethttp.StatusOK {
		t.Fatalf("qq heartbeat callback: %d %s", heartbeatResp.Code, heartbeatResp.Body.String())
	}

	var triggerCount int
	if err := env.pool.QueryRow(
		context.Background(),
		`SELECT COUNT(*)
		   FROM scheduled_triggers st
		   JOIN channel_identities ci ON ci.id = st.channel_identity_id
		  WHERE st.channel_id = $1
		    AND ci.platform_subject_id = $2
		    AND st.trigger_kind = 'heartbeat'`,
		channel.ID,
		"20001",
	).Scan(&triggerCount); err != nil {
		t.Fatalf("query heartbeat trigger: %v", err)
	}
	if triggerCount != 1 {
		t.Fatalf("expected one group heartbeat trigger, got %d", triggerCount)
	}

	var ownerHeartbeat int
	if err := env.pool.QueryRow(
		context.Background(),
		`SELECT cil.heartbeat_enabled
		   FROM channel_identity_links cil
		   JOIN channel_identities ci ON ci.id = cil.channel_identity_id
		  WHERE cil.channel_id = $1
		    AND ci.platform_subject_id = $2`,
		channel.ID,
		"10001",
	).Scan(&ownerHeartbeat); err != nil {
		t.Fatalf("query owner heartbeat config: %v", err)
	}
	if ownerHeartbeat != 1 {
		t.Fatalf("expected owner binding heartbeat enabled, got %d", ownerHeartbeat)
	}

	var groupLinkCount int
	if err := env.pool.QueryRow(
		context.Background(),
		`SELECT COUNT(*)
		   FROM channel_identity_links cil
		   JOIN channel_identities ci ON ci.id = cil.channel_identity_id
		  WHERE cil.channel_id = $1
		    AND ci.platform_subject_id = $2`,
		channel.ID,
		"20001",
	).Scan(&groupLinkCount); err != nil {
		t.Fatalf("query group link count: %v", err)
	}
	if groupLinkCount != 0 {
		t.Fatalf("group heartbeat target should not appear as binding, got %d", groupLinkCount)
	}

	mu.Lock()
	gotRequests := append([]onebotclient.SendMsgRequest(nil), requests...)
	mu.Unlock()
	if len(gotRequests) != 2 {
		t.Fatalf("expected two group replies, got %#v", gotRequests)
	}
	if gotRequests[0].GroupID != "20001" {
		t.Fatalf("unexpected group reply target: %#v", gotRequests[0])
	}
	if len(gotRequests[0].Message) != 1 || gotRequests[0].Message[0].Type != "text" {
		t.Fatalf("unexpected reply message: %#v", gotRequests[0].Message)
	}
	var text onebotclient.TextData
	if err := json.Unmarshal(gotRequests[0].Message[0].Data, &text); err != nil {
		t.Fatalf("decode reply text: %v", err)
	}
	if strings.TrimSpace(text.Text) != "绑定成功。" {
		t.Fatalf("unexpected reply text: %q", text.Text)
	}
}
