package shell

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"testing"

	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
	"arkloop/services/shared/objectstore"
)

func TestManagerExecCommand_IdempotentReuse(t *testing.T) {
	agent := &fakeAgent{}
	pool := &fakePool{agent: agent}
	mgr := session.NewManager(session.ManagerConfig{MaxSessions: 10, Pool: pool, MaxLifetimeSeconds: 3600})
	shellMgr := NewManager(mgr, nil, nil, nil, nil, logging.NewJSONLogger("test", nil), Config{})

	for range 2 {
		resp, err := shellMgr.ExecCommand(context.Background(), ExecCommandRequest{SessionID: "sess-1", Tier: "lite", OrgID: "org-a", Command: "pwd"})
		if err != nil {
			t.Fatalf("exec_command failed: %v", err)
		}
		if resp.Status != StatusIdle {
			t.Fatalf("expected idle, got %s", resp.Status)
		}
	}

	if pool.acquireCount != 1 {
		t.Fatalf("expected acquire once, got %d", pool.acquireCount)
	}
}

func TestManagerExecCommand_Busy(t *testing.T) {
	agent := &fakeAgent{actionHandler: func(req AgentRequest) AgentResponse {
		if req.Action == "exec_command" {
			return AgentResponse{Action: req.Action, Code: CodeSessionBusy, Error: "shell session is busy"}
		}
		return idleShellResponse(req)
	}}
	pool := &fakePool{agent: agent}
	mgr := session.NewManager(session.ManagerConfig{MaxSessions: 10, Pool: pool, MaxLifetimeSeconds: 3600})
	shellMgr := NewManager(mgr, nil, nil, nil, nil, logging.NewJSONLogger("test", nil), Config{})

	_, err := shellMgr.ExecCommand(context.Background(), ExecCommandRequest{SessionID: "sess-1", Tier: "lite", OrgID: "org-a", Command: "sleep 1"})
	if err == nil {
		t.Fatal("expected busy error")
	}
	shellErr, ok := err.(*Error)
	if !ok || shellErr.Code != CodeSessionBusy {
		t.Fatalf("expected busy shell error, got %#v", err)
	}
}

func TestManagerOrgMismatch(t *testing.T) {
	agent := &fakeAgent{}
	pool := &fakePool{agent: agent}
	mgr := session.NewManager(session.ManagerConfig{MaxSessions: 10, Pool: pool, MaxLifetimeSeconds: 3600})
	shellMgr := NewManager(mgr, nil, nil, nil, nil, logging.NewJSONLogger("test", nil), Config{})

	if _, err := shellMgr.ExecCommand(context.Background(), ExecCommandRequest{SessionID: "sess-1", Tier: "lite", OrgID: "org-a", Command: "pwd"}); err != nil {
		t.Fatalf("exec_command failed: %v", err)
	}
	_, err := shellMgr.WriteStdin(context.Background(), WriteStdinRequest{SessionID: "sess-1", OrgID: "org-b"})
	if err == nil {
		t.Fatal("expected org mismatch")
	}
	shellErr, ok := err.(*Error)
	if !ok || shellErr.Code != CodeOrgMismatch {
		t.Fatalf("expected org mismatch shell error, got %#v", err)
	}
}

