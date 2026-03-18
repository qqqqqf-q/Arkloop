package generativeuisource

import (
	"slices"
	"strings"
	"testing"
)

func TestAvailableModulesIncludesCanonicalSet(t *testing.T) {
	modules := AvailableModules()
	for _, want := range []string{"interactive", "chart", "mockup", "art", "diagram"} {
		if !slices.Contains(modules, want) {
			t.Fatalf("expected module %q in %v", want, modules)
		}
	}
}

func TestBuildDocumentSupportsMockupModule(t *testing.T) {
	doc, err := BuildDocument([]string{"mockup"})
	if err != nil {
		t.Fatalf("build document: %v", err)
	}
	if len(doc.Modules) != 1 || doc.Modules[0] != "mockup" {
		t.Fatalf("unexpected modules: %v", doc.Modules)
	}
	if !strings.Contains(doc.Content, "## UI components") {
		t.Fatalf("mockup document missing UI components section\n%s", doc.Content)
	}
}

func TestBuildDocumentNormalizesModuleOrderAndDeduplicatesInput(t *testing.T) {
	doc, err := BuildDocument([]string{" mockup ", "interactive", "mockup", "unknown"})
	if err != nil {
		t.Fatalf("build document: %v", err)
	}
	if !slices.Equal(doc.Modules, []string{"mockup", "interactive"}) {
		t.Fatalf("unexpected normalized modules: %v", doc.Modules)
	}
	if count := strings.Count(doc.Content, "## UI components"); count != 1 {
		t.Fatalf("expected UI components section once, got %d", count)
	}
}
