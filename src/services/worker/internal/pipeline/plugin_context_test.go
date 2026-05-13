package pipeline

import (
	"context"
	"strings"
	"testing"

	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
)

func TestPluginContextInjectsBeforeMemory(t *testing.T) {
	rc := &RunContext{
		Run: data.Run{ID: uuid.New(), AccountID: uuid.New(), ThreadID: uuid.New()},
		PromptAssembly: PromptAssembly{Segments: []PromptSegment{{
			Name:      "persona.base",
			Target:    PromptTargetSystemPrefix,
			Role:      "system",
			Text:      "persona",
			Stability: PromptStabilityStablePrefix,
		}}},
	}
	rc.SystemPrompt = rc.MaterializedSystemPrompt()

	pluginMW := NewPluginContextMiddlewareWithLoader(func(context.Context, *RunContext) ([]PromptSegment, error) {
		return []PromptSegment{{
			Name:      "plugin.context.demo",
			Target:    PromptTargetSystemPrefix,
			Role:      "system",
			Text:      "plugin context",
			Stability: PromptStabilitySessionPrefix,
		}}, nil
	})
	memoryMW := func(ctx context.Context, rc *RunContext, next RunHandler) error {
		rc.AppendPromptSegment(PromptSegment{
			Name:      "memory.snapshot",
			Target:    PromptTargetSystemPrefix,
			Role:      "system",
			Text:      "memory",
			Stability: PromptStabilitySessionPrefix,
		})
		return next(ctx, rc)
	}

	handler := Build([]RunMiddleware{pluginMW, memoryMW}, func(context.Context, *RunContext) error { return nil })
	if err := handler(context.Background(), rc); err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}

	prompt := rc.MaterializedSystemPrompt()
	personaIndex := strings.Index(prompt, "persona")
	pluginIndex := strings.Index(prompt, "plugin context")
	memoryIndex := strings.Index(prompt, "memory")
	if personaIndex < 0 || pluginIndex < 0 || memoryIndex < 0 {
		t.Fatalf("expected all prompt segments, got %q", prompt)
	}
	if personaIndex >= pluginIndex || pluginIndex >= memoryIndex {
		t.Fatalf("unexpected prompt order: %q", prompt)
	}
	if len(rc.PluginContext) != 1 {
		t.Fatalf("expected plugin context segment on RunContext")
	}
}

func TestPluginManifestContextSkipsPathOnly(t *testing.T) {
	content, ok := pluginManifestContextContent([]byte(`{"context":{"path":"context.md"}}`))
	if ok || content != "" {
		t.Fatalf("expected path-only context to be skipped, got %q", content)
	}
}

func TestPluginManifestHooksRenderLaunchSpec(t *testing.T) {
	raw := []byte(`{"hooks":[{"id":"h","event":"before_tool_use","type":"command","launch_spec":{"command":"${PLUGIN_DATA}/bin/hook","args":["${runtime.cua-driver.path}","${settings.level}","${platform}","${arch}"]}}]}`)
	hooks, err := pluginManifestHooks("plugin-a", raw, map[string]any{"level": "debug"}, map[string]any{"plugin_data": "/tmp/plugin", "cua-driver.path": "/tmp/cua-driver"})
	if err != nil {
		t.Fatalf("plugin manifest hooks: %v", err)
	}
	if len(hooks) != 1 {
		t.Fatalf("expected hook, got %d", len(hooks))
	}
	if got := hooks[0].LaunchSpec["command"]; got != "/tmp/plugin/bin/hook" {
		t.Fatalf("expected rendered command, got %#v", hooks[0].LaunchSpec)
	}
	args, ok := hooks[0].LaunchSpec["args"].([]any)
	if !ok || len(args) != 4 || args[0] != "/tmp/cua-driver" || args[1] != "debug" || args[2] == "" || args[3] == "" {
		t.Fatalf("expected rendered args, got %#v", hooks[0].LaunchSpec["args"])
	}
	if hooks[0].HookConfig.Type != "command" || len(hooks[0].HookConfig.Command) != 1 || hooks[0].HookConfig.Command[0] != "/tmp/plugin/bin/hook" {
		t.Fatalf("expected command hook config, got %#v", hooks[0].HookConfig)
	}
	if len(hooks[0].HookConfig.Args) != 4 || hooks[0].HookConfig.Args[0] != "/tmp/cua-driver" {
		t.Fatalf("expected command args, got %#v", hooks[0].HookConfig.Args)
	}
}

func TestPluginManifestHooksRejectUnresolvedPlaceholder(t *testing.T) {
	raw := []byte(`{"hooks":[{"id":"h","event":"before_tool_use","type":"command","command":"${runtime.missing.path}"}]}`)
	_, err := pluginManifestHooks("plugin-a", raw, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown plugin placeholder") {
		t.Fatalf("expected unresolved placeholder error, got %v", err)
	}
}
