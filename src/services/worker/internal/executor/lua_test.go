package executor

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/subagentctl"
	"arkloop/services/worker/internal/tools"
	"github.com/google/uuid"
)

type stubSubAgentControl struct {
	spawn     func(ctx context.Context, req subagentctl.SpawnRequest) (subagentctl.StatusSnapshot, error)
	sendInput func(ctx context.Context, req subagentctl.SendInputRequest) (subagentctl.StatusSnapshot, error)
	wait      func(ctx context.Context, req subagentctl.WaitRequest) (subagentctl.StatusSnapshot, error)
	resume    func(ctx context.Context, req subagentctl.ResumeRequest) (subagentctl.StatusSnapshot, error)
	close     func(ctx context.Context, req subagentctl.CloseRequest) (subagentctl.StatusSnapshot, error)
	interrupt func(ctx context.Context, req subagentctl.InterruptRequest) (subagentctl.StatusSnapshot, error)
	getStatus func(ctx context.Context, subAgentID uuid.UUID) (subagentctl.StatusSnapshot, error)
	list      func(ctx context.Context) ([]subagentctl.StatusSnapshot, error)
}

func (s stubSubAgentControl) Spawn(ctx context.Context, req subagentctl.SpawnRequest) (subagentctl.StatusSnapshot, error) {
	if s.spawn == nil {
		return subagentctl.StatusSnapshot{}, errors.New("spawn not implemented")
	}
	return s.spawn(ctx, req)
}

func (s stubSubAgentControl) SendInput(ctx context.Context, req subagentctl.SendInputRequest) (subagentctl.StatusSnapshot, error) {
	if s.sendInput == nil {
		return subagentctl.StatusSnapshot{}, errors.New("send_input not implemented")
	}
	return s.sendInput(ctx, req)
}

func (s stubSubAgentControl) Wait(ctx context.Context, req subagentctl.WaitRequest) (subagentctl.StatusSnapshot, error) {
	if s.wait == nil {
		return subagentctl.StatusSnapshot{}, errors.New("wait not implemented")
	}
	return s.wait(ctx, req)
}

func (s stubSubAgentControl) Resume(ctx context.Context, req subagentctl.ResumeRequest) (subagentctl.StatusSnapshot, error) {
	if s.resume == nil {
		return subagentctl.StatusSnapshot{}, errors.New("resume not implemented")
	}
	return s.resume(ctx, req)
}

func (s stubSubAgentControl) Close(ctx context.Context, req subagentctl.CloseRequest) (subagentctl.StatusSnapshot, error) {
	if s.close == nil {
		return subagentctl.StatusSnapshot{}, errors.New("close not implemented")
	}
	return s.close(ctx, req)
}

func (s stubSubAgentControl) Interrupt(ctx context.Context, req subagentctl.InterruptRequest) (subagentctl.StatusSnapshot, error) {
	if s.interrupt == nil {
		return subagentctl.StatusSnapshot{}, errors.New("interrupt not implemented")
	}
	return s.interrupt(ctx, req)
}

func (s stubSubAgentControl) GetStatus(ctx context.Context, subAgentID uuid.UUID) (subagentctl.StatusSnapshot, error) {
	if s.getStatus == nil {
		return subagentctl.StatusSnapshot{}, errors.New("get_status not implemented")
	}
	return s.getStatus(ctx, subAgentID)
}

func (s stubSubAgentControl) ListChildren(ctx context.Context) ([]subagentctl.StatusSnapshot, error) {
	if s.list == nil {
		return nil, errors.New("list not implemented")
	}
	return s.list(ctx)
}

func newOutputControl(run func(personaID string, input string) (string, error)) stubSubAgentControl {
	var (
		mu      sync.Mutex
		outputs = map[uuid.UUID]string{}
		errs    = map[uuid.UUID]error{}
	)
	return stubSubAgentControl{
		spawn: func(_ context.Context, req subagentctl.SpawnRequest) (subagentctl.StatusSnapshot, error) {
			subAgentID := uuid.New()
			output, err := run(req.PersonaID, req.Input)
			mu.Lock()
			outputs[subAgentID] = output
			errs[subAgentID] = err
			mu.Unlock()
			personaID := req.PersonaID
			return subagentctl.StatusSnapshot{
				SubAgentID:  subAgentID,
				Status:      data.SubAgentStatusQueued,
				PersonaID:   &personaID,
				ContextMode: req.ContextMode,
			}, nil
		},
		wait: func(_ context.Context, req subagentctl.WaitRequest) (subagentctl.StatusSnapshot, error) {
			mu.Lock()
			output := outputs[req.SubAgentID]
			err := errs[req.SubAgentID]
			mu.Unlock()
			if err != nil {
				return subagentctl.StatusSnapshot{}, err
			}
			return subagentctl.StatusSnapshot{SubAgentID: req.SubAgentID, Status: data.SubAgentStatusCompleted, LastOutput: &output}, nil
		},
	}
}

func TestNewLuaExecutor_MissingScript(t *testing.T) {
	_, err := NewLuaExecutor(map[string]any{})
	if err == nil {
		t.Fatal("expected error when script is missing")
	}
}

func TestNewLuaExecutor_EmptyScript(t *testing.T) {
	_, err := NewLuaExecutor(map[string]any{"script": "   "})
	if err == nil {
		t.Fatal("expected error when script is blank")
	}
}

