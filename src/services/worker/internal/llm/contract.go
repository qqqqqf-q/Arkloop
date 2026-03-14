package llm

import (
	"strings"

	"arkloop/services/shared/messagecontent"
)

const (
	ErrorClassProviderRetryable    = "provider.retryable"
	ErrorClassProviderNonRetryable = "provider.non_retryable"
	ErrorClassBudgetExceeded       = "budget.exceeded"
	ErrorClassPolicyDenied         = "policy.denied"
	ErrorClassInternalError        = "internal.error"
	ErrorClassInternalStreamEnded  = "internal.stream_ended"
	ErrorClassRoutingNotFound      = "routing.not_found"
)

type Usage struct {
	InputTokens  *int
	OutputTokens *int
	TotalTokens  *int
	// Anthropic: cache_creation_input_tokens（1.25× input price）
	CacheCreationInputTokens *int
	// Anthropic: cache_read_input_tokens（0.10× input price）
	CacheReadInputTokens *int
	// OpenAI: prompt_tokens_details.cached_tokens（0.50× input price）
	CachedTokens *int
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
	if u.CacheCreationInputTokens != nil {
		payload["cache_creation_input_tokens"] = *u.CacheCreationInputTokens
	}
	if u.CacheReadInputTokens != nil {
		payload["cache_read_input_tokens"] = *u.CacheReadInputTokens
	}
	if u.CachedTokens != nil {
		payload["cached_tokens"] = *u.CachedTokens
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

type ContentPart struct {
	Type          string
	Text          string
	CacheControl  *string // "ephemeral"（Anthropic prompt caching）
	Attachment    *messagecontent.AttachmentRef
	ExtractedText string
	Data          []byte
}

type TextPart = ContentPart

type ImagePart = ContentPart

type FilePart = ContentPart

func (p ContentPart) Kind() string {
	trimmed := strings.TrimSpace(p.Type)
	if trimmed != "" {
		return trimmed
	}
	if p.Attachment == nil {
		return messagecontent.PartTypeText
	}
	if strings.TrimSpace(p.ExtractedText) != "" {
		return messagecontent.PartTypeFile
	}
	return messagecontent.PartTypeImage
}

func (p ContentPart) ToJSON() map[string]any {
	switch p.Kind() {
	case messagecontent.PartTypeImage:
		return map[string]any{
			"type":       messagecontent.PartTypeImage,
			"attachment": p.Attachment,
		}
	case messagecontent.PartTypeFile:
		payload := map[string]any{
			"type":           messagecontent.PartTypeFile,
			"attachment":     p.Attachment,
			"extracted_text": p.ExtractedText,
		}
		return payload
	default:
		payload := map[string]any{"type": messagecontent.PartTypeText, "text": p.Text}
		if p.CacheControl != nil {
			payload["cache_control"] = *p.CacheControl
		}
		return payload
	}
}

func PartPromptText(part ContentPart) string {
	switch part.Kind() {
	case messagecontent.PartTypeText:
		return part.Text
	case messagecontent.PartTypeFile:
		ref := messagecontent.Part{
			Type:          messagecontent.PartTypeFile,
			Attachment:    part.Attachment,
			ExtractedText: part.ExtractedText,
		}
		return messagecontent.PromptText(ref)
	default:
		return ""
	}
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
	Content   []ContentPart
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
	Model            string
	Messages         []Message
	Temperature      *float64
	MaxOutputTokens  *int
	Tools            []ToolSpec
	Metadata         map[string]any
	ExperimentalJSON map[string]any
	ReasoningMode    string // "auto" | "enabled" | "disabled" | "none"
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

type SegmentDisplay struct {
	Mode  string // "visible" | "collapsed" | "hidden"
	Label string // 前端折叠块标题
}

func (d SegmentDisplay) ToJSON() map[string]any {
	return map[string]any{
		"mode":  d.Mode,
		"label": d.Label,
	}
}

// StreamSegmentStart 通知前端：后续事件直到 StreamSegmentEnd 属于此段落。
type StreamSegmentStart struct {
	SegmentID string
	Kind      string // "thinking" | "planning_round" | "direction_check" | "tool_group"
	Display   SegmentDisplay
}

func (s StreamSegmentStart) ToDataJSON() map[string]any {
	return map[string]any{
		"segment_id": s.SegmentID,
		"kind":       s.Kind,
		"display":    s.Display.ToJSON(),
	}
}

// StreamSegmentEnd 标记段落结束。
type StreamSegmentEnd struct {
	SegmentID string
}

func (s StreamSegmentEnd) ToDataJSON() map[string]any {
	return map[string]any{
		"segment_id": s.SegmentID,
	}
}

func InternalStreamEndedError() GatewayError {
	return GatewayError{
		ErrorClass: ErrorClassInternalStreamEnded,
		Message:    "upstream stream ended prematurely without completion",
	}
}

func mapOrEmpty(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func partsToJSON(parts []ContentPart) []map[string]any {
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
