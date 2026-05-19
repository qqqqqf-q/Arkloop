package mcp

import (
	"testing"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestConvertAnnotationsNil(t *testing.T) {
	result := convertAnnotations(nil)
	if result != nil {
		t.Fatalf("expected nil, got %#v", result)
	}
}

func TestConvertAnnotationsFull(t *testing.T) {
	destructive := true
	openWorld := false
	sdk := &sdkmcp.ToolAnnotations{
		DestructiveHint: &destructive,
		IdempotentHint:  true,
		OpenWorldHint:   &openWorld,
		ReadOnlyHint:    true,
		Title:           "My Tool",
	}
	result := convertAnnotations(sdk)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if *result.DestructiveHint != true {
		t.Fatalf("DestructiveHint: expected true, got %v", *result.DestructiveHint)
	}
	if result.IdempotentHint != true {
		t.Fatalf("IdempotentHint: expected true, got %v", result.IdempotentHint)
	}
	if *result.OpenWorldHint != false {
		t.Fatalf("OpenWorldHint: expected false, got %v", *result.OpenWorldHint)
	}
	if result.ReadOnlyHint != true {
		t.Fatalf("ReadOnlyHint: expected true, got %v", result.ReadOnlyHint)
	}
	if result.Title != "My Tool" {
		t.Fatalf("Title: expected 'My Tool', got %q", result.Title)
	}
}

func TestConvertAnnotationsEmptyHints(t *testing.T) {
	sdk := &sdkmcp.ToolAnnotations{
		Title: "Just a title",
	}
	result := convertAnnotations(sdk)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.DestructiveHint != nil {
		t.Fatalf("DestructiveHint: expected nil, got %v", *result.DestructiveHint)
	}
	if result.IdempotentHint != false {
		t.Fatalf("IdempotentHint: expected false, got %v", result.IdempotentHint)
	}
	if result.OpenWorldHint != nil {
		t.Fatalf("OpenWorldHint: expected nil, got %v", *result.OpenWorldHint)
	}
	if result.ReadOnlyHint != false {
		t.Fatalf("ReadOnlyHint: expected false, got %v", result.ReadOnlyHint)
	}
}

func TestMcpRiskLevelNilAnnotations(t *testing.T) {
	level := mcpRiskLevel(nil)
	if level != tools.RiskLevelHigh {
		t.Fatalf("expected RiskLevelHigh, got %s", level)
	}
}

func TestMcpRiskLevelReadOnly(t *testing.T) {
	level := mcpRiskLevel(&llm.ToolAnnotations{ReadOnlyHint: true})
	if level != tools.RiskLevelLow {
		t.Fatalf("expected RiskLevelLow, got %s", level)
	}
}

func TestMcpRiskLevelDestructive(t *testing.T) {
	destructive := true
	level := mcpRiskLevel(&llm.ToolAnnotations{DestructiveHint: &destructive})
	if level != tools.RiskLevelHigh {
		t.Fatalf("expected RiskLevelHigh, got %s", level)
	}
}

func TestMcpRiskLevelDefault(t *testing.T) {
	level := mcpRiskLevel(&llm.ToolAnnotations{})
	if level != tools.RiskLevelMedium {
		t.Fatalf("expected RiskLevelMedium, got %s", level)
	}
}

func TestMcpSideEffectsNilAnnotations(t *testing.T) {
	if !mcpSideEffects(nil) {
		t.Fatal("expected true for nil annotations")
	}
}

func TestMcpSideEffectsReadOnly(t *testing.T) {
	if mcpSideEffects(&llm.ToolAnnotations{ReadOnlyHint: true}) {
		t.Fatal("expected false for read-only tool")
	}
}

func TestMcpSideEffectsDefault(t *testing.T) {
	if !mcpSideEffects(&llm.ToolAnnotations{}) {
		t.Fatal("expected true for non-read-only tool")
	}
}
