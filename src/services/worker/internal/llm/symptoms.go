package llm

import "strings"

// SymptomID 表示 provider 错误响应规范化后的症状标识。
// 任何下游决策（quirk 选择、错误分类、oversize 判定）都应基于 symptom，
// 而不是各自再去解析 provider 的原始字段。
type SymptomID string

const (
	SymptomContextLengthExceeded     SymptomID = "context_length_exceeded"
	SymptomReasoningContentPassback  SymptomID = "reasoning_content_must_passback"
	SymptomXHighReasoningUnsupported SymptomID = "xhigh_reasoning_unsupported"
	SymptomToolChoiceUnsupported     SymptomID = "tool_choice_unsupported"
	SymptomUnsignedThinking          SymptomID = "unsigned_thinking"
	SymptomTempMustBeOneOnThinking   SymptomID = "temp_must_be_one_on_thinking"
	SymptomEmptyTextOnThinking       SymptomID = "empty_text_on_thinking"
	SymptomCacheControlRejected      SymptomID = "cache_control_rejected"
)

// SymptomMatchContext 是 detector 的输入，包含从 provider 响应中已抽取的字段。
type SymptomMatchContext struct {
	Status  int
	RawBody string
	Details map[string]any
}

// Symptom 描述一个症状的识别规则。
type Symptom struct {
	ID    SymptomID
	Match func(ctx SymptomMatchContext) bool
}

// DetectSymptoms 遍历注册表，返回命中的 symptom 集合（保持注册顺序、不重复）。
func DetectSymptoms(ctx SymptomMatchContext, registry []Symptom) []SymptomID {
	if len(registry) == 0 {
		return nil
	}
	var hit []SymptomID
	for _, s := range registry {
		if s.Match == nil {
			continue
		}
		if s.Match(ctx) {
			hit = append(hit, s.ID)
		}
	}
	return hit
}

