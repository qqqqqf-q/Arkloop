package pipeline_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/personas"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

func TestPersonaResolutionPreferredCredentialSet(t *testing.T) {
	credName := "my-anthropic"
	reg := buildPersonaRegistry(t, personas.Definition{
		ID:                  "test-persona",
		Version:             "1",
		Title:               "Test Persona",
		SoulMD:              "persona soul",
		PromptMD:            "# test",
		ExecutorType:        "agent.simple",
		ExecutorConfig:      map[string]any{},
		PreferredCredential: &credName,
	})
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{InputJSON: map[string]any{"persona_id": "test-persona"}}

	var capturedCredName string
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		capturedCredName = rc.PreferredCredentialName
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedCredName != credName {
		t.Fatalf("expected PreferredCredentialName %q, got %q", credName, capturedCredName)
	}
}

func TestPersonaResolutionNoPreferredCredentialEmpty(t *testing.T) {
	reg := buildPersonaRegistry(t, personas.Definition{
		ID:             "test-persona",
		Version:        "1",
		Title:          "Test Persona",
		SoulMD:         "persona soul",
		PromptMD:       "# test",
		ExecutorType:   "agent.simple",
		ExecutorConfig: map[string]any{},
	})
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{InputJSON: map[string]any{"persona_id": "test-persona"}}

	var credName string
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		credName = rc.PreferredCredentialName
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if credName != "" {
		t.Fatalf("expected PreferredCredentialName empty, got %q", credName)
	}
}

func TestPersonaResolutionUserRouteIDNotAffectedByPersonaCredential(t *testing.T) {
	personaCred := "my-anthropic"
	userRouteID := "openai-gpt4"
	reg := buildPersonaRegistry(t, personas.Definition{
		ID:                  "test-persona",
		Version:             "1",
		Title:               "Test Persona",
		SoulMD:              "persona soul",
		PromptMD:            "# test",
		ExecutorType:        "agent.simple",
		ExecutorConfig:      map[string]any{},
		PreferredCredential: &personaCred,
	})
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
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		capturedRouteID = rc.InputJSON["route_id"]
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedRouteID != userRouteID {
		t.Fatalf("expected user route_id %q to be preserved, got %v", userRouteID, capturedRouteID)
	}
}

func TestPersonaResolutionLoadsModelReasoningAndPromptCache(t *testing.T) {
	model := "demo-cred^gpt-5-mini"
	reg := buildPersonaRegistry(t, personas.Definition{
		ID:                 "test-persona",
		Version:            "1",
		Title:              "Test Persona",
		SoulMD:             "persona soul",
		PromptMD:           "system prompt",
		ExecutorType:       "agent.simple",
		ExecutorConfig:     map[string]any{},
		Model:              &model,
		ReasoningMode:      "high",
		PromptCacheControl: "system_prompt",
	})
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{InputJSON: map[string]any{"persona_id": "test-persona"}}

	var gotConfig *pipeline.ResolvedAgentConfig
	var gotSystemPrompt string
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		gotConfig = rc.AgentConfig
		gotSystemPrompt = rc.SystemPrompt
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotConfig == nil {
		t.Fatal("expected AgentConfig to be populated from persona")
	}
	if gotConfig.Model == nil || *gotConfig.Model != model {
		t.Fatalf("unexpected model: %#v", gotConfig.Model)
	}
	if gotConfig.ReasoningMode != "high" {
		t.Fatalf("unexpected reasoning_mode: %q", gotConfig.ReasoningMode)
	}
	if gotConfig.PromptCacheControl != "system_prompt" {
		t.Fatalf("unexpected prompt_cache_control: %q", gotConfig.PromptCacheControl)
	}
	if gotSystemPrompt != "persona soul\n\nsystem prompt" {
		t.Fatalf("unexpected system prompt: %q", gotSystemPrompt)
	}
}

