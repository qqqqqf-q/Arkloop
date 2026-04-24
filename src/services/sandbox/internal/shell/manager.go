package shell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"arkloop/services/sandbox/internal/environment"
	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
	sandboxskills "arkloop/services/sandbox/internal/skills"
	"arkloop/services/shared/objectstore"
)

const (
	// maxShellSessions is the maximum number of shell sessions allowed.
	// When reached, LRU eviction is attempted before rejecting new sessions.
	maxShellSessions = 64
	// protectedRing is the number of most recently used sessions protected from LRU eviction.
	protectedRing = 8
)

type Service interface {
	ExecCommand(ctx context.Context, req ExecCommandRequest) (*Response, error)
	WriteStdin(ctx context.Context, req WriteStdinRequest) (*Response, error)
	ReadOutputDeltas(ctx context.Context, sessionID, accountID string) (*OutputDeltasResponse, error)
	DebugSnapshot(ctx context.Context, sessionID, accountID string) (*DebugResponse, error)
	ForkSession(ctx context.Context, req ForkSessionRequest) (*ForkSessionResponse, error)
	Close(ctx context.Context, sessionID, accountID string) error
}

type Manager struct {
	compute         *session.Manager
	artifactStore   artifactStore
	stateStore      stateStore
	restoreRegistry SessionRestoreRegistry
	envManager      *environment.Manager
	skillOverlay    *sandboxskills.OverlayManager
	logger          *logging.JSONLogger
	config          Config

	mu       sync.Mutex
	sessions map[string]*managedSession
	// lruOrder tracks session access order for LRU eviction.
	// Most recently used is at the end; oldest is at the front.
	// Only sessions with compute != nil are considered for eviction.
	lruOrder []string
}

type managedSession struct {
	mu sync.Mutex

	compute      *session.Session
	accountID        string
	profileRef   string
	workspaceRef string
	commandSeq   int64
	uploadedSeq  int64
	artifactSeen map[string]artifactVersion
}

type transportError struct {
	err error
}

func (e *transportError) Error() string {
	return e.err.Error()
}

func (e *transportError) Unwrap() error {
	return e.err
}

func NewManager(compute *session.Manager, artifactStore artifactStore, stateStore stateStore, restoreRegistry SessionRestoreRegistry, envManager *environment.Manager, skillOverlay *sandboxskills.OverlayManager, logger *logging.JSONLogger, cfg Config) *Manager {
	if restoreRegistry == nil {
		restoreRegistry = NewMemorySessionRestoreRegistry()
	}
	normalized := normalizeConfig(cfg)
	mgr := &Manager{
		compute:         compute,
		artifactStore:   artifactStore,
		stateStore:      stateStore,
		restoreRegistry: restoreRegistry,
		envManager:      envManager,
		skillOverlay:    skillOverlay,
		logger:          logger,
		config:          normalized,
		sessions:        make(map[string]*managedSession),
	}
	if compute != nil {
		compute.SetBeforeDelete(mgr.beforeComputeDelete)
	}
	return mgr
}

