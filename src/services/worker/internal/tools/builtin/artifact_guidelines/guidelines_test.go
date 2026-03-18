package artifactguidelines

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"arkloop/services/worker/internal/tools"
	generativeuisource "arkloop/services/worker/internal/tools/builtin/generative_ui_source"
	visualizereadme "arkloop/services/worker/internal/tools/builtin/visualize_read_me"
)

func TestBuildGuidelines_ChartIncludesReferenceChartSection(t *testing.T) {
	doc, err := generativeuisource.BuildDocument([]string{"chart"})
	if err != nil {
		t.Fatalf("build document: %v", err)
	}
	got := doc.Content

	for _, want := range []string{
		`## Core Design System`,
		`https://cdnjs.cloudflare.com/ajax/libs/Chart.js/4.4.1/chart.umd.js`,
		`responsive: true`,
		`maintainAspectRatio: false`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("guidelines missing %q\n%s", want, got)
		}
	}
}

func TestBuildGuidelines_InteractiveAndChartDeduplicateSharedSections(t *testing.T) {
	doc, err := generativeuisource.BuildDocument([]string{"interactive", "chart"})
	if err != nil {
		t.Fatalf("build document: %v", err)
	}
	got := doc.Content

	if count := strings.Count(got, "## UI components"); count != 1 {
		t.Fatalf("expected ui components once, got %d", count)
	}
	if count := strings.Count(got, "## Color palette"); count != 1 {
		t.Fatalf("expected color palette once, got %d", count)
	}
}

func TestArtifactGuidelinesMatchesVisualizeReadMe(t *testing.T) {
	args := map[string]any{"modules": []any{"interactive", "mockup"}}

	legacy := ToolExecutor{}.Execute(context.Background(), "artifact_guidelines", args, tools.ExecutionContext{}, "call_legacy")
	canonical := visualizereadme.NewToolExecutor().Execute(context.Background(), "visualize_read_me", args, tools.ExecutionContext{}, "call_canonical")

	if legacy.Error != nil {
		t.Fatalf("legacy alias returned error: %+v", legacy.Error)
	}
	if canonical.Error != nil {
		t.Fatalf("canonical tool returned error: %+v", canonical.Error)
	}
	if !reflect.DeepEqual(legacy.ResultJSON, canonical.ResultJSON) {
		t.Fatalf("expected identical payloads\nlegacy=%#v\ncanonical=%#v", legacy.ResultJSON, canonical.ResultJSON)
	}
}