func TestNewLuaExecutor_ValidScript(t *testing.T) {
	ex, err := NewLuaExecutor(map[string]any{"script": "context.set_output('hello')"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ex == nil {
		t.Fatal("factory returned nil")
	}
}

func TestLuaExecutor_ContextSetOutput(t *testing.T) {
	ex, _ := NewLuaExecutor(map[string]any{
		"script": `context.set_output("hello from lua")`,
	})
	rc := buildLuaRC(nil)
	emitter := events.NewEmitter("trace")

	var got []events.RunEvent
	err := ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var deltaTexts []string
	completedCount := 0
	for _, ev := range got {
		switch ev.Type {
		case "message.delta":
			if delta, ok := ev.DataJSON["content_delta"].(string); ok {
				deltaTexts = append(deltaTexts, delta)
			}
		case "run.completed":
			completedCount++
		}
	}
	if len(deltaTexts) == 0 || deltaTexts[0] != "hello from lua" {
		t.Fatalf("expected message.delta with 'hello from lua', got: %v", deltaTexts)
	}
	if completedCount != 1 {
		t.Fatalf("expected 1 run.completed, got %d", completedCount)
	}
}

func TestLuaExecutor_NoOutput_StillCompletes(t *testing.T) {
	ex, _ := NewLuaExecutor(map[string]any{
		"script": `local x = 1 + 1`,
	})
	rc := buildLuaRC(nil)
	emitter := events.NewEmitter("trace")

	var got []events.RunEvent
	err := ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	completedCount := 0
	deltaCount := 0
	for _, ev := range got {
		if ev.Type == "run.completed" {
			completedCount++
		}
		if ev.Type == "message.delta" {
			deltaCount++
		}
	}
	if completedCount != 1 {
		t.Fatalf("expected 1 run.completed, got %d", completedCount)
	}
	if deltaCount != 0 {
		t.Fatalf("expected no message.delta when no output set, got %d", deltaCount)
	}
}

func TestLuaExecutor_ScriptError_EmitsRunFailed(t *testing.T) {
	ex, _ := NewLuaExecutor(map[string]any{
		"script": `this is not valid lua @@@@`,
	})
	rc := buildLuaRC(nil)
	emitter := events.NewEmitter("trace")

	var got []events.RunEvent
	err := ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	var failedCount int
	for _, ev := range got {
		if ev.Type == "run.failed" {
			failedCount++
			if ec, ok := ev.DataJSON["error_class"].(string); !ok || ec != "agent.lua.script_error" {
				t.Fatalf("expected error_class=agent.lua.script_error, got: %v", ev.DataJSON["error_class"])
			}
		}
	}
	if failedCount != 1 {
		t.Fatalf("expected 1 run.failed, got %d", failedCount)
	}
}

func TestLuaExecutor_ContextGet(t *testing.T) {
	ex, _ := NewLuaExecutor(map[string]any{
		"script": `
local v = context.get("user_prompt")
context.set_output(v)
`,
	})
	rc := buildLuaRC(nil)
	rc.InputJSON = map[string]any{"user_prompt": "test input"}

	emitter := events.NewEmitter("trace")
	var got []events.RunEvent
	err := ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	for _, ev := range got {
		if ev.Type == "message.delta" {
			if delta, ok := ev.DataJSON["content_delta"].(string); ok && delta == "test input" {
				return
			}
		}
	}
	t.Fatal("expected message.delta with 'test input'")
}

func TestLuaExecutor_ContextGet_MissingKey(t *testing.T) {
	ex, _ := NewLuaExecutor(map[string]any{
		"script": `
local v = context.get("nonexistent")
if v == nil then
  context.set_output("nil_ok")
end
`,
	})
	rc := buildLuaRC(nil)
	emitter := events.NewEmitter("trace")
	var got []events.RunEvent
	err := ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	for _, ev := range got {
		if ev.Type == "message.delta" {
			if delta, ok := ev.DataJSON["content_delta"].(string); ok && delta == "nil_ok" {
				return
			}
		}
	}
	t.Fatal("expected message.delta with 'nil_ok'")
}

func TestLuaExecutor_MemorySearch_Stub(t *testing.T) {
	ex, _ := NewLuaExecutor(map[string]any{
		"script": `
local results, err = memory.search("test query")
if err then error(err) end
context.set_output(results)
`,
	})
	rc := buildLuaRC(nil)
	emitter := events.NewEmitter("trace")
	var got []events.RunEvent
	err := ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	for _, ev := range got {
		if ev.Type == "message.delta" {
			if delta, _ := ev.DataJSON["content_delta"].(string); delta == "[]" {
				return
			}
		}
	}
	t.Fatal("expected memory.search stub to return '[]'")
}

func TestDefaultExecutorRegistry_ContainsAgentLua(t *testing.T) {
	reg := DefaultExecutorRegistry()
	ex, err := reg.Build("agent.lua", map[string]any{"script": "context.set_output('ok')"})
	if err != nil {
		t.Fatalf("Build agent.lua failed: %v", err)
	}
	if ex == nil {
		t.Fatal("Build returned nil")
	}
}

// buildLuaRC 构建适合 LuaExecutor 测试的最小 RunContext。
func buildLuaRC(gateway llm.Gateway) *pipeline.RunContext {
	rc := &pipeline.RunContext{
		Run: data.Run{
			ID:       uuid.New(),
			AccountID:    uuid.New(),
			ThreadID: uuid.New(),
		},
		TraceID:                "lua-test-trace",
		InputJSON:              map[string]any{},
		ReasoningIterations:    10,
		ToolContinuationBudget: 32,
		ToolBudget:             map[string]any{},
		PerToolSoftLimits:      tools.DefaultPerToolSoftLimits(),
	}
	if gateway != nil {
		rc.Gateway = gateway
		rc.SelectedRoute = &routing.SelectedProviderRoute{
			Route: routing.ProviderRouteRule{
				ID:    "default",
				Model: "stub",
			},
		}
	}
	return rc
}

type luaSeqGateway struct {
	events []llm.StreamEvent
}

func (g *luaSeqGateway) Stream(_ context.Context, _ llm.Request, yield func(llm.StreamEvent) error) error {
	for _, event := range g.events {
		if err := yield(event); err != nil {
			return err
		}
	}
	return nil
}

// --- mock MemoryProvider for Lua binding tests ---

type luaMemMock struct {
	findHits    []memory.MemoryHit
	findErr     error
	contentText string
	contentErr  error
	writeErr    error
	deleteErr   error
}

func (m *luaMemMock) Find(_ context.Context, _ memory.MemoryIdentity, _ memory.MemoryScope, _ string, _ int) ([]memory.MemoryHit, error) {
	return m.findHits, m.findErr
}

func (m *luaMemMock) Content(_ context.Context, _ memory.MemoryIdentity, _ string, _ memory.MemoryLayer) (string, error) {
	return m.contentText, m.contentErr
}

func (m *luaMemMock) AppendSessionMessages(_ context.Context, _ memory.MemoryIdentity, _ string, _ []memory.MemoryMessage) error {
	return nil
}

func (m *luaMemMock) CommitSession(_ context.Context, _ memory.MemoryIdentity, _ string) error {
	return nil
}

func (m *luaMemMock) Write(_ context.Context, _ memory.MemoryIdentity, _ memory.MemoryScope, _ memory.MemoryEntry) error {
	return m.writeErr
}

func (m *luaMemMock) Delete(_ context.Context, _ memory.MemoryIdentity, _ string) error {
	return m.deleteErr
}

// buildLuaRCWithMemory 构造注入了 MemoryProvider 和 UserID 的 RunContext。
func buildLuaRCWithMemory(mp memory.MemoryProvider) *pipeline.RunContext {
	rc := buildLuaRC(nil)
	uid := uuid.New()
	rc.UserID = &uid
	rc.MemoryProvider = mp
	return rc
}

func runLuaScript(t *testing.T, script string, rc *pipeline.RunContext) []events.RunEvent {
	t.Helper()
	ex, err := NewLuaExecutor(map[string]any{"script": script})
	if err != nil {
		t.Fatalf("NewLuaExecutor failed: %v", err)
	}
	emitter := events.NewEmitter("trace")
	var got []events.RunEvent
	if err := ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	}); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	return got
}

