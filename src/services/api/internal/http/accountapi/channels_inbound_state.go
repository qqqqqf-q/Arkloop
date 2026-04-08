package accountapi

import (
	"encoding/json"
	"strings"
	"time"
)

const (
	inboundStatePendingDispatch     = "pending_dispatch"
	inboundStateDeliveredToRun      = "delivered_to_existing_run"
	inboundStateEnqueuedNewRun      = "new_run_enqueued"
	inboundStateIgnoredUnlinked     = "ignored_unlinked"
	inboundStatePassivePersisted    = "passive_persisted"
	inboundStateCommandHandled      = "command_handled"
	inboundStateThrottledNoRun      = "throttled_before_enqueue"
	inboundStateAbsorbedHeartbeat   = "absorbed_by_heartbeat"
	inboundMetadataStateKey         = "ingress_state"
	inboundMetadataDispatchAfterKey = "dispatch_after_unix_ms"
	inboundMetadataDispatchModeKey  = "dispatch_mode"
	inboundMetadataPreTailKey       = "pre_tail_message_id"
	inboundDispatchModeBurstV1      = "burst_v1"
)

var channelInboundBurstWindow = 1000 * time.Millisecond

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

func inboundLedgerInt64(raw json.RawMessage, key string) (int64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0, false
	}
	switch value := payload[key].(type) {
	case float64:
		return int64(value), true
	case int64:
		return value, true
	case int:
		return int64(value), true
	default:
		return 0, false
	}
}

func inboundLedgerBool(raw json.RawMessage, key string) (bool, bool) {
	if len(raw) == 0 {
		return false, false
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false, false
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return false, false
	}
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes":
			return true, true
		case "false", "0", "no":
			return false, true
		default:
			return false, false
		}
	default:
		return false, false
	}
}

func inboundLedgerDispatchAfterUnixMs(raw json.RawMessage) (int64, bool) {
	return inboundLedgerInt64(raw, inboundMetadataDispatchAfterKey)
}

func applyInboundBurstMetadata(raw json.RawMessage, dispatchAfterUnixMs int64) json.RawMessage {
	payload := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &payload)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload[inboundMetadataDispatchAfterKey] = dispatchAfterUnixMs
	payload[inboundMetadataDispatchModeKey] = inboundDispatchModeBurstV1
	encoded, err := json.Marshal(payload)
	if err != nil {
		return raw
	}
	return encoded
}

func applyInboundLedgerState(raw json.RawMessage, state string) json.RawMessage {
	payload := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &payload)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload[inboundMetadataStateKey] = state
	encoded, err := json.Marshal(payload)
	if err != nil {
		return raw
	}
	return encoded
}

func nextInboundBurstDispatchAfter(now time.Time) int64 {
	return now.Add(channelInboundBurstWindow).UnixMilli()
}

func setChannelInboundBurstWindowForTest(tb interface{ Cleanup(func()) }, window time.Duration) {
	previous := channelInboundBurstWindow
	channelInboundBurstWindow = window
	tb.Cleanup(func() {
		channelInboundBurstWindow = previous
	})
}
