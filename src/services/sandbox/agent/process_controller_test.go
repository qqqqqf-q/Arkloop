package main

import (
	"strings"
	"testing"
	"time"

	processapi "arkloop/services/sandbox/internal/process"
)

func TestProcessControllerBufferedCommandCompletes(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewProcessController()

	resp, code, msg := controller.Exec(processapi.AgentExecRequest{
		Command:   "printf 'hello\\n'",
		Mode:      processapi.ModeBuffered,
		TimeoutMs: 5000,
	})
	if code != "" {
		t.Fatalf("exec failed: %s %s", code, msg)
	}
	if resp.Status != processapi.StatusExited {
		t.Fatalf("expected exited status, got %#v", resp)
	}
	if strings.TrimSpace(resp.Stdout) != "hello" {
		t.Fatalf("expected stdout hello, got %#v", resp.Stdout)
	}
	if resp.ProcessRef != "" {
		t.Fatalf("buffered mode should not expose process_ref, got %q", resp.ProcessRef)
	}
}

func TestProcessControllerRejectsInvalidMode(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewProcessController()

	resp, code, msg := controller.Exec(processapi.AgentExecRequest{
		Command:   "printf 'hello\\n'",
		Mode:      "bogus",
		TimeoutMs: 5000,
	})
	if resp != nil {
		t.Fatalf("expected nil response, got %#v", resp)
	}
	if code != processapi.CodeInvalidMode {
		t.Fatalf("expected invalid mode code, got %s %s", code, msg)
	}
	if !strings.Contains(msg, "unsupported mode: bogus") {
		t.Fatalf("unexpected message: %s", msg)
	}
}

func TestProcessControllerStdinModeAcceptsInputAndEOF(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewProcessController()

	start, code, msg := controller.Exec(processapi.AgentExecRequest{
		Command:   "cat",
		Mode:      processapi.ModeStdin,
		TimeoutMs: 5000,
	})
	if code != "" {
		t.Fatalf("stdin exec failed: %s %s", code, msg)
	}
	if start.ProcessRef == "" || start.Status != processapi.StatusRunning {
		t.Fatalf("expected running process with ref, got %#v", start)
	}

	text := "hello from stdin\n"
	resp, code, msg := controller.Continue(processapi.ContinueProcessRequest{
		ProcessRef: start.ProcessRef,
		Cursor:     start.NextCursor,
		WaitMs:     1000,
		StdinText:  &text,
		InputSeq:   int64Ptr(1),
		CloseStdin: true,
	})
	if code != "" {
		t.Fatalf("continue failed: %s %s", code, msg)
	}
	if strings.TrimSpace(resp.Stdout) != "hello from stdin" {
		t.Fatalf("expected echoed stdin, got %#v", resp.Stdout)
	}
	if resp.Status != processapi.StatusRunning && resp.Status != processapi.StatusExited {
		t.Fatalf("expected running or exited status after close_stdin, got %#v", resp)
	}
	if resp.AcceptedInputSeq == nil || *resp.AcceptedInputSeq != 1 {
		t.Fatalf("expected accepted_input_seq=1, got %#v", resp.AcceptedInputSeq)
	}
	if resp.Status == processapi.StatusRunning {
		final, code, msg := controller.Continue(processapi.ContinueProcessRequest{
			ProcessRef: start.ProcessRef,
			Cursor:     resp.NextCursor,
			WaitMs:     1000,
		})
		if code != "" {
			t.Fatalf("final continue failed: %s %s", code, msg)
		}
		if final.Status != processapi.StatusExited {
			t.Fatalf("expected exited status after draining closed stdin, got %#v", final)
		}
	}
}

