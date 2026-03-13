package acp

import (
	"context"
	"strings"

	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/pipeline"
)

// default ACP agent command (OpenCode as the first ACP agent)
var defaultACPCommand = []string{"opencode", "acp"}

// Executor implements pipeline.AgentExecutor for ACP-compatible agent tasks.
// ACP is a generic session protocol; any agent implementing the ACP interface
// (OpenCode, Aider, custom agents, etc.) can be used by specifying the command
// in executor_config.
type Executor struct {
	command []string // agent launch command, from executor_config or default
}

// NewACPExecutor is the factory for "agent.acp" executor type.
// Supported config keys:
//   - command: []string — agent launch command (default: ["opencode", "acp"])
func NewACPExecutor(config map[string]any) (pipeline.AgentExecutor, error) {
	cmd := defaultACPCommand
	if raw, ok := config["command"]; ok {
		switch v := raw.(type) {
		case []string:
			if len(v) > 0 {
				cmd = v
			}
		case []any:
			var parts []string
			for _, item := range v {
				if s, ok := item.(string); ok {
					parts = append(parts, s)
				}
			}
			if len(parts) > 0 {
				cmd = parts
			}
		}
	}
	return &Executor{command: cmd}, nil
}

func (e *Executor) Execute(
	ctx context.Context,
	rc *pipeline.RunContext,
	emitter events.Emitter,
	yield func(events.RunEvent) error,
) error {
	if rc.Runtime == nil || rc.Runtime.SandboxBaseURL == "" {
		errClass := "acp.sandbox_unavailable"
		return yield(emitter.Emit("run.failed", map[string]any{
			"error_class": errClass,
			"message":     "sandbox not available for ACP execution",
		}, nil, &errClass))
	}

	prompt := extractPrompt(rc.Messages)
	if prompt == "" {
		errClass := "acp.empty_prompt"
		return yield(emitter.Emit("run.failed", map[string]any{
			"error_class": errClass,
			"message":     "no user prompt found in messages",
		}, nil, &errClass))
	}

	cwd := "/workspace"

	// build launch command, append --cwd if not already present
	cmd := append([]string(nil), e.command...)
	hasCwd := false
	for _, arg := range cmd {
		if arg == "--cwd" {
			hasCwd = true
			break
		}
	}
	if !hasCwd && cwd != "" {
		cmd = append(cmd, "--cwd", cwd)
	}

	cfg := BridgeConfig{
		SandboxBaseURL:   rc.Runtime.SandboxBaseURL,
		SandboxAuthToken: rc.Runtime.SandboxAuthToken,
		SessionID:        rc.Run.ID.String(),
		AccountID:        rc.Run.AccountID.String(),
		Command:          cmd,
		Cwd:              cwd,
	}

	client := NewClient(cfg.SandboxBaseURL, cfg.SandboxAuthToken)
	bridge := NewBridge(client, cfg)
	return bridge.Run(ctx, prompt, emitter, yield)
}

// extractPrompt returns the combined text of the last user message.
func extractPrompt(messages []llm.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		var parts []string
		for _, p := range messages[i].Content {
			if t := strings.TrimSpace(llm.PartPromptText(p)); t != "" {
				parts = append(parts, t)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return ""
}
