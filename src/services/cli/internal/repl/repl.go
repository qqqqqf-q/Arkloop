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
	"arkloop/services/cli/internal/renderer"
	"arkloop/services/cli/internal/runner"
)

type REPL struct {
	client   *apiclient.Client
	params   apiclient.RunParams
	renderer *renderer.Renderer
	threadID string
	timeout  time.Duration
}

func NewREPL(client *apiclient.Client, params apiclient.RunParams, threadID string, timeout time.Duration) *REPL {
	return &REPL{
		client:   client,
		params:   params,
		renderer: renderer.NewRenderer(os.Stdout),
		threadID: threadID,
		timeout:  timeout,
	}
}

func (r *REPL) Run(ctx context.Context) error {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Fprint(os.Stderr, "> ")

		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(os.Stderr)
				return nil
			}
			return fmt.Errorf("read input: %w", err)
		}

		input := strings.TrimSpace(line)

		switch {
		case input == "":
			continue
		case input == "/quit" || input == "/exit":
			return nil
		case input == "/thread":
			fmt.Fprintf(os.Stderr, "thread: %s\n", r.threadID)
			continue
		case input == "/new":
			r.threadID = ""
			fmt.Fprintln(os.Stderr, "new thread")
			continue
		}

		if r.threadID == "" {
			tid, err := r.client.CreateThread(ctx, "")
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				continue
			}
			r.threadID = tid
		}

		runCtx, cancel := context.WithTimeout(ctx, r.timeout)
		_, execErr := runner.Execute(runCtx, r.client, r.threadID, input, r.params, r.renderer.OnEvent)
		cancel()

		r.renderer.Flush()

		if execErr != nil && !errors.Is(execErr, context.Canceled) && !errors.Is(execErr, context.DeadlineExceeded) {
			fmt.Fprintf(os.Stderr, "error: %v\n", execErr)
		}

		if ctx.Err() != nil {
			return nil
		}
	}
}
