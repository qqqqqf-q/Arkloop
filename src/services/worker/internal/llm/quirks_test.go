package llm

import (
	"reflect"
	"sync"
	"testing"
)

func TestQuirkMatch_EchoReasoningContent(t *testing.T) {
	q := openAIQuirks[0]
	if q.ID != QuirkEchoReasoningContent {
		t.Fatalf("expected echo_reasoning_content, got %s", q.ID)
	}
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"deepseek_real", 400, `{"error":{"message":"Error from provider (DeepSeek): The reasoning_content in the thinking mode must be passed back to the API."}}`, true},
		{"moonshot_missing", 400, `{"error":{"message":"reasoning_content is missing when thinking is enabled"}}`, true},
		{"wrong_status", 500, `reasoning_content must be passed back`, false},
		{"missing_phrase", 400, `reasoning_content is invalid`, false},
		{"missing_field", 400, `must be passed back`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := q.Match(tc.status, tc.body)
			if got != tc.want {
				t.Fatalf("Match(%d,%q)=%v want %v", tc.status, tc.body, got, tc.want)
			}
		})
	}
}

func TestQuirkMatch_DowngradeXHighReasoning(t *testing.T) {
	q := openAIQuirks[1]
	if q.ID != QuirkDowngradeXHighReasoning {
		t.Fatalf("expected downgrade_xhigh_reasoning, got %s", q.ID)
	}
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{
			name:   "xiaomi_literal_error",
			status: 400,
			body:   `{"message":"Error from provider (Xiaomi): [{'type': 'literal_error', 'loc': ('body', 'reasoning_effort'), 'msg': \"Input should be 'low', 'medium' or 'high'\", 'input': 'xhigh', 'ctx': {'expected': \"'low', 'medium' or 'high'\"}}]"}`,
			want:   true,
		},
		{"wrong_status", 500, `reasoning_effort xhigh expected low medium high`, false},
		{"missing_xhigh", 400, `reasoning_effort expected low medium high`, false},
		{"xhigh_does_not_count_as_high", 400, `reasoning_effort input xhigh expected low medium`, false},
		{"missing_expected_values", 400, `reasoning_effort xhigh is invalid`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := q.Match(tc.status, tc.body)
			if got != tc.want {
				t.Fatalf("Match(%d,%q)=%v want %v", tc.status, tc.body, got, tc.want)
			}
		})
	}
}