func TestPersonaResolutionAppendsPendingSubAgentCallbacksBlock(t *testing.T) {
	reg := buildPersonaRegistry(t, personas.Definition{
		ID:             "test-persona",
		Version:        "1",
		Title:          "Test Persona",
		SoulMD:         "persona soul",
		PromptMD:       "system prompt",
		ExecutorType:   "agent.simple",
		ExecutorConfig: map[string]any{},
	})
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{"persona_id": "test-persona"},
		PendingSubAgentCallbacks: []data.ThreadSubAgentCallbackRecord{
			{
				ID:         uuid.New(),
				SubAgentID: uuid.New(),
				Status:     data.SubAgentStatusCompleted,
				PayloadJSON: map[string]any{
					"message": "phase one done",
				},
			},
		},
	}

	var gotSystemPrompt string
	var gotRuntimePrompt string
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		gotSystemPrompt = rc.SystemPrompt
		gotRuntimePrompt = rc.RuntimePrompt
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotSystemPrompt != "persona soul\n\nsystem prompt" {
		t.Fatalf("expected base persona prompt in system prompt, got %q", gotSystemPrompt)
	}
	if want := "<pending_subagent_callbacks>"; !strings.Contains(gotRuntimePrompt, want) {
		t.Fatalf("expected %q in runtime prompt, got %q", want, gotRuntimePrompt)
	}
	if want := `"status":"completed"`; !strings.Contains(gotRuntimePrompt, want) {
		t.Fatalf("expected %q in runtime prompt, got %q", want, gotRuntimePrompt)
	}
	if want := `"message":"phase one done"`; !strings.Contains(gotRuntimePrompt, want) {
		t.Fatalf("expected callback payload in runtime prompt, got %q", gotRuntimePrompt)
	}
}

func TestPersonaResolutionRestoresPlanModePromptAfterReset(t *testing.T) {
	reg := buildPersonaRegistry(t, personas.Definition{
		ID:             "test-persona",
		Version:        "1",
		Title:          "Test Persona",
		SoulMD:         "persona soul",
		PromptMD:       "system prompt",
		ExecutorType:   "agent.simple",
		ExecutorConfig: map[string]any{},
	})
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	threadID := uuid.New()
	rc := &pipeline.RunContext{
		Run: data.Run{
			ThreadID: threadID,
		},
		InputJSON: map[string]any{
			"persona_id":         "test-persona",
			"collaboration_mode": "plan",
		},
	}
	pipeline.ApplyCollaborationMode(rc)

	var gotRuntimePrompt string
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		gotRuntimePrompt = rc.RuntimePrompt
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rc.IsPlanMode {
		t.Fatal("expected plan mode to remain active")
	}
	if !strings.Contains(gotRuntimePrompt, "<system-reminder>") {
		t.Fatalf("expected plan mode prompt after persona reset, got %q", gotRuntimePrompt)
	}
	if !strings.Contains(gotRuntimePrompt, "plans/"+threadID.String()+".md") {
		t.Fatalf("expected plan path in runtime prompt, got %q", gotRuntimePrompt)
	}
}

func TestPersonaResolutionRestoresLearningModePromptAfterReset(t *testing.T) {
	reg := buildPersonaRegistry(t, personas.Definition{
		ID:             "test-persona",
		Version:        "1",
		Title:          "Test Persona",
		SoulMD:         "persona soul",
		PromptMD:       "system prompt",
		ExecutorType:   "agent.simple",
		ExecutorConfig: map[string]any{},
	})
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{
			"persona_id":            "test-persona",
			"learning_mode_enabled": true,
		},
	}
	pipeline.ApplyLearningMode(rc)

	var gotRuntimePrompt string
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		gotRuntimePrompt = rc.RuntimePrompt
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rc.LearningModeEnabled {
		t.Fatal("expected learning mode to remain active")
	}
	if !strings.Contains(gotRuntimePrompt, "学习辅导已在当前 thread 启用") {
		t.Fatalf("expected learning prompt after persona reset, got %q", gotRuntimePrompt)
	}
	if !strings.Contains(gotRuntimePrompt, "不替换当前 persona") {
		t.Fatalf("expected persona overlay boundary in runtime prompt, got %q", gotRuntimePrompt)
	}
	if !strings.Contains(gotRuntimePrompt, "U-S-T") {
		t.Fatalf("expected teaching method in runtime prompt, got %q", gotRuntimePrompt)
	}
	if !strings.Contains(gotRuntimePrompt, "LaTeX") {
		t.Fatalf("expected math guidance in runtime prompt, got %q", gotRuntimePrompt)
	}
}