func TestProcessControllerPTYCloseStdinRejected(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewProcessController()

	start, code, msg := controller.Exec(processapi.AgentExecRequest{
		Command:   "cat",
		Mode:      processapi.ModePTY,
		TimeoutMs: 5000,
	})
	if code != "" {
		t.Fatalf("pty exec failed: %s %s", code, msg)
	}
	if start.ProcessRef == "" || start.Status != processapi.StatusRunning {
		t.Fatalf("expected running process with ref, got %#v", start)
	}

	resp, code, msg := controller.Continue(processapi.ContinueProcessRequest{
		ProcessRef: start.ProcessRef,
		Cursor:     start.NextCursor,
		WaitMs:     1000,
		CloseStdin: true,
	})
	if resp != nil {
		t.Fatalf("expected nil response, got %#v", resp)
	}
	if code != processapi.CodeCloseStdinUnsupported {
		t.Fatalf("expected close_stdin unsupported, got %s %s", code, msg)
	}
	if !strings.Contains(msg, "close_stdin is not supported for mode: pty") {
		t.Fatalf("unexpected message: %s", msg)
	}

	if _, code, msg = controller.Terminate(processapi.AgentRefRequest{ProcessRef: start.ProcessRef}); code != "" {
		t.Fatalf("terminate failed: %s %s", code, msg)
	}
}

func TestProcessControllerTerminateMarksTerminated(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewProcessController()

	start, code, msg := controller.Exec(processapi.AgentExecRequest{
		Command:   "sleep 30",
		Mode:      processapi.ModeFollow,
		TimeoutMs: 5000,
	})
	if code != "" {
		t.Fatalf("follow exec failed: %s %s", code, msg)
	}
	if start.ProcessRef == "" {
		t.Fatalf("expected process_ref, got %#v", start)
	}

	resp, code, msg := controller.Terminate(processapi.AgentRefRequest{ProcessRef: start.ProcessRef})
	if code != "" {
		t.Fatalf("terminate failed: %s %s", code, msg)
	}
	if resp.Status != processapi.StatusTerminated {
		t.Fatalf("expected terminated status, got %#v", resp)
	}
}

func TestProcessControllerPTYRespectsResize(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewProcessController()

	start, code, msg := controller.Exec(processapi.AgentExecRequest{
		Command:   "stty size; sleep 0.2",
		Mode:      processapi.ModePTY,
		TimeoutMs: 5000,
		Size:      &processapi.Size{Rows: 40, Cols: 100},
	})
	if code != "" {
		t.Fatalf("pty exec failed: %s %s", code, msg)
	}
	if start.ProcessRef == "" {
		t.Fatalf("expected process_ref, got %#v", start)
	}
	if _, code, msg = controller.Resize(processapi.AgentResizeRequest{
		ProcessRef: start.ProcessRef,
		Rows:       50,
		Cols:       120,
	}); code != "" {
		t.Fatalf("resize failed: %s %s", code, msg)
	}

	time.Sleep(250 * time.Millisecond)
	resp, code, msg := controller.Continue(processapi.ContinueProcessRequest{
		ProcessRef: start.ProcessRef,
		Cursor:     start.NextCursor,
		WaitMs:     1000,
	})
	if code != "" {
		t.Fatalf("continue failed: %s %s", code, msg)
	}
	if !strings.Contains(start.Stdout+resp.Stdout, "40 100") {
		t.Fatalf("expected initial pty size in output, got start=%#v resp=%#v", start.Stdout, resp.Stdout)
	}
}

func TestProcessControllerPTYCloseStdinEndsInteractiveRead(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewProcessController()

	start, code, msg := controller.Exec(processapi.AgentExecRequest{
		Command:   "python3 -c 'import sys; data=sys.stdin.read(); print(data.upper())'",
		Mode:      processapi.ModePTY,
		TimeoutMs: 5000,
	})
	if code != "" {
		t.Fatalf("pty exec failed: %s %s", code, msg)
	}
	text := "hello pty\n"
	resp, code, msg := controller.Continue(processapi.ContinueProcessRequest{
		ProcessRef: start.ProcessRef,
		Cursor:     start.NextCursor,
		WaitMs:     1000,
		StdinText:  &text,
		InputSeq:   int64Ptr(2),
		CloseStdin: true,
	})
	if code != processapi.CodeCloseStdinUnsupported {
		t.Fatalf("expected close_stdin unsupported, got code=%s msg=%s resp=%#v", code, msg, resp)
	}
	if !strings.Contains(msg, "close_stdin is not supported for mode: pty") {
		t.Fatalf("unexpected close_stdin message: %s", msg)
	}
	if _, code, msg = controller.Terminate(processapi.AgentRefRequest{ProcessRef: start.ProcessRef}); code != "" {
		t.Fatalf("terminate failed: %s %s", code, msg)
	}
}

