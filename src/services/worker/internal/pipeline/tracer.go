package pipeline

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
)

const (
	pipelineTraceMaxEvents     = 500
	pipelineTraceMaxToolNames  = 20
	pipelineTraceTruncateEvent = "tracer.truncated"
)

type Tracer interface {
	Event(middleware, event string, fields map[string]any)
}

type traceEvent struct {
	Middleware string
	EventName  string
	Fields     map[string]any
}

type bufTracer struct {
	runID     uuid.UUID
	accountID uuid.UUID
	writer    data.RunPipelineEventsWriter
	mu        sync.Mutex
	events    []traceEvent
	maxEvents int
	dropped   int
	truncated bool
}

func NewBufTracer(runID, accountID uuid.UUID, writer data.RunPipelineEventsWriter) Tracer {
	if writer == nil {
		return nil
	}
	return &bufTracer{
		runID:     runID,
		accountID: accountID,
		writer:    writer,
		maxEvents: pipelineTraceMaxEvents,
	}
}

func FlushTracer(tracer Tracer) {
	buf, ok := tracer.(*bufTracer)
	if !ok || buf == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	buf.flush(ctx)
}

func (t *bufTracer) Event(middleware, event string, fields map[string]any) {
	if t == nil {
		return
	}
	middleware = strings.TrimSpace(middleware)
	event = strings.TrimSpace(event)
	if middleware == "" || event == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	maxEvents := t.maxEvents
	if maxEvents <= 0 {
		maxEvents = pipelineTraceMaxEvents
	}
	if t.truncated {
		t.dropped++
		return
	}
	if len(t.events) < maxEvents-1 {
		t.events = append(t.events, traceEvent{
			Middleware: middleware,
			EventName:  event,
			Fields:     cloneTraceFields(fields),
		})
		return
	}

	t.truncated = true
	t.dropped = 1
	t.events = append(t.events, traceEvent{
		Middleware: "tracer",
		EventName:  pipelineTraceTruncateEvent,
		Fields: map[string]any{
			"max_events": maxEvents,
		},
	})
}

func (t *bufTracer) flush(ctx context.Context) {
	if t == nil || t.writer == nil {
		return
	}

	t.mu.Lock()
	events := append([]traceEvent(nil), t.events...)
	dropped := t.dropped
	if dropped > 0 && len(events) > 0 {
		last := &events[len(events)-1]
		if last.EventName == pipelineTraceTruncateEvent {
			last.Fields = cloneTraceFields(last.Fields)
			last.Fields["dropped_events"] = dropped
			last.Fields["events_before_truncation"] = len(events) - 1 + dropped
		}
	}
	t.mu.Unlock()

	if len(events) == 0 {
		return
	}

	records := make([]data.RunPipelineEventRecord, 0, len(events))
	for idx, event := range events {
		records = append(records, data.RunPipelineEventRecord{
			RunID:      t.runID,
			AccountID:  t.accountID,
			Middleware: event.Middleware,
			EventName:  event.EventName,
			Seq:        idx + 1,
			FieldsJSON: cloneTraceFields(event.Fields),
		})
	}
	if err := t.writer.InsertBatch(ctx, records); err != nil {
		slog.WarnContext(ctx, "pipeline trace flush failed", "run_id", t.runID.String(), "err", err.Error())
	}
}

func emitTraceEvent(rc *RunContext, middleware, event string, fields map[string]any) {
	if rc == nil || rc.Tracer == nil {
		return
	}
	rc.Tracer.Event(middleware, event, fields)
}

func cloneTraceFields(fields map[string]any) map[string]any {
	if len(fields) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(fields))
	for key, value := range fields {
		cloned[key] = value
	}
	return cloned
}

func traceToolNames(specs []string) []string {
	if len(specs) == 0 {
		return nil
	}
	limit := len(specs)
	if limit > pipelineTraceMaxToolNames {
		limit = pipelineTraceMaxToolNames
	}
	out := make([]string, 0, limit)
	for _, spec := range specs[:limit] {
		name := strings.TrimSpace(spec)
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func traceToolCalls(toolCalls []string) []string {
	return traceToolNames(toolCalls)
}

func summarizeMemoryInjection(before, after string) (bool, bool, int) {
	if before == after {
		return false, false, 0
	}
	delta := after
	if len(after) >= len(before) && strings.HasPrefix(after, before) {
		delta = after[len(before):]
	}
	return strings.Contains(delta, "<memory>"), strings.Contains(delta, "<notebook>"), len(delta)
}