func TestPersonaResolutionLoadsSystemSummarizerConfig(t *testing.T) {
	reg := buildPersonaRegistry(t,
		personas.Definition{
			ID:             "test-persona",
			Version:        "1",
			Title:          "Test Persona",
			SoulMD:         "persona soul",
			PromptMD:       "# test",
			ExecutorType:   "agent.simple",
			ExecutorConfig: map[string]any{},
		},
		personas.Definition{
			ID:      personas.SystemSummarizerPersonaID,
			Version: "1",
			Title:   "Summarizer",
			TitleSummarizer: &personas.TitleSummarizerConfig{
				Prompt:    "title prompt",
				MaxTokens: 111,
			},
			ResultSummarizer: &personas.ResultSummarizerConfig{
				Prompt:         "result prompt",
				MaxTokens:      222,
				ThresholdBytes: 333,
			},
		},
	)
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{InputJSON: map[string]any{"persona_id": "test-persona"}}

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		if rc.TitleSummarizer == nil || rc.TitleSummarizer.Prompt != "title prompt" {
			t.Fatalf("unexpected title summarizer: %#v", rc.TitleSummarizer)
		}
		if rc.ResultSummarizer == nil || rc.ResultSummarizer.Prompt != "result prompt" {
			t.Fatalf("unexpected result summarizer: %#v", rc.ResultSummarizer)
		}
		if rc.ResultSummarizer.ThresholdBytes != 333 {
			t.Fatalf("unexpected result threshold: %d", rc.ResultSummarizer.ThresholdBytes)
		}
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPersonaResolutionAppliesBudgets(t *testing.T) {
	reg := buildPersonaRegistry(t, personas.Definition{
		ID:             "p1",
		Version:        "1",
		Title:          "Test",
		SoulMD:         "persona soul",
		PromptMD:       "test",
		ExecutorType:   "agent.simple",
		ExecutorConfig: map[string]any{},
		Budgets: personas.Budgets{
			ReasoningIterations:    intPtr(4),
			ToolContinuationBudget: intPtr(12),
			MaxOutputTokens:        intPtr(900),
			Temperature:            floatPtr(0.3),
			TopP:                   floatPtr(0.8),
		},
	})
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{
		InputJSON:                     map[string]any{"persona_id": "p1"},
		AgentReasoningIterationsLimit: 9,
		ToolContinuationBudgetLimit:   18,
	}

	var (
		gotReasoningIterations int
		gotToolContinuation    int
		gotMaxOutputTokens     *int
		gotTemperature         *float64
		gotTopP                *float64
	)
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		gotReasoningIterations = rc.ReasoningIterations
		gotToolContinuation = rc.ToolContinuationBudget
		gotMaxOutputTokens = rc.MaxOutputTokens
		gotTemperature = rc.Temperature
		gotTopP = rc.TopP
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotReasoningIterations != 4 {
		t.Fatalf("unexpected reasoning iterations: %d", gotReasoningIterations)
	}
	if gotToolContinuation != 12 {
		t.Fatalf("unexpected tool continuation budget: %d", gotToolContinuation)
	}
	if gotMaxOutputTokens == nil || *gotMaxOutputTokens != 900 {
		t.Fatalf("unexpected max output tokens: %#v", gotMaxOutputTokens)
	}
	if gotTemperature == nil || *gotTemperature != 0.3 {
		t.Fatalf("unexpected temperature: %#v", gotTemperature)
	}
	if gotTopP == nil || *gotTopP != 0.8 {
		t.Fatalf("unexpected top_p: %#v", gotTopP)
	}
}

func TestPersonaResolutionToolAllowlistAndDenylist(t *testing.T) {
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

	reg := buildPersonaRegistry(t, personas.Definition{
		ID:             "p1",
		Version:        "1",
		Title:          "Test",
		PromptMD:       "test",
		ExecutorType:   "agent.simple",
		ExecutorConfig: map[string]any{},
		ToolAllowlist:  []string{"tool_b", "tool_c"},
		ToolDenylist:   []string{"tool_c"},
	})
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{"persona_id": "p1"},
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
		t.Fatalf("tool_a should not be in persona allowlist, got %v", gotAllowlist)
	}
	if _, ok := gotAllowlist["tool_c"]; ok {
		t.Fatalf("tool_c should be removed by denylist, got %v", gotAllowlist)
	}
}

