package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"arkloop/services/cli/internal/apiclient"
	"arkloop/services/cli/internal/formatter"
	"arkloop/services/cli/internal/renderer"
	"arkloop/services/cli/internal/repl"
	"arkloop/services/cli/internal/runner"
	"arkloop/services/cli/internal/sse"
)

type exitError struct {
	code int
}

func (e *exitError) Error() string { return fmt.Sprintf("exit %d", e.code) }

var errRunUsage = errors.New("run usage")
var version = "dev"

type commandRoute string

const (
	commandRun          commandRoute = "run"
	commandVersion      commandRoute = "version"
	commandChat         commandRoute = "chat"
	commandStatus       commandRoute = "status"
	commandModelsList   commandRoute = "models.list"
	commandPersonasList commandRoute = "personas.list"
	commandSessionsList commandRoute = "sessions.list"
	commandSessionsChat commandRoute = "sessions.resume"
)

type routedCommand struct {
	kind commandRoute
	args []string
}

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

	route, err := routeCommand(os.Args[1:])
	if err != nil {
		printUsage()
		return &exitError{2}
	}

	switch route.kind {
	case commandVersion:
		return cmdVersion(route.args)
	case commandRun:
		return cmdRun(ctx, route.args)
	case commandChat:
		return cmdChat(ctx, route.args)
	case commandStatus:
		return cmdStatus(ctx, route.args)
	case commandModelsList:
		return cmdModelsList(ctx, route.args)
	case commandPersonasList:
		return cmdPersonasList(ctx, route.args)
	case commandSessionsList:
		return cmdSessionsList(ctx, route.args)
	case commandSessionsChat:
		return cmdSessionsResume(ctx, route.args)
	default:
		printUsage()
		return &exitError{2}
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `usage: ark <command> [flags]

commands:
  version                        print CLI version
  run <prompt>                   execute a single run and exit
  chat                           interactive multi-turn conversation
  status                         show current desktop connection status
  models list                    list configured models
  personas list                  list selectable personas
  sessions list                  list recent sessions
  sessions resume <session-id>   resume an existing session`)
}

func routeCommand(args []string) (routedCommand, error) {
	if len(args) == 0 {
		return routedCommand{}, errors.New("missing command")
	}

	switch args[0] {
	case "--version":
		return routedCommand{kind: commandVersion, args: args[1:]}, nil
	case "version":
		return routedCommand{kind: commandVersion, args: args[1:]}, nil
	case "run":
		return routedCommand{kind: commandRun, args: args[1:]}, nil
	case "chat":
		return routedCommand{kind: commandChat, args: args[1:]}, nil
	case "status":
		return routedCommand{kind: commandStatus, args: args[1:]}, nil
	case "models":
		if len(args) >= 2 && args[1] == "list" {
			return routedCommand{kind: commandModelsList, args: args[2:]}, nil
		}
	case "personas":
		if len(args) >= 2 && args[1] == "list" {
			return routedCommand{kind: commandPersonasList, args: args[2:]}, nil
		}
	case "sessions":
		if len(args) >= 2 {
			switch args[1] {
			case "list":
				return routedCommand{kind: commandSessionsList, args: args[2:]}, nil
			case "resume":
				return routedCommand{kind: commandSessionsChat, args: args[2:]}, nil
			}
		}
	}

	return routedCommand{}, fmt.Errorf("unknown command")
}

func cmdVersion(args []string) error {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: ark version")
		return &exitError{2}
	}
	return writeVersion(os.Stdout)
}

func writeVersion(output io.Writer) error {
	_, err := fmt.Fprintf(output, "ark version %s\n", version)
	return err
}

