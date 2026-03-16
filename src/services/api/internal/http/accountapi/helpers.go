package accountapi

import (
	"context"
	"strconv"
	"time"

	nethttp "net/http"

	"arkloop/services/api/internal/audit"
	"arkloop/services/api/internal/auth"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/entitlement"
	httpkit "arkloop/services/api/internal/http/httpkit"

	"github.com/google/uuid"
)

type environmentStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
}

type authUser struct {
	ID              uuid.UUID
	Username        string
	Email           *string
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
}

func authenticateUser(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	authService *auth.Service,
) (*authUser, bool) {
	token, ok := httpkit.ParseBearerToken(w, r, traceID)
	if !ok {
		return nil, false
	}

	user, err := authService.AuthenticateUser(r.Context(), token)
	if err != nil {
		switch typed := err.(type) {
		case auth.TokenExpiredError:
			httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.token_expired", typed.Error(), traceID, nil)
		case auth.TokenInvalidError:
			httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.invalid_token", typed.Error(), traceID, nil)
		case auth.UserNotFoundError:
			httpkit.WriteError(w, nethttp.StatusUnauthorized, "auth.user_not_found", "user not found", traceID, nil)
		case auth.SuspendedUserError:
			httpkit.WriteError(w, nethttp.StatusForbidden, "auth.user_suspended", "account suspended", traceID, nil)
		default:
			httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		}
		return nil, false
	}

	return &authUser{
		ID:              user.ID,
		Username:        user.Username,
		Email:           user.Email,
		EmailVerifiedAt: user.EmailVerifiedAt,
		CreatedAt:       user.CreatedAt,
	}, true
}

func requireEntitlementInt(
	ctx context.Context,
	w nethttp.ResponseWriter,
	traceID string,
	entSvc *entitlement.Service,
	accountID uuid.UUID,
	key string,
	actual int64,
	errCode string,
	errMsg string,
) bool {
	if entSvc == nil {
		return true
	}

	val, err := entSvc.Resolve(ctx, accountID, key)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return false
	}

	limit, _ := strconv.ParseInt(val.Raw, 10, 64)
	if limit <= 0 {
		return true
	}
	if actual >= limit {
		httpkit.WriteError(w, nethttp.StatusForbidden, errCode, errMsg, traceID, nil)
		return false
	}
	return true
}

func authorizeRunOrAudit(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	traceID string,
	actor *httpkit.Actor,
	action string,
	run *data.Run,
	auditWriter *audit.Writer,
) bool {
	if actor == nil || run == nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return false
	}
	if actor.HasPermission(auth.PermPlatformAdmin) {
		return true
	}

	denyReason := "owner_mismatch"
	if actor.AccountID != run.AccountID {
		denyReason = "account_mismatch"
	} else if run.CreatedByUserID == nil {
		denyReason = "no_owner"
	} else if *run.CreatedByUserID == actor.UserID {
		return true
	}

	if auditWriter != nil {
		auditWriter.WriteAccessDenied(
			r.Context(),
			traceID,
			actor.AccountID,
			actor.UserID,
			action,
			"run",
			run.ID.String(),
			run.AccountID,
			run.CreatedByUserID,
			denyReason,
		)
	}

	httpkit.WriteError(w, nethttp.StatusForbidden, "policy.denied", "access denied", traceID, map[string]any{"action": action})
	return false
}
