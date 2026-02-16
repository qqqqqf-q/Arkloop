package llm

const (
	ErrorClassProviderRetryable    = "provider.retryable"
	ErrorClassProviderNonRetryable = "provider.non_retryable"
	ErrorClassBudgetExceeded       = "budget.exceeded"
	ErrorClassPolicyDenied         = "policy.denied"
	ErrorClassInternalError        = "internal.error"
	ErrorClassInternalStreamEnded  = "internal.stream_ended"
)

type Usage struct {
	InputTokens  *int
	OutputTokens *int
	TotalTokens  *int
}

func (u Usage) ToJSON() map[string]any {
	payload := map[string]any{}
	if u.InputTokens != nil {
		payload["input_tokens"] = *u.InputTokens
	}
	if u.OutputTokens != nil {
		payload["output_tokens"] = *u.OutputTokens
	}
	if u.TotalTokens != nil {
		payload["total_tokens"] = *u.TotalTokens
	}
	return payload
}

type Cost struct {
	Currency     string
	AmountMicros int
}

func (c Cost) ToJSON() map[string]any {
	return map[string]any{
		"currency":      c.Currency,
		"amount_micros": c.AmountMicros,
	}
}

type GatewayError struct {
	ErrorClass string
	Message    string
	Details    map[string]any
}

func (e GatewayError) ToJSON() map[string]any {
	payload := map[string]any{
		"error_class": e.ErrorClass,
		"message":     e.Message,
	}
	if len(e.Details) > 0 {
		payload["details"] = e.Details
	}
	return payload
}

type TextPart struct {
	Text string
}

func (p TextPart) ToJSON() map[string]any {
	return map[string]any{"type": "text", "text": p.Text}
}

type ToolCall struct {
	ToolCallID    string
	ToolName      string
	ArgumentsJSON map[string]any
}

func (c ToolCall) ToDataJSON() map[string]any {
	return map[string]any{
		"tool_call_id": c.ToolCallID,
		"tool_name":    c.ToolName,
		"arguments":    mapOrEmpty(c.ArgumentsJSON),
	}
}

type Message struct {
	Role      string
	Content   []TextPart
	ToolCalls []ToolCall
}

func (m Message) ToJSON() map[string]any {
	payload := map[string]any{
		"role":    m.Role,
		"content": partsToJSON(m.Content),
	}
	if len(m.ToolCalls) > 0 {
		items := make([]map[string]any, 0, len(m.ToolCalls))
		for _, call := range m.ToolCalls {
			items = append(items, call.ToDataJSON())
		}
		payload["tool_calls"] = items
	}
	return payload
}

type ToolSpec struct {
	Name        string
	Description *string
	JSONSchema  map[string]any
}

func (s ToolSpec) ToJSON() map[string]any {
	payload := map[string]any{
		"name":   s.Name,
		"schema": mapOrEmpty(s.JSONSchema),
	}
	if s.Description != nil {
		payload["description"] = *s.Description
	}
	return payload
}

type Request struct {
	Model           string
	Messages         []Message
	Temperature      *float64
	MaxOutputTokens  *int
	Tools            []ToolSpec
	Metadata         map[string]any
	ExperimentalJSON map[string]any
}

func (r Request) ToJSON() map[string]any {
	payload := map[string]any{
		"model":    r.Model,
		"messages": messagesToJSON(r.Messages),
	}
	if r.Temperature != nil {
		payload["temperature"] = *r.Temperature
	}
	if r.MaxOutputTokens != nil {
		payload["max_output_tokens"] = *r.MaxOutputTokens
	}
	if len(r.Tools) > 0 {
		tools := make([]map[string]any, 0, len(r.Tools))
		for _, spec := range r.Tools {
			tools = append(tools, spec.ToJSON())
		}
		payload["tools"] = tools
	}
	if len(r.Metadata) > 0 {
		payload["metadata"] = r.Metadata
	}
	return payload
}

type StreamMessageDelta struct {
	ContentDelta string
	Role         string
	Channel      *string
}

func (d StreamMessageDelta) ToDataJSON() map[string]any {
	payload := map[string]any{
		"content_delta": d.ContentDelta,
		"role":          d.Role,
	}
	if d.Channel != nil {
		payload["channel"] = *d.Channel
	}
	return payload
}

