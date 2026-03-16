package sandbox

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

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

	minimumWriterLeaseTTL = 2 * time.Minute
	execWriterLeasePad    = 5 * time.Second
)

type resolvedSession struct {
	SessionRef               string
	ResolvedVia              string
	Reused                   bool
	RestoredFromRestoreState bool
	FromSessionRef           string
	ShareScope               string
	OpenMode                 string
	AllowUnavailableFallback bool
	DefaultBindingKey        *string
	Record                   *data.ShellSessionRecord
}

func (r *resolvedSession) ProfileRef(fallback string) string {
	if r != nil && r.Record != nil && strings.TrimSpace(r.Record.ProfileRef) != "" {
		return strings.TrimSpace(r.Record.ProfileRef)
	}
	return strings.TrimSpace(fallback)
}

func (r *resolvedSession) WorkspaceRef(fallback string) string {
	if r != nil && r.Record != nil && strings.TrimSpace(r.Record.WorkspaceRef) != "" {
		return strings.TrimSpace(r.Record.WorkspaceRef)
	}
	return strings.TrimSpace(fallback)
}

func (r *resolvedSession) ProjectID(fallback *uuid.UUID) *uuid.UUID {
	if r != nil && r.Record != nil && r.Record.ProjectID != nil {
		copied := *r.Record.ProjectID
		return &copied
	}
	return uuidPtr(fallback)
}

func (r *resolvedSession) ThreadID(fallback *uuid.UUID) *uuid.UUID {
	if r != nil && r.Record != nil && r.Record.ThreadID != nil {
		copied := *r.Record.ThreadID
		return &copied
	}
	return uuidPtr(fallback)
}

type sessionOrchestrator struct {
	pool            *pgxpool.Pool
	sessionType     string
	sessionsRepo    data.ShellSessionsRepository
	registryService *registryService
	acl             *sessionACLEvaluator

	mu             sync.Mutex
	memorySessions map[string]data.ShellSessionRecord
}

func newSessionOrchestrator(pool *pgxpool.Pool) *sessionOrchestrator {
	return newSessionOrchestratorWithType(pool, data.ShellSessionTypeShell)
}

func newSessionOrchestratorWithType(pool *pgxpool.Pool, sessionType string) *sessionOrchestrator {
	return &sessionOrchestrator{
		pool:            pool,
		sessionType:     normalizeSessionType(sessionType),
		registryService: newRegistryService(pool),
		acl:             newSessionACLEvaluator(pool),
		memorySessions:  map[string]data.ShellSessionRecord{},
	}
}

func (o *sessionOrchestrator) resolveExecSession(
	ctx context.Context,
	req execCommandArgs,
	execCtx tools.ExecutionContext,
) (*resolvedSession, *tools.ExecutionError) {
	mode := normalizeSessionMode(req.SessionMode)
	shareScope, err := normalizeRequestedShareScope(req.ShareScope)
	if err != nil {
		return nil, sandboxArgsError(err.Error())
	}
	if mode == sessionModeResume && strings.TrimSpace(req.SessionRef) == "" {
		return nil, sandboxArgsError("parameter session_ref is required when session_mode=resume")
	}
	if mode == sessionModeFork && strings.TrimSpace(req.FromSessionRef) == "" {
		return nil, sandboxArgsError("parameter from_session_ref is required when session_mode=fork")
	}
	if shareScope != "" && mode == sessionModeAuto && strings.TrimSpace(req.SessionRef) != "" {
		return nil, sandboxArgsError("parameter share_scope is not supported when session_ref is provided")
	}
	if shareScope != "" && mode == sessionModeResume {
		return nil, sandboxArgsError("parameter share_scope is not supported when session_mode=resume")
	}
	if shareScope != "" && mode == sessionModeFork {
		return nil, sandboxArgsError("parameter share_scope is not supported when session_mode=fork")
	}

	if strings.TrimSpace(req.SessionRef) != "" && mode == sessionModeAuto {
		return o.lookupExplicit(ctx, execCtx, strings.TrimSpace(req.SessionRef), "explicit_resume")
	}

	switch mode {
	case sessionModeResume:
		return o.lookupExplicit(ctx, execCtx, strings.TrimSpace(req.SessionRef), "explicit_resume")
	case sessionModeFork:
		base, err := o.lookupExplicit(ctx, execCtx, strings.TrimSpace(req.FromSessionRef), "fork_from_restore_state")
		if err != nil {
			return nil, err
		}
		created, createErr := o.createForkedSession(ctx, execCtx, base)
		if createErr != nil {
			return nil, createErr
		}
		created.ResolvedVia = "fork_from_restore_state"
		created.FromSessionRef = base.SessionRef
		created.RestoredFromRestoreState = true
		return created, nil
	case sessionModeNew:
		resolvedShareScope := requestedShareScopeOrDefault(execCtx, shareScope)
		created, err := o.createSession(ctx, execCtx, resolvedShareScope, defaultBindingKeyForShareScope(execCtx, resolvedShareScope))
		if err != nil {
			return nil, err
		}
		created.ResolvedVia = "new_session"
		return created, nil
	default:
		if resolved := o.lookupRunDefault(ctx, execCtx, ""); resolved != nil {
			resolved.Reused = true
			resolved.ResolvedVia = "run_default"
			resolved.AllowUnavailableFallback = true
			return resolved, nil
		}
		if resolved := o.lookupDefaultBinding(ctx, execCtx, data.ShellDefaultBindingKeyForThread(execCtx.ThreadID), "", "thread_default"); resolved != nil {
			resolved.Reused = true
			resolved.AllowUnavailableFallback = true
			return resolved, nil
		}
		if resolved := o.lookupDefaultBinding(ctx, execCtx, data.ShellDefaultBindingKeyForWorkspace(execCtx.WorkspaceRef), "", "workspace_default"); resolved != nil {
			resolved.Reused = true
			resolved.AllowUnavailableFallback = true
			return resolved, nil
		}

		resolvedShareScope := requestedShareScopeOrDefault(execCtx, shareScope)
		defaultBindingKey := defaultBindingKeyForShareScope(execCtx, resolvedShareScope)
		created, err := o.createSession(ctx, execCtx, resolvedShareScope, defaultBindingKey)
		if err != nil {
			return nil, err
		}
		created.ResolvedVia = "new_session"
		return created, nil
	}
}