func TestManagerDebugSnapshot_ProxyAgentResponse(t *testing.T) {
	agent := &fakeAgent{actionHandler: func(req AgentRequest) AgentResponse {
		switch req.Action {
		case "shell_debug_snapshot":
			code := 7
			return AgentResponse{Action: req.Action, Debug: &AgentDebugResponse{
				Status:                 StatusIdle,
				Cwd:                    "/workspace/demo",
				Running:                false,
				TimedOut:               true,
				ExitCode:               &code,
				PendingOutputBytes:     12,
				PendingOutputTruncated: true,
				Transcript:             DebugTranscript{Text: "headtail", Truncated: true, OmittedBytes: 99},
				Tail:                   "tail",
			}}
		default:
			return idleShellResponse(req)
		}
	}}
	pool := &fakePool{agent: agent}
	mgr := session.NewManager(session.ManagerConfig{MaxSessions: 10, Pool: pool, MaxLifetimeSeconds: 3600})
	shellMgr := NewManager(mgr, nil, nil, nil, nil, logging.NewJSONLogger("test", nil), Config{})

	if _, err := shellMgr.ExecCommand(context.Background(), ExecCommandRequest{SessionID: "sess-1", Tier: "lite", OrgID: "org-a", Command: "pwd"}); err != nil {
		t.Fatalf("exec_command failed: %v", err)
	}

	resp, err := shellMgr.DebugSnapshot(context.Background(), "sess-1", "org-a")
	if err != nil {
		t.Fatalf("debug snapshot failed: %v", err)
	}
	if resp.SessionID != "sess-1" {
		t.Fatalf("unexpected session id: %#v", resp)
	}
	if resp.Cwd != "/workspace/demo" || !resp.TimedOut || resp.ExitCode == nil || *resp.ExitCode != 7 {
		t.Fatalf("unexpected debug response: %#v", resp)
	}
	if !resp.PendingOutputTruncated || resp.PendingOutputBytes != 12 {
		t.Fatalf("unexpected pending state: %#v", resp)
	}
	if !resp.Transcript.Truncated || resp.Transcript.OmittedBytes != 99 || resp.Tail != "tail" {
		t.Fatalf("unexpected transcript response: %#v", resp)
	}
}

func TestManagerClose_ReclaimsComputeSession(t *testing.T) {
	agent := &fakeAgent{}
	pool := &fakePool{agent: agent}
	mgr := session.NewManager(session.ManagerConfig{MaxSessions: 10, Pool: pool, MaxLifetimeSeconds: 3600})
	shellMgr := NewManager(mgr, nil, nil, nil, nil, logging.NewJSONLogger("test", nil), Config{})

	if _, err := shellMgr.ExecCommand(context.Background(), ExecCommandRequest{SessionID: "sess-1", Tier: "lite", OrgID: "org-a", Command: "pwd"}); err != nil {
		t.Fatalf("exec_command failed: %v", err)
	}
	if err := shellMgr.Close(context.Background(), "sess-1", "org-a"); err != nil {
		t.Fatalf("close failed: %v", err)
	}
	if pool.destroyCount != 1 {
		t.Fatalf("expected destroy once, got %d", pool.destroyCount)
	}
}

func TestManagerExecCommand_RecreatesSessionAfterClose(t *testing.T) {
	agent := &fakeAgent{}
	pool := &fakePool{agent: agent}
	mgr := session.NewManager(session.ManagerConfig{MaxSessions: 10, Pool: pool, MaxLifetimeSeconds: 3600})
	shellMgr := NewManager(mgr, nil, nil, nil, nil, logging.NewJSONLogger("test", nil), Config{})

	for attempt := 0; attempt < 2; attempt++ {
		resp, err := shellMgr.ExecCommand(context.Background(), ExecCommandRequest{SessionID: "sess-1", Tier: "lite", OrgID: "org-a", Command: "pwd"})
		if err != nil {
			t.Fatalf("exec_command attempt %d failed: %v", attempt+1, err)
		}
		if resp.Status != StatusIdle {
			t.Fatalf("expected idle response, got %#v", resp)
		}
		if attempt == 0 {
			if err := shellMgr.Close(context.Background(), "sess-1", "org-a"); err != nil {
				t.Fatalf("close failed: %v", err)
			}
		}
	}

	if pool.acquireCount != 2 {
		t.Fatalf("expected session to be reacquired after close, got %d", pool.acquireCount)
	}
}

func TestManagerClose_CaptureStateFailureKeepsComputeSession(t *testing.T) {
	agent := &fakeAgent{actionHandler: func(req AgentRequest) AgentResponse {
		if req.Action == "shell_capture_state" {
			return AgentResponse{Action: req.Action, Error: "capture state failed"}
		}
		return idleShellResponse(req)
	}}
	pool := &fakePool{agent: agent}
	state := newMemoryStateStore()
	mgr := session.NewManager(session.ManagerConfig{MaxSessions: 10, Pool: pool, MaxLifetimeSeconds: 3600})
	shellMgr := NewManager(mgr, nil, state, nil, nil, logging.NewJSONLogger("test", nil), Config{})

	if _, err := shellMgr.ExecCommand(context.Background(), ExecCommandRequest{SessionID: "sess-1", Tier: "lite", OrgID: "org-a", Command: "pwd"}); err != nil {
		t.Fatalf("exec_command failed: %v", err)
	}
	if err := shellMgr.Close(context.Background(), "sess-1", "org-a"); err == nil {
		t.Fatal("expected close failure")
	}
	if pool.destroyCount != 0 {
		t.Fatalf("expected compute session to stay alive, got destroy count %d", pool.destroyCount)
	}
}

