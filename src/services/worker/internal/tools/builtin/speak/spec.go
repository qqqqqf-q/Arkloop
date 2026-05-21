package speak

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const ToolName = "speak"

var AgentSpec = tools.AgentToolSpec{
	Name:        ToolName,
	Version:     "1",
	Description: "Required in group discuss runs before speaking to the group. Without this tool, assistant text remains private internal monologue.",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: true,
}

var Spec = llm.ToolSpec{
	Name:        ToolName,
	Description: strPtr("Call this before visible group-chat text in discuss mode. Assistant text is private until speak is called. The tool does not contain message text; write the message as assistant text after the tool call."),
	JSONSchema: map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"reply_to_message_id": map[string]any{
				"type":        "string",
				"description": "Optional platform message id to attach the visible assistant text as a reply.",
			},
		},
	},
}

func strPtr(s string) *string { return &s }
