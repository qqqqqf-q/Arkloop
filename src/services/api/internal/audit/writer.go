package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type Writer struct {
	auditRepo      *data.AuditLogRepository
	membershipRepo *data.OrgMembershipRepository
	logger         *observability.JSONLogger
}

func NewWriter(
	auditRepo *data.AuditLogRepository,
	membershipRepo *data.OrgMembershipRepository,
	logger *observability.JSONLogger,
) *Writer {
	return &Writer{
		auditRepo:      auditRepo,
		membershipRepo: membershipRepo,
		logger:         logger,
	}
}

func (w *Writer) WriteLoginFailed(ctx context.Context, traceID string, login string) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	loginHash := sha256Hex(login)
	targetType := "user_login"
	targetID := loginHash
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		Action:    "auth.login",
		TargetType: &targetType,
		TargetID:  &targetID,
		TraceID:   traceID,
		IPAddress: ip,
		UserAgent: ua,
		Metadata: map[string]any{
			"result":     "failed",
			"method":     "password",
			"login_hash": loginHash,
		},
	}); err != nil {
		w.logError(traceID, "failed to write login-failed audit log", err)
	}
}

func (w *Writer) WriteLoginSucceeded(ctx context.Context, traceID string, userID uuid.UUID, login string) {
	if w == nil || w.auditRepo == nil {
		return
	}

	var orgID *uuid.UUID
	if w.membershipRepo != nil {
		membership, err := w.membershipRepo.GetDefaultForUser(ctx, userID)
		if err != nil {
			w.logError(traceID, "failed to read default org", err)
		} else if membership != nil {
			orgID = &membership.OrgID
		}
	}

	ip, ua := requestMetaFromContext(ctx)
	loginHash := sha256Hex(login)
	targetType := "user"
	targetID := userID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		OrgID:       orgID,
		ActorUserID: &userID,
		Action:      "auth.login",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata: map[string]any{
			"result":     "succeeded",
			"method":     "password",
			"login_hash": loginHash,
		},
	}); err != nil {
		w.logError(traceID, "failed to write login-succeeded audit log", err)
	}
}

func (w *Writer) WriteTokenRefreshed(ctx context.Context, traceID string, userID uuid.UUID) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	targetType := "user"
	targetID := userID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		ActorUserID: &userID,
		Action:      "auth.refresh",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata:    map[string]any{},
	}); err != nil {
		w.logError(traceID, "failed to write refresh audit log", err)
	}
}

func (w *Writer) WriteLogout(ctx context.Context, traceID string, userID uuid.UUID) {
	if w == nil || w.auditRepo == nil {
		return
	}

	var orgID *uuid.UUID
	if w.membershipRepo != nil {
		membership, err := w.membershipRepo.GetDefaultForUser(ctx, userID)
		if err != nil {
			w.logError(traceID, "failed to read default org", err)
		} else if membership != nil {
			orgID = &membership.OrgID
		}
	}

	ip, ua := requestMetaFromContext(ctx)
	targetType := "user"
	targetID := userID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		OrgID:       orgID,
		ActorUserID: &userID,
		Action:      "auth.logout",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata:    map[string]any{},
	}); err != nil {
		w.logError(traceID, "failed to write logout audit log", err)
	}
}

func (w *Writer) WriteUserRegistered(ctx context.Context, traceID string, userID uuid.UUID, login string) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	loginHash := sha256Hex(login)
	targetType := "user"
	targetID := userID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		ActorUserID: &userID,
		Action:      "auth.register",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata: map[string]any{
			"login_hash": loginHash,
		},
	}); err != nil {
		w.logError(traceID, "failed to write register audit log", err)
	}
}

func (w *Writer) WriteRunCancelRequested(
	ctx context.Context,
	traceID string,
	actorOrgID uuid.UUID,
	actorUserID uuid.UUID,
	runID uuid.UUID,
) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	targetType := "run"
	targetID := runID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		OrgID:       &actorOrgID,
		ActorUserID: &actorUserID,
		Action:      "runs.cancel",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata: map[string]any{
			"result": "requested",
		},
	}); err != nil {
		w.logError(traceID, "failed to write cancel audit log", err)
	}
}

func (w *Writer) WriteThreadDeleted(
	ctx context.Context,
	traceID string,
	actorOrgID uuid.UUID,
	actorUserID uuid.UUID,
	threadID uuid.UUID,
) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	targetType := "thread"
	targetID := threadID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		OrgID:       &actorOrgID,
		ActorUserID: &actorUserID,
		Action:      "threads.delete",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata:    map[string]any{"result": "deleted"},
	}); err != nil {
		w.logError(traceID, "failed to write thread-deleted audit log", err)
	}
}

