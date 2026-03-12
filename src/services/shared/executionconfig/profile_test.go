package executionconfig

import "testing"

func TestResolveEffectiveProfileClampsBudgets(t *testing.T) {
	profile := ResolveEffectiveProfile(
		PlatformLimits{AgentReasoningIterations: 10, ToolContinuationBudget: 32},
		&AgentConfigProfile{Name: "org-default", MaxOutputTokens: intPtr(4096), Temperature: floatPtr(0.2), TopP: floatPtr(0.8), ReasoningMode: "auto"},
		&PersonaProfile{
			SoulMD:                  "persona soul",
			PromptMD:                "persona prompt",
			PreferredCredentialName: strPtr("cred-a"),
			Budgets: RequestedBudgets{
				ReasoningIterations:    intPtr(20),
				ToolContinuationBudget: intPtr(8),
				MaxOutputTokens:        intPtr(8192),
				Temperature:            floatPtr(0.4),
				PerToolSoftLimits: PerToolSoftLimits{
					"write_stdin": {MaxContinuations: intPtr(9)},
				},
			},
		},
	)

	if profile.SystemPrompt != "persona soul\n\npersona prompt" {
		t.Fatalf("unexpected system_prompt: %q", profile.SystemPrompt)
	}

	if profile.ReasoningIterations != 10 {
		t.Fatalf("unexpected reasoning_iterations: %d", profile.ReasoningIterations)
	}
	if profile.ToolContinuationBudget != 8 {
		t.Fatalf("unexpected tool_continuation_budget: %d", profile.ToolContinuationBudget)
	}
	if profile.MaxOutputTokens == nil || *profile.MaxOutputTokens != 4096 {
		t.Fatalf("unexpected max_output_tokens: %v", profile.MaxOutputTokens)
	}
	if profile.Temperature == nil || *profile.Temperature != 0.4 {
		t.Fatalf("unexpected temperature: %v", profile.Temperature)
	}
	if profile.TopP == nil || *profile.TopP != 0.8 {
		t.Fatalf("unexpected top_p: %v", profile.TopP)
	}
	if profile.PreferredCredentialName != "cred-a" {
		t.Fatalf("unexpected preferred_credential_name: %q", profile.PreferredCredentialName)
	}
	if writeLimit := profile.PerToolSoftLimits["write_stdin"]; writeLimit.MaxContinuations == nil || *writeLimit.MaxContinuations != 9 {
		t.Fatalf("unexpected write_stdin limit: %#v", writeLimit)
	}
}

func TestResolveEffectiveProfileJoinsPromptSegments(t *testing.T) {
	tests := []struct {
		name     string
		agent    *AgentConfigProfile
		persona  *PersonaProfile
		expected string
	}{
		{
			name:     "agent config only",
			agent:    &AgentConfigProfile{SystemPrompt: strPtr("agent prompt")},
			expected: "agent prompt",
		},
		{
			name:     "soul only",
			persona:  &PersonaProfile{SoulMD: "persona soul"},
			expected: "persona soul",
		},
		{
			name:     "prompt only",
			persona:  &PersonaProfile{PromptMD: "persona prompt"},
			expected: "persona prompt",
		},
		{
			name:     "soul and prompt",
			persona:  &PersonaProfile{SoulMD: "persona soul", PromptMD: "persona prompt"},
			expected: "persona soul\n\npersona prompt",
		},
		{
			name:     "full chain",
			agent:    &AgentConfigProfile{SystemPrompt: strPtr("agent prompt")},
			persona:  &PersonaProfile{SoulMD: "persona soul", PromptMD: "persona prompt"},
			expected: "agent prompt\n\npersona soul\n\npersona prompt",
		},
		{
			name:     "skip blank soul",
			agent:    &AgentConfigProfile{SystemPrompt: strPtr("agent prompt")},
			persona:  &PersonaProfile{SoulMD: "   ", PromptMD: "persona prompt"},
			expected: "agent prompt\n\npersona prompt",
		},
		{
			name:     "nil persona keeps old behavior",
			agent:    &AgentConfigProfile{SystemPrompt: strPtr(" agent prompt ")},
			persona:  nil,
			expected: "agent prompt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := ResolveEffectiveProfile(PlatformLimits{}, tt.agent, tt.persona)
			if profile.SystemPrompt != tt.expected {
				t.Fatalf("unexpected system_prompt: %q", profile.SystemPrompt)
			}
		})
	}
}

func TestResolveEffectiveProfilePrefersPersonaNamedAgentConfig(t *testing.T) {
	profile := ResolveEffectiveProfile(
		PlatformLimits{},
		&AgentConfigProfile{Name: "platform-default", MaxOutputTokens: intPtr(2048)},
		&PersonaProfile{ResolvedAgentConfigName: strPtr("named-config")},
	)
	if profile.ResolvedAgentConfigName != "named-config" {
		t.Fatalf("unexpected resolved agent config name: %q", profile.ResolvedAgentConfigName)
	}
	if profile.ReasoningIterations != 0 {
		t.Fatalf("unexpected default reasoning limit: %d", profile.ReasoningIterations)
	}
	if profile.ToolContinuationBudget != 32 {
		t.Fatalf("unexpected default continuation limit: %d", profile.ToolContinuationBudget)
	}
}

