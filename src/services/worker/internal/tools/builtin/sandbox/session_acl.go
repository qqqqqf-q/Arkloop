package sandbox

import (
	"context"
	"strings"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/tools"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	orgRoleOwner         = "owner"
	orgRoleOrgAdmin      = "org_admin"
	orgRolePlatformAdmin = "platform_admin"
)

type sessionACLEvaluator struct {
	pool            *pgxpool.Pool
	membershipsRepo data.AccountMembershipsRepository
}

func newSessionACLEvaluator(pool *pgxpool.Pool) *sessionACLEvaluator {
	return &sessionACLEvaluator{pool: pool}
}

func (e *sessionACLEvaluator) AuthorizeSession(
	ctx context.Context,
	execCtx tools.ExecutionContext,
	record data.ShellSessionRecord,
) *tools.ExecutionError {
	requestedAccountID := derefUUID(execCtx.AccountID)
	if record.AccountID != uuid.Nil && requestedAccountID != uuid.Nil && record.AccountID != requestedAccountID {
		return sandboxPermissionDenied("shell session access denied", map[string]any{
			"reason":      "org_mismatch",
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
	if e.pool == nil {
		return sandboxPermissionDenied("shell session access denied", map[string]any{
			"reason":      "account_scope_acl_unavailable",
			"session_ref": strings.TrimSpace(sessionRef),
			"share_scope": shareScope,
		})
	}
	membership, err := e.membershipsRepo.GetByAccountAndUser(ctx, e.pool, *execCtx.AccountID, *execCtx.UserID)
	if err != nil {
		return sandboxPermissionDenied("shell session access denied", map[string]any{
			"reason":      "account_scope_acl_error",
			"session_ref": strings.TrimSpace(sessionRef),
			"share_scope": shareScope,
		})
	}
	if membership == nil || !canUseOrgSharedSession(membership.Role) {
		return sandboxPermissionDenied("shell session access denied", map[string]any{
			"reason":      "account_scope_forbidden",
			"session_ref": strings.TrimSpace(sessionRef),
			"share_scope": shareScope,
		})
	}
	return nil
}

func canUseOrgSharedSession(role string) bool {
	switch strings.TrimSpace(role) {
	case orgRoleOwner, orgRoleOrgAdmin, orgRolePlatformAdmin:
		return true
	default:
		return false
	}
}
