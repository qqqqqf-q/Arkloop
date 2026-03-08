package sandbox

import (
	"context"
	"strings"
	"sync"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/tools"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	sessionModeAuto   = "auto"
	sessionModeNew    = "new"
	sessionModeResume = "resume"
	sessionModeFork   = "fork"
)

type resolvedSession struct {
	SessionRef               string
	ResolvedVia              string
	Reused                   bool
	RestoredFromRestoreState bool
	FromSessionRef           string
	PersistBinding           bool
	ShareScope               string
	Record                   *data.ShellSessionRecord
}

type sessionOrchestrator struct {
	pool            *pgxpool.Pool
	sessionsRepo    data.ShellSessionsRepository
	bindingsRepo    data.DefaultShellSessionBindingsRepository
	registryService *registryService

	mu             sync.Mutex
	runDefaults    map[string]string
	memorySessions map[string]data.ShellSessionRecord
	memoryBindings map[string]string
}

func newSessionOrchestrator(pool *pgxpool.Pool) *sessionOrchestrator {
	return &sessionOrchestrator{
		pool:            pool,
		registryService: newRegistryService(pool),
		runDefaults:     map[string]string{},
		memorySessions:  map[string]data.ShellSessionRecord{},
		memoryBindings:  map[string]string{},
	}
}

func (o *sessionOrchestrator) resolveExecSession(
	ctx context.Context,
	req execCommandArgs,
	execCtx tools.ExecutionContext,
) (*resolvedSession, *tools.ExecutionError) {
	mode := normalizeSessionMode(req.SessionMode)
	if mode == sessionModeResume && strings.TrimSpace(req.SessionRef) == "" {
		return nil, sandboxArgsError("parameter session_ref is required when session_mode=resume")
	}
	if mode == sessionModeFork && strings.TrimSpace(req.FromSessionRef) == "" {
		return nil, sandboxArgsError("parameter from_session_ref is required when session_mode=fork")
	}

	if strings.TrimSpace(req.SessionRef) != "" && mode == sessionModeAuto {
		return o.lookupExplicit(ctx, execCtx, strings.TrimSpace(req.SessionRef), "explicit_resume")
	}

	switch mode {
	case sessionModeResume:
		return o.lookupExplicit(ctx, execCtx, strings.TrimSpace(req.SessionRef), "explicit_resume")
	case sessionModeFork:
		base, err := o.lookupExplicit(ctx, execCtx, strings.TrimSpace(req.FromSessionRef), "fork_from_checkpoint")
		if err != nil {
			return nil, err
		}
		created, createErr := o.createSession(ctx, execCtx, data.ShellShareScopeRun, false)
		if createErr != nil {
			return nil, createErr
		}
		created.ResolvedVia = "fork_from_checkpoint"
		created.FromSessionRef = base.SessionRef
		created.RestoredFromRestoreState = true
		return created, nil
	case sessionModeNew:
		created, err := o.createSession(ctx, execCtx, data.ShellShareScopeRun, false)
		if err != nil {
			return nil, err
		}
		created.ResolvedVia = "new_session"
		return created, nil
	default:
		if ref := o.getRunDefault(execCtx.RunID); ref != "" {
			resolved, err := o.lookupExplicit(ctx, execCtx, ref, "run_default")
			if err == nil {
				resolved.Reused = true
				return resolved, nil
			}
		}
		if ref := o.lookupPersistentDefault(ctx, execCtx, data.ShellBindingScopeThread); ref != "" {
			resolved, err := o.lookupExplicit(ctx, execCtx, ref, "thread_default")
			if err == nil {
				resolved.Reused = true
				return resolved, nil
			}
		}
		if ref := o.lookupPersistentDefault(ctx, execCtx, data.ShellBindingScopeWorkspace); ref != "" {
			resolved, err := o.lookupExplicit(ctx, execCtx, ref, "workspace_default")
			if err == nil {
				resolved.Reused = true
				return resolved, nil
			}
		}

		shareScope := defaultShareScope(execCtx)
		created, err := o.createSession(ctx, execCtx, shareScope, true)
		if err != nil {
			return nil, err
		}
		created.ResolvedVia = "new_session"
		return created, nil
	}
}

func (o *sessionOrchestrator) resolveWriteSession(
	ctx context.Context,
	req writeStdinArgs,
	execCtx tools.ExecutionContext,
) (*resolvedSession, *tools.ExecutionError) {
	if strings.TrimSpace(req.SessionRef) == "" {
		return nil, sandboxArgsError("parameter session_ref is required")
	}
	return o.lookupExplicit(ctx, execCtx, strings.TrimSpace(req.SessionRef), "explicit_resume")
}