func (m *Manager) ExecCommand(ctx context.Context, req ExecCommandRequest) (*Response, error) {
	if err := ValidateTimeoutMs(req.TimeoutMs); err != nil {
		return nil, err
	}
	req.OpenMode = NormalizeOpenMode(strings.TrimSpace(req.OpenMode))
	entry, created := m.getOrCreateEntry(req.SessionID, req.AccountID)
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if err := ensureAccount(entry.accountID, req.AccountID); err != nil {
		return nil, err
	}

	shellEnv := map[string]string(nil)
	prepared := req
	createdCompute := false
	restored := false
	restoreRevision := ""
	if entry.compute == nil {
		var err error
		prepared, shellEnv, restored, restoreRevision, createdCompute, err = m.openExecCommandSession(ctx, req, entry)
		if err != nil {
			if createdCompute {
				if m.envManager != nil {
					m.envManager.Drop(req.SessionID)
				}
				entry.compute = nil
				_ = m.compute.DeleteSkipHook(ctx, req.SessionID, req.AccountID)
			}
			if created {
				m.dropEntry(req.SessionID, entry)
			}
			return nil, err
		}
	} else {
		prepared := m.boundIdentityRequest(req, entry)
		if err := m.prepareEnvironment(ctx, prepared, entry); err != nil {
			return nil, err
		}
		req = prepared
		if ref := strings.TrimSpace(req.ProfileRef); ref != "" {
			entry.profileRef = ref
		}
		if ref := strings.TrimSpace(req.WorkspaceRef); ref != "" {
			entry.workspaceRef = ref
		}
	}

	entry.commandSeq++
	result, err := m.invokeExecCommand(ctx, entry, prepared, shellEnv)
	if err != nil {
		entry.commandSeq--
		if _, ok := err.(*transportError); ok {
			m.dropEntry(req.SessionID, entry)
			if createdCompute {
				if m.envManager != nil {
					m.envManager.Drop(req.SessionID)
				}
				_ = m.compute.DeleteSkipHook(ctx, req.SessionID, req.AccountID)
			}
			if !created {
				return nil, notFoundError()
			}
			return nil, notFoundError()
		}
		return nil, err
	}

	resp := m.toResponse(req.SessionID, result)
	resp.Restored = restored
	resp.RestoreRevision = restoreRevision
	m.attachArtifacts(ctx, req.SessionID, entry, result, resp)
	if result != nil && !result.Running && m.envManager != nil {
		m.envManager.MarkAllDirty(req.SessionID)
	}
	return resp, nil
}

func (m *Manager) openExecCommandSession(ctx context.Context, req ExecCommandRequest, entry *managedSession) (ExecCommandRequest, map[string]string, bool, string, bool, error) {
	computeSession, err := m.compute.GetOrCreate(ctx, req.SessionID, req.Tier, req.AccountID)
	if err != nil {
		if strings.Contains(err.Error(), "account mismatch") {
			return ExecCommandRequest{}, nil, false, "", false, accountMismatchError()
		}
		if strings.Contains(err.Error(), "max sessions reached:") {
			// Attempt LRU eviction of oldest non-protected session before retrying.
			if m.evictOldestNonProtected(ctx) {
				computeSession, err = m.compute.GetOrCreate(ctx, req.SessionID, req.Tier, req.AccountID)
				if err != nil {
					return ExecCommandRequest{}, nil, false, "", false, maxSessionsExceededError(m.compute.MaxSessions())
				}
			} else {
				return ExecCommandRequest{}, nil, false, "", false, maxSessionsExceededError(m.compute.MaxSessions())
			}
		} else {
			return ExecCommandRequest{}, nil, false, "", false, fmt.Errorf("get shell compute session: %w", err)
		}
	}
	entry.compute = computeSession
	entry.accountID = computeSession.AccountID
	if entry.artifactSeen == nil {
		entry.artifactSeen = make(map[string]artifactVersion)
	}
	prepared, shellEnv, restored, restoreRevision := m.prepareExecCommandRequest(ctx, req, entry)
	prepared = m.boundIdentityRequest(prepared, entry)
	if req.OpenMode == OpenModeAttachOrRestore && !restored {
		if m.envManager != nil {
			m.envManager.Drop(req.SessionID)
		}
		entry.compute = nil
		_ = m.compute.DeleteSkipHook(ctx, req.SessionID, req.AccountID)
		return ExecCommandRequest{}, nil, false, "", true, notFoundError()
	}
	if err := m.prepareEnvironment(ctx, prepared, entry); err != nil {
		return ExecCommandRequest{}, nil, false, "", true, err
	}
	if ref := strings.TrimSpace(prepared.ProfileRef); ref != "" {
		entry.profileRef = ref
	}
	if ref := strings.TrimSpace(prepared.WorkspaceRef); ref != "" {
		entry.workspaceRef = ref
	}
	return prepared, shellEnv, restored, restoreRevision, true, nil
}