func (o *sessionOrchestrator) resolveFallbackSession(
	ctx context.Context,
	req execCommandArgs,
	execCtx tools.ExecutionContext,
	failed *resolvedSession,
) (*resolvedSession, *tools.ExecutionError) {
	if failed == nil || !failed.AllowUnavailableFallback {
		return nil, nil
	}
	if err := o.clearLiveSession(ctx, execCtx, failed.SessionRef); err != nil && !data.IsShellSessionNotFound(err) {
		return nil, sandboxArgsError(err.Error())
	}
	skip := failed.SessionRef
	switch failed.ResolvedVia {
	case "run_default":
		if resolved := o.lookupDefaultBinding(ctx, execCtx, data.ShellDefaultBindingKeyForThread(execCtx.ThreadID), skip, "thread_default"); resolved != nil {
			resolved.Reused = true
			resolved.AllowUnavailableFallback = true
			return resolved, nil
		}
		if resolved := o.lookupDefaultBinding(ctx, execCtx, data.ShellDefaultBindingKeyForWorkspace(execCtx.WorkspaceRef), skip, "workspace_default"); resolved != nil {
			resolved.Reused = true
			resolved.AllowUnavailableFallback = true
			return resolved, nil
		}
	case "thread_default":
		if resolved := o.lookupDefaultBinding(ctx, execCtx, data.ShellDefaultBindingKeyForWorkspace(execCtx.WorkspaceRef), skip, "workspace_default"); resolved != nil {
			resolved.Reused = true
			resolved.AllowUnavailableFallback = true
			return resolved, nil
		}
	case "workspace_default":
	}
	shareScope := requestedShareScopeOrDefault(execCtx, strings.TrimSpace(req.ShareScope))
	defaultBindingKey := defaultBindingKeyForShareScope(execCtx, shareScope)
	if failed.ResolvedVia == "thread_default" || failed.ResolvedVia == "workspace_default" {
		shareScope = failed.ShareScope
		defaultBindingKey = failed.DefaultBindingKey
	}
	created, err := o.createSession(ctx, execCtx, shareScope, defaultBindingKey)
	if err != nil {
		return nil, err
	}
	created.ResolvedVia = "new_session"
	return created, nil
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
	if aclErr := o.acl.AuthorizeSession(ctx, execCtx, record); aclErr != nil {
		return nil, aclErr
	}
	return &resolvedSession{
		SessionRef:               sessionRef,
		ResolvedVia:              resolvedVia,
		Reused:                   true,
		ShareScope:               record.ShareScope,
		OpenMode:                 openModeAttachOrRestore,
		DefaultBindingKey:        record.DefaultBindingKey,
		Record:                   &record,
		RestoredFromRestoreState: record.LiveSessionID == nil && strings.TrimSpace(stringPtrValue(record.LatestRestoreRev)) != "",
	}, nil
}