func TestPersonaResolutionAppliesRoleOverlay(t *testing.T) {
	model := "worker-cred^gpt-5-mini"
	credential := "worker-cred"
	reg := buildPersonaRegistry(t, personas.Definition{
		ID:             "p1",
		Version:        "1",
		Title:          "Test",
		SoulMD:         "base soul",
		PromptMD:       "base prompt",
		ExecutorType:   "agent.simple",
		ExecutorConfig: map[string]any{},
		Roles: map[string]personas.RoleOverride{
			"worker": {
				SoulMD:              personas.StringOverride{Set: true, Value: "worker soul"},
				PromptMD:            personas.StringOverride{Set: true, Value: "worker prompt"},
				HasToolAllowlist:    true,
				ToolAllowlist:       []string{"tool_b"},
				HasToolDenylist:     true,
				ToolDenylist:        []string{"tool_c"},
				Model:               personas.OptionalStringOverride{Set: true, Value: &model},
				PreferredCredential: personas.OptionalStringOverride{Set: true, Value: &credential},
				ReasoningMode:       personas.EnumStringOverride{Set: true, Value: "high"},
				PromptCacheControl:  personas.EnumStringOverride{Set: true, Value: "system_prompt"},
				Budgets: personas.BudgetsOverride{
					HasMaxOutputTokens: true,
					MaxOutputTokens:    intPtr(256),
				},
			},
		},
	})
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{
		InputJSON: map[string]any{"persona_id": "p1", "role": "worker"},
		AllowlistSet: map[string]struct{}{
			"tool_a": {},
			"tool_b": {},
			"tool_c": {},
		},
		ToolRegistry: tools.NewRegistry(),
	}
	for _, spec := range []tools.AgentToolSpec{
		{Name: "tool_a", Version: "1", Description: "a", RiskLevel: tools.RiskLevelLow},
		{Name: "tool_b", Version: "1", Description: "b", RiskLevel: tools.RiskLevelLow},
		{Name: "tool_c", Version: "1", Description: "c", RiskLevel: tools.RiskLevelLow},
	} {
		if err := rc.ToolRegistry.Register(spec); err != nil {
			t.Fatalf("register tool: %v", err)
		}
	}

	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		if rc.SystemPrompt != "base soul\n\nbase prompt\n\nworker soul\n\nworker prompt" {
			return fmt.Errorf("unexpected system prompt: %q", rc.SystemPrompt)
		}
		if rc.AgentConfig == nil || rc.AgentConfig.Model == nil || *rc.AgentConfig.Model != model {
			return fmt.Errorf("unexpected model: %#v", rc.AgentConfig)
		}
		if rc.PreferredCredentialName != credential {
			return fmt.Errorf("unexpected credential: %q", rc.PreferredCredentialName)
		}
		if rc.AgentConfig.PromptCacheControl != "system_prompt" {
			return fmt.Errorf("unexpected prompt cache control: %q", rc.AgentConfig.PromptCacheControl)
		}
		if rc.AgentConfig.ReasoningMode != "high" {
			return fmt.Errorf("unexpected reasoning mode: %q", rc.AgentConfig.ReasoningMode)
		}
		if rc.MaxOutputTokens == nil || *rc.MaxOutputTokens != 256 {
			return fmt.Errorf("unexpected max output tokens: %#v", rc.MaxOutputTokens)
		}
		if _, ok := rc.AllowlistSet["tool_b"]; !ok {
			return fmt.Errorf("tool_b missing from allowlist: %#v", rc.AllowlistSet)
		}
		if _, ok := rc.AllowlistSet["tool_a"]; ok {
			return fmt.Errorf("tool_a should be removed: %#v", rc.AllowlistSet)
		}
		if _, ok := rc.AllowlistSet["tool_c"]; ok {
			return fmt.Errorf("tool_c should be denied: %#v", rc.AllowlistSet)
		}
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPersonaResolutionUnknownRoleUsesBasePersona(t *testing.T) {
	reg := buildPersonaRegistry(t, personas.Definition{
		ID:             "p1",
		Version:        "1",
		Title:          "Test",
		SoulMD:         "base soul",
		PromptMD:       "base prompt",
		ExecutorType:   "agent.simple",
		ExecutorConfig: map[string]any{},
		Roles: map[string]personas.RoleOverride{
			"worker": {PromptMD: personas.StringOverride{Set: true, Value: "worker prompt"}},
		},
	})
	mw := pipeline.NewPersonaResolutionMiddleware(
		func() *personas.Registry { return reg },
		nil, data.RunsRepository{}, data.RunEventsRepository{}, nil,
	)

	rc := &pipeline.RunContext{InputJSON: map[string]any{"persona_id": "p1", "role": "reviewer"}}
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		if rc.SystemPrompt != "base soul\n\nbase prompt" {
			return fmt.Errorf("unexpected system prompt: %q", rc.SystemPrompt)
		}
		return nil
	})
	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func buildPersonaRegistry(t *testing.T, defs ...personas.Definition) *personas.Registry {
	t.Helper()
	reg := personas.NewRegistry()
	for _, def := range defs {
		if err := reg.Register(def); err != nil {
			t.Fatalf("register persona failed: %v", err)
		}
	}
	return reg
}

func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }

func strPtr(v string) *string { return &v }
