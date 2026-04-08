package llm

import (
	"encoding/json"
	"fmt"
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

	// 架构错误契约: 5 类错误分类
	ErrorClassConfigMissing       = "config.missing"
	ErrorClassConfigInvalid       = "config.invalid"
	ErrorClassRuntimePolicyDenied = "runtime_policy.denied"
	ErrorClassProviderFailed      = "external_provider.failed"
	ErrorClassPlatformError       = "internal_platform.error"
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
	Signature     string
	CacheControl  *string // "ephemeral"（Anthropic prompt caching）
	Attachment    *messagecontent.AttachmentRef
	ExtractedText string
	Data          []byte
	TrustSource   string // "system" | "user" | "tool" | "memory" | "file" | "mcp" | ""
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
	case "thinking":
		payload := map[string]any{
			"type":     "thinking",
			"thinking": p.Text,
		}
		if strings.TrimSpace(p.Signature) != "" {
			payload["signature"] = p.Signature
		}
		return payload
	case "redacted_thinking":
		return map[string]any{
			"type": "redacted_thinking",
			"data": p.Text,
		}
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
	Role         string
	Phase        *string
	Content      []ContentPart
	ToolCalls    []ToolCall
	OutputTokens *int64 // assistant 消息的实际 output tokens，用于上下文裁剪
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
	if m.Phase != nil && strings.TrimSpace(*m.Phase) != "" {
		payload["phase"] = strings.TrimSpace(*m.Phase)
	}
	return payload
}