func (o *sessionOrchestrator) lookupRunDefault(ctx context.Context, execCtx tools.ExecutionContext, skipSessionRef string) *resolvedSession {
	if execCtx.RunID == uuid.Nil {
		return nil
	}
	record, found, err := o.lookupLatestByRun(ctx, derefUUID(execCtx.AccountID), execCtx.RunID)
	if err != nil || !found || record.SessionRef == strings.TrimSpace(skipSessionRef) {
		return nil
	}
	if o.acl.AuthorizeSession(ctx, execCtx, record) != nil {
		return nil
	}
	return &resolvedSession{
		SessionRef:        record.SessionRef,
		ShareScope:        record.ShareScope,
		OpenMode:          openModeAttachOrRestore,
		DefaultBindingKey: record.DefaultBindingKey,
		Record:            &record,
	}
}

func (o *sessionOrchestrator) lookupDefaultBinding(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	defaultBindingKey string,
	skipSessionRef string,
	resolvedVia string,
) *resolvedSession {
	accountID := derefUUID(execCtx.AccountID)
	profileRef := strings.TrimSpace(execCtx.ProfileRef)
	defaultBindingKey = strings.TrimSpace(defaultBindingKey)
	if accountID == uuid.Nil || profileRef == "" || defaultBindingKey == "" {
		return nil
	}
	record, found, err := o.lookupByDefaultBindingKey(ctx, accountID, profileRef, defaultBindingKey)
	if err != nil || !found || record.SessionRef == strings.TrimSpace(skipSessionRef) {
		return nil
	}
	if o.acl.AuthorizeSession(ctx, execCtx, record) != nil {
		return nil
	}
	return &resolvedSession{
		SessionRef:        record.SessionRef,
		ResolvedVia:       resolvedVia,
		ShareScope:        record.ShareScope,
		OpenMode:          openModeAttachOrRestore,
		DefaultBindingKey: record.DefaultBindingKey,
		Record:            &record,
	}
}

func (o *sessionOrchestrator) createSession(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	shareScope string,
	defaultBindingKey *string,
) (*resolvedSession, *tools.ExecutionError) {
	return o.createSessionWithRef(ctx, execCtx, shareScope, defaultBindingKey, "")
}

func (o *sessionOrchestrator) createSessionWithRef(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	shareScope string,
	defaultBindingKey *string,
	sessionRef string,
) (*resolvedSession, *tools.ExecutionError) {
	if aclErr := o.acl.AuthorizeShareScopeCreation(ctx, execCtx, shareScope); aclErr != nil {
		return nil, aclErr
	}
	sessionRef = strings.TrimSpace(sessionRef)
	if sessionRef == "" {
		sessionRef = newSessionRef(o.sessionType)
	}
	record := data.ShellSessionRecord{
		SessionRef:        sessionRef,
		SessionType:       o.sessionType,
		AccountID:             derefUUID(execCtx.AccountID),
		ProfileRef:        strings.TrimSpace(execCtx.ProfileRef),
		WorkspaceRef:      strings.TrimSpace(execCtx.WorkspaceRef),
		ProjectID:         uuidPtr(execCtx.ProjectID),
		ThreadID:          execCtx.ThreadID,
		RunID:             uuidPtr(execCtx.RunID),
		ShareScope:        shareScope,
		State:             data.ShellSessionStateReady,
		DefaultBindingKey: defaultBindingKey,
		MetadataJSON:      map[string]any{},
	}
	if o.pool != nil {
		if record.AccountID == uuid.Nil {
			return nil, sandboxArgsError("account_id is required for shell sessions")
		}
		if record.ProfileRef == "" || record.WorkspaceRef == "" {
			return nil, sandboxArgsError("profile_ref and workspace_ref are required for shell sessions")
		}
	}
	if err := o.saveSession(ctx, execCtx, record); err != nil {
		return nil, sandboxArgsError(err.Error())
	}
	return &resolvedSession{
		SessionRef:        sessionRef,
		ResolvedVia:       "new_session",
		Reused:            false,
		ShareScope:        shareScope,
		OpenMode:          openModeCreate,
		DefaultBindingKey: defaultBindingKey,
		Record:            &record,
	}, nil
}

func (o *sessionOrchestrator) createForkedSession(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	base *resolvedSession,
) (*resolvedSession, *tools.ExecutionError) {
	if base == nil || base.Record == nil {
		return nil, sandboxArgsError("fork source session is required")
	}
	record := *base.Record
	record.SessionRef = newSessionRef(o.sessionType)
	record.SessionType = o.sessionType
	record.AccountID = derefUUID(execCtx.AccountID)
	record.RunID = uuidPtr(execCtx.RunID)
	record.State = data.ShellSessionStateReady
	record.LiveSessionID = nil
	record.LatestRestoreRev = nil
	record.DefaultBindingKey = nil
	record.LeaseOwnerID = nil
	record.LeaseUntil = nil
	record.MetadataJSON = cloneMetadata(record.MetadataJSON)
	if err := o.saveSession(ctx, execCtx, record); err != nil {
		return nil, sandboxArgsError(err.Error())
	}
	return &resolvedSession{
		SessionRef:        record.SessionRef,
		ResolvedVia:       "new_session",
		Reused:            false,
		ShareScope:        record.ShareScope,
		OpenMode:          openModeCreate,
		DefaultBindingKey: record.DefaultBindingKey,
		Record:            &record,
	}, nil
}

