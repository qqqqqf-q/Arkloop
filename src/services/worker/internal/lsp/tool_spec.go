package lsp

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var LSPToolLLMSpec = llm.ToolSpec{
	Name: "lsp",
	Description: strPtr("Code intelligence via LSP. Operations: definition, references, hover, " +
		"document_symbols, workspace_symbols, type_definition, implementation, diagnostics, rename, " +
		"prepare_call_hierarchy, incoming_calls, outgoing_calls."),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operation": map[string]any{
				"type": "string",
				"enum": []string{
					"definition",
					"references",
					"hover",
					"document_symbols",
					"workspace_symbols",
					"type_definition",
					"implementation",
					"diagnostics",
					"rename",
					"prepare_call_hierarchy",
					"incoming_calls",
					"outgoing_calls",
				},
				"description": "LSP operation. definition/references/hover/type_definition/implementation/prepare_call_hierarchy/incoming_calls/outgoing_calls require file_path+line+column; document_symbols requires file_path; workspace_symbols requires query; rename requires file_path+line+column+new_name; diagnostics only needs operation.",
			},
			"file_path": map[string]any{"type": "string", "description": "Absolute or workspace-relative path to the file."},
			"line":      map[string]any{"type": "integer", "description": "1-based line number."},
			"column":    map[string]any{"type": "integer", "description": "1-based column number (byte offset in line)."},
			"query":     map[string]any{"type": "string", "description": "Search query for workspace_symbols."},
			"new_name":  map[string]any{"type": "string", "description": "New name for rename."},
			"include_declaration": map[string]any{
				"type":        "boolean",
				"description": "Optional for references. Include declaration in results. Default true.",
			},
		},
		"required":             []string{"operation"},
		"additionalProperties": false,
	},
}

var LSPToolAgentSpec = tools.AgentToolSpec{
	Name:        "lsp",
	Version:     "1",
	Description: "Code intelligence via Language Server Protocol",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true, // rename writes files
}

func strPtr(s string) *string { return &s }