// resolveToken 按优先级解析 token：flag > 环境变量 > ~/.arkloop/desktop.token > 默认值。
func resolveToken(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := strings.TrimSpace(os.Getenv("ARKLOOP_TOKEN")); v != "" {
		return v
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

type desktopConfig struct {
	Mode  string `json:"mode"`
	Local struct {
		Port int `json:"port"`
	} `json:"local"`
}

// resolveHost 按优先级解析 host：显式 flag > ~/.arkloop/config.json(local.port) > 默认值。
func resolveHost(flagValue string, flagProvided bool) string {
	if flagProvided && strings.TrimSpace(flagValue) != "" {
		return strings.TrimSpace(flagValue)
	}
	if v := strings.TrimSpace(os.Getenv("ARKLOOP_HOST")); v != "" {
		return v
	}

	home, err := os.UserHomeDir()
	if err == nil {
		raw, err := os.ReadFile(filepath.Join(home, ".arkloop", "config.json"))
		if err == nil {
			var cfg desktopConfig
			if err := json.Unmarshal(raw, &cfg); err == nil &&
				cfg.Mode == "local" &&
				cfg.Local.Port > 0 &&
				cfg.Local.Port <= 65535 {
				return fmt.Sprintf("http://127.0.0.1:%d", cfg.Local.Port)
			}
		}
	}

	if strings.TrimSpace(flagValue) != "" {
		return strings.TrimSpace(flagValue)
	}
	return apiclient.DefaultBaseURL
}

func flagWasProvided(fs *flag.FlagSet, name string) bool {
	provided := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			provided = true
		}
	})
	return provided
}

func newClientFromFlags(host, token string, fs *flag.FlagSet) *apiclient.Client {
	return apiclient.NewClient(resolveHost(host, flagWasProvided(fs, "host")), resolveToken(token))
}

func ensureOutputFormat(outputFormat string) error {
	switch outputFormat {
	case formatter.OutputText, formatter.OutputJSON:
		return nil
	default:
		return fmt.Errorf("unknown output format: %s", outputFormat)
	}
}

func splitFlagAndPositionalArgs(args []string, valueFlags map[string]struct{}) ([]string, []string, error) {
	flagArgs := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		token := args[i]
		if strings.HasPrefix(token, "-") && token != "-" {
			flagArgs = append(flagArgs, token)
			if !flagConsumesValue(token, valueFlags) {
				continue
			}
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("missing value for %s", token)
			}
			i++
			flagArgs = append(flagArgs, args[i])
			continue
		}
		positionals = append(positionals, token)
	}

	return flagArgs, positionals, nil
}

func flagConsumesValue(token string, valueFlags map[string]struct{}) bool {
	if strings.Contains(token, "=") {
		return false
	}
	name := strings.TrimLeft(token, "-")
	_, ok := valueFlags[name]
	return ok
}

func stdinHasData(stdin *os.File) bool {
	info, err := stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) == 0
}

func loadPrompt(prompt string, promptFile string, stdin io.Reader, allowImplicitStdin bool) (string, error) {
	if strings.TrimSpace(promptFile) != "" && prompt != "" {
		return "", errRunUsage
	}
	if strings.TrimSpace(promptFile) != "" {
		if promptFile == "-" {
			data, err := io.ReadAll(stdin)
			if err != nil {
				return "", fmt.Errorf("read prompt from stdin: %w", err)
			}
			return string(data), nil
		}
		data, err := os.ReadFile(promptFile)
		if err != nil {
			return "", fmt.Errorf("read prompt file: %w", err)
		}
		return string(data), nil
	}
	if prompt == "" {
		if allowImplicitStdin {
			data, err := io.ReadAll(stdin)
			if err != nil {
				return "", fmt.Errorf("read prompt from stdin: %w", err)
			}
			return string(data), nil
		}
		return "", errRunUsage
	}
	if prompt != "-" {
		return prompt, nil
	}

	data, err := io.ReadAll(stdin)
	if err != nil {
		return "", fmt.Errorf("read prompt from stdin: %w", err)
	}
	return string(data), nil
}

func printRunUsage() {
	fmt.Fprintln(os.Stderr, "usage: ark run [flags] <prompt>")
}

func requestedRunOutputFormat(args []string) string {
	for i := 0; i < len(args); i++ {
		token := args[i]
		if token == "--" {
			return formatter.OutputText
		}
		if token == "-output-format" || token == "--output-format" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return formatter.OutputText
		}
		if strings.HasPrefix(token, "-output-format=") {
			return strings.TrimPrefix(token, "-output-format=")
		}
		if strings.HasPrefix(token, "--output-format=") {
			return strings.TrimPrefix(token, "--output-format=")
		}
	}
	return formatter.OutputText
}

func writeRunErrorResult(output io.Writer, err error) error {
	return json.NewEncoder(output).Encode(runErrorLine(err))
}

func handleRunCommandError(outputFormat string, err error, exitCode int) error {
	if outputFormat == formatter.OutputJSON || outputFormat == "stream-json" {
		if encodeErr := writeRunErrorResult(os.Stdout, err); encodeErr != nil {
			return fmt.Errorf("encode error result: %w", encodeErr)
		}
		return &exitError{exitCode}
	}
	if exitCode == 2 {
		printRunUsage()
		return &exitError{2}
	}
	return err
}