func TestManagerRestoreFromStateOnReopen(t *testing.T) {
	agent := &fakeAgent{actionHandler: func(req AgentRequest) AgentResponse {
		switch req.Action {
		case "shell_capture_state":
			return AgentResponse{Action: req.Action, State: &AgentStateResponse{Cwd: "/workspace/demo", Env: map[string]string{"FOO": "bar"}}}
		case "exec_command":
			code := 0
			return AgentResponse{Action: req.Action, Session: &AgentSessionResponse{Status: StatusIdle, Cwd: "/workspace/demo", ExitCode: &code}}
		default:
			return idleShellResponse(req)
		}
	}}
	pool := &fakePool{agent: agent}
	state := newMemoryStateStore()
	mgr := session.NewManager(session.ManagerConfig{MaxSessions: 10, Pool: pool, MaxLifetimeSeconds: 3600})
	shellMgr := NewManager(mgr, nil, state, nil, nil, logging.NewJSONLogger("test", nil), Config{})

	if _, err := shellMgr.ExecCommand(context.Background(), ExecCommandRequest{SessionID: "sess-1", Tier: "lite", OrgID: "org-a", Command: "echo ok"}); err != nil {
		t.Fatalf("exec_command failed: %v", err)
	}
	entry, err := shellMgr.getExistingEntry("sess-1", "org-a")
	if err != nil {
		t.Fatalf("get entry failed: %v", err)
	}
	entry.mu.Lock()
	entry.artifactSeen = map[string]artifactVersion{"report.txt": {Size: 3, Data: base64.StdEncoding.EncodeToString([]byte("abc"))}}
	entry.mu.Unlock()
	if err := shellMgr.Close(context.Background(), "sess-1", "org-a"); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	if _, err := shellMgr.ExecCommand(context.Background(), ExecCommandRequest{SessionID: "sess-1", Tier: "lite", OrgID: "org-a", Command: "echo again"}); err != nil {
		t.Fatalf("reopen exec_command failed: %v", err)
	}
	if agent.lastExecCwd != "/workspace/demo" {
		t.Fatalf("unexpected restored cwd: %s", agent.lastExecCwd)
	}
	if agent.lastExecEnv["FOO"] != "bar" {
		t.Fatalf("unexpected restored env: %#v", agent.lastExecEnv)
	}
	restoredEntry, err := shellMgr.getExistingEntry("sess-1", "org-a")
	if err != nil {
		t.Fatalf("restored entry missing: %v", err)
	}
	restoredEntry.mu.Lock()
	defer restoredEntry.mu.Unlock()
	if restoredEntry.commandSeq != 2 || restoredEntry.uploadedSeq != 2 {
		t.Fatalf("unexpected restored seq: command=%d uploaded=%d", restoredEntry.commandSeq, restoredEntry.uploadedSeq)
	}
	if restoredEntry.artifactSeen["report.txt"].SHA256 == "" {
		t.Fatalf("unexpected restored artifacts: %#v", restoredEntry.artifactSeen)
	}
	if restoredEntry.artifactSeen["report.txt"].Data != "" {
		t.Fatalf("legacy artifact data should be cleared: %#v", restoredEntry.artifactSeen)
	}
}

