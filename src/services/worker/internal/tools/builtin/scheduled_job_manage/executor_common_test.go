package scheduled_job_manage

import (
	"context"
	"errors"
	"testing"
	"time"

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

func TestExecutorCommonUpdateSupportsNullableClearFields(t *testing.T) {
	accountID := uuid.New()
	jobID := uuid.New()
	repo := &scheduledJobRepoStub{}
	exec := &executorCommon{db: stubDB{}, repo: repo}

	result := exec.Execute(context.Background(), ToolName, map[string]any{
		"action":       "update",
		"job_id":       jobID.String(),
		"thread_id":    nil,
		"interval_min": nil,
		"monthly_day":  nil,
		"weekly_day":   nil,
		"fire_at":      nil,
	}, tools.ExecutionContext{
		AccountID: &accountID,
	}, "")
	if result.Error != nil {
		t.Fatalf("expected update to pass executor validation, got %v", result.Error)
	}
	if repo.updateCalls != 1 {
		t.Fatalf("update calls = %d, want 1", repo.updateCalls)
	}
	assertClearedUUID(t, "thread_id", repo.lastUpdate.ThreadID)
	assertClearedInt(t, "interval_min", repo.lastUpdate.IntervalMin)
	assertClearedInt(t, "monthly_day", repo.lastUpdate.MonthlyDay)
	assertClearedInt(t, "weekly_day", repo.lastUpdate.WeeklyDay)
	assertClearedTime(t, "fire_at", repo.lastUpdate.FireAt)
}

func TestSpecAllowsNullForUpdateClearFields(t *testing.T) {
	properties, ok := Spec.JSONSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("tool spec properties missing")
	}

	for field, want := range map[string][]string{
		"thread_id":    {"string", "null"},
		"interval_min": {"integer", "null"},
		"monthly_day":  {"integer", "null"},
		"weekly_day":   {"integer", "null"},
		"fire_at":      {"string", "null"},
	} {
		property, ok := properties[field].(map[string]any)
		if !ok {
			t.Fatalf("property %s missing", field)
		}
		types, ok := property["type"].([]string)
		if !ok {
			t.Fatalf("property %s type is %T, want []string", field, property["type"])
		}
		if len(types) != len(want) {
			t.Fatalf("property %s types = %v, want %v", field, types, want)
		}
		for i := range want {
			if types[i] != want[i] {
				t.Fatalf("property %s types = %v, want %v", field, types, want)
			}
		}
	}
}

type scheduledJobRepoStub struct {
	createdJob  data.ScheduledJob
	lastUpdate  data.UpdateJobParams
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

func (s *scheduledJobRepoStub) UpdateJob(_ context.Context, _ data.DB, _ uuid.UUID, _ uuid.UUID, upd data.UpdateJobParams) error {
	s.updateCalls++
	s.lastUpdate = upd
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

func assertClearedUUID(t *testing.T, field string, value **uuid.UUID) {
	t.Helper()
	if value == nil {
		t.Fatalf("%s outer pointer is nil", field)
	}
	if *value != nil {
		t.Fatalf("%s inner pointer = %v, want nil", field, **value)
	}
}

func assertClearedInt(t *testing.T, field string, value **int) {
	t.Helper()
	if value == nil {
		t.Fatalf("%s outer pointer is nil", field)
	}
	if *value != nil {
		t.Fatalf("%s inner pointer = %d, want nil", field, **value)
	}
}

func assertClearedTime(t *testing.T, field string, value **time.Time) {
	t.Helper()
	if value == nil {
		t.Fatalf("%s outer pointer is nil", field)
	}
	if *value != nil {
		t.Fatalf("%s inner pointer = %s, want nil", field, (**value).Format(time.RFC3339Nano))
	}
}