func cmdRun(ctx context.Context, args []string) error {
	valueFlags := map[string]struct{}{
		"host":          {},
		"token":         {},
		"timeout":       {},
		"persona":       {},
		"model":         {},
		"work-dir":      {},
		"reasoning":     {},
		"thread":        {},
		"output-format": {},
		"prompt-file":   {},
	}
	requestedFormat := requestedRunOutputFormat(args)
	flagArgs, positionals, err := splitFlagAndPositionalArgs(args, valueFlags)
	if err != nil {
		return handleRunCommandError(requestedFormat, err, 2)
	}

	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	host := fs.String("host", apiclient.DefaultBaseURL, "desktop API address")
	token := fs.String("token", "", "bearer token")
	timeout := fs.Duration("timeout", 0, "run timeout, 0 disables timeout")
	persona := fs.String("persona", "", "persona_id")
	model := fs.String("model", "", "model key")
	workDir := fs.String("work-dir", "", "working directory")
	reasoning := fs.String("reasoning", "", "reasoning_mode")
	threadID := fs.String("thread", "", "reuse existing thread")
	outputFormat := fs.String("output-format", "text", "output format: text, json, stream-json")
	promptFile := fs.String("prompt-file", "", "load prompt from file path, use - for stdin")
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printRunUsage()
			fs.SetOutput(os.Stderr)
			fs.PrintDefaults()
			return nil
		}
		return handleRunCommandError(requestedFormat, err, 2)
	}
	switch *outputFormat {
	case formatter.OutputText, formatter.OutputJSON, "stream-json":
	default:
		return handleRunCommandError(*outputFormat, fmt.Errorf("unknown output format: %s", *outputFormat), 2)
	}

	prompt := ""
	if len(positionals) > 1 {
		return handleRunCommandError(*outputFormat, errRunUsage, 2)
	}
	if len(positionals) == 1 {
		prompt = positionals[0]
	}

	prompt, err = loadPrompt(prompt, *promptFile, os.Stdin, stdinHasData(os.Stdin))
	if err != nil {
		exitCode := 1
		if errors.Is(err, errRunUsage) {
			exitCode = 2
		}
		return handleRunCommandError(*outputFormat, err, exitCode)
	}

	client := newClientFromFlags(*host, *token, fs)
	params := apiclient.RunParams{
		PersonaID:     *persona,
		Model:         *model,
		WorkDir:       *workDir,
		ReasoningMode: *reasoning,
	}

	runCtx, cancel := withOptionalTimeout(ctx, *timeout)
	defer cancel()

	switch *outputFormat {
	case "text":
		return runText(runCtx, client, *threadID, prompt, params)
	case "json":
		return runJSON(runCtx, os.Stdout, client, *threadID, prompt, params)
	case "stream-json":
		return runStreamJSON(runCtx, os.Stdout, client, *threadID, prompt, params)
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

func runResultLine(result runner.RunResult) map[string]any {
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
	return out
}

func runErrorLine(err error) map[string]any {
	return map[string]any{
		"type":        "result",
		"thread_id":   "",
		"run_id":      "",
		"status":      "error",
		"result":      "",
		"duration_ms": 0,
		"tool_calls":  0,
		"is_error":    true,
		"error":       err.Error(),
	}
}

func runJSON(ctx context.Context, output io.Writer, client *apiclient.Client, threadID, prompt string, params apiclient.RunParams) error {
	enc := json.NewEncoder(output)
	result, err := runner.Execute(ctx, client, threadID, prompt, params, nil)
	if err != nil {
		if encodeErr := enc.Encode(runErrorLine(err)); encodeErr != nil {
			return fmt.Errorf("encode error result: %w", encodeErr)
		}
		return &exitError{1}
	}

	if err := enc.Encode(runResultLine(result)); err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	if result.IsError {
		return &exitError{1}
	}
	return nil
}

func runStreamJSON(ctx context.Context, output io.Writer, client *apiclient.Client, threadID, prompt string, params apiclient.RunParams) error {
	enc := json.NewEncoder(output)
	var eventEncodeErr error

	onEvent := func(e sse.Event) {
		if eventEncodeErr != nil {
			return
		}
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
		if err := enc.Encode(line); err != nil {
			eventEncodeErr = err
		}
	}

	result, err := runner.Execute(ctx, client, threadID, prompt, params, onEvent)
	if eventEncodeErr != nil {
		return fmt.Errorf("encode stream event: %w", eventEncodeErr)
	}
	if err != nil {
		if encodeErr := enc.Encode(runErrorLine(err)); encodeErr != nil {
			return fmt.Errorf("encode error result: %w", encodeErr)
		}
		return &exitError{1}
	}

	if err := enc.Encode(runResultLine(result)); err != nil {
		return fmt.Errorf("encode result: %w", err)
	}

	if result.IsError {
		return &exitError{1}
	}
	return nil
}

func cmdStatus(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	host := fs.String("host", apiclient.DefaultBaseURL, "desktop API address")
	token := fs.String("token", "", "bearer token")
	outputFormat := fs.String("output-format", formatter.OutputText, "output format: text, json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: ark status [flags]")
		return &exitError{2}
	}
	if err := ensureOutputFormat(*outputFormat); err != nil {
		return err
	}

	client := newClientFromFlags(*host, *token, fs)
	me, err := client.GetMe(ctx)
	if err != nil {
		return err
	}

	return formatter.PrintStatus(os.Stdout, *outputFormat, formatter.StatusView{
		Host:        client.BaseURL(),
		Connected:   true,
		UserID:      me.ID,
		Username:    me.Username,
		AccountID:   me.AccountID,
		WorkEnabled: me.WorkEnabled,
	})
}

