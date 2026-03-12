package http

import (
	"context"
	"strconv"

	nethttp "net/http"

	"arkloop/services/api/internal/entitlement"

	"github.com/google/uuid"
)

// requireEntitlementInt 解析整型权益值，当 actual >= limit 时写入错误响应并返回 false。
// entSvc 为 nil 时直接返回 true（fail-open）。
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
		WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return false
	}

	limit, _ := strconv.ParseInt(val.Raw, 10, 64)
	if limit <= 0 {
		// 0 或负数视为不限制
		return true
	}

	if actual >= limit {
		WriteError(w, nethttp.StatusForbidden, errCode, errMsg, traceID, nil)
		return false
	}

	return true
}
