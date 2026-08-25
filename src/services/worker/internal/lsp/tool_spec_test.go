package lsp

import "testing"

func TestLSPToolLLMSpecUsesFlatObjectSchema(t *testing.T) {
	if _, ok := LSPToolLLMSpec.JSONSchema["oneOf"]; ok {
		t.Fatalf("lsp schema should not use top-level oneOf: %#v", LSPToolLLMSpec.JSONSchema)
	}
	if got := LSPToolLLMSpec.JSONSchema["type"]; got != "object" {
		t.Fatalf("lsp schema type = %#v, want object", got)
	}
	props, ok := LSPToolLLMSpec.JSONSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("lsp schema properties missing: %#v", LSPToolLLMSpec.JSONSchema)
	}
	operation, ok := props["operation"].(map[string]any)
	if !ok {
		t.Fatalf("operation schema missing: %#v", props)
	}
	enumVals, ok := operation["enum"].([]string)
	if !ok {
		t.Fatalf("operation enum missing: %#v", operation)
	}
	want := map[string]bool{
		"definition": true, "references": true, "hover": true,
		"document_symbols": true, "workspace_symbols": true,
		"type_definition": true, "implementation": true,
		"diagnostics": true, "rename": true,
		"prepare_call_hierarchy": true, "incoming_calls": true,
		"outgoing_calls": true,
	}
	if len(enumVals) != len(want) {
		t.Fatalf("operation enum len = %d, want %d (%v)", len(enumVals), len(want), enumVals)
	}
	for _, value := range enumVals {
		if !want[value] {
			t.Fatalf("unexpected operation enum value %q", value)
		}
		delete(want, value)
	}
	if len(want) != 0 {
		t.Fatalf("missing operation enum values: %#v", want)
	}
}

func TestValidateArgsByOperation(t *testing.T) {
	if err := validateArgs(lspArgs{Operation: "workspace_symbols", Query: "client"}); err != nil {
		t.Fatalf("workspace_symbols should accept query-only args: %v", err)
	}
	if err := validateArgs(lspArgs{Operation: "rename", FilePath: "/tmp/x.go", Line: 1, Column: 1, NewName: "next"}); err != nil {
		t.Fatalf("rename should accept full args: %v", err)
	}
	if err := validateArgs(lspArgs{Operation: "rename", FilePath: "/tmp/x.go", Line: 1, Column: 1}); err == nil {
		t.Fatal("rename without new_name should fail")
	}
}