func TestQuirkMatch_StripUnsignedThinking(t *testing.T) {
	q := anthropicQuirks[0]
	if q.ID != QuirkStripUnsignedThinking {
		t.Fatalf("unexpected id %s", q.ID)
	}
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"real", 400, `{"error":{"message":"Invalid signature in thinking block"}}`, true},
		{"wrong_status", 200, `Invalid signature thinking`, false},
		{"missing_thinking", 400, `Invalid signature for token`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := q.Match(tc.status, tc.body); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestQuirkMatch_ForceTempOneOnThinking(t *testing.T) {
	q := anthropicQuirks[1]
	if q.ID != QuirkForceTempOneOnThink {
		t.Fatalf("unexpected id %s", q.ID)
	}
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"real", 400, `temperature may only be set to 1 when thinking is enabled`, true},
		{"wrong_status", 500, `temperature may only be set to 1`, false},
		{"missing_temp", 400, `value may only be set to 1`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := q.Match(tc.status, tc.body); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestQuirkMatch_EchoEmptyTextOnThinking(t *testing.T) {
	q := anthropicQuirks[2]
	if q.ID != QuirkEchoEmptyTextOnThink {
		t.Fatalf("unexpected id %s", q.ID)
	}
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"deepseek_content_passback", 400, `{"error":{"message":"The content in the thinking mode must be passed back to the API."}}`, true},
		{"deepseek_underscore_mode", 400, `content in thinking_mode must be passback`, true},
		{"wrong_status", 500, `content in thinking mode must be passed back`, false},
		{"missing_content", 400, `thinking must be passed back`, false},
		{"missing_mode", 400, `content thinking block must be passed back`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := q.Match(tc.status, tc.body); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestQuirkApply_EchoReasoningContent(t *testing.T) {
	payload := map[string]any{
		"messages": []map[string]any{
			{"role": "user", "content": "hi"},
			{"role": "assistant", "content": "hello"},
			{"role": "assistant", "content": "x", "reasoning_content": "keep"},
		},
		"input": []any{
			map[string]any{"role": "assistant", "content": []any{}},
		},
	}
	applyEchoReasoningContent(payload)
	msgs := payload["messages"].([]map[string]any)
	if _, ok := msgs[0]["reasoning_content"]; ok {
		t.Fatalf("user must not get reasoning_content")
	}
	if msgs[1]["reasoning_content"] != "" {
		t.Fatalf("assistant should get empty reasoning_content, got %#v", msgs[1])
	}
	if msgs[2]["reasoning_content"] != "keep" {
		t.Fatalf("must not overwrite existing: %#v", msgs[2])
	}
	inputItem := payload["input"].([]any)[0].(map[string]any)
	if _, ok := inputItem["reasoning_content"]; ok {
		t.Fatalf("responses input must not get reasoning_content: %#v", inputItem)
	}
}

func TestQuirkApply_DowngradeXHighReasoning(t *testing.T) {
	payload := map[string]any{
		"reasoning_effort": "xhigh",
		"reasoning": map[string]any{
			"effort":  "xhigh",
			"summary": "auto",
		},
	}
	applyDowngradeXHighReasoning(payload)
	if payload["reasoning_effort"] != "high" {
		t.Fatalf("expected chat reasoning_effort high, got %#v", payload["reasoning_effort"])
	}
	reasoning := payload["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" || reasoning["summary"] != "auto" {
		t.Fatalf("unexpected responses reasoning: %#v", reasoning)
	}

	unchanged := map[string]any{"reasoning_effort": "medium", "reasoning": map[string]any{"effort": "low"}}
	applyDowngradeXHighReasoning(unchanged)
	if unchanged["reasoning_effort"] != "medium" || unchanged["reasoning"].(map[string]any)["effort"] != "low" {
		t.Fatalf("non-xhigh reasoning must not change: %#v", unchanged)
	}
}

func TestQuirkApply_StripUnsignedThinking(t *testing.T) {
	payload := map[string]any{
		"messages": []map[string]any{
			{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "thinking", "thinking": "drop"},
					{"type": "thinking", "thinking": "drop2", "signature": ""},
					{"type": "thinking", "thinking": "keep", "signature": "sig"},
					{"type": "text", "text": "answer"},
				},
			},
		},
	}
	applyStripUnsignedThinking(payload)
	content := payload["messages"].([]map[string]any)[0]["content"].([]map[string]any)
	if len(content) != 2 {
		t.Fatalf("expected 2 blocks left, got %#v", content)
	}
	if content[0]["thinking"] != "keep" || content[1]["type"] != "text" {
		t.Fatalf("unexpected content after strip: %#v", content)
	}
}

func TestQuirkApply_EchoEmptyTextOnThinking(t *testing.T) {
	payload := map[string]any{
		"messages": []map[string]any{
			{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "thinking", "thinking": "plan", "signature": "sig"},
				},
			},
			{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "thinking", "thinking": "plan", "signature": "sig"},
					{"type": "text", "text": "answer"},
				},
			},
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "thinking", "thinking": "ignored"},
				},
			},
			{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "thinking", "thinking": "plan", "signature": "sig"},
					{"type": "tool_use", "id": "toolu_1", "name": "read", "input": map[string]any{}},
					{"type": "tool_use", "id": "toolu_2", "name": "write", "input": map[string]any{}},
				},
			},
			{
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "thinking", "thinking": "plan", "signature": "sig"},
					map[string]any{"type": "tool_use", "id": "toolu_3", "name": "read", "input": map[string]any{}},
				},
			},
		},
	}
	applyEchoEmptyTextOnThinking(payload)
	msgs := payload["messages"].([]map[string]any)
	first := msgs[0]["content"].([]map[string]any)
	if len(first) != 2 || first[1]["type"] != "text" || first[1]["text"] != "" {
		t.Fatalf("assistant thinking-only message must get empty text block: %#v", first)
	}
	second := msgs[1]["content"].([]map[string]any)
	if len(second) != 2 {
		t.Fatalf("assistant with text must not get duplicate empty text: %#v", second)
	}
	user := msgs[2]["content"].([]map[string]any)
	if len(user) != 1 {
		t.Fatalf("user message must not change: %#v", user)
	}
	withTools := msgs[3]["content"].([]map[string]any)
	if len(withTools) != 4 {
		t.Fatalf("assistant with tool_use must keep all blocks: %#v", withTools)
	}
	if withTools[0]["type"] != "thinking" || withTools[1]["type"] != "text" || withTools[1]["text"] != "" || withTools[2]["type"] != "tool_use" || withTools[3]["type"] != "tool_use" {
		t.Fatalf("empty text must be inserted before first tool_use: %#v", withTools)
	}
	rawWithTools := msgs[4]["content"].([]any)
	if len(rawWithTools) != 3 {
		t.Fatalf("raw assistant with tool_use must keep all blocks: %#v", rawWithTools)
	}
	rawText, _ := rawWithTools[1].(map[string]any)
	rawTool, _ := rawWithTools[2].(map[string]any)
	if rawText["type"] != "text" || rawText["text"] != "" || rawTool["type"] != "tool_use" {
		t.Fatalf("raw empty text must be inserted before first tool_use: %#v", rawWithTools)
	}
}