func TestProcessControllerContinueRequiresInputSeqForEmptyString(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewProcessController()

	start, code, msg := controller.Exec(processapi.AgentExecRequest{
		Command:   "cat",
		Mode:      processapi.ModeStdin,
		TimeoutMs: 5000,
	})
	if code != "" {
		t.Fatalf("stdin exec failed: %s %s", code, msg)
	}

	empty := ""
	resp, code, msg := controller.Continue(processapi.ContinueProcessRequest{
		ProcessRef: start.ProcessRef,
		Cursor:     start.NextCursor,
		WaitMs:     100,
		StdinText:  &empty,
	})
	if resp != nil {
		t.Fatalf("expected nil response, got %#v", resp)
	}
	if code != processapi.CodeInputSeqRequired {
		t.Fatalf("expected input_seq_required, got %s %s", code, msg)
	}
}

func TestProcessControllerDuplicateInputSeqAfterCloseStdinIsIdempotent(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewProcessController()

	start, code, msg := controller.Exec(processapi.AgentExecRequest{
		Command:   "cat",
		Mode:      processapi.ModeStdin,
		TimeoutMs: 5000,
	})
	if code != "" {
		t.Fatalf("stdin exec failed: %s %s", code, msg)
	}

	text := "repeat\n"
	first, code, msg := controller.Continue(processapi.ContinueProcessRequest{
		ProcessRef: start.ProcessRef,
		Cursor:     start.NextCursor,
		WaitMs:     1000,
		StdinText:  &text,
		InputSeq:   int64Ptr(1),
		CloseStdin: true,
	})
	if code != "" {
		t.Fatalf("first continue failed: %s %s", code, msg)
	}
	if first.AcceptedInputSeq == nil || *first.AcceptedInputSeq != 1 {
		t.Fatalf("expected accepted_input_seq=1, got %#v", first.AcceptedInputSeq)
	}

	second, code, msg := controller.Continue(processapi.ContinueProcessRequest{
		ProcessRef: start.ProcessRef,
		Cursor:     first.NextCursor,
		WaitMs:     100,
		StdinText:  &text,
		InputSeq:   int64Ptr(1),
	})
	if code != "" {
		t.Fatalf("duplicate continue should be idempotent, got %s %s", code, msg)
	}
	if second.AcceptedInputSeq == nil || *second.AcceptedInputSeq != 1 {
		t.Fatalf("expected duplicate accepted_input_seq=1, got %#v", second.AcceptedInputSeq)
	}
	if second.Stdout != "" {
		t.Fatalf("duplicate continue should not append output, got %#v", second.Stdout)
	}
}

func TestProcessControllerCloseStdinDoesNotRequireImmediateExit(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewProcessController()

	start, code, msg := controller.Exec(processapi.AgentExecRequest{
		Command:   "python3 -c 'import sys,time; sys.stdin.read(); time.sleep(3); print(\"done\")'",
		Mode:      processapi.ModeStdin,
		TimeoutMs: 6000,
	})
	if code != "" {
		t.Fatalf("stdin exec failed: %s %s", code, msg)
	}

	payload := "hello\n"
	resp, code, msg := controller.Continue(processapi.ContinueProcessRequest{
		ProcessRef: start.ProcessRef,
		Cursor:     start.NextCursor,
		WaitMs:     100,
		StdinText:  &payload,
		InputSeq:   int64Ptr(1),
		CloseStdin: true,
	})
	if code != "" {
		t.Fatalf("continue failed: %s %s", code, msg)
	}
	if resp.Status != processapi.StatusRunning {
		t.Fatalf("close_stdin should not force immediate terminal state, got %#v", resp)
	}
	if resp.AcceptedInputSeq == nil || *resp.AcceptedInputSeq != 1 {
		t.Fatalf("expected accepted_input_seq=1, got %#v", resp.AcceptedInputSeq)
	}
}

