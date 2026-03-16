package documentwrite

import (
	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

// create_artifact: primary spec
var CreateArtifactAgentSpec = tools.AgentToolSpec{
	Name:        "create_artifact",
	Version:     "1",
	Description: "create an interactive or static artifact (HTML, SVG, Markdown)",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: true,
}

var CreateArtifactLlmSpec = llm.ToolSpec{
	Name:        "create_artifact",
	Description: strPtr(sharedtoolmeta.Must("create_artifact").LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "display name for the artifact",
			},
			"filename": map[string]any{
				"type":        "string",
				"description": "output filename with extension, e.g. chart.html, diagram.svg, report.md",
			},
			"display": map[string]any{
				"type":        "string",
				"enum":        []string{"inline", "panel"},
				"default":     "inline",
				"description": "inline: render in chat flow; panel: open in side panel",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "full content of the artifact (HTML/SVG/Markdown). MUST be the last parameter generated.",
			},
		},
		"required":             []string{"title", "filename", "content"},
		"additionalProperties": false,
	},
}

// document_write: backward-compatible alias
var AgentSpec = tools.AgentToolSpec{
	Name:        "document_write",
	Version:     "1",
	Description: "write a Markdown document and upload it as a downloadable artifact",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: true,
}

var LlmSpec = llm.ToolSpec{
	Name:        "document_write",
	Description: strPtr(sharedtoolmeta.Must("document_write").LLMDescription),
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
