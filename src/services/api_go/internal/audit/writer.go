package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"arkloop/services/api_go/internal/data"
	"arkloop/services/api_go/internal/observability"

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

	loginHash := sha256Hex(login)
	targetType := "user_login"
	targetID := loginHash
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		Action:     "auth.login",
		TargetType: &targetType,
		TargetID:   &targetID,
		TraceID:    traceID,
		Metadata: map[string]any{
			"result":     "failed",
			"method":     "password",
			"login_hash": loginHash,
		},
	}); err != nil {
		w.logError(traceID, "写入登录失败审计失败", err)
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
			w.logError(traceID, "读取默认组织失败", err)
		} else if membership != nil {
			orgID = &membership.OrgID
		}
	}

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
		Metadata: map[string]any{
			"result":     "succeeded",
			"method":     "password",
			"login_hash": loginHash,
		},
	}); err != nil {
		w.logError(traceID, "写入登录成功审计失败", err)
	}
}

func (w *Writer) WriteTokenRefreshed(ctx context.Context, traceID string, userID uuid.UUID) {
	if w == nil || w.auditRepo == nil {
		return
	}

	targetType := "user"
	targetID := userID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		ActorUserID: &userID,
		Action:      "auth.refresh",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		Metadata:    map[string]any{},
	}); err != nil {
		w.logError(traceID, "写入 refresh 审计失败", err)
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
			w.logError(traceID, "读取默认组织失败", err)
		} else if membership != nil {
			orgID = &membership.OrgID
		}
	}

	targetType := "user"
	targetID := userID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		OrgID:       orgID,
		ActorUserID: &userID,
		Action:      "auth.logout",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		Metadata:    map[string]any{},
	}); err != nil {
		w.logError(traceID, "写入 logout 审计失败", err)
	}
}

func (w *Writer) WriteUserRegistered(ctx context.Context, traceID string, userID uuid.UUID, login string) {
	if w == nil || w.auditRepo == nil {
		return
	}

	loginHash := sha256Hex(login)
	targetType := "user"
	targetID := userID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		ActorUserID: &userID,
		Action:      "auth.register",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		Metadata: map[string]any{
			"login_hash": loginHash,
		},
	}); err != nil {
		w.logError(traceID, "写入 register 审计失败", err)
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

	targetType := "run"
	targetID := runID.String()
	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		OrgID:       &actorOrgID,
		ActorUserID: &actorUserID,
		Action:      "runs.cancel",
		TargetType:  &targetType,
		TargetID:    &targetID,
		TraceID:     traceID,
		Metadata: map[string]any{
			"result": "requested",
		},
	}); err != nil {
		w.logError(traceID, "写入 cancel 审计失败", err)
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
			"访问拒绝",
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

	if err := w.auditRepo.Create(ctx, data.AuditLogCreateParams{
		OrgID:       &actorOrgID,
		ActorUserID: &actorUserID,
		Action:      cleanedAction,
		TargetType:  targetTypePtr,
		TargetID:    targetIDPtr,
		TraceID:     traceID,
		Metadata:    meta,
	}); err != nil {
		w.logError(traceID, "写入访问拒绝审计失败", err)
	}
}

func (w *Writer) logError(traceID string, msg string, err error) {
	if w == nil || w.logger == nil || err == nil {
		return
	}
	w.logger.Error(msg, observability.LogFields{TraceID: &traceID}, map[string]any{"error": err.Error()})
}

func sha256Hex(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