func MessageFromJSONMap(raw map[string]any) (Message, error) {
	role, _ := raw["role"].(string)
	role = strings.TrimSpace(role)
	if role == "" {
		return Message{}, fmt.Errorf("message missing role")
	}

	parts, err := contentPartsFromJSON(raw["content"])
	if err != nil {
		return Message{}, err
	}

	var phase *string
	if value, ok := raw["phase"].(string); ok && strings.TrimSpace(value) != "" {
		trimmed := strings.TrimSpace(value)
		phase = &trimmed
	}

	message := Message{
		Role:    role,
		Phase:   phase,
		Content: parts,
	}
	if rawCalls, ok := raw["tool_calls"].([]any); ok {
		calls := make([]ToolCall, 0, len(rawCalls))
		for idx, rawCall := range rawCalls {
			callObj, ok := rawCall.(map[string]any)
			if !ok {
				return Message{}, fmt.Errorf("message tool_calls[%d] is not an object", idx)
			}
			call, err := ToolCallFromJSONMap(callObj)
			if err != nil {
				return Message{}, err
			}
			calls = append(calls, call)
		}
		message.ToolCalls = calls
	}
	return message, nil
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

// ToolChoice controls whether the LLM must call a tool.
type ToolChoice struct {
	Mode     string // "auto" | "required" | "specific"
	ToolName string // only used when Mode="specific"
}

type Request struct {
	Model            string
	Messages         []Message
	Temperature      *float64
	MaxOutputTokens  *int
	Tools            []ToolSpec
	ToolChoice       *ToolChoice
	Metadata         map[string]any
	ExperimentalJSON map[string]any
	ReasoningMode    string // "auto" | "enabled" | "disabled" | "none" | "minimal" | "low" | "medium" | "high" | "xhigh" (accepts aliases like "off"/"max")
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
	// 上下文分解统计
	SystemBytes        int
	ToolsBytes         int
	MessagesBytes      int
	RoleBytes          map[string]int // "system"/"user"/"assistant"/"tool" -> bytes
	ToolSchemaBytesMap map[string]int // tool name -> schema bytes
	StablePrefixHash   string
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
	if r.SystemBytes > 0 {
		payload["system_bytes"] = r.SystemBytes
	}
	if r.ToolsBytes > 0 {
		payload["tools_bytes"] = r.ToolsBytes
	}
	if r.MessagesBytes > 0 {
		payload["messages_bytes"] = r.MessagesBytes
	}
	if len(r.RoleBytes) > 0 {
		payload["role_bytes"] = r.RoleBytes
	}
	if len(r.ToolSchemaBytesMap) > 0 {
		payload["tool_schema_bytes_by_name"] = r.ToolSchemaBytesMap
	}
	if r.StablePrefixHash != "" {
		payload["stable_prefix_hash"] = r.StablePrefixHash
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
	ToolCallID   string
	ToolName     string
	ResultJSON   map[string]any
	ContentParts []ContentPart // 多模态附件（图片等），由 agent loop 注入 tool result message
	Error        *GatewayError
	Usage        *Usage
	Cost         *Cost
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
	LlmCallID        string
	Usage            *Usage
	Cost             *Cost
	AssistantMessage *Message
}

func (c StreamRunCompleted) ToDataJSON() map[string]any {
	payload := map[string]any{}
	if c.LlmCallID != "" {
		payload["llm_call_id"] = c.LlmCallID
	}
	if c.Usage != nil {
		payload["usage"] = c.Usage.ToJSON()
	}
	if c.Cost != nil {
		payload["cost"] = c.Cost.ToJSON()
	}
	if c.AssistantMessage != nil {
		payload["assistant_message"] = c.AssistantMessage.ToJSON()
	}
	return payload
}

type StreamRunFailed struct {
	LlmCallID string
	Error     GatewayError
	Usage     *Usage
	Cost      *Cost
}

func (f StreamRunFailed) ToDataJSON() map[string]any {
	payload := f.Error.ToJSON()
	if f.LlmCallID != "" {
		payload["llm_call_id"] = f.LlmCallID
	}
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

// ToolCallArgumentDelta carries a single streaming chunk of tool call arguments.
// Yielded by LLM providers during streaming to enable progressive frontend rendering.
type ToolCallArgumentDelta struct {
	ToolCallIndex  int
	ToolCallID     string
	ToolName       string
	ArgumentsDelta string
}

func (d ToolCallArgumentDelta) ToDataJSON() map[string]any {
	m := map[string]any{
		"tool_call_index": d.ToolCallIndex,
		"arguments_delta": d.ArgumentsDelta,
	}
	if d.ToolCallID != "" {
		m["tool_call_id"] = d.ToolCallID
	}
	if d.ToolName != "" {
		m["tool_name"] = d.ToolName
	}
	return m
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

func VisibleContentParts(parts []ContentPart) []ContentPart {
	if len(parts) == 0 {
		return nil
	}
	out := make([]ContentPart, 0, len(parts))
	for _, part := range parts {
		switch part.Kind() {
		case messagecontent.PartTypeText, messagecontent.PartTypeImage, messagecontent.PartTypeFile:
			out = append(out, part)
		}
	}
	return out
}

func VisibleMessageText(message Message) string {
	var b strings.Builder
	for _, part := range VisibleContentParts(message.Content) {
		switch part.Kind() {
		case messagecontent.PartTypeText:
			b.WriteString(part.Text)
		case messagecontent.PartTypeFile:
			b.WriteString(PartPromptText(part))
		}
	}
	return b.String()
}

func BuildAssistantThreadContentJSON(message Message) (json.RawMessage, error) {
	visibleParts := VisibleContentParts(message.Content)
	content := messagecontent.Content{Parts: make([]messagecontent.Part, 0, len(visibleParts))}
	for _, part := range visibleParts {
		switch part.Kind() {
		case messagecontent.PartTypeText:
			content.Parts = append(content.Parts, messagecontent.Part{Type: messagecontent.PartTypeText, Text: part.Text})
		case messagecontent.PartTypeFile:
			content.Parts = append(content.Parts, messagecontent.Part{
				Type:          messagecontent.PartTypeFile,
				Attachment:    part.Attachment,
				ExtractedText: part.ExtractedText,
			})
		case messagecontent.PartTypeImage:
			content.Parts = append(content.Parts, messagecontent.Part{
				Type:       messagecontent.PartTypeImage,
				Attachment: part.Attachment,
			})
		}
	}

	payload := map[string]any{
		"parts": content.Parts,
	}
	if needsAssistantStateEnvelope(message) {
		state := map[string]any{
			"content": partsToJSON(message.Content),
		}
		if message.Phase != nil && strings.TrimSpace(*message.Phase) != "" {
			state["phase"] = strings.TrimSpace(*message.Phase)
		}
		payload["assistant_state"] = state
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// BuildIntermediateAssistantContentJSON serializes an intermediate assistant message
// that includes tool_calls into content_json for persistence.
func BuildIntermediateAssistantContentJSON(message Message, toolCalls []ToolCall) (json.RawMessage, error) {
	visibleParts := VisibleContentParts(message.Content)
	parts := make([]messagecontent.Part, 0, len(visibleParts))
	for _, part := range visibleParts {
		switch part.Kind() {
		case messagecontent.PartTypeText:
			parts = append(parts, messagecontent.Part{Type: messagecontent.PartTypeText, Text: part.Text})
		case messagecontent.PartTypeFile:
			parts = append(parts, messagecontent.Part{
				Type:          messagecontent.PartTypeFile,
				Attachment:    part.Attachment,
				ExtractedText: part.ExtractedText,
			})
		case messagecontent.PartTypeImage:
			parts = append(parts, messagecontent.Part{
				Type:       messagecontent.PartTypeImage,
				Attachment: part.Attachment,
			})
		}
	}

	payload := map[string]any{
		"parts": parts,
	}

	if len(toolCalls) > 0 {
		tcItems := make([]map[string]any, 0, len(toolCalls))
		for _, tc := range toolCalls {
			tcItems = append(tcItems, tc.ToDataJSON())
		}
		payload["tool_calls"] = tcItems
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func AssistantMessageFromThreadContentJSON(raw []byte) (*Message, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var envelope struct {
		AssistantState json.RawMessage `json:"assistant_state"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if len(envelope.AssistantState) == 0 {
		return nil, nil
	}
	var state struct {
		Phase   *string `json:"phase"`
		Content []any   `json:"content"`
	}
	if err := json.Unmarshal(envelope.AssistantState, &state); err != nil {
		return nil, err
	}
	parts, err := contentPartsFromJSON(state.Content)
	if err != nil {
		return nil, err
	}
	message := &Message{
		Role:    "assistant",
		Phase:   state.Phase,
		Content: parts,
	}
	return message, nil
}

func needsAssistantStateEnvelope(message Message) bool {
	if message.Phase != nil && strings.TrimSpace(*message.Phase) != "" {
		return true
	}
	for _, part := range message.Content {
		switch part.Kind() {
		case "thinking", "redacted_thinking":
			return true
		}
	}
	return false
}

func contentPartsFromJSON(raw any) ([]ContentPart, error) {
	if raw == nil {
		return nil, nil
	}
	rawParts, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("message content is not an array")
	}
	parts := make([]ContentPart, 0, len(rawParts))
	for idx, rawPart := range rawParts {
		partObj, ok := rawPart.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("message content[%d] is not an object", idx)
		}
		part, err := contentPartFromJSONMap(partObj)
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	return parts, nil
}

func contentPartFromJSONMap(raw map[string]any) (ContentPart, error) {
	typ, _ := raw["type"].(string)
	typ = strings.TrimSpace(typ)
	switch typ {
	case "", messagecontent.PartTypeText:
		part := ContentPart{
			Type: messagecontent.PartTypeText,
			Text: stringValue(raw["text"]),
		}
		if cacheControl, ok := raw["cache_control"].(string); ok && strings.TrimSpace(cacheControl) != "" {
			trimmed := strings.TrimSpace(cacheControl)
			part.CacheControl = &trimmed
		}
		return part, nil
	case "thinking":
		return ContentPart{
			Type:      "thinking",
			Text:      stringValue(firstNonNil(raw["thinking"], raw["text"])),
			Signature: strings.TrimSpace(stringValue(raw["signature"])),
		}, nil
	case "redacted_thinking":
		return ContentPart{
			Type: "redacted_thinking",
			Text: stringValue(firstNonNil(raw["data"], raw["text"])),
		}, nil
	case messagecontent.PartTypeImage:
		attachment, err := attachmentRefFromJSON(raw["attachment"])
		if err != nil {
			return ContentPart{}, err
		}
		return ContentPart{Type: messagecontent.PartTypeImage, Attachment: attachment}, nil
	case messagecontent.PartTypeFile:
		attachment, err := attachmentRefFromJSON(raw["attachment"])
		if err != nil {
			return ContentPart{}, err
		}
		return ContentPart{
			Type:          messagecontent.PartTypeFile,
			Attachment:    attachment,
			ExtractedText: stringValue(raw["extracted_text"]),
		}, nil
	default:
		return ContentPart{}, fmt.Errorf("unsupported content part type %q", typ)
	}
}

func attachmentRefFromJSON(raw any) (*messagecontent.AttachmentRef, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("attachment is not an object")
	}
	return &messagecontent.AttachmentRef{
		Key:      strings.TrimSpace(stringValue(obj["key"])),
		Filename: strings.TrimSpace(stringValue(obj["filename"])),
		MimeType: strings.TrimSpace(stringValue(obj["mime_type"])),
		Size:     int64(intValue(obj["size"])),
	}, nil
}

func ToolCallFromJSONMap(raw map[string]any) (ToolCall, error) {
	callID := strings.TrimSpace(stringValue(firstNonNil(raw["tool_call_id"], raw["call_id"], raw["id"])))
	toolName := strings.TrimSpace(stringValue(firstNonNil(raw["tool_name"], raw["name"])))
	if callID == "" || toolName == "" {
		return ToolCall{}, fmt.Errorf("tool call missing id or name")
	}
	args, _ := raw["arguments"].(map[string]any)
	if args == nil {
		args, _ = raw["arguments_json"].(map[string]any)
	}
	return ToolCall{
		ToolCallID:    callID,
		ToolName:      toolName,
		ArgumentsJSON: mapOrEmpty(args),
	}, nil
}

func stringValue(raw any) string {
	text, _ := raw.(string)
	return text
}

func intValue(raw any) int {
	switch value := raw.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
