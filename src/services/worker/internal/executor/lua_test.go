package executor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/routing"
	"github.com/google/uuid"
)

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

func TestLuaExecutor_AgentRun_SpawnChildRunNil(t *testing.T) {
	ex, _ := NewLuaExecutor(map[string]any{
		"script": `
local out, err = agent.run("some_skill", "input")
if err then
  context.set_output("err:" .. err)
end
`,
	})
	rc := buildLuaRC(nil)
	rc.SpawnChildRun = nil // 未初始化

	emitter := events.NewEmitter("trace")
	var got []events.RunEvent
	_ = ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})

	for _, ev := range got {
		if ev.Type == "message.delta" {
			delta, _ := ev.DataJSON["content_delta"].(string)
			if strings.HasPrefix(delta, "err:") {
				return
			}
		}
	}
	t.Fatal("expected error message when SpawnChildRun is nil")
}

func TestLuaExecutor_AgentRun_SpawnChildRunCalled(t *testing.T) {
	var capturedSkill, capturedInput string
	ex, _ := NewLuaExecutor(map[string]any{
		"script": `
local out, err = agent.run("lite", "what is 2+2?")
if err then error(err) end
context.set_output(out)
`,
	})
	rc := buildLuaRC(nil)
	rc.SpawnChildRun = func(_ context.Context, skillID string, input string) (string, error) {
		capturedSkill = skillID
		capturedInput = input
		return "4", nil
	}

	emitter := events.NewEmitter("trace")
	var got []events.RunEvent
	err := ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if capturedSkill != "lite" {
		t.Fatalf("expected skill 'lite', got %q", capturedSkill)
	}
	if capturedInput != "what is 2+2?" {
		t.Fatalf("unexpected input: %q", capturedInput)
	}
	for _, ev := range got {
		if ev.Type == "message.delta" {
			if delta, _ := ev.DataJSON["content_delta"].(string); delta == "4" {
				return
			}
		}
	}
	t.Fatal("expected message.delta with '4'")
}

func TestLuaExecutor_ContextCancelled_AgentRun(t *testing.T) {
	ex, _ := NewLuaExecutor(map[string]any{
		"script": `
local out, err = agent.run("lite", "input")
if err then
  context.set_output("cancelled")
end
`,
	})
	rc := buildLuaRC(nil)
	rc.SpawnChildRun = func(_ context.Context, _ string, _ string) (string, error) {
		return "", errors.New("some error")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	emitter := events.NewEmitter("trace")
	var got []events.RunEvent
	_ = ex.Execute(ctx, rc, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	// 取消后 agent.run binding 应返回 ctx error，脚本设置输出 "cancelled"
	found := false
	for _, ev := range got {
		if ev.Type == "message.delta" {
			if delta, _ := ev.DataJSON["content_delta"].(string); delta == "cancelled" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected 'cancelled' output when context is already cancelled")
	}
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
			OrgID:    uuid.New(),
			ThreadID: uuid.New(),
		},
		TraceID:   "lua-test-trace",
		InputJSON: map[string]any{},
		ToolBudget: map[string]any{},
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

func TestLuaExecutor_AgentRunParallel_SpawnChildRunNil(t *testing.T) {
	ex, _ := NewLuaExecutor(map[string]any{
		"script": `
local results, errs = agent.run_parallel({{skill="lite", input="q"}})
if errs == nil then
  context.set_output("err:" .. tostring(results))
else
  context.set_output("err:nil_spawn")
end
`,
	})
	rc := buildLuaRC(nil)
	rc.SpawnChildRun = nil

	emitter := events.NewEmitter("trace")
	var got []events.RunEvent
	_ = ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})

	for _, ev := range got {
		if ev.Type == "message.delta" {
			if delta, _ := ev.DataJSON["content_delta"].(string); strings.HasPrefix(delta, "err:") {
				return
			}
		}
	}
	t.Fatal("expected error output when SpawnChildRun is nil")
}

func TestLuaExecutor_AgentRunParallel_EmptyTasks(t *testing.T) {
	ex, _ := NewLuaExecutor(map[string]any{
		"script": `
local results, errs = agent.run_parallel({})
context.set_output(tostring(#results) .. ":" .. tostring(#errs))
`,
	})
	rc := buildLuaRC(nil)
	rc.SpawnChildRun = func(_ context.Context, _ string, _ string) (string, error) {
		return "x", nil
	}

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
			if delta, _ := ev.DataJSON["content_delta"].(string); delta == "0:0" {
				return
			}
		}
	}
	t.Fatal("expected '0:0' for empty tasks")
}