func TestQuirkApply_ForceTempOneOnThinking(t *testing.T) {
	temp := 0.7
	payload := map[string]any{
		"thinking":    map[string]any{"type": "enabled", "budget_tokens": 1024},
		"temperature": temp,
	}
	applyForceTempOneOnThinking(payload)
	if payload["temperature"] != 1.0 {
		t.Fatalf("expected temperature=1, got %#v", payload["temperature"])
	}

	disabled := map[string]any{"thinking": map[string]any{"type": "disabled"}, "temperature": 0.7}
	applyForceTempOneOnThinking(disabled)
	if disabled["temperature"] != 0.7 {
		t.Fatalf("must not change temperature when thinking disabled: %#v", disabled)
	}

	missing := map[string]any{"temperature": 0.5}
	applyForceTempOneOnThinking(missing)
	if missing["temperature"] != 0.5 {
		t.Fatalf("must not change without thinking: %#v", missing)
	}
}

func TestQuirkMatch_StripCacheControl(t *testing.T) {
	q := anthropicQuirks[3]
	if q.ID != QuirkStripCacheControl {
		t.Fatalf("unexpected id %s", q.ID)
	}
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{
			name:   "didl_extra_inputs",
			status: 400,
			body:   `{"error":{"message":"Extra inputs are not permitted, field: messages[1].content[0].cache_control"}}`,
			want:   true,
		},
		{
			name:   "wrong_status",
			status: 500,
			body:   `Extra inputs are not permitted cache_control`,
			want:   false,
		},
		{
			name:   "missing_cache_control",
			status: 400,
			body:   `Extra inputs are not permitted, field: messages[1].foo`,
			want:   false,
		},
		{
			name:   "missing_extra_inputs",
			status: 400,
			body:   `cache_control is invalid`,
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := q.Match(tc.status, tc.body); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestQuirkApply_StripCacheControl(t *testing.T) {
	payload := map[string]any{
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": "hello", "cache_control": map[string]any{"type": "ephemeral"}},
				},
			},
			{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "text", "text": "hi", "cache_control": map[string]any{"type": "ephemeral"}},
					{"type": "tool_use", "id": "t1", "name": "read", "input": map[string]any{}},
				},
			},
		},
		"system": []map[string]any{
			{"type": "text", "text": "sys", "cache_control": map[string]any{"type": "ephemeral"}},
		},
		"tools": []map[string]any{
			{"name": "read", "input_schema": map[string]any{}, "cache_control": map[string]any{"type": "ephemeral"}},
		},
	}
	applyStripCacheControl(payload)

	msgs := payload["messages"].([]map[string]any)
	userContent := msgs[0]["content"].([]map[string]any)
	if _, ok := userContent[0]["cache_control"]; ok {
		t.Fatalf("user message cache_control must be removed")
	}
	assistantContent := msgs[1]["content"].([]map[string]any)
	if _, ok := assistantContent[0]["cache_control"]; ok {
		t.Fatalf("assistant message cache_control must be removed")
	}

	system := payload["system"].([]map[string]any)
	if _, ok := system[0]["cache_control"]; ok {
		t.Fatalf("system cache_control must be removed")
	}

	tools := payload["tools"].([]map[string]any)
	if _, ok := tools[0]["cache_control"]; ok {
		t.Fatalf("tool cache_control must be removed")
	}
}

