package llm

import "testing"

func TestEstimateProviderPayloadBytes_OpenAIChatCountsSystemToolsAndMessages(t *testing.T) {
	cfg := ResolvedGatewayConfig{
		ProtocolKind: ProtocolKindOpenAIChatCompletions,
		Model:        "gpt-4o",
		OpenAI:       &OpenAIProtocolConfig{},
	}
	base := Request{
		Messages: []Message{
			{Role: "user", Content: []TextPart{{Text: "hello"}}},
		},
	}
	withSystem := Request{
		Messages: []Message{
			{Role: "system", Content: []TextPart{{Text: "policy"}}},
			{Role: "user", Content: []TextPart{{Text: "hello"}}},
		},
	}
	withTools := Request{
		Messages: []Message{
			{Role: "user", Content: []TextPart{{Text: "hello"}}},
		},
		Tools: []ToolSpec{
			{
				Name:        "search",
				Description: strPtr("search tool"),
				JSONSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
	withSystemAndTools := Request{
		Messages: withSystem.Messages,
		Tools:    withTools.Tools,
	}

	baseBytes, err := EstimateProviderPayloadBytes(cfg, base)
	if err != nil {
		t.Fatalf("EstimateProviderPayloadBytes(base): %v", err)
	}
	systemBytes, err := EstimateProviderPayloadBytes(cfg, withSystem)
	if err != nil {
		t.Fatalf("EstimateProviderPayloadBytes(withSystem): %v", err)
	}
	toolsBytes, err := EstimateProviderPayloadBytes(cfg, withTools)
	if err != nil {
		t.Fatalf("EstimateProviderPayloadBytes(withTools): %v", err)
	}
	richBytes, err := EstimateProviderPayloadBytes(cfg, withSystemAndTools)
	if err != nil {
		t.Fatalf("EstimateProviderPayloadBytes(withSystemAndTools): %v", err)
	}

	if systemBytes <= baseBytes {
		t.Fatalf("expected system block to increase provider bytes: base=%d system=%d", baseBytes, systemBytes)
	}
	if toolsBytes <= baseBytes {
		t.Fatalf("expected tools schema to increase provider bytes: base=%d tools=%d", baseBytes, toolsBytes)
	}
	if richBytes <= systemBytes || richBytes <= toolsBytes {
		t.Fatalf("expected system+tools payload to be largest: system=%d tools=%d rich=%d", systemBytes, toolsBytes, richBytes)
	}
}

func TestEstimateProviderPayloadBytes_OpenAIChatIncludesSystemAndTools(t *testing.T) {
	cfg := ResolvedGatewayConfig{
		ProtocolKind: ProtocolKindOpenAIChatCompletions,
		Model:        "gpt-4o",
		OpenAI: &OpenAIProtocolConfig{
			AdvancedPayloadJSON: map[string]any{"top_p": 0.9},
		},
	}
	base := Request{
		Messages: []Message{
			{Role: "user", Content: []TextPart{{Text: "hello"}}},
		},
	}
	withSystemAndTools := Request{
		Messages: []Message{
			{Role: "system", Content: []TextPart{{Text: "guardrails"}}},
			{Role: "user", Content: []TextPart{{Text: "hello"}}},
		},
		Tools: []ToolSpec{
			{
				Name:       "search",
				JSONSchema: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}},
			},
		},
	}

	baseBytes, err := EstimateProviderPayloadBytes(cfg, base)
	if err != nil {
		t.Fatalf("EstimateProviderPayloadBytes(base): %v", err)
	}
	richBytes, err := EstimateProviderPayloadBytes(cfg, withSystemAndTools)
	if err != nil {
		t.Fatalf("EstimateProviderPayloadBytes(withSystemAndTools): %v", err)
	}
	if richBytes <= baseBytes {
		t.Fatalf("expected system/tool payload to increase provider bytes: base=%d rich=%d", baseBytes, richBytes)
	}
}

func TestBuildOpenAIResponsesPayloadForEstimateUsesResponsesToolChoice(t *testing.T) {
	payload, err := buildOpenAIResponsesPayloadForEstimate(&OpenAIProtocolConfig{}, Request{
		Model: "gpt-5",
		Messages: []Message{
			{Role: "system", Content: []TextPart{{Text: "policy"}}},
			{Role: "user", Content: []TextPart{{Text: "hello"}}},
		},
		Tools: []ToolSpec{{
			Name:       "echo",
			JSONSchema: map[string]any{"type": "object"},
		}},
		ToolChoice: &ToolChoice{Mode: "specific", ToolName: "echo"},
	})
	if err != nil {
		t.Fatalf("buildOpenAIResponsesPayloadForEstimate: %v", err)
	}
	choice, ok := payload["tool_choice"].(map[string]any)
	if !ok {
		t.Fatalf("expected map tool_choice, got %#v", payload["tool_choice"])
	}
	if choice["type"] != "function" || choice["name"] != "echo" || choice["function"] != nil {
		t.Fatalf("unexpected responses tool_choice: %#v", choice)
	}
	if payload["instructions"] != "policy" {
		t.Fatalf("expected system instructions, got %#v", payload["instructions"])
	}
}

func strPtr(v string) *string { return &v }

func TestEstimateProviderPayloadBytes_AnthropicIncludesPromptPlanSystemBlocks(t *testing.T) {
	cfg := ResolvedGatewayConfig{
		ProtocolKind: ProtocolKindAnthropicMessages,
		Model:        "claude-opus-4-1",
		Anthropic:    &AnthropicProtocolConfig{},
	}
	base := Request{
		Messages: []Message{
			{Role: "user", Content: []TextPart{{Text: "hello"}}},
		},
	}
	withPromptPlan := base
	withPromptPlan.PromptPlan = &PromptPlan{
		SystemBlocks: []PromptPlanBlock{{
			Name:      "persona",
			Target:    PromptTargetSystemPrefix,
			Role:      "system",
			Text:      "keep the exact travel times and booking IDs",
			Stability: CacheStabilityStablePrefix,
		}},
	}

	baseBytes, err := EstimateProviderPayloadBytes(cfg, base)
	if err != nil {
		t.Fatalf("EstimateProviderPayloadBytes(base): %v", err)
	}
	plannedBytes, err := EstimateProviderPayloadBytes(cfg, withPromptPlan)
	if err != nil {
		t.Fatalf("EstimateProviderPayloadBytes(withPromptPlan): %v", err)
	}
	if plannedBytes <= baseBytes {
		t.Fatalf("expected prompt plan system blocks to increase provider bytes: base=%d planned=%d", baseBytes, plannedBytes)
	}
}

func TestEstimateProviderPayloadBytes_GeminiIncludesAdvancedPayload(t *testing.T) {
	req := Request{
		Model: "gemini-2.5-pro",
		Messages: []Message{
			{Role: "user", Content: []TextPart{{Text: "hello"}}},
		},
	}
	baseCfg := ResolvedGatewayConfig{
		ProtocolKind: ProtocolKindGeminiGenerateContent,
		Model:        "gemini-2.5-pro",
		Gemini:       &GeminiProtocolConfig{},
	}
	advancedCfg := ResolvedGatewayConfig{
		ProtocolKind: ProtocolKindGeminiGenerateContent,
		Model:        "gemini-2.5-pro",
		Gemini: &GeminiProtocolConfig{
			AdvancedPayloadJSON: map[string]any{
				"generationConfig": map[string]any{
					"candidateCount": float64(2),
				},
			},
		},
	}

	baseBytes, err := EstimateProviderPayloadBytes(baseCfg, req)
	if err != nil {
		t.Fatalf("EstimateProviderPayloadBytes(baseCfg): %v", err)
	}
	advancedBytes, err := EstimateProviderPayloadBytes(advancedCfg, req)
	if err != nil {
		t.Fatalf("EstimateProviderPayloadBytes(advancedCfg): %v", err)
	}
	if advancedBytes <= baseBytes {
		t.Fatalf("expected advanced payload to increase provider bytes: base=%d advanced=%d", baseBytes, advancedBytes)
	}
}
