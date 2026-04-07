package data

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type stubAccountSettingsRow struct {
	value any
	err   error
}

func (s stubAccountSettingsRow) Scan(dest ...any) error {
	if s.err != nil {
		return s.err
	}
	if len(dest) != 1 {
		return errors.New("unexpected dest length")
	}
	switch target := dest[0].(type) {
	case *any:
		*target = s.value
		return nil
	default:
		return errors.New("unexpected dest type")
	}
}

type stubAccountSettingsQueryer struct {
	row stubAccountSettingsRow
}

func (s stubAccountSettingsQueryer) QueryRow(context.Context, string, ...any) pgx.Row {
	return s.row
}

func TestAccountSettingsRepositoryReadsStringJSON(t *testing.T) {
	repo := NewAccountSettingsRepository(stubAccountSettingsQueryer{
		row: stubAccountSettingsRow{value: `{"pipeline_trace_enabled":true}`},
	})
	enabled, err := repo.PipelineTraceEnabled(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("PipelineTraceEnabled returned error: %v", err)
	}
	if !enabled {
		t.Fatal("expected pipeline trace to be enabled")
	}
}

func TestAccountSettingsRepositoryHandlesMissingRows(t *testing.T) {
	repo := NewAccountSettingsRepository(stubAccountSettingsQueryer{
		row: stubAccountSettingsRow{err: pgx.ErrNoRows},
	})
	enabled, err := repo.PipelineTraceEnabled(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("PipelineTraceEnabled returned error: %v", err)
	}
	if enabled {
		t.Fatal("expected pipeline trace to be disabled")
	}
}
