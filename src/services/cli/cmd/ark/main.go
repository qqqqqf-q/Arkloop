package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"arkloop/services/cli/internal/apiclient"
	"arkloop/services/cli/internal/renderer"
	"arkloop/services/cli/internal/repl"
	"arkloop/services/cli/internal/runner"
	"arkloop/services/cli/internal/sse"
)

type exitError struct {
	code int
}

func (e *exitError) Error() string { return fmt.Sprintf("exit %d", e.code) }

func main() {
	err := run()
	if err == nil {
		return
	}
	var ee *exitError
	if errors.As(err, &ee) {
		os.Exit(ee.code)
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func run() error {
	if len(os.Args) < 2 {
		printUsage()
		return &exitError{2}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	switch os.Args[1] {
	case "run":
		return cmdRun(ctx, os.Args[2:])
	case "chat":
		return cmdChat(ctx, os.Args[2:])
	default:
		printUsage()
		return &exitError{2}
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `usage: ark <command> [flags]

commands:
  run   <prompt>    execute a single run and exit
  chat              interactive multi-turn conversation`)
}

// resolveToken 按优先级解析 token：flag > 环境变量 > ~/.arkloop/desktop.token > 默认值。
func resolveToken(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := strings.TrimSpace(os.Getenv("ARKLOOP_DESKTOP_TOKEN")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err == nil {
		if data, err := os.ReadFile(filepath.Join(home, ".arkloop", "desktop.token")); err == nil {
			if v := strings.TrimSpace(string(data)); v != "" {
				return v
			}
		}
	}
	return apiclient.DefaultToken
}

func cmdRun(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	host := fs.String("host", apiclient.DefaultBaseURL, "desktop API address")
	token := fs.String("token", "", "bearer token")
	timeout := fs.Duration("timeout", 5*time.Minute, "run timeout")
	persona := fs.String("persona", "", "persona_id")
	model := fs.String("model", "", "model key")
	workDir := fs.String("work-dir", "", "working directory")
	reasoning := fs.String("reasoning", "", "reasoning_mode")
	threadID := fs.String("thread", "", "reuse existing thread")
	outputFormat := fs.String("output-format", "text", "output format: text, json, stream-json")
	fs.Parse(args)

	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: ark run [flags] <prompt>")
		return &exitError{2}
	}
	prompt := fs.Arg(0)

	client := apiclient.NewClient(*host, resolveToken(*token))
	params := apiclient.RunParams{
		PersonaID:     *persona,
		Model:         *model,
		WorkDir:       *workDir,
		ReasoningMode: *reasoning,
	}

	runCtx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	switch *outputFormat {
	case "text":
		return runText(runCtx, client, *threadID, prompt, params)
	case "json":
		return runJSON(runCtx, client, *threadID, prompt, params)
	case "stream-json":
		return runStreamJSON(runCtx, client, *threadID, prompt, params)
	default:
		return fmt.Errorf("unknown output format: %s", *outputFormat)
	}
}

func runText(ctx context.Context, client *apiclient.Client, threadID, prompt string, params apiclient.RunParams) error {
	r := renderer.NewRenderer(os.Stdout)
	result, err := runner.Execute(ctx, client, threadID, prompt, params, r.OnEvent)
	r.Flush()
	if err != nil {
		return err
	}
	if result.IsError {
		return &exitError{1}
	}
	return nil
}

func runJSON(ctx context.Context, client *apiclient.Client, threadID, prompt string, params apiclient.RunParams) error {
	result, err := runner.Execute(ctx, client, threadID, prompt, params, nil)
	if err != nil {
		return err
	}

	out := map[string]any{
		"type":        "result",
		"thread_id":   result.ThreadID,
		"run_id":      result.RunID,
		"status":      result.Status,
		"result":      result.Output,
		"duration_ms": result.DurationMs,
		"tool_calls":  result.ToolCalls,
		"is_error":    result.IsError,
	}
	if result.Error != "" {
		out["error"] = result.Error
	}

	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	if result.IsError {
		return &exitError{1}
	}
	return nil
}

func runStreamJSON(ctx context.Context, client *apiclient.Client, threadID, prompt string, params apiclient.RunParams) error {
	enc := json.NewEncoder(os.Stdout)

	onEvent := func(e sse.Event) {
		line := map[string]any{
			"type": e.Type,
			"seq":  e.Seq,
		}
		if e.ToolName != "" {
			line["tool_name"] = e.ToolName
		}
		for k, v := range e.Data {
			line[k] = v
		}
		enc.Encode(line)
	}

	result, err := runner.Execute(ctx, client, threadID, prompt, params, onEvent)
	if err != nil {
		return err
	}

	final := map[string]any{
		"type":        "result",
		"thread_id":   result.ThreadID,
		"run_id":      result.RunID,
		"status":      result.Status,
		"result":      result.Output,
		"duration_ms": result.DurationMs,
		"tool_calls":  result.ToolCalls,
		"is_error":    result.IsError,
	}
	if result.Error != "" {
		final["error"] = result.Error
	}
	enc.Encode(final)

	if result.IsError {
		return &exitError{1}
	}
	return nil
}

func cmdChat(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("chat", flag.ExitOnError)
	host := fs.String("host", apiclient.DefaultBaseURL, "desktop API address")
	token := fs.String("token", "", "bearer token")
	timeout := fs.Duration("timeout", 5*time.Minute, "per-turn timeout")
	persona := fs.String("persona", "", "persona_id")
	model := fs.String("model", "", "model key")
	workDir := fs.String("work-dir", "", "working directory")
	threadID := fs.String("thread", "", "continue from existing thread")
	fs.Parse(args)

	client := apiclient.NewClient(*host, resolveToken(*token))
	params := apiclient.RunParams{
		PersonaID: *persona,
		Model:     *model,
		WorkDir:   *workDir,
	}

	r := repl.NewREPL(client, params, *threadID, *timeout)
	return r.Run(ctx)
}