func (o *sessionOrchestrator) resolveBrowserSession(
	ctx context.Context,
	req browserArgs,
	execCtx tools.ExecutionContext,
) (*resolvedSession, *tools.ExecutionError) {
	if resolved := o.lookupRunDefault(ctx, execCtx, ""); resolved != nil {
		resolved.Reused = true
		resolved.ResolvedVia = "run_default"
		resolved.AllowUnavailableFallback = true
		return resolved, nil
	}
	if resolved := o.lookupDefaultBinding(ctx, execCtx, data.ShellDefaultBindingKeyForThread(execCtx.ThreadID), "", "thread_default"); resolved != nil {
		resolved.Reused = true
		resolved.AllowUnavailableFallback = true
		return resolved, nil
	}
	if resolved := o.lookupDefaultBinding(ctx, execCtx, data.ShellDefaultBindingKeyForWorkspace(execCtx.WorkspaceRef), "", "workspace_default"); resolved != nil {
		resolved.Reused = true
		resolved.AllowUnavailableFallback = true
		return resolved, nil
	}
	shareScope := defaultShareScope(execCtx)
	defaultBindingKey := defaultBindingKeyForShareScope(execCtx, shareScope)
	created, err := o.createSession(ctx, execCtx, shareScope, defaultBindingKey)
	if err != nil {
		return nil, err
	}
	created.ResolvedVia = "new_session"
	return created, nil
}

func (o *sessionOrchestrator) lookupLatestByRun(
	ctx context.Context,
	accountID uuid.UUID,
	runID uuid.UUID,
) (data.ShellSessionRecord, bool, error) {
	if o.pool == nil {
		o.mu.Lock()
		defer o.mu.Unlock()
		var selected data.ShellSessionRecord
		found := false
		for _, record := range o.memorySessions {
			if record.AccountID != accountID || record.SessionType != o.sessionType || record.RunID == nil || *record.RunID != runID || record.State == data.ShellSessionStateClosed {
				continue
			}
			if !found || record.LastUsedAt.After(selected.LastUsedAt) || (record.LastUsedAt.Equal(selected.LastUsedAt) && record.UpdatedAt.After(selected.UpdatedAt)) {
				selected = record
				found = true
			}
		}
		return selected, found, nil
	}
	record, err := o.sessionsRepo.GetLatestByRunAndType(ctx, o.pool, accountID, runID, o.sessionType)
	if err != nil {
		if data.IsShellSessionNotFound(err) {
			return data.ShellSessionRecord{}, false, nil
		}
		return data.ShellSessionRecord{}, false, err
	}
	return record, true, nil
}

func (o *sessionOrchestrator) lookupByDefaultBindingKey(
	ctx context.Context,
	accountID uuid.UUID,
	profileRef string,
	defaultBindingKey string,
) (data.ShellSessionRecord, bool, error) {
	if o.pool == nil {
		o.mu.Lock()
		defer o.mu.Unlock()
		var selected data.ShellSessionRecord
		found := false
		for _, record := range o.memorySessions {
			if record.AccountID != accountID || record.SessionType != o.sessionType || strings.TrimSpace(record.ProfileRef) != strings.TrimSpace(profileRef) {
				continue
			}
			if strings.TrimSpace(stringPtrValue(record.DefaultBindingKey)) != strings.TrimSpace(defaultBindingKey) || record.State == data.ShellSessionStateClosed {
				continue
			}
			selected = record
			found = true
			break
		}
		return selected, found, nil
	}
	record, err := o.sessionsRepo.GetByDefaultBindingKeyAndType(ctx, o.pool, accountID, profileRef, defaultBindingKey, o.sessionType)
	if err != nil {
		if data.IsShellSessionNotFound(err) {
			return data.ShellSessionRecord{}, false, nil
		}
		return data.ShellSessionRecord{}, false, err
	}
	return record, true, nil
}

func (o *sessionOrchestrator) lookupSession(ctx context.Context, execCtx tools.ExecutionContext, sessionRef string) (data.ShellSessionRecord, bool, error) {
	if o.pool == nil {
		o.mu.Lock()
		defer o.mu.Unlock()
		record, ok := o.memorySessions[sessionRef]
		if ok && record.SessionType == o.sessionType {
			return record, true, nil
		}
		return data.ShellSessionRecord{}, false, nil
	}
	record, err := o.sessionsRepo.GetBySessionRefAndType(ctx, o.pool, derefUUID(execCtx.AccountID), sessionRef, o.sessionType)
	if err != nil {
		if data.IsShellSessionNotFound(err) {
			return data.ShellSessionRecord{}, false, nil
		}
		return data.ShellSessionRecord{}, false, err
	}
	return record, true, nil
}

