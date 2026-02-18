package http

import (
	"encoding/json"
	"fmt"

	nethttp "net/http"

	"arkloop/services/api/internal/observability"
)

type ErrorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
	Details any    `json:"details,omitempty"`
}

func WriteError(w nethttp.ResponseWriter, statusCode int, code string, message string, traceID string, details any) {
	if traceID == "" {
		traceID = observability.NewTraceID()
	}

	envelope := ErrorEnvelope{
		Code:    code,
		Message: message,
		TraceID: traceID,
		Details: details,
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		payload = []byte(fmt.Sprintf(`{"code":"internal_error","message":"marshal 失败","trace_id":"%s"}`, traceID))
		statusCode = nethttp.StatusInternalServerError
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set(observability.TraceIDHeader, traceID)
	w.WriteHeader(statusCode)
	_, _ = w.Write(payload)
}
