package subagentctl

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"arkloop/services/worker/internal/data"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const childThreadTTL = 7 * 24 * time.Hour

type SubAgentRunFactory struct {
	pool *pgxpool.Pool
}

func NewSubAgentRunFactory(pool *pgxpool.Pool) *SubAgentRunFactory {
	return &SubAgentRunFactory{pool: pool}
}

func (f *SubAgentRunFactory) CreateSpawnRun(
	ctx context.Context,
	tx pgx.Tx,
	parentRun data.Run,
	personaID string,
	input string,
	forcedRunID *uuid.UUID,
) (data.SubAgentRecord, uuid.UUID, error) {
	lineage, err := (data.RunsRepository{}).GetLineage(ctx, tx, parentRun.ID)
	if err != nil {
		return data.SubAgentRecord{}, uuid.Nil, err
	}
	createdSubAgent, err := (data.SubAgentRepository{}).Create(ctx, tx, data.SubAgentCreateParams{
		OrgID:          parentRun.OrgID,
		ParentRunID:    parentRun.ID,
		ParentThreadID: parentRun.ThreadID,
		RootRunID:      lineage.RootRunID,
		RootThreadID:   lineage.RootThreadID,
		Depth:          lineage.Depth + 1,
		PersonaID:      stringPtr(personaID),
		SourceType:     data.SubAgentSourceTypeThreadSpawn,
		ContextMode:    data.SubAgentContextModeIsolated,
	})
	if err != nil {
		return data.SubAgentRecord{}, uuid.Nil, fmt.Errorf("create sub_agent: %w", err)
	}
	if _, err := (data.SubAgentEventAppender{}).Append(ctx, tx, createdSubAgent.ID, nil, data.SubAgentEventTypeSpawnRequested, map[string]any{
		"persona_id":       personaID,
		"context_mode":     createdSubAgent.ContextMode,
		"source_type":      data.SubAgentSourceTypeThreadSpawn,
		"parent_run_id":    parentRun.ID.String(),
		"parent_thread_id": parentRun.ThreadID.String(),
	}, nil); err != nil {
		return data.SubAgentRecord{}, uuid.Nil, fmt.Errorf("append spawn_requested: %w", err)
	}
	childThreadID, err := f.createChildThread(ctx, tx, parentRun)
	if err != nil {
		return data.SubAgentRecord{}, uuid.Nil, err
	}
	if _, err := insertUserMessage(ctx, tx, parentRun.OrgID, childThreadID, input); err != nil {
		return data.SubAgentRecord{}, uuid.Nil, fmt.Errorf("insert child message: %w", err)
	}
	childRunID, err := f.createQueuedRun(ctx, tx, parentRun, createdSubAgent, childThreadID, forcedRunID, data.SubAgentEventTypeSpawned, map[string]any{
		"thread_id": childThreadID.String(),
	}, nil)
	if err != nil {
		return data.SubAgentRecord{}, uuid.Nil, err
	}
	return createdSubAgent, childRunID, nil
}

func (f *SubAgentRunFactory) CreateRunForExistingSubAgent(
	ctx context.Context,
	tx pgx.Tx,
	subAgent data.SubAgentRecord,
	input string,
	forcedRunID *uuid.UUID,
	primaryEventType string,
	primaryPayload map[string]any,
	errorClass *string,
) (uuid.UUID, error) {
	ownerRun, err := (data.RunsRepository{}).GetRun(ctx, tx, subAgent.ParentRunID)
	if err != nil {
		return uuid.Nil, err
	}
	if ownerRun == nil {
		return uuid.Nil, fmt.Errorf("parent run not found: %s", subAgent.ParentRunID)
	}
	threadID, runID, err := resolveSubAgentThread(ctx, tx, subAgent)
	if err != nil {
		return uuid.Nil, err
	}
	payload := cloneMap(primaryPayload)
	payload["thread_id"] = threadID.String()
	if runID != uuid.Nil {
		payload["run_id"] = runID.String()
	}
	trimmedInput := strings.TrimSpace(input)
	if trimmedInput != "" {
		messageID, err := insertUserMessage(ctx, tx, subAgent.OrgID, threadID, trimmedInput)
		if err != nil {
			return uuid.Nil, fmt.Errorf("insert sub_agent input: %w", err)
		}
		payload["message_id"] = messageID.String()
		payload["input_bytes"] = len([]byte(trimmedInput))
	}
	return f.createQueuedRun(ctx, tx, *ownerRun, subAgent, threadID, forcedRunID, primaryEventType, payload, errorClass)
}

func (f *SubAgentRunFactory) CreateRunFromPendingInputs(ctx context.Context, tx pgx.Tx, subAgent data.SubAgentRecord) (*uuid.UUID, error) {
	pendingRepo := data.SubAgentPendingInputsRepository{}
	items, err := pendingRepo.ListBySubAgentForUpdate(ctx, tx, subAgent.ID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	parts := make([]string, 0, len(items))
	ids := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		parts = append(parts, strings.TrimSpace(item.Input))
		ids = append(ids, item.ID)
	}
	combined := strings.Join(parts, "\n\n")
	ownerRun, err := (data.RunsRepository{}).GetRun(ctx, tx, subAgent.ParentRunID)
	if err != nil {
		return nil, err
	}
	if ownerRun == nil {
		return nil, fmt.Errorf("parent run not found: %s", subAgent.ParentRunID)
	}
	threadID, _, err := resolveSubAgentThread(ctx, tx, subAgent)
	if err != nil {
		return nil, err
	}
	messageID, err := insertUserMessage(ctx, tx, subAgent.OrgID, threadID, combined)
	if err != nil {
		return nil, fmt.Errorf("insert pending input message: %w", err)
	}
	childRunID, err := f.createQueuedRun(ctx, tx, *ownerRun, subAgent, threadID, nil, "", map[string]any{
		"thread_id":     threadID.String(),
		"message_id":    messageID.String(),
		"input_bytes":   len([]byte(combined)),
		"pending_count": len(items),
	}, nil)
	if err != nil {
		return nil, err
	}
	if err := pendingRepo.DeleteBatch(ctx, tx, ids); err != nil {
		return nil, err
	}
	return &childRunID, nil
}

