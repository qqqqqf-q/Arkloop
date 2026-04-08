package accountapi

import "encoding/json"

const (
	inboundStatePendingDispatch  = "pending_dispatch"
	inboundStateDeliveredToRun   = "delivered_to_existing_run"
	inboundStateEnqueuedNewRun   = "new_run_enqueued"
	inboundStateIgnoredUnlinked  = "ignored_unlinked"
	inboundStatePassivePersisted = "passive_persisted"
	inboundStateCommandHandled   = "command_handled"
	inboundStateThrottledNoRun   = "throttled_before_enqueue"
	inboundStateAbsorbedHeartbeat = "absorbed_by_heartbeat"
	inboundMetadataStateKey      = "ingress_state"
	inboundMetadataPreTailKey    = "pre_tail_message_id"
)

func inboundLedgerMetadata(base map[string]any, state string) json.RawMessage {
	payload := make(map[string]any, len(base)+1)
	for key, value := range base {
		payload[key] = value
	}
	payload[inboundMetadataStateKey] = state
	encoded, err := json.Marshal(payload)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}

func inboundLedgerState(raw json.RawMessage) string {
	value, _ := inboundLedgerString(raw, inboundMetadataStateKey)
	return value
}

func inboundLedgerString(raw json.RawMessage, key string) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", false
	}
	value, _ := payload[key].(string)
	if value == "" {
		return "", false
	}
	return value, true
}
