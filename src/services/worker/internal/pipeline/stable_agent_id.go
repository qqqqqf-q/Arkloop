package pipeline

import (
	"github.com/google/uuid"
)

// StableAgentID returns the deterministic memory bucket for the current run.
// Memory identity is user-centric: channel/project/persona must not create new buckets.
func StableAgentID(rc *RunContext) string {
	if rc == nil || rc.UserID == nil || *rc.UserID == uuid.Nil {
		return "user_unknown"
	}
	return "user_" + rc.UserID.String()
}
