package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	processapi "arkloop/services/sandbox/internal/process"

	"github.com/creack/pty"
)

const (
	processStartupWait = 150 * time.Millisecond
	processDrainGrace  = 100 * time.Millisecond
	processKillGrace   = 2 * time.Second
	processPollTick    = 100 * time.Millisecond
	processRingBytes   = 1 << 20
)

type ProcessController struct {
	mu        sync.Mutex
	processes map[string]*managedProcess
}

type managedProcess struct {
	mu sync.Mutex

	ref        string
	mode       string
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	ptyFile    *os.File
	allowStdin bool

	stdinClosed bool
	status      string
	exitCode    *int

	requestedStatus string
	inFlight        bool

	acceptedInputSeqs map[int64]struct{}
	output            *processapi.ItemBuffer
	updateCh          chan struct{}
	readerWG          sync.WaitGroup
}

func NewProcessController() *ProcessController {
	return &ProcessController{
		processes: map[string]*managedProcess{},
	}
}

func (c *ProcessController) Close() {
	c.mu.Lock()
	processes := make([]*managedProcess, 0, len(c.processes))
	for _, proc := range c.processes {
		processes = append(processes, proc)
	}
	c.processes = map[string]*managedProcess{}
	c.mu.Unlock()

	for _, proc := range processes {
		proc.mu.Lock()
		if proc.status == processapi.StatusRunning {
			killProcessLocked(proc, processapi.StatusCancelled)
		}
		proc.mu.Unlock()
	}
}

func (c *ProcessController) Exec(req processapi.AgentExecRequest) (*processapi.Response, string, string) {
	command := strings.TrimSpace(req.Command)
	if command == "" {
		return nil, processapi.CodeInvalidMode, "command is required"
	}
	rawMode := strings.TrimSpace(req.Mode)
	if err := processapi.ValidateMode(rawMode); err != nil {
		return nil, err.Code, err.Message
	}
	if err := ensureWorkloadBaseDirs(); err != nil {
		return nil, processapi.CodeProcessNotFound, err.Error()
	}
	mode := processapi.NormalizeMode(rawMode)
	proc, err := c.startProcess(processapi.AgentExecRequest{
		Command:   command,
		Mode:      mode,
		Cwd:       strings.TrimSpace(req.Cwd),
		TimeoutMs: req.TimeoutMs,
		Size:      req.Size,
		Env:       req.Env,
	})
	if err != nil {
		code, message := mapProcessError(err)
		return nil, code, message
	}
	if mode == processapi.ModeBuffered {
		resp, err := c.waitUntilDone(proc, 0, req.TimeoutMs)
		if err != nil {
			code, message := mapProcessError(err)
			return nil, code, message
		}
		c.mu.Lock()
		delete(c.processes, proc.ref)
		c.mu.Unlock()
		resp.ProcessRef = ""
		return resp, "", ""
	}
	resp, err := c.waitForSnapshot(proc, 0, processStartupWait, nil)
	if err != nil {
		code, message := mapProcessError(err)
		return nil, code, message
	}
	resp.ProcessRef = proc.ref
	c.releaseProcessIfDrained(proc, resp)
	return resp, "", ""
}

