package scheduled_job_manage

import (
	"context"
	"errors"
	"testing"

	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestExecutorCommonMutationsWakeScheduler(t *testing.T) {
	accountID := uuid.New()
	jobID := uuid.New()
	tests := []struct {
		name        string
		args        map[string]any
		createCalls int
		updateCalls int
		deleteCalls int
	}{
		{
			name: "create",
			args: map[string]any{
				"action":        "create",
				"name":          "daily digest",
				"prompt":        "ping",
				"schedule_kind": "interval",
				"interval_min":  5.0,
			},
			createCalls: 1,
		},
		{
			name: "update",
			args: map[string]any{
				"action": "update",
				"job_id": jobID.String(),
				"name":   "renamed",
			},
			updateCalls: 1,
		},
		{
			name: "delete",
			args: map[string]any{
				"action": "delete",
				"job_id": jobID.String(),
			},
			deleteCalls: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &scheduledJobRepoStub{
				createdJob: data.ScheduledJob{ID: jobID},
			}
			exec := &executorCommon{db: stubDB{}, repo: repo}

			result := exec.Execute(context.Background(), ToolName, tc.args, tools.ExecutionContext{
				AccountID: &accountID,
				PersonaID: "persona-default",
			}, "")
			if result.Error != nil {
				t.Fatalf("execute returned error: %v", result.Error)
			}
			if repo.notifyCalls != 1 {
				t.Fatalf("notify calls = %d, want 1", repo.notifyCalls)
			}
			if repo.createCalls != tc.createCalls {
				t.Fatalf("create calls = %d, want %d", repo.createCalls, tc.createCalls)
			}
			if repo.updateCalls != tc.updateCalls {
				t.Fatalf("update calls = %d, want %d", repo.updateCalls, tc.updateCalls)
			}
			if repo.deleteCalls != tc.deleteCalls {
				t.Fatalf("delete calls = %d, want %d", repo.deleteCalls, tc.deleteCalls)
			}
		})
	}
}

func TestExecutorCommonDoesNotWakeSchedulerOnMutationError(t *testing.T) {
	accountID := uuid.New()
	repo := &scheduledJobRepoStub{
		createErr: errors.New("boom"),
	}
	exec := &executorCommon{db: stubDB{}, repo: repo}

	result := exec.Execute(context.Background(), ToolName, map[string]any{
		"action":        "create",
		"name":          "daily digest",
		"prompt":        "ping",
		"schedule_kind": "interval",
		"interval_min":  5.0,
	}, tools.ExecutionContext{
		AccountID: &accountID,
		PersonaID: "persona-default",
	}, "")
	if result.Error == nil {
		t.Fatal("expected error result")
	}
	if repo.notifyCalls != 0 {
		t.Fatalf("notify calls = %d, want 0", repo.notifyCalls)
	}
}

func TestExecutorCommonUpdateAllowsClearingCronWhenSwitchingScheduleKind(t *testing.T) {
	accountID := uuid.New()
	jobID := uuid.New()
	repo := &scheduledJobRepoStub{}
	exec := &executorCommon{db: stubDB{}, repo: repo}

	result := exec.Execute(context.Background(), ToolName, map[string]any{
		"action":        "update",
		"job_id":        jobID.String(),
		"schedule_kind": "interval",
		"interval_min":  15.0,
		"cron_expr":     "",
	}, tools.ExecutionContext{
		AccountID: &accountID,
	}, "")
	if result.Error != nil {
		t.Fatalf("expected update to pass executor validation, got %v", result.Error)
	}
	if repo.updateCalls != 1 {
		t.Fatalf("update calls = %d, want 1", repo.updateCalls)
	}
}

type scheduledJobRepoStub struct {
	createdJob  data.ScheduledJob
	createErr   error
	updateErr   error
	deleteErr   error
	createCalls int
	updateCalls int
	deleteCalls int
	notifyCalls int
}

func (s *scheduledJobRepoStub) ListByAccount(context.Context, data.DB, uuid.UUID) ([]data.ScheduledJobWithTrigger, error) {
	return nil, nil
}

func (s *scheduledJobRepoStub) GetByID(context.Context, data.DB, uuid.UUID, uuid.UUID) (*data.ScheduledJobWithTrigger, error) {
	return nil, nil
}

func (s *scheduledJobRepoStub) CreateJob(context.Context, data.DB, data.ScheduledJob) (data.ScheduledJob, error) {
	s.createCalls++
	if s.createErr != nil {
		return data.ScheduledJob{}, s.createErr
	}
	return s.createdJob, nil
}

func (s *scheduledJobRepoStub) UpdateJob(context.Context, data.DB, uuid.UUID, uuid.UUID, data.UpdateJobParams) error {
	s.updateCalls++
	return s.updateErr
}

func (s *scheduledJobRepoStub) DeleteJob(context.Context, data.DB, uuid.UUID, uuid.UUID) error {
	s.deleteCalls++
	return s.deleteErr
}

func (s *scheduledJobRepoStub) SetTriggerFireNow(context.Context, data.DB, uuid.UUID) error {
	return nil
}

func (s *scheduledJobRepoStub) NotifyScheduler(context.Context, data.DB) error {
	s.notifyCalls++
	return nil
}

func (s *scheduledJobRepoStub) ListRunsByJobID(context.Context, data.DB, uuid.UUID, int) ([]map[string]any, error) {
	return nil, nil
}

type stubDB struct{}

func (stubDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected Exec call")
}

func (stubDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query call")
}

func (stubDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("unexpected QueryRow call")
}

func (stubDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	panic("unexpected BeginTx call")
}
