package process

import (
	"context"
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
	sandboxskills "arkloop/services/sandbox/internal/skills"
	"arkloop/services/shared/skillstore"
)

const (
	defaultTier        = "lite"
	callTimeoutPadding = 5 * time.Second
)

type Service interface {
	ExecCommand(ctx context.Context, req ExecCommandRequest) (*Response, error)
	ContinueProcess(ctx context.Context, req ContinueProcessRequest) (*Response, error)
	TerminateProcess(ctx context.Context, req TerminateProcessRequest) (*Response, error)
	ResizeProcess(ctx context.Context, req ResizeProcessRequest) (*Response, error)
	CloseSession(ctx context.Context, sessionID, accountID string) error
}

type Manager struct {
	compute       *session.Manager
	artifactStore artifactStore
	envManager    *environment.Manager
	skillOverlay  *sandboxskills.OverlayManager
	logger        *logging.JSONLogger

	mu       sync.Mutex
	sessions map[string]*managedSession
}

type managedSession struct {
	mu sync.Mutex

	compute      *session.Session
	accountID    string
	profileRef   string
	workspaceRef string
	commandSeq   int64
	artifactSeen map[string]artifactVersion
	processSeq   map[string]int64
}

func NewManager(
	compute *session.Manager,
	artifactStore artifactStore,
	envManager *environment.Manager,
	skillOverlay *sandboxskills.OverlayManager,
	logger *logging.JSONLogger,
) *Manager {
	mgr := &Manager{
		compute:       compute,
		artifactStore: artifactStore,
		envManager:    envManager,
		skillOverlay:  skillOverlay,
		logger:        logger,
		sessions:      map[string]*managedSession{},
	}
	if compute != nil {
		compute.AddBeforeDelete(mgr.beforeComputeDelete)
	}
	return mgr
}

