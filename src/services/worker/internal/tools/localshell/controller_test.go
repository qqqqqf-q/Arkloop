//go:build desktop

package localshell

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProcessControllerBufferedCommandCompletes(t *testing.T) {
	controller := NewProcessController()

	resp, err := controller.ExecCommand(ExecCommandRequest{
		Command:   "printf 'hello\\n'",
		Mode:      ModeBuffered,
		TimeoutMs: 5000,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if resp.Status != StatusExited {
		t.Fatalf("expected exited status, got %#v", resp)
	}
	if strings.TrimSpace(resp.Stdout) != "hello" {
		t.Fatalf("expected stdout hello, got %#v", resp.Stdout)
	}
	if resp.ProcessRef != "" {
		t.Fatalf("buffered mode should not expose process_ref, got %q", resp.ProcessRef)
	}
}

func TestProcessControllerBufferedExitsWhenBackgroundChildKeepsStdoutOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell background process semantics")
	}
	controller := NewProcessController()

	started := time.Now()
	resp, err := controller.ExecCommand(ExecCommandRequest{
		Command:   "printf 'done\\n'; (sleep 3) &",
		Mode:      ModeBuffered,
		TimeoutMs: 5000,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if resp.Status != StatusExited {
		t.Fatalf("expected exited status, got %#v", resp)
	}
	if !strings.Contains(resp.Stdout, "done") {
		t.Fatalf("expected stdout to contain done, got %#v", resp.Stdout)
	}
	if elapsed := time.Since(started); elapsed > 1500*time.Millisecond {
		t.Fatalf("process waited for inherited stdout pipe: %s", elapsed)
	}
}

func TestProcessControllerFollowExitsWhenBackgroundChildKeepsStdoutOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell background process semantics")
	}
	controller := NewProcessController()

	started := time.Now()
	resp, err := controller.ExecCommand(ExecCommandRequest{
		Command:   "printf 'done\\n'; (sleep 3) &",
		Mode:      ModeFollow,
		TimeoutMs: 5000,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	stdout := resp.Stdout
	if resp.Status == StatusRunning {
		resp, err = controller.ContinueProcess(ContinueProcessRequest{
			ProcessRef: resp.ProcessRef,
			Cursor:     resp.NextCursor,
			WaitMs:     1000,
		})
		if err != nil {
			t.Fatalf("continue failed: %v", err)
		}
		stdout += resp.Stdout
	}
	if resp.Status != StatusExited {
		t.Fatalf("expected exited status, got %#v", resp)
	}
	if !strings.Contains(stdout, "done") {
		t.Fatalf("expected stdout to contain done, got %#v", stdout)
	}
	if elapsed := time.Since(started); elapsed > 1500*time.Millisecond {
		t.Fatalf("process waited for inherited stdout pipe: %s", elapsed)
	}
}

func TestProcessControllerRejectsInvalidMode(t *testing.T) {
	controller := NewProcessController()

	_, err := controller.ExecCommand(ExecCommandRequest{
		Command:   "printf 'hello\\n'",
		Mode:      "bogus",
		TimeoutMs: 5000,
		Cwd:       t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected invalid mode error")
	}
	if got := err.Error(); got != "unsupported mode: bogus" {
		t.Fatalf("expected unsupported mode error, got %q", got)
	}
}

func TestProcessControllerStdinModeAcceptsInputAndEOF(t *testing.T) {
	controller := NewProcessController()

	start, err := controller.ExecCommand(ExecCommandRequest{
		Command:   "cat",
		Mode:      ModeStdin,
		TimeoutMs: 5000,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("stdin exec failed: %v", err)
	}
	if start.ProcessRef == "" || start.Status != StatusRunning {
		t.Fatalf("expected running process with ref, got %#v", start)
	}

	text := "hello from stdin\n"
	resp, err := controller.ContinueProcess(ContinueProcessRequest{
		ProcessRef: start.ProcessRef,
		Cursor:     start.NextCursor,
		WaitMs:     1000,
		StdinText:  &text,
		InputSeq:   int64Ptr(1),
		CloseStdin: true,
	})
	if err != nil {
		t.Fatalf("continue failed: %v", err)
	}
	if strings.TrimSpace(resp.Stdout) != "hello from stdin" {
		t.Fatalf("expected echoed stdin, got %#v", resp.Stdout)
	}
	if resp.Status != StatusRunning && resp.Status != StatusExited {
		t.Fatalf("expected running or exited status after close_stdin, got %#v", resp)
	}
	if resp.AcceptedInputSeq == nil || *resp.AcceptedInputSeq != 1 {
		t.Fatalf("expected accepted_input_seq=1, got %#v", resp.AcceptedInputSeq)
	}
	if resp.Status == StatusRunning {
		final, err := controller.ContinueProcess(ContinueProcessRequest{
			ProcessRef: start.ProcessRef,
			Cursor:     resp.NextCursor,
			WaitMs:     1000,
		})
		if err != nil {
			t.Fatalf("final continue failed: %v", err)
		}
		if final.Status != StatusExited {
			t.Fatalf("expected exited status after draining closed stdin, got %#v", final)
		}
	}
}

func TestProcessControllerTerminateMarksTerminated(t *testing.T) {
	controller := NewProcessController()

	start, err := controller.ExecCommand(ExecCommandRequest{
		Command:   "sleep 30",
		Mode:      ModeFollow,
		TimeoutMs: 5000,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("follow exec failed: %v", err)
	}
	if start.ProcessRef == "" {
		t.Fatalf("expected process_ref, got %#v", start)
	}

	resp, err := controller.TerminateProcess(TerminateProcessRequest{ProcessRef: start.ProcessRef})
	if err != nil {
		t.Fatalf("terminate failed: %v", err)
	}
	if resp.Status != StatusTerminated {
		t.Fatalf("expected terminated status, got %#v", resp)
	}
}

func TestProcessControllerPTYRespectsResize(t *testing.T) {
	controller := NewProcessController()

	start, err := controller.ExecCommand(ExecCommandRequest{
		Command:   "stty size; sleep 0.2",
		Mode:      ModePTY,
		TimeoutMs: 5000,
		Size:      &Size{Rows: 40, Cols: 100},
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("pty exec failed: %v", err)
	}
	if start.ProcessRef == "" {
		t.Fatalf("expected process_ref, got %#v", start)
	}

	if _, err = controller.ResizeProcess(ResizeProcessRequest{
		ProcessRef: start.ProcessRef,
		Rows:       50,
		Cols:       120,
	}); err != nil {
		t.Fatalf("resize failed: %v", err)
	}

	time.Sleep(250 * time.Millisecond)
	resp, err := controller.ContinueProcess(ContinueProcessRequest{
		ProcessRef: start.ProcessRef,
		Cursor:     start.NextCursor,
		WaitMs:     1000,
	})
	if err != nil {
		t.Fatalf("continue failed: %v", err)
	}
	if !strings.Contains(start.Stdout+resp.Stdout, "40 100") {
		t.Fatalf("expected initial pty size in output, got start=%#v resp=%#v", start.Stdout, resp.Stdout)
	}
}

func TestProcessControllerContinueRequiresInputSeqForEmptyString(t *testing.T) {
	controller := NewProcessController()

	start, err := controller.ExecCommand(ExecCommandRequest{
		Command:   "cat",
		Mode:      ModeStdin,
		TimeoutMs: 5000,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("stdin exec failed: %v", err)
	}

	empty := ""
	_, err = controller.ContinueProcess(ContinueProcessRequest{
		ProcessRef: start.ProcessRef,
		Cursor:     start.NextCursor,
		WaitMs:     100,
		StdinText:  &empty,
	})
	if err == nil {
		t.Fatal("expected input_seq validation error")
	}
	if got := err.Error(); got != "input_seq is required when stdin_text is provided" {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestProcessControllerDuplicateInputSeqAfterCloseStdinIsIdempotent(t *testing.T) {
	controller := NewProcessController()

	start, err := controller.ExecCommand(ExecCommandRequest{
		Command:   "cat",
		Mode:      ModeStdin,
		TimeoutMs: 5000,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("stdin exec failed: %v", err)
	}

	text := "repeat\n"
	first, err := controller.ContinueProcess(ContinueProcessRequest{
		ProcessRef: start.ProcessRef,
		Cursor:     start.NextCursor,
		WaitMs:     1000,
		StdinText:  &text,
		InputSeq:   int64Ptr(1),
		CloseStdin: true,
	})
	if err != nil {
		t.Fatalf("first continue failed: %v", err)
	}
	if first.AcceptedInputSeq == nil || *first.AcceptedInputSeq != 1 {
		t.Fatalf("expected accepted_input_seq=1, got %#v", first.AcceptedInputSeq)
	}

	second, err := controller.ContinueProcess(ContinueProcessRequest{
		ProcessRef: start.ProcessRef,
		Cursor:     first.NextCursor,
		WaitMs:     100,
		StdinText:  &text,
		InputSeq:   int64Ptr(1),
	})
	if err != nil {
		t.Fatalf("duplicate continue should be idempotent, got error: %v", err)
	}
	if second.AcceptedInputSeq == nil || *second.AcceptedInputSeq != 1 {
		t.Fatalf("expected duplicate accepted_input_seq=1, got %#v", second.AcceptedInputSeq)
	}
	if second.Stdout != "" {
		t.Fatalf("duplicate continue should not append output, got %#v", second.Stdout)
	}
}

func TestProcessControllerCloseStdinDoesNotRequireImmediateExit(t *testing.T) {
	controller := NewProcessController()

	start, err := controller.ExecCommand(ExecCommandRequest{
		Command:   "python3 -c 'import sys,time; sys.stdin.read(); time.sleep(3); print(\"done\")'",
		Mode:      ModeStdin,
		TimeoutMs: 6000,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("stdin exec failed: %v", err)
	}

	payload := "hello\n"
	resp, err := controller.ContinueProcess(ContinueProcessRequest{
		ProcessRef: start.ProcessRef,
		Cursor:     start.NextCursor,
		WaitMs:     100,
		StdinText:  &payload,
		InputSeq:   int64Ptr(1),
		CloseStdin: true,
	})
	if err != nil {
		t.Fatalf("continue failed: %v", err)
	}
	if resp.Status != StatusRunning {
		t.Fatalf("close_stdin should not force immediate terminal state, got %#v", resp)
	}
	if resp.AcceptedInputSeq == nil || *resp.AcceptedInputSeq != 1 {
		t.Fatalf("expected accepted_input_seq=1, got %#v", resp.AcceptedInputSeq)
	}
}

func TestProcessControllerCancelMarksCancelled(t *testing.T) {
	controller := NewProcessController()

	start, err := controller.ExecCommand(ExecCommandRequest{
		Command:   "sleep 30",
		Mode:      ModeFollow,
		TimeoutMs: 5000,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("follow exec failed: %v", err)
	}

	resp, err := controller.CancelProcess(TerminateProcessRequest{ProcessRef: start.ProcessRef})
	if err != nil {
		t.Fatalf("cancel failed: %v", err)
	}
	if resp.Status != StatusCancelled {
		t.Fatalf("expected cancelled status, got %#v", resp)
	}
}

func TestProcessControllerCleanupRunMarksCancelled(t *testing.T) {
	controller := NewProcessController()

	start, err := controller.ExecCommand(ExecCommandRequest{
		RunID:     "run-cancel",
		Command:   "sleep 30",
		Mode:      ModeFollow,
		TimeoutMs: 5000,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("follow exec failed: %v", err)
	}

	controller.CleanupRun("run-cancel")

	resp, err := controller.ContinueProcess(ContinueProcessRequest{
		ProcessRef: start.ProcessRef,
		Cursor:     start.NextCursor,
		WaitMs:     1000,
	})
	if err != nil {
		t.Fatalf("continue after cleanup failed: %v", err)
	}
	if resp.Status != StatusCancelled {
		t.Fatalf("expected cancelled status after cleanup, got %#v", resp)
	}
}

func TestProcessControllerReleasesDrainedProcess(t *testing.T) {
	controller := NewProcessController()

	start, err := controller.ExecCommand(ExecCommandRequest{
		Command:   "printf 'done\\n'",
		Mode:      ModeFollow,
		TimeoutMs: 5000,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("follow exec failed: %v", err)
	}
	if start.ProcessRef == "" {
		t.Fatalf("expected process_ref, got %#v", start)
	}
	if start.Status == StatusRunning {
		final, err := controller.ContinueProcess(ContinueProcessRequest{
			ProcessRef: start.ProcessRef,
			Cursor:     start.NextCursor,
			WaitMs:     1000,
		})
		if err != nil {
			t.Fatalf("final continue failed: %v", err)
		}
		if final.Status != StatusExited {
			t.Fatalf("expected exited status, got %#v", final)
		}
	}
	if _, err := controller.getProcess(start.ProcessRef); err == nil {
		t.Fatalf("expected drained process %s to be released", start.ProcessRef)
	}
}

func TestProcessControllerReleasesTerminalProcessAfterRetention(t *testing.T) {
	controller := NewProcessController()
	originalRetention := processTerminalRetention
	processTerminalRetention = 20 * time.Millisecond
	defer func() {
		processTerminalRetention = originalRetention
	}()

	start, err := controller.ExecCommand(ExecCommandRequest{
		Command:   "sleep 0.05",
		Mode:      ModeFollow,
		TimeoutMs: 5000,
		Cwd:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("follow exec failed: %v", err)
	}
	if start.ProcessRef == "" {
		t.Fatalf("expected process_ref, got %#v", start)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := controller.getProcess(start.ProcessRef); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected terminal process %s to be released after retention", start.ProcessRef)
}

func TestBuildProcessEnvDoesNotInheritHostEnvironment(t *testing.T) {
	t.Setenv("ARKLOOP_HOST_SECRET", "host-only")
	home := t.TempDir()
	t.Setenv("HOME", home)

	env := buildProcessEnv(nil, false)
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "ARKLOOP_HOST_SECRET=host-only") {
		t.Fatalf("expected host environment to be isolated, got %q", joined)
	}
	if !strings.Contains(joined, "PATH=") {
		t.Fatalf("expected sanitized PATH in env, got %q", joined)
	}
	expectedBin := filepath.Join(home, ".arkloop", "bin")
	if !strings.Contains(joined, "PATH="+expectedBin+string(filepath.ListSeparator)) {
		t.Fatalf("expected desktop bin in PATH, got %q", joined)
	}
	if runtime.GOOS == "windows" {
		if !strings.Contains(joined, "SystemRoot=") {
			t.Fatalf("expected SystemRoot in env, got %q", joined)
		}
		if !strings.Contains(joined, "ComSpec=") {
			t.Fatalf("expected ComSpec in env, got %q", joined)
		}
		if !strings.Contains(joined, "TEMP=") || !strings.Contains(joined, "TMP=") {
			t.Fatalf("expected Windows temp vars in env, got %q", joined)
		}
	} else if !strings.Contains(joined, "LANG="+defaultProcessLang) {
		t.Fatalf("expected sanitized LANG in env, got %q", joined)
	}
	if !strings.Contains(joined, "PYTHONDONTWRITEBYTECODE=1") {
		t.Fatalf("expected Python bytecode writes disabled, got %q", joined)
	}
}

func TestRTKRewriteTimesOutAndFallsBack(t *testing.T) {
	originalBin := rtkBinCache
	originalRunner := rtkRewriteRunner
	rtkBinCache = "/tmp/fake-rtk"
	rtkBinOnce = sync.Once{}
	rtkBinOnce.Do(func() {})
	defer func() {
		rtkBinCache = originalBin
		rtkBinOnce = sync.Once{}
		rtkRewriteRunner = originalRunner
	}()

	rtkRewriteRunner = func(ctx context.Context, bin string, command string) (string, error) {
		_ = bin
		_ = command
		<-ctx.Done()
		return "", ctx.Err()
	}

	started := time.Now()
	if got := rtkRewrite(context.Background(), "git status"); got != "" {
		t.Fatalf("expected empty rewrite on timeout, got %q", got)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("rewrite timeout took too long: %s", elapsed)
	}
}

func TestRTKRewriteSkipsUnsafeCommandWithoutRunner(t *testing.T) {
	originalBin := rtkBinCache
	originalRunner := rtkRewriteRunner
	rtkBinCache = "/tmp/fake-rtk"
	rtkBinOnce = sync.Once{}
	rtkBinOnce.Do(func() {})
	defer func() {
		rtkBinCache = originalBin
		rtkBinOnce = sync.Once{}
		rtkRewriteRunner = originalRunner
	}()

	called := false
	rtkRewriteRunner = func(ctx context.Context, bin string, command string) (string, error) {
		called = true
		return "rewritten", nil
	}

	if got := rtkRewrite(context.Background(), "echo 'quoted'"); got != "" {
		t.Fatalf("expected empty rewrite for unsafe command, got %q", got)
	}
	if called {
		t.Fatal("rewrite runner should not be called for unsafe commands")
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}
