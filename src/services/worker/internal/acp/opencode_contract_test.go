package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const envRunOpenCodeACPContract = "ARKLOOP_RUN_ACP_CONTRACT"

func TestOpenCodeACPContract(t *testing.T) {
	if os.Getenv(envRunOpenCodeACPContract) != "1" {
		t.Skipf("set %s=1 to run real opencode ACP contract checks", envRunOpenCodeACPContract)
	}
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skipf("opencode not found in PATH: %v", err)
	}

	helpCtx, helpCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer helpCancel()
	helpCmd := exec.CommandContext(helpCtx, "opencode", "acp", "--help")
	if output, err := helpCmd.CombinedOutput(); err != nil {
		t.Skipf("opencode acp --help unavailable: %v (%s)", err, strings.TrimSpace(string(output)))
	}

	contract := startOpenCodeACPContractProcess(t)
	defer contract.stop(t)

	t.Run("session_new_returns_session_id", func(t *testing.T) {
		msg := NewSessionNewMessage(1, SessionModeCode, contract.cwd)
		if err := contract.send(msg); err != nil {
			t.Fatalf("send session/new: %v", err)
		}

		sessionID, transcript := contract.waitForSessionNew(t, 20*time.Second)
		if strings.TrimSpace(sessionID) == "" {
			t.Fatalf("session/new returned empty session id; transcript=%s", transcript)
		}
		contract.protocolSessionID = sessionID
		t.Logf("observed protocol session id: %s", sessionID)
	})

	t.Run("session_prompt_yields_observable_contract", func(t *testing.T) {
		if strings.TrimSpace(contract.protocolSessionID) == "" {
			t.Fatal("protocol session id missing from session/new")
		}

		msg := NewSessionPromptMessage(2, contract.protocolSessionID, "Reply with the single word READY.")
		if err := contract.send(msg); err != nil {
			t.Fatalf("send session/prompt: %v", err)
		}

		updates, rawLines, sawPromptResult := contract.waitForPromptObservation(t, 2, 45*time.Second)
		if len(rawLines) == 0 {
			t.Fatal("session/prompt produced no observable stdout")
		}
		if !sawPromptResult && len(updates) == 0 {
			t.Fatalf("session/prompt produced stdout but no parseable ACP updates or prompt result; raw=%q", rawLines)
		}

		t.Logf("observed %d raw lines, %d normalized updates, prompt_result=%v", len(rawLines), len(updates), sawPromptResult)
		for _, update := range updates {
			t.Logf("update type=%s status=%s summary=%s message=%s", update.Type, update.Status, update.Summary, update.Message)
		}
	})
}

type openCodeACPContractProcess struct {
	cmd               *exec.Cmd
	stdin             io.WriteCloser
	stdout            *bufio.Reader
	stderr            strings.Builder
	cwd               string
	protocolSessionID string
}

func startOpenCodeACPContractProcess(t *testing.T) *openCodeACPContractProcess {
	t.Helper()

	cwd := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	cmd := exec.CommandContext(ctx, "opencode", "acp")
	cmd.Dir = cwd

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start opencode acp: %v", err)
	}

	process := &openCodeACPContractProcess{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		cwd:    cwd,
	}
	go func() {
		_, _ = io.Copy(&process.stderr, stderr)
	}()
	return process
}

func (p *openCodeACPContractProcess) stop(t *testing.T) {
	t.Helper()
	if p.cmd == nil || p.cmd.Process == nil {
		return
	}
	_ = p.stdin.Close()
	done := make(chan error, 1)
	go func() {
		done <- p.cmd.Wait()
	}()
	select {
	case err := <-done:
		if err != nil && !isExpectedProcessExit(err) {
			t.Logf("opencode acp exit: %v; stderr=%s", err, strings.TrimSpace(p.stderr.String()))
		}
	case <-time.After(5 * time.Second):
		_ = p.cmd.Process.Kill()
		<-done
	}
}

func (p *openCodeACPContractProcess) send(msg ACPMessage) error {
	payload, err := MarshalMessage(msg)
	if err != nil {
		return err
	}
	_, err = p.stdin.Write(payload)
	return err
}

func (p *openCodeACPContractProcess) waitForSessionNew(t *testing.T, timeout time.Duration) (string, []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var lines []string
	for {
		line, err := p.readLine(ctx)
		if err != nil {
			t.Fatalf("read session/new response: %v; stderr=%s; stdout=%q", err, strings.TrimSpace(p.stderr.String()), lines)
		}
		lines = append(lines, line)

		var msg ACPMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if msg.Result == nil {
			continue
		}
		raw, err := json.Marshal(msg.Result)
		if err != nil {
			continue
		}
		var result SessionNewResult
		if err := json.Unmarshal(raw, &result); err != nil {
			continue
		}
		if strings.TrimSpace(result.SessionID) != "" {
			return result.SessionID, lines
		}
	}
}

func (p *openCodeACPContractProcess) waitForPromptObservation(t *testing.T, promptRequestID int, timeout time.Duration) ([]SessionUpdateParams, []string, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var (
		lines           []string
		updates         []SessionUpdateParams
		sawPromptResult bool
	)

	for {
		line, err := p.readLine(ctx)
		if err != nil {
			t.Fatalf("read session/prompt observation: %v; stderr=%s; stdout=%q", err, strings.TrimSpace(p.stderr.String()), lines)
		}
		lines = append(lines, line)

		parsed, parseErr := ParseUpdates(line + "\n")
		if parseErr == nil && len(parsed) > 0 {
			updates = append(updates, parsed...)
			last := parsed[len(parsed)-1]
			if last.Type == UpdateTypeComplete || last.Type == UpdateTypeError {
				return updates, lines, sawPromptResult
			}
		}

		var msg ACPMessage
		if err := json.Unmarshal([]byte(line), &msg); err == nil {
			if msg.ID != nil && *msg.ID == promptRequestID && msg.Method == "" && msg.Result != nil {
				sawPromptResult = true
				return updates, lines, sawPromptResult
			}
		}
	}
}

func (p *openCodeACPContractProcess) readLine(ctx context.Context) (string, error) {
	type lineResult struct {
		line string
		err  error
	}
	ch := make(chan lineResult, 1)
	go func() {
		line, err := p.stdout.ReadString('\n')
		ch <- lineResult{line: strings.TrimSpace(line), err: err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-ch:
		if result.err != nil {
			if errors.Is(result.err, io.EOF) && result.line != "" {
				return result.line, nil
			}
			return "", result.err
		}
		return result.line, nil
	}
}

func isExpectedProcessExit(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}