func (w *Writer) WriteAccessDenied(
	ctx context.Context,
	traceID string,
	actorOrgID uuid.UUID,
	actorUserID uuid.UUID,
	action string,
	targetType string,
	targetID string,
	resourceOrgID uuid.UUID,
	resourceOwnerUserID *uuid.UUID,
	denyReason string,
) {
	if w == nil || w.auditRepo == nil {
		return
	}

	var owner any
	if resourceOwnerUserID == nil {
		owner = nil
	} else {
		owner = resourceOwnerUserID.String()
	}

	orgID := actorOrgID.String()
	if w.logger != nil {
		w.logger.Info(
			"access denied",
			observability.LogFields{TraceID: &traceID, OrgID: &orgID},
			map[string]any{
				"action":                 action,
				"target_type":            targetType,
				"target_id":              targetID,
				"deny_reason":            denyReason,
				"actor_org_id":           actorOrgID.String(),
				"actor_user_id":          actorUserID.String(),
				"resource_org_id":        resourceOrgID.String(),
				"resource_owner_user_id": owner,
			},
		)
	}

	cleanedAction := strings.TrimSpace(action)
	if cleanedAction == "" {
		return
	}

	cleanedTargetType := strings.TrimSpace(targetType)
	cleanedTargetID := strings.TrimSpace(targetID)
	var targetTypePtr *string
	if cleanedTargetType != "" {
		targetTypePtr = &cleanedTargetType
	}
	var targetIDPtr *string
	if cleanedTargetID != "" {
		targetIDPtr = &cleanedTargetID
	}

	meta := map[string]any{
		"result":                 "denied",
		"deny_reason":            denyReason,
		"actor_org_id":           actorOrgID.String(),
		"actor_user_id":          actorUserID.String(),
		"resource_org_id":        resourceOrgID.String(),
		"resource_owner_user_id": owner,
	}

	ip, ua := requestMetaFromContext(ctx)
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		OrgID:       &actorOrgID,
		ActorUserID: &actorUserID,
		Action:      cleanedAction,
		TargetType:  targetTypePtr,
		TargetID:    targetIDPtr,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata:    meta,
	}); err != nil {
		w.logError(traceID, "failed to write access-denied audit log", err)
	}
}

func (w *Writer) WriteAPIKeyCreated(
	ctx context.Context,
	traceID string,
	orgID uuid.UUID,
	userID uuid.UUID,
	keyID uuid.UUID,
	name string,
) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	targetType := "api_key"
	targetID := keyID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		OrgID:       &orgID,
		ActorUserID: &userID,
		Action:      "api_keys.create",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata:    map[string]any{"name": name},
	}); err != nil {
		w.logError(traceID, "failed to write api_key-created audit log", err)
	}
}

func (w *Writer) WriteAPIKeyRevoked(
	ctx context.Context,
	traceID string,
	orgID uuid.UUID,
	userID uuid.UUID,
	keyID uuid.UUID,
) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	targetType := "api_key"
	targetID := keyID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		OrgID:       &orgID,
		ActorUserID: &userID,
		Action:      "api_keys.revoke",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata:    map[string]any{},
	}); err != nil {
		w.logError(traceID, "failed to write api_key-revoked audit log", err)
	}
}

func (w *Writer) WriteAPIKeyUsed(
	ctx context.Context,
	traceID string,
	orgID uuid.UUID,
	userID uuid.UUID,
	keyID uuid.UUID,
	action string,
) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	apiKeyIDPtr := keyID
	targetType := "api_key"
	targetID := keyID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		OrgID:       &orgID,
		ActorUserID: &userID,
		Action:      action,
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		APIKeyID:    &apiKeyIDPtr,
		Metadata:    map[string]any{},
	}); err != nil {
		w.logError(traceID, "failed to write api_key-used audit log", err)
	}
}

func (w *Writer) WriteOrgInvitationCreated(
	ctx context.Context,
	traceID string,
	orgID uuid.UUID,
	actorUserID uuid.UUID,
	invitationID uuid.UUID,
	email string,
	role string,
) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	targetType := "org_invitation"
	targetID := invitationID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		OrgID:       &orgID,
		ActorUserID: &actorUserID,
		Action:      "org_invitations.create",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata:    map[string]any{"email": email, "role": role},
	}); err != nil {
		w.logError(traceID, "failed to write org_invitation-created audit log", err)
	}
}

