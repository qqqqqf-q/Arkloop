package shell

import (
	"fmt"
	"net/http"
)

const (
	CodeSessionBusy         = "shell.session_busy"
	CodeSessionNotFound     = "shell.session_not_found"
	CodeInvalidCursor       = "shell.invalid_cursor"
	CodeNotRunning          = "shell.not_running"
	CodeSignalFailed        = "shell.signal_failed"
	CodeTimeoutTooLarge     = "shell.timeout_too_large"
	CodeAccountMismatch     = "sandbox.account_mismatch"
	CodeMaxSessionsExceeded = "shell.max_sessions_exceeded"
)

type Error struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *Error) Error() string {
	return e.Message
}

func newError(code, message string, httpStatus int) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: httpStatus}
}

func busyError() *Error {
	return newError(CodeSessionBusy, "shell session is busy", http.StatusConflict)
}

func notFoundError() *Error {
	return newError(CodeSessionNotFound, "shell session not found", http.StatusNotFound)
}

func notRunningError() *Error {
	return newError(CodeNotRunning, "shell session is not running", http.StatusConflict)
}

func timeoutTooLargeError() *Error {
	return newError(CodeTimeoutTooLarge, "timeout_ms must not exceed 300000", http.StatusBadRequest)
}

func accountMismatchError() *Error {
	return newError(CodeAccountMismatch, "session belongs to another account", http.StatusForbidden)
}

func maxSessionsExceededError(maxSessions int) *Error {
	return newError(CodeMaxSessionsExceeded, fmt.Sprintf("max shell sessions reached: %d", maxSessions), http.StatusServiceUnavailable)
}
