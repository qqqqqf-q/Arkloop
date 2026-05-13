package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

type pluginHookRunnerFunc func(context.Context, PluginHookInvocation) (PluginHookResult, error)

func (f pluginHookRunnerFunc) RunPluginHook(ctx context.Context, inv PluginHookInvocation) (PluginHookResult, error) {
	return f(ctx, inv)
}

func TestPluginBeforeToolUseModifyArgs(t *testing.T) {
	rc := &RunContext{
		Run: dataRunForPluginTest(),
		PluginHooks: []PluginHookConfig{{
			PluginID: "test-plugin",
			HookID:   "before",
			Event:    PluginHookBeforeToolUse,
		}},
		PluginHookRunner: pluginHookRunnerFunc(func(_ context.Context, inv PluginHookInvocation) (PluginHookResult, error) {
			if inv.ToolCall.ToolName != "echo" {
				t.Fatalf("unexpected tool name: %q", inv.ToolCall.ToolName)
			}
			return PluginHookResult{
				Decision:     "modify",
				ModifiedArgs: map[string]any{"text": "modified"},
			}, nil
		}),
	}

	result := RunPluginBeforeToolUse(context.Background(), rc, llm.ToolCall{
		ToolName:      "echo",
		ArgumentsJSON: map[string]any{"text": "original"},
	})

	if result.Result != nil {
		t.Fatalf("expected modified call to continue")
	}
	if result.Call.ToolName != "echo" {
		t.Fatalf("plugin must not modify tool name, got %q", result.Call.ToolName)
	}
	if got := result.Call.ArgumentsJSON["text"]; got != "modified" {
		t.Fatalf("expected modified args, got %#v", result.Call.ArgumentsJSON)
	}
}

func TestPluginBeforeToolUseDenyReturnsPolicyDenied(t *testing.T) {
	rc := &RunContext{
		Run: dataRunForPluginTest(),
		PluginHooks: []PluginHookConfig{{
			PluginID: "test-plugin",
			HookID:   "before",
			Event:    PluginHookBeforeToolUse,
		}},
		PluginHookRunner: pluginHookRunnerFunc(func(context.Context, PluginHookInvocation) (PluginHookResult, error) {
			return PluginHookResult{Decision: "deny", Message: "blocked"}, nil
		}),
	}

	result := RunPluginBeforeToolUse(context.Background(), rc, llm.ToolCall{ToolName: "echo"})
	if result.Result == nil || result.Result.Error == nil {
		t.Fatalf("expected denied execution result")
	}
	if result.Result.Error.ErrorClass != tools.PolicyDeniedCode {
		t.Fatalf("expected policy.denied, got %q", result.Result.Error.ErrorClass)
	}
}

func TestPluginHookTimeoutContinues(t *testing.T) {
	rc := &RunContext{
		Run: dataRunForPluginTest(),
		PluginHooks: []PluginHookConfig{{
			PluginID: "test-plugin",
			HookID:   "before",
			Event:    PluginHookBeforeToolUse,
			Timeout:  time.Millisecond,
		}},
		PluginHookRunner: pluginHookRunnerFunc(func(ctx context.Context, _ PluginHookInvocation) (PluginHookResult, error) {
			<-ctx.Done()
			return PluginHookResult{}, ctx.Err()
		}),
	}

	result := RunPluginBeforeToolUse(context.Background(), rc, llm.ToolCall{
		ToolName:      "echo",
		ArgumentsJSON: map[string]any{"text": "original"},
	})
	if result.Result != nil {
		t.Fatalf("expected timeout to continue")
	}
	if got := result.Call.ArgumentsJSON["text"]; got != "original" {
		t.Fatalf("expected original args after timeout, got %v", got)
	}
}

func TestPluginBeforeModelInjectsSystemSegment(t *testing.T) {
	rc := &RunContext{
		Run: dataRunForPluginTest(),
		PluginHooks: []PluginHookConfig{{
			PluginID: "test-plugin",
			HookID:   "before-model",
			Event:    PluginHookBeforeModelCall,
		}},
		PluginHookRunner: pluginHookRunnerFunc(func(context.Context, PluginHookInvocation) (PluginHookResult, error) {
			return PluginHookResult{InjectSegments: []PromptSegment{{
				Name:   "segment",
				Target: PromptTargetSystemPrefix,
				Role:   "system",
				Text:   "plugin model context",
			}}}, nil
		}),
	}

	request, err := RunPluginBeforeModelCall(context.Background(), rc, llm.Request{
		Messages: []llm.Message{{Role: "user", Content: []llm.TextPart{{Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("RunPluginBeforeModelCall: %v", err)
	}
	if len(request.Messages) != 2 || request.Messages[0].Role != "system" {
		t.Fatalf("expected injected system message, got %#v", request.Messages)
	}
	if strings.Contains(rc.SystemPrompt, "plugin model context") {
		t.Fatalf("plugin before_model injection must not persist into run context, got %q", rc.SystemPrompt)
	}
	request, err = RunPluginBeforeModelCall(context.Background(), rc, llm.Request{
		Messages: []llm.Message{{Role: "user", Content: []llm.TextPart{{Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("RunPluginBeforeModelCall second call: %v", err)
	}
	if len(request.Messages) != 2 {
		t.Fatalf("expected per-call injection without accumulation, got %#v", request.Messages)
	}
}

func TestPluginBeforeModelDenyReturnsError(t *testing.T) {
	rc := &RunContext{
		Run: dataRunForPluginTest(),
		PluginHooks: []PluginHookConfig{{
			PluginID: "test-plugin",
			HookID:   "before-model",
			Event:    PluginHookBeforeModelCall,
		}},
		PluginHookRunner: pluginHookRunnerFunc(func(context.Context, PluginHookInvocation) (PluginHookResult, error) {
			return PluginHookResult{Decision: "deny", Message: "no model call"}, nil
		}),
	}

	_, err := RunPluginBeforeModelCall(context.Background(), rc, llm.Request{})
	if !errors.Is(err, ErrPluginModelDenied) {
		t.Fatalf("expected ErrPluginModelDenied, got %v", err)
	}
}

func TestPluginHooksStayOutOfGlobalRegistry(t *testing.T) {
	registry := NewHookRegistry()
	rc := &RunContext{Run: dataRunForPluginTest(), HookRegistry: registry}
	mw := NewPluginHooksMiddlewareWithLoader(func(context.Context, *RunContext) ([]PluginHookConfig, error) {
		return []PluginHookConfig{{PluginID: "p", HookID: "h", Event: PluginHookBeforeModelCall}}, nil
	})

	err := mw(context.Background(), rc, func(context.Context, *RunContext) error { return nil })
	if err != nil {
		t.Fatalf("middleware failed: %v", err)
	}
	if len(rc.PluginHooks) != 1 {
		t.Fatalf("expected per-run plugin hook")
	}
	if got := len(registry.beforeModelHooks()); got != 0 {
		t.Fatalf("plugin hook leaked into global registry: %d", got)
	}
}

func dataRunForPluginTest() data.Run {
	return data.Run{ID: uuid.New(), AccountID: uuid.New(), ThreadID: uuid.New()}
}
