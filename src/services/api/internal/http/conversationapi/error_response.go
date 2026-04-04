package conversationapi

import (
	httpkit "arkloop/services/api/internal/http/httpkit"
	nethttp "net/http"
)

func writeInternalError(w nethttp.ResponseWriter, traceID string, err error) {
	httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, internalErrorDetails(err))
}