func TestManagerArtifactsUseDefaultSessionKeyShape(t *testing.T) {
	agent := &fakeAgent{
		actionHandler: completedShellAction,
		fetchArtifactsHandler: func() (*session.FetchArtifactsResult, error) {
			return &session.FetchArtifactsResult{Artifacts: []session.ArtifactEntry{{Filename: "report.txt", MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("hello"))}}}, nil
		},
	}
	store := newFakeArtifactStore()
	pool := &fakePool{agent: agent}
	mgr := session.NewManager(session.ManagerConfig{MaxSessions: 10, Pool: pool, MaxLifetimeSeconds: 3600})
	shellMgr := NewManager(mgr, store, nil, nil, nil, logging.NewJSONLogger("test", nil), Config{})

	resp, err := shellMgr.ExecCommand(context.Background(), ExecCommandRequest{SessionID: "run-1/shell/default", Tier: "lite", OrgID: "org-a", Command: "echo ok"})
	if err != nil {
		t.Fatalf("exec_command failed: %v", err)
	}
	if len(resp.Artifacts) != 1 {
		t.Fatalf("expected one artifact, got %#v", resp.Artifacts)
	}
	if got := resp.Artifacts[0].Key; got != "org-a/run-1/shell/default/1/report.txt" {
		t.Fatalf("unexpected artifact key: %s", got)
	}
	if len(store.puts) != 1 {
		t.Fatalf("expected one upload, got %d", len(store.puts))
	}
}

func TestManagerArtifactsSkipUnchangedContent(t *testing.T) {
	artifact := session.ArtifactEntry{Filename: "report.txt", MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("same"))}
	agent := &fakeAgent{
		actionHandler: completedShellAction,
		fetchArtifactsHandler: func() (*session.FetchArtifactsResult, error) {
			return &session.FetchArtifactsResult{Artifacts: []session.ArtifactEntry{artifact}}, nil
		},
	}
	store := newFakeArtifactStore()
	pool := &fakePool{agent: agent}
	mgr := session.NewManager(session.ManagerConfig{MaxSessions: 10, Pool: pool, MaxLifetimeSeconds: 3600})
	shellMgr := NewManager(mgr, store, nil, nil, nil, logging.NewJSONLogger("test", nil), Config{})

	first, err := shellMgr.ExecCommand(context.Background(), ExecCommandRequest{SessionID: "run-2/shell/default", Tier: "lite", OrgID: "org-a", Command: "echo one"})
	if err != nil {
		t.Fatalf("first exec_command failed: %v", err)
	}
	second, err := shellMgr.ExecCommand(context.Background(), ExecCommandRequest{SessionID: "run-2/shell/default", OrgID: "org-a", Command: "echo two"})
	if err != nil {
		t.Fatalf("second exec_command failed: %v", err)
	}
	if len(first.Artifacts) != 1 {
		t.Fatalf("expected first exec to upload artifact, got %#v", first.Artifacts)
	}
	if len(second.Artifacts) != 0 {
		t.Fatalf("expected no new artifacts, got %#v", second.Artifacts)
	}
	if len(store.puts) != 1 {
		t.Fatalf("expected one upload, got %d", len(store.puts))
	}
}

func TestManagerArtifactsUploadChangedContentWithNewSequence(t *testing.T) {
	call := 0
	agent := &fakeAgent{
		actionHandler: completedShellAction,
		fetchArtifactsHandler: func() (*session.FetchArtifactsResult, error) {
			call++
			content := "first"
			if call > 1 {
				content = "second"
			}
			return &session.FetchArtifactsResult{Artifacts: []session.ArtifactEntry{{Filename: "report.txt", MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte(content))}}}, nil
		},
	}
	store := newFakeArtifactStore()
	pool := &fakePool{agent: agent}
	mgr := session.NewManager(session.ManagerConfig{MaxSessions: 10, Pool: pool, MaxLifetimeSeconds: 3600})
	shellMgr := NewManager(mgr, store, nil, nil, nil, logging.NewJSONLogger("test", nil), Config{})

	if _, err := shellMgr.ExecCommand(context.Background(), ExecCommandRequest{SessionID: "run-3/shell/default", Tier: "lite", OrgID: "org-a", Command: "echo one"}); err != nil {
		t.Fatalf("first exec_command failed: %v", err)
	}
	if _, err := shellMgr.ExecCommand(context.Background(), ExecCommandRequest{SessionID: "run-3/shell/default", OrgID: "org-a", Command: "echo two"}); err != nil {
		t.Fatalf("second exec_command failed: %v", err)
	}
	if len(store.puts) != 2 {
		t.Fatalf("expected two uploads, got %d", len(store.puts))
	}
	if store.puts[0].Key != "org-a/run-3/shell/default/1/report.txt" {
		t.Fatalf("unexpected first key: %s", store.puts[0].Key)
	}
	if store.puts[1].Key != "org-a/run-3/shell/default/2/report.txt" {
		t.Fatalf("unexpected second key: %s", store.puts[1].Key)
	}
}

