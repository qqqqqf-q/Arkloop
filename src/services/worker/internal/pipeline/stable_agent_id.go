package pipeline

import (
	"strings"

	"github.com/google/uuid"
)

// StableAgentID returns the deterministic memory bucket for the current run.
// Channel conversations isolate first, then project-scoped runs, then persona.
func StableAgentID(rc *RunContext) string {
	if rc == nil {
		return "default"
	}
	if rc.ChannelContext != nil && rc.ChannelContext.ChannelID != uuid.Nil {
		return "channel_" + rc.ChannelContext.ChannelID.String()
	}
	if rc.Run.ProjectID != nil && *rc.Run.ProjectID != uuid.Nil {
		return "project_" + rc.Run.ProjectID.String()
	}
	if rc.PersonaDefinition != nil {
		if id := strings.TrimSpace(rc.PersonaDefinition.ID); id != "" {
			return "persona_" + id
		}
	}
	return "default"
}