func deltaTexts(evs []events.RunEvent) []string {
	var out []string
	for _, ev := range evs {
		if ev.Type == "message.delta" {
			if d, ok := ev.DataJSON["content_delta"].(string); ok {
				out = append(out, d)
			}
		}
	}
	return out
}

func TestLuaExecutor_SubAgentLegacyBindingsRemoved(t *testing.T) {
	evs := runLuaScript(t, `
if agent.run == nil and agent.run_parallel == nil then
  context.set_output("legacy_removed")
else
  context.set_output("legacy_present")
end
`, buildLuaRC(nil))

	texts := deltaTexts(evs)
	if len(texts) == 0 || texts[0] != "legacy_removed" {
		t.Fatalf("expected legacy bindings removed, got: %v", texts)
	}
}

func TestLuaExecutor_SubAgentSpawn_Unavailable(t *testing.T) {
	evs := runLuaScript(t, `
local status, err = agent.spawn({ persona_id = "lite", input = "hello" })
if status ~= nil then
  error("unexpected status")
end
context.set_output(err or "")
`, buildLuaRC(nil))

	texts := deltaTexts(evs)
	if len(texts) == 0 || texts[0] != "agent.spawn not available: SubAgentControl not initialized" {
		t.Fatalf("unexpected spawn unavailable output: %v", texts)
	}
}

func TestLuaExecutor_SubAgentSpawnWait_Success(t *testing.T) {
	var captured subagentctl.SpawnRequest
	rc := buildLuaRC(nil)
	rc.SubAgentControl = stubSubAgentControl{
		spawn: func(_ context.Context, req subagentctl.SpawnRequest) (subagentctl.StatusSnapshot, error) {
			captured = req
			subAgentID := uuid.New()
			personaID := req.PersonaID
			return subagentctl.StatusSnapshot{
				SubAgentID:  subAgentID,
				Status:      data.SubAgentStatusQueued,
				PersonaID:   &personaID,
				ContextMode: req.ContextMode,
			}, nil
		},
		wait: func(_ context.Context, req subagentctl.WaitRequest) (subagentctl.StatusSnapshot, error) {
			output := "4"
			return subagentctl.StatusSnapshot{
				SubAgentID:  req.SubAgentID,
				Status:      data.SubAgentStatusCompleted,
				ContextMode: data.SubAgentContextModeIsolated,
				LastOutput:  &output,
			}, nil
		},
	}

	evs := runLuaScript(t, `
local spawned, spawn_err = agent.spawn({ persona_id = "lite", input = "what is 2+2?" })
if spawn_err ~= nil then error(spawn_err) end
if spawned.id == nil or spawned.status ~= "queued" or spawned.context_mode ~= "isolated" then
  error("bad spawn status")
end
local waited, wait_err = agent.wait(spawned.id)
if wait_err ~= nil then error(wait_err) end
context.set_output(waited.output or "")
`, rc)

	if captured.PersonaID != "lite" {
		t.Fatalf("expected persona lite, got %q", captured.PersonaID)
	}
	if captured.Input != "what is 2+2?" {
		t.Fatalf("unexpected input: %q", captured.Input)
	}
	if captured.ContextMode != data.SubAgentContextModeIsolated {
		t.Fatalf("expected isolated context mode, got %q", captured.ContextMode)
	}
	texts := deltaTexts(evs)
	if len(texts) == 0 || texts[0] != "4" {
		t.Fatalf("expected waited output 4, got: %v", texts)
	}
}