func (c *ProcessController) Continue(req processapi.ContinueProcessRequest) (*processapi.Response, string, string) {
	cursor, err := processapi.ParseCursor(req.Cursor)
	if err != nil {
		return nil, processapi.CodeInvalidCursor, err.Error()
	}

	proc, err := c.getProcess(req.ProcessRef)
	if err != nil {
		code, message := mapProcessError(err)
		return nil, code, message
	}

	var accepted *int64
	proc.mu.Lock()
	if proc.inFlight {
		proc.mu.Unlock()
		return nil, processapi.CodeProcessBusy, "process is busy"
	}
	if err := validateCursorLocked(proc, cursor); err != nil {
		proc.mu.Unlock()
		code, message := mapProcessError(err)
		return nil, code, message
	}
	proc.inFlight = true
	if req.StdinText != nil {
		if req.InputSeq == nil || *req.InputSeq <= 0 {
			proc.inFlight = false
			proc.mu.Unlock()
			return nil, processapi.CodeInputSeqRequired, "input_seq is required when stdin_text is provided"
		}
		value := *req.InputSeq
		accepted = &value
		if _, exists := proc.acceptedInputSeqs[*req.InputSeq]; !exists {
			if !proc.allowStdin || proc.stdin == nil || proc.stdinClosed {
				proc.inFlight = false
				proc.mu.Unlock()
				return nil, processapi.CodeStdinNotSupported, "process does not accept stdin"
			}
			if *req.StdinText != "" {
				if _, err := io.WriteString(proc.stdin, *req.StdinText); err != nil {
					proc.inFlight = false
					proc.mu.Unlock()
					code, message := mapProcessError(err)
					return nil, code, message
				}
			}
			proc.acceptedInputSeqs[*req.InputSeq] = struct{}{}
		}
	}
	if req.CloseStdin {
		if proc.mode == processapi.ModePTY {
			proc.inFlight = false
			proc.mu.Unlock()
			return nil, processapi.CodeCloseStdinUnsupported, "close_stdin is not supported for mode: pty"
		}
		if !proc.allowStdin || proc.stdin == nil {
			proc.inFlight = false
			proc.mu.Unlock()
			return nil, processapi.CodeStdinNotSupported, "process does not accept stdin"
		}
		if err := closeProcessStdinLocked(proc); err != nil {
			proc.inFlight = false
			proc.mu.Unlock()
			code, message := mapProcessError(err)
			return nil, code, message
		}
	}
	proc.mu.Unlock()

	resp, err := c.waitForSnapshot(proc, cursor, time.Duration(processapi.NormalizeWaitMs(req.WaitMs))*time.Millisecond, accepted)
	proc.mu.Lock()
	proc.inFlight = false
	notifyLocked(proc)
	proc.mu.Unlock()
	c.releaseProcessIfDrained(proc, resp)
	if err != nil {
		code, message := mapProcessError(err)
		return nil, code, message
	}
	return resp, "", ""
}

func (c *ProcessController) Terminate(req processapi.AgentRefRequest) (*processapi.Response, string, string) {
	status := strings.TrimSpace(req.Status)
	switch status {
	case "":
		status = processapi.StatusTerminated
	case processapi.StatusTerminated, processapi.StatusCancelled:
	default:
		status = processapi.StatusTerminated
	}
	return c.stopProcess(req.ProcessRef, status)
}

func (c *ProcessController) Cancel(req processapi.AgentRefRequest) (*processapi.Response, string, string) {
	return c.stopProcess(req.ProcessRef, processapi.StatusCancelled)
}

func (c *ProcessController) stopProcess(processRef string, status string) (*processapi.Response, string, string) {
	proc, err := c.getProcess(processRef)
	if err != nil {
		code, message := mapProcessError(err)
		return nil, code, message
	}
	proc.mu.Lock()
	head := proc.output.HeadSeq()
	if proc.status == processapi.StatusRunning {
		killProcessLocked(proc, status)
	}
	proc.mu.Unlock()

	resp, err := c.waitUntilStopped(proc, head)
	c.releaseProcessIfDrained(proc, resp)
	if err != nil {
		code, message := mapProcessError(err)
		return nil, code, message
	}
	return resp, "", ""
}

func (c *ProcessController) Resize(req processapi.AgentResizeRequest) (*processapi.Response, string, string) {
	proc, err := c.getProcess(req.ProcessRef)
	if err != nil {
		code, message := mapProcessError(err)
		return nil, code, message
	}
	proc.mu.Lock()
	defer proc.mu.Unlock()
	if proc.mode != processapi.ModePTY || proc.ptyFile == nil {
		return nil, processapi.CodeResizeNotSupported, "process is not a PTY session"
	}
	if proc.status != processapi.StatusRunning {
		return nil, processapi.CodeNotRunning, "process is not running"
	}
	if req.Rows <= 0 || req.Cols <= 0 {
		return nil, processapi.CodeInvalidSize, "rows and cols must be positive"
	}
	if err := pty.Setsize(proc.ptyFile, &pty.Winsize{Rows: uint16(req.Rows), Cols: uint16(req.Cols)}); err != nil {
		return nil, processapi.CodeResizeNotSupported, err.Error()
	}
	return &processapi.Response{Status: proc.status, ProcessRef: proc.ref}, "", ""
}

