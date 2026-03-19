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
	mu       sync.Mutex
	startFn  func(ctx context.Context, req StartRequest) (*StartResponse, error)
	writeFn  func(ctx context.Context, req WriteRequest) error
	readFn   func(ctx context.Context, req ReadRequest) (*ReadResponse, error)
	stopFn   func(ctx context.Context, req StopRequest) error
	waitFn   func(ctx context.Context, req WaitRequest) (*WaitResponse, error)
	statusFn func(ctx context.Context, req StatusRequest) (*StatusResponse, error)

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

func (m *mockTransport) Status(ctx context.Context, req StatusRequest) (*StatusResponse, error) {
	if m.statusFn != nil {
		return m.statusFn(ctx, req)
	}
	return &StatusResponse{Running: true}, nil
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
	// Build the nested update object matching opencode's actual format
	update := map[string]any{
		"sessionUpdate": u.Type,
	}
	switch u.Type {
	case UpdateTypeTextDelta:
		update["textDelta"] = u.Content
	case UpdateTypeToolCall:
		update["toolName"] = u.Name
		update["args"] = u.Arguments
	case UpdateTypeToolResult:
		update["toolName"] = u.Name
		update["result"] = u.Output
	case UpdateTypeStatus:
		update["status"] = u.Status
	case UpdateTypeComplete:
		update["summary"] = u.Summary
	case UpdateTypeError:
		update["message"] = u.Message
	case UpdateTypePermission:
		update["permissionId"] = u.PermissionID
		update["sensitive"] = u.Sensitive
		if u.Content != "" {
			update["textDelta"] = u.Content
		}
	}
	raw := sessionUpdateRaw{
		SessionID: sessionID,
		Update:    update,
	}
	return mustMarshalLine(ACPMessage{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params:  raw,
	})
}