func TestLuaExecutor_SubAgentSend_Interrupt(t *testing.T) {
	subAgentID := uuid.New()
	var captured subagentctl.SendInputRequest
	rc := buildLuaRC(nil)
	rc.SubAgentControl = stubSubAgentControl{
		sendInput: func(_ context.Context, req subagentctl.SendInputRequest) (subagentctl.StatusSnapshot, error) {
			captured = req
			return subagentctl.StatusSnapshot{SubAgentID: req.SubAgentID, Status: data.SubAgentStatusRunning}, nil
		},
	}

	evs := runLuaScript(t, `
local sent, err = agent.send("`+subAgentID.String()+`", "follow-up", { interrupt = true })
if err ~= nil then error(err) end
context.set_output(sent.status)
`, rc)

	if captured.SubAgentID != subAgentID {
		t.Fatalf("unexpected sub_agent_id: %s", captured.SubAgentID)
	}
	if captured.Input != "follow-up" {
		t.Fatalf("unexpected send input: %q", captured.Input)
	}
	if !captured.Interrupt {
		t.Fatal("expected interrupt=true")
	}
	texts := deltaTexts(evs)
	if len(texts) == 0 || texts[0] != data.SubAgentStatusRunning {
		t.Fatalf("expected running status output, got: %v", texts)
	}
}

func TestLuaExecutor_SubAgentWait_TimeoutMs(t *testing.T) {
	subAgentID := uuid.New()
	var captured time.Duration
	rc := buildLuaRC(nil)
	rc.SubAgentControl = stubSubAgentControl{
		wait: func(_ context.Context, req subagentctl.WaitRequest) (subagentctl.StatusSnapshot, error) {
			captured = req.Timeout
			return subagentctl.StatusSnapshot{SubAgentID: req.SubAgentID, Status: data.SubAgentStatusCompleted}, nil
		},
	}

	evs := runLuaScript(t, `
local waited, err = agent.wait("`+subAgentID.String()+`", 2500)
if err ~= nil then error(err) end
context.set_output(waited.status)
`, rc)

	if captured != 2500*time.Millisecond {
		t.Fatalf("expected 2500ms timeout, got %s", captured)
	}
	texts := deltaTexts(evs)
	if len(texts) == 0 || texts[0] != data.SubAgentStatusCompleted {
		t.Fatalf("expected completed status output, got: %v", texts)
	}
}

func TestLuaExecutor_SubAgentResumeAndClose_ReturnStatus(t *testing.T) {
	resumeID := uuid.New()
	closeID := uuid.New()
	var resumedID uuid.UUID
	var closedID uuid.UUID
	rc := buildLuaRC(nil)
	rc.SubAgentControl = stubSubAgentControl{
		resume: func(_ context.Context, req subagentctl.ResumeRequest) (subagentctl.StatusSnapshot, error) {
			resumedID = req.SubAgentID
			return subagentctl.StatusSnapshot{SubAgentID: req.SubAgentID, Status: data.SubAgentStatusRunning}, nil
		},
		close: func(_ context.Context, req subagentctl.CloseRequest) (subagentctl.StatusSnapshot, error) {
			closedID = req.SubAgentID
			return subagentctl.StatusSnapshot{SubAgentID: req.SubAgentID, Status: data.SubAgentStatusClosed}, nil
		},
	}

	evs := runLuaScript(t, `
local resumed, resume_err = agent.resume("`+resumeID.String()+`")
if resume_err ~= nil then error(resume_err) end
local closed, close_err = agent.close("`+closeID.String()+`")
if close_err ~= nil then error(close_err) end
context.set_output(resumed.status .. "|" .. closed.status)
`, rc)

	if resumedID != resumeID {
		t.Fatalf("unexpected resume id: %s", resumedID)
	}
	if closedID != closeID {
		t.Fatalf("unexpected close id: %s", closedID)
	}
	texts := deltaTexts(evs)
	if len(texts) == 0 || texts[0] != data.SubAgentStatusRunning+"|"+data.SubAgentStatusClosed {
		t.Fatalf("unexpected resume/close output: %v", texts)
	}
}