func TestManagerArtifactsRetryFailedUploadsOnPoll(t *testing.T) {
	agent := &fakeAgent{
		actionHandler: completedShellAction,
		fetchArtifactsHandler: func() (*session.FetchArtifactsResult, error) {
			return &session.FetchArtifactsResult{Artifacts: []session.ArtifactEntry{
				{Filename: "a.txt", MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("aaa"))},
				{Filename: "b.txt", MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("bbb"))},
			}}, nil
		},
	}
	store := newFakeArtifactStore()
	store.failKeys["org-a/run-4/shell/default/1/b.txt"] = 1
	pool := &fakePool{agent: agent}
	mgr := session.NewManager(session.ManagerConfig{MaxSessions: 10, Pool: pool, MaxLifetimeSeconds: 3600})
	shellMgr := NewManager(mgr, store, nil, nil, nil, logging.NewJSONLogger("test", nil), Config{})

	first, err := shellMgr.ExecCommand(context.Background(), ExecCommandRequest{SessionID: "run-4/shell/default", Tier: "lite", OrgID: "org-a", Command: "echo one"})
	if err != nil {
		t.Fatalf("exec_command failed: %v", err)
	}
	if len(first.Artifacts) != 1 || first.Artifacts[0].Filename != "a.txt" {
		t.Fatalf("unexpected first artifacts: %#v", first.Artifacts)
	}
	entry, err := shellMgr.getExistingEntry("run-4/shell/default", "org-a")
	if err != nil {
		t.Fatalf("get entry failed: %v", err)
	}
	entry.mu.Lock()
	if entry.uploadedSeq != 0 {
		entry.mu.Unlock()
		t.Fatalf("expected uploaded seq to stay pending, got %d", entry.uploadedSeq)
	}
	entry.mu.Unlock()

	second, err := shellMgr.WriteStdin(context.Background(), WriteStdinRequest{SessionID: "run-4/shell/default", OrgID: "org-a"})
	if err != nil {
		t.Fatalf("write_stdin failed: %v", err)
	}
	if len(second.Artifacts) != 1 || second.Artifacts[0].Filename != "b.txt" {
		t.Fatalf("unexpected retried artifacts: %#v", second.Artifacts)
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.uploadedSeq != 1 {
		t.Fatalf("expected uploaded seq 1, got %d", entry.uploadedSeq)
	}
	if len(store.puts) != 2 {
		t.Fatalf("expected two successful uploads, got %d", len(store.puts))
	}
	if store.countKey("org-a/run-4/shell/default/1/a.txt") != 1 {
		t.Fatalf("artifact a.txt should upload once, puts=%#v", store.puts)
	}
	if store.countKey("org-a/run-4/shell/default/1/b.txt") != 1 {
		t.Fatalf("artifact b.txt should upload once successfully, puts=%#v", store.puts)
	}
}

