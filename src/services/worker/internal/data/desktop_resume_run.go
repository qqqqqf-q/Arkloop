//go:build desktop

package data

import (
	"arkloop/services/shared/runresume"
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	DesktopAutoContinueSource           = "auto_continue"
	DesktopRuntimeResumeSource          = "runtime_resume"
	desktopRunStartedSourceKey          = "source"
	desktopRunStartedContinuationSource = "continuation_source"
	desktopRunStartedContinuationLoop   = "continuation_loop"
	desktopRunStartedContinuationReply  = "continuation_response"
)

func DesktopRunHasRecoverableOutput(
	ctx context.Context,
	tx pgx.Tx,
	runID uuid.UUID,
) (bool, error) {
	if runID == uuid.Nil {
		return false, fmt.Errorf("run_id must not be empty")
	}
	eventType, err := (DesktopRunEventsRepository{}).GetLatestEventType(ctx, tx, runID, runresume.RecoverableEventTypeNames())
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(eventType) != "", nil
}

func DesktopCreateAutoContinueRunInTx(
	ctx context.Context,
	tx pgx.Tx,
	parentRun Run,
) (Run, error) {
	if tx == nil {
		return Run{}, fmt.Errorf("tx must not be nil")
	}
	if parentRun.ID == uuid.Nil {
		return Run{}, fmt.Errorf("parent run must not be empty")
	}
	if parentRun.AccountID == uuid.Nil || parentRun.ThreadID == uuid.Nil {
		return Run{}, fmt.Errorf("parent run identity is incomplete")
	}

	var exists int
	err := tx.QueryRow(ctx,
		`SELECT 1
		   FROM runs
		  WHERE thread_id = $1
		    AND parent_run_id IS NULL
		    AND status IN ('running', 'cancelling')
		    AND id <> $2
		  LIMIT 1`,
		parentRun.ThreadID,
		parentRun.ID,
	).Scan(&exists)
	if err != nil && !isNoRows(err) {
		return Run{}, fmt.Errorf("check active resumed run conflict: %w", err)
	}
	if exists == 1 {
		return Run{}, fmt.Errorf("thread already has another active root run")
	}

	_, startedData, err := (DesktopRunEventsRepository{}).FirstEventData(ctx, tx, parentRun.ID)
	if err != nil {
		return Run{}, fmt.Errorf("load parent run.started data: %w", err)
	}
	startedData = cloneDesktopMap(startedData)
	if startedData == nil {
		startedData = map[string]any{}
	}
	startedData[desktopRunStartedSourceKey] = DesktopAutoContinueSource
	startedData[desktopRunStartedContinuationSource] = DesktopRuntimeResumeSource
	startedData[desktopRunStartedContinuationLoop] = true
	startedData[desktopRunStartedContinuationReply] = true

	runID := uuid.New()
	if _, err := tx.Exec(ctx,
		`INSERT INTO runs (id, account_id, thread_id, created_by_user_id, status, resume_from_run_id)
		 VALUES ($1, $2, $3, $4, 'running', $5)`,
		runID,
		parentRun.AccountID,
		parentRun.ThreadID,
		parentRun.CreatedByUserID,
		parentRun.ID,
	); err != nil {
		return Run{}, fmt.Errorf("insert auto-continue run: %w", err)
	}
	if _, err := (DesktopRunEventsRepository{}).AppendEvent(ctx, tx, runID, "run.started", startedData, nil, nil); err != nil {
		return Run{}, fmt.Errorf("append auto-continue run.started: %w", err)
	}

	return Run{
		ID:              runID,
		AccountID:       parentRun.AccountID,
		ThreadID:        parentRun.ThreadID,
		Status:          "running",
		ResumeFromRunID: &parentRun.ID,
		CreatedByUserID: parentRun.CreatedByUserID,
		ProfileRef:      parentRun.ProfileRef,
		WorkspaceRef:    parentRun.WorkspaceRef,
	}, nil
}

func cloneDesktopMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
