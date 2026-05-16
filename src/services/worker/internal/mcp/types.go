package mcp

import (
	"fmt"
	"strings"
)

type Tool struct {
	Name        string
	Title       *string
	Description *string
	InputSchema map[string]any
}

type ToolCallResult struct {
	Content []map[string]any
	IsError bool
}

type TimeoutError struct {
	Message string
}

func (e TimeoutError) Error() string { return e.Message }

type DisconnectedError struct {
	Message string
}

func (e DisconnectedError) Error() string { return e.Message }

type RpcError struct {
	Code    *int
	Message string
	Data    any
}

func (e RpcError) Error() string { return e.Message }

type ProtocolError struct {
	Message string
}

func (e ProtocolError) Error() string { return e.Message }

type AuthRequiredError struct {
	ServerID   string
	StatusCode int
	Reason     string
	Cause      error
}

func (e AuthRequiredError) Error() string {
	if e.Cause != nil {
		return "mcp auth_required: " + e.Reason + ": " + e.Cause.Error()
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("mcp auth_required: %s: status %d", e.Reason, e.StatusCode)
	}
	return "mcp auth_required: " + e.Reason
}

func (e AuthRequiredError) Unwrap() error { return e.Cause }

func cloneStringMap(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]string, len(value))
	for key, item := range value {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = item
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
