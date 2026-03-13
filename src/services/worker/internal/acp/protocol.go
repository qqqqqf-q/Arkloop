package acp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ACP protocol messages (JSON-RPC over stdio to opencode process).
//
// Worker writes these to stdin via sandbox /v1/acp/write:
//   - session/new, session/prompt, session/cancel
//
// OpenCode writes these to stdout, Worker reads via /v1/acp/read:
//   - session/update (various update types)

type ACPMessage struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      *int      `json:"id,omitempty"`
	Method  string    `json:"method"`
	Params  any       `json:"params,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *ACPError `json:"error,omitempty"`
}

type ACPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// session/new params
type SessionNewParams struct {
	Mode string `json:"mode"`
}

// session/new result
type SessionNewResult struct {
	SessionID string `json:"session_id"`
}

// session/prompt params
type SessionPromptParams struct {
	SessionID string `json:"session_id"`
	Prompt    string `json:"prompt"`
}

// session/cancel params
type SessionCancelParams struct {
	SessionID string `json:"session_id"`
}

// session/update params (opencode -> worker, multiple update types)
type SessionUpdateParams struct {
	SessionID string         `json:"session_id"`
	Type      string         `json:"type"`
	Status    string         `json:"status,omitempty"`
	Content   string         `json:"content,omitempty"`
	Name      string         `json:"name,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Output    string         `json:"output,omitempty"`
	Summary   string         `json:"summary,omitempty"`
	Message   string         `json:"message,omitempty"`
}

const (
	UpdateTypeStatus     = "status"
	UpdateTypeTextDelta  = "text_delta"
	UpdateTypeToolCall   = "tool_call"
	UpdateTypeToolResult = "tool_result"
	UpdateTypeComplete   = "complete"
	UpdateTypeError      = "error"

	StatusWorking = "working"
	StatusIdle    = "idle"

	SessionModeCode = "code"
)

func NewSessionNewMessage(id int, mode string) ACPMessage {
	return ACPMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "session/new",
		Params:  SessionNewParams{Mode: mode},
	}
}

func NewSessionPromptMessage(id int, sessionID, prompt string) ACPMessage {
	return ACPMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "session/prompt",
		Params:  SessionPromptParams{SessionID: sessionID, Prompt: prompt},
	}
}

func NewSessionCancelMessage(id int, sessionID string) ACPMessage {
	return ACPMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "session/cancel",
		Params:  SessionCancelParams{SessionID: sessionID},
	}
}

// MarshalMessage encodes an ACP message as JSON followed by a newline delimiter.
func MarshalMessage(msg ACPMessage) ([]byte, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal acp message: %w", err)
	}
	return append(data, '\n'), nil
}

// ParseUpdates parses newline-delimited JSON messages from stdout
// and extracts session/update params.
func ParseUpdates(data string) ([]SessionUpdateParams, error) {
	if strings.TrimSpace(data) == "" {
		return nil, nil
	}

	lines := strings.Split(data, "\n")
	var updates []SessionUpdateParams

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var msg ACPMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			return updates, fmt.Errorf("parse acp message: %w", err)
		}

		if msg.Method != "session/update" {
			continue
		}
		if msg.Params == nil {
			continue
		}

		// re-marshal params to decode into SessionUpdateParams
		raw, err := json.Marshal(msg.Params)
		if err != nil {
			return updates, fmt.Errorf("re-marshal update params: %w", err)
		}
		var p SessionUpdateParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return updates, fmt.Errorf("decode update params: %w", err)
		}
		updates = append(updates, p)
	}

	return updates, nil
}
