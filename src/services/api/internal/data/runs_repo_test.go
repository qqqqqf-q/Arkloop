package data

import (
	"testing"

	"github.com/google/uuid"
)

func TestShouldResumeFromRun(t *testing.T) {
	id := uuid.New()
	latestMessage := &threadTailMessage{ID: uuid.New(), Role: "user"}
	run := &Run{ID: id, Status: "cancelled"}
	if got := shouldResumeFromRun(run, latestMessage, uuid.New().String(), ""); got == nil || *got != id {
		t.Fatalf("expected resume pointer for cancelled run, got %v", got)
	}
	run.Status = "interrupted"
	if got := shouldResumeFromRun(run, latestMessage, uuid.New().String(), ""); got == nil || *got != id {
		t.Fatalf("expected resume pointer for interrupted run, got %v", got)
	}
	run.Status = "running"
	if got := shouldResumeFromRun(run, latestMessage, uuid.New().String(), ""); got != nil {
		t.Fatalf("did not expect resume pointer for running run, got %v", got)
	}
	if got := shouldResumeFromRun(nil, latestMessage, uuid.New().String(), ""); got != nil {
		t.Fatalf("did not expect resume pointer for nil run, got %v", got)
	}
	if got := shouldResumeFromRun(run, nil, uuid.New().String(), ""); got != nil {
		t.Fatalf("did not expect resume pointer without latest message, got %v", got)
	}
	if got := shouldResumeFromRun(&Run{ID: id, Status: "interrupted"}, &threadTailMessage{ID: uuid.New(), Role: "assistant"}, uuid.New().String(), ""); got != nil {
		t.Fatalf("did not expect resume pointer for non-user tail message, got %v", got)
	}
	if got := shouldResumeFromRun(&Run{ID: id, Status: "interrupted"}, latestMessage, "", ""); got != nil {
		t.Fatalf("did not expect resume pointer without previous anchor, got %v", got)
	}
	if got := shouldResumeFromRun(&Run{ID: id, Status: "interrupted"}, latestMessage, latestMessage.ID.String(), ""); got != nil {
		t.Fatalf("did not expect resume pointer when thread tail did not advance, got %v", got)
	}
	if got := shouldResumeFromRun(&Run{ID: id, Status: "interrupted"}, latestMessage, uuid.New().String(), "heartbeat"); got != nil {
		t.Fatalf("did not expect heartbeat runs to become resume sources, got %v", got)
	}
}

func TestApplyContinuationMetadata(t *testing.T) {
	resumeID := uuid.New()

	t.Run("none", func(t *testing.T) {
		meta := applyContinuationMetadata(nil, nil)
		if meta["continuation_source"] != "none" {
			t.Fatalf("expected continuation_source none, got %#v", meta["continuation_source"])
		}
		if loop, ok := meta["continuation_loop"].(bool); !ok || loop {
			t.Fatalf("expected continuation_loop false, got %#v", meta["continuation_loop"])
		}
		if _, ok := meta["continuation_response"]; ok {
			t.Fatalf("did not expect continuation_response for nil resume, got %#v", meta["continuation_response"])
		}
	})

	t.Run("user followup", func(t *testing.T) {
		meta := applyContinuationMetadata(map[string]any{"foo": "bar"}, &resumeID)
		if meta["continuation_source"] != "user_followup" {
			t.Fatalf("expected continuation_source user_followup, got %#v", meta["continuation_source"])
		}
		if loop, ok := meta["continuation_loop"].(bool); !ok || !loop {
			t.Fatalf("expected continuation_loop true, got %#v", meta["continuation_loop"])
		}
		if response, ok := meta["continuation_response"].(bool); !ok || !response {
			t.Fatalf("expected continuation_response true, got %#v", meta["continuation_response"])
		}
		if meta["foo"] != "bar" {
			t.Fatalf("expected existing keys preserved, got %#v", meta)
		}
	})
}
