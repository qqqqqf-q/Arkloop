package onebotclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendPrivateMsg(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/send_private_msg" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["user_id"] != "12345" {
			t.Errorf("expected user_id 12345, got %v", body["user_id"])
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"retcode": 0,
			"data":    map[string]any{"message_id": "100"},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", nil)
	resp, err := c.SendPrivateMsg(context.Background(), "12345", TextSegments("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.MessageID.String() != "100" {
		t.Errorf("expected message_id 100, got %s", resp.MessageID.String())
	}
}

func TestSendGroupMsg(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/send_group_msg" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"retcode": 0,
			"data":    map[string]any{"message_id": "200"},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", nil)
	resp, err := c.SendGroupMsg(context.Background(), "67890", TextSegments("hi group"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.MessageID.String() != "200" {
		t.Errorf("expected message_id 200, got %s", resp.MessageID.String())
	}
}

func TestCallJSONWithToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"retcode": 0,
			"data":    map[string]any{"user_id": "111", "nickname": "test"},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "my-secret-token", nil)
	_, err := c.GetLoginInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer my-secret-token" {
		t.Errorf("expected Bearer auth, got %q", gotAuth)
	}
}

func TestCallJSONError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"status":  "failed",
			"retcode": 100,
			"message": "bot not online",
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", nil)
	_, err := c.SendPrivateMsg(context.Background(), "12345", TextSegments("x"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEventPlainText(t *testing.T) {
	segments := []MessageSegment{
		{Type: "text", Data: json.RawMessage(`{"text":"hello "}`)},
		{Type: "at", Data: json.RawMessage(`{"qq":"12345"}`)},
		{Type: "text", Data: json.RawMessage(`{"text":"world"}`)},
	}
	msgBytes, _ := json.Marshal(segments)
	e := Event{
		PostType:    "message",
		MessageType: "group",
		Message:     msgBytes,
	}
	got := e.PlainText()
	if got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}
}

func TestEventPlainTextFallbackRawMessage(t *testing.T) {
	e := Event{
		PostType:   "message",
		RawMessage: "raw text",
	}
	if e.PlainText() != "raw text" {
		t.Errorf("expected 'raw text', got %q", e.PlainText())
	}
}
