package executionconfig

import "testing"

func TestResolveEffectiveProfileClampsBudgets(t *testing.T) {
	profile := ResolveEffectiveProfile(
		PlatformLimits{AgentReasoningIterations: 10, ToolContinuationBudget: 32},
		&AgentConfigProfile{Name: "org-default", MaxOutputTokens: intPtr(4096), Temperature: floatPtr(0.2), TopP: floatPtr(0.8), ReasoningMode: "auto"},
		&PersonaProfile{
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

func TestResolveEffectiveProfilePrefersPersonaNamedAgentConfig(t *testing.T) {
	profile := ResolveEffectiveProfile(
		PlatformLimits{},
		&AgentConfigProfile{Name: "platform-default", MaxOutputTokens: intPtr(2048)},
		&PersonaProfile{ResolvedAgentConfigName: strPtr("named-config")},
	)
	if profile.ResolvedAgentConfigName != "named-config" {
		t.Fatalf("unexpected resolved agent config name: %q", profile.ResolvedAgentConfigName)
	}
	if profile.ReasoningIterations != 10 {
		t.Fatalf("unexpected default reasoning limit: %d", profile.ReasoningIterations)
	}
	if profile.ToolContinuationBudget != 32 {
		t.Fatalf("unexpected default continuation limit: %d", profile.ToolContinuationBudget)
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

func floatPtr(value float64) *float64 {
	return &value
}

func strPtr(value string) *string {
	return &value
}