func TestManagerRestoreStateSkipsRawArtifactDataAndRestoresSequence(t *testing.T) {
	call := 0
	agent := &fakeAgent{
		actionHandler: func(req AgentRequest) AgentResponse {
			switch req.Action {
			case "shell_capture_state":
				return AgentResponse{Action: req.Action, State: &AgentStateResponse{Cwd: "/workspace/demo", Env: map[string]string{"FOO": "bar"}}}
			default:
				return completedShellAction(req)
			}
		},
		fetchArtifactsHandler: func() (*session.FetchArtifactsResult, error) {
			call++
			content := "first"
			if call > 1 {
				content = "second"
			}
			return &session.FetchArtifactsResult{Artifacts: []session.ArtifactEntry{{Filename: "report.txt", MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte(content))}}}, nil
		},
	}
	store := newFakeArtifactStore()
	state := newMemoryStateStore()
	pool := &fakePool{agent: agent}
	mgr := session.NewManager(session.ManagerConfig{MaxSessions: 10, Pool: pool, MaxLifetimeSeconds: 3600})
	shellMgr := NewManager(mgr, store, state, nil, nil, logging.NewJSONLogger("test", nil), Config{})

	if _, err := shellMgr.ExecCommand(context.Background(), ExecCommandRequest{SessionID: "run-5/shell/default", Tier: "lite", OrgID: "org-a", Command: "echo one"}); err != nil {
		t.Fatalf("first exec_command failed: %v", err)
	}
	if err := shellMgr.Close(context.Background(), "run-5/shell/default", "org-a"); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	restoreStateBytes, err := state.findByPrefix("sessions/run-5/shell/default/restore/")
	if err != nil {
		t.Fatalf("load restore state failed: %v", err)
	}
	if jsonContains(restoreStateBytes, "data") {
		t.Fatalf("restore state should not persist raw artifact data: %s", string(restoreStateBytes))
	}
	if !jsonContains(restoreStateBytes, "sha256") {
		t.Fatalf("restore state should persist artifact hash: %s", string(restoreStateBytes))
	}

	if _, err := shellMgr.ExecCommand(context.Background(), ExecCommandRequest{SessionID: "run-5/shell/default", Tier: "lite", OrgID: "org-a", Command: "echo two"}); err != nil {
		t.Fatalf("second exec_command failed: %v", err)
	}
	if len(store.puts) != 2 {
		t.Fatalf("expected two uploads, got %d", len(store.puts))
	}
	if store.puts[1].Key != "org-a/run-5/shell/default/2/report.txt" {
		t.Fatalf("unexpected restored sequence key: %s", store.puts[1].Key)
	}
}

func TestManagerReclaimIgnoresCaptureStateFailure(t *testing.T) {
	agent := &fakeAgent{actionHandler: func(req AgentRequest) AgentResponse {
		if req.Action == "shell_capture_state" {
			return AgentResponse{Action: req.Action, Error: "capture state failed"}
		}
		return idleShellResponse(req)
	}}
	pool := &fakePool{agent: agent}
	state := newMemoryStateStore()
	mgr := session.NewManager(session.ManagerConfig{MaxSessions: 10, Pool: pool, MaxLifetimeSeconds: 3600})
	shellMgr := NewManager(mgr, nil, state, nil, nil, logging.NewJSONLogger("test", nil), Config{})

	if _, err := shellMgr.ExecCommand(context.Background(), ExecCommandRequest{SessionID: "sess-1", Tier: "lite", OrgID: "org-a", Command: "pwd"}); err != nil {
		t.Fatalf("exec_command failed: %v", err)
	}
	if err := mgr.DeleteWithOptions(context.Background(), "sess-1", "org-a", session.DeleteOptions{Reason: session.DeleteReasonIdleTimeout, IgnoreHookError: true}); err != nil {
		t.Fatalf("delete with hook ignore failed: %v", err)
	}
	if pool.destroyCount != 1 {
		t.Fatalf("expected destroy once, got %d", pool.destroyCount)
	}
}

type fakePool struct {
	mu           sync.Mutex
	agent        *fakeAgent
	acquireCount int
	destroyCount int
}

func (p *fakePool) Acquire(_ context.Context, tier string) (*session.Session, *os.Process, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.acquireCount++
	return &session.Session{
		Tier:      tier,
		SocketDir: "fake-socket",
		Dial:      p.agent.Dial,
	}, nil, nil
}

func (p *fakePool) DestroyVM(_ *os.Process, _ string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.destroyCount++
}

func (p *fakePool) Ready() bool              { return true }
func (p *fakePool) Stats() session.PoolStats { return session.PoolStats{} }
func (p *fakePool) Drain(_ context.Context)  {}

type fakeAgent struct {
	mu                    sync.Mutex
	actionHandler         func(req AgentRequest) AgentResponse
	fetchArtifactsHandler func() (*session.FetchArtifactsResult, error)
	execHandler           func(job session.ExecJob) session.ExecResult
	lastExecEnv           map[string]string
	lastExecCwd           string
}

