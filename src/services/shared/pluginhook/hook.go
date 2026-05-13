package pluginhook

import (
	"encoding/json"
	"fmt"
	"strings"
)

type HookEvent string

const (
	EventBeforeToolUse HookEvent = "BeforeToolUse"
	EventAfterToolUse  HookEvent = "AfterToolUse"
	EventBeforeModel   HookEvent = "BeforeModel"
	EventAfterModel    HookEvent = "AfterModel"
	EventSessionStart  HookEvent = "SessionStart"
	EventSessionEnd    HookEvent = "SessionEnd"

	EventBeforeRun  HookEvent = EventSessionStart
	EventAfterRun   HookEvent = EventSessionEnd
	EventBeforeTool HookEvent = EventBeforeToolUse
	EventAfterTool  HookEvent = EventAfterToolUse
)

type HookType string

const (
	HookTypeCommand HookType = "command"
	HookTypeHTTP    HookType = "http"
)

type HookAction string

const (
	ActionContinue HookAction = "continue"
	ActionDeny     HookAction = "deny"
	ActionModify   HookAction = "modify"
)

type HookConfig struct {
	PluginID   string            `json:"plugin_id"`
	PluginData string            `json:"plugin_data"`
	Event      HookEvent         `json:"event"`
	Type       HookType          `json:"type"`
	Command    []string          `json:"command,omitempty"`
	Args       []string          `json:"args,omitempty"`
	URL        string            `json:"url,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	TimeoutMS  int               `json:"timeout_ms,omitempty"`
}

type HookInput struct {
	Event    HookEvent      `json:"event"`
	PluginID string         `json:"plugin_id"`
	RunID    string         `json:"run_id"`
	Payload  map[string]any `json:"payload,omitempty"`
}

func (i HookInput) MarshalJSON() ([]byte, error) {
	out := make(map[string]any, len(i.Payload)+3)
	for key, value := range i.Payload {
		key = strings.TrimSpace(key)
		if key != "" {
			out[key] = value
		}
	}
	if i.Event != "" {
		out["event"] = i.Event
	}
	if i.PluginID != "" {
		out["plugin_id"] = i.PluginID
	}
	if i.RunID != "" {
		out["run_id"] = i.RunID
	}
	return json.Marshal(out)
}

type PromptSegment struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

type HookOutput struct {
	Action         HookAction       `json:"action"`
	Reason         string           `json:"reason,omitempty"`
	Message        string           `json:"message,omitempty"`
	ModifiedArgs   *json.RawMessage `json:"args,omitempty"`
	Modified       *json.RawMessage `json:"modified,omitempty"`
	InjectSegments []PromptSegment  `json:"inject_segments,omitempty"`
	Error          string           `json:"error,omitempty"`
	Extra          map[string]any   `json:"-"`
	raw            map[string]any
}

func (o *HookOutput) UnmarshalJSON(data []byte) error {
	type hookOutputAlias HookOutput
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var alias hookOutputAlias
	if value, ok := raw["action"]; ok {
		if err := json.Unmarshal(value, &alias.Action); err != nil {
			return err
		}
	}
	if value, ok := raw["reason"]; ok {
		_ = json.Unmarshal(value, &alias.Reason)
	}
	if value, ok := raw["message"]; ok {
		_ = json.Unmarshal(value, &alias.Message)
	}
	if value, ok := raw["error"]; ok {
		_ = json.Unmarshal(value, &alias.Error)
	}
	if value, ok := raw["args"]; ok {
		copied := append(json.RawMessage(nil), value...)
		alias.ModifiedArgs = &copied
	}
	if value, ok := raw["modified"]; ok {
		copied := append(json.RawMessage(nil), value...)
		alias.Modified = &copied
	}
	if value, ok := raw["inject_segments"]; ok {
		segments, err := decodePromptSegments(value)
		if err != nil {
			return err
		}
		alias.InjectSegments = segments
	}
	*o = HookOutput(alias)
	return nil
}

func decodePromptSegments(data []byte) ([]PromptSegment, error) {
	var objectSegments []PromptSegment
	if err := json.Unmarshal(data, &objectSegments); err == nil {
		return objectSegments, nil
	}
	var stringSegments []string
	if err := json.Unmarshal(data, &stringSegments); err != nil {
		return nil, err
	}
	out := make([]PromptSegment, 0, len(stringSegments))
	for _, segment := range stringSegments {
		out = append(out, PromptSegment{Role: "system", Content: segment})
	}
	return out, nil
}

func (o HookOutput) validate(event HookEvent) error {
	event = NormalizeEvent(event)
	switch o.Action {
	case "", ActionContinue:
		o.Action = ActionContinue
	case ActionDeny, ActionModify:
	default:
		return fmt.Errorf("plugin hook action %q is invalid", o.Action)
	}
	if len(o.InjectSegments) > 0 && event != EventBeforeModel {
		return fmt.Errorf("plugin hook inject_segments is only valid before_model")
	}
	if o.Action == ActionModify && event != EventBeforeToolUse {
		return fmt.Errorf("plugin hook modify is only valid before_tool_use")
	}
	return nil
}

func NormalizeEvent(event HookEvent) HookEvent {
	text := strings.TrimSpace(string(event))
	if text == "" {
		return ""
	}
	var out strings.Builder
	for i, r := range text {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				out.WriteByte('_')
			}
			out.WriteRune(r + ('a' - 'A'))
			continue
		}
		switch r {
		case '-', '.', ' ':
			out.WriteByte('_')
		default:
			out.WriteRune(r)
		}
	}
	switch strings.ToLower(strings.Trim(out.String(), "_")) {
	case "before_tool_use", "before_tool":
		return EventBeforeToolUse
	case "after_tool_use", "after_tool":
		return EventAfterToolUse
	case "before_model", "before_model_call":
		return EventBeforeModel
	case "after_model", "after_model_response":
		return EventAfterModel
	case "session_start", "before_run":
		return EventSessionStart
	case "session_end", "after_run":
		return EventSessionEnd
	default:
		return HookEvent(text)
	}
}

func continueWithError(message string) HookOutput {
	return HookOutput{Action: ActionContinue, Error: message}
}
