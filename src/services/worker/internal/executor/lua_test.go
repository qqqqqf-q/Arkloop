package executor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
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