func TestLuaExecutor_SubAgentContextCancelled(t *testing.T) {
	rc := buildLuaRC(nil)
	rc.SubAgentControl = stubSubAgentControl{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ex, err := NewLuaExecutor(map[string]any{
		"script": `
local status, spawn_err = agent.spawn({ persona_id = "lite", input = "hi" })
if status ~= nil then error("unexpected status") end
context.set_output(spawn_err or "")
`,
	})
	if err != nil {
		t.Fatalf("NewLuaExecutor failed: %v", err)
	}
	emitter := events.NewEmitter("trace")
	var evs []events.RunEvent
	if err := ex.Execute(ctx, rc, emitter, func(ev events.RunEvent) error {
		evs = append(evs, ev)
		return nil
	}); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	texts := deltaTexts(evs)
	if len(texts) == 0 || texts[0] != context.Canceled.Error() {
		t.Fatalf("expected cancelled output, got: %v", texts)
	}
}

func TestLuaExecutor_MemorySearch_WithProvider(t *testing.T) {
	mp := &luaMemMock{
		findHits: []memory.MemoryHit{
			{URI: "viking://user/memories/prefs/lang", Abstract: "Go", Score: 0.9},
		},
	}
	rc := buildLuaRCWithMemory(mp)
	evs := runLuaScript(t, `
local res, err = memory.search("language")
if err then error(err) end
context.set_output(res)
`, rc)

	texts := deltaTexts(evs)
	if len(texts) == 0 {
		t.Fatal("expected message.delta output")
	}
	if !strings.Contains(texts[0], "viking://user/memories/prefs/lang") {
		t.Fatalf("expected URI in output, got: %q", texts[0])
	}
}

func TestLuaExecutor_MemoryRead_WithProvider(t *testing.T) {
	mp := &luaMemMock{contentText: "user prefers Go"}
	rc := buildLuaRCWithMemory(mp)
	evs := runLuaScript(t, `
local content, err = memory.read("viking://user/memories/prefs/lang")
if err then error(err) end
context.set_output(content)
`, rc)

	texts := deltaTexts(evs)
	if len(texts) == 0 || texts[0] != "user prefers Go" {
		t.Fatalf("expected 'user prefers Go', got: %v", texts)
	}
}

func TestLuaExecutor_MemoryWrite_WithProvider(t *testing.T) {
	mp := &luaMemMock{}
	rc := buildLuaRCWithMemory(mp)
	evs := runLuaScript(t, `
local uri, err = memory.write("preferences", "language", "Go")
if err then error(err) end
context.set_output(uri)
`, rc)

	texts := deltaTexts(evs)
	if len(texts) == 0 {
		t.Fatal("expected URI output from memory.write")
	}
	if !strings.HasPrefix(texts[0], "viking://") {
		t.Fatalf("expected viking:// URI, got: %q", texts[0])
	}
}

func TestLuaExecutor_MemoryForget_WithProvider(t *testing.T) {
	mp := &luaMemMock{}
	rc := buildLuaRCWithMemory(mp)
	evs := runLuaScript(t, `
local ok, err = memory.forget("viking://user/memories/prefs/lang")
if err then error(err) end
if ok then context.set_output("deleted") end
`, rc)

	texts := deltaTexts(evs)
	if len(texts) == 0 || texts[0] != "deleted" {
		t.Fatalf("expected 'deleted', got: %v", texts)
	}
}

func TestLuaExecutor_MemoryRead_ProviderNil(t *testing.T) {
	rc := buildLuaRC(nil) // provider nil
	evs := runLuaScript(t, `
local content, err = memory.read("viking://user/memories/prefs/lang")
if err then
  context.set_output("err:" .. err)
else
  context.set_output("ok")
end
`, rc)

	texts := deltaTexts(evs)
	if len(texts) == 0 || !strings.HasPrefix(texts[0], "err:") {
		t.Fatalf("expected error when provider is nil, got: %v", texts)
	}
}

func TestLuaExecutor_MemoryWrite_ProviderNil(t *testing.T) {
	rc := buildLuaRC(nil)
	evs := runLuaScript(t, `
local uri, err = memory.write("preferences", "language", "Go")
if err then
  context.set_output("err:" .. err)
else
  context.set_output("ok")
end
`, rc)

	texts := deltaTexts(evs)
	if len(texts) == 0 || !strings.HasPrefix(texts[0], "err:") {
		t.Fatalf("expected error when provider is nil, got: %v", texts)
	}
}

func TestLuaExecutor_MemoryForget_ProviderNil(t *testing.T) {
	rc := buildLuaRC(nil)
	evs := runLuaScript(t, `
local ok, err = memory.forget("viking://user/memories/prefs/lang")
if err then
  context.set_output("err:" .. err)
else
  context.set_output("ok")
end
`, rc)

	texts := deltaTexts(evs)
	if len(texts) == 0 || !strings.HasPrefix(texts[0], "err:") {
		t.Fatalf("expected error when provider is nil, got: %v", texts)
	}
}

// --- context.emit tests ---

func TestLuaExecutor_ContextEmit_Table(t *testing.T) {
	evs := runLuaScript(t, `
context.emit("run.segment.start", {
  segment_id = "seg1",
  kind = "search_planning",
  display = { mode = "visible", label = "Testing" }
})
`, buildLuaRC(nil))

	for _, ev := range evs {
		if ev.Type == "run.segment.start" {
			if ev.DataJSON["segment_id"] == "seg1" && ev.DataJSON["kind"] == "search_planning" {
				display, ok := ev.DataJSON["display"].(map[string]any)
				if ok && display["label"] == "Testing" {
					return
				}
			}
		}
	}
	t.Fatal("expected run.segment.start with segment_id=seg1")
}

func TestLuaExecutor_ContextEmit_JSONString(t *testing.T) {
	evs := runLuaScript(t, `
context.emit("run.segment.end", '{"segment_id":"seg1"}')
`, buildLuaRC(nil))

	for _, ev := range evs {
		if ev.Type == "run.segment.end" && ev.DataJSON["segment_id"] == "seg1" {
			return
		}
	}
	t.Fatal("expected run.segment.end with segment_id=seg1")
}

func TestLuaExecutor_ContextEmit_InvalidJSON(t *testing.T) {
	evs := runLuaScript(t, `
local ok, err = context.emit("run.segment.start", "not json")
if not ok then
  context.set_output("emit_failed")
end
`, buildLuaRC(nil))

	texts := deltaTexts(evs)
	if len(texts) == 0 || texts[0] != "emit_failed" {
		t.Fatalf("expected emit_failed, got: %v", texts)
	}
}

// --- context.get extensions ---

func TestLuaExecutor_ContextGet_SystemPrompt(t *testing.T) {
	rc := buildLuaRC(nil)
	rc.SystemPrompt = "You are a search assistant."
	evs := runLuaScript(t, `
context.set_output(context.get("system_prompt"))
`, rc)

	texts := deltaTexts(evs)
	if len(texts) == 0 || texts[0] != "You are a search assistant." {
		t.Fatalf("expected system_prompt, got: %v", texts)
	}
}

func TestLuaExecutor_ContextGet_Messages(t *testing.T) {
	rc := buildLuaRC(nil)
	rc.Messages = []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "hello"}}},
		{Role: "assistant", Content: []llm.TextPart{{Text: "hi"}}},
	}
	evs := runLuaScript(t, `
local msgs = context.get("messages")
local parsed = json.decode(msgs)
context.set_output(tostring(#parsed) .. ":" .. parsed[1].role)
`, rc)

	texts := deltaTexts(evs)
	if len(texts) == 0 || texts[0] != "2:user" {
		t.Fatalf("expected '2:user', got: %v", texts)
	}
}

// --- agent.generate tests ---

