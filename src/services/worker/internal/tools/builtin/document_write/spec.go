package documentwrite

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

var AgentSpec = tools.AgentToolSpec{
	Name:        "document_write",
	Version:     "1",
	Description: "write a Markdown document and upload it as a downloadable artifact",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: true,
}

var LlmSpec = llm.ToolSpec{
	Name:        "document_write",
	Description: strPtr("write a Markdown document and save it as a downloadable file artifact. Use this tool when the user requests a report, summary, plan, article, or any long-form document. Provide the full Markdown content; the file will be uploaded and returned as a downloadable artifact. Reference the result artifact using [label](artifact:<key>)."),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"filename": map[string]any{
				"type":        "string",
				"description": "output filename, must end with .md",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "full Markdown content of the document",
			},
		},
		"required":             []string{"filename", "content"},
		"additionalProperties": false,
	},
}

func strPtr(s string) *string { return &s }