func (m *Manager) boundIdentityRequest(req ExecCommandRequest, entry *managedSession) ExecCommandRequest {
	prepared := req
	if entry == nil {
		return prepared
	}
	if ref := strings.TrimSpace(entry.profileRef); ref != "" {
		prepared.ProfileRef = ref
	}
	if ref := strings.TrimSpace(entry.workspaceRef); ref != "" {
		prepared.WorkspaceRef = ref
	}
	return prepared
}

func (m *Manager) WriteStdin(ctx context.Context, req WriteStdinRequest) (*Response, error) {
	entry, err := m.getExistingEntry(req.SessionID, req.AccountID)
	if err != nil {
		return nil, err
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if err := ensureAccount(entry.accountID, req.AccountID); err != nil {
		return nil, err
	}

	result, err := m.invokeWriteStdin(ctx, entry, req)
	if err != nil {
		if _, ok := err.(*transportError); ok {
			m.dropEntry(req.SessionID, entry)
			if m.envManager != nil {
				m.envManager.Drop(req.SessionID)
			}
			return nil, notFoundError()
		}
		return nil, err
	}

	resp := m.toResponse(req.SessionID, result)
	m.attachArtifacts(ctx, req.SessionID, entry, result, resp)
	if result != nil && !result.Running && m.envManager != nil {
		m.envManager.MarkAllDirty(req.SessionID)
	}
	return resp, nil
}

func (m *Manager) DebugSnapshot(ctx context.Context, sessionID, accountID string) (*DebugResponse, error) {
	entry, err := m.getExistingEntry(sessionID, accountID)
	if err != nil {
		return nil, err
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if err := ensureAccount(entry.accountID, accountID); err != nil {
		return nil, err
	}

	result, err := m.invokeDebugSnapshot(ctx, entry)
	if err != nil {
		if _, ok := err.(*transportError); ok {
			m.dropEntry(sessionID, entry)
			return nil, notFoundError()
		}
		return nil, err
	}

	return m.toDebugResponse(sessionID, result), nil
}

func (m *Manager) ReadOutputDeltas(ctx context.Context, sessionID, accountID string) (*OutputDeltasResponse, error) {
	entry, err := m.getExistingEntry(sessionID, accountID)
	if err != nil {
		return nil, err
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if err := ensureAccount(entry.accountID, accountID); err != nil {
		return nil, err
	}

	result, err := m.invokeReadOutputDeltas(ctx, entry)
	if err != nil {
		if _, ok := err.(*transportError); ok {
			m.dropEntry(sessionID, entry)
			return nil, notFoundError()
		}
		return nil, err
	}

	return result, nil
}

func (m *Manager) ForkSession(ctx context.Context, req ForkSessionRequest) (*ForkSessionResponse, error) {
	req.FromSessionID = strings.TrimSpace(req.FromSessionID)
	req.ToSessionID = strings.TrimSpace(req.ToSessionID)
	req.AccountID = strings.TrimSpace(req.AccountID)
	if req.FromSessionID == "" || req.ToSessionID == "" {
		return nil, notFoundError()
	}
	if req.FromSessionID == req.ToSessionID {
		return nil, fmt.Errorf("fork source and destination must differ")
	}
	if m.stateStore == nil {
		return nil, notFoundError()
	}
	if entry, err := m.getExistingEntry(req.FromSessionID, req.AccountID); err == nil {
		entry.mu.Lock()
		if err := ensureAccount(entry.accountID, req.AccountID); err != nil {
			entry.mu.Unlock()
			return nil, err
		}
		if m.envManager != nil {
			if err := m.envManager.FlushNow(ctx, req.FromSessionID); err != nil {
				entry.mu.Unlock()
				return nil, err
			}
		}
		if err := m.saveRestoreStateLocked(ctx, req.FromSessionID, entry); err != nil {
			var shellErr *Error
			if errors.As(err, &shellErr) && shellErr.Code == CodeSessionBusy {
				entry.mu.Unlock()
				if _, loadErr := loadLatestRestoreState(ctx, m.stateStore, m.restoreRegistry, req.AccountID, req.FromSessionID); loadErr == nil {
					goto copyRestoreState
				} else if objectstore.IsNotFound(loadErr) || errors.Is(loadErr, os.ErrNotExist) {
					return nil, busyError()
				} else {
					return nil, loadErr
				}
			}
			entry.mu.Unlock()
			return nil, err
		}
		entry.mu.Unlock()
	}

copyRestoreState:
	revision, err := copyLatestRestoreState(ctx, m.stateStore, m.restoreRegistry, req.AccountID, req.FromSessionID, req.ToSessionID, m.config.RestoreTTL)
	if err != nil {
		if objectstore.IsNotFound(err) {
			return nil, notFoundError()
		}
		return nil, err
	}
	return &ForkSessionResponse{RestoreRevision: revision}, nil
}

func (m *Manager) Close(ctx context.Context, sessionID, accountID string) error {
	entry, err := m.getExistingEntry(sessionID, accountID)
	if err != nil {
		return err
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if err := ensureAccount(entry.accountID, accountID); err != nil {
		return err
	}
	if entry.compute == nil {
		return notFoundError()
	}
	if m.envManager != nil {
		if err := m.envManager.FlushNow(ctx, sessionID); err != nil {
			return err
		}
	}
	if err := m.saveRestoreStateLocked(ctx, sessionID, entry); err != nil {
		return err
	}
	deleteErr := m.compute.DeleteSkipHook(ctx, sessionID, accountID)
	if deleteErr != nil && strings.Contains(deleteErr.Error(), "account mismatch") {
		return accountMismatchError()
	}
	if deleteErr != nil {
		return notFoundError()
	}
	if m.envManager != nil {
		m.envManager.Drop(sessionID)
	}
	m.dropEntry(sessionID, entry)
	return nil
}

func ensureAccount(boundAccountID, accountID string) error {
	if accountID != "" && boundAccountID != "" && accountID != boundAccountID {
		return accountMismatchError()
	}
	return nil
}

func (m *Manager) getOrCreateEntry(sessionID, accountID string) (*managedSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry, ok := m.sessions[sessionID]; ok {
		// Move to end of LRU order (most recently used).
		m.moveToEndLRU(sessionID)
		return entry, false
	}
	entry := &managedSession{accountID: accountID, artifactSeen: make(map[string]artifactVersion)}
	m.sessions[sessionID] = entry
	m.lruOrder = append(m.lruOrder, sessionID)
	return entry, true
}

func (m *Manager) getExistingEntry(sessionID, accountID string) (*managedSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.sessions[sessionID]
	if !ok {
		return nil, notFoundError()
	}
	if err := ensureAccount(entry.accountID, accountID); err != nil {
		return nil, err
	}
	return entry, nil
}

func (m *Manager) dropEntry(sessionID string, entry *managedSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current, ok := m.sessions[sessionID]; ok && current == entry {
		delete(m.sessions, sessionID)
		m.removeFromLRU(sessionID)
	}
}

// moveToEndLRU moves sessionID to the end of lruOrder (most recently used).
func (m *Manager) moveToEndLRU(sessionID string) {
	for i, id := range m.lruOrder {
		if id == sessionID {
			m.lruOrder = append(m.lruOrder[:i], m.lruOrder[i+1:]...)
			m.lruOrder = append(m.lruOrder, sessionID)
			return
		}
	}
}

// removeFromLRU removes sessionID from lruOrder.
func (m *Manager) removeFromLRU(sessionID string) {
	for i, id := range m.lruOrder {
		if id == sessionID {
			m.lruOrder = append(m.lruOrder[:i], m.lruOrder[i+1:]...)
			return
		}
	}
}

// evictOldestNonProtected evicts the oldest non-protected session to make room for a new one.
// Returns true if eviction was performed, false if no evictable session was found.
// Protected sessions are the most recently used ones (protectedRing count from end of lruOrder).
func (m *Manager) evictOldestNonProtected(ctx context.Context) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find oldest session that:
	// 1. Has a compute session allocated
	// 2. Is not in the protected ring (most recent protectedRing sessions)
	// 3. Is not the current session being created (entry.compute == nil at this point)
	for i, sessionID := range m.lruOrder {
		// Protected ring starts from the end (most recently used).
		protectedStart := len(m.lruOrder) - protectedRing
		if i >= protectedStart {
			// This session is protected.
			continue
		}
		entry, ok := m.sessions[sessionID]
		if !ok || entry == nil {
			continue
		}
		entry.mu.Lock()
		hasCompute := entry.compute != nil
		entry.mu.Unlock()
		if !hasCompute {
			continue
		}
		// Evict this session.
		m.logger.Info("shell_lru_evict", logging.LogFields{SessionID: &sessionID}, map[string]any{
			"reason":          "max_sessions_reached",
			"active_sessions": len(m.sessions),
		})
		if m.envManager != nil {
			_ = m.envManager.FlushNow(ctx, sessionID)
			m.envManager.Drop(sessionID)
		}
		_ = m.compute.DeleteSkipHook(ctx, sessionID, entry.accountID)
		delete(m.sessions, sessionID)
		m.lruOrder = append(m.lruOrder[:i], m.lruOrder[i+1:]...)
		return true
	}
	return false
}

func (m *Manager) prepareExecCommandRequest(ctx context.Context, req ExecCommandRequest, entry *managedSession) (ExecCommandRequest, map[string]string, bool, string) {
	prepared := req
	if entry.compute == nil || m.stateStore == nil {
		return prepared, nil, false, ""
	}
	state, err := loadLatestRestoreState(ctx, m.stateStore, m.restoreRegistry, entry.accountID, req.SessionID)
	if err != nil {
		if objectstore.IsNotFound(err) {
			return prepared, nil, false, ""
		}
		m.logger.Warn("shell restore state load failed", logging.LogFields{SessionID: &req.SessionID}, map[string]any{"error": err.Error()})
		return prepared, nil, false, ""
	}
	entry.commandSeq = state.LastCommandSeq
	entry.uploadedSeq = state.UploadedSeq
	entry.artifactSeen = cloneArtifactSeen(state.ArtifactSeen)
	if entry.profileRef == "" {
		entry.profileRef = strings.TrimSpace(state.ProfileRef)
	}
	if entry.workspaceRef == "" {
		entry.workspaceRef = strings.TrimSpace(state.WorkspaceRef)
	}
	prepared.Cwd = m.resolveOpenCwd(ctx, req.SessionID, entry.compute, req.Cwd, state.Cwd)
	return prepared, state.EnvSnapshot, true, state.Revision
}

func (m *Manager) prepareEnvironment(ctx context.Context, req ExecCommandRequest, entry *managedSession) error {
	if entry.compute == nil {
		return nil
	}
	if m.envManager != nil {
		if err := m.envManager.Prepare(ctx, req.SessionID, entry.compute, environment.Binding{
			AccountID:        req.AccountID,
			ProfileRef:   req.ProfileRef,
			WorkspaceRef: req.WorkspaceRef,
		}); err != nil {
			return fmt.Errorf("prepare environment: %w", err)
		}
	}
	if m.skillOverlay != nil {
		if err := m.skillOverlay.Apply(ctx, entry.compute, req.EnabledSkills); err != nil {
			return fmt.Errorf("apply skill overlay: %w", err)
		}
	}
	return nil
}

func (m *Manager) saveRestoreStateLocked(ctx context.Context, sessionID string, entry *managedSession) error {
	if entry.compute == nil || m.stateStore == nil {
		return nil
	}
	state, err := m.invokeState(ctx, entry, "shell_capture_state", AgentStateRequest{})
	if err != nil {
		return fmt.Errorf("capture restore state: %w", err)
	}
	now := time.Now().UTC()
	restoreState := SessionRestoreState{
		Version:        shellStateVersion,
		Revision:       nextRestoreRevision(now),
		AccountID:          entry.accountID,
		SessionID:      sessionID,
		ProfileRef:     entry.profileRef,
		WorkspaceRef:   entry.workspaceRef,
		Cwd:            strings.TrimSpace(state.Cwd),
		EnvSnapshot:    state.Env,
		LastCommandSeq: entry.commandSeq,
		UploadedSeq:    entry.uploadedSeq,
		ArtifactSeen:   cloneArtifactSeen(entry.artifactSeen),
		CreatedAt:      now.Format(time.RFC3339Nano),
		ExpiresAt:      restoreExpiryString(now, m.config.RestoreTTL),
	}
	if restoreState.Cwd == "" {
		restoreState.Cwd = defaultRestoreCwd
	}
	if err := saveRestoreState(ctx, m.stateStore, m.restoreRegistry, restoreState); err != nil {
		return err
	}
	m.logger.Info("session_restore_save", logging.LogFields{SessionID: &sessionID}, map[string]any{"flush_result": "succeeded", "revision": restoreState.Revision, "restore_state_duration_ms": time.Since(now).Milliseconds()})
	return nil
}

func (m *Manager) beforeComputeDelete(ctx context.Context, sn *session.Session, reason session.DeleteReason) error {
	if sn == nil {
		return nil
	}
	var envErr error
	if m.envManager != nil {
		envErr = m.envManager.FlushNow(ctx, sn.ID)
		m.envManager.Drop(sn.ID)
	}
	m.mu.Lock()
	entry := m.sessions[sn.ID]
	m.mu.Unlock()
	if entry == nil {
		return envErr
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.compute == nil {
		return envErr
	}
	err := m.saveRestoreStateLocked(ctx, sn.ID, entry)
	if err == nil {
		entry.compute = nil
		return envErr
	}
	m.logger.Warn("session_restore_save", logging.LogFields{SessionID: &sn.ID}, map[string]any{"flush_result": "failed", "error": err.Error(), "reason": string(reason)})
	return errors.Join(envErr, err)
}

func (m *Manager) resolveOpenCwd(ctx context.Context, sessionID string, sn *session.Session, requested, restored string) string {
	candidate := strings.TrimSpace(requested)
	if candidate == "" {
		candidate = strings.TrimSpace(restored)
	}
	if candidate == "" {
		return defaultRestoreCwd
	}
	if sn == nil {
		return candidate
	}
	result, err := sn.Exec(ctx, session.ExecJob{Language: "shell", Code: "[ -d " + shellQuote(candidate) + " ]", TimeoutMs: 5000})
	if err == nil && result != nil && result.ExitCode == 0 {
		return candidate
	}
	m.logger.Warn("shell cwd fallback", logging.LogFields{SessionID: &sessionID}, map[string]any{"cwd": candidate})
	return defaultRestoreCwd
}

func cloneArtifactSeen(source map[string]artifactVersion) map[string]artifactVersion {
	if len(source) == 0 {
		return make(map[string]artifactVersion)
	}
	clone := make(map[string]artifactVersion, len(source))
	for key, value := range source {
		clone[key] = value
	}
	normalizeArtifactVersions(clone)
	return clone
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func (m *Manager) invokeExecCommand(ctx context.Context, entry *managedSession, req ExecCommandRequest, shellEnv map[string]string) (*AgentSessionResponse, error) {
	merged := shellEnv
	if len(req.Env) > 0 {
		if merged == nil {
			merged = make(map[string]string, len(req.Env))
		}
		for k, v := range req.Env {
			merged[k] = v
		}
	}
	payload := AgentRequest{
		Action: "exec_command",
		ExecCommand: &AgentExecCommandRequest{
			Cwd:         req.Cwd,
			Command:     req.Command,
			TimeoutMs:   req.TimeoutMs,
			YieldTimeMs: req.YieldTimeMs,
			Background:  req.Background,
			Env:         merged,
		},
	}
	return m.invokeSession(ctx, entry, payload, req.TimeoutMs, req.YieldTimeMs)
}

func (m *Manager) invokeWriteStdin(ctx context.Context, entry *managedSession, req WriteStdinRequest) (*AgentSessionResponse, error) {
	payload := AgentRequest{
		Action: "write_stdin",
		WriteStdin: &AgentWriteStdinRequest{
			Chars:       req.Chars,
			YieldTimeMs: req.YieldTimeMs,
		},
	}
	return m.invokeSession(ctx, entry, payload, 0, req.YieldTimeMs)
}

func (m *Manager) invokeDebugSnapshot(ctx context.Context, entry *managedSession) (*AgentDebugResponse, error) {
	if entry.compute == nil {
		return nil, notFoundError()
	}
	entry.compute.TouchActivity()
	callTimeout := 10 * time.Second
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	conn, err := entry.compute.Dial(ctx)
	if err != nil {
		return nil, &transportError{err: fmt.Errorf("connect to agent: %w", err)}
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(callTimeout))

	req := AgentRequest{Action: "shell_debug_snapshot"}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, &transportError{err: fmt.Errorf("send debug request: %w", err)}
	}
	respBody, err := io.ReadAll(conn)
	if err != nil {
		return nil, &transportError{err: fmt.Errorf("read debug response: %w", err)}
	}
	var resp AgentResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, &transportError{err: fmt.Errorf("decode debug response: %w", err)}
	}
	if resp.Error != "" {
		switch resp.Code {
		case CodeSessionNotFound:
			return nil, notFoundError()
		default:
			return nil, errors.New(resp.Error)
		}
	}
	if resp.Debug == nil {
		return nil, &transportError{err: fmt.Errorf("debug response missing body")}
	}
	return resp.Debug, nil
}

func (m *Manager) invokeReadOutputDeltas(ctx context.Context, entry *managedSession) (*OutputDeltasResponse, error) {
	if entry.compute == nil {
		return nil, notFoundError()
	}
	entry.compute.TouchActivity()
	callTimeout := 10 * time.Second
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	conn, err := entry.compute.Dial(ctx)
	if err != nil {
		return nil, &transportError{err: fmt.Errorf("connect to agent: %w", err)}
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(callTimeout))

	req := AgentRequest{Action: "exec_read_output"}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, &transportError{err: fmt.Errorf("send read output request: %w", err)}
	}
	respBody, err := io.ReadAll(conn)
	if err != nil {
		return nil, &transportError{err: fmt.Errorf("read output response: %w", err)}
	}
	var resp AgentResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, &transportError{err: fmt.Errorf("decode output response: %w", err)}
	}
	if resp.Error != "" {
		switch resp.Code {
		case CodeSessionNotFound:
			return nil, notFoundError()
		default:
			return nil, errors.New(resp.Error)
		}
	}
	if resp.ExecOutput == nil {
		return nil, &transportError{err: fmt.Errorf("exec_output response missing body")}
	}
	return &OutputDeltasResponse{
		Stdout:  resp.ExecOutput.Stdout,
		Stderr:  resp.ExecOutput.Stderr,
		Running: resp.ExecOutput.Running,
	}, nil
}

func (m *Manager) invokeSession(ctx context.Context, entry *managedSession, payload AgentRequest, timeoutMs, yieldTimeMs int) (*AgentSessionResponse, error) {
	if entry.compute == nil {
		return nil, notFoundError()
	}
	entry.compute.TouchActivity()
	callTimeout := time.Duration(maxInt(timeoutMs, yieldTimeMs, 5000)+5000) * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	conn, err := entry.compute.Dial(ctx)
	if err != nil {
		return nil, &transportError{err: fmt.Errorf("connect to agent: %w", err)}
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(callTimeout))

	if err := json.NewEncoder(conn).Encode(payload); err != nil {
		return nil, &transportError{err: fmt.Errorf("send shell request: %w", err)}
	}

	respBody, err := io.ReadAll(conn)
	if err != nil {
		return nil, &transportError{err: fmt.Errorf("read shell response: %w", err)}
	}

	var resp AgentResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, &transportError{err: fmt.Errorf("decode shell response: %w", err)}
	}
	if resp.Error != "" {
		switch resp.Code {
		case CodeSessionBusy:
			return nil, busyError()
		case CodeSessionNotFound:
			return nil, notFoundError()
		case CodeNotRunning:
			return nil, notRunningError()
		default:
			return nil, errors.New(resp.Error)
		}
	}
	if resp.Session == nil {
		return nil, &transportError{err: fmt.Errorf("shell response missing body")}
	}
	return resp.Session, nil
}

