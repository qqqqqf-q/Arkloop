//go:build !desktop

package data

import (
	"context"
	"strings"
	"testing"

	"arkloop/services/shared/pgnotify"
	"arkloop/services/shared/schedulekind"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestScheduledJobsRepositoryNotifySchedulerUsesScheduledJobsChannel(t *testing.T) {
	db := &notifyDBStub{}

	err := ScheduledJobsRepository{}.NotifyScheduler(context.Background(), db)
	if err != nil {
		t.Fatalf("notify scheduler: %v", err)
	}
	if db.execSQL != `SELECT pg_notify($1, '')` {
		t.Fatalf("exec sql = %q, want %q", db.execSQL, `SELECT pg_notify($1, '')`)
	}
	if len(db.execArgs) != 1 {
		t.Fatalf("exec args len = %d, want 1", len(db.execArgs))
	}
	if got := db.execArgs[0]; got != pgnotify.ChannelScheduledJobs {
		t.Fatalf("notify channel = %v, want %q", got, pgnotify.ChannelScheduledJobs)
	}
}

func TestScheduledJobsRepositoryCreateRejectsDeleteAfterRunForNonAt(t *testing.T) {
	job := ScheduledJob{
		ID:             uuid.New(),
		AccountID:      uuid.New(),
		Name:           "interval job",
		PersonaKey:     "assistant",
		Prompt:         "run it",
		ScheduleKind:   schedulekind.Interval,
		IntervalMin:    intPtr(5),
		DeleteAfterRun: true,
		Enabled:        true,
		Timezone:       "UTC",
	}

	_, err := ScheduledJobsRepository{}.CreateJob(context.Background(), &notifyDBStub{}, job)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "delete_after_run") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyJobUpdateCopiesMutableExecutionFields(t *testing.T) {
	job := ScheduledJob{
		Name:       "before",
		Prompt:     "before prompt",
		PersonaKey: "before-persona",
		Model:      "before-model",
		WorkDir:    "/before",
	}
	personaKey := "after-persona"
	model := "after-model"
	workDir := "/after"
	upd := UpdateJobParams{
		PersonaKey: &personaKey,
		Model:      &model,
		WorkDir:    &workDir,
	}

	applyJobUpdate(&job, upd)

	if job.PersonaKey != personaKey {
		t.Fatalf("persona_key = %q, want %q", job.PersonaKey, personaKey)
	}
	if job.Model != model {
		t.Fatalf("model = %q, want %q", job.Model, model)
	}
	if job.WorkDir != workDir {
		t.Fatalf("work_dir = %q, want %q", job.WorkDir, workDir)
	}
}

func intPtr(v int) *int {
	return &v
}

type notifyDBStub struct {
	execSQL  string
	execArgs []any
}

func (s *notifyDBStub) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	s.execSQL = sql
	s.execArgs = append([]any(nil), args...)
	return pgconn.NewCommandTag("SELECT 1"), nil
}

func (s *notifyDBStub) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query call")
}

func (s *notifyDBStub) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("unexpected QueryRow call")
}

func (s *notifyDBStub) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	panic("unexpected BeginTx call")
}
