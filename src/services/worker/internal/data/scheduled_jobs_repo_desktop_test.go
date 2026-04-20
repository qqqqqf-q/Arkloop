//go:build desktop

package data

import (
	"context"
	"strings"
	"testing"
	"time"

	shareddesktop "arkloop/services/shared/desktop"
	"arkloop/services/shared/eventbus"
	"arkloop/services/shared/pgnotify"
	"arkloop/services/shared/schedulekind"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestDesktopScheduledJobsRepositoryNotifySchedulerPublishesWakeEvent(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewLocalEventBus()
	defer bus.Close()

	previous := shareddesktop.GetEventBus()
	shareddesktop.SetEventBus(bus)
	defer shareddesktop.SetEventBus(previous)

	sub, err := bus.Subscribe(ctx, pgnotify.ChannelScheduledJobs)
	if err != nil {
		t.Fatalf("subscribe scheduled jobs: %v", err)
	}
	defer sub.Close()

	err = DesktopScheduledJobsRepository{}.NotifyScheduler(ctx, nil)
	if err != nil {
		t.Fatalf("notify scheduler: %v", err)
	}

	select {
	case msg := <-sub.Channel():
		if msg.Topic != pgnotify.ChannelScheduledJobs {
			t.Fatalf("message topic = %q, want %q", msg.Topic, pgnotify.ChannelScheduledJobs)
		}
	case <-time.After(time.Second):
		t.Fatal("expected scheduled jobs wake event")
	}
}

func TestDesktopScheduledJobsRepositoryCreateRejectsDeleteAfterRunForNonAt(t *testing.T) {
	job := ScheduledJob{
		ID:             uuid.New(),
		AccountID:      uuid.New(),
		Name:           "interval job",
		PersonaKey:     "assistant",
		Prompt:         "run it",
		ScheduleKind:   schedulekind.Interval,
		IntervalMin:    desktopIntPtr(5),
		DeleteAfterRun: true,
		Enabled:        true,
		Timezone:       "UTC",
	}

	_, err := DesktopScheduledJobsRepository{}.CreateJob(context.Background(), nilDB{}, job)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "delete_after_run") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDesktopApplyJobUpdateCopiesMutableExecutionFields(t *testing.T) {
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

type nilDB struct{}

func (nilDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected Exec call")
}

func (nilDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query call")
}

func (nilDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("unexpected QueryRow call")
}

func (nilDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	panic("unexpected BeginTx call")
}

func desktopIntPtr(v int) *int {
	return &v
}