func (o *sessionOrchestrator) saveSession(ctx context.Context, execCtx tools.ExecutionContext, record data.ShellSessionRecord) error {
	now := time.Now().UTC()
	record.LastUsedAt = now
	record.UpdatedAt = now
	if o.pool == nil {
		o.mu.Lock()
		defer o.mu.Unlock()
		if record.DefaultBindingKey != nil {
			bindingKey := stringPtrValue(record.DefaultBindingKey)
			for sessionRef, existing := range o.memorySessions {
				if sessionRef == record.SessionRef {
					continue
				}
				if existing.AccountID != record.AccountID || existing.SessionType != record.SessionType || existing.State == data.ShellSessionStateClosed {
					continue
				}
				if strings.TrimSpace(existing.ProfileRef) != strings.TrimSpace(record.ProfileRef) || stringPtrValue(existing.DefaultBindingKey) != bindingKey {
					continue
				}
				existing.DefaultBindingKey = nil
				existing.LastUsedAt = now
				existing.UpdatedAt = now
				o.memorySessions[sessionRef] = existing
			}
		}
		o.memorySessions[record.SessionRef] = record
		return nil
	}
	if err := o.registryService.UpsertProfileRegistry(ctx, record.AccountID, execCtx.UserID, record.ProfileRef, stringPtr(record.WorkspaceRef)); err != nil {
		return err
	}
	if err := o.registryService.UpsertWorkspaceRegistry(ctx, record.AccountID, execCtx.UserID, record.ProjectID, record.WorkspaceRef, nil); err != nil {
		return err
	}
	return o.sessionsRepo.Upsert(ctx, o.pool, record)
}

func (o *sessionOrchestrator) clearLiveSession(ctx context.Context, execCtx tools.ExecutionContext, sessionRef string) error {
	if o.pool == nil {
		o.mu.Lock()
		defer o.mu.Unlock()
		record, ok := o.memorySessions[sessionRef]
		if !ok {
			return nil
		}
		record.LiveSessionID = nil
		record.LeaseOwnerID = nil
		record.LeaseUntil = nil
		record.State = data.ShellSessionStateReady
		record.LastUsedAt = time.Now().UTC()
		record.UpdatedAt = record.LastUsedAt
		o.memorySessions[sessionRef] = record
		return nil
	}
	return o.sessionsRepo.ClearLiveSession(ctx, o.pool, derefUUID(execCtx.AccountID), sessionRef)
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
	accountID := derefUUID(execCtx.AccountID)
	state := data.ShellSessionStateReady
	if resp.Running {
		state = data.ShellSessionStateBusy
	}
	record := data.ShellSessionRecord{
		SessionRef:        resolution.SessionRef,
		SessionType:       o.sessionType,
		AccountID:             accountID,
		ProfileRef:        resolution.ProfileRef(execCtx.ProfileRef),
		WorkspaceRef:      resolution.WorkspaceRef(execCtx.WorkspaceRef),
		ProjectID:         resolution.ProjectID(execCtx.ProjectID),
		ThreadID:          resolution.ThreadID(execCtx.ThreadID),
		RunID:             uuidPtr(execCtx.RunID),
		ShareScope:        resolution.ShareScope,
		State:             state,
		LiveSessionID:     stringPtr(resolution.SessionRef),
		DefaultBindingKey: resolution.DefaultBindingKey,
		MetadataJSON:      map[string]any{},
	}
	if resolution.Record != nil {
		record = *resolution.Record
		record.AccountID = accountID
		record.SessionType = o.sessionType
		record.RunID = uuidPtr(execCtx.RunID)
		record.State = state
		record.LiveSessionID = stringPtr(resolution.SessionRef)
		if record.MetadataJSON == nil {
			record.MetadataJSON = map[string]any{}
		}
	}
	if !resp.Running {
		record.LeaseOwnerID = nil
		record.LeaseUntil = nil
	}
	if strings.TrimSpace(resp.RestoreRevision) != "" {
		record.LatestRestoreRev = stringPtr(strings.TrimSpace(resp.RestoreRevision))
	}
	record.LastUsedAt = time.Now().UTC()
	record.UpdatedAt = record.LastUsedAt
	if o.pool == nil {
		o.mu.Lock()
		defer o.mu.Unlock()
		o.memorySessions[resolution.SessionRef] = record
		return
	}
	if err := o.sessionsRepo.Upsert(ctx, o.pool, record); err != nil {
		return
	}
	if err := o.registryService.UpsertProfileRegistry(ctx, accountID, execCtx.UserID, record.ProfileRef, stringPtr(record.WorkspaceRef)); err != nil {
		return
	}
	var defaultShellSessionRef *string
	if o.sessionType == data.ShellSessionTypeShell && strings.HasPrefix(stringPtrValue(record.DefaultBindingKey), "workspace:") {
		defaultShellSessionRef = stringPtr(record.SessionRef)
	}
	_ = o.registryService.UpsertWorkspaceRegistry(ctx, accountID, execCtx.UserID, record.ProjectID, record.WorkspaceRef, defaultShellSessionRef)
}

