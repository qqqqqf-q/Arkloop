package llm

import (
	"strings"
	"sync"
)

type QuirkID string

const (
	QuirkEchoReasoningContent    QuirkID = "echo_reasoning_content"
	QuirkDowngradeXHighReasoning QuirkID = "downgrade_xhigh_reasoning"
	QuirkStripUnsignedThinking   QuirkID = "strip_unsigned_thinking"
	QuirkForceTempOneOnThink     QuirkID = "force_temp_one_on_thinking"
	QuirkEchoEmptyTextOnThink    QuirkID = "echo_empty_text_on_thinking"
	QuirkStripCacheControl       QuirkID = "strip_cache_control"
)

type Quirk struct {
	ID    QuirkID
	Match func(status int, rawBody string) bool
	Apply func(payload map[string]any)
}

type QuirkStore struct {
	mu     sync.RWMutex
	active map[QuirkID]struct{}
}

func NewQuirkStore() *QuirkStore {
	return &QuirkStore{active: map[QuirkID]struct{}{}}
}

func (s *QuirkStore) Has(id QuirkID) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.active[id]
	return ok
}

func (s *QuirkStore) Set(id QuirkID) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		s.active = map[QuirkID]struct{}{}
	}
	s.active[id] = struct{}{}
}

func (s *QuirkStore) ApplyAll(payload map[string]any, registry []Quirk) {
	if s == nil || payload == nil {
		return
	}
	for _, q := range registry {
		if !s.Has(q.ID) || q.Apply == nil {
			continue
		}
		q.Apply(payload)
	}
}

func (s *QuirkStore) DetectFromError(status int, rawBody string, registry []Quirk) (QuirkID, bool) {
	return detectQuirk(status, rawBody, registry)
}

func detectQuirk(status int, rawBody string, registry []Quirk) (QuirkID, bool) {
	for _, q := range registry {
		if q.Match == nil {
			continue
		}
		if q.Match(status, rawBody) {
			return q.ID, true
		}
	}
	return "", false
}

var openAIQuirks = []Quirk{
	{
		ID: QuirkEchoReasoningContent,
		Match: func(status int, rawBody string) bool {
			if status != 400 {
				return false
			}
			lower := strings.ToLower(rawBody)
			if !strings.Contains(lower, "reasoning_content") {
				return false
			}
			if strings.Contains(lower, "passed back") {
				return true
			}
			return strings.Contains(lower, "reasoning_content is missing") &&
				strings.Contains(lower, "thinking is enabled")
		},
		Apply: applyEchoReasoningContent,
	},
	{
		ID:    QuirkDowngradeXHighReasoning,
		Match: matchXHighReasoningUnsupported,
		Apply: applyDowngradeXHighReasoning,
	},
}

var anthropicQuirks = []Quirk{
	{
		ID: QuirkStripUnsignedThinking,
		Match: func(status int, rawBody string) bool {
			if status != 400 {
				return false
			}
			return strings.Contains(rawBody, "Invalid signature in thinking")
		},
		Apply: applyStripUnsignedThinking,
	},
	{
		ID: QuirkForceTempOneOnThink,
		Match: func(status int, rawBody string) bool {
			if status != 400 {
				return false
			}
			return strings.Contains(rawBody, "temperature") &&
				strings.Contains(rawBody, "may only be set to 1") &&
				strings.Contains(rawBody, "thinking")
		},
		Apply: applyForceTempOneOnThinking,
	},
	{
		ID: QuirkEchoEmptyTextOnThink,
		Match: func(status int, rawBody string) bool {
			if status != 400 {
				return false
			}
			lower := strings.ToLower(rawBody)
			return (strings.Contains(lower, "thinking mode") || strings.Contains(lower, "thinking_mode")) &&
				strings.Contains(lower, "content") &&
				strings.Contains(lower, "pass") &&
				strings.Contains(lower, "back")
		},
		Apply: applyEchoEmptyTextOnThinking,
	},
	{
		ID: QuirkStripCacheControl,
		Match: func(status int, rawBody string) bool {
			if status != 400 {
				return false
			}
			lower := strings.ToLower(rawBody)
			return strings.Contains(lower, "cache_control") &&
				strings.Contains(lower, "extra inputs are not permitted")
		},
		Apply: applyStripCacheControl,
	},
}

