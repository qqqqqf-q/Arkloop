package pipeline_test

import (
	"context"
	"errors"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/tools"
	spawnagent "arkloop/services/worker/internal/tools/builtin/spawn_agent"
	websearch "arkloop/services/worker/internal/tools/builtin/web_search"

	"github.com/google/uuid"
)

type stubToolDescriptionOverridesRepo struct {
	platform    []data.ToolDescriptionOverride
	project     []data.ToolDescriptionOverride
	platformErr error
	projectErr  error
}

func (s stubToolDescriptionOverridesRepo) ListByScope(_ context.Context, _ *uuid.UUID, scope string) ([]data.ToolDescriptionOverride, error) {
	switch scope {
	case "platform":
		return s.platform, s.platformErr
	case "project":
		return s.project, s.projectErr
	default:
		return nil, nil
	}
}

func TestToolDescriptionOverrideMiddlewareAppliesPlatformAndOrg(t *testing.T) {
	projectID := uuid.New()
	repo := stubToolDescriptionOverridesRepo{
		platform: []data.ToolDescriptionOverride{
			{ToolName: "web_search", Description: "platform search"},
			{ToolName: "spawn_agent", Description: "platform spawn"},
		},
		project: []data.ToolDescriptionOverride{
			{ToolName: "spawn_agent", Description: "project spawn"},
		},
	}

	rc := &pipeline.RunContext{
		Run: data.Run{ID: uuid.New(), OrgID: uuid.New(), ProjectID: &projectID},
		ToolSpecs: []llm.ToolSpec{
			websearch.LlmSpec,
			spawnagent.LlmSpec,
			{Name: "mcp_external", Description: stringPtr("external description")},
		},
	}

	mw := pipeline.NewToolDescriptionOverrideMiddleware(repo)
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		if got := deref(rc.ToolSpecs[0].Description); got != "platform search" {
			t.Fatalf("unexpected web_search description: %s", got)
		}
		if got := deref(rc.ToolSpecs[1].Description); got != "project spawn" {
			t.Fatalf("unexpected spawn_agent description: %s", got)
		}
		if got := deref(rc.ToolSpecs[2].Description); got != "external description" {
			t.Fatalf("mcp description should stay unchanged, got %s", got)
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("middleware error: %v", err)
	}
}

func TestToolDescriptionOverrideMiddlewareFailsOpenOnRepoError(t *testing.T) {
	projectID := uuid.New()
	repo := stubToolDescriptionOverridesRepo{
		platform: []data.ToolDescriptionOverride{{ToolName: "web_search", Description: "platform search"}},
		projectErr: errors.New("boom"),
	}

	rc := &pipeline.RunContext{
		Run: data.Run{ID: uuid.New(), OrgID: uuid.New(), ProjectID: &projectID},
		ToolSpecs: []llm.ToolSpec{
			websearch.LlmSpec,
		},
	}
	defaultDescription := deref(websearch.LlmSpec.Description)

	mw := pipeline.NewToolDescriptionOverrideMiddleware(repo)
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		if got := deref(rc.ToolSpecs[0].Description); got != defaultDescription {
			t.Fatalf("expected default description on repo failure, got %s", got)
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("middleware error: %v", err)
	}
}

func TestToolDescriptionOverrideMiddlewareRemovesDisabledTools(t *testing.T) {
	repo := stubToolDescriptionOverridesRepo{
		platform: []data.ToolDescriptionOverride{{ToolName: "document_write", IsDisabled: true}},
	}

	registry := tools.NewRegistry()
	if err := registry.Register(tools.AgentToolSpec{Name: "document_write", Version: "1", Description: "doc", RiskLevel: tools.RiskLevelLow}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	rc := &pipeline.RunContext{
		Run:          data.Run{ID: uuid.New(), OrgID: uuid.New()},
		ToolRegistry: registry,
		AllowlistSet: map[string]struct{}{"document_write": {}},
		ToolSpecs:    []llm.ToolSpec{{Name: "document_write", Description: stringPtr("doc")}},
	}

	mw := pipeline.NewToolDescriptionOverrideMiddleware(repo)
	h := pipeline.Build([]pipeline.RunMiddleware{mw}, func(_ context.Context, rc *pipeline.RunContext) error {
		if _, ok := rc.AllowlistSet["document_write"]; ok {
			t.Fatal("document_write should be removed from allowlist")
		}
		return nil
	})

	if err := h(context.Background(), rc); err != nil {
		t.Fatalf("middleware error: %v", err)
	}
}

func stringPtr(value string) *string { return &value }

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