func (a *fakeAgent) Dial(_ context.Context) (net.Conn, error) {
	client, server := net.Pipe()
	go func() {
		defer server.Close()
		var envelope map[string]json.RawMessage
		if err := json.NewDecoder(server).Decode(&envelope); err != nil {
			return
		}
		if _, ok := envelope["action"]; ok {
			var action struct {
				Action string `json:"action"`
			}
			if err := json.Unmarshal(mustJSON(tolerateEnvelope(envelope)), &action); err != nil {
				return
			}
			if action.Action == "fetch_artifacts" {
				result, fetchErr := a.handleFetchArtifacts()
				resp := map[string]any{"action": action.Action}
				if fetchErr != nil {
					resp["error"] = fetchErr.Error()
				} else {
					resp["artifacts"] = result
				}
				_ = json.NewEncoder(server).Encode(resp)
				return
			}
			var actionReq AgentRequest
			if err := json.Unmarshal(mustJSON(tolerateEnvelope(envelope)), &actionReq); err != nil {
				return
			}
			_ = json.NewEncoder(server).Encode(a.handleAction(actionReq))
			return
		}
		if _, ok := envelope["language"]; ok {
			var execReq session.ExecJob
			if err := json.Unmarshal(mustJSON(tolerateEnvelope(envelope)), &execReq); err != nil {
				return
			}
			_ = json.NewEncoder(server).Encode(a.handleExec(execReq))
		}
	}()
	return client, nil
}

func (a *fakeAgent) handleAction(req AgentRequest) AgentResponse {
	a.mu.Lock()
	if req.Action == "exec_command" && req.ExecCommand != nil {
		a.lastExecCwd = req.ExecCommand.Cwd
		a.lastExecEnv = cloneMap(req.ExecCommand.Env)
	}
	custom := a.actionHandler
	a.mu.Unlock()
	if custom != nil {
		return custom(req)
	}
	if req.Action == "shell_debug_snapshot" {
		return debugShellResponse(req)
	}
	return idleShellResponse(req)
}

func (a *fakeAgent) handleExec(job session.ExecJob) session.ExecResult {
	a.mu.Lock()
	custom := a.execHandler
	a.mu.Unlock()
	if custom != nil {
		return custom(job)
	}
	return session.ExecResult{ExitCode: 0}
}

func (a *fakeAgent) handleFetchArtifacts() (*session.FetchArtifactsResult, error) {
	a.mu.Lock()
	handler := a.fetchArtifactsHandler
	a.mu.Unlock()
	if handler != nil {
		return handler()
	}
	return &session.FetchArtifactsResult{Artifacts: []session.ArtifactEntry{}}, nil
}

func completedShellAction(req AgentRequest) AgentResponse {
	code := 0
	cwd := requestCwd(req)
	return AgentResponse{Action: req.Action, Session: &AgentSessionResponse{Status: StatusIdle, Cwd: cwd, ExitCode: &code}}
}

func idleShellResponse(req AgentRequest) AgentResponse {
	return AgentResponse{Action: req.Action, Session: &AgentSessionResponse{Status: StatusIdle, Cwd: requestCwd(req)}}
}

func debugShellResponse(req AgentRequest) AgentResponse {
	return AgentResponse{Action: req.Action, Debug: &AgentDebugResponse{
		Status:                 StatusIdle,
		Cwd:                    requestCwd(req),
		PendingOutputBytes:     0,
		PendingOutputTruncated: false,
		Transcript:             DebugTranscript{},
		Tail:                   "",
	}}
}

func requestCwd(req AgentRequest) string {
	if req.ExecCommand != nil && req.ExecCommand.Cwd != "" {
		return req.ExecCommand.Cwd
	}
	return "/workspace"
}

func cloneMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return map[string]string{}
	}
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func tolerateEnvelope(envelope map[string]json.RawMessage) map[string]json.RawMessage {
	return envelope
}

func mustJSON(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}

type fakeArtifactStore struct {
	mu       sync.Mutex
	puts     []artifactPut
	failKeys map[string]int
}

type artifactPut struct {
	Key         string
	Data        []byte
	ContentType string
}

func newFakeArtifactStore() *fakeArtifactStore {
	return &fakeArtifactStore{failKeys: make(map[string]int)}
}

