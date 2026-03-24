package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/google/uuid"
)

type Writer struct {
	auditRepo      *data.AuditLogRepository
	membershipRepo *data.AccountMembershipRepository
	logger         *slog.Logger
}

func NewWriter(
	auditRepo *data.AuditLogRepository,
	membershipRepo *data.AccountMembershipRepository,
	logger *slog.Logger,
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
		Action:     "auth.login",
		TargetType: &targetType,
		TargetID:   &targetID,
		TraceID:    traceID,
		IPAddress:  ip,
		UserAgent:  ua,
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

	var accountID *uuid.UUID
	if w.membershipRepo != nil {
		membership, err := w.membershipRepo.GetDefaultForUser(ctx, userID)
		if err != nil {
			w.logError(traceID, "failed to read default account", err)
		} else if membership != nil {
			accountID = &membership.AccountID
		}
	}

	ip, ua := requestMetaFromContext(ctx)
	loginHash := sha256Hex(login)
	targetType := "user"
	targetID := userID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		AccountID:       accountID,
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

	var accountID *uuid.UUID
	if w.membershipRepo != nil {
		membership, err := w.membershipRepo.GetDefaultForUser(ctx, userID)
		if err != nil {
			w.logError(traceID, "failed to read default account", err)
		} else if membership != nil {
			accountID = &membership.AccountID
		}
	}

	ip, ua := requestMetaFromContext(ctx)
	targetType := "user"
	targetID := userID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		AccountID:       accountID,
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

func (w *Writer) WriteAuthResolved(ctx context.Context, traceID string, identity string, nextStep string) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	identityHash := sha256Hex(identity)
	targetType := "user_login"
	targetID := identityHash
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		Action:     "auth.resolve",
		TargetType: &targetType,
		TargetID:   &targetID,
		TraceID:    traceID,
		IPAddress:  ip,
		UserAgent:  ua,
		Metadata: map[string]any{
			"identity_hash": identityHash,
			"next_step":     strings.TrimSpace(nextStep),
		},
	}); err != nil {
		w.logError(traceID, "failed to write auth-resolve audit log", err)
	}
}

func (w *Writer) WriteLoginOTPSent(ctx context.Context, traceID string, userID uuid.UUID, email string) {
	if w == nil || w.auditRepo == nil {
		return
	}

	var accountID *uuid.UUID
	if w.membershipRepo != nil {
		membership, err := w.membershipRepo.GetDefaultForUser(ctx, userID)
		if err != nil {
			w.logError(traceID, "failed to read default account", err)
		} else if membership != nil {
			accountID = &membership.AccountID
		}
	}

	ip, ua := requestMetaFromContext(ctx)
	targetType := "user"
	targetID := userID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		AccountID:       accountID,
		ActorUserID: &userID,
		Action:      "auth.login_otp_send",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata: map[string]any{
			"result":     "sent",
			"method":     "email_otp",
			"login_hash": sha256Hex(email),
		},
	}); err != nil {
		w.logError(traceID, "failed to write login-otp-send audit log", err)
	}
}

func (w *Writer) WriteRunCancelRequested(
	ctx context.Context,
	traceID string,
	actorAccountID uuid.UUID,
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
		AccountID:       &actorAccountID,
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
	actorAccountID uuid.UUID,
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
		AccountID:       &actorAccountID,
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
	actorAccountID uuid.UUID,
	actorUserID uuid.UUID,
	action string,
	targetType string,
	targetID string,
	resourceAccountID uuid.UUID,
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

	accountID := actorAccountID.String()
	if w.logger != nil {
		w.logger.Info(
			"access denied",
			"trace_id", traceID,
			"account_id", accountID,
			"action", action,
			"target_type", targetType,
			"target_id", targetID,
			"deny_reason", denyReason,
			"actor_account_id", actorAccountID.String(),
			"actor_user_id", actorUserID.String(),
			"resource_account_id", resourceAccountID.String(),
			"resource_owner_user_id", owner,
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
		"actor_account_id":           actorAccountID.String(),
		"actor_user_id":          actorUserID.String(),
		"resource_account_id":        resourceAccountID.String(),
		"resource_owner_user_id": owner,
	}

	ip, ua := requestMetaFromContext(ctx)
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		AccountID:       &actorAccountID,
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
	accountID uuid.UUID,
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
		AccountID:       &accountID,
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
	accountID uuid.UUID,
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
		AccountID:       &accountID,
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
	accountID uuid.UUID,
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
		AccountID:       &accountID,
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

func (w *Writer) WriteAccountInvitationCreated(
	ctx context.Context,
	traceID string,
	accountID uuid.UUID,
	actorUserID uuid.UUID,
	invitationID uuid.UUID,
	email string,
	role string,
) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	targetType := "account_invitation"
	targetID := invitationID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		AccountID:       &accountID,
		ActorUserID: &actorUserID,
		Action:      "account_invitations.create",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata:    map[string]any{"email": email, "role": role},
	}); err != nil {
		w.logError(traceID, "failed to write account_invitation-created audit log", err)
	}
}

