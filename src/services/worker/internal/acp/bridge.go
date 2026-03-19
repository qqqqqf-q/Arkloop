package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"arkloop/services/worker/internal/events"
)

const (
	defaultPollInterval   = 500 * time.Millisecond
	defaultReadMaxBytes   = 32 * 1024
	defaultControlTimeout = 5 * time.Second
)

// BridgeConfig holds configuration for a single ACP bridge run.
type BridgeConfig struct {
	RuntimeSessionKey string
	AccountID         string
	Tier              string
	Command           []string // agent launch command, e.g. ["opencode","acp","--cwd","/workspace"]
	Cwd               string   // workspace directory inside sandbox
	Env               map[string]string

	PollInterval   time.Duration // how often to read stdout, default 500ms
	ReadMaxBytes   int           // max bytes per read, default 32KB
	KillGraceMs    int           // configurable kill grace period
	CleanupDelayMs int           // configurable cleanup delay
	// StandardCancelCalibrated gates session/cancel usage behind an explicit
	// contract calibration flag. When false, cancellation falls back to host stop.
	StandardCancelCalibrated bool
}

// BridgeState holds the serializable state of a Bridge for registry storage.
type BridgeState struct {
	HostProcessID     string
	ProtocolSessionID string
	OutputCursor      uint64
	AgentVersion      string
}

// EnsureSessionResult describes whether EnsureSession created a new runtime handle.
type EnsureSessionResult struct {
	Created bool
}

// Bridge manages a single ACP session lifecycle.
type Bridge struct {
	tr                ProcessHost
	config            BridgeConfig
	hostProcessID     string // host-side process/runtime handle
	protocolSessionID string // provider protocol session id returned by session/new
	agentVersion      string // set after Start
	outputCursor      uint64 // read cursor for stdout
	msgIDSeq          int    // JSON-RPC message ID sequence
}

// NewBridge creates a Bridge. The host can be backed by sandbox or local processes.
func NewBridge(tr ProcessHost, config BridgeConfig) *Bridge {
	if config.PollInterval == 0 {
		config.PollInterval = defaultPollInterval
	}
	if config.ReadMaxBytes == 0 {
		config.ReadMaxBytes = defaultReadMaxBytes
	}
	return &Bridge{tr: tr, config: config}
}

// State returns the current bridge state for serialization.
func (b *Bridge) State() BridgeState {
	return BridgeState{
		HostProcessID:     b.hostProcessID,
		ProtocolSessionID: b.protocolSessionID,
		OutputCursor:      b.outputCursor,
		AgentVersion:      b.agentVersion,
	}
}

// Bind connects the bridge to an existing process and ACP session without starting a new one.
func (b *Bridge) Bind(state BridgeState) {
	b.hostProcessID = state.HostProcessID
	b.protocolSessionID = state.ProtocolSessionID
	b.outputCursor = state.OutputCursor
	b.agentVersion = state.AgentVersion
}

// CheckRuntimeAlive queries the host for the existing runtime handle status.
// Returns nil if the process is running, error otherwise.
// Also updates the cursor to the latest stdout position.
func (b *Bridge) CheckRuntimeAlive(ctx context.Context) error {
	if b.hostProcessID == "" {
		return fmt.Errorf("bridge: no process bound")
	}
	resp, err := b.tr.Status(ctx, StatusRequest{
		RuntimeSessionKey: b.config.RuntimeSessionKey,
		AccountID:         b.config.AccountID,
		ProcessID:         b.hostProcessID,
	})
	if err != nil {
		return fmt.Errorf("bridge: status check failed: %w", err)
	}
	if !resp.Running {
		return fmt.Errorf("bridge: process %s is not running (exited=%v)", b.hostProcessID, resp.Exited)
	}
	b.outputCursor = resp.StdoutCursor
	return nil
}

// CheckAlive keeps the older method name used by existing callers and tests.
func (b *Bridge) CheckAlive(ctx context.Context) error {
	return b.CheckRuntimeAlive(ctx)
}

