package runner

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"arkloop/services/cli/internal/apiclient"
)

func TestExecuteReconnectsAfterStreamEOF(t *testing.T) {
	client, server := newRunnerTestClient(t, func(server *runnerTestServer, w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/threads/thread-1/messages":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/threads/thread-1/runs":
			writeJSON(t, w, `{"run_id":"run-1"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/runs/run-1":
			server.getRunCalls++
			writeJSON(t, w, `{"run_id":"run-1","thread_id":"thread-1","status":"running"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/runs/run-1/events":
			afterSeq := r.URL.Query().Get("after_seq")
			server.afterSeqs = append(server.afterSeqs, afterSeq)
			w.Header().Set("Content-Type", "text/event-stream")
			switch afterSeq {
			case "0":
				writeSSEEvent(t, w, 1, "message.delta", `{"content_delta":"hello "}`)
			case "1":
				writeSSEEvent(t, w, 1, "message.delta", `{"content_delta":"hello "}`)
				writeSSEEvent(t, w, 2, "message.delta", `{"content_delta":"world"}`)
				writeSSEEvent(t, w, 3, "tool.call", `{"tool_name":"ls"}`)
				writeSSEEvent(t, w, 4, "run.completed", `{}`)
			default:
				t.Fatalf("unexpected after_seq: %s", afterSeq)
			}
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
		}
	})

	withReconnectBudget(t, 2, time.Millisecond, time.Millisecond)

	result, err := Execute(context.Background(), client, "thread-1", "hello", apiclient.RunParams{}, nil, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("unexpected status: %#v", result)
	}
	if result.Output != "hello world" {
		t.Fatalf("unexpected output: %q", result.Output)
	}
	if result.ToolCalls != 1 {
		t.Fatalf("unexpected tool calls: %d", result.ToolCalls)
	}
	if got := strings.Join(server.afterSeqs, ","); got != "0,1" {
		t.Fatalf("unexpected after_seq flow: %s", got)
	}
	if server.getRunCalls != 1 {
		t.Fatalf("unexpected get run calls: %d", server.getRunCalls)
	}
}

func TestExecuteUsesRunStatusWhenTerminalEventIsMissing(t *testing.T) {
	client, server := newRunnerTestClient(t, func(server *runnerTestServer, w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/threads/thread-1/messages":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/threads/thread-1/runs":
			writeJSON(t, w, `{"run_id":"run-1"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/runs/run-1":
			server.getRunCalls++
			writeJSON(t, w, `{"run_id":"run-1","thread_id":"thread-1","status":"completed"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/runs/run-1/events":
			server.afterSeqs = append(server.afterSeqs, r.URL.Query().Get("after_seq"))
			w.Header().Set("Content-Type", "text/event-stream")
			writeSSEEvent(t, w, 1, "message.delta", `{"content_delta":"done"}`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
		}
	})

	withReconnectBudget(t, 1, time.Millisecond, time.Millisecond)

	result, err := Execute(context.Background(), client, "thread-1", "hello", apiclient.RunParams{}, nil, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("unexpected status: %#v", result)
	}
	if result.Output != "done" {
		t.Fatalf("unexpected output: %q", result.Output)
	}
	if got := strings.Join(server.afterSeqs, ","); got != "0" {
		t.Fatalf("unexpected after_seq flow: %s", got)
	}
	if server.getRunCalls != 1 {
		t.Fatalf("unexpected get run calls: %d", server.getRunCalls)
	}
}

func TestExecuteFailsAfterReconnectBudgetExhausted(t *testing.T) {
	client, server := newRunnerTestClient(t, func(server *runnerTestServer, w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/threads/thread-1/messages":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/threads/thread-1/runs":
			writeJSON(t, w, `{"run_id":"run-1"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/runs/run-1":
			server.getRunCalls++
			writeJSON(t, w, `{"run_id":"run-1","thread_id":"thread-1","status":"running"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/runs/run-1/events":
			server.afterSeqs = append(server.afterSeqs, r.URL.Query().Get("after_seq"))
			w.Header().Set("Content-Type", "text/event-stream")
			writeSSEEvent(t, w, 1, "message.delta", `{"content_delta":"x"}`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
		}
	})

	withReconnectBudget(t, 2, time.Millisecond, time.Millisecond)

	result, err := Execute(context.Background(), client, "thread-1", "hello", apiclient.RunParams{}, nil, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != "error" {
		t.Fatalf("unexpected status: %#v", result)
	}
	if !strings.Contains(result.Error, "reconnect exhausted after 2 attempts") {
		t.Fatalf("unexpected error: %q", result.Error)
	}
	if got := strings.Join(server.afterSeqs, ","); got != "0,1,1" {
		t.Fatalf("unexpected after_seq flow: %s", got)
	}
	if server.getRunCalls != 3 {
		t.Fatalf("unexpected get run calls: %d", server.getRunCalls)
	}
}

type runnerTestServer struct {
	mu          sync.Mutex
	afterSeqs   []string
	getRunCalls int
}

func newRunnerTestClient(t *testing.T, handler func(*runnerTestServer, http.ResponseWriter, *http.Request)) (*apiclient.Client, *runnerTestServer) {
	t.Helper()
	serverState := &runnerTestServer{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverState.mu.Lock()
		defer serverState.mu.Unlock()
		handler(serverState, w, r)
	}))
	t.Cleanup(server.Close)
	return apiclient.NewClient(server.URL, "test-token"), serverState
}

func withReconnectBudget(t *testing.T, attempts int, baseDelay, maxDelay time.Duration) {
	t.Helper()
	oldAttempts := sseReconnectMaxAttempts
	oldBaseDelay := sseReconnectBaseDelay
	oldMaxDelay := sseReconnectMaxDelay
	sseReconnectMaxAttempts = attempts
	sseReconnectBaseDelay = baseDelay
	sseReconnectMaxDelay = maxDelay
	t.Cleanup(func() {
		sseReconnectMaxAttempts = oldAttempts
		sseReconnectBaseDelay = oldBaseDelay
		sseReconnectMaxDelay = oldMaxDelay
	})
}

func writeJSON(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatalf("write json: %v", err)
	}
}

func writeSSEEvent(t *testing.T, w http.ResponseWriter, seq int64, eventType string, payload string) {
	t.Helper()
	if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", seq, eventType, payload); err != nil {
		t.Fatalf("write sse event: %v", err)
	}
}