func (c *ProcessController) startProcess(req processapi.AgentExecRequest) (*managedProcess, error) {
	ref, err := newToken()
	if err != nil {
		return nil, err
	}
	shellPath, shellArgs := resolveProcessShellCommand(req.Command)
	cmd := exec.Command(shellPath, shellArgs...)
	prepareWorkloadProcessCmd(cmd, req.Cwd, req.Env)

	proc := &managedProcess{
		ref:               ref,
		mode:              processapi.NormalizeMode(req.Mode),
		cmd:               cmd,
		allowStdin:        processapi.NormalizeMode(req.Mode) == processapi.ModeStdin || processapi.NormalizeMode(req.Mode) == processapi.ModePTY,
		status:            processapi.StatusRunning,
		acceptedInputSeqs: map[int64]struct{}{},
		output:            processapi.NewItemBuffer(processRingBytes),
		updateCh:          make(chan struct{}),
	}

	switch proc.mode {
	case processapi.ModePTY:
		size := req.Size
		if size == nil {
			size = &processapi.Size{Rows: 24, Cols: 80}
		}
		file, err := pty.StartWithAttrs(cmd, &pty.Winsize{Rows: uint16(size.Rows), Cols: uint16(size.Cols)}, cmd.SysProcAttr)
		if err != nil {
			return nil, err
		}
		proc.stdin = file
		proc.ptyFile = file
		proc.readerWG.Add(1)
		go c.readStream(proc, processapi.StreamPTY, file)
	default:
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return nil, err
		}
		if proc.allowStdin {
			stdin, err := cmd.StdinPipe()
			if err != nil {
				return nil, err
			}
			proc.stdin = stdin
		}
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		proc.readerWG.Add(2)
		go c.readStream(proc, processapi.StreamStdout, stdout)
		go c.readStream(proc, processapi.StreamStderr, stderr)
	}

	if proc.mode == processapi.ModePTY {
		// StartWithAttrs already starts the command.
	} else if cmd.Process == nil {
		return nil, errors.New("process failed to start")
	}

	c.mu.Lock()
	c.processes[proc.ref] = proc
	c.mu.Unlock()
	go c.waitForExit(proc, req.TimeoutMs)
	return proc, nil
}

func (c *ProcessController) waitUntilDone(proc *managedProcess, cursor uint64, timeoutMs int) (*processapi.Response, error) {
	wait := time.Duration(processapi.NormalizeTimeoutMs(processapi.ModeBuffered, timeoutMs))*time.Millisecond + processKillGrace
	deadline := time.Now().Add(wait)
	for {
		proc.mu.Lock()
		resp, err := snapshotLocked(proc, cursor)
		ch := proc.updateCh
		running := proc.status == processapi.StatusRunning
		proc.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if !running {
			return resp, nil
		}
		if time.Now().After(deadline) {
			return nil, processapi.NewTerminalStateError()
		}
		select {
		case <-ch:
		case <-time.After(processPollTick):
		}
	}
}

func (c *ProcessController) waitUntilStopped(proc *managedProcess, cursor uint64) (*processapi.Response, error) {
	deadline := time.Now().Add(processKillGrace + processDrainGrace)
	for {
		proc.mu.Lock()
		resp, err := snapshotLocked(proc, cursor)
		ch := proc.updateCh
		running := proc.status == processapi.StatusRunning
		proc.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if !running {
			return resp, nil
		}
		if time.Now().After(deadline) {
			return nil, processapi.NewTerminalStateError()
		}
		select {
		case <-ch:
		case <-time.After(processPollTick):
		}
	}
}

func (c *ProcessController) waitForSnapshot(proc *managedProcess, cursor uint64, wait time.Duration, accepted *int64) (*processapi.Response, error) {
	deadline := time.Now().Add(wait)
	for {
		proc.mu.Lock()
		resp, err := snapshotLocked(proc, cursor)
		ch := proc.updateCh
		done := resp != nil && (len(resp.Items) > 0 || proc.status != processapi.StatusRunning || time.Now().After(deadline) || wait <= 0)
		if resp != nil && accepted != nil {
			value := *accepted
			resp.AcceptedInputSeq = &value
		}
		proc.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if done {
			return resp, nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return resp, nil
		}
		select {
		case <-ch:
		case <-time.After(minDuration(processPollTick, remaining)):
		}
	}
}