func testConfig() BridgeConfig {
	return BridgeConfig{
		RuntimeSessionKey: "ses-1",
		AccountID:         "acc-1",
		Command:           []string{"opencode", "acp", "--cwd", "/workspace"},
		Cwd:               "/workspace",
		PollInterval:      time.Millisecond,
		ReadMaxBytes:      32 * 1024,
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
			if req.RuntimeSessionKey != "ses-1" {
				t.Errorf("start runtime_session_key = %q, want %q", req.RuntimeSessionKey, "ses-1")
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
	if got[0].DataJSON["runtime_session_key"] != "ses-1" {
		t.Errorf("runtime_session_key = %v, want %q", got[0].DataJSON["runtime_session_key"], "ses-1")
	}
	if got[2].DataJSON["summary"] != "done" {
		t.Errorf("summary = %v, want %q", got[2].DataJSON["summary"], "done")
	}

	if mock.stopped {
		t.Error("process should not be stopped after successful Run (caller owns lifecycle)")
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
	bridge.Close()
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
	bridge.Close()
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
	cancelSent := false
	for _, w := range mock.writes {
		if strings.Contains(w.Data, "session/cancel") {
			cancelSent = true
			break
		}
	}
	mock.mu.Unlock()
	if !cancelSent {
		t.Error("session/cancel was not sent")
	}
	bridge.Close()
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
		{
			name:   "permission_request",
			update: SessionUpdateParams{Type: UpdateTypePermission, PermissionID: "p1", Content: "delete file", Sensitive: true},
			wantOK: false, // permission requests are handled directly in pollUpdates, not mapped
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

func TestBridge_PermissionRequest(t *testing.T) {
	const acpSID = "acp-session-perm"
	readCount := 0

	mock := &mockTransport{
		startFn: func(_ context.Context, _ StartRequest) (*StartResponse, error) {
			return &StartResponse{ProcessID: "proc-perm", AgentVersion: "local-sandbox/1.0"}, nil
		},
		readFn: func(_ context.Context, _ ReadRequest) (*ReadResponse, error) {
			readCount++
			switch readCount {
			case 1:
				return &ReadResponse{Data: sessionNewResponseLine(1, acpSID), NextCursor: 100}, nil
			case 2:
				return &ReadResponse{
					Data: sessionUpdateLine(acpSID, SessionUpdateParams{
						Type:         UpdateTypePermission,
						PermissionID: "perm-001",
						Content:      "execute rm -rf /tmp/test",
						Sensitive:    true,
					}),
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
	emitter := events.NewEmitter("trace-perm")
	var got []events.RunEvent

	err := bridge.Run(context.Background(), "clean up", emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	wantTypes := []string{"run.started", "acp.permission_required", "run.completed"}
	if len(got) != len(wantTypes) {
		t.Fatalf("got %d events %v, want %d %v", len(got), eventTypes(got), len(wantTypes), wantTypes)
	}
	for i, want := range wantTypes {
		if got[i].Type != want {
			t.Errorf("event[%d].Type = %q, want %q", i, got[i].Type, want)
		}
	}

	permEvt := got[1]
	if permEvt.DataJSON["permission_id"] != "perm-001" {
		t.Errorf("permission_id = %v, want %q", permEvt.DataJSON["permission_id"], "perm-001")
	}
	if permEvt.DataJSON["approved"] != true {
		t.Errorf("approved = %v, want true", permEvt.DataJSON["approved"])
	}
	if permEvt.DataJSON["sensitive"] != true {
		t.Errorf("sensitive = %v, want true", permEvt.DataJSON["sensitive"])
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	permSent := false
	for _, w := range mock.writes {
		if strings.Contains(w.Data, "session/permission") && strings.Contains(w.Data, "perm-001") {
			permSent = true
			break
		}
	}
	if !permSent {
		t.Error("session/permission response was not sent back to OpenCode")
	}
}

func TestBridge_EnrichedStartEvent(t *testing.T) {
	const acpSID = "acp-session-enrich"
	readCount := 0

	mock := &mockTransport{
		startFn: func(_ context.Context, req StartRequest) (*StartResponse, error) {
			if req.KillGraceMs != 3000 {
				t.Errorf("KillGraceMs = %d, want 3000", req.KillGraceMs)
			}
			return &StartResponse{ProcessID: "proc-enrich", AgentVersion: "local-sandbox/1.0"}, nil
		},
		readFn: func(_ context.Context, _ ReadRequest) (*ReadResponse, error) {
			readCount++
			switch readCount {
			case 1:
				return &ReadResponse{Data: sessionNewResponseLine(1, acpSID), NextCursor: 100}, nil
			case 2:
				return &ReadResponse{
					Data:       sessionUpdateLine(acpSID, SessionUpdateParams{Type: UpdateTypeComplete, Summary: "ok"}),
					NextCursor: 200,
				}, nil
			default:
				return nil, nil
			}
		},
	}

	cfg := testConfig()
	cfg.KillGraceMs = 3000
	bridge := NewBridge(mock, cfg)
	emitter := events.NewEmitter("trace-enrich")
	var got []events.RunEvent

	err := bridge.Run(context.Background(), "test", emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(got) < 1 || got[0].Type != "run.started" {
		t.Fatalf("expected run.started first, got %v", eventTypes(got))
	}
	startData := got[0].DataJSON
	if startData["agent_version"] != "local-sandbox/1.0" {
		t.Errorf("agent_version = %v, want %q", startData["agent_version"], "local-sandbox/1.0")
	}
	if startData["command"] == nil {
		t.Error("command should be included in start event")
	}
}

func TestBridge_ProcessExitWithDiagnostics(t *testing.T) {
	const acpSID = "acp-session-diag"
	readCount := 0

	mock := &mockTransport{
		startFn: func(_ context.Context, _ StartRequest) (*StartResponse, error) {
			return &StartResponse{ProcessID: "proc-diag", AgentVersion: "local-sandbox/1.0"}, nil
		},
		readFn: func(_ context.Context, _ ReadRequest) (*ReadResponse, error) {
			readCount++
			switch readCount {
			case 1:
				return &ReadResponse{Data: sessionNewResponseLine(1, acpSID), NextCursor: 100}, nil
			case 2:
				exitCode := 1
				return &ReadResponse{
					NextCursor:   200,
					Exited:       true,
					ExitCode:     &exitCode,
					Stderr:       "fatal error: out of memory",
					ErrorSummary: "fatal error: out of memory",
				}, nil
			default:
				return nil, nil
			}
		},
	}

	bridge := NewBridge(mock, testConfig())
	emitter := events.NewEmitter("trace-diag")
	var got []events.RunEvent

	err := bridge.Run(context.Background(), "run something", emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(got) < 2 || got[1].Type != "run.failed" {
		t.Fatalf("expected run.failed, got %v", eventTypes(got))
	}
	failData := got[1].DataJSON
	if failData["layer"] != "opencode" {
		t.Errorf("layer = %v, want %q", failData["layer"], "opencode")
	}
	if failData["error_summary"] != "fatal error: out of memory" {
		t.Errorf("error_summary = %v, want %q", failData["error_summary"], "fatal error: out of memory")
	}
	if failData["agent_version"] != "local-sandbox/1.0" {
		t.Errorf("agent_version = %v, want %q", failData["agent_version"], "local-sandbox/1.0")
	}
}

// ---------------------------------------------------------------------------
// PR-10: State / Bind / CheckAlive / RunPrompt tests
// ---------------------------------------------------------------------------

func TestBridge_State(t *testing.T) {
	const acpSID = "acp-session-state"
	readCount := 0

	mock := &mockTransport{
		startFn: func(_ context.Context, _ StartRequest) (*StartResponse, error) {
			return &StartResponse{ProcessID: "proc-state", AgentVersion: "v1.0"}, nil
		},
		readFn: func(_ context.Context, _ ReadRequest) (*ReadResponse, error) {
			readCount++
			switch readCount {
			case 1:
				return &ReadResponse{Data: sessionNewResponseLine(1, acpSID), NextCursor: 100}, nil
			case 2:
				return &ReadResponse{
					Data:       sessionUpdateLine(acpSID, SessionUpdateParams{Type: UpdateTypeComplete, Summary: "ok"}),
					NextCursor: 250,
				}, nil
			default:
				return nil, nil
			}
		},
	}

	bridge := NewBridge(mock, testConfig())
	emitter := events.NewEmitter("trace-state")

	err := bridge.Run(context.Background(), "test", emitter, func(ev events.RunEvent) error { return nil })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	state := bridge.State()
	if state.HostProcessID != "proc-state" {
		t.Errorf("HostProcessID = %q, want %q", state.HostProcessID, "proc-state")
	}
	if state.ProtocolSessionID != acpSID {
		t.Errorf("ProtocolSessionID = %q, want %q", state.ProtocolSessionID, acpSID)
	}
	if state.OutputCursor != 250 {
		t.Errorf("OutputCursor = %d, want 250", state.OutputCursor)
	}
	if state.AgentVersion != "v1.0" {
		t.Errorf("AgentVersion = %q, want %q", state.AgentVersion, "v1.0")
	}
}

func TestBridge_BindAndRunPrompt(t *testing.T) {
	const acpSID = "acp-session-reuse"
	readCount := 0

	mock := &mockTransport{
		startFn: func(_ context.Context, _ StartRequest) (*StartResponse, error) {
			t.Fatal("Start should not be called during RunPrompt")
			return nil, nil
		},
		readFn: func(_ context.Context, req ReadRequest) (*ReadResponse, error) {
			readCount++
			switch readCount {
			case 1:
				return &ReadResponse{
					Data:       sessionUpdateLine(acpSID, SessionUpdateParams{Type: UpdateTypeTextDelta, Content: "reused output"}),
					NextCursor: 350,
				}, nil
			case 2:
				return &ReadResponse{
					Data:       sessionUpdateLine(acpSID, SessionUpdateParams{Type: UpdateTypeComplete, Summary: "reuse done"}),
					NextCursor: 400,
				}, nil
			default:
				return nil, nil
			}
		},
	}

	bridge := NewBridge(mock, testConfig())
	bridge.Bind(BridgeState{
		HostProcessID:     "proc-existing",
		ProtocolSessionID: acpSID,
		OutputCursor:      300,
		AgentVersion:      "v1.0",
	})

	emitter := events.NewEmitter("trace-reuse")
	var got []events.RunEvent

	err := bridge.RunPrompt(context.Background(), "second task", emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("RunPrompt: %v", err)
	}

	wantTypes := []string{"run.started", "message.delta", "run.completed"}
	if len(got) != len(wantTypes) {
		t.Fatalf("got %d events %v, want %d", len(got), eventTypes(got), len(wantTypes))
	}
	for i, want := range wantTypes {
		if got[i].Type != want {
			t.Errorf("event[%d].Type = %q, want %q", i, got[i].Type, want)
		}
	}

	// Verify "reused" flag in run.started event
	if got[0].DataJSON["reused"] != true {
		t.Errorf("run.started reused = %v, want true", got[0].DataJSON["reused"])
	}
	if got[0].DataJSON["runtime_session_key"] != "ses-1" {
		t.Errorf("runtime_session_key = %v, want %q", got[0].DataJSON["runtime_session_key"], "ses-1")
	}

	// Verify session/prompt was sent (not session/new)
	mock.mu.Lock()
	promptSent := false
	newSent := false
	for _, w := range mock.writes {
		if strings.Contains(w.Data, "session/prompt") {
			promptSent = true
		}
		if strings.Contains(w.Data, "session/new") {
			newSent = true
		}
	}
	mock.mu.Unlock()
	if !promptSent {
		t.Error("session/prompt was not sent")
	}
	if newSent {
		t.Error("session/new should not be sent during RunPrompt")
	}

	// Verify cursor advanced
	state := bridge.State()
	if state.OutputCursor != 400 {
		t.Errorf("OutputCursor = %d, want 400", state.OutputCursor)
	}
}

func TestBridge_CheckAlive(t *testing.T) {
	t.Run("alive", func(t *testing.T) {
		mock := &mockTransport{
			statusFn: func(_ context.Context, req StatusRequest) (*StatusResponse, error) {
				return &StatusResponse{
					Running:      true,
					StdoutCursor: 500,
				}, nil
			},
		}
		bridge := NewBridge(mock, testConfig())
		bridge.Bind(BridgeState{HostProcessID: "proc-alive", ProtocolSessionID: "ses-alive", OutputCursor: 100})

		if err := bridge.CheckAlive(context.Background()); err != nil {
			t.Fatalf("CheckAlive: %v", err)
		}
		// Cursor should be updated to server-reported position
		if bridge.State().OutputCursor != 500 {
			t.Errorf("OutputCursor = %d, want 500", bridge.State().OutputCursor)
		}
	})

	t.Run("dead", func(t *testing.T) {
		mock := &mockTransport{
			statusFn: func(_ context.Context, _ StatusRequest) (*StatusResponse, error) {
				return &StatusResponse{Running: false, Exited: true}, nil
			},
		}
		bridge := NewBridge(mock, testConfig())
		bridge.Bind(BridgeState{HostProcessID: "proc-dead", ProtocolSessionID: "ses-dead"})

		if err := bridge.CheckAlive(context.Background()); err == nil {
			t.Fatal("CheckAlive should fail for dead process")
		}
	})

	t.Run("no_process", func(t *testing.T) {
		mock := &mockTransport{}
		bridge := NewBridge(mock, testConfig())
		if err := bridge.CheckAlive(context.Background()); err == nil {
			t.Fatal("CheckAlive should fail with no process bound")
		}
	})
}

func TestBridge_RunPromptRequiresState(t *testing.T) {
	mock := &mockTransport{}
	bridge := NewBridge(mock, testConfig())
	emitter := events.NewEmitter("trace-noop")

	err := bridge.RunPrompt(context.Background(), "test", emitter, func(ev events.RunEvent) error { return nil })
	if err == nil {
		t.Fatal("RunPrompt should fail without Bind/Run")
	}
	if !strings.Contains(err.Error(), "no process bound") {
		t.Errorf("error = %q, want 'no process bound'", err.Error())
	}
}

func TestBridge_MultiplePrompts(t *testing.T) {
	const acpSID = "acp-session-multi"
	readCount := 0

	mock := &mockTransport{
		startFn: func(_ context.Context, _ StartRequest) (*StartResponse, error) {
			return &StartResponse{ProcessID: "proc-multi", AgentVersion: "v1.0"}, nil
		},
		readFn: func(_ context.Context, req ReadRequest) (*ReadResponse, error) {
			readCount++
			switch readCount {
			case 1: // session/new response
				return &ReadResponse{Data: sessionNewResponseLine(1, acpSID), NextCursor: 100}, nil
			case 2: // first prompt complete
				return &ReadResponse{
					Data:       sessionUpdateLine(acpSID, SessionUpdateParams{Type: UpdateTypeComplete, Summary: "first done"}),
					NextCursor: 200,
				}, nil
			case 3: // second prompt output
				return &ReadResponse{
					Data:       sessionUpdateLine(acpSID, SessionUpdateParams{Type: UpdateTypeTextDelta, Content: "second output"}),
					NextCursor: 300,
				}, nil
			case 4: // second prompt complete
				return &ReadResponse{
					Data:       sessionUpdateLine(acpSID, SessionUpdateParams{Type: UpdateTypeComplete, Summary: "second done"}),
					NextCursor: 400,
				}, nil
			default:
				return nil, nil
			}
		},
	}

	bridge := NewBridge(mock, testConfig())
	emitter := events.NewEmitter("trace-multi")

	// First prompt via Run()
	var firstEvents []events.RunEvent
	err := bridge.Run(context.Background(), "first task", emitter, func(ev events.RunEvent) error {
		firstEvents = append(firstEvents, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if len(firstEvents) != 2 || firstEvents[1].Type != "run.completed" {
		t.Fatalf("first run events: %v", eventTypes(firstEvents))
	}

	// Second prompt via RunPrompt() (reuse)
	var secondEvents []events.RunEvent
	err = bridge.RunPrompt(context.Background(), "second task", emitter, func(ev events.RunEvent) error {
		secondEvents = append(secondEvents, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("second RunPrompt: %v", err)
	}
	if len(secondEvents) != 3 {
		t.Fatalf("second run events: %v", eventTypes(secondEvents))
	}
	wantTypes := []string{"run.started", "message.delta", "run.completed"}
	for i, want := range wantTypes {
		if secondEvents[i].Type != want {
			t.Errorf("second event[%d].Type = %q, want %q", i, secondEvents[i].Type, want)
		}
	}

	// Verify cursor reflects all reads
	state := bridge.State()
	if state.OutputCursor != 400 {
		t.Errorf("final OutputCursor = %d, want 400", state.OutputCursor)
	}
	if state.HostProcessID != "proc-multi" {
		t.Errorf("HostProcessID = %q, want %q", state.HostProcessID, "proc-multi")
	}
}
