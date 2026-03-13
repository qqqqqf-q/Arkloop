package data

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type SubAgentEventAppender struct {
	SubAgentsRepo SubAgentRepository
	EventsRepo    SubAgentEventsRepository
}

func (a SubAgentEventAppender) Append(
	ctx context.Context,
	tx pgx.Tx,
	subAgentID uuid.UUID,
	runID *uuid.UUID,
	eventType string,
	dataJSON map[string]any,
	errorClass *string,
) (int64, error) {
	payload := cloneSubAgentEventData(dataJSON)
	trimmedErrorClass := normalizeSubAgentOptionalString(errorClass)
	if trimmedErrorClass != nil {
		if _, ok := payload["error_class"]; !ok {
			payload["error_class"] = *trimmedErrorClass
		}
	}
	return a.EventsRepo.AppendEvent(ctx, tx, subAgentID, runID, eventType, payload, trimmedErrorClass)
}

func (a SubAgentEventAppender) AppendForCurrentRun(
	ctx context.Context,
	tx pgx.Tx,
	runID uuid.UUID,
	eventType string,
	dataJSON map[string]any,
	errorClass *string,
) (*SubAgentRecord, int64, error) {
	record, err := a.SubAgentsRepo.GetByCurrentRunID(ctx, tx, runID)
	if err != nil || record == nil {
		return record, 0, err
	}
	seq, err := a.Append(ctx, tx, record.ID, &runID, eventType, dataJSON, errorClass)
	if err != nil {
		return record, 0, err
	}
	return record, seq, nil
}

func SubAgentTerminalEventType(status string) (string, error) {
	switch strings.TrimSpace(status) {
	case SubAgentStatusCompleted:
		return SubAgentEventTypeCompleted, nil
	case SubAgentStatusFailed:
		return SubAgentEventTypeFailed, nil
	case SubAgentStatusCancelled:
		return SubAgentEventTypeCancelled, nil
	default:
		return "", fmt.Errorf("unsupported terminal sub_agent status: %s", status)
	}
}

func cloneSubAgentEventData(dataJSON map[string]any) map[string]any {
	if len(dataJSON) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(dataJSON))
	for key, value := range dataJSON {
		cloned[key] = value
	}
	return cloned
}
