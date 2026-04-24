package repl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"arkloop/services/cli/internal/apiclient"
	"arkloop/services/cli/internal/formatter"
	"arkloop/services/cli/internal/renderer"
	"arkloop/services/cli/internal/runner"
)

type REPL struct {
	client   *apiclient.Client
	params   apiclient.RunParams
	renderer *renderer.Renderer
	threadID string
	timeout  time.Duration
	stdout   io.Writer
	stderr   io.Writer
}

func NewREPL(client *apiclient.Client, params apiclient.RunParams, threadID string, timeout time.Duration) *REPL {
	return newREPLWithWriters(client, params, threadID, timeout, os.Stdout, os.Stderr)
}

func newREPLWithWriters(
	client *apiclient.Client,
	params apiclient.RunParams,
	threadID string,
	timeout time.Duration,
	stdout io.Writer,
	stderr io.Writer,
) *REPL {
	return &REPL{
		client:   client,
		params:   params,
		renderer: renderer.NewRenderer(stdout),
		threadID: threadID,
		timeout:  timeout,
		stdout:   stdout,
		stderr:   stderr,
	}
}

func (r *REPL) Run(ctx context.Context) error {
	reader := bufio.NewReader(os.Stdin)
	if err := r.printStatus(); err != nil {
		return err
	}

	for {
		_, _ = fmt.Fprint(r.stderr, "> ")

		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				_, _ = fmt.Fprintln(r.stderr)
				r.printResumeHint()
				return nil
			}
			return fmt.Errorf("read input: %w", err)
		}

		input := strings.TrimSpace(line)

		switch {
		case input == "":
			continue
		case strings.HasPrefix(input, "/"):
			handled, err := r.handleCommand(ctx, input)
			if errors.Is(err, io.EOF) {
				r.printResumeHint()
				return nil
			}
			if err != nil {
				_, _ = fmt.Fprintf(r.stderr, "error: %v\n", err)
			}
			if handled {
				continue
			}
		}

		if r.threadID == "" {
			tid, err := r.client.CreateThread(ctx, "")
			if err != nil {
				_, _ = fmt.Fprintf(r.stderr, "error: %v\n", err)
				continue
			}
			r.threadID = tid
		}

		runCtx, cancel := withOptionalTimeout(ctx, r.timeout)
		_, execErr := runner.Execute(runCtx, r.client, r.threadID, input, r.params, r.renderer.OnEvent)
		cancel()

		r.renderer.Flush()

		if execErr != nil && !errors.Is(execErr, context.Canceled) && !errors.Is(execErr, context.DeadlineExceeded) {
			_, _ = fmt.Fprintf(r.stderr, "error: %v\n", execErr)
		}

		if ctx.Err() != nil {
			r.printResumeHint()
			return nil
		}
	}
}

func withOptionalTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func (r *REPL) handleCommand(ctx context.Context, input string) (bool, error) {
	switch {
	case input == "/quit" || input == "/exit":
		return true, io.EOF
	case input == "/help":
		_, err := fmt.Fprintln(r.stderr, "/help\n/status\n/model <name>\n/persona <key>\n/thread\n/new\n/quit\n/exit")
		return true, err
	case input == "/status":
		return true, r.printStatus()
	case input == "/thread":
		_, err := fmt.Fprintf(r.stderr, "session id: %s\n", sessionIDValue(r.threadID))
		return true, err
	case input == "/new":
		r.threadID = ""
		_, err := fmt.Fprintln(r.stderr, "new session")
		return true, err
	case strings.HasPrefix(input, "/model "):
		model := strings.TrimSpace(strings.TrimPrefix(input, "/model "))
		if model == "" {
			return true, fmt.Errorf("usage: /model <name>")
		}
		if err := r.setModel(ctx, model); err != nil {
			return true, err
		}
		_, err := fmt.Fprintf(r.stderr, "model: %s\n", model)
		return true, err
	case strings.HasPrefix(input, "/persona "):
		persona := strings.TrimSpace(strings.TrimPrefix(input, "/persona "))
		if persona == "" {
			return true, fmt.Errorf("usage: /persona <key>")
		}
		if err := r.setPersona(ctx, persona); err != nil {
			return true, err
		}
		_, err := fmt.Fprintf(r.stderr, "persona: %s\n", persona)
		return true, err
	case strings.HasPrefix(input, "/"):
		return true, fmt.Errorf("unknown command: %s", input)
	default:
		return false, nil
	}
}

func (r *REPL) printStatus() error {
	return formatter.PrintChatStatus(r.stderr, formatter.ChatStatusView{
		Host:      r.client.BaseURL(),
		SessionID: sessionIDValue(r.threadID),
		Model:     r.params.Model,
		Persona:   r.params.PersonaID,
		WorkDir:   r.params.WorkDir,
		Timeout:   timeoutDisplay(r.timeout),
	})
}

func (r *REPL) setModel(ctx context.Context, model string) error {
	providers, err := r.client.ListLlmProviders(ctx)
	if err != nil {
		return err
	}
	for _, provider := range providers {
		for _, item := range provider.Models {
			if item.Model == model {
				r.params.Model = model
				return nil
			}
		}
	}
	return fmt.Errorf("model not found: %s", model)
}

func (r *REPL) setPersona(ctx context.Context, personaKey string) error {
	personas, err := r.client.ListSelectablePersonas(ctx)
	if err != nil {
		return err
	}
	for _, persona := range personas {
		if persona.PersonaKey == personaKey {
			r.params.PersonaID = personaKey
			return nil
		}
	}
	return fmt.Errorf("persona not found: %s", personaKey)
}

func sessionIDValue(threadID string) string {
	if strings.TrimSpace(threadID) == "" {
		return "new"
	}
	return threadID
}

func timeoutDisplay(timeout time.Duration) string {
	if timeout <= 0 {
		return ""
	}
	return timeout.String()
}

func (r *REPL) printResumeHint() {
	if strings.TrimSpace(r.threadID) != "" {
		_, _ = fmt.Fprintf(r.stderr, "\nTo resume this session:\n  ark sessions resume %s\n", r.threadID)
	}
}
