package local

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	shellapi "arkloop/services/sandbox/internal/shell"

	"github.com/google/uuid"
)

// pumpWG tracks outstanding pipe-drain goroutines so waitLoop can ensure
// all pipe data is captured before calling cmd.Wait() (which closes pipes).

const (
	stdoutBufSize = 1 << 20   // 1 MB (matches shellapi.RingBufferBytes)
	stderrBufSize = 64 * 1024 // 64 KB
	readBufChunk  = 4 * 1024  // io copy chunk
	killGrace     = 2 * time.Second
	cleanupDelay  = 5 * time.Minute
)

// limitedBuffer caps writes at a fixed limit, silently discarding overflow.
type limitedBuffer struct {
	limit int
	buf   []byte
}

func newLimitedBuffer(limit int) *limitedBuffer {
	if limit < 0 {
		limit = 0
	}
	initCap := limit
	if initCap > 1024 {
		initCap = 1024
	}
	return &limitedBuffer{limit: limit, buf: make([]byte, 0, initCap)}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 || len(b.buf) >= b.limit {
		return len(p), nil
	}
	remaining := b.limit - len(b.buf)
	if len(p) <= remaining {
		b.buf = append(b.buf, p...)
	} else {
		b.buf = append(b.buf, p[:remaining]...)
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	return string(b.buf)
}

// process manages a local OS process with ring-buffered stdout/stderr capture.
type process struct {
	id           string
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       *shellapi.RingBuffer
	stderr       *limitedBuffer
	killGrace    time.Duration
	cleanupDelay time.Duration

	mu       sync.Mutex
	exitCode *int
	exited   bool
	exitCh   chan struct{}
}

func startProcess(command []string, cwd string, env map[string]string, timeoutMs int, killGraceMs int, cleanupDelayMs int) (*process, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("command must not be empty")
	}

	cmd := exec.Command(command[0], command[1:]...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	if len(env) > 0 {
		merged := os.Environ()
		for k, v := range env {
			merged = append(merged, k+"="+v)
		}
		cmd.Env = merged
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start process: %w", err)
	}

	kg := killGrace
	if killGraceMs > 0 {
		kg = time.Duration(killGraceMs) * time.Millisecond
	}
	cd := cleanupDelay
	if cleanupDelayMs > 0 {
		cd = time.Duration(cleanupDelayMs) * time.Millisecond
	}

	p := &process{
		id:           uuid.NewString(),
		cmd:          cmd,
		stdin:        stdinPipe,
		stdout:       shellapi.NewRingBuffer(stdoutBufSize),
		stderr:       newLimitedBuffer(stderrBufSize),
		killGrace:    kg,
		cleanupDelay: cd,
		exitCh:       make(chan struct{}),
	}

	var pumpDone sync.WaitGroup
	pumpDone.Add(2)
	go p.pumpOutput(stdoutPipe, true, &pumpDone)
	go p.pumpOutput(stderrPipe, false, &pumpDone)
	go p.waitLoop(&pumpDone)

	if timeoutMs > 0 {
		go p.enforceTimeout(time.Duration(timeoutMs) * time.Millisecond)
	}

	return p, nil
}

func (p *process) pumpOutput(r io.Reader, isStdout bool, wg *sync.WaitGroup) {
	defer wg.Done()
	buf := make([]byte, readBufChunk)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			p.mu.Lock()
			if isStdout {
				p.stdout.Append(buf[:n])
			} else {
				_, _ = p.stderr.Write(buf[:n])
			}
			p.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (p *process) waitLoop(pumpDone *sync.WaitGroup) {
	// Drain all pipe readers before calling cmd.Wait(), which closes them.
	pumpDone.Wait()
	err := p.cmd.Wait()
	code := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			code = -1
		}
	}

	p.mu.Lock()
	p.exitCode = &code
	p.exited = true
	p.mu.Unlock()

	close(p.exitCh)
}

func (p *process) enforceTimeout(d time.Duration) {
	select {
	case <-p.exitCh:
	case <-time.After(d):
		_ = p.cmd.Process.Kill()
	}
}
