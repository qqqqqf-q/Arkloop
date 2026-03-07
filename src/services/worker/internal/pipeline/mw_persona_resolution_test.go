package pipeline_test

import (
	"context"
	"fmt"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/tools"
)

// TestPersonaResolutionPreferredCredentialSet 验证 persona 有 preferred_credential 时，设置 rc.PreferredCredentialName。
func TestPersonaResolutionPreferredCredentialSet(t *testing.T) {
	credName := "my-anthropic"
	reg := buildPersonaRegistry(t, "test-persona", &credName)
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{"persona_id": "test-persona"},
	}

	var capturedCredName string
	terminal := func(ctx context.Context, rc *pipeline.RunContext) error {
		capturedCredName = rc.PreferredCredentialName
		return nil
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, terminal)
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedCredName != credName {
		t.Fatalf("expected PreferredCredentialName %q, got %q", credName, capturedCredName)
	}
}

// TestPersonaResolutionNoPreferredCredentialEmpty 验证 persona 无 preferred_credential 时，PreferredCredentialName 为空。
func TestPersonaResolutionNoPreferredCredentialEmpty(t *testing.T) {
	reg := buildPersonaRegistry(t, "test-persona", nil)
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{"persona_id": "test-persona"},
	}

	var credName string
	terminal := func(ctx context.Context, rc *pipeline.RunContext) error {
		credName = rc.PreferredCredentialName
		return nil
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, terminal)
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if credName != "" {
		t.Fatalf("expected PreferredCredentialName empty, got %q", credName)
	}
}

// TestPersonaResolutionUserRouteIDNotAffectedByPersonaCredential 验证用户显式传 route_id 时不被 persona credential 覆盖。
func TestPersonaResolutionUserRouteIDNotAffectedByPersonaCredential(t *testing.T) {
	personaCred := "my-anthropic"
	userRouteID := "openai-gpt4"
	reg := buildPersonaRegistry(t, "test-persona", &personaCred)
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{
			"persona_id": "test-persona",
			"route_id":   userRouteID,
		},
	}

	var capturedRouteID any
	terminal := func(ctx context.Context, rc *pipeline.RunContext) error {
		capturedRouteID = rc.InputJSON["route_id"]
		return nil
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, terminal)
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedRouteID != userRouteID {
		t.Fatalf("expected user route_id %q to be preserved, got %v", userRouteID, capturedRouteID)
	}
}

func buildPersonaRegistry(t *testing.T, id string, preferredCredential *string) *personas.Registry {
	t.Helper()
	return buildPersonaRegistryFull(t, id, preferredCredential, nil)
}

func buildPersonaRegistryFull(t *testing.T, id string, preferredCredential *string, agentConfigName *string) *personas.Registry {
	t.Helper()
	reg := personas.NewRegistry()
	def := personas.Definition{
		ID:                  id,
		Version:             "1",
		Title:               "Test Persona",
		PromptMD:            "# test",
		ExecutorType:        "agent.simple",
		ExecutorConfig:      map[string]any{},
		PreferredCredential: preferredCredential,
		AgentConfigName:     agentConfigName,
	}
	if err := reg.Register(def); err != nil {
		t.Fatalf("register persona failed: %v", err)
	}
	return reg
}

// TestPersonaResolutionAgentConfigNameNilPreservesInheritance 验证 persona 无 agent_config_name 时，rc.AgentConfig 保持继承链结果不变。
func TestPersonaResolutionAgentConfigNameNilPreservesInheritance(t *testing.T) {
	reg := buildPersonaRegistryFull(t, "test-persona", nil, nil)
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	existing := &pipeline.ResolvedAgentConfig{Model: strPtr("inherited-model")}
	rc := &pipeline.RunContext{
		InputJSON:   map[string]any{"persona_id": "test-persona"},
		AgentConfig: existing,
	}

	var capturedConfig *pipeline.ResolvedAgentConfig
	terminal := func(ctx context.Context, rc *pipeline.RunContext) error {
		capturedConfig = rc.AgentConfig
		return nil
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, terminal)
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedConfig != existing {
		t.Fatalf("expected AgentConfig to be unchanged (inherited), got different pointer")
	}
}

func strPtr(s string) *string     { return &s }
func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }

// buildPersonaRegistryWithBudgets 构建带完整 Budgets 的 persona registry。
func buildPersonaRegistryWithBudgets(
	t *testing.T,
	id string,
	promptMD string,
	budgets personas.Budgets,
	toolAllowlist []string,
	toolDenylist []string,
) *personas.Registry {
	t.Helper()
	reg := personas.NewRegistry()
	def := personas.Definition{
		ID:             id,
		Version:        "1",
		Title:          "Test",
		PromptMD:       promptMD,
		ExecutorType:   "agent.simple",
		ExecutorConfig: map[string]any{},
		Budgets:        budgets,
		ToolAllowlist:  toolAllowlist,
		ToolDenylist:   toolDenylist,
	}
	if err := reg.Register(def); err != nil {
		t.Fatalf("register persona: %v", err)
	}
	return reg
}