func (o *sessionOrchestrator) lookupExplicit(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	sessionRef string,
	resolvedVia string,
) (*resolvedSession, *tools.ExecutionError) {
	record, found, err := o.lookupSession(ctx, execCtx, sessionRef)
	if err != nil {
		return nil, sandboxArgsError(err.Error())
	}
	if !found {
		return nil, &tools.ExecutionError{ErrorClass: errorSandboxError, Message: "shell session not found", Details: map[string]any{"session_ref": sessionRef}}
	}
	return &resolvedSession{
		SessionRef:               sessionRef,
		ResolvedVia:              resolvedVia,
		Reused:                   true,
		ShareScope:               record.ShareScope,
		Record:                   &record,
		RestoredFromRestoreState: record.LiveSessionID == nil && (strings.TrimSpace(stringPtrValue(record.LatestRestoreRev)) != "" || strings.TrimSpace(stringPtrValue(record.LatestCheckpointRev)) != ""),
	}, nil
}

func (o *sessionOrchestrator) createSession(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	shareScope string,
	persistBinding bool,
) (*resolvedSession, *tools.ExecutionError) {
	sessionRef := newSessionRef()
	record := data.ShellSessionRecord{
		SessionRef:   sessionRef,
		OrgID:        derefUUID(execCtx.OrgID),
		ProfileRef:   strings.TrimSpace(execCtx.ProfileRef),
		WorkspaceRef: strings.TrimSpace(execCtx.WorkspaceRef),
		ThreadID:     execCtx.ThreadID,
		RunID:        uuidPtr(execCtx.RunID),
		ShareScope:   shareScope,
		State:        data.ShellSessionStateReady,
		MetadataJSON: map[string]any{},
	}
	if o.pool != nil {
		if record.OrgID == uuid.Nil {
			return nil, sandboxArgsError("org_id is required for shell sessions")
		}
		if record.ProfileRef == "" || record.WorkspaceRef == "" {
			return nil, sandboxArgsError("profile_ref and workspace_ref are required for shell sessions")
		}
	}
	if err := o.saveSession(ctx, execCtx, record); err != nil {
		return nil, sandboxArgsError(err.Error())
	}
	if persistBinding {
		o.persistDefaultBinding(ctx, execCtx, sessionRef)
	}
	o.setRunDefault(execCtx.RunID, sessionRef)
	return &resolvedSession{
		SessionRef:     sessionRef,
		ResolvedVia:    "new_session",
		Reused:         false,
		ShareScope:     shareScope,
		PersistBinding: persistBinding,
		Record:         &record,
	}, nil
}

func (o *sessionOrchestrator) persistDefaultBinding(ctx context.Context, execCtx tools.ExecutionContext, sessionRef string) {
	if strings.TrimSpace(execCtx.ProfileRef) == "" {
		return
	}
	if o.pool == nil {
		o.mu.Lock()
		defer o.mu.Unlock()
		if target := data.ShellBindingTargetForThread(execCtx.ThreadID); target != "" {
			o.memoryBindings[bindingKey(derefUUID(execCtx.OrgID), execCtx.ProfileRef, data.ShellBindingScopeThread, target)] = sessionRef
			return
		}
		if target := data.ShellBindingTargetForWorkspace(execCtx.WorkspaceRef); target != "" {
			o.memoryBindings[bindingKey(derefUUID(execCtx.OrgID), execCtx.ProfileRef, data.ShellBindingScopeWorkspace, target)] = sessionRef
		}
		return
	}
	if target := data.ShellBindingTargetForThread(execCtx.ThreadID); target != "" {
		_ = o.bindingsRepo.Upsert(ctx, o.pool, derefUUID(execCtx.OrgID), execCtx.ProfileRef, data.ShellBindingScopeThread, target, sessionRef)
		return
	}
	if target := data.ShellBindingTargetForWorkspace(execCtx.WorkspaceRef); target != "" {
		_ = o.bindingsRepo.Upsert(ctx, o.pool, derefUUID(execCtx.OrgID), execCtx.ProfileRef, data.ShellBindingScopeWorkspace, target, sessionRef)
	}
}

func (o *sessionOrchestrator) lookupPersistentDefault(ctx context.Context, execCtx tools.ExecutionContext, scope string) string {
	orgID := derefUUID(execCtx.OrgID)
	profileRef := strings.TrimSpace(execCtx.ProfileRef)
	if orgID == uuid.Nil || profileRef == "" {
		return ""
	}
	target := ""
	switch scope {
	case data.ShellBindingScopeThread:
		target = data.ShellBindingTargetForThread(execCtx.ThreadID)
	case data.ShellBindingScopeWorkspace:
		target = data.ShellBindingTargetForWorkspace(execCtx.WorkspaceRef)
	}
	if target == "" {
		return ""
	}
	if o.pool == nil {
		o.mu.Lock()
		defer o.mu.Unlock()
		return strings.TrimSpace(o.memoryBindings[bindingKey(orgID, profileRef, scope, target)])
	}
	ref, err := o.bindingsRepo.Get(ctx, o.pool, orgID, profileRef, scope, target)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(ref)
}

