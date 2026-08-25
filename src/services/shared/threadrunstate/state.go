package threadrunstate

import (
	"context"
	"encoding/json"

	"arkloop/services/shared/eventbus"

	"github.com/google/uuid"
)

const Topic = "arkloop:thread_run_state"

type ChangedPayload struct {
	AccountID string `json:"account_id"`
	ThreadID  string `json:"thread_id"`
}

func Encode(accountID uuid.UUID, threadID uuid.UUID) string {
	payload, err := json.Marshal(ChangedPayload{
		AccountID: accountID.String(),
		ThreadID:  threadID.String(),
	})
	if err != nil {
		return ""
	}
	return string(payload)
}

func Decode(raw string) (uuid.UUID, uuid.UUID, bool) {
	var payload ChangedPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	accountID, err := uuid.Parse(payload.AccountID)
	if err != nil || accountID == uuid.Nil {
		return uuid.Nil, uuid.Nil, false
	}
	threadID, err := uuid.Parse(payload.ThreadID)
	if err != nil || threadID == uuid.Nil {
		return uuid.Nil, uuid.Nil, false
	}
	return accountID, threadID, true
}

func Publish(ctx context.Context, bus eventbus.EventBus, accountID uuid.UUID, threadID uuid.UUID) {
	if accountID == uuid.Nil || threadID == uuid.Nil {
		return
	}
	payload := Encode(accountID, threadID)
	if payload == "" {
		return
	}
	if bus != nil {
		_ = bus.Publish(ctx, Topic, payload)
	}
}