// TestPersonaResolutionSystemPromptLayering 验证 SystemPrompt 的分层拼接逻辑。
func TestPersonaResolutionSystemPromptLayering(t *testing.T) {
	tests := []struct {
		name             string
		agentPrompt      *string
		personaPrompt    string
		wantSystemPrompt string
	}{
		{
			name:             "prefix_and_suffix",
			agentPrompt:      strPtr("agent-prefix"),
			personaPrompt:    "persona-body",
			wantSystemPrompt: "agent-prefix\n\npersona-body",
		},
		{
			name:             "only_persona",
			agentPrompt:      nil,
			personaPrompt:    "persona-only",
			wantSystemPrompt: "persona-only",
		},
		{
			name:             "only_agent_config",
			agentPrompt:      strPtr("agent-only"),
			personaPrompt:    "",
			wantSystemPrompt: "agent-only",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := buildPersonaRegistryWithBudgets(t, "p1", tt.personaPrompt, personas.Budgets{}, nil, nil)
			mw := pipeline.NewPersonaResolutionMiddleware(
				func() *personas.Registry { return reg },
				nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
			)

			var agentConfig *pipeline.ResolvedAgentConfig
			if tt.agentPrompt != nil {
				agentConfig = &pipeline.ResolvedAgentConfig{SystemPrompt: tt.agentPrompt}
			}

			rc := &pipeline.RunContext{
				InputJSON:   map[string]any{"persona_id": "p1"},
				AgentConfig: agentConfig,
			}

			var got string
			h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
				got = rc.SystemPrompt
				return nil
			})
			if err := h(context.Background(), rc); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantSystemPrompt {
				t.Fatalf("SystemPrompt = %q, want %q", got, tt.wantSystemPrompt)
			}
		})
	}
}