func (c *ProcessController) readStream(proc *managedProcess, stream string, reader io.ReadCloser) {
	defer proc.readerWG.Done()
	defer func() { _ = reader.Close() }()
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			proc.mu.Lock()
			proc.output.Append(stream, string(buf[:n]))
			notifyLocked(proc)
			proc.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (c *ProcessController) waitForExit(proc *managedProcess, timeoutMs int) {
	var timer *time.Timer
	if timeoutMs > 0 {
		timer = time.AfterFunc(time.Duration(timeoutMs)*time.Millisecond, func() {
			proc.mu.Lock()
			defer proc.mu.Unlock()
			if proc.status != processapi.StatusRunning {
				return
			}
			killProcessLocked(proc, processapi.StatusTimedOut)
		})
	}
	err := proc.cmd.Wait()
	if timer != nil {
		timer.Stop()
	}
	proc.readerWG.Wait()
	time.Sleep(processDrainGrace)

	proc.mu.Lock()
	defer proc.mu.Unlock()
	exitCode := exitCodeFromError(err)
	proc.exitCode = &exitCode
	if proc.requestedStatus != "" {
		proc.status = proc.requestedStatus
	} else {
		proc.status = processapi.StatusExited
	}
	notifyLocked(proc)
}

func (c *ProcessController) releaseProcessIfDrained(proc *managedProcess, resp *processapi.Response) {
	if proc == nil || resp == nil {
		return
	}
	if resp.Status == processapi.StatusRunning || resp.HasMore {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if current, ok := c.processes[proc.ref]; ok && current == proc {
		delete(c.processes, proc.ref)
	}
}

func (c *ProcessController) getProcess(processRef string) (*managedProcess, error) {
	ref := strings.TrimSpace(processRef)
	if ref == "" {
		return nil, &processapi.Error{Code: processapi.CodeProcessNotFound, Message: "process not found", HTTPStatus: 404}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	proc, ok := c.processes[ref]
	if !ok {
		return nil, &processapi.Error{Code: processapi.CodeProcessNotFound, Message: "process not found", HTTPStatus: 404}
	}
	return proc, nil
}

func snapshotLocked(proc *managedProcess, cursor uint64) (*processapi.Response, error) {
	if err := validateCursorLocked(proc, cursor); err != nil {
		return nil, err
	}
	items, next, hasMore, truncated, ok := proc.output.ReadFrom(cursor, 0)
	if !ok {
		return nil, &processapi.Error{Code: processapi.CodeInvalidCursor, Message: "cursor is invalid", HTTPStatus: 400}
	}
	stdout, stderr := flattenItems(items)
	resp := &processapi.Response{
		Status:     proc.status,
		ProcessRef: proc.ref,
		Stdout:     stdout,
		Stderr:     stderr,
		Cursor:     processapi.CursorString(cursor),
		NextCursor: processapi.CursorString(next),
		Items:      items,
		HasMore:    hasMore,
		Truncated:  truncated,
	}
	if truncated {
		resp.OutputRef = processOutputRef(proc.ref, cursor, next)
	}
	if proc.exitCode != nil {
		value := *proc.exitCode
		resp.ExitCode = &value
	}
	return resp, nil
}

func processOutputRef(processRef string, cursor uint64, next uint64) string {
	ref := strings.TrimSpace(processRef)
	if ref == "" {
		ref = "buffered"
	}
	return fmt.Sprintf("process:%s:%d:%d", ref, cursor, next)
}

func validateCursorLocked(proc *managedProcess, cursor uint64) error {
	if cursor < proc.output.HeadSeq() {
		return &processapi.Error{Code: processapi.CodeCursorExpired, Message: "cursor has expired", HTTPStatus: 409}
	}
	if cursor > proc.output.NextSeq() {
		return &processapi.Error{Code: processapi.CodeInvalidCursor, Message: "cursor is invalid", HTTPStatus: 400}
	}
	return nil
}

func closeProcessStdinLocked(proc *managedProcess) error {
	if proc.stdinClosed || proc.stdin == nil {
		return nil
	}
	if proc.mode == processapi.ModePTY {
		return errors.New("close_stdin is not supported for mode: " + proc.mode)
	}
	if err := proc.stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		return err
	}
	proc.stdinClosed = true
	return nil
}

func killProcessLocked(proc *managedProcess, status string) {
	if proc.cmd == nil || proc.cmd.Process == nil || proc.requestedStatus != "" {
		return
	}
	proc.requestedStatus = status
	if proc.mode != processapi.ModePTY {
		_ = closeProcessStdinLocked(proc)
	}
	pid := proc.cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	time.AfterFunc(processKillGrace, func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	})
}

func notifyLocked(proc *managedProcess) {
	close(proc.updateCh)
	proc.updateCh = make(chan struct{})
}

func flattenItems(items []processapi.OutputItem) (string, string) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	for _, item := range items {
		switch item.Stream {
		case processapi.StreamStderr:
			stderr.WriteString(item.Text)
		default:
			stdout.WriteString(item.Text)
		}
	}
	return stdout.String(), stderr.String()
}

func resolveProcessShellCommand(command string) (string, []string) {
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash", []string{"--noprofile", "--norc", "-lc", command}
	}
	return "/bin/sh", []string{"-lc", command}
}

func exitCodeFromError(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return status.ExitStatus()
		}
	}
	return 1
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}

func mapProcessError(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	var processErr *processapi.Error
	if errors.As(err, &processErr) {
		return processErr.Code, processErr.Message
	}
	return processapi.CodeProcessNotFound, err.Error()
}
