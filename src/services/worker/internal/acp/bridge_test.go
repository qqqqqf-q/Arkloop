package acp

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"arkloop/services/worker/internal/events"
)

// --- mock transport ---

type mockTransport struct {
	mu      sync.Mutex
	startFn func(ctx context.Context, req StartRequest) (*StartResponse, error)
	writeFn func(ctx context.Context, req WriteRequest) error
	readFn  func(ctx context.Context, req ReadRequest) (*ReadResponse, error)
	stopFn  func(ctx context.Context, req StopRequest) error
	waitFn  func(ctx context.Context, req WaitRequest) (*WaitResponse, error)

	writes  []WriteRequest
	stopped bool
}

func (m *mockTransport) Start(ctx context.Context, req StartRequest) (*StartResponse, error) {
	return m.startFn(ctx, req)
}

func (m *mockTransport) Write(ctx context.Context, req WriteRequest) error {
	m.mu.Lock()
	m.writes = append(m.writes, req)
	m.mu.Unlock()
	if m.writeFn != nil {
		return m.writeFn(ctx, req)
	}
	return nil
}

func (m *mockTransport) Read(ctx context.Context, req ReadRequest) (*ReadResponse, error) {
	return m.readFn(ctx, req)
}

func (m *mockTransport) Stop(ctx context.Context, req StopRequest) error {
	m.mu.Lock()
	m.stopped = true
	m.mu.Unlock()
	if m.stopFn != nil {
		return m.stopFn(ctx, req)
	}
	return nil
}

func (m *mockTransport) Wait(ctx context.Context, req WaitRequest) (*WaitResponse, error) {
	if m.waitFn != nil {
		return m.waitFn(ctx, req)
	}
	return &WaitResponse{}, nil
}

// --- test helpers ---

func mustMarshalLine(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(data) + "\n"
}

func sessionNewResponseLine(id int, sessionID string) string {
	return mustMarshalLine(ACPMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Result:  SessionNewResult{SessionID: sessionID},
	})
}

func sessionUpdateLine(sessionID string, u SessionUpdateParams) string {
	u.SessionID = sessionID
	return mustMarshalLine(ACPMessage{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params:  u,
	})
}

func testConfig() BridgeConfig {
	return BridgeConfig{
		SessionID:    "ses-1",
		AccountID:    "acc-1",
		Command:      []string{"opencode", "acp", "--cwd", "/workspace"},
		Cwd:          "/workspace",
		PollInterval: time.Millisecond,
		ReadMaxBytes: 32 * 1024,
	}
}

func eventTypes(evts []events.RunEvent) []string {
	out := make([]string, len(evts))
	for i, e := range evts {
		out[i] = e.Type
	}
	return out
}

// --- tests ---

