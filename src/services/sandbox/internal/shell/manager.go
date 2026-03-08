package shell

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"arkloop/services/sandbox/internal/environment"
	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
	"arkloop/services/shared/objectstore"
)

type Service interface {
	ExecCommand(ctx context.Context, req ExecCommandRequest) (*Response, error)
	WriteStdin(ctx context.Context, req WriteStdinRequest) (*Response, error)
	DebugSnapshot(ctx context.Context, sessionID, orgID string) (*DebugResponse, error)
	Close(ctx context.Context, sessionID, orgID string) error
}

type Manager struct {
	compute       *session.Manager
	artifactStore artifactStore
	stateStore    stateStore
	envManager    *environment.Manager
	logger        *logging.JSONLogger

	mu       sync.Mutex
	sessions map[string]*managedSession
}

type managedSession struct {
	mu sync.Mutex

	compute      *session.Session
	orgID        string
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

func NewManager(compute *session.Manager, artifactStore artifactStore, stateStore stateStore, envManager *environment.Manager, logger *logging.JSONLogger) *Manager {
	mgr := &Manager{
		compute:       compute,
		artifactStore: artifactStore,
		stateStore:    stateStore,
		envManager:    envManager,
		logger:        logger,
		sessions:      make(map[string]*managedSession),
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
	entry, created := m.getOrCreateEntry(req.SessionID, req.OrgID)
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if err := ensureOrg(entry.orgID, req.OrgID); err != nil {
		return nil, err
	}

	shellEnv := map[string]string(nil)
	prepared := req
	createdCompute := false
	if entry.compute == nil {
		computeSession, err := m.compute.GetOrCreate(ctx, req.SessionID, req.Tier, req.OrgID)
		if err != nil {
			m.dropEntry(req.SessionID, entry)
			if strings.Contains(err.Error(), "org mismatch") {
				return nil, orgMismatchError()
			}
			return nil, fmt.Errorf("get shell compute session: %w", err)
		}
		entry.compute = computeSession
		entry.orgID = computeSession.OrgID
		if entry.artifactSeen == nil {
			entry.artifactSeen = make(map[string]artifactVersion)
		}
		if err := m.prepareEnvironment(ctx, req, entry); err != nil {
			return nil, err
		}
		prepared, shellEnv = m.prepareExecCommandRequest(ctx, req, entry)
		createdCompute = true
	} else if err := m.prepareEnvironment(ctx, req, entry); err != nil {
		return nil, err
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
				_ = m.compute.DeleteSkipHook(ctx, req.SessionID, req.OrgID)
			}
			if !created {
				return nil, notFoundError()
			}
			return nil, notFoundError()
		}
		return nil, err
	}

	resp := m.toResponse(req.SessionID, result)
	m.attachArtifacts(ctx, req.SessionID, entry, result, resp)
	if result != nil && !result.Running && m.envManager != nil {
		m.envManager.MarkDirty(req.SessionID)
	}
	return resp, nil
}

