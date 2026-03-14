package acp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, NewClient(srv.URL, "test-token")
}

func readJSON(t *testing.T, r *http.Request, v any) {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func TestClient_Start(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/acp/start" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing auth header")
		}

		var req StartRequest
		readJSON(t, r, &req)

		if req.SessionID != "sess-1" {
			t.Errorf("session_id = %q, want %q", req.SessionID, "sess-1")
		}
		if req.AccountID != "acct-1" {
			t.Errorf("account_id = %q, want %q", req.AccountID, "acct-1")
		}
		if len(req.Command) != 2 || req.Command[0] != "opencode" {
			t.Errorf("command = %v, want [opencode acp]", req.Command)
		}

		writeJSON(t, w, StartResponse{ProcessID: "proc-123", Status: "running"})
	})

	resp, err := client.Start(context.Background(), StartRequest{
		SessionID: "sess-1",
		AccountID: "acct-1",
		Command:   []string{"opencode", "acp"},
		Cwd:       "/workspace",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if resp.ProcessID != "proc-123" {
		t.Errorf("ProcessID = %q, want %q", resp.ProcessID, "proc-123")
	}
	if resp.Status != "running" {
		t.Errorf("Status = %q, want %q", resp.Status, "running")
	}
}

func TestClient_Write(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/acp/write" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var req WriteRequest
		readJSON(t, r, &req)

		if req.ProcessID != "proc-123" {
			t.Errorf("process_id = %q, want %q", req.ProcessID, "proc-123")
		}
		if req.Data != `{"jsonrpc":"2.0"}` {
			t.Errorf("data = %q", req.Data)
		}

		w.WriteHeader(http.StatusOK)
	})

	err := client.Write(context.Background(), WriteRequest{
		SessionID: "sess-1",
		AccountID: "acct-1",
		ProcessID: "proc-123",
		Data:      `{"jsonrpc":"2.0"}`,
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
}

func TestClient_Read(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/acp/read" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var req ReadRequest
		readJSON(t, r, &req)

		if req.Cursor != 42 {
			t.Errorf("cursor = %d, want 42", req.Cursor)
		}

		writeJSON(t, w, ReadResponse{
			Data:       `{"method":"session/update"}`,
			NextCursor: 100,
			Truncated:  false,
			Exited:     false,
		})
	})

	resp, err := client.Read(context.Background(), ReadRequest{
		SessionID: "sess-1",
		AccountID: "acct-1",
		ProcessID: "proc-123",
		Cursor:    42,
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if resp.NextCursor != 100 {
		t.Errorf("NextCursor = %d, want 100", resp.NextCursor)
	}
	if resp.Data != `{"method":"session/update"}` {
		t.Errorf("Data = %q", resp.Data)
	}
}

func TestClient_Stop(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/acp/stop" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var req StopRequest
		readJSON(t, r, &req)

		if req.Force {
			t.Errorf("force = true, want false")
		}

		w.WriteHeader(http.StatusOK)
	})

	err := client.Stop(context.Background(), StopRequest{
		SessionID: "sess-1",
		AccountID: "acct-1",
		ProcessID: "proc-123",
		Force:     false,
	})
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestClient_Wait(t *testing.T) {
	exitCode := 0
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/acp/wait" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var req WaitRequest
		readJSON(t, r, &req)

		if req.TimeoutMs != 30000 {
			t.Errorf("timeout_ms = %d, want 30000", req.TimeoutMs)
		}

		writeJSON(t, w, WaitResponse{
			Exited:   true,
			ExitCode: &exitCode,
			Stdout:   "done",
		})
	})

	resp, err := client.Wait(context.Background(), WaitRequest{
		SessionID: "sess-1",
		AccountID: "acct-1",
		ProcessID: "proc-123",
		TimeoutMs: 30000,
	})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !resp.Exited {
		t.Errorf("Exited = false, want true")
	}
	if resp.ExitCode == nil || *resp.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", resp.ExitCode)
	}
	if resp.Stdout != "done" {
		t.Errorf("Stdout = %q, want %q", resp.Stdout, "done")
	}
}

func TestClient_StartError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantCode   int
	}{
		{"not found", http.StatusNotFound, "session not found", 404},
		{"server error", http.StatusInternalServerError, "internal error", 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			})

			_, err := client.Start(context.Background(), StartRequest{
				SessionID: "sess-1",
				AccountID: "acct-1",
				Command:   []string{"opencode", "acp"},
			})
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			ce, ok := err.(*ClientError)
			if !ok {
				t.Fatalf("error type = %T, want *ClientError", err)
			}
			if ce.StatusCode != tt.wantCode {
				t.Errorf("StatusCode = %d, want %d", ce.StatusCode, tt.wantCode)
			}
			if ce.Message != tt.body {
				t.Errorf("Message = %q, want %q", ce.Message, tt.body)
			}
		})
	}
}