func normalizeSessionMode(value string) string {
	switch strings.TrimSpace(value) {
	case sessionModeNew, sessionModeResume, sessionModeFork:
		return strings.TrimSpace(value)
	default:
		return sessionModeAuto
	}
}

func defaultShareScope(execCtx tools.ExecutionContext) string {
	if execCtx.ThreadID != nil && *execCtx.ThreadID != uuid.Nil {
		return data.ShellShareScopeThread
	}
	return data.ShellShareScopeWorkspace
}

func requestedShareScopeOrDefault(execCtx tools.ExecutionContext, requested string) string {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		return requested
	}
	return defaultShareScope(execCtx)
}

func normalizeRequestedShareScope(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "":
		return "", nil
	case data.ShellShareScopeRun, data.ShellShareScopeThread, data.ShellShareScopeWorkspace, data.ShellShareScopeAccount:
		return strings.TrimSpace(value), nil
	default:
		return "", fmt.Errorf("parameter share_scope must be one of run, thread, workspace, org")
	}
}

func defaultBindingKeyForShareScope(execCtx tools.ExecutionContext, shareScope string) *string {
	var value string
	switch shareScope {
	case data.ShellShareScopeRun:
		return nil
	case data.ShellShareScopeThread:
		value = data.ShellDefaultBindingKeyForThread(execCtx.ThreadID)
	case data.ShellShareScopeWorkspace:
		value = data.ShellDefaultBindingKeyForWorkspace(execCtx.WorkspaceRef)
	}
	return stringPtr(value)
}