// MergeSymptomsIntoDetails 把检测到的 symptom 写入 details["symptoms"]（[]string 形式）。
// 已存在的 symptom 会合并去重，便于多次解析路径累加。
func MergeSymptomsIntoDetails(details map[string]any, symptoms []SymptomID) map[string]any {
	if len(symptoms) == 0 {
		return details
	}
	if details == nil {
		details = map[string]any{}
	}
	seen := map[string]struct{}{}
	var out []string
	if existing, ok := details["symptoms"].([]string); ok {
		for _, s := range existing {
			if _, dup := seen[s]; dup {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	} else if existingAny, ok := details["symptoms"].([]any); ok {
		for _, item := range existingAny {
			s, _ := item.(string)
			if s == "" {
				continue
			}
			if _, dup := seen[s]; dup {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	for _, sym := range symptoms {
		s := string(sym)
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	details["symptoms"] = out
	return details
}

// DetailsHaveSymptom 判断 details["symptoms"] 中是否包含目标 symptom。
func DetailsHaveSymptom(details map[string]any, want SymptomID) bool {
	if details == nil {
		return false
	}
	target := string(want)
	switch list := details["symptoms"].(type) {
	case []string:
		for _, s := range list {
			if s == target {
				return true
			}
		}
	case []SymptomID:
		for _, s := range list {
			if s == want {
				return true
			}
		}
	case []any:
		for _, item := range list {
			s, _ := item.(string)
			if s == target {
				return true
			}
		}
	}
	return false
}

func detailString(details map[string]any, key string) string {
	if details == nil {
		return ""
	}
	v, _ := details[key].(string)
	return v
}

// contextLengthTextHints 是各家 provider 在 message/body 文本中提示
// 上下文超长的典型片段，全部以小写形式存放。
var contextLengthTextHints = []string{
	"context length",
	"context window",
	"prompt is too long",
	"input exceeds",
	"maximum context length",
	"too many tokens",
}

func bodyContainsContextLengthHint(rawBody string) bool {
	if rawBody == "" {
		return false
	}
	lower := strings.ToLower(rawBody)
	for _, hint := range contextLengthTextHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

// openAISymptoms 注册了 OpenAI 协议家族（chat completions / responses / codex）
// 共享的 symptom 识别规则。
var openAISymptoms = []Symptom{
	{
		ID: SymptomContextLengthExceeded,
		Match: func(c SymptomMatchContext) bool {
			if detailString(c.Details, "openai_error_code") == "context_length_exceeded" {
				return true
			}
			if detailString(c.Details, "openai_error_type") == "context_length_exceeded" {
				return true
			}
			if strings.Contains(c.RawBody, `"code":"context_length_exceeded"`) ||
				strings.Contains(c.RawBody, `"type":"context_length_exceeded"`) {
				return true
			}
			return bodyContainsContextLengthHint(c.RawBody)
		},
	},
	{
		ID: SymptomReasoningContentPassback,
		Match: func(c SymptomMatchContext) bool {
			if c.Status != 400 {
				return false
			}
			lower := strings.ToLower(c.RawBody)
			if !strings.Contains(lower, "reasoning_content") {
				return false
			}
			if strings.Contains(lower, "passed back") {
				return true
			}
			return strings.Contains(lower, "reasoning_content is missing") &&
				strings.Contains(lower, "thinking is enabled")
		},
	},
	{
		ID: SymptomXHighReasoningUnsupported,
		Match: func(c SymptomMatchContext) bool {
			if c.Status != 400 {
				return false
			}
			lower := strings.ToLower(c.RawBody)
			if !strings.Contains(lower, "reasoning_effort") || !hasLowerAlphaToken(lower, "xhigh") {
				return false
			}
			if !hasLowerAlphaToken(lower, "low") || !hasLowerAlphaToken(lower, "medium") || !hasLowerAlphaToken(lower, "high") {
				return false
			}
			return strings.Contains(lower, "expected") ||
				strings.Contains(lower, "input should be") ||
				strings.Contains(lower, "literal_error")
		},
	},
	{
		ID: SymptomToolChoiceUnsupported,
		Match: func(c SymptomMatchContext) bool {
			if c.Status != 400 {
				return false
			}
			lower := strings.ToLower(c.RawBody)
			if !strings.Contains(lower, "tool_choice") {
				return false
			}
			return strings.Contains(lower, "does not support") ||
				strings.Contains(lower, "unsupported")
		},
	},
}

// anthropicSymptoms 注册 Anthropic Messages API 的 symptom 规则。
var anthropicSymptoms = []Symptom{
	{
		ID: SymptomContextLengthExceeded,
		Match: func(c SymptomMatchContext) bool {
			if detailString(c.Details, "anthropic_error_type") == "context_length_exceeded" {
				return true
			}
			return bodyContainsContextLengthHint(c.RawBody)
		},
	},
	{
		ID: SymptomReasoningContentPassback,
		Match: func(c SymptomMatchContext) bool {
			if c.Status != 400 {
				return false
			}
			lower := strings.ToLower(c.RawBody)
			if !strings.Contains(lower, "reasoning_content") {
				return false
			}
			if strings.Contains(lower, "passed back") {
				return true
			}
			return strings.Contains(lower, "reasoning_content is missing") &&
				strings.Contains(lower, "thinking is enabled")
		},
	},
	{
		ID: SymptomUnsignedThinking,
		Match: func(c SymptomMatchContext) bool {
			if c.Status != 400 {
				return false
			}
			return strings.Contains(c.RawBody, "Invalid signature in thinking")
		},
	},
	{
		ID: SymptomTempMustBeOneOnThinking,
		Match: func(c SymptomMatchContext) bool {
			if c.Status != 400 {
				return false
			}
			return strings.Contains(c.RawBody, "temperature") &&
				strings.Contains(c.RawBody, "may only be set to 1") &&
				strings.Contains(c.RawBody, "thinking")
		},
	},
	{
		ID: SymptomEmptyTextOnThinking,
		Match: func(c SymptomMatchContext) bool {
			if c.Status != 400 {
				return false
			}
			lower := strings.ToLower(c.RawBody)
			if strings.Contains(lower, "reasoning_content") {
				return false // handled by SymptomReasoningContentPassback
			}
			return (strings.Contains(lower, "thinking mode") || strings.Contains(lower, "thinking_mode")) &&
				strings.Contains(lower, "content") &&
				strings.Contains(lower, "pass") &&
				strings.Contains(lower, "back")
		},
	},
	{
		ID: SymptomCacheControlRejected,
		Match: func(c SymptomMatchContext) bool {
			if c.Status != 400 {
				return false
			}
			lower := strings.ToLower(c.RawBody)
			return strings.Contains(lower, "cache_control") &&
				strings.Contains(lower, "extra inputs are not permitted")
		},
	},
}