func (m *Manager) WriteStdin(ctx context.Context, req WriteStdinRequest) (*Response, error) {
	entry, err := m.getExistingEntry(req.SessionID, req.OrgID)
	if err != nil {
		return nil, err
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if err := ensureOrg(entry.orgID, req.OrgID); err != nil {
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
		m.envManager.MarkDirty(req.SessionID)
	}
	return resp, nil
}

func (m *Manager) DebugSnapshot(ctx context.Context, sessionID, orgID string) (*DebugResponse, error) {
	entry, err := m.getExistingEntry(sessionID, orgID)
	if err != nil {
		return nil, err
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if err := ensureOrg(entry.orgID, orgID); err != nil {
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

func (m *Manager) Close(ctx context.Context, sessionID, orgID string) error {
	entry, err := m.getExistingEntry(sessionID, orgID)
	if err != nil {
		return err
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if err := ensureOrg(entry.orgID, orgID); err != nil {
		return err
	}
	if entry.compute == nil {
		return notFoundError()
	}
	if err := m.checkpointLocked(ctx, sessionID, entry); err != nil {
		return err
	}
	if m.envManager != nil {
		if err := m.envManager.FlushNow(ctx, sessionID); err != nil {
			return err
		}
	}
	deleteErr := m.compute.DeleteSkipHook(ctx, sessionID, orgID)
	if deleteErr != nil && strings.Contains(deleteErr.Error(), "org mismatch") {
		return orgMismatchError()
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

func ensureOrg(boundOrgID, orgID string) error {
	if orgID != "" && boundOrgID != "" && orgID != boundOrgID {
		return orgMismatchError()
	}
	return nil
}

func (m *Manager) getOrCreateEntry(sessionID, orgID string) (*managedSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry, ok := m.sessions[sessionID]; ok {
		return entry, false
	}
	entry := &managedSession{orgID: orgID, artifactSeen: make(map[string]artifactVersion)}
	m.sessions[sessionID] = entry
	return entry, true
}

func (m *Manager) getExistingEntry(sessionID, orgID string) (*managedSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.sessions[sessionID]
	if !ok {
		return nil, notFoundError()
	}
	if err := ensureOrg(entry.orgID, orgID); err != nil {
		return nil, err
	}
	return entry, nil
}

func (m *Manager) dropEntry(sessionID string, entry *managedSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current, ok := m.sessions[sessionID]; ok && current == entry {
		delete(m.sessions, sessionID)
	}
}

func (m *Manager) prepareExecCommandRequest(ctx context.Context, req ExecCommandRequest, entry *managedSession) (ExecCommandRequest, map[string]string) {
	prepared := req
	if entry.compute == nil || m.stateStore == nil {
		return prepared, nil
	}
	manifest, archive, err := loadLatestCheckpoint(ctx, m.stateStore, entry.orgID, req.SessionID)
	if err != nil {
		if objectstore.IsNotFound(err) {
			return prepared, nil
		}
		m.logger.Warn("shell checkpoint load failed", logging.LogFields{SessionID: &req.SessionID}, map[string]any{"error": err.Error()})
		return prepared, nil
	}
	if _, err := m.invokeCheckpoint(ctx, entry, "shell_restore_import", AgentCheckpointRequest{Archive: base64.StdEncoding.EncodeToString(archive)}); err != nil {
		m.logger.Warn("shell checkpoint restore failed", logging.LogFields{SessionID: &req.SessionID}, map[string]any{"error": err.Error(), "revision": manifest.Revision})
		return prepared, nil
	}
	entry.commandSeq = manifest.LastCommandSeq
	entry.uploadedSeq = manifest.UploadedSeq
	entry.artifactSeen = cloneArtifactSeen(manifest.ArtifactSeen)
	prepared.Cwd = m.resolveOpenCwd(ctx, req.SessionID, entry.compute, req.Cwd, manifest.Cwd)
	return prepared, manifest.EnvSnapshot
}

func (m *Manager) prepareEnvironment(ctx context.Context, req ExecCommandRequest, entry *managedSession) error {
	if m.envManager == nil || entry.compute == nil {
		return nil
	}
	if err := m.envManager.Prepare(ctx, req.SessionID, entry.compute, environment.Binding{
		OrgID:        req.OrgID,
		ProfileRef:   req.ProfileRef,
		WorkspaceRef: req.WorkspaceRef,
	}); err != nil {
		return fmt.Errorf("prepare environment: %w", err)
	}
	return nil
}

func (m *Manager) checkpointLocked(ctx context.Context, sessionID string, entry *managedSession) error {
	if entry.compute == nil || m.stateStore == nil {
		return nil
	}
	checkpoint, err := m.invokeCheckpoint(ctx, entry, "shell_checkpoint_export", AgentCheckpointRequest{})
	if err != nil {
		return fmt.Errorf("checkpoint export: %w", err)
	}
	archive, err := base64.StdEncoding.DecodeString(checkpoint.Archive)
	if err != nil {
		return fmt.Errorf("decode checkpoint archive: %w", err)
	}
	now := time.Now().UTC()
	manifest := checkpointManifest{
		Version:        shellStateVersion,
		Revision:       nextCheckpointRevision(now),
		OrgID:          entry.orgID,
		SessionID:      sessionID,
		Cwd:            strings.TrimSpace(checkpoint.Cwd),
		EnvSnapshot:    checkpoint.Env,
		LastCommandSeq: entry.commandSeq,
		UploadedSeq:    entry.uploadedSeq,
		ArtifactSeen:   cloneArtifactSeen(entry.artifactSeen),
		CreatedAt:      now.Format(time.RFC3339Nano),
	}
	if manifest.Cwd == "" {
		manifest.Cwd = defaultRestoreCwd
	}
	if err := saveCheckpoint(ctx, m.stateStore, manifest, archive); err != nil {
		return err
	}
	m.logger.Info("shell checkpoint stored", logging.LogFields{SessionID: &sessionID}, map[string]any{"revision": manifest.Revision})
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
	err := m.checkpointLocked(ctx, sn.ID, entry)
	if err == nil {
		entry.compute = nil
		return envErr
	}
	m.logger.Warn("shell checkpoint before delete failed", logging.LogFields{SessionID: &sn.ID}, map[string]any{"error": err.Error(), "reason": string(reason)})
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
	payload := AgentRequest{
		Action: "exec_command",
		ExecCommand: &AgentExecCommandRequest{
			Cwd:         req.Cwd,
			Command:     req.Command,
			TimeoutMs:   req.TimeoutMs,
			YieldTimeMs: req.YieldTimeMs,
			Env:         shellEnv,
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
	defer conn.Close()
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
	defer conn.Close()
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

func (m *Manager) invokeCheckpoint(ctx context.Context, entry *managedSession, action string, checkpointReq AgentCheckpointRequest) (*AgentCheckpointResponse, error) {
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
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(callTimeout))

	req := AgentRequest{Action: action, Checkpoint: &checkpointReq}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, &transportError{err: fmt.Errorf("send checkpoint request: %w", err)}
	}
	respBody, err := io.ReadAll(conn)
	if err != nil {
		return nil, &transportError{err: fmt.Errorf("read checkpoint response: %w", err)}
	}
	var resp AgentResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, &transportError{err: fmt.Errorf("decode checkpoint response: %w", err)}
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
	if resp.Checkpoint == nil {
		return nil, &transportError{err: fmt.Errorf("checkpoint response missing body")}
	}
	return resp.Checkpoint, nil
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
