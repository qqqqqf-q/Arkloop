package runresume

var RecoverableEventTypes = []string{
	"message.delta",
	"tool.call",
	"tool.call.delta",
	"tool.result",
	"run.segment.start",
	"run.segment.end",
}

func RecoverableEventTypeNames() []string {
	out := make([]string, len(RecoverableEventTypes))
	copy(out, RecoverableEventTypes)
	return out
}