func (w *Writer) WriteAccountInvitationAccepted(
	ctx context.Context,
	traceID string,
	accountID uuid.UUID,
	actorUserID uuid.UUID,
	invitationID uuid.UUID,
	email string,
) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	targetType := "account_invitation"
	targetID := invitationID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		AccountID:       &accountID,
		ActorUserID: &actorUserID,
		Action:      "account_invitations.accept",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata:    map[string]any{"email": email},
	}); err != nil {
		w.logError(traceID, "failed to write account_invitation-accepted audit log", err)
	}
}

func (w *Writer) WriteAccountInvitationRevoked(
	ctx context.Context,
	traceID string,
	accountID uuid.UUID,
	actorUserID uuid.UUID,
	invitationID uuid.UUID,
) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	targetType := "account_invitation"
	targetID := invitationID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		AccountID:       &accountID,
		ActorUserID: &actorUserID,
		Action:      "account_invitations.revoke",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata:    map[string]any{},
	}); err != nil {
		w.logError(traceID, "failed to write account_invitation-revoked audit log", err)
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

func (w *Writer) WriteRedemptionCodeBatchCreated(
	ctx context.Context,
	traceID string,
	actorUserID uuid.UUID,
	batchID string,
	count int,
	codeType string,
) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	targetType := "redemption_code_batch"
	targetID := batchID
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		ActorUserID: &actorUserID,
		Action:      "redemption_codes.batch_create",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata: map[string]any{
			"count": count,
			"type":  codeType,
		},
	}); err != nil {
		w.logError(traceID, "failed to write redemption_code-batch-created audit log", err)
	}
}

func (w *Writer) WriteRedemptionCodeRedeemed(
	ctx context.Context,
	traceID string,
	accountID uuid.UUID,
	userID uuid.UUID,
	codeID uuid.UUID,
	codeType string,
	value string,
) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	targetType := "redemption_code"
	targetID := codeID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		AccountID:       &accountID,
		ActorUserID: &userID,
		Action:      "redemption_codes.redeem",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata: map[string]any{
			"type":  codeType,
			"value": value,
		},
	}); err != nil {
		w.logError(traceID, "failed to write redemption_code-redeemed audit log", err)
	}
}

func (w *Writer) WriteBroadcastCreated(
	ctx context.Context,
	traceID string,
	userID uuid.UUID,
	broadcastID uuid.UUID,
	targetType string,
	targetID *uuid.UUID,
) {
	if w == nil || w.auditRepo == nil {
		return
	}

	ip, ua := requestMetaFromContext(ctx)
	tType := "notification_broadcast"
	tID := broadcastID.String()
	meta := map[string]any{
		"target_type": targetType,
	}
	if targetID != nil {
		meta["target_id"] = targetID.String()
	}

	var accountID *uuid.UUID
	if w.membershipRepo != nil {
		membership, err := w.membershipRepo.GetDefaultForUser(ctx, userID)
		if err != nil {
			w.logError(traceID, "failed to read default account for broadcast audit", err)
		} else if membership != nil {
			accountID = &membership.AccountID
		}
	}
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		AccountID:       accountID,
		ActorUserID: &userID,
		Action:      "notifications.broadcast_created",
		TargetType:  &tType,
		TargetID:    &tID,
		TraceID:     traceID,
		IPAddress:   ip,
		UserAgent:   ua,
		Metadata:    meta,
	}); err != nil {
		w.logError(traceID, "failed to write broadcast-created audit log", err)
	}
}

func (w *Writer) WriteCreditsAdjusted(
	ctx context.Context,
	traceID string,
	actorUserID uuid.UUID,
	targetAccountID uuid.UUID,
	amount int64,
	note string,
	beforeState any,
	afterState any,
) {
	targetType := "account"
	targetID := targetAccountID.String()
	w.writeStateChange(ctx, traceID, &targetAccountID, &actorUserID, "credits.adjust", &targetType, &targetID, map[string]any{
		"amount":           amount,
		"note":             note,
		"transaction_type": "admin_adjustment",
	}, beforeState, afterState, "failed to write credits-adjusted audit log")
}

func (w *Writer) WriteCreditsBulkAdjusted(
	ctx context.Context,
	traceID string,
	actorUserID uuid.UUID,
	amount int64,
	note string,
	affectedCount int64,
) {
	targetType := "credits_batch"
	w.writeStateChange(ctx, traceID, nil, &actorUserID, "credits.bulk_adjust", &targetType, nil, map[string]any{
		"amount":         amount,
		"note":           note,
		"affected_count": affectedCount,
	}, nil, nil, "failed to write credits-bulk-adjust audit log")
}

func (w *Writer) WriteCreditsResetAll(
	ctx context.Context,
	traceID string,
	actorUserID uuid.UUID,
	note string,
	affectedCount int64,
) {
	targetType := "credits_batch"
	w.writeStateChange(ctx, traceID, nil, &actorUserID, "credits.reset_all", &targetType, nil, map[string]any{
		"note":           note,
		"affected_count": affectedCount,
		"operation":      "reset_all",
	}, nil, nil, "failed to write credits-reset-all audit log")
}

