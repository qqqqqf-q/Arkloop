package pipeline

import (
	"context"
	"testing"
	"time"

	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
)

type stubRunPipelineEventsWriter struct {
	records []data.RunPipelineEventRecord
}

func (s *stubRunPipelineEventsWriter) InsertBatch(_ context.Context, records []data.RunPipelineEventRecord) error {
	s.records = append([]data.RunPipelineEventRecord(nil), records...)
	return nil
}

func (s *stubRunPipelineEventsWriter) DeleteOlderThan(context.Context, time.Time) error {
	return nil
}

func TestBufTracerFlushAddsTruncationEvent(t *testing.T) {
	writer := &stubRunPipelineEventsWriter{}
	tracer := NewBufTracer(uuid.New(), uuid.New(), writer)
	for i := 0; i < 505; i++ {
		tracer.Event("agent_loop", "agent_loop.llm_call_completed", map[string]any{
			"idx": i,
		})
	}

	FlushTracer(tracer)

	if got := len(writer.records); got != pipelineTraceMaxEvents {
		t.Fatalf("record count = %d, want %d", got, pipelineTraceMaxEvents)
	}
	last := writer.records[len(writer.records)-1]
	if last.EventName != pipelineTraceTruncateEvent {
		t.Fatalf("last event = %q, want %q", last.EventName, pipelineTraceTruncateEvent)
	}
	if got := last.FieldsJSON["dropped_events"]; got != 6 {
		t.Fatalf("dropped_events = %#v, want 6", got)
	}
	if got := last.FieldsJSON["events_before_truncation"]; got != 505 {
		t.Fatalf("events_before_truncation = %#v, want 505", got)
	}
}

func TestFlushTracerIgnoresNil(t *testing.T) {
	FlushTracer(nil)
}