func cmdModelsList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("models list", flag.ExitOnError)
	host := fs.String("host", apiclient.DefaultBaseURL, "desktop API address")
	token := fs.String("token", "", "bearer token")
	outputFormat := fs.String("output-format", formatter.OutputText, "output format: text, json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: ark models list [flags]")
		return &exitError{2}
	}
	if err := ensureOutputFormat(*outputFormat); err != nil {
		return err
	}

	client := newClientFromFlags(*host, *token, fs)
	providers, err := client.ListLlmProviders(ctx)
	if err != nil {
		return err
	}

	return formatter.PrintModels(os.Stdout, *outputFormat, modelViewsFromProviders(providers))
}

func cmdPersonasList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("personas list", flag.ExitOnError)
	host := fs.String("host", apiclient.DefaultBaseURL, "desktop API address")
	token := fs.String("token", "", "bearer token")
	outputFormat := fs.String("output-format", formatter.OutputText, "output format: text, json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: ark personas list [flags]")
		return &exitError{2}
	}
	if err := ensureOutputFormat(*outputFormat); err != nil {
		return err
	}

	client := newClientFromFlags(*host, *token, fs)
	personas, err := client.ListSelectablePersonas(ctx)
	if err != nil {
		return err
	}

	return formatter.PrintPersonas(os.Stdout, *outputFormat, personaViews(personas))
}

func cmdSessionsList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("sessions list", flag.ExitOnError)
	host := fs.String("host", apiclient.DefaultBaseURL, "desktop API address")
	token := fs.String("token", "", "bearer token")
	outputFormat := fs.String("output-format", formatter.OutputText, "output format: text, json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: ark sessions list [flags]")
		return &exitError{2}
	}
	if err := ensureOutputFormat(*outputFormat); err != nil {
		return err
	}

	client := newClientFromFlags(*host, *token, fs)
	threads, err := client.ListAllThreads(ctx)
	if err != nil {
		return err
	}

	return formatter.PrintSessions(os.Stdout, *outputFormat, sessionViews(threads))
}

func cmdSessionsResume(ctx context.Context, args []string) error {
	valueFlags := map[string]struct{}{
		"host":     {},
		"token":    {},
		"timeout":  {},
		"persona":  {},
		"model":    {},
		"work-dir": {},
	}
	flagArgs, positionals, err := splitFlagAndPositionalArgs(args, valueFlags)
	if err != nil {
		fmt.Fprintln(os.Stderr, "usage: ark sessions resume [flags] <session-id>")
		return &exitError{2}
	}

	fs := flag.NewFlagSet("sessions resume", flag.ExitOnError)
	host := fs.String("host", apiclient.DefaultBaseURL, "desktop API address")
	token := fs.String("token", "", "bearer token")
	timeout := fs.Duration("timeout", 0, "per-turn timeout, 0 disables timeout")
	persona := fs.String("persona", "", "persona_id")
	model := fs.String("model", "", "model key")
	workDir := fs.String("work-dir", "", "working directory")
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}

	if len(positionals) != 1 {
		fmt.Fprintln(os.Stderr, "usage: ark sessions resume [flags] <session-id>")
		return &exitError{2}
	}

	client := newClientFromFlags(*host, *token, fs)
	params := apiclient.RunParams{
		PersonaID: *persona,
		Model:     *model,
		WorkDir:   *workDir,
	}

	r := repl.NewREPL(client, params, positionals[0], *timeout)
	return r.Run(ctx)
}