func (w *Writer) WriteEntitlementOverrideSet(
	ctx context.Context,
	traceID string,
	actorUserID uuid.UUID,
	targetAccountID uuid.UUID,
	overrideID uuid.UUID,
	key string,
	beforeState any,
	afterState any,
) {
	targetType := "entitlement_override"
	targetID := overrideID.String()
	w.writeStateChange(ctx, traceID, &targetAccountID, &actorUserID, "entitlements.override_set", &targetType, &targetID, map[string]any{
		"key": key,
	}, beforeState, afterState, "failed to write entitlement-override-set audit log")
}

func (w *Writer) WriteEntitlementOverrideDeleted(
	ctx context.Context,
	traceID string,
	actorUserID uuid.UUID,
	targetAccountID uuid.UUID,
	overrideID uuid.UUID,
	key string,
	beforeState any,
) {
	targetType := "entitlement_override"
	targetID := overrideID.String()
	w.writeStateChange(ctx, traceID, &targetAccountID, &actorUserID, "entitlements.override_delete", &targetType, &targetID, map[string]any{
		"key": key,
	}, beforeState, nil, "failed to write entitlement-override-delete audit log")
}

func (w *Writer) WriteFeatureFlagCreated(
	ctx context.Context,
	traceID string,
	actorUserID uuid.UUID,
	flagID uuid.UUID,
	key string,
	afterState any,
) {
	targetType := "feature_flag"
	targetID := flagID.String()
	w.writeStateChange(ctx, traceID, nil, &actorUserID, "feature_flags.create", &targetType, &targetID, map[string]any{
		"key": key,
	}, nil, afterState, "failed to write feature-flag-created audit log")
}

func (w *Writer) WriteFeatureFlagUpdated(
	ctx context.Context,
	traceID string,
	actorUserID uuid.UUID,
	flagID uuid.UUID,
	key string,
	beforeState any,
	afterState any,
) {
	targetType := "feature_flag"
	targetID := flagID.String()
	w.writeStateChange(ctx, traceID, nil, &actorUserID, "feature_flags.update", &targetType, &targetID, map[string]any{
		"key": key,
	}, beforeState, afterState, "failed to write feature-flag-updated audit log")
}

func (w *Writer) WriteFeatureFlagDeleted(
	ctx context.Context,
	traceID string,
	actorUserID uuid.UUID,
	flagID uuid.UUID,
	key string,
	beforeState any,
) {
	targetType := "feature_flag"
	targetID := flagID.String()
	w.writeStateChange(ctx, traceID, nil, &actorUserID, "feature_flags.delete", &targetType, &targetID, map[string]any{
		"key": key,
	}, beforeState, nil, "failed to write feature-flag-deleted audit log")
}

func (w *Writer) WriteFeatureFlagAccountOverrideSet(
	ctx context.Context,
	traceID string,
	actorUserID uuid.UUID,
	targetAccountID uuid.UUID,
	flagKey string,
	beforeState any,
	afterState any,
) {
	targetType := "feature_flag_account_override"
	targetID := targetAccountID.String() + ":" + flagKey
	w.writeStateChange(ctx, traceID, &targetAccountID, &actorUserID, "feature_flags.account_override_set", &targetType, &targetID, map[string]any{
		"key": flagKey,
	}, beforeState, afterState, "failed to write feature-flag-account-override-set audit log")
}

func (w *Writer) WriteFeatureFlagAccountOverrideDeleted(
	ctx context.Context,
	traceID string,
	actorUserID uuid.UUID,
	targetAccountID uuid.UUID,
	flagKey string,
	beforeState any,
) {
	targetType := "feature_flag_account_override"
	targetID := targetAccountID.String() + ":" + flagKey
	w.writeStateChange(ctx, traceID, &targetAccountID, &actorUserID, "feature_flags.account_override_delete", &targetType, &targetID, map[string]any{
		"key": flagKey,
	}, beforeState, nil, "failed to write feature-flag-account-override-delete audit log")
}

func (w *Writer) writeStateChange(
	ctx context.Context,
	traceID string,
	accountID *uuid.UUID,
	actorUserID *uuid.UUID,
	action string,
	targetType *string,
	targetID *string,
	metadata map[string]any,
	beforeState any,
	afterState any,
	msg string,
) {
	if w == nil || w.auditRepo == nil {
		return
	}
	if metadata == nil {
		metadata = map[string]any{}
	}

	ip, ua := requestMetaFromContext(ctx)
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		AccountID:           accountID,
		ActorUserID:     actorUserID,
		Action:          action,
		TargetType:      targetType,
		TargetID:        targetID,
		TraceID:         traceID,
		IPAddress:       ip,
		UserAgent:       ua,
		Metadata:        metadata,
		BeforeStateJSON: beforeState,
		AfterStateJSON:  afterState,
	}); err != nil {
		w.logError(traceID, msg, err)
	}
}

func (w *Writer) logError(traceID string, msg string, err error) {
	if w == nil || w.logger == nil || err == nil {
		return
	}
	w.logger.Error(msg, "trace_id", traceID, "error", err.Error())
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