func newSessionRef(sessionType string) string {
	prefix := "shref_"
	if normalizeSessionType(sessionType) == data.ShellSessionTypeBrowser {
		prefix = "brref_"
	}
	return prefix + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func normalizeSessionType(value string) string {
	if strings.TrimSpace(value) == data.ShellSessionTypeBrowser {
		return data.ShellSessionTypeBrowser
	}
	return data.ShellSessionTypeShell
}

func derefUUID(value *uuid.UUID) uuid.UUID {
	if value == nil {
		return uuid.Nil
	}
	return *value
}

func uuidPtr(value any) *uuid.UUID {
	switch typed := value.(type) {
	case uuid.UUID:
		if typed == uuid.Nil {
			return nil
		}
		copied := typed
		return &copied
	case *uuid.UUID:
		if typed == nil || *typed == uuid.Nil {
			return nil
		}
		copied := *typed
		return &copied
	default:
		return nil
	}
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

func (o *sessionOrchestrator) prepareExecWriterLease(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	resolution *resolvedSession,
	ownerID string,
	timeoutMs int,
) *tools.ExecutionError {
	if resolution == nil {
		return nil
	}
	record := o.sessionRecord(execCtx, resolution)
	if record.State == data.ShellSessionStateBusy && hasActiveWriterLease(record, time.Now().UTC()) {
		currentOwner := strings.TrimSpace(stringPtrValue(record.LeaseOwnerID))
		if isLeaseFromSameRun(currentOwner, execCtx.RunID) {
			return sessionRunningError(resolution.SessionRef)
		}
		return shellBusyError(resolution.SessionRef, "fork")
	}
	updated, err := o.acquireWriterLease(ctx, execCtx, resolution, ownerID, execWriterLeaseUntil(timeoutMs))
	if err != nil {
		return err
	}
	resolution.Record = &updated
	return nil
}

func (o *sessionOrchestrator) prepareWriteWriterLease(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	resolution *resolvedSession,
	ownerID string,
	hasInput bool,
) *tools.ExecutionError {
	if resolution == nil || !hasInput {
		return nil
	}
	updated, err := o.acquireWriterLease(ctx, execCtx, resolution, ownerID, writeWriterLeaseUntil())
	if err != nil {
		return err
	}
	resolution.Record = &updated
	return nil
}

func (o *sessionOrchestrator) reconcileWriteWriterLease(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	resolution *resolvedSession,
	ownerID string,
	hasInput bool,
	resp execSessionResponse,
) {
	if resolution == nil {
		return
	}
	if !resp.Running {
		_ = o.clearFinishedWriterLease(ctx, execCtx, resolution)
		return
	}
	if hasInput {
		return
	}
	if strings.TrimSpace(stringPtrValue(resolution.RecordLeaseOwnerID())) != ownerID {
		return
	}
	updated, err := o.renewWriterLease(ctx, execCtx, resolution, ownerID, writeWriterLeaseUntil())
	if err != nil {
		return
	}
	resolution.Record = &updated
}

func (o *sessionOrchestrator) releaseWriterLease(ctx context.Context, execCtx tools.ExecutionContext, resolution *resolvedSession, ownerID string) {
	if resolution == nil || strings.TrimSpace(ownerID) == "" {
		return
	}
	if o.pool == nil {
		o.mu.Lock()
		defer o.mu.Unlock()
		record, ok := o.memorySessions[resolution.SessionRef]
		if !ok || strings.TrimSpace(stringPtrValue(record.LeaseOwnerID)) != strings.TrimSpace(ownerID) {
			return
		}
		record.LeaseOwnerID = nil
		record.LeaseUntil = nil
		record.LastUsedAt = time.Now().UTC()
		record.UpdatedAt = record.LastUsedAt
		o.memorySessions[resolution.SessionRef] = record
		resolution.Record = &record
		return
	}
	if err := o.sessionsRepo.ReleaseWriterLease(ctx, o.pool, derefUUID(execCtx.AccountID), resolution.SessionRef, ownerID); err != nil {
		return
	}
	if resolution.Record != nil {
		resolution.Record.LeaseOwnerID = nil
		resolution.Record.LeaseUntil = nil
	}
}

func (o *sessionOrchestrator) clearFinishedWriterLease(ctx context.Context, execCtx tools.ExecutionContext, resolution *resolvedSession) error {
	if resolution == nil {
		return nil
	}
	if o.pool == nil {
		o.mu.Lock()
		defer o.mu.Unlock()
		record, ok := o.memorySessions[resolution.SessionRef]
		if !ok {
			return nil
		}
		record.LeaseOwnerID = nil
		record.LeaseUntil = nil
		record.State = data.ShellSessionStateReady
		record.LastUsedAt = time.Now().UTC()
		record.UpdatedAt = record.LastUsedAt
		o.memorySessions[resolution.SessionRef] = record
		resolution.Record = &record
		return nil
	}
	if err := o.sessionsRepo.ClearFinishedWriterLease(ctx, o.pool, derefUUID(execCtx.AccountID), resolution.SessionRef); err != nil && !data.IsShellSessionNotFound(err) {
		return err
	}
	if resolution.Record != nil {
		resolution.Record.LeaseOwnerID = nil
		resolution.Record.LeaseUntil = nil
		resolution.Record.State = data.ShellSessionStateReady
	}
	return nil
}

func (o *sessionOrchestrator) acquireWriterLease(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	resolution *resolvedSession,
	ownerID string,
	leaseUntil time.Time,
) (data.ShellSessionRecord, *tools.ExecutionError) {
	return o.updateWriterLease(ctx, execCtx, resolution, ownerID, leaseUntil, false)
}

func (o *sessionOrchestrator) renewWriterLease(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	resolution *resolvedSession,
	ownerID string,
	leaseUntil time.Time,
) (data.ShellSessionRecord, *tools.ExecutionError) {
	return o.updateWriterLease(ctx, execCtx, resolution, ownerID, leaseUntil, true)
}

func (o *sessionOrchestrator) updateWriterLease(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	resolution *resolvedSession,
	ownerID string,
	leaseUntil time.Time,
	renewOnly bool,
) (data.ShellSessionRecord, *tools.ExecutionError) {
	ownerID = strings.TrimSpace(ownerID)
	if resolution == nil || ownerID == "" {
		return data.ShellSessionRecord{}, sandboxArgsError("run_id is required for shell writer lease")
	}
	if o.pool == nil {
		return o.updateMemoryWriterLease(execCtx, resolution, ownerID, leaseUntil, renewOnly)
	}
	var (
		record data.ShellSessionRecord
		err    error
	)
	if renewOnly {
		record, err = o.sessionsRepo.RenewWriterLease(ctx, o.pool, derefUUID(execCtx.AccountID), resolution.SessionRef, ownerID, leaseUntil)
	} else {
		record, err = o.sessionsRepo.AcquireWriterLease(ctx, o.pool, derefUUID(execCtx.AccountID), resolution.SessionRef, ownerID, leaseUntil)
	}
	if err == nil {
		return record, nil
	}
	if data.IsShellSessionLeaseConflict(err) {
		return data.ShellSessionRecord{}, shellBusyError(resolution.SessionRef, "wait_for_current_writer")
	}
	if data.IsShellSessionNotFound(err) {
		return data.ShellSessionRecord{}, &tools.ExecutionError{ErrorClass: errorSandboxError, Message: "shell session not found", Details: map[string]any{"session_ref": resolution.SessionRef, "code": "shell.session_not_found"}}
	}
	return data.ShellSessionRecord{}, sandboxArgsError(err.Error())
}

func (o *sessionOrchestrator) updateMemoryWriterLease(
	execCtx tools.ExecutionContext,
	resolution *resolvedSession,
	ownerID string,
	leaseUntil time.Time,
	renewOnly bool,
) (data.ShellSessionRecord, *tools.ExecutionError) {
	o.mu.Lock()
	defer o.mu.Unlock()
	record, ok := o.memorySessions[resolution.SessionRef]
	if !ok {
		record = o.sessionRecord(execCtx, resolution)
	}
	now := time.Now().UTC()
	currentOwner := strings.TrimSpace(stringPtrValue(record.LeaseOwnerID))
	active := hasActiveWriterLease(record, now)
	if renewOnly {
		if currentOwner != ownerID {
			return data.ShellSessionRecord{}, shellBusyError(resolution.SessionRef, "wait_for_current_writer")
		}
	} else if active && currentOwner != ownerID {
		return data.ShellSessionRecord{}, shellBusyError(resolution.SessionRef, "wait_for_current_writer")
	}
	if currentOwner != "" && currentOwner != ownerID {
		record.LeaseEpoch++
	}
	record.LeaseOwnerID = stringPtr(ownerID)
	record.LeaseUntil = timePtr(leaseUntil)
	record.LastUsedAt = now
	record.UpdatedAt = now
	o.memorySessions[resolution.SessionRef] = record
	return record, nil
}

func (o *sessionOrchestrator) sessionRecord(execCtx tools.ExecutionContext, resolution *resolvedSession) data.ShellSessionRecord {
	if resolution != nil && resolution.Record != nil {
		return *resolution.Record
	}
	return data.ShellSessionRecord{
		SessionRef:        resolution.SessionRef,
		AccountID:             derefUUID(execCtx.AccountID),
		ProfileRef:        strings.TrimSpace(execCtx.ProfileRef),
		WorkspaceRef:      strings.TrimSpace(execCtx.WorkspaceRef),
		ProjectID:         uuidPtr(execCtx.ProjectID),
		ThreadID:          execCtx.ThreadID,
		RunID:             uuidPtr(execCtx.RunID),
		ShareScope:        resolution.ShareScope,
		State:             data.ShellSessionStateReady,
		DefaultBindingKey: resolution.DefaultBindingKey,
		MetadataJSON:      map[string]any{},
	}
}

func (r *resolvedSession) RecordLeaseOwnerID() *string {
	if r == nil || r.Record == nil {
		return nil
	}
	return r.Record.LeaseOwnerID
}

func writerLeaseOwner(execCtx tools.ExecutionContext, toolCallID string) string {
	if execCtx.RunID == uuid.Nil {
		return ""
	}
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		toolCallID = "direct"
	}
	return "run:" + execCtx.RunID.String() + ":call:" + toolCallID
}

func cloneMetadata(source map[string]any) map[string]any {
	if len(source) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func execWriterLeaseUntil(timeoutMs int) time.Time {
	now := time.Now().UTC()
	leaseTTL := minimumWriterLeaseTTL
	if timeoutMs > 0 {
		candidate := time.Duration(timeoutMs)*time.Millisecond + execWriterLeasePad
		if candidate > leaseTTL {
			leaseTTL = candidate
		}
	}
	return now.Add(leaseTTL)
}

func writeWriterLeaseUntil() time.Time {
	return time.Now().UTC().Add(minimumWriterLeaseTTL)
}

func hasActiveWriterLease(record data.ShellSessionRecord, now time.Time) bool {
	return record.LeaseOwnerID != nil && record.LeaseUntil != nil && record.LeaseUntil.After(now.UTC())
}

func timePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copyValue := value.UTC()
	return &copyValue
}

// isLeaseFromSameRun reports whether a lease owner ID belongs to the given run.
func isLeaseFromSameRun(leaseOwnerID string, runID uuid.UUID) bool {
	if runID == uuid.Nil || leaseOwnerID == "" {
		return false
	}
	return strings.HasPrefix(leaseOwnerID, "run:"+runID.String()+":")
}

// sessionRunningError is returned when the session is busy because a command
// from the same run is still executing. The model should poll with write_stdin.
func sessionRunningError(sessionRef string) *tools.ExecutionError {
	details := map[string]any{
		"code":        "shell.session_running",
		"retry_via":   "write_stdin",
		"session_ref": sessionRef,
	}
	return &tools.ExecutionError{
		ErrorClass: errorSandboxError,
		Message:    "shell session has a running command",
		Details:    details,
	}
}