// TestPersonaResolutionReasoningIterationsClamping 验证 ReasoningIterations 被 Persona 值钳制但不超过平台上限。
func TestPersonaResolutionReasoningIterationsClamping(t *testing.T) {
	tests := []struct {
		name          string
		platformLimit int
		personaVal    *int
		want          int
	}{
		{
			name:          "persona_below_limit",
			platformLimit: 10,
			personaVal:    intPtr(5),
			want:          5,
		},
		{
			name:          "persona_above_limit_clamped",
			platformLimit: 10,
			personaVal:    intPtr(20),
			want:          10,
		},
		{
			name:          "persona_nil_uses_limit",
			platformLimit: 10,
			personaVal:    nil,
			want:          10,
		},
		{
			name:          "platform_unlimited_persona_sets_limit",
			platformLimit: 0,
			personaVal:    intPtr(5),
			want:          5,
		},
		{
			name:          "platform_unlimited_persona_nil_keeps_unlimited",
			platformLimit: 0,
			personaVal:    nil,
			want:          0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := buildPersonaRegistryWithBudgets(t, "p1", "test", personas.Budgets{
				ReasoningIterations: tt.personaVal,
			}, nil, nil)
			mw := pipeline.NewPersonaResolutionMiddleware(
				func() *personas.Registry { return reg },
				nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
			)

			rc := &pipeline.RunContext{
				InputJSON:                     map[string]any{"persona_id": "p1"},
				AgentReasoningIterationsLimit: tt.platformLimit,
			}

			var got int
			h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
				got = rc.ReasoningIterations
				return nil
			})
			if err := h(context.Background(), rc); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ReasoningIterations = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPersonaResolutionToolContinuationBudgetClamping(t *testing.T) {
	tests := []struct {
		name          string
		platformLimit int
		personaVal    *int
		want          int
	}{
		{name: "persona_below_limit", platformLimit: 32, personaVal: intPtr(8), want: 8},
		{name: "persona_above_limit_clamped", platformLimit: 32, personaVal: intPtr(64), want: 32},
		{name: "persona_nil_uses_limit", platformLimit: 32, personaVal: nil, want: 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := buildPersonaRegistryWithBudgets(t, "p1", "test", personas.Budgets{
				ToolContinuationBudget: tt.personaVal,
			}, nil, nil)
			mw := pipeline.NewPersonaResolutionMiddleware(
				func() *personas.Registry { return reg },
				nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
			)

			rc := &pipeline.RunContext{
				InputJSON:                   map[string]any{"persona_id": "p1"},
				ToolContinuationBudgetLimit: tt.platformLimit,
			}

			var got int
			h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
				got = rc.ToolContinuationBudget
				return nil
			})
			if err := h(context.Background(), rc); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ToolContinuationBudget = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPersonaResolutionMergesPerToolSoftLimits(t *testing.T) {
	reg := buildPersonaRegistryWithBudgets(t, "p1", "test", personas.Budgets{
		PerToolSoftLimits: tools.PerToolSoftLimits{
			"write_stdin": {
				MaxContinuations: intPtr(9),
				MaxYieldTimeMs:   intPtr(2500),
			},
		},
	}, nil, nil)
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{InputJSON: map[string]any{"persona_id": "p1"}}
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		execLimit := rc.PerToolSoftLimits["exec_command"]
		if execLimit.MaxOutputBytes == nil || *execLimit.MaxOutputBytes != tools.DefaultExecCommandMaxOutputBytes {
			return fmt.Errorf("unexpected exec_command max_output_bytes: %v", execLimit.MaxOutputBytes)
		}
		writeLimit := rc.PerToolSoftLimits["write_stdin"]
		if writeLimit.MaxContinuations == nil || *writeLimit.MaxContinuations != 9 {
			return fmt.Errorf("unexpected write_stdin max_continuations: %v", writeLimit.MaxContinuations)
		}
		if writeLimit.MaxYieldTimeMs == nil || *writeLimit.MaxYieldTimeMs != 2500 {
			return fmt.Errorf("unexpected write_stdin max_yield_time_ms: %v", writeLimit.MaxYieldTimeMs)
		}
		if writeLimit.MaxOutputBytes == nil || *writeLimit.MaxOutputBytes != tools.DefaultWriteStdinMaxOutputBytes {
			return fmt.Errorf("unexpected write_stdin max_output_bytes: %v", writeLimit.MaxOutputBytes)
		}
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
}

// TestPersonaResolutionMaxOutputTokensCapping 验证 MaxOutputTokens 受 AgentConfig 上界约束。
func TestPersonaResolutionMaxOutputTokensCapping(t *testing.T) {
	tests := []struct {
		name       string
		agentMax   *int
		personaMax *int
		want       *int
	}{
		{
			name:       "persona_below_agent",
			agentMax:   intPtr(4096),
			personaMax: intPtr(2048),
			want:       intPtr(2048),
		},
		{
			name:       "persona_above_agent_capped",
			agentMax:   intPtr(4096),
			personaMax: intPtr(8192),
			want:       intPtr(4096),
		},
		{
			name:       "agent_nil_persona_set",
			agentMax:   nil,
			personaMax: intPtr(2048),
			want:       intPtr(2048),
		},
		{
			name:       "persona_nil_uses_agent",
			agentMax:   intPtr(4096),
			personaMax: nil,
			want:       intPtr(4096),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := buildPersonaRegistryWithBudgets(t, "p1", "test", personas.Budgets{
				MaxOutputTokens: tt.personaMax,
			}, nil, nil)
			mw := pipeline.NewPersonaResolutionMiddleware(
				func() *personas.Registry { return reg },
				nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
			)

			var agentConfig *pipeline.ResolvedAgentConfig
			if tt.agentMax != nil {
				agentConfig = &pipeline.ResolvedAgentConfig{MaxOutputTokens: tt.agentMax}
			}

			rc := &pipeline.RunContext{
				InputJSON:   map[string]any{"persona_id": "p1"},
				AgentConfig: agentConfig,
			}

			var got *int
			h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
				got = rc.MaxOutputTokens
				return nil
			})
			if err := h(context.Background(), rc); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.want == nil {
				if got != nil {
					t.Fatalf("MaxOutputTokens = %d, want nil", *got)
				}
			} else {
				if got == nil {
					t.Fatalf("MaxOutputTokens = nil, want %d", *tt.want)
				}
				if *got != *tt.want {
					t.Fatalf("MaxOutputTokens = %d, want %d", *got, *tt.want)
				}
			}
		})
	}
}