func (m *Manager) ExecCommand(ctx context.Context, req ExecCommandRequest) (*Response, error) {
	if err := ValidateExecRequest(req); err != nil {
		return nil, err
	}
	entry, err := m.getOrCreateEntry(ctx, req.SessionID, req.AccountID, req.Tier)
	if err != nil {
		return nil, err
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if err := ensureAccount(entry.accountID, req.AccountID); err != nil {
		return nil, err
	}
	if req.ProfileRef != "" {
		entry.profileRef = strings.TrimSpace(req.ProfileRef)
	}
	if req.WorkspaceRef != "" {
		entry.workspaceRef = strings.TrimSpace(req.WorkspaceRef)
	}
	if err := m.prepareEnvironment(ctx, entry, req.AccountID, entry.profileRef, entry.workspaceRef, req.EnabledSkills); err != nil {
		return nil, err
	}

	entry.commandSeq++
	commandSeq := entry.commandSeq
	resp, err := m.invoke(ctx, entry, AgentRequest{
		Action: "process_exec",
		ExecCommand: &AgentExecRequest{
			Command:   req.Command,
			Mode:      NormalizeMode(req.Mode),
			Cwd:       req.Cwd,
			TimeoutMs: NormalizeTimeoutMs(req.Mode, req.TimeoutMs),
			Size:      req.Size,
			Env:       req.Env,
		},
	}, NormalizeTimeoutMs(req.Mode, req.TimeoutMs))
	if err != nil {
		return nil, err
	}
	if ref := strings.TrimSpace(resp.ProcessRef); ref != "" {
		entry.processSeq[ref] = commandSeq
	}
	m.attachArtifacts(ctx, req.SessionID, entry, commandSeq, resp)
	if resp.Status != StatusRunning && m.envManager != nil {
		m.envManager.MarkAllDirty(req.SessionID)
	}
	return resp, nil
}

func (m *Manager) ContinueProcess(ctx context.Context, req ContinueProcessRequest) (*Response, error) {
	if err := ValidateContinueRequest(req); err != nil {
		return nil, err
	}
	entry, err := m.getExistingEntry(req.SessionID, req.AccountID)
	if err != nil {
		return nil, err
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if err := ensureAccount(entry.accountID, req.AccountID); err != nil {
		return nil, err
	}
	timeoutMs := defaultFollowLimit
	if seq := entry.processSeq[strings.TrimSpace(req.ProcessRef)]; seq == 0 {
		entry.processSeq[strings.TrimSpace(req.ProcessRef)] = entry.commandSeq
	}
	resp, err := m.invoke(ctx, entry, AgentRequest{
		Action:          "process_continue",
		ContinueProcess: &req,
	}, timeoutMs)
	if err != nil {
		return nil, err
	}
	m.attachArtifacts(ctx, req.SessionID, entry, entry.processSeq[strings.TrimSpace(req.ProcessRef)], resp)
	if resp.Status != StatusRunning && m.envManager != nil {
		m.envManager.MarkAllDirty(req.SessionID)
	}
	return resp, nil
}

func (m *Manager) TerminateProcess(ctx context.Context, req TerminateProcessRequest) (*Response, error) {
	entry, err := m.getExistingEntry(req.SessionID, req.AccountID)
	if err != nil {
		return nil, err
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if err := ensureAccount(entry.accountID, req.AccountID); err != nil {
		return nil, err
	}
	resp, err := m.invoke(ctx, entry, AgentRequest{
		Action: "process_terminate",
		TerminateProcess: &AgentRefRequest{
			ProcessRef: req.ProcessRef,
		},
	}, defaultFollowLimit)
	if err != nil {
		return nil, err
	}
	m.attachArtifacts(ctx, req.SessionID, entry, entry.processSeq[strings.TrimSpace(req.ProcessRef)], resp)
	if m.envManager != nil {
		m.envManager.MarkAllDirty(req.SessionID)
	}
	return resp, nil
}

func (m *Manager) ResizeProcess(ctx context.Context, req ResizeProcessRequest) (*Response, error) {
	if err := ValidateResizeRequest(req); err != nil {
		return nil, err
	}
	entry, err := m.getExistingEntry(req.SessionID, req.AccountID)
	if err != nil {
		return nil, err
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if err := ensureAccount(entry.accountID, req.AccountID); err != nil {
		return nil, err
	}
	return m.invoke(ctx, entry, AgentRequest{
		Action: "process_resize",
		ResizeProcess: &AgentResizeRequest{
			ProcessRef: req.ProcessRef,
			Rows:       req.Rows,
			Cols:       req.Cols,
		},
	}, defaultFollowLimit)
}

func (m *Manager) CloseSession(ctx context.Context, sessionID, accountID string) error {
	entry, err := m.getExistingEntry(sessionID, accountID)
	if err != nil {
		return err
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if err := ensureAccount(entry.accountID, accountID); err != nil {
		return err
	}
	for processRef := range entry.processSeq {
		if strings.TrimSpace(processRef) == "" {
			continue
		}
		if _, err := m.invoke(ctx, entry, AgentRequest{
			Action: "process_terminate",
			TerminateProcess: &AgentRefRequest{
				ProcessRef: processRef,
				Status:     StatusCancelled,
			},
		}, defaultFollowLimit); err != nil {
			var procErr *Error
			if errors.As(err, &procErr) && (procErr.Code == CodeProcessNotFound || procErr.Code == CodeNotRunning) {
				continue
			}
			if strings.Contains(err.Error(), "process not found") {
				continue
			}
			return err
		}
	}
	if m.envManager != nil {
		if err := m.envManager.FlushNow(ctx, sessionID); err != nil {
			return err
		}
		m.envManager.Drop(sessionID)
	}
	for processRef := range entry.processSeq {
		if _, err := m.invoke(ctx, entry, AgentRequest{
			Action: "process_cancel",
			CancelProcess: &AgentRefRequest{
				ProcessRef: processRef,
			},
		}, defaultFollowLimit); err != nil {
			if procErr, ok := err.(*Error); ok && procErr.Code == CodeProcessNotFound {
				continue
			}
			return err
		}
	}
	m.dropEntry(sessionID, entry)
	return nil
}

func (m *Manager) prepareEnvironment(
	ctx context.Context,
	entry *managedSession,
	accountID string,
	profileRef string,
	workspaceRef string,
	skills []skillstore.ResolvedSkill,
) error {
	if entry.compute == nil {
		return processNotFoundError()
	}
	if m.envManager != nil {
		if err := m.envManager.Prepare(ctx, entry.compute.ID, entry.compute, environment.Binding{
			AccountID:    accountID,
			ProfileRef:   profileRef,
			WorkspaceRef: workspaceRef,
		}); err != nil {
			return fmt.Errorf("prepare environment: %w", err)
		}
	}
	if m.skillOverlay != nil {
		if err := m.skillOverlay.Apply(ctx, entry.compute, skills); err != nil {
			return fmt.Errorf("apply skill overlay: %w", err)
		}
	}
	return nil
}

func (m *Manager) invoke(ctx context.Context, entry *managedSession, req AgentRequest, timeoutMs int) (*Response, error) {
	if entry.compute == nil {
		return nil, processNotFoundError()
	}
	entry.compute.TouchActivity()
	callTimeout := time.Duration(timeoutMs)*time.Millisecond + callTimeoutPadding
	if callTimeout <= 0 {
		callTimeout = time.Duration(defaultFollowLimit)*time.Millisecond + callTimeoutPadding
	}
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	conn, err := entry.compute.Dial(callCtx)
	if err != nil {
		return nil, fmt.Errorf("connect to agent: %w", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(callTimeout))

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("send process request: %w", err)
	}
	body, err := io.ReadAll(conn)
	if err != nil {
		return nil, fmt.Errorf("read process response: %w", err)
	}
	var resp AgentResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode process response: %w", err)
	}
	if resp.Error != "" {
		return nil, mapAgentError(resp.Code, resp.Error)
	}
	if resp.Process == nil {
		return nil, fmt.Errorf("process response missing body")
	}
	return resp.Process, nil
}

func (m *Manager) getOrCreateEntry(ctx context.Context, sessionID, accountID, tier string) (*managedSession, error) {
	entry, created := m.getOrCreateEntryLocal(sessionID, accountID)
	if entry.compute != nil {
		return entry, nil
	}
	computeSession, err := m.compute.GetOrCreate(ctx, sessionID, normalizeTier(tier), accountID)
	if err != nil {
		if strings.Contains(err.Error(), "account mismatch") {
			return nil, accountMismatchError()
		}
		if strings.Contains(err.Error(), "max sessions reached:") {
			return nil, maxSessionsExceededError(m.compute.MaxSessions())
		}
		return nil, err
	}
	entry.mu.Lock()
	entry.compute = computeSession
	entry.accountID = computeSession.AccountID
	entry.mu.Unlock()
	if created {
		return entry, nil
	}
	return entry, nil
}

func normalizeTier(value string) string {
	if strings.TrimSpace(value) == "" {
		return defaultTier
	}
	return strings.TrimSpace(value)
}

func (m *Manager) getOrCreateEntryLocal(sessionID, accountID string) (*managedSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry, ok := m.sessions[sessionID]; ok {
		return entry, false
	}
	entry := &managedSession{
		accountID:    accountID,
		artifactSeen: map[string]artifactVersion{},
		processSeq:   map[string]int64{},
	}
	m.sessions[sessionID] = entry
	return entry, true
}

func (m *Manager) getExistingEntry(sessionID, accountID string) (*managedSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.sessions[sessionID]
	if !ok {
		return nil, processNotFoundError()
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
	}
}

func ensureAccount(boundAccountID, accountID string) error {
	if accountID != "" && boundAccountID != "" && accountID != boundAccountID {
		return accountMismatchError()
	}
	return nil
}

func (m *Manager) beforeComputeDelete(ctx context.Context, sn *session.Session, _ session.DeleteReason) error {
	if sn == nil {
		return nil
	}
	if m.envManager != nil {
		_ = m.envManager.FlushNow(ctx, sn.ID)
		m.envManager.Drop(sn.ID)
	}
	m.mu.Lock()
	delete(m.sessions, sn.ID)
	m.mu.Unlock()
	return nil
}

func (m *Manager) attachArtifacts(ctx context.Context, sessionID string, entry *managedSession, commandSeq int64, resp *Response) {
	if resp == nil || entry == nil || entry.compute == nil || commandSeq <= 0 || m.artifactStore == nil {
		return
	}
	upload := collectArtifacts(ctx, entry.compute, sessionID, commandSeq, m.artifactStore, entry.artifactSeen, m.logger)
	entry.artifactSeen = upload.NextKnown
	if len(upload.Refs) > 0 {
		resp.Artifacts = upload.Refs
	}
}

func mapAgentError(code, message string) error {
	switch code {
	case CodeProcessBusy:
		return busyError()
	case CodeCursorExpired:
		return cursorExpiredError()
	case CodeInvalidCursor:
		return invalidCursorError()
	case CodeInputSeqRequired:
		return inputSeqRequiredError()
	case CodeInputSeqInvalid:
		return inputSeqInvalidError()
	case CodeStdinNotSupported:
		return stdinNotSupportedError()
	case CodeResizeNotSupported:
		return resizeNotSupportedError()
	case CodeInvalidMode:
		return invalidModeError(message)
	case CodeInvalidSize:
		return invalidSizeError()
	case CodeTimeoutRequired:
		return timeoutRequiredError()
	case CodeTimeoutTooLarge:
		return timeoutTooLargeError()
	case CodeAccountMismatch:
		return accountMismatchError()
	case CodeProcessNotFound:
		return processNotFoundError()
	default:
		return fmt.Errorf("%s", message)
	}
}