func applyEchoReasoningContent(payload map[string]any) {
	walkAssistantItems(payload["messages"], echoReasoningContentOnItem)
}

func walkAssistantItems(value any, fn func(map[string]any)) {
	switch list := value.(type) {
	case []map[string]any:
		for _, item := range list {
			if item == nil {
				continue
			}
			if role, _ := item["role"].(string); role == "assistant" {
				fn(item)
			}
		}
	case []any:
		for _, raw := range list {
			item, ok := raw.(map[string]any)
			if !ok || item == nil {
				continue
			}
			if role, _ := item["role"].(string); role == "assistant" {
				fn(item)
			}
		}
	}
}

func echoReasoningContentOnItem(item map[string]any) {
	if _, ok := item["reasoning_content"]; ok {
		return
	}
	item["reasoning_content"] = ""
}

func matchXHighReasoningUnsupported(status int, rawBody string) bool {
	if status != 400 {
		return false
	}
	lower := strings.ToLower(rawBody)
	if !strings.Contains(lower, "reasoning_effort") || !hasLowerAlphaToken(lower, "xhigh") {
		return false
	}
	if !hasLowerAlphaToken(lower, "low") || !hasLowerAlphaToken(lower, "medium") || !hasLowerAlphaToken(lower, "high") {
		return false
	}
	return strings.Contains(lower, "expected") ||
		strings.Contains(lower, "input should be") ||
		strings.Contains(lower, "literal_error")
}

func hasLowerAlphaToken(text string, want string) bool {
	for _, token := range strings.FieldsFunc(text, func(r rune) bool {
		return r < 'a' || r > 'z'
	}) {
		if token == want {
			return true
		}
	}
	return false
}

func applyDowngradeXHighReasoning(payload map[string]any) {
	if effort, _ := payload["reasoning_effort"].(string); strings.EqualFold(effort, "xhigh") {
		payload["reasoning_effort"] = "high"
	}
	reasoning, ok := payload["reasoning"].(map[string]any)
	if !ok {
		return
	}
	if effort, _ := reasoning["effort"].(string); strings.EqualFold(effort, "xhigh") {
		reasoning["effort"] = "high"
	}
}

func applyEchoEmptyTextOnThinking(payload map[string]any) {
	walkAssistantItems(payload["messages"], echoEmptyTextOnThinkingItem)
}

func echoEmptyTextOnThinkingItem(item map[string]any) {
	switch content := item["content"].(type) {
	case []map[string]any:
		if !blocksHaveThinking(content) || blocksHaveText(content) {
			return
		}
		item["content"] = insertEmptyTextBeforeToolUse(content)
	case []any:
		if !rawBlocksHaveThinking(content) || rawBlocksHaveText(content) {
			return
		}
		item["content"] = insertRawEmptyTextBeforeToolUse(content)
	}
}

func insertEmptyTextBeforeToolUse(blocks []map[string]any) []map[string]any {
	text := map[string]any{"type": "text", "text": ""}
	for i, block := range blocks {
		if typ, _ := block["type"].(string); typ == "tool_use" {
			out := make([]map[string]any, 0, len(blocks)+1)
			out = append(out, blocks[:i]...)
			out = append(out, text)
			return append(out, blocks[i:]...)
		}
	}
	return append(blocks, text)
}

func insertRawEmptyTextBeforeToolUse(blocks []any) []any {
	text := map[string]any{"type": "text", "text": ""}
	for i, raw := range blocks {
		block, ok := raw.(map[string]any)
		if ok {
			if typ, _ := block["type"].(string); typ == "tool_use" {
				out := make([]any, 0, len(blocks)+1)
				out = append(out, blocks[:i]...)
				out = append(out, text)
				return append(out, blocks[i:]...)
			}
		}
	}
	return append(blocks, text)
}