func TestResolveEffectiveProfileAllowsFinitePersonaWhenPlatformReasoningUnlimited(t *testing.T) {
	profile := ResolveEffectiveProfile(
		PlatformLimits{AgentReasoningIterations: 0, ToolContinuationBudget: 32},
		nil,
		&PersonaProfile{
			Budgets: RequestedBudgets{
				ReasoningIterations: intPtr(6),
			},
		},
	)
	if profile.ReasoningIterations != 6 {
		t.Fatalf("unexpected reasoning_iterations: %d", profile.ReasoningIterations)
	}
}

func TestParseRequestedBudgetsJSON(t *testing.T) {
	budgets, err := ParseRequestedBudgetsJSON([]byte(`{"reasoning_iterations":4,"tool_continuation_budget":12,"per_tool_soft_limits":{"write_stdin":{"max_continuations":7,"max_output_bytes":12345}}}`))
	if err != nil {
		t.Fatalf("parse budgets: %v", err)
	}
	if budgets.ReasoningIterations == nil || *budgets.ReasoningIterations != 4 {
		t.Fatalf("unexpected reasoning_iterations: %v", budgets.ReasoningIterations)
	}
	if budgets.ToolContinuationBudget == nil || *budgets.ToolContinuationBudget != 12 {
		t.Fatalf("unexpected tool_continuation_budget: %v", budgets.ToolContinuationBudget)
	}
	if writeLimit := budgets.PerToolSoftLimits["write_stdin"]; writeLimit.MaxContinuations == nil || *writeLimit.MaxContinuations != 7 {
		t.Fatalf("unexpected write_stdin max_continuations: %#v", writeLimit)
	}
	if writeLimit := budgets.PerToolSoftLimits["write_stdin"]; writeLimit.MaxOutputBytes == nil || *writeLimit.MaxOutputBytes != 12345 {
		t.Fatalf("unexpected write_stdin max_output_bytes: %#v", writeLimit)
	}
}

func TestParseRequestedBudgetsRejectsInvalidSoftLimit(t *testing.T) {
	_, err := ParseRequestedBudgetsJSON([]byte(`{"per_tool_soft_limits":{"write_stdin":{"max_yield_time_ms":40000}}}`))
	if err == nil || err.Error() != "budgets.per_tool_soft_limits.write_stdin.max_yield_time_ms must be less than or equal to 30000" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseRequestedBudgetsMap_CostBudgetFields(t *testing.T) {
	raw := map[string]any{
		"max_cost_micros":         float64(5000000),
		"max_total_output_tokens": float64(100000),
	}
	b, err := ParseRequestedBudgetsMap(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.MaxCostMicros == nil || *b.MaxCostMicros != 5000000 {
		t.Fatalf("expected MaxCostMicros=5000000, got %v", b.MaxCostMicros)
	}
	if b.MaxTotalOutputTokens == nil || *b.MaxTotalOutputTokens != 100000 {
		t.Fatalf("expected MaxTotalOutputTokens=100000, got %v", b.MaxTotalOutputTokens)
	}
}

func TestParseRequestedBudgetsMap_CostBudgetRejectsNonPositive(t *testing.T) {
	cases := []struct {
		name string
		raw  map[string]any
	}{
		{"zero_cost", map[string]any{"max_cost_micros": float64(0)}},
		{"negative_cost", map[string]any{"max_cost_micros": float64(-1)}},
		{"zero_tokens", map[string]any{"max_total_output_tokens": float64(0)}},
		{"string_cost", map[string]any{"max_cost_micros": "abc"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseRequestedBudgetsMap(tc.raw)
			if err == nil {
				t.Fatalf("expected error for %v", tc.raw)
			}
		})
	}
}

func TestResolveEffectiveProfile_CostBudgetPropagation(t *testing.T) {
	costMicros := int64(10000000)
	outputTokens := int64(200000)
	persona := &PersonaProfile{
		Budgets: RequestedBudgets{
			MaxCostMicros:        &costMicros,
			MaxTotalOutputTokens: &outputTokens,
		},
	}
	profile := ResolveEffectiveProfile(PlatformLimits{ToolContinuationBudget: 32}, nil, persona)
	if profile.MaxCostMicros == nil || *profile.MaxCostMicros != 10000000 {
		t.Fatalf("expected MaxCostMicros=10000000, got %v", profile.MaxCostMicros)
	}
	if profile.MaxTotalOutputTokens == nil || *profile.MaxTotalOutputTokens != 200000 {
		t.Fatalf("expected MaxTotalOutputTokens=200000, got %v", profile.MaxTotalOutputTokens)
	}
}

func floatPtr(value float64) *float64 {
	return &value
}

func strPtr(value string) *string {
	return &value
}