// EnsureSession makes sure the host runtime process and ACP protocol session exist.
func (b *Bridge) EnsureSession(ctx context.Context) (*EnsureSessionResult, error) {
	result := &EnsureSessionResult{}
	if err := b.ensureHostProcess(ctx, result); err != nil {
		return nil, err
	}
	if err := b.ensureProtocolSession(ctx, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (b *Bridge) emitRunStarted(reused bool, emitter events.Emitter, yield func(events.RunEvent) error) error {
	return yield(emitter.Emit("run.started", map[string]any{
		"status":              "working",
		"command":             b.config.Command,
		"agent_version":       b.agentVersion,
		"runtime_session_key": b.config.RuntimeSessionKey,
		"reused":              reused,
	}, nil, nil))
}

// RunPrompt sends a prompt to an already-established ACP session.
// The bridge must already be bound to a runtime handle and protocol session.
func (b *Bridge) RunPrompt(ctx context.Context, prompt string, emitter events.Emitter, yield func(events.RunEvent) error) error {
	if b.hostProcessID == "" {
		return fmt.Errorf("bridge: no process bound, call EnsureSession or Bind first")
	}
	if b.protocolSessionID == "" {
		return fmt.Errorf("bridge: no ACP session, call EnsureSession or Bind first")
	}

	if err := b.emitRunStarted(true, emitter, yield); err != nil {
		return err
	}

	if err := b.sendMessage(ctx, NewSessionPromptMessage(b.nextID(), b.protocolSessionID, prompt)); err != nil {
		return fmt.Errorf("bridge: send session/prompt: %w", err)
	}

	return b.pollUpdates(ctx, emitter, yield)
}

// Close explicitly stops the ACP process. Call when the session is truly done.
func (b *Bridge) Close() {
	b.cleanup()
}

func (b *Bridge) nextID() int {
	b.msgIDSeq++
	return b.msgIDSeq
}

func (b *Bridge) ensureHostProcess(ctx context.Context, result *EnsureSessionResult) error {
	if b.hostProcessID != "" {
		return nil
	}
	cmd := b.config.Command
	if len(cmd) == 0 {
		return fmt.Errorf("acp bridge: command not configured")
	}

	resp, err := b.tr.Start(ctx, StartRequest{
		RuntimeSessionKey: b.config.RuntimeSessionKey,
		AccountID:         b.config.AccountID,
		Tier:              b.config.Tier,
		Command:           cmd,
		Cwd:               b.config.Cwd,
		Env:               b.config.Env,
		KillGraceMs:       b.config.KillGraceMs,
		CleanupDelayMs:    b.config.CleanupDelayMs,
	})
	if err != nil {
		return fmt.Errorf("bridge: start opencode process: %w", err)
	}
	b.hostProcessID = resp.ProcessID
	b.agentVersion = resp.AgentVersion
	result.Created = true
	slog.Info("acp: agent process started", "process_id", b.hostProcessID, "runtime_session_key", b.config.RuntimeSessionKey, "command", cmd[0])
	return nil
}

func (b *Bridge) ensureProtocolSession(ctx context.Context, result *EnsureSessionResult) error {
	if b.protocolSessionID != "" {
		return nil
	}
	if err := b.sendMessage(ctx, NewSessionNewMessage(b.nextID(), SessionModeCode, b.config.Cwd)); err != nil {
		return fmt.Errorf("bridge: send session/new: %w", err)
	}
	if err := b.waitForSessionNew(ctx); err != nil {
		return fmt.Errorf("bridge: wait for session/new response: %w", err)
	}
	result.Created = true
	slog.Info("acp: session created", "protocol_session_id", b.protocolSessionID, "runtime_session_key", b.config.RuntimeSessionKey)
	return nil
}

// EnsureAndRunTurn ensures the session exists and then runs one prompt turn on it.
func (b *Bridge) EnsureAndRunTurn(ctx context.Context, prompt string, emitter events.Emitter, yield func(events.RunEvent) error) error {
	ensureResult, err := b.EnsureSession(ctx)
	if err != nil {
		return err
	}
	if err := b.emitRunStarted(!ensureResult.Created, emitter, yield); err != nil {
		return err
	}
	if err := b.sendMessage(ctx, NewSessionPromptMessage(b.nextID(), b.protocolSessionID, prompt)); err != nil {
		return fmt.Errorf("bridge: send session/prompt: %w", err)
	}
	return b.pollUpdates(ctx, emitter, yield)
}

// Run keeps the original method shape for existing callers.
func (b *Bridge) Run(ctx context.Context, prompt string, emitter events.Emitter, yield func(events.RunEvent) error) error {
	return b.EnsureAndRunTurn(ctx, prompt, emitter, yield)
}

func (b *Bridge) sendMessage(ctx context.Context, msg ACPMessage) error {
	data, err := MarshalMessage(msg)
	if err != nil {
		return err
	}
	return b.tr.Write(ctx, WriteRequest{
		RuntimeSessionKey: b.config.RuntimeSessionKey,
		AccountID:         b.config.AccountID,
		ProcessID:         b.hostProcessID,
		Data:              string(data),
	})
}

func (b *Bridge) read(ctx context.Context) (*ReadResponse, error) {
	return b.tr.Read(ctx, ReadRequest{
		RuntimeSessionKey: b.config.RuntimeSessionKey,
		AccountID:         b.config.AccountID,
		ProcessID:         b.hostProcessID,
		Cursor:            b.outputCursor,
		MaxBytes:          b.config.ReadMaxBytes,
	})
}

func (b *Bridge) waitForSessionNew(ctx context.Context) error {
	for {
		resp, err := b.read(ctx)
		if err != nil {
			return fmt.Errorf("bridge: read stdout: %w", err)
		}
		b.outputCursor = resp.NextCursor

		if resp.Data != "" {
			if sid, ok := parseSessionNewResponse(resp.Data); ok {
				b.protocolSessionID = sid
				return nil
			}
		}

		if resp.Exited {
			return fmt.Errorf("bridge: opencode process exited before session/new response (check sandbox logs)")
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(b.config.PollInterval):
		}
	}
}

// parseSessionNewResponse extracts session_id from a JSON-RPC result in stdout data.
func parseSessionNewResponse(data string) (string, bool) {
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
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
		if result.SessionID != "" {
			return result.SessionID, true
		}
	}
	return "", false
}

func (b *Bridge) pollUpdates(ctx context.Context, emitter events.Emitter, yield func(events.RunEvent) error) error {
	for {
		resp, err := b.read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return b.handleCancellation(emitter, yield)
			}
			return fmt.Errorf("bridge: read stdout: %w", err)
		}
		b.outputCursor = resp.NextCursor

		if resp.Data != "" {
			updates, err := ParseUpdates(resp.Data)
			if err != nil {
				slog.Warn("acp: parse updates failed", "error", err)
			}
			for _, u := range updates {
				if u.Type == UpdateTypePermission {
					return b.handlePermissionRequest(emitter, yield, u)
				}

				ev, ok := mapUpdateToEvent(u, emitter)
				if !ok || ev.Type == "run.started" {
					continue // run.started already emitted before poll loop
				}
				if err := yield(ev); err != nil {
					return err
				}
				if u.Type == UpdateTypeComplete || u.Type == UpdateTypeError {
					return nil
				}
			}
		}

		if resp.Exited {
			errClass := "acp.process_exited"
			diagnostic := map[string]any{
				"error_class":         errClass,
				"message":             "opencode process exited unexpectedly",
				"layer":               "opencode",
				"process_id":          b.hostProcessID,
				"runtime_session_key": b.config.RuntimeSessionKey,
				"command":             b.config.Command,
				"agent_version":       b.agentVersion,
			}
			if resp.ErrorSummary != "" {
				diagnostic["error_summary"] = resp.ErrorSummary
			}
			if resp.Stderr != "" && len(resp.Stderr) <= 1024 {
				diagnostic["stderr_tail"] = resp.Stderr
			} else if resp.Stderr != "" {
				diagnostic["stderr_tail"] = resp.Stderr[len(resp.Stderr)-1024:]
			}
			return yield(emitter.Emit("run.failed", diagnostic, nil, &errClass))
		}

		select {
		case <-ctx.Done():
			return b.handleCancellation(emitter, yield)
		case <-time.After(b.config.PollInterval):
		}
	}
}

