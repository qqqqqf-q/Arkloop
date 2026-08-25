package sandbox

import (
	"context"
	"strings"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/tools"
	"github.com/google/uuid"
)

type sessionACLEvaluator struct{}

func newSessionACLEvaluator() *sessionACLEvaluator {
	return &sessionACLEvaluator{}
}

func (e *sessionACLEvaluator) AuthorizeSession(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	record data.ShellSessionRecord,
) *tools.ExecutionError {
	requestedAccountID := derefUUID(execCtx.AccountID)
	if record.AccountID != uuid.Nil && requestedAccountID != uuid.Nil && record.AccountID != requestedAccountID {
		return sandboxPermissionDenied("shell session access denied", map[string]any{
			"reason":      "account_mismatch",
			"session_ref": record.SessionRef,
		})
	}
	if strings.TrimSpace(record.ProfileRef) != strings.TrimSpace(execCtx.ProfileRef) {
		return sandboxPermissionDenied("shell session access denied", map[string]any{
			"reason":      "profile_mismatch",
			"session_ref": record.SessionRef,
			"share_scope": record.ShareScope,
		})
	}

	switch record.ShareScope {
	case data.ShellShareScopeRun:
		if record.RunID != nil && *record.RunID != execCtx.RunID {
			return sandboxPermissionDenied("shell session access denied", map[string]any{
				"reason":      "run_scope_mismatch",
				"session_ref": record.SessionRef,
				"share_scope": record.ShareScope,
			})
		}
	case data.ShellShareScopeThread:
		if record.ThreadID != nil {
			if execCtx.ThreadID == nil || *record.ThreadID != *execCtx.ThreadID {
				return sandboxPermissionDenied("shell session access denied", map[string]any{
					"reason":      "thread_scope_mismatch",
					"session_ref": record.SessionRef,
					"share_scope": record.ShareScope,
				})
			}
		}
	case data.ShellShareScopeWorkspace:
		if strings.TrimSpace(record.WorkspaceRef) != "" && strings.TrimSpace(record.WorkspaceRef) != strings.TrimSpace(execCtx.WorkspaceRef) {
			return sandboxPermissionDenied("shell session access denied", map[string]any{
				"reason":      "workspace_scope_mismatch",
				"session_ref": record.SessionRef,
				"share_scope": record.ShareScope,
			})
		}
	case data.ShellShareScopeAccount:
		if err := e.authorizeAccountShare(ctx, execCtx, record.SessionRef, record.ShareScope); err != nil {
			return err
		}
	default:
		return sandboxPermissionDenied("shell session access denied", map[string]any{
			"reason":      "invalid_share_scope",
			"session_ref": record.SessionRef,
			"share_scope": record.ShareScope,
		})
	}
	return nil
}

func (e *sessionACLEvaluator) AuthorizeShareScopeCreation(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	shareScope string,
) *tools.ExecutionError {
	if strings.TrimSpace(shareScope) != data.ShellShareScopeAccount {
		return nil
	}
	return e.authorizeAccountShare(ctx, execCtx, "", shareScope)
}

func (e *sessionACLEvaluator) authorizeAccountShare(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	sessionRef string,
	shareScope string,
) *tools.ExecutionError {
	if execCtx.AccountID == nil || *execCtx.AccountID == uuid.Nil || execCtx.UserID == nil || *execCtx.UserID == uuid.Nil {
		return sandboxPermissionDenied("shell session access denied", map[string]any{
			"reason":      "account_scope_actor_missing",
			"session_ref": strings.TrimSpace(sessionRef),
			"share_scope": shareScope,
		})
	}
	// account 级共享仅限 bot owner 本人;多用户 membership 角色判定已随
	// 账户模型塌缩移除,语义等价于"执行上下文身份 == desktop owner"。
	if *execCtx.UserID != data.DesktopUserID {
		return sandboxPermissionDenied("shell session access denied", map[string]any{
			"reason":      "account_scope_forbidden",
			"session_ref": strings.TrimSpace(sessionRef),
			"share_scope": shareScope,
		})
	}
	return nil
}
