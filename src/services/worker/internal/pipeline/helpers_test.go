package pipeline_test

import (
	"testing"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/tools"
	understandimage "arkloop/services/worker/internal/tools/builtin/understand_image"
)

func TestFilterToolSpecsDedupesToLlmGroupName(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(tools.AgentToolSpec{
		Name:        "web_search.tavily",
		LlmName:     "web_search",
		Version:     "1",
		Description: "x",
		RiskLevel:   tools.RiskLevelLow,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	specs := []llm.ToolSpec{
		{Name: "web_search"},
		{Name: "web_fetch"},
	}
	allow := map[string]struct{}{
		"web_search.tavily": {},
	}

	filtered := pipeline.FilterToolSpecs(specs, allow, registry)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(filtered))
	}
	if filtered[0].Name != "web_search" {
		t.Fatalf("expected web_search, got %q", filtered[0].Name)
	}
}

func TestResolveProviderAllowlistAmbiguousProviders(t *testing.T) {
	registry := tools.NewRegistry()
	for _, spec := range []tools.AgentToolSpec{
		{
			Name:        "web_search.tavily",
			LlmName:     "web_search",
			Version:     "1",
			Description: "x",
			RiskLevel:   tools.RiskLevelLow,
		},
		{
			Name:        "web_search.searxng",
			LlmName:     "web_search",
			Version:     "1",
			Description: "x",
			RiskLevel:   tools.RiskLevelLow,
		},
	} {
		if err := registry.Register(spec); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	allow := map[string]struct{}{
		"web_search.tavily":  {},
		"web_search.searxng": {},
	}

	_, err := pipeline.ResolveProviderAllowlist(allow, registry, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestResolveProviderAllowlistDbActiveOverridesLegacyGroup(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(tools.AgentToolSpec{
		Name:        "web_fetch",
		Version:     "1",
		Description: "x",
		RiskLevel:   tools.RiskLevelLow,
	}); err != nil {
		t.Fatalf("register legacy: %v", err)
	}
	if err := registry.Register(tools.AgentToolSpec{
		Name:        "web_fetch.basic",
		LlmName:     "web_fetch",
		Version:     "1",
		Description: "x",
		RiskLevel:   tools.RiskLevelLow,
	}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	allow := map[string]struct{}{
		"web_fetch": {},
	}
	active := map[string]string{
		"web_fetch": "web_fetch.basic",
	}

	resolved, err := pipeline.ResolveProviderAllowlist(allow, registry, active)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := resolved["web_fetch.basic"]; !ok {
		t.Fatalf("expected web_fetch.basic in resolved allowlist, got %+v", resolved)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved tool, got %d", len(resolved))
	}
}

func TestResolveProviderAllowlistMapsUnderstandImageToolToProviderGroup(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(understandimage.AgentSpec); err != nil {
		t.Fatalf("register legacy: %v", err)
	}
	if err := registry.Register(understandimage.AgentSpecMiniMax); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	allow := map[string]struct{}{
		"understand_image": {},
	}
	active := map[string]string{
		"image_understanding": understandimage.ProviderNameMiniMax,
	}

	resolved, err := pipeline.ResolveProviderAllowlist(allow, registry, active)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := resolved[understandimage.ProviderNameMiniMax]; !ok {
		t.Fatalf("expected %s in resolved allowlist, got %+v", understandimage.ProviderNameMiniMax, resolved)
	}
	if !pipeline.ToolAllowed(allow, registry, understandimage.ProviderNameMiniMax) {
		t.Fatalf("expected provider tool to be allowed via understand_image group")
	}
}
