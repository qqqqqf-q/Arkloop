//go:build !desktop

package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"arkloop/services/worker/internal/testutil"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEscapeTelegramMarkdownV2EscapesReservedCharacters(t *testing.T) {
	input := "_*[]()~`>#+-=|{}.!"
	want := "\\_\\*\\[\\]\\(\\)\\~\\`\\>\\#\\+\\-\\=\\|\\{\\}\\.\\!"

	if got := escapeTelegramMarkdownV2(input); got != want {
		t.Fatalf("unexpected escaped text: got %q want %q", got, want)
	}
}

func TestSplitTelegramMessagePrefersParagraphBoundary(t *testing.T) {
	segments := splitTelegramMessage("alpha paragraph.\n\nbeta gamma delta", 20)
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	if segments[0] != "alpha paragraph." {
		t.Fatalf("unexpected first segment: %q", segments[0])
	}
	if segments[1] != "beta gamma delta" {
		t.Fatalf("unexpected second segment: %q", segments[1])
	}
}

func TestSplitTelegramMessageFallsBackToHardLimit(t *testing.T) {
	segments := splitTelegramMessage(strings.Repeat("x", 9), 4)
	if len(segments) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(segments))
	}
	if segments[0] != "xxxx" || segments[1] != "xxxx" || segments[2] != "x" {
		t.Fatalf("unexpected hard split result: %#v", segments)
	}
}

func TestRecordChannelDeliveryFailureAppendsRunEvent(t *testing.T) {
	db := testutil.SetupPostgresDatabase(t, "pipeline_channel_delivery")
	pool, err := pgxpool.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	accountID := uuid.New()
	projectID := uuid.New()
	threadID := uuid.New()
	runID := uuid.New()
	seedPipelineThread(t, pool, accountID, threadID, projectID)
	seedPipelineRun(t, pool, accountID, threadID, runID, nil)

	recordChannelDeliveryFailure(context.Background(), pool, runID, errors.New("send boom"))

	var errorMessage string
	if err := pool.QueryRow(
		context.Background(),
		`SELECT data_json->>'error'
		   FROM run_events
		  WHERE run_id = $1 AND type = 'run.channel_delivery_failed'
		  ORDER BY seq DESC
		  LIMIT 1`,
		runID,
	).Scan(&errorMessage); err != nil {
		t.Fatalf("load run event: %v", err)
	}
	if errorMessage != "send boom" {
		t.Fatalf("unexpected error payload: %q", errorMessage)
	}
}