func TestQuirkStore_Concurrent(t *testing.T) {
	store := NewQuirkStore()
	var wg sync.WaitGroup
	ids := []QuirkID{QuirkEchoReasoningContent, QuirkDowngradeXHighReasoning, QuirkStripUnsignedThinking, QuirkForceTempOneOnThink, QuirkEchoEmptyTextOnThink}
	for _, id := range ids {
		id := id
		wg.Add(2)
		go func() { defer wg.Done(); store.Set(id) }()
		go func() { defer wg.Done(); _ = store.Has(id) }()
	}
	wg.Wait()
	for _, id := range ids {
		if !store.Has(id) {
			t.Fatalf("missing %s", id)
		}
	}
}

func TestQuirkStore_ApplyAll_OnlyActive(t *testing.T) {
	store := NewQuirkStore()
	payload := map[string]any{
		"messages": []map[string]any{{"role": "assistant", "content": "hi"}},
	}
	original := map[string]any{"messages": []map[string]any{{"role": "assistant", "content": "hi"}}}
	store.ApplyAll(payload, openAIQuirks)
	if !reflect.DeepEqual(payload, original) {
		t.Fatalf("inactive store must not modify payload")
	}
	store.Set(QuirkEchoReasoningContent)
	store.ApplyAll(payload, openAIQuirks)
	msgs := payload["messages"].([]map[string]any)
	if msgs[0]["reasoning_content"] != "" {
		t.Fatalf("after Set, reasoning_content must be added: %#v", msgs[0])
	}
}

func TestDetectQuirk(t *testing.T) {
	id, ok := detectQuirk(400, `reasoning_content must be passed back to the API`, openAIQuirks)
	if !ok || id != QuirkEchoReasoningContent {
		t.Fatalf("expected echo, got %s ok=%v", id, ok)
	}
	if _, ok := detectQuirk(200, `reasoning_content must be passed back`, openAIQuirks); ok {
		t.Fatalf("status 200 must not match")
	}
	id, ok = detectQuirk(400, `reasoning_content is missing because thinking is enabled`, openAIQuirks)
	if !ok || id != QuirkEchoReasoningContent {
		t.Fatalf("expected moonshot echo, got %s ok=%v", id, ok)
	}
	id, ok = detectQuirk(400, `reasoning_effort input xhigh expected low medium high`, openAIQuirks)
	if !ok || id != QuirkDowngradeXHighReasoning {
		t.Fatalf("expected xhigh downgrade, got %s ok=%v", id, ok)
	}
	id, ok = detectQuirk(400, `Invalid signature in thinking block`, anthropicQuirks)
	if !ok || id != QuirkStripUnsignedThinking {
		t.Fatalf("expected strip, got %s ok=%v", id, ok)
	}
	id, ok = detectQuirk(400, `content in thinking mode must be passed back`, anthropicQuirks)
	if !ok || id != QuirkEchoEmptyTextOnThink {
		t.Fatalf("expected anthropic echo empty text, got %s ok=%v", id, ok)
	}
}
