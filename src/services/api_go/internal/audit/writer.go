package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

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