func (w *Writer) WriteOrgInvitationAccepted(
	ctx context.Context,
	traceID string,
	orgID uuid.UUID,
	actorUserID uuid.UUID,
	invitationID uuid.UUID,
	email string,
) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	targetType := "org_invitation"
	targetID := invitationID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		OrgID:       &orgID,
		ActorUserID: &actorUserID,
		Action:      "org_invitations.accept",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata:    map[string]any{"email": email},
	}); err != nil {
		w.logError(traceID, "failed to write org_invitation-accepted audit log", err)
	}
}

func (w *Writer) WriteOrgInvitationRevoked(
	ctx context.Context,
	traceID string,
	orgID uuid.UUID,
	actorUserID uuid.UUID,
	invitationID uuid.UUID,
) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	targetType := "org_invitation"
	targetID := invitationID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		OrgID:       &orgID,
		ActorUserID: &actorUserID,
		Action:      "org_invitations.revoke",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata:    map[string]any{},
	}); err != nil {
		w.logError(traceID, "failed to write org_invitation-revoked audit log", err)
	}
}

func (w *Writer) WriteUserStatusChanged(
	ctx context.Context,
	traceID string,
	actorUserID uuid.UUID,
	targetUserID uuid.UUID,
	oldStatus string,
	newStatus string,
) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	targetType := "user"
	targetID := targetUserID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		ActorUserID: &actorUserID,
		Action:      "users.status_changed",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata: map[string]any{
			"old_status": oldStatus,
			"new_status": newStatus,
		},
	}); err != nil {
		w.logError(traceID, "failed to write user-status-changed audit log", err)
	}
}

func (w *Writer) WriteInviteCodeCreated(
	ctx context.Context,
	traceID string,
	userID uuid.UUID,
	codeID uuid.UUID,
) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	targetType := "invite_code"
	targetID := codeID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		ActorUserID: &userID,
		Action:      "invite_codes.create",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata:    map[string]any{},
	}); err != nil {
		w.logError(traceID, "failed to write invite_code-created audit log", err)
	}
}

func (w *Writer) WriteInviteCodeReset(
	ctx context.Context,
	traceID string,
	userID uuid.UUID,
	oldCodeID uuid.UUID,
	newCodeID uuid.UUID,
) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	targetType := "invite_code"
	targetID := newCodeID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		ActorUserID: &userID,
		Action:      "invite_codes.reset",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata:    map[string]any{"old_code_id": oldCodeID.String()},
	}); err != nil {
		w.logError(traceID, "failed to write invite_code-reset audit log", err)
	}
}

func (w *Writer) WriteInviteCodeUpdated(
	ctx context.Context,
	traceID string,
	actorUserID uuid.UUID,
	codeID uuid.UUID,
	changes map[string]any,
) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	targetType := "invite_code"
	targetID := codeID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		ActorUserID: &actorUserID,
		Action:      "invite_codes.update",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata:    changes,
	}); err != nil {
		w.logError(traceID, "failed to write invite_code-updated audit log", err)
	}
}

func (w *Writer) WriteReferralCreated(
	ctx context.Context,
	traceID string,
	inviterUserID uuid.UUID,
	inviteeUserID uuid.UUID,
	codeID uuid.UUID,
	referralID uuid.UUID,
) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	targetType := "referral"
	targetID := referralID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		ActorUserID: &inviteeUserID,
		Action:      "referrals.create",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata: map[string]any{
			"inviter_user_id": inviterUserID.String(),
			"invite_code_id":  codeID.String(),
		},
	}); err != nil {
		w.logError(traceID, "failed to write referral-created audit log", err)
	}
}

func (w *Writer) logError(traceID string, msg string, err error) {
	if w == nil || w.logger == nil || err == nil {
		return
	}
	w.logger.Error(msg, observability.LogFields{TraceID: &traceID}, map[string]any{"error": err.Error()})
}

// requestMetaFromContext 从 context 提取 IP 和 User-Agent 供审计写入。
func requestMetaFromContext(ctx context.Context) (ip *string, ua *string) {
	if raw := observability.ClientIPFromContext(ctx); raw != "" {
		ip = &raw
	}
	if raw := observability.UserAgentFromContext(ctx); raw != "" {
		ua = &raw
	}
	return ip, ua
}

func sha256Hex(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