func (m *Manager) invokeState(ctx context.Context, entry *managedSession, action string, stateReq AgentStateRequest) (*AgentStateResponse, error) {
	if entry.compute == nil {
		return nil, notFoundError()
	}
	entry.compute.TouchActivity()
	callTimeout := 2 * time.Minute
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	conn, err := entry.compute.Dial(ctx)
	if err != nil {
		return nil, &transportError{err: fmt.Errorf("connect to agent: %w", err)}
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(callTimeout))

	req := AgentRequest{Action: action, State: &stateReq}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, &transportError{err: fmt.Errorf("send shell state request: %w", err)}
	}
	respBody, err := io.ReadAll(conn)
	if err != nil {
		return nil, &transportError{err: fmt.Errorf("read shell state response: %w", err)}
	}
	var resp AgentResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, &transportError{err: fmt.Errorf("decode shell state response: %w", err)}
	}
	if resp.Error != "" {
		switch resp.Code {
		case CodeSessionBusy:
			return nil, busyError()
		case CodeSessionNotFound:
			return nil, notFoundError()
		default:
			return nil, errors.New(resp.Error)
		}
	}
	if resp.State == nil {
		return nil, &transportError{err: fmt.Errorf("shell state response missing body")}
	}
	return resp.State, nil
}