func TestLuaExecutor_AgentGenerate_Basic(t *testing.T) {
	gw := llm.NewStubGateway(llm.StubGatewayConfig{Enabled: true, DeltaCount: 1})
	rc := buildLuaRC(gw)
	evs := runLuaScript(t, `
local out, err = agent.generate("system", "user input")
if err then error(err) end
context.set_output(out)
`, rc)

	texts := deltaTexts(evs)
	if len(texts) == 0 || texts[0] != "stub delta 1" {
		t.Fatalf("expected 'stub delta 1', got: %v", texts)
	}
	// agent.generate 不应产生 message.delta 事件（只在最后 set_output 时产生）
	deltasBeforeSetOutput := 0
	for _, ev := range evs {
		if ev.Type == "message.delta" {
			deltasBeforeSetOutput++
		}
	}
	if deltasBeforeSetOutput != 1 {
		t.Fatalf("agent.generate should not yield message.delta, but got %d (1 from set_output)", deltasBeforeSetOutput)
	}
}

func TestLuaExecutor_AgentGenerate_GatewayNil(t *testing.T) {
	rc := buildLuaRC(nil)
	evs := runLuaScript(t, `
local out, err = agent.generate("sys", "msg")
if err then
  context.set_output("err:" .. err)
end
`, rc)

	texts := deltaTexts(evs)
	if len(texts) == 0 || !strings.HasPrefix(texts[0], "err:") {
		t.Fatalf("expected error when gateway is nil, got: %v", texts)
	}
}

func TestLuaExecutor_AgentGenerate_MaxTokens(t *testing.T) {
	gw := llm.NewStubGateway(llm.StubGatewayConfig{Enabled: true, DeltaCount: 1})
	rc := buildLuaRC(gw)
	evs := runLuaScript(t, `
local out, err = agent.generate("sys", "msg", {max_tokens = 256})
if err then error(err) end
context.set_output(out)
`, rc)

	texts := deltaTexts(evs)
	if len(texts) == 0 {
		t.Fatal("expected output from agent.generate with max_tokens")
	}
}

// --- agent.stream tests ---

func TestLuaExecutor_AgentStream_StringMessage(t *testing.T) {
	gw := llm.NewStubGateway(llm.StubGatewayConfig{Enabled: true, DeltaCount: 3})
	rc := buildLuaRC(gw)
	evs := runLuaScript(t, `
local out, err = agent.stream("system prompt", "user query")
if err then error(err) end
-- out 应包含完整文本，不需要 set_output
`, rc)

	// agent.stream 应产生 message.delta 事件
	var deltas []string
	for _, ev := range evs {
		if ev.Type == "message.delta" {
			if d, ok := ev.DataJSON["content_delta"].(string); ok {
				deltas = append(deltas, d)
			}
		}
	}
	if len(deltas) != 3 {
		t.Fatalf("expected 3 message.delta from stream, got %d", len(deltas))
	}
}

func TestLuaExecutor_AgentStream_MessagesTable(t *testing.T) {
	gw := llm.NewStubGateway(llm.StubGatewayConfig{Enabled: true, DeltaCount: 2})
	rc := buildLuaRC(gw)
	evs := runLuaScript(t, `
local msgs = {
  {role = "user", content = "hello"},
  {role = "assistant", content = "hi"},
  {role = "user", content = "how are you"},
}
local out, err = agent.stream("system", msgs)
if err then error(err) end
`, rc)

	var deltaCount int
	for _, ev := range evs {
		if ev.Type == "message.delta" {
			deltaCount++
		}
	}
	if deltaCount != 2 {
		t.Fatalf("expected 2 message.delta, got %d", deltaCount)
	}
}

func TestLuaExecutor_AgentStream_GatewayNil(t *testing.T) {
	rc := buildLuaRC(nil)
	evs := runLuaScript(t, `
local out, err = agent.stream("sys", "msg")
if err then
  context.set_output("err:" .. err)
end
`, rc)

	texts := deltaTexts(evs)
	if len(texts) == 0 || !strings.HasPrefix(texts[0], "err:") {
		t.Fatalf("expected error when gateway is nil, got: %v", texts)
	}
}

func TestLuaExecutor_AgentLoopCapture_CapturesTextWithoutDirectDelta(t *testing.T) {
	inputTokens := 11
	outputTokens := 29
	gw := &luaSeqGateway{
		events: []llm.StreamEvent{
			llm.StreamSegmentStart{
				SegmentID: "s1",
				Kind:      "thinking",
				Display:   llm.SegmentDisplay{Mode: "visible", Label: "Step"},
			},
			llm.StreamMessageDelta{ContentDelta: "captured-", Role: "assistant"},
			llm.StreamMessageDelta{ContentDelta: "result", Role: "assistant"},
			llm.StreamSegmentEnd{SegmentID: "s1"},
			llm.StreamRunCompleted{
				Usage: &llm.Usage{
					InputTokens:  &inputTokens,
					OutputTokens: &outputTokens,
				},
			},
		},
	}
	rc := buildLuaRC(gw)
	evs := runLuaScript(t, `
local out, err = agent.loop_capture("system", "query")
if err then error(err) end
context.set_output(out)
`, rc)

	deltas := deltaTexts(evs)
	if len(deltas) != 1 || deltas[0] != "captured-result" {
		t.Fatalf("expected only set_output delta 'captured-result', got: %v", deltas)
	}

	var hasSegmentStart bool
	var hasSegmentEnd bool
	var usage map[string]any
	for _, ev := range evs {
		switch ev.Type {
		case "run.segment.start":
			hasSegmentStart = true
		case "run.segment.end":
			hasSegmentEnd = true
		case "run.completed":
			raw, ok := ev.DataJSON["usage"].(map[string]any)
			if ok {
				usage = raw
			}
		}
	}
	if !hasSegmentStart || !hasSegmentEnd {
		t.Fatalf("expected run.segment events passthrough, start=%v end=%v", hasSegmentStart, hasSegmentEnd)
	}
	if usage == nil {
		t.Fatal("expected run.completed usage from loop_capture")
	}
	if usage["input_tokens"] != inputTokens || usage["output_tokens"] != outputTokens {
		t.Fatalf("unexpected usage payload: %#v", usage)
	}
}