func TestLuaExecutor_AgentRunParallel_AllSucceed(t *testing.T) {
	ex, _ := NewLuaExecutor(map[string]any{
		"script": `
local tasks = {
  {skill="lite", input="q1"},
  {skill="lite", input="q2"},
  {skill="lite", input="q3"},
}
local results, errs = agent.run_parallel(tasks)
local out = ""
for i = 1, #results do
  if errs[i] ~= nil then error("unexpected error at " .. i) end
  out = out .. results[i]
end
context.set_output(out)
`,
	})
	rc := buildLuaRC(nil)
	rc.SpawnChildRun = func(_ context.Context, _ string, input string) (string, error) {
		return input + "_ok", nil
	}

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
			if delta, _ := ev.DataJSON["content_delta"].(string); delta == "q1_okq2_okq3_ok" {
				return
			}
		}
	}
	t.Fatal("expected concatenated results from parallel tasks")
}

func TestLuaExecutor_AgentRunParallel_PartialFailure(t *testing.T) {
	ex, _ := NewLuaExecutor(map[string]any{
		"script": `
local tasks = {
  {skill="lite", input="ok"},
  {skill="lite", input="fail"},
}
local results, errs = agent.run_parallel(tasks)
local out = ""
if results[1] ~= nil then out = out .. "r1:" .. results[1] end
if errs[2] ~= nil then out = out .. ";e2:yes" end
context.set_output(out)
`,
	})
	rc := buildLuaRC(nil)
	rc.SpawnChildRun = func(_ context.Context, _ string, input string) (string, error) {
		if input == "fail" {
			return "", errors.New("task failed")
		}
		return input + "_done", nil
	}

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
			if delta, _ := ev.DataJSON["content_delta"].(string); delta == "r1:ok_done;e2:yes" {
				return
			}
		}
	}
	t.Fatal("expected partial failure output")
}

func TestLuaExecutor_AgentRunParallel_ContextAlreadyCancelled(t *testing.T) {
	ex, _ := NewLuaExecutor(map[string]any{
		"script": `
local results, errs = agent.run_parallel({{skill="lite", input="q"}})
if errs == nil then
  context.set_output("early_cancel_err:" .. tostring(results))
else
  context.set_output("early_cancel_ok")
end
`,
	})
	rc := buildLuaRC(nil)
	rc.SpawnChildRun = func(_ context.Context, _ string, _ string) (string, error) {
		return "", errors.New("irrelevant")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	emitter := events.NewEmitter("trace")
	var got []events.RunEvent
	_ = ex.Execute(ctx, rc, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})

	for _, ev := range got {
		if ev.Type == "message.delta" {
			if delta, _ := ev.DataJSON["content_delta"].(string); strings.HasPrefix(delta, "early_cancel") {
				return
			}
		}
	}
	t.Fatal("expected output after cancelled context")
}

func TestLuaExecutor_AgentRunParallel_ExceedsLimit(t *testing.T) {
	// 构造超过 maxParallelTasks 数量的任务
	script := `
local tasks = {}
for i = 1, 33 do
  tasks[i] = {skill="lite", input="q"}
end
local results, errs = agent.run_parallel(tasks)
if errs == nil then
  context.set_output("unexpected_success")
else
  context.set_output("limit_exceeded")
end
`
	ex, _ := NewLuaExecutor(map[string]any{"script": script})
	rc := buildLuaRC(nil)
	rc.SpawnChildRun = func(_ context.Context, _ string, _ string) (string, error) {
		return "x", nil
	}

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
			if delta, _ := ev.DataJSON["content_delta"].(string); delta == "limit_exceeded" {
				return
			}
		}
	}
	t.Fatal("expected limit_exceeded when task count exceeds maxParallelTasks")
}

func TestLuaExecutor_AgentRunParallel_ObservabilityEvents(t *testing.T) {
	ex, _ := NewLuaExecutor(map[string]any{
		"script": `
local tasks = {
  {skill="lite", input="q1"},
  {skill="pro",  input="q2"},
  {skill="lite", input="q3"},
}
local results, errs = agent.run_parallel(tasks)
context.set_output("ok")
`,
	})
	rc := buildLuaRC(nil)
	rc.SpawnChildRun = func(_ context.Context, _ string, input string) (string, error) {
		return input + "_done", nil
	}

	emitter := events.NewEmitter("trace")
	var got []events.RunEvent
	err := ex.Execute(context.Background(), rc, emitter, func(ev events.RunEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var dispatchFound, completeFound bool
	for _, ev := range got {
		if ev.Type == "agent.parallel_dispatch" {
			count, _ := ev.DataJSON["task_count"].(int)
			if count == 3 {
				dispatchFound = true
			}
		}
		if ev.Type == "agent.parallel_complete" {
			success, _ := ev.DataJSON["success_count"].(int)
			errCount, _ := ev.DataJSON["error_count"].(int)
			if success == 3 && errCount == 0 {
				completeFound = true
			}
		}
	}
	if !dispatchFound {
		t.Error("agent.parallel_dispatch event with task_count=3 not found")
	}
	if !completeFound {
		t.Error("agent.parallel_complete event with success_count=3 not found")
	}
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