func TestProcessControllerCancelMarksCancelled(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewProcessController()

	start, code, msg := controller.Exec(processapi.AgentExecRequest{
		Command:   "sleep 30",
		Mode:      processapi.ModeFollow,
		TimeoutMs: 5000,
	})
	if code != "" {
		t.Fatalf("follow exec failed: %s %s", code, msg)
	}

	resp, code, msg := controller.Cancel(processapi.AgentRefRequest{ProcessRef: start.ProcessRef})
	if code != "" {
		t.Fatalf("cancel failed: %s %s", code, msg)
	}
	if resp.Status != processapi.StatusCancelled {
		t.Fatalf("expected cancelled status, got %#v", resp)
	}
}

func TestProcessControllerReleasesDrainedProcess(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewProcessController()

	start, code, msg := controller.Exec(processapi.AgentExecRequest{
		Command:   "printf 'done\\n'",
		Mode:      processapi.ModeFollow,
		TimeoutMs: 5000,
	})
	if code != "" {
		t.Fatalf("follow exec failed: %s %s", code, msg)
	}
	if start.ProcessRef == "" {
		t.Fatalf("expected process_ref, got %#v", start)
	}
	if start.Status == processapi.StatusRunning {
		final, code, msg := controller.Continue(processapi.ContinueProcessRequest{
			ProcessRef: start.ProcessRef,
			Cursor:     start.NextCursor,
			WaitMs:     1000,
		})
		if code != "" {
			t.Fatalf("final continue failed: %s %s", code, msg)
		}
		if final.Status != processapi.StatusExited {
			t.Fatalf("expected exited status, got %#v", final)
		}
	}
	if _, err := controller.getProcess(start.ProcessRef); err == nil {
		t.Fatalf("expected drained process %s to be released", start.ProcessRef)
	}
}

func TestProcessControllerReturnsCursorExpiredAfterBufferOverflow(t *testing.T) {
	workspace := t.TempDir()
	bindShellDirs(t, workspace)
	controller := NewProcessController()

	start, code, msg := controller.Exec(processapi.AgentExecRequest{
		Command:   "python3 -c 'import sys; sys.stdout.write(\"x\" * 1300000)'",
		Mode:      processapi.ModeFollow,
		TimeoutMs: 5000,
	})
	if code != "" {
		t.Fatalf("follow exec failed: %s %s", code, msg)
	}
	if start.ProcessRef == "" {
		t.Fatalf("expected process_ref, got %#v", start)
	}
	defer func() {
		if _, termCode, termMsg := controller.Terminate(processapi.AgentRefRequest{ProcessRef: start.ProcessRef}); termCode != "" && termCode != processapi.CodeProcessNotFound {
			t.Fatalf("terminate failed: %s %s", termCode, termMsg)
		}
	}()
	proc, err := controller.getProcess(start.ProcessRef)
	if err != nil {
		t.Fatalf("get process: %v", err)
	}
	proc.mu.Lock()
	for proc.output.HeadSeq() == 0 {
		proc.output.Append(processapi.StreamStdout, strings.Repeat("x", processRingBytes+4096))
	}
	proc.mu.Unlock()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, code, _ = controller.Continue(processapi.ContinueProcessRequest{
			ProcessRef: start.ProcessRef,
			Cursor:     "0",
			WaitMs:     100,
		})
		if code == processapi.CodeCursorExpired {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("expected cursor_expired, got %s", code)
}

func int64Ptr(value int64) *int64 {
	return &value
}