// TestPersonaResolutionTemperatureTopPOverride 验证 Persona 的 Temperature/TopP 覆盖 AgentConfig。
func TestPersonaResolutionTemperatureTopPOverride(t *testing.T) {
	tests := []struct {
		name        string
		agentTemp   *float64
		personaTemp *float64
		agentTopP   *float64
		personaTopP *float64
		wantTemp    *float64
		wantTopP    *float64
	}{
		{
			name:        "persona_overrides_agent",
			agentTemp:   floatPtr(0.7),
			personaTemp: floatPtr(0.3),
			agentTopP:   floatPtr(0.9),
			personaTopP: floatPtr(0.5),
			wantTemp:    floatPtr(0.3),
			wantTopP:    floatPtr(0.5),
		},
		{
			name:        "persona_nil_uses_agent",
			agentTemp:   floatPtr(0.7),
			personaTemp: nil,
			agentTopP:   floatPtr(0.9),
			personaTopP: nil,
			wantTemp:    floatPtr(0.7),
			wantTopP:    floatPtr(0.9),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := buildPersonaRegistryWithBudgets(t, "p1", "test", personas.Budgets{
				Temperature: tt.personaTemp,
				TopP:        tt.personaTopP,
			}, nil, nil)
			mw := pipeline.NewPersonaResolutionMiddleware(
				func() *personas.Registry { return reg },
				nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
			)

			rc := &pipeline.RunContext{
				InputJSON: map[string]any{"persona_id": "p1"},
				AgentConfig: &pipeline.ResolvedAgentConfig{
					Temperature: tt.agentTemp,
					TopP:        tt.agentTopP,
				},
			}

			var gotTemp, gotTopP *float64
			h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
				gotTemp = rc.Temperature
				gotTopP = rc.TopP
				return nil
			})
			if err := h(context.Background(), rc); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			checkFloat := func(label string, got, want *float64) {
				t.Helper()
				if want == nil {
					if got != nil {
						t.Fatalf("%s = %f, want nil", label, *got)
					}
					return
				}
				if got == nil {
					t.Fatalf("%s = nil, want %f", label, *want)
				}
				if *got != *want {
					t.Fatalf("%s = %f, want %f", label, *got, *want)
				}
			}
			checkFloat("Temperature", gotTemp, tt.wantTemp)
			checkFloat("TopP", gotTopP, tt.wantTopP)
		})
	}
}

// TestPersonaResolutionToolAllowlistIntersection 验证 Persona allowlist 与 AgentConfig 缩窄池的交集。
func TestPersonaResolutionToolAllowlistIntersection(t *testing.T) {
	registry := tools.NewRegistry()
	for _, spec := range []tools.AgentToolSpec{
		{Name: "tool_a", Version: "1", Description: "a", RiskLevel: tools.RiskLevelLow},
		{Name: "tool_b", Version: "1", Description: "b", RiskLevel: tools.RiskLevelLow},
		{Name: "tool_c", Version: "1", Description: "c", RiskLevel: tools.RiskLevelLow},
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatalf("register tool: %v", err)
		}
	}

	reg := buildPersonaRegistryWithBudgets(t, "p1", "test", personas.Budgets{}, []string{"tool_b", "tool_c"}, nil)
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{"persona_id": "p1"},
		AgentConfig: &pipeline.ResolvedAgentConfig{
			ToolPolicy:    "allowlist",
			ToolAllowlist: []string{"tool_a", "tool_b"},
		},
		AllowlistSet: map[string]struct{}{
			"tool_a": {},
			"tool_b": {},
			"tool_c": {},
		},
		ToolRegistry: registry,
	}

	var gotAllowlist map[string]struct{}
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		gotAllowlist = rc.AllowlistSet
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := gotAllowlist["tool_b"]; !ok {
		t.Fatalf("expected tool_b in allowlist, got %v", gotAllowlist)
	}
	if _, ok := gotAllowlist["tool_a"]; ok {
		t.Fatalf("tool_a should not be in persona-narrowed allowlist, got %v", gotAllowlist)
	}
	if _, ok := gotAllowlist["tool_c"]; ok {
		t.Fatalf("tool_c should not survive AgentConfig narrowing, got %v", gotAllowlist)
	}
}

// TestPersonaResolutionToolDenylistRemoval 验证 Persona denylist 从当前池中移除工具。
func TestPersonaResolutionToolDenylistRemoval(t *testing.T) {
	registry := tools.NewRegistry()
	for _, spec := range []tools.AgentToolSpec{
		{Name: "tool_a", Version: "1", Description: "a", RiskLevel: tools.RiskLevelLow},
		{Name: "tool_b", Version: "1", Description: "b", RiskLevel: tools.RiskLevelLow},
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatalf("register tool: %v", err)
		}
	}

	reg := buildPersonaRegistryWithBudgets(t, "p1", "test", personas.Budgets{}, nil, []string{"tool_a"})
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{"persona_id": "p1"},
		AllowlistSet: map[string]struct{}{
			"tool_a": {},
			"tool_b": {},
		},
		ToolRegistry: registry,
	}

	var gotAllowlist map[string]struct{}
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		gotAllowlist = rc.AllowlistSet
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := gotAllowlist["tool_a"]; ok {
		t.Fatalf("tool_a should be denied, got %v", gotAllowlist)
	}
	if _, ok := gotAllowlist["tool_b"]; !ok {
		t.Fatalf("tool_b should remain, got %v", gotAllowlist)
	}
}