type StreamLlmRequest struct {
	LlmCallID     string
	ProviderKind  string
	APIMode       string
	BaseURL       *string
	Path          *string
	PayloadJSON   map[string]any
	RedactedHints map[string]any
}

func (r StreamLlmRequest) ToDataJSON() map[string]any {
	payload := map[string]any{
		"llm_call_id":    r.LlmCallID,
		"provider_kind":  r.ProviderKind,
		"api_mode":       r.APIMode,
		"payload":        mapOrEmpty(r.PayloadJSON),
		"redacted_hints": mapOrEmpty(r.RedactedHints),
	}
	if r.BaseURL != nil {
		payload["base_url"] = *r.BaseURL
	}
	if r.Path != nil {
		payload["path"] = *r.Path
	}
	return payload
}

type StreamLlmResponseChunk struct {
	LlmCallID    string
	ProviderKind string
	APIMode      string
	Raw          string
	ChunkJSON    any
	StatusCode   *int
	Truncated    bool
}

func (c StreamLlmResponseChunk) ToDataJSON() map[string]any {
	payload := map[string]any{
		"llm_call_id":   c.LlmCallID,
		"provider_kind": c.ProviderKind,
		"api_mode":      c.APIMode,
		"raw":           c.Raw,
		"truncated":     c.Truncated,
	}
	if c.ChunkJSON != nil {
		payload["json"] = c.ChunkJSON
	}
	if c.StatusCode != nil {
		payload["status_code"] = *c.StatusCode
	}
	return payload
}

type StreamToolResult struct {
	ToolCallID string
	ToolName   string
	ResultJSON map[string]any
	Error      *GatewayError
	Usage      *Usage
	Cost       *Cost
}

func (r StreamToolResult) ToDataJSON() map[string]any {
	payload := map[string]any{
		"tool_call_id": r.ToolCallID,
		"tool_name":    r.ToolName,
	}
	if r.ResultJSON != nil {
		payload["result"] = r.ResultJSON
	}
	if r.Error != nil {
		payload["error"] = r.Error.ToJSON()
	}
	if r.Usage != nil {
		payload["usage"] = r.Usage.ToJSON()
	}
	if r.Cost != nil {
		payload["cost"] = r.Cost.ToJSON()
	}
	return payload
}

type StreamProviderFallback struct {
	ProviderKind string
	FromAPIMode  string
	ToAPIMode    string
	Reason       string
	StatusCode   *int
}

func (f StreamProviderFallback) ToDataJSON() map[string]any {
	payload := map[string]any{
		"provider_kind": f.ProviderKind,
		"from_api_mode": f.FromAPIMode,
		"to_api_mode":   f.ToAPIMode,
		"reason":        f.Reason,
	}
	if f.StatusCode != nil {
		payload["status_code"] = *f.StatusCode
	}
	return payload
}

type StreamRunCompleted struct {
	Usage *Usage
	Cost  *Cost
}

func (c StreamRunCompleted) ToDataJSON() map[string]any {
	payload := map[string]any{}
	if c.Usage != nil {
		payload["usage"] = c.Usage.ToJSON()
	}
	if c.Cost != nil {
		payload["cost"] = c.Cost.ToJSON()
	}
	return payload
}

type StreamRunFailed struct {
	Error GatewayError
	Usage *Usage
	Cost  *Cost
}

func (f StreamRunFailed) ToDataJSON() map[string]any {
	payload := f.Error.ToJSON()
	if f.Usage != nil {
		payload["usage"] = f.Usage.ToJSON()
	}
	if f.Cost != nil {
		payload["cost"] = f.Cost.ToJSON()
	}
	return payload
}

func InternalStreamEndedError() GatewayError {
	return GatewayError{
		ErrorClass: ErrorClassInternalStreamEnded,
		Message:    "上游流在未结束状态时提前结束",
	}
}

func mapOrEmpty(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func partsToJSON(parts []TextPart) []map[string]any {
	if len(parts) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		out = append(out, part.ToJSON())
	}
	return out
}

func messagesToJSON(messages []Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		out = append(out, message.ToJSON())
	}
	return out
}