func (b *Bridge) handleCancellation(emitter events.Emitter, yield func(events.RunEvent) error) error {
	cancelPayload := map[string]any{
		"reason":      "context_cancelled",
		"cancel_mode": "host_stop",
	}

	if b.protocolSessionID != "" && b.config.StandardCancelCalibrated {
		if err := b.sendStandardCancel(); err != nil {
			slog.Warn("acp: standard cancel failed, falling back to host stop", "error", err, "process_id", b.hostProcessID)
			cancelPayload["fallback_from"] = "session_cancel"
		} else {
			stopped, err := b.waitForTurnStop()
			if err == nil && stopped {
				cancelPayload["cancel_mode"] = "session_cancel"
				return yield(emitter.Emit("run.cancelled", cancelPayload, nil, nil))
			}
			slog.Warn("acp: session cancel did not settle turn, falling back to host stop", "error", err, "process_id", b.hostProcessID)
			cancelPayload["fallback_from"] = "session_cancel"
		}
	}

	if err := b.stopHostProcess(defaultControlTimeout, false); err != nil {
		errClass := "acp.cancel_failed"
		failed := map[string]any{
			"error_class": errClass,
			"message":     "failed to stop ACP process after cancellation",
			"layer":       "opencode",
			"process_id":  b.hostProcessID,
		}
		if fallbackFrom, ok := cancelPayload["fallback_from"]; ok {
			failed["fallback_from"] = fallbackFrom
		}
		failed["stop_error"] = err.Error()
		return yield(emitter.Emit("run.failed", failed, nil, &errClass))
	}

	return yield(emitter.Emit("run.cancelled", cancelPayload, nil, nil))
}