func cmdChat(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("chat", flag.ExitOnError)
	host := fs.String("host", apiclient.DefaultBaseURL, "desktop API address")
	token := fs.String("token", "", "bearer token")
	timeout := fs.Duration("timeout", 0, "per-turn timeout, 0 disables timeout")
	persona := fs.String("persona", "", "persona_id")
	model := fs.String("model", "", "model key")
	workDir := fs.String("work-dir", "", "working directory")
	threadID := fs.String("thread", "", "continue from existing thread")
	if err := fs.Parse(args); err != nil {
		return err
	}

	client := newClientFromFlags(*host, *token, fs)
	params := apiclient.RunParams{
		PersonaID: *persona,
		Model:     *model,
		WorkDir:   *workDir,
	}

	r := repl.NewREPL(client, params, *threadID, *timeout)
	return r.Run(ctx)
}

func modelViewsFromProviders(providers []apiclient.LlmProvider) []formatter.ModelView {
	views := make([]formatter.ModelView, 0)
	for _, provider := range providers {
		for _, model := range provider.Models {
			tags := append([]string{}, model.Tags...)
			views = append(views, formatter.ModelView{
				Model:        model.Model,
				ProviderID:   model.ProviderID,
				ProviderName: provider.Name,
				IsDefault:    model.IsDefault,
				ShowInPicker: model.ShowInPicker,
				Tags:         tags,
			})
		}
	}

	sort.SliceStable(views, func(i, j int) bool {
		left := views[i]
		right := views[j]
		if left.IsDefault != right.IsDefault {
			return left.IsDefault
		}
		if left.ShowInPicker != right.ShowInPicker {
			return left.ShowInPicker
		}
		if left.ProviderName != right.ProviderName {
			return left.ProviderName < right.ProviderName
		}
		return left.Model < right.Model
	})
	return views
}

func personaViews(personas []apiclient.Persona) []formatter.PersonaView {
	sorted := append([]apiclient.Persona(nil), personas...)

	sort.SliceStable(sorted, func(i, j int) bool {
		left := sorted[i]
		right := sorted[j]
		leftOrder := left.SelectorOrder
		if leftOrder == 0 {
			leftOrder = 99
		}
		rightOrder := right.SelectorOrder
		if rightOrder == 0 {
			rightOrder = 99
		}
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}

		leftName := strings.TrimSpace(left.SelectorName)
		if leftName == "" {
			leftName = strings.TrimSpace(left.DisplayName)
		}
		if leftName == "" {
			leftName = left.PersonaKey
		}
		rightName := strings.TrimSpace(right.SelectorName)
		if rightName == "" {
			rightName = strings.TrimSpace(right.DisplayName)
		}
		if rightName == "" {
			rightName = right.PersonaKey
		}
		if leftName != rightName {
			return leftName < rightName
		}
		return left.PersonaKey < right.PersonaKey
	})

	views := make([]formatter.PersonaView, 0, len(sorted))
	for _, persona := range sorted {
		views = append(views, formatter.PersonaView{
			PersonaKey:    persona.PersonaKey,
			SelectorName:  persona.SelectorName,
			DisplayName:   persona.DisplayName,
			Model:         persona.Model,
			ReasoningMode: persona.ReasoningMode,
			Source:        persona.Source,
		})
	}
	return views
}

func sessionViews(threads []apiclient.Thread) []formatter.SessionView {
	views := make([]formatter.SessionView, 0, len(threads))
	for _, thread := range threads {
		title := ""
		if thread.Title != nil {
			title = *thread.Title
		}
		activeRunID := ""
		if thread.ActiveRunID != nil {
			activeRunID = *thread.ActiveRunID
		}
		views = append(views, formatter.SessionView{
			ID:          thread.ID,
			Title:       title,
			Mode:        thread.Mode,
			CreatedAt:   thread.CreatedAt,
			UpdatedAt:   thread.UpdatedAt,
			ActiveRunID: activeRunID,
			IsPrivate:   thread.IsPrivate,
		})
	}
	return views
}

func withOptionalTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}