func blocksHaveThinking(blocks []map[string]any) bool {
	for _, block := range blocks {
		if isThinkingBlock(block) {
			return true
		}
	}
	return false
}

func blocksHaveText(blocks []map[string]any) bool {
	for _, block := range blocks {
		if typ, _ := block["type"].(string); typ == "text" {
			return true
		}
	}
	return false
}

func rawBlocksHaveThinking(blocks []any) bool {
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if ok && isThinkingBlock(block) {
			return true
		}
	}
	return false
}

func rawBlocksHaveText(blocks []any) bool {
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if typ, _ := block["type"].(string); typ == "text" {
			return true
		}
	}
	return false
}

func isThinkingBlock(block map[string]any) bool {
	switch typ, _ := block["type"].(string); typ {
	case "thinking", "redacted_thinking":
		return true
	default:
		return false
	}
}

func applyStripUnsignedThinking(payload map[string]any) {
	switch messages := payload["messages"].(type) {
	case []map[string]any:
		filtered := make([]map[string]any, 0, len(messages))
		for _, msg := range messages {
			stripUnsignedThinkingFromMessage(msg)
			if !messageHasEmptyContent(msg) {
				filtered = append(filtered, msg)
			}
		}
		payload["messages"] = filtered
	case []any:
		filtered := make([]any, 0, len(messages))
		for _, raw := range messages {
			msg, ok := raw.(map[string]any)
			if !ok {
				filtered = append(filtered, raw)
				continue
			}
			stripUnsignedThinkingFromMessage(msg)
			if !messageHasEmptyContent(msg) {
				filtered = append(filtered, raw)
			}
		}
		payload["messages"] = filtered
	}
}

func messageHasEmptyContent(msg map[string]any) bool {
	if msg == nil {
		return true
	}
	switch c := msg["content"].(type) {
	case []map[string]any:
		return len(c) == 0
	case []any:
		return len(c) == 0
	case string:
		return strings.TrimSpace(c) == ""
	case nil:
		return true
	}
	return false
}

func stripUnsignedThinkingFromMessage(msg map[string]any) {
	if msg == nil {
		return
	}
	switch content := msg["content"].(type) {
	case []map[string]any:
		filtered := make([]map[string]any, 0, len(content))
		for _, block := range content {
			if isUnsignedThinking(block) {
				continue
			}
			filtered = append(filtered, block)
		}
		msg["content"] = filtered
	case []any:
		filtered := make([]any, 0, len(content))
		for _, raw := range content {
			block, ok := raw.(map[string]any)
			if ok && isUnsignedThinking(block) {
				continue
			}
			filtered = append(filtered, raw)
		}
		msg["content"] = filtered
	}
}

func isUnsignedThinking(block map[string]any) bool {
	if block == nil {
		return false
	}
	if typ, _ := block["type"].(string); typ != "thinking" {
		return false
	}
	sig, ok := block["signature"]
	if !ok {
		return true
	}
	s, _ := sig.(string)
	return strings.TrimSpace(s) == ""
}

func applyForceTempOneOnThinking(payload map[string]any) {
	thinking, ok := payload["thinking"].(map[string]any)
	if !ok {
		return
	}
	if typ, _ := thinking["type"].(string); typ != "enabled" {
		return
	}
	payload["temperature"] = 1.0
}

func applyStripCacheControl(payload map[string]any) {
	if messages, ok := payload["messages"].([]map[string]any); ok {
		for _, msg := range messages {
			if content, ok := msg["content"].([]map[string]any); ok {
				for _, block := range content {
					delete(block, "cache_control")
				}
			}
		}
	}
	if system, ok := payload["system"].([]map[string]any); ok {
		for _, block := range system {
			delete(block, "cache_control")
		}
	}
	if tools, ok := payload["tools"].([]map[string]any); ok {
		for _, tool := range tools {
			delete(tool, "cache_control")
		}
	}
}
