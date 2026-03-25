package pipeline

import (
	"testing"

	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
)

func TestStableAgentID_UsesUserID(t *testing.T) {
	userID := uuid.New()
	rc := &RunContext{
		Run:    data.Run{AccountID: uuid.New(), ID: uuid.New(), ThreadID: uuid.New()},
		UserID: &userID,
	}

	got := StableAgentID(rc)
	want := "user_" + userID.String()
	if got != want {
		t.Fatalf("unexpected stable agent id: got %q want %q", got, want)
	}
}

func TestStableAgentID_FallbackWhenUserMissing(t *testing.T) {
	if got := StableAgentID(nil); got != "user_unknown" {
		t.Fatalf("unexpected id for nil run context: %q", got)
	}
	if got := StableAgentID(&RunContext{}); got != "user_unknown" {
		t.Fatalf("unexpected id for missing user: %q", got)
	}
}