func TestLuaExecutor_AgentStreamRoute_UsesResolvedRoute(t *testing.T) {
	mainGW := &luaSeqGateway{
		events: []llm.StreamEvent{
			llm.StreamMessageDelta{ContentDelta: "main", Role: "assistant"},
			llm.StreamRunCompleted{},
		},
	}
	routeGW := &luaSeqGateway{
		events: []llm.StreamEvent{
			llm.StreamMessageDelta{ContentDelta: "route", Role: "assistant"},
			llm.StreamRunCompleted{},
		},
	}
	rc := buildLuaRC(mainGW)
	var resolvedRouteID string
	rc.ResolveGatewayForRouteID = func(_ context.Context, routeID string) (llm.Gateway, *routing.SelectedProviderRoute, error) {
		resolvedRouteID = routeID
		return routeGW, &routing.SelectedProviderRoute{
			Route: routing.ProviderRouteRule{
				ID:    routeID,
				Model: "route-model",
			},
		}, nil
	}

	evs := runLuaScript(t, `
local out, err = agent.stream_route("final-route", "sys", "msg")
if err then error(err) end
`, rc)
	if resolvedRouteID != "final-route" {
		t.Fatalf("expected resolver called with final-route, got %q", resolvedRouteID)
	}
	deltas := deltaTexts(evs)
	if len(deltas) == 0 || deltas[0] != "route" {
		t.Fatalf("expected route gateway delta, got %v", deltas)
	}
}

func TestLuaExecutor_AgentStreamRoute_EmptyRouteFallsBackToPrimaryGateway(t *testing.T) {
	mainGW := &luaSeqGateway{
		events: []llm.StreamEvent{
			llm.StreamMessageDelta{ContentDelta: "primary", Role: "assistant"},
			llm.StreamRunCompleted{},
		},
	}
	rc := buildLuaRC(mainGW)
	resolverCalled := false
	rc.ResolveGatewayForRouteID = func(_ context.Context, _ string) (llm.Gateway, *routing.SelectedProviderRoute, error) {
		resolverCalled = true
		return nil, nil, errors.New("should not be called")
	}

	evs := runLuaScript(t, `
local out, err = agent.stream_route("", "sys", "msg")
if err then error(err) end
`, rc)
	if resolverCalled {
		t.Fatal("resolver should not be called when route_id is empty")
	}
	deltas := deltaTexts(evs)
	if len(deltas) == 0 || deltas[0] != "primary" {
		t.Fatalf("expected primary gateway delta, got %v", deltas)
	}
}

func TestLuaExecutor_AgentStreamRoute_RouteResolveFailedCanFallback(t *testing.T) {
	mainGW := &luaSeqGateway{
		events: []llm.StreamEvent{
			llm.StreamMessageDelta{ContentDelta: "fallback", Role: "assistant"},
			llm.StreamRunCompleted{},
		},
	}
	rc := buildLuaRC(mainGW)
	rc.ResolveGatewayForRouteID = func(_ context.Context, _ string) (llm.Gateway, *routing.SelectedProviderRoute, error) {
		return nil, nil, errors.New("route missing")
	}

	evs := runLuaScript(t, `
local out, err = agent.stream_route("missing-route", "sys", "msg")
if err and string.find(err, "route_resolve_failed:", 1, true) == 1 then
  local fb, fbErr = agent.stream("sys", "msg")
  if fbErr then error(fbErr) end
end
`, rc)
	deltas := deltaTexts(evs)
	if len(deltas) == 0 || deltas[0] != "fallback" {
		t.Fatalf("expected fallback gateway delta after resolve error, got %v", deltas)
	}
	for _, ev := range evs {
		if ev.Type == "run.failed" {
			t.Fatalf("did not expect run.failed in resolve fallback path: %#v", ev.DataJSON)
		}
	}
}

func TestLuaExecutor_AgentStreamRoute_StreamStartedFailureEmitsRunFailed(t *testing.T) {
	mainGW := &luaSeqGateway{
		events: []llm.StreamEvent{
			llm.StreamMessageDelta{ContentDelta: "main", Role: "assistant"},
			llm.StreamRunCompleted{},
		},
	}
	routeGW := &luaSeqGateway{
		events: []llm.StreamEvent{
			llm.StreamMessageDelta{ContentDelta: "partial", Role: "assistant"},
			llm.StreamRunFailed{
				Error: llm.GatewayError{
					ErrorClass: llm.ErrorClassProviderNonRetryable,
					Message:    "route stream failed",
				},
			},
		},
	}
	rc := buildLuaRC(mainGW)
	rc.ResolveGatewayForRouteID = func(_ context.Context, routeID string) (llm.Gateway, *routing.SelectedProviderRoute, error) {
		return routeGW, &routing.SelectedProviderRoute{
			Route: routing.ProviderRouteRule{
				ID:    routeID,
				Model: "route-model",
			},
		}, nil
	}

	evs := runLuaScript(t, `
local out, err = agent.stream_route("final-route", "sys", "msg")
if err and string.find(err, "stream_terminal_failed:", 1, true) == 1 then
  return
end
if err then error(err) end
`, rc)

	deltas := deltaTexts(evs)
	if len(deltas) == 0 || deltas[0] != "partial" {
		t.Fatalf("expected partial delta before failure, got %v", deltas)
	}
	runFailedCount := 0
	for _, ev := range evs {
		if ev.Type == "run.failed" {
			runFailedCount++
			if msg, _ := ev.DataJSON["message"].(string); msg != "route stream failed" {
				t.Fatalf("unexpected run.failed message: %#v", ev.DataJSON)
			}
		}
	}
	if runFailedCount != 1 {
		t.Fatalf("expected 1 run.failed event, got %d", runFailedCount)
	}
}