func (o *sessionOrchestrator) lookupSession(ctx context.Context, execCtx tools.ExecutionContext, sessionRef string) (data.ShellSessionRecord, bool, error) {
	if o.pool == nil {
		o.mu.Lock()
		defer o.mu.Unlock()
		record, ok := o.memorySessions[sessionRef]
		if ok {
			return record, true, nil
		}
		return data.ShellSessionRecord{SessionRef: sessionRef, ShareScope: data.ShellShareScopeRun, State: data.ShellSessionStateReady}, true, nil
	}
	record, err := o.sessionsRepo.GetBySessionRef(ctx, o.pool, derefUUID(execCtx.OrgID), sessionRef)
	if err != nil {
		if data.IsShellSessionNotFound(err) {
			return data.ShellSessionRecord{}, false, nil
		}
		return data.ShellSessionRecord{}, false, err
	}
	return record, true, nil
}

func (o *sessionOrchestrator) saveSession(ctx context.Context, execCtx tools.ExecutionContext, record data.ShellSessionRecord) error {
	if o.pool == nil {
		o.mu.Lock()
		defer o.mu.Unlock()
		o.memorySessions[record.SessionRef] = record
		return nil
	}
	if err := o.registryService.EnsureProfileRegistry(ctx, record.OrgID, record.ProfileRef); err != nil {
		return err
	}
	if err := o.registryService.EnsureWorkspaceRegistry(ctx, record.OrgID, record.WorkspaceRef); err != nil {
		return err
	}
	return o.sessionsRepo.Upsert(ctx, o.pool, record)
}

func (o *sessionOrchestrator) markResult(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	resolution *resolvedSession,
	resp execSessionResponse,
) {
	if resolution == nil {
		return
	}
	orgID := derefUUID(execCtx.OrgID)
	state := data.ShellSessionStateReady
	if resp.Running {
		state = data.ShellSessionStateBusy
	}
	if o.pool == nil {
		o.mu.Lock()
		defer o.mu.Unlock()
		record := o.memorySessions[resolution.SessionRef]
		record.State = state
		liveSessionID := resolution.SessionRef
		record.LiveSessionID = &liveSessionID
		if strings.TrimSpace(resp.RestoreRevision) != "" {
			record.LatestRestoreRev = stringPtr(strings.TrimSpace(resp.RestoreRevision))
		}
		o.memorySessions[resolution.SessionRef] = record
		return
	}
	record := data.ShellSessionRecord{
		SessionRef:       resolution.SessionRef,
		OrgID:            orgID,
		ProfileRef:       strings.TrimSpace(execCtx.ProfileRef),
		WorkspaceRef:     strings.TrimSpace(execCtx.WorkspaceRef),
		ThreadID:         execCtx.ThreadID,
		RunID:            uuidPtr(execCtx.RunID),
		ShareScope:       normalizeRecordShareScope(resolution),
		State:            state,
		LiveSessionID:    stringPtr(resolution.SessionRef),
		LatestRestoreRev: stringPtr(strings.TrimSpace(resp.RestoreRevision)),
		MetadataJSON:     map[string]any{},
	}
	_ = o.sessionsRepo.Upsert(ctx, o.pool, record)
}

func (o *sessionOrchestrator) setRunDefault(runID uuid.UUID, sessionRef string) {
	if runID == uuid.Nil || strings.TrimSpace(sessionRef) == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.runDefaults[runID.String()] = sessionRef
}

func (o *sessionOrchestrator) getRunDefault(runID uuid.UUID) string {
	if runID == uuid.Nil {
		return ""
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return strings.TrimSpace(o.runDefaults[runID.String()])
}

func normalizeSessionMode(value string) string {
	switch strings.TrimSpace(value) {
	case sessionModeNew, sessionModeResume, sessionModeFork:
		return strings.TrimSpace(value)
	default:
		return sessionModeAuto
	}
}

func normalizeRecordShareScope(resolution *resolvedSession) string {
	if resolution == nil {
		return data.ShellShareScopeThread
	}
	if strings.TrimSpace(resolution.ShareScope) != "" {
		return strings.TrimSpace(resolution.ShareScope)
	}
	if resolution.PersistBinding {
		return data.ShellShareScopeThread
	}
	return data.ShellShareScopeRun
}

func defaultShareScope(execCtx tools.ExecutionContext) string {
	if execCtx.ThreadID != nil && *execCtx.ThreadID != uuid.Nil {
		return data.ShellShareScopeThread
	}
	return data.ShellShareScopeWorkspace
}

func newSessionRef() string {
	return "shref_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func bindingKey(orgID uuid.UUID, profileRef string, scope string, target string) string {
	return orgID.String() + "|" + strings.TrimSpace(profileRef) + "|" + strings.TrimSpace(scope) + "|" + strings.TrimSpace(target)
}

func derefUUID(value *uuid.UUID) uuid.UUID {
	if value == nil {
		return uuid.Nil
	}
	return *value
}

func uuidPtr(value uuid.UUID) *uuid.UUID {
	if value == uuid.Nil {
		return nil
	}
	copied := value
	return &copied
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	copied := value
	return &copied
}
