package events

import (
	"strings"
	"time"
)

type RunEvent struct {
	Type       string
	DataJSON   map[string]any
	ToolName   *string
	ErrorClass *string
	OccurredAt time.Time
}

type Emitter struct {
	traceID string
}

func NewEmitter(traceID string) Emitter {
	return Emitter{traceID: strings.TrimSpace(traceID)}
}

func (e Emitter) Emit(eventType string, dataJSON map[string]any, toolName *string, errorClass *string) RunEvent {
	payload := map[string]any{}
	for key, value := range dataJSON {
		payload[key] = value
	}
	if e.traceID != "" {
		payload["trace_id"] = e.traceID
	}
	return RunEvent{
		Type:       eventType,
		DataJSON:   payload,
		ToolName:   toolName,
		ErrorClass: errorClass,
		OccurredAt: time.Now().UTC(),
	}
}