func TestLuaExecutor_AgentStreamAgent_UsesResolvedAgentName(t *testing.T) {
	mainGW := &luaSeqGateway{
		events: []llm.StreamEvent{
			llm.StreamMessageDelta{ContentDelta: "main", Role: "assistant"},
			llm.StreamRunCompleted{},
		},
	}
	agentGW := &luaSeqGateway{
		events: []llm.StreamEvent{
			llm.StreamMessageDelta{ContentDelta: "agent", Role: "assistant"},
			llm.StreamRunCompleted{},
		},
	}
	rc := buildLuaRC(mainGW)
	var resolvedAgentName string
	rc.ResolveGatewayForAgentName = func(_ context.Context, agentName string) (llm.Gateway, *routing.SelectedProviderRoute, error) {
		resolvedAgentName = agentName
		return agentGW, &routing.SelectedProviderRoute{
			Route: routing.ProviderRouteRule{
				ID:    "agent-route",
				Model: "agent-model",
			},
		}, nil
	}

	evs := runLuaScript(t, `
local out, err = agent.stream_agent("sub-haiku-4.5", "sys", "msg")
if err then error(err) end
`, rc)
	if resolvedAgentName != "sub-haiku-4.5" {
		t.Fatalf("expected resolver called with sub-haiku-4.5, got %q", resolvedAgentName)
	}
	deltas := deltaTexts(evs)
	if len(deltas) == 0 || deltas[0] != "agent" {
		t.Fatalf("expected agent gateway delta, got %v", deltas)
	}
}

func TestLuaExecutor_AgentStreamAgent_ResolveFailedCanFallback(t *testing.T) {
	mainGW := &luaSeqGateway{
		events: []llm.StreamEvent{
			llm.StreamMessageDelta{ContentDelta: "fallback", Role: "assistant"},
			llm.StreamRunCompleted{},
		},
	}
	rc := buildLuaRC(mainGW)
	rc.ResolveGatewayForAgentName = func(_ context.Context, _ string) (llm.Gateway, *routing.SelectedProviderRoute, error) {
		return nil, nil, errors.New("agent not found")
	}

	evs := runLuaScript(t, `
local out, err = agent.stream_agent("sub-haiku-4.5", "sys", "msg")
if err and string.find(err, "agent_resolve_failed:", 1, true) == 1 then
  local fb, fbErr = agent.stream("sys", "msg")
  if fbErr then error(fbErr) end
end
`, rc)
	deltas := deltaTexts(evs)
	if len(deltas) == 0 || deltas[0] != "fallback" {
		t.Fatalf("expected fallback delta after agent resolve error, got %v", deltas)
	}
}

// --- tools.call_parallel tests ---

func TestLuaExecutor_ToolsCallParallel_EmptyCalls(t *testing.T) {
	rc := buildLuaRC(nil)
	evs := runLuaScript(t, `
local results, errs = tools.call_parallel({})
context.set_output(tostring(#results) .. ":" .. tostring(#errs))
`, rc)

	texts := deltaTexts(evs)
	if len(texts) == 0 || texts[0] != "0:0" {
		t.Fatalf("expected '0:0', got: %v", texts)
	}
}

func TestLuaExecutor_ToolsCallParallel_ExecutorNil(t *testing.T) {
	rc := buildLuaRC(nil)
	rc.ToolExecutor = nil
	evs := runLuaScript(t, `
local results, errs = tools.call_parallel({{name="web_search", args='{"query":"test"}'}})
if results == nil then
  context.set_output("nil_exec")
end
`, rc)

	texts := deltaTexts(evs)
	if len(texts) == 0 || texts[0] != "nil_exec" {
		t.Fatalf("expected 'nil_exec', got: %v", texts)
	}
}

func TestLuaExecutor_Sandbox_OsBlocked(t *testing.T) {
	evs := runLuaScript(t, `
if os == nil then
  context.set_output("os_blocked")
else
  context.set_output("os_available")
end
`, buildLuaRC(nil))

	texts := deltaTexts(evs)
	if len(texts) == 0 || texts[0] != "os_blocked" {
		t.Fatalf("expected os to be blocked, got: %v", texts)
	}
}

func TestLuaExecutor_Sandbox_IoBlocked(t *testing.T) {
	evs := runLuaScript(t, `
if io == nil then
  context.set_output("io_blocked")
else
  context.set_output("io_available")
end
`, buildLuaRC(nil))

	texts := deltaTexts(evs)
	if len(texts) == 0 || texts[0] != "io_blocked" {
		t.Fatalf("expected io to be blocked, got: %v", texts)
	}
}

func TestLuaExecutor_Sandbox_DebugBlocked(t *testing.T) {
	evs := runLuaScript(t, `
if debug == nil then
  context.set_output("debug_blocked")
else
  context.set_output("debug_available")
end
`, buildLuaRC(nil))

	texts := deltaTexts(evs)
	if len(texts) == 0 || texts[0] != "debug_blocked" {
		t.Fatalf("expected debug to be blocked, got: %v", texts)
	}
}

func TestLuaExecutor_Sandbox_DofileBlocked(t *testing.T) {
	evs := runLuaScript(t, `
if dofile == nil then
  context.set_output("dofile_blocked")
else
  context.set_output("dofile_available")
end
`, buildLuaRC(nil))

	texts := deltaTexts(evs)
	if len(texts) == 0 || texts[0] != "dofile_blocked" {
		t.Fatalf("expected dofile to be blocked, got: %v", texts)
	}
}

func TestLuaExecutor_Sandbox_SafeLibsAvailable(t *testing.T) {
	evs := runLuaScript(t, `
local result = tostring(type(string.len)) .. "," .. tostring(type(math.abs)) .. "," .. tostring(type(table.insert))
context.set_output(result)
`, buildLuaRC(nil))

	texts := deltaTexts(evs)
	if len(texts) == 0 || texts[0] != "function,function,function" {
		t.Fatalf("expected safe libs to be available, got: %v", texts)
	}
}
