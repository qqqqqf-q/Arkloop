package acp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ACP protocol messages (JSON-RPC over stdio to opencode process).
//
// Worker writes these to stdin via sandbox /v1/acp/write:
//   - session/new, session/prompt
//   - session/cancel only after the real provider contract is calibrated
//
// OpenCode writes these to stdout, Worker reads via /v1/acp/read:
//   - session/update (various update types)
//   - permission_request may be observed, but Worker does not round-trip
//     session/permission until that contract is calibrated

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
	Mode       string `json:"mode"`
	Cwd        string `json:"cwd"`
	MCPServers []any  `json:"mcpServers"`
}

// session/new result
type SessionNewResult struct {
	SessionID string `json:"sessionId"`
}

// PromptPart is a single content element in a session/prompt message.
type PromptPart struct {
	Type string `json:"type"` // "text", "image", etc.
	Text string `json:"text,omitempty"`
}

// session/prompt params
type SessionPromptParams struct {
	SessionID string       `json:"sessionId"`
	Prompt    []PromptPart `json:"prompt"`
}

// session/cancel params
type SessionCancelParams struct {
	SessionID string `json:"sessionId"`
}

// sessionUpdateRaw is the raw params from opencode's session/update messages.
// opencode wraps the actual update in a nested "update" object.
type sessionUpdateRaw struct {
	SessionID string         `json:"sessionId"`
	Update    map[string]any `json:"update"`
}

// SessionUpdateParams is the normalized update used internally.
type SessionUpdateParams struct {
	SessionID    string         `json:"sessionId"`
	Type         string         `json:"type"`
	Status       string         `json:"status,omitempty"`
	Content      string         `json:"content,omitempty"`
	Name         string         `json:"name,omitempty"`
	Arguments    map[string]any `json:"arguments,omitempty"`
	Output       string         `json:"output,omitempty"`
	Summary      string         `json:"summary,omitempty"`
	Message      string         `json:"message,omitempty"`
	PermissionID string         `json:"permission_id,omitempty"`
	Sensitive    bool           `json:"sensitive,omitempty"`
}

const (
	UpdateTypeStatus     = "status"
	UpdateTypeTextDelta  = "text_delta"
	UpdateTypeToolCall   = "tool_call"
	UpdateTypeToolResult = "tool_result"
	UpdateTypeComplete   = "complete"
	UpdateTypeError      = "error"
	UpdateTypePermission = "permission_request"

	StatusWorking = "working"
	StatusIdle    = "idle"

	SessionModeCode = "code"
)

func NewSessionNewMessage(id int, mode string, cwd string) ACPMessage {
	return ACPMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "session/new",
		Params:  SessionNewParams{Mode: mode, Cwd: cwd, MCPServers: []any{}},
	}
}

func NewSessionPromptMessage(id int, sessionID, prompt string) ACPMessage {
	return ACPMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "session/prompt",
		Params:  SessionPromptParams{SessionID: sessionID, Prompt: []PromptPart{{Type: "text", Text: prompt}}},
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

// promptResult is the result of a session/prompt JSON-RPC response.
type promptResult struct {
	StopReason string `json:"stopReason"`
}

// ParseUpdates parses newline-delimited JSON messages from stdout
// and extracts session/update params.
// opencode wraps updates in: {"sessionId":"...","update":{"sessionUpdate":"<type>",...}}
// It also detects session/prompt JSON-RPC responses (with id + result.stopReason)
// as completion signals.
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

		// JSON-RPC response (has id, no method) = session/prompt completion
		if msg.ID != nil && msg.Method == "" && msg.Result != nil {
			raw, _ := json.Marshal(msg.Result)
			var pr promptResult
			if json.Unmarshal(raw, &pr) == nil && pr.StopReason != "" {
				updates = append(updates, SessionUpdateParams{
					Type:    UpdateTypeComplete,
					Summary: pr.StopReason,
				})
			}
			continue
		}

		if msg.Method != "session/update" {
			continue
		}
		if msg.Params == nil {
			continue
		}

		// re-marshal params to decode into raw update wrapper
		raw, err := json.Marshal(msg.Params)
		if err != nil {
			return updates, fmt.Errorf("re-marshal update params: %w", err)
		}
		var wrapper sessionUpdateRaw
		if err := json.Unmarshal(raw, &wrapper); err != nil {
			return updates, fmt.Errorf("decode update wrapper: %w", err)
		}
		if wrapper.Update == nil {
			continue
		}

		p := normalizeUpdate(wrapper.SessionID, wrapper.Update)
		updates = append(updates, p)
	}

	return updates, nil
}

// normalizeUpdate maps opencode's nested update object to our flat SessionUpdateParams.
func normalizeUpdate(sessionID string, u map[string]any) SessionUpdateParams {
	p := SessionUpdateParams{SessionID: sessionID}

	// opencode uses "sessionUpdate" as the type discriminator
	if t, ok := u["sessionUpdate"].(string); ok {
		p.Type = mapUpdateType(t)
	}

	// text delta
	if v, ok := u["textDelta"].(string); ok {
		p.Content = v
	}
	// agent_thought_chunk / agent_message_chunk: content is {type, text}
	if c, ok := u["content"].(map[string]any); ok {
		if t, ok := c["text"].(string); ok && p.Content == "" {
			p.Content = t
		}
	}
	// tool call fields
	if v, ok := u["title"].(string); ok {
		p.Name = v
	}
	if v, ok := u["toolName"].(string); ok {
		p.Name = v
	}
	if v, ok := u["toolCallId"].(string); ok {
		if p.Arguments == nil {
			p.Arguments = make(map[string]any)
		}
		p.Arguments["tool_call_id"] = v
	}
	if v, ok := u["args"].(map[string]any); ok {
		p.Arguments = v
	}
	if v, ok := u["rawInput"].(map[string]any); ok && p.Arguments == nil {
		p.Arguments = v
	}
	// tool result / tool_call_update with completed status
	if v, ok := u["result"].(string); ok {
		p.Output = v
	}
	// status
	if v, ok := u["status"].(string); ok {
		p.Status = v
	}
	// error message
	if v, ok := u["message"].(string); ok {
		p.Message = v
	}
	// summary (complete)
	if v, ok := u["summary"].(string); ok {
		p.Summary = v
	}
	// permission fields
	if v, ok := u["permissionId"].(string); ok {
		p.PermissionID = v
	}
	if v, ok := u["sensitive"].(bool); ok {
		p.Sensitive = v
	}

	return p
}

// mapUpdateType translates opencode sessionUpdate type names to internal constants.
func mapUpdateType(t string) string {
	switch t {
	case "text_delta", "textDelta":
		return UpdateTypeTextDelta
	case "agent_thought_chunk", "agent_message_chunk":
		return UpdateTypeTextDelta
	case "tool_call", "toolCall":
		return UpdateTypeToolCall
	case "tool_call_update":
		return UpdateTypeToolCall
	case "tool_result", "toolResult":
		return UpdateTypeToolResult
	case "status":
		return UpdateTypeStatus
	case "complete", "session_complete":
		return UpdateTypeComplete
	case "error":
		return UpdateTypeError
	case "permission_request", "permissionRequest":
		return UpdateTypePermission
	default:
		return t // pass through unknown types
	}
}