func (m *Manager) attachArtifacts(ctx context.Context, sessionID string, entry *managedSession, result *AgentSessionResponse, resp *Response) {
	if result == nil || result.Running || result.ExitCode == nil {
		return
	}
	if entry.commandSeq == 0 || entry.uploadedSeq >= entry.commandSeq {
		return
	}
	upload := collectArtifacts(ctx, entry.compute, sessionID, entry.commandSeq, m.artifactStore, entry.artifactSeen, m.logger)
	entry.artifactSeen = upload.NextKnown
	if upload.CanAdvanceSeq {
		entry.uploadedSeq = entry.commandSeq
	}
	resp.Artifacts = upload.Refs
	if resp.Artifacts == nil {
		resp.Artifacts = []ArtifactRef{}
	}
}

func (m *Manager) toResponse(sessionID string, result *AgentSessionResponse) *Response {
	resp := &Response{
		SessionID: sessionID,
		Status:    result.Status,
		Cwd:       result.Cwd,
		Output:    result.Output,
		Running:   result.Running,
		Truncated: result.Truncated,
		TimedOut:  result.TimedOut,
		ExitCode:  result.ExitCode,
	}
	if result.Running {
		resp.Status = StatusRunning
	}
	if !result.Running && resp.Status == "" {
		resp.Status = StatusIdle
	}
	return resp
}

func (m *Manager) toDebugResponse(sessionID string, result *AgentDebugResponse) *DebugResponse {
	if result == nil {
		return &DebugResponse{SessionID: sessionID}
	}
	resp := &DebugResponse{
		SessionID:              sessionID,
		Status:                 result.Status,
		Cwd:                    result.Cwd,
		Running:                result.Running,
		TimedOut:               result.TimedOut,
		ExitCode:               result.ExitCode,
		PendingOutputBytes:     result.PendingOutputBytes,
		PendingOutputTruncated: result.PendingOutputTruncated,
		Transcript:             result.Transcript,
		Tail:                   result.Tail,
	}
	if resp.Status == "" {
		resp.Status = StatusIdle
	}
	return resp
}

func maxInt(values ...int) int {
	max := 0
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}