func (s *fakeArtifactStore) PutObject(_ context.Context, key string, data []byte, options objectstore.PutOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if remaining := s.failKeys[key]; remaining > 0 {
		s.failKeys[key] = remaining - 1
		return errors.New("upload failed")
	}
	s.puts = append(s.puts, artifactPut{Key: key, Data: append([]byte(nil), data...), ContentType: options.ContentType})
	return nil
}

func (s *fakeArtifactStore) countKey(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, put := range s.puts {
		if put.Key == key {
			count++
		}
	}
	return count
}

func jsonContains(data []byte, key string) bool {
	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		return false
	}
	return containsJSONKey(payload, key)
}

func containsJSONKey(value any, key string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if _, ok := typed[key]; ok {
			return true
		}
		for _, nested := range typed {
			if containsJSONKey(nested, key) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if containsJSONKey(nested, key) {
				return true
			}
		}
	}
	return false
}

type memoryStateStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMemoryStateStore() *memoryStateStore {
	return &memoryStateStore{data: make(map[string][]byte)}
}

func (s *memoryStateStore) Put(_ context.Context, key string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = append([]byte(nil), data...)
	return nil
}

func (s *memoryStateStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.data[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}

func (s *memoryStateStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func (s *memoryStateStore) findByPrefix(prefix string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, data := range s.data {
		if strings.HasPrefix(key, prefix) {
			return append([]byte(nil), data...), nil
		}
	}
	return nil, os.ErrNotExist
}

func TestManagerExecCommand_AttachOrRestoreRequiresLiveOrRestoreState(t *testing.T) {
	agent := &fakeAgent{}
	pool := &fakePool{agent: agent}
	mgr := session.NewManager(session.ManagerConfig{MaxSessions: 10, Pool: pool, MaxLifetimeSeconds: 3600})
	shellMgr := NewManager(mgr, nil, newMemoryStateStore(), nil, nil, logging.NewJSONLogger("test", nil), Config{})

	_, err := shellMgr.ExecCommand(context.Background(), ExecCommandRequest{
		SessionID: "sess-strict",
		OpenMode:  OpenModeAttachOrRestore,
		Tier:      "lite",
		OrgID:     "org-a",
		Command:   "pwd",
	})
	if err == nil {
		t.Fatal("expected not found")
	}
	shellErr, ok := err.(*Error)
	if !ok || shellErr.Code != CodeSessionNotFound {
		t.Fatalf("expected session not found, got %#v", err)
	}
}

func TestManagerExecCommand_AttachOrRestoreRestoresState(t *testing.T) {
	agent := &fakeAgent{actionHandler: func(req AgentRequest) AgentResponse {
		switch req.Action {
		case "shell_capture_state":
			return AgentResponse{Action: req.Action, State: &AgentStateResponse{Cwd: "/workspace/demo", Env: map[string]string{"FOO": "bar"}}}
		default:
			return completedShellAction(req)
		}
	}}
	pool := &fakePool{agent: agent}
	state := newMemoryStateStore()
	mgr := session.NewManager(session.ManagerConfig{MaxSessions: 10, Pool: pool, MaxLifetimeSeconds: 3600})
	shellMgr := NewManager(mgr, nil, state, nil, nil, logging.NewJSONLogger("test", nil), Config{})

	if _, err := shellMgr.ExecCommand(context.Background(), ExecCommandRequest{SessionID: "sess-restore", Tier: "lite", OrgID: "org-a", Command: "echo one"}); err != nil {
		t.Fatalf("seed exec_command failed: %v", err)
	}
	if err := shellMgr.Close(context.Background(), "sess-restore", "org-a"); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	resp, err := shellMgr.ExecCommand(context.Background(), ExecCommandRequest{
		SessionID: "sess-restore",
		OpenMode:  OpenModeAttachOrRestore,
		Tier:      "lite",
		OrgID:     "org-a",
		Command:   "echo two",
	})
	if err != nil {
		t.Fatalf("restore exec_command failed: %v", err)
	}
	if !resp.Restored {
		t.Fatalf("expected restored response, got %#v", resp)
	}
	if agent.lastExecCwd != "/workspace/demo" {
		t.Fatalf("unexpected restored cwd: %s", agent.lastExecCwd)
	}
	if agent.lastExecEnv["FOO"] != "bar" {
		t.Fatalf("unexpected restored env: %#v", agent.lastExecEnv)
	}
}