func (b *Bridge) handlePermissionRequest(emitter events.Emitter, yield func(events.RunEvent) error, update SessionUpdateParams) error {
	slog.Warn("acp: permission request observed before contract calibration",
		"permission_id", update.PermissionID,
		"description", update.Content,
		"sensitive", update.Sensitive,
		"runtime_session_key", b.config.RuntimeSessionKey,
	)

	if err := yield(emitter.Emit("acp.permission_required", map[string]any{
		"permission_id":      update.PermissionID,
		"description":        update.Content,
		"sensitive":          update.Sensitive,
		"approved":           false,
		"response_supported": false,
	}, nil, nil)); err != nil {
		return err
	}

	stopErr := b.stopHostProcess(defaultControlTimeout, false)
	errClass := "acp.permission_unsupported"
	failed := map[string]any{
		"error_class":        errClass,
		"message":            "provider requested permission before session/permission was calibrated",
		"layer":              "opencode",
		"permission_id":      update.PermissionID,
		"sensitive":          update.Sensitive,
		"response_supported": false,
	}
	if update.Content != "" {
		failed["description"] = update.Content
	}
	if stopErr != nil {
		failed["stop_error"] = stopErr.Error()
	}
	return yield(emitter.Emit("run.failed", failed, nil, &errClass))
}

func (b *Bridge) sendStandardCancel() error {
	cancelCtx, cancel := context.WithTimeout(context.Background(), defaultControlTimeout)
	defer cancel()
	return b.sendMessage(cancelCtx, NewSessionCancelMessage(b.nextID(), b.protocolSessionID))
}

func (b *Bridge) waitForTurnStop() (bool, error) {
	waitCtx, cancel := context.WithTimeout(context.Background(), defaultControlTimeout)
	defer cancel()

	for {
		resp, err := b.read(waitCtx)
		if err != nil {
			return false, err
		}
		b.outputCursor = resp.NextCursor

		if resp.Data != "" {
			updates, err := ParseUpdates(resp.Data)
			if err != nil {
				slog.Warn("acp: parse updates during cancel wait failed", "error", err)
			}
			for _, u := range updates {
				if isTurnStoppedUpdate(u) {
					return true, nil
				}
			}
		}

		if resp.Exited {
			return true, nil
		}

		select {
		case <-waitCtx.Done():
			return false, waitCtx.Err()
		case <-time.After(b.config.PollInterval):
		}
	}
}

func isTurnStoppedUpdate(update SessionUpdateParams) bool {
	if update.Type == UpdateTypeComplete || update.Type == UpdateTypeError {
		return true
	}
	return update.Type == UpdateTypeStatus && update.Status == StatusIdle
}

func (b *Bridge) stopHostProcess(timeout time.Duration, force bool) error {
	if b.hostProcessID == "" {
		return nil
	}
	processID := b.hostProcessID
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := b.tr.Stop(ctx, StopRequest{
		RuntimeSessionKey: b.config.RuntimeSessionKey,
		AccountID:         b.config.AccountID,
		ProcessID:         processID,
		Force:             force,
		GracePeriodMs:     b.config.KillGraceMs,
	}); err != nil {
		return err
	}

	b.hostProcessID = ""
	b.protocolSessionID = ""
	b.outputCursor = 0
	return nil
}

func (b *Bridge) cleanup() {
	if err := b.stopHostProcess(10*time.Second, false); err != nil {
		slog.Warn("acp: stop process failed", "error", err, "process_id", b.hostProcessID)
	}
}

func mapUpdateToEvent(update SessionUpdateParams, emitter events.Emitter) (events.RunEvent, bool) {
	switch update.Type {
	case UpdateTypeStatus:
		if update.Status == StatusWorking {
			return emitter.Emit("run.started", map[string]any{"status": "working"}, nil, nil), true
		}
		return events.RunEvent{}, false

	case UpdateTypeTextDelta:
		return emitter.Emit("message.delta", map[string]any{
			"content_delta": update.Content,
			"role":          "assistant",
		}, nil, nil), true

	case UpdateTypeToolCall:
		name := update.Name
		// tool_call_update with status "completed" has result content
		if update.Status == "completed" {
			return emitter.Emit("tool.result", map[string]any{
				"tool_name": update.Name,
				"output":    update.Output,
			}, &name, nil), true
		}
		return emitter.Emit("tool.call", map[string]any{
			"tool_name": update.Name,
			"arguments": update.Arguments,
		}, &name, nil), true

	case UpdateTypeToolResult:
		name := update.Name
		return emitter.Emit("tool.result", map[string]any{
			"tool_name": update.Name,
			"output":    update.Output,
		}, &name, nil), true

	case UpdateTypeComplete:
		return emitter.Emit("run.completed", map[string]any{
			"summary": update.Summary,
		}, nil, nil), true

	case UpdateTypeError:
		errClass := "acp.agent_error"
		return emitter.Emit("run.failed", map[string]any{
			"error_class": errClass,
			"message":     update.Message,
			"layer":       "opencode",
		}, nil, &errClass), true
	}

	return events.RunEvent{}, false
}
