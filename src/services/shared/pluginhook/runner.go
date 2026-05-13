package pluginhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type CommandHookRunner struct {
	config HookConfig
}

type HTTPHookRunner struct {
	config HookConfig
	client *http.Client
}

func NewCommandHookRunner(config HookConfig) *CommandHookRunner {
	return &CommandHookRunner{config: config}
}

func NewHTTPHookRunner(config HookConfig, client *http.Client) *HTTPHookRunner {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPHookRunner{config: config, client: client}
}

func (r *CommandHookRunner) Run(ctx context.Context, input HookInput) (HookOutput, error) {
	ctx, cancel := withTimeout(ctx, r.config.TimeoutMS)
	defer cancel()
	input = r.normalizeInput(input)
	encoded, err := json.Marshal(input)
	if err != nil {
		return HookOutput{}, fmt.Errorf("marshal hook input: %w", err)
	}
	command := r.command()
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return HookOutput{}, fmt.Errorf("plugin command hook command must not be empty")
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Stdin = bytes.NewReader(encoded)
	cmd.Env = append(os.Environ(),
		"ARKLOOP_PLUGIN_ID="+r.config.PluginID,
		"ARKLOOP_PLUGIN_DATA="+r.config.PluginData,
		"ARKLOOP_RUN_ID="+input.RunID,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return continueWithError("plugin command hook timeout"), nil
		}
		return HookOutput{}, fmt.Errorf("run plugin command hook: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	output, err := decodeOutput(stdout.Bytes(), input.Event)
	if err != nil {
		return HookOutput{}, err
	}
	return output, nil
}

func (r *HTTPHookRunner) Run(ctx context.Context, input HookInput) (HookOutput, error) {
	ctx, cancel := withTimeout(ctx, r.config.TimeoutMS)
	defer cancel()
	input = r.normalizeInput(input)
	encoded, err := json.Marshal(input)
	if err != nil {
		return HookOutput{}, fmt.Errorf("marshal hook input: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(r.config.URL), bytes.NewReader(encoded))
	if err != nil {
		return HookOutput{}, fmt.Errorf("create plugin http hook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range r.config.Headers {
		req.Header.Set(key, value)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return continueWithError("plugin http hook timeout"), nil
		}
		return HookOutput{}, fmt.Errorf("post plugin http hook: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return HookOutput{}, fmt.Errorf("plugin http hook status: %d", resp.StatusCode)
	}
	var buffer bytes.Buffer
	if _, err := buffer.ReadFrom(resp.Body); err != nil {
		return HookOutput{}, fmt.Errorf("read plugin http hook output: %w", err)
	}
	return decodeOutput(buffer.Bytes(), input.Event)
}

func (r *CommandHookRunner) normalizeInput(input HookInput) HookInput {
	if input.Event == "" {
		input.Event = r.config.Event
	}
	input.Event = NormalizeEvent(input.Event)
	if input.PluginID == "" {
		input.PluginID = r.config.PluginID
	}
	return input
}

func (r *CommandHookRunner) command() []string {
	command := append([]string(nil), r.config.Command...)
	if len(r.config.Args) > 0 {
		command = append(command, r.config.Args...)
	}
	return command
}

func (r *HTTPHookRunner) normalizeInput(input HookInput) HookInput {
	if input.Event == "" {
		input.Event = r.config.Event
	}
	input.Event = NormalizeEvent(input.Event)
	if input.PluginID == "" {
		input.PluginID = r.config.PluginID
	}
	return input
}

func decodeOutput(data []byte, event HookEvent) (HookOutput, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return HookOutput{Action: ActionContinue}, nil
	}
	var output HookOutput
	if err := json.Unmarshal(data, &output); err != nil {
		return HookOutput{}, fmt.Errorf("decode plugin hook output: %w", err)
	}
	if err := output.validate(event); err != nil {
		return HookOutput{}, err
	}
	if output.Action == "" {
		output.Action = ActionContinue
	}
	return output, nil
}

func withTimeout(ctx context.Context, timeoutMS int) (context.Context, context.CancelFunc) {
	if timeoutMS <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
}