func TestBridge_FullLifecycle(t *testing.T) {
	const acpSID = "acp-session-abc"
	readCount := 0

	mock := &mockTransport{
		startFn: func(_ context.Context, req StartRequest) (*StartResponse, error) {
			if req.SessionID != "ses-1" {
				t.Errorf("start session_id = %q, want %q", req.SessionID, "ses-1")
			}
			return &StartResponse{ProcessID: "proc-1", Status: "running"}, nil
		},
		readFn: func(_ context.Context, _ ReadRequest) (*ReadResponse, error) {
			readCount++
			switch readCount {
			case 1:
				return &ReadResponse{
					Data:       sessionNewResponseLine(1, acpSID),
					NextCursor: 100,
				}, nil
			case 2:
				return &ReadResponse{
					Data:       sessionUpdateLine(acpSID, SessionUpdateParams{Type: UpdateTypeTextDelta, Content: "hello world"}),
					NextCursor: 200,
				}, nil
			case 3:
				return &ReadResponse{
					Data:       sessionUpdateLine(acpSID, SessionUpdateParams{Type: UpdateTypeComplete, Summary: "done"}),
					NextCursor: 300,
				}, nil
			default:
				t.Fatal("unexpected extra read call")
				return nil, nil
			}
		},
	}

	bridge := NewBridge(mock, testConfig())
	emitter := events.NewEmitter("trace-1")
	var got []events.RunEvent

	err := bridge.Run(context.Background(), "write tests", emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	wantTypes := []string{"run.started", "message.delta", "run.completed"}
	if len(got) != len(wantTypes) {
		t.Fatalf("got %d events %v, want %d %v", len(got), eventTypes(got), len(wantTypes), wantTypes)
	}
	for i, want := range wantTypes {
		if got[i].Type != want {
			t.Errorf("event[%d].Type = %q, want %q", i, got[i].Type, want)
		}
	}

	if got[1].DataJSON["content_delta"] != "hello world" {
		t.Errorf("delta content = %v, want %q", got[1].DataJSON["content_delta"], "hello world")
	}
	if got[2].DataJSON["summary"] != "done" {
		t.Errorf("summary = %v, want %q", got[2].DataJSON["summary"], "done")
	}

	if !mock.stopped {
		t.Error("process was not stopped during cleanup")
	}
}

func TestBridge_ErrorDuringExecution(t *testing.T) {
	const acpSID = "acp-session-err"
	readCount := 0

	mock := &mockTransport{
		startFn: func(_ context.Context, _ StartRequest) (*StartResponse, error) {
			return &StartResponse{ProcessID: "proc-2"}, nil
		},
		readFn: func(_ context.Context, _ ReadRequest) (*ReadResponse, error) {
			readCount++
			switch readCount {
			case 1:
				return &ReadResponse{Data: sessionNewResponseLine(1, acpSID), NextCursor: 100}, nil
			case 2:
				return &ReadResponse{
					Data:       sessionUpdateLine(acpSID, SessionUpdateParams{Type: UpdateTypeError, Message: "out of tokens"}),
					NextCursor: 200,
				}, nil
			default:
				return &ReadResponse{NextCursor: 200}, nil
			}
		},
	}

	bridge := NewBridge(mock, testConfig())
	emitter := events.NewEmitter("trace-2")
	var got []events.RunEvent

	err := bridge.Run(context.Background(), "do stuff", emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	wantTypes := []string{"run.started", "run.failed"}
	if len(got) != len(wantTypes) {
		t.Fatalf("got %d events %v, want %d %v", len(got), eventTypes(got), len(wantTypes), wantTypes)
	}
	for i, want := range wantTypes {
		if got[i].Type != want {
			t.Errorf("event[%d].Type = %q, want %q", i, got[i].Type, want)
		}
	}

	if got[1].ErrorClass == nil || *got[1].ErrorClass != "acp.agent_error" {
		t.Errorf("error_class = %v, want %q", got[1].ErrorClass, "acp.agent_error")
	}
	if got[1].DataJSON["message"] != "out of tokens" {
		t.Errorf("message = %v, want %q", got[1].DataJSON["message"], "out of tokens")
	}
	if !mock.stopped {
		t.Error("process was not stopped during cleanup")
	}
}

func TestBridge_ProcessExitsUnexpectedly(t *testing.T) {
	const acpSID = "acp-session-exit"
	readCount := 0

	mock := &mockTransport{
		startFn: func(_ context.Context, _ StartRequest) (*StartResponse, error) {
			return &StartResponse{ProcessID: "proc-3"}, nil
		},
		readFn: func(_ context.Context, _ ReadRequest) (*ReadResponse, error) {
			readCount++
			switch readCount {
			case 1:
				return &ReadResponse{Data: sessionNewResponseLine(1, acpSID), NextCursor: 100}, nil
			case 2:
				exitCode := 1
				return &ReadResponse{NextCursor: 200, Exited: true, ExitCode: &exitCode}, nil
			default:
				return &ReadResponse{NextCursor: 200, Exited: true}, nil
			}
		},
	}

	bridge := NewBridge(mock, testConfig())
	emitter := events.NewEmitter("trace-3")
	var got []events.RunEvent

	err := bridge.Run(context.Background(), "run something", emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	wantTypes := []string{"run.started", "run.failed"}
	if len(got) != len(wantTypes) {
		t.Fatalf("got %d events %v, want %d %v", len(got), eventTypes(got), len(wantTypes), wantTypes)
	}

	if got[1].ErrorClass == nil || *got[1].ErrorClass != "acp.process_exited" {
		t.Errorf("error_class = %v, want %q", got[1].ErrorClass, "acp.process_exited")
	}
	if !mock.stopped {
		t.Error("process was not stopped during cleanup")
	}
}

func TestBridge_ContextCancellation(t *testing.T) {
	const acpSID = "acp-session-cancel"
	readCount := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mock := &mockTransport{
		startFn: func(_ context.Context, _ StartRequest) (*StartResponse, error) {
			return &StartResponse{ProcessID: "proc-4"}, nil
		},
		readFn: func(rctx context.Context, _ ReadRequest) (*ReadResponse, error) {
			readCount++
			switch readCount {
			case 1:
				return &ReadResponse{Data: sessionNewResponseLine(1, acpSID), NextCursor: 100}, nil
			case 2:
				cancel() // cancel after returning this data
				return &ReadResponse{
					Data:       sessionUpdateLine(acpSID, SessionUpdateParams{Type: UpdateTypeTextDelta, Content: "partial"}),
					NextCursor: 200,
				}, nil
			default:
				return nil, rctx.Err()
			}
		},
	}

	bridge := NewBridge(mock, testConfig())
	emitter := events.NewEmitter("trace-4")
	var got []events.RunEvent

	err := bridge.Run(ctx, "long task", emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	wantTypes := []string{"run.started", "message.delta", "run.cancelled"}
	if len(got) != len(wantTypes) {
		t.Fatalf("got %d events %v, want %d %v", len(got), eventTypes(got), len(wantTypes), wantTypes)
	}
	for i, want := range wantTypes {
		if got[i].Type != want {
			t.Errorf("event[%d].Type = %q, want %q", i, got[i].Type, want)
		}
	}

	// session/cancel should have been sent
	mock.mu.Lock()
	defer mock.mu.Unlock()
	cancelSent := false
	for _, w := range mock.writes {
		if strings.Contains(w.Data, "session/cancel") {
			cancelSent = true
			break
		}
	}
	if !cancelSent {
		t.Error("session/cancel was not sent")
	}
	if !mock.stopped {
		t.Error("process was not stopped during cleanup")
	}
}

func TestMapUpdateToEvent(t *testing.T) {
	emitter := events.NewEmitter("trace-map")

	tests := []struct {
		name      string
		update    SessionUpdateParams
		wantType  string
		wantOK    bool
		wantTool  string
		wantError string
	}{
		{
			name:     "status working",
			update:   SessionUpdateParams{Type: UpdateTypeStatus, Status: StatusWorking},
			wantType: "run.started",
			wantOK:   true,
		},
		{
			name:   "status idle - ignored",
			update: SessionUpdateParams{Type: UpdateTypeStatus, Status: StatusIdle},
			wantOK: false,
		},
		{
			name:     "text_delta",
			update:   SessionUpdateParams{Type: UpdateTypeTextDelta, Content: "hi"},
			wantType: "message.delta",
			wantOK:   true,
		},
		{
			name: "tool_call",
			update: SessionUpdateParams{
				Type: UpdateTypeToolCall, Name: "read_file",
				Arguments: map[string]any{"path": "/a.txt"},
			},
			wantType: "tool.call",
			wantOK:   true,
			wantTool: "read_file",
		},
		{
			name:     "tool_result",
			update:   SessionUpdateParams{Type: UpdateTypeToolResult, Name: "read_file", Output: "contents"},
			wantType: "tool.result",
			wantOK:   true,
			wantTool: "read_file",
		},
		{
			name:     "complete",
			update:   SessionUpdateParams{Type: UpdateTypeComplete, Summary: "all done"},
			wantType: "run.completed",
			wantOK:   true,
		},
		{
			name:      "error",
			update:    SessionUpdateParams{Type: UpdateTypeError, Message: "bad"},
			wantType:  "run.failed",
			wantOK:    true,
			wantError: "acp.agent_error",
		},
		{
			name:   "unknown type",
			update: SessionUpdateParams{Type: "something_else"},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, ok := mapUpdateToEvent(tt.update, emitter)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if ev.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", ev.Type, tt.wantType)
			}
			if tt.wantTool != "" {
				if ev.ToolName == nil || *ev.ToolName != tt.wantTool {
					t.Errorf("ToolName = %v, want %q", ev.ToolName, tt.wantTool)
				}
			}
			if tt.wantError != "" {
				if ev.ErrorClass == nil || *ev.ErrorClass != tt.wantError {
					t.Errorf("ErrorClass = %v, want %q", ev.ErrorClass, tt.wantError)
				}
			}
		})
	}
}
