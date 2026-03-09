package sandbox

import (
	"context"
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

type sessionOrchestrator struct {
	pool            *pgxpool.Pool
	sessionsRepo    data.ShellSessionsRepository
	registryService *registryService

	mu             sync.Mutex
	memorySessions map[string]data.ShellSessionRecord
}

func newSessionOrchestrator(pool *pgxpool.Pool) *sessionOrchestrator {
	return &sessionOrchestrator{
		pool:            pool,
		registryService: newRegistryService(pool),
		memorySessions:  map[string]data.ShellSessionRecord{},
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
		base, err := o.lookupExplicit(ctx, execCtx, strings.TrimSpace(req.FromSessionRef), "fork_from_restore_state")
		if err != nil {
			return nil, err
		}
		created, createErr := o.createSession(ctx, execCtx, data.ShellShareScopeRun, nil)
		if createErr != nil {
			return nil, createErr
		}
		created.ResolvedVia = "fork_from_restore_state"
		created.FromSessionRef = base.SessionRef
		created.RestoredFromRestoreState = true
		return created, nil
	case sessionModeNew:
		created, err := o.createSession(ctx, execCtx, data.ShellShareScopeRun, nil)
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

		shareScope := defaultShareScope(execCtx)
		defaultBindingKey := defaultBindingKeyForShareScope(execCtx, shareScope)
		created, err := o.createSession(ctx, execCtx, shareScope, defaultBindingKey)
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
	shareScope := defaultShareScope(execCtx)
	defaultBindingKey := defaultBindingKeyForShareScope(execCtx, shareScope)
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
	record, found, err := o.lookupLatestByRun(ctx, derefUUID(execCtx.OrgID), execCtx.RunID)
	if err != nil || !found || record.SessionRef == strings.TrimSpace(skipSessionRef) {
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
	orgID := derefUUID(execCtx.OrgID)
	profileRef := strings.TrimSpace(execCtx.ProfileRef)
	defaultBindingKey = strings.TrimSpace(defaultBindingKey)
	if orgID == uuid.Nil || profileRef == "" || defaultBindingKey == "" {
		return nil
	}
	record, found, err := o.lookupByDefaultBindingKey(ctx, orgID, profileRef, defaultBindingKey)
	if err != nil || !found || record.SessionRef == strings.TrimSpace(skipSessionRef) {
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
	sessionRef := newSessionRef()
	record := data.ShellSessionRecord{
		SessionRef:        sessionRef,
		OrgID:             derefUUID(execCtx.OrgID),
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

func (o *sessionOrchestrator) lookupLatestByRun(
	ctx context.Context,
	orgID uuid.UUID,
	runID uuid.UUID,
) (data.ShellSessionRecord, bool, error) {
	if o.pool == nil {
		o.mu.Lock()
		defer o.mu.Unlock()
		var selected data.ShellSessionRecord
		found := false
		for _, record := range o.memorySessions {
			if record.OrgID != orgID || record.RunID == nil || *record.RunID != runID || record.State == data.ShellSessionStateClosed {
				continue
			}
			if !found || record.LastUsedAt.After(selected.LastUsedAt) || (record.LastUsedAt.Equal(selected.LastUsedAt) && record.UpdatedAt.After(selected.UpdatedAt)) {
				selected = record
				found = true
			}
		}
		return selected, found, nil
	}
	record, err := o.sessionsRepo.GetLatestByRun(ctx, o.pool, orgID, runID)
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
	orgID uuid.UUID,
	profileRef string,
	defaultBindingKey string,
) (data.ShellSessionRecord, bool, error) {
	if o.pool == nil {
		o.mu.Lock()
		defer o.mu.Unlock()
		var selected data.ShellSessionRecord
		found := false
		for _, record := range o.memorySessions {
			if record.OrgID != orgID || strings.TrimSpace(record.ProfileRef) != strings.TrimSpace(profileRef) {
				continue
			}
			if strings.TrimSpace(stringPtrValue(record.DefaultBindingKey)) != strings.TrimSpace(defaultBindingKey) || record.State == data.ShellSessionStateClosed {
				continue
			}
			if !found || record.LastUsedAt.After(selected.LastUsedAt) || (record.LastUsedAt.Equal(selected.LastUsedAt) && record.UpdatedAt.After(selected.UpdatedAt)) {
				selected = record
				found = true
			}
		}
		return selected, found, nil
	}
	record, err := o.sessionsRepo.GetByDefaultBindingKey(ctx, o.pool, orgID, profileRef, defaultBindingKey)
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
		if ok {
			return record, true, nil
		}
		return data.ShellSessionRecord{}, false, nil
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
	now := time.Now().UTC()
	record.LastUsedAt = now
	record.UpdatedAt = now
	if o.pool == nil {
		o.mu.Lock()
		defer o.mu.Unlock()
		o.memorySessions[record.SessionRef] = record
		return nil
	}
	if err := o.registryService.UpsertProfileRegistry(ctx, record.OrgID, execCtx.UserID, record.ProfileRef, stringPtr(record.WorkspaceRef)); err != nil {
		return err
	}
	if err := o.registryService.UpsertWorkspaceRegistry(ctx, record.OrgID, execCtx.UserID, execCtx.ProjectID, record.WorkspaceRef, nil); err != nil {
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
		record.State = data.ShellSessionStateReady
		record.LastUsedAt = time.Now().UTC()
		record.UpdatedAt = record.LastUsedAt
		o.memorySessions[sessionRef] = record
		return nil
	}
	return o.sessionsRepo.ClearLiveSession(ctx, o.pool, derefUUID(execCtx.OrgID), sessionRef)
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
	record := data.ShellSessionRecord{
		SessionRef:        resolution.SessionRef,
		OrgID:             orgID,
		ProfileRef:        strings.TrimSpace(execCtx.ProfileRef),
		WorkspaceRef:      strings.TrimSpace(execCtx.WorkspaceRef),
		ProjectID:         uuidPtr(execCtx.ProjectID),
		ThreadID:          execCtx.ThreadID,
		RunID:             uuidPtr(execCtx.RunID),
		ShareScope:        resolution.ShareScope,
		State:             state,
		LiveSessionID:     stringPtr(resolution.SessionRef),
		DefaultBindingKey: resolution.DefaultBindingKey,
		MetadataJSON:      map[string]any{},
	}
	if resolution.Record != nil {
		record = *resolution.Record
		record.OrgID = orgID
		record.ProfileRef = strings.TrimSpace(execCtx.ProfileRef)
		record.WorkspaceRef = strings.TrimSpace(execCtx.WorkspaceRef)
		record.ProjectID = uuidPtr(execCtx.ProjectID)
		record.ThreadID = execCtx.ThreadID
		record.RunID = uuidPtr(execCtx.RunID)
		record.ShareScope = resolution.ShareScope
		record.State = state
		record.LiveSessionID = stringPtr(resolution.SessionRef)
		record.DefaultBindingKey = resolution.DefaultBindingKey
		if record.MetadataJSON == nil {
			record.MetadataJSON = map[string]any{}
		}
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
	if err := o.registryService.UpsertProfileRegistry(ctx, orgID, execCtx.UserID, record.ProfileRef, stringPtr(record.WorkspaceRef)); err != nil {
		return
	}
	var defaultShellSessionRef *string
	if strings.HasPrefix(stringPtrValue(record.DefaultBindingKey), "workspace:") {
		defaultShellSessionRef = stringPtr(record.SessionRef)
	}
	_ = o.registryService.UpsertWorkspaceRegistry(ctx, orgID, execCtx.UserID, execCtx.ProjectID, record.WorkspaceRef, defaultShellSessionRef)
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

func defaultBindingKeyForShareScope(execCtx tools.ExecutionContext, shareScope string) *string {
	var value string
	switch shareScope {
	case data.ShellShareScopeThread:
		value = data.ShellDefaultBindingKeyForThread(execCtx.ThreadID)
	case data.ShellShareScopeWorkspace:
		value = data.ShellDefaultBindingKeyForWorkspace(execCtx.WorkspaceRef)
	}
	return stringPtr(value)
}

func newSessionRef() string {
	return "shref_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