func (f *SubAgentRunFactory) createChildThread(ctx context.Context, tx pgx.Tx, parentRun data.Run) (uuid.UUID, error) {
	var childThreadID uuid.UUID
	if err := tx.QueryRow(ctx,
		`INSERT INTO threads (org_id, project_id, is_private, expires_at)
		 VALUES ($1, $2, TRUE, now() + make_interval(secs => $3))
		 RETURNING id`,
		parentRun.OrgID,
		parentRun.ProjectID,
		int64(childThreadTTL.Seconds()),
	).Scan(&childThreadID); err != nil {
		return uuid.Nil, fmt.Errorf("create child thread: %w", err)
	}
	return childThreadID, nil
}

func (f *SubAgentRunFactory) createQueuedRun(
	ctx context.Context,
	tx pgx.Tx,
	parentRun data.Run,
	subAgent data.SubAgentRecord,
	threadID uuid.UUID,
	forcedRunID *uuid.UUID,
	primaryEventType string,
	primaryPayload map[string]any,
	errorClass *string,
) (uuid.UUID, error) {
	childRunID := uuid.New()
	if forcedRunID != nil && *forcedRunID != uuid.Nil {
		childRunID = *forcedRunID
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO runs (id, org_id, thread_id, parent_run_id, created_by_user_id, profile_ref, workspace_ref, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 'running')`,
		childRunID,
		parentRun.OrgID,
		threadID,
		parentRun.ID,
		parentRun.CreatedByUserID,
		parentRun.ProfileRef,
		parentRun.WorkspaceRef,
	); err != nil {
		return uuid.Nil, fmt.Errorf("insert child run: %w", err)
	}
	var seq int64
	if err := tx.QueryRow(ctx, `SELECT nextval('run_events_seq_global')`).Scan(&seq); err != nil {
		return uuid.Nil, fmt.Errorf("alloc seq: %w", err)
	}
	personaID := derefString(subAgent.PersonaID)
	eventData, err := json.Marshal(map[string]any{"persona_id": personaID})
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal run.started data: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO run_events (run_id, seq, type, data_json)
		 VALUES ($1, $2, 'run.started', $3::jsonb)`,
		childRunID, seq, string(eventData),
	); err != nil {
		return uuid.Nil, fmt.Errorf("insert run.started: %w", err)
	}
	if err := (data.SubAgentRepository{}).TransitionToQueued(ctx, tx, subAgent.ID, childRunID); err != nil {
		return uuid.Nil, fmt.Errorf("mark sub_agent queued: %w", err)
	}
	payload := map[string]any{
		"run_id":     childRunID.String(),
		"thread_id":  threadID.String(),
		"persona_id": personaID,
	}
	for key, value := range primaryPayload {
		payload[key] = value
	}
	appender := data.SubAgentEventAppender{}
	if strings.TrimSpace(primaryEventType) != "" {
		if _, err := appender.Append(ctx, tx, subAgent.ID, &childRunID, primaryEventType, payload, errorClass); err != nil {
			return uuid.Nil, fmt.Errorf("append %s: %w", primaryEventType, err)
		}
	}
	if _, err := appender.Append(ctx, tx, subAgent.ID, &childRunID, data.SubAgentEventTypeRunQueued, payload, nil); err != nil {
		return uuid.Nil, fmt.Errorf("append run_queued: %w", err)
	}
	return childRunID, nil
}

func resolveSubAgentThread(ctx context.Context, tx pgx.Tx, record data.SubAgentRecord) (uuid.UUID, uuid.UUID, error) {
	candidateRunID := uuid.Nil
	if record.CurrentRunID != nil {
		candidateRunID = *record.CurrentRunID
	} else if record.LastCompletedRunID != nil {
		candidateRunID = *record.LastCompletedRunID
	}
	if candidateRunID == uuid.Nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("sub_agent has no run context")
	}
	run, err := (data.RunsRepository{}).GetRun(ctx, tx, candidateRunID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if run == nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("run not found: %s", candidateRunID)
	}
	return run.ThreadID, run.ID, nil
}

func insertUserMessage(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, threadID uuid.UUID, content string) (uuid.UUID, error) {
	var messageID uuid.UUID
	err := tx.QueryRow(ctx,
		`INSERT INTO messages (org_id, thread_id, role, content, metadata_json)
		 VALUES ($1, $2, 'user', $3, '{}'::jsonb)
		 RETURNING id`,
		orgID,
		threadID,
		strings.TrimSpace(content),
	).Scan(&messageID)
	return messageID, err
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(src))
	for key, value := range src {
		cloned[key] = value
	}
	return cloned
}
