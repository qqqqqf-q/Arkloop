package pipeline

import (
	"encoding/json"
)

type ProjectMetaContext struct {
	ProjectID string `json:"project_id"`
}

func ApplyProjectContext(rc *RunContext) {
	if rc.ProjectMetaContext == nil || *rc.ProjectMetaContext == "" {
		return
	}

	var ctx ProjectMetaContext
	if err := json.Unmarshal([]byte(*rc.ProjectMetaContext), &ctx); err != nil || ctx.ProjectID == "" {
		return
	}

	rc.UpsertPromptSegment(PromptSegment{
		Name:   "project.context",
		Target: PromptTargetSystemPrefix,
		Text: "" +
			"<system-reminder>\n" +
			"You are in **Project Mode**. A project has been created for this thread.\n" +
			"Project ID: " + ctx.ProjectID + "\n" +
			"Use the available MCP tools to load project context (design system, skill, metadata)\n" +
			"before generating any design output.\n" +
			"</system-reminder>",
	})
}
