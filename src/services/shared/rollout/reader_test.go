package rollout

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"arkloop/services/shared/objectstore"

	"github.com/google/uuid"
)

type fakeBlobStore struct {
	data map[string][]byte
}

func (f *fakeBlobStore) Put(_ context.Context, _ string, _ []byte) error {
	return errors.New("not implemented")
}

func (f *fakeBlobStore) PutIfAbsent(_ context.Context, _ string, _ []byte) (bool, error) {
	return false, errors.New("not implemented")
}

func (f *fakeBlobStore) Get(_ context.Context, key string) ([]byte, error) {
	if f.data == nil {
		return nil, fmt.Errorf("not found: %s", key)
	}
	b, ok := f.data[key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	return b, nil
}

func (f *fakeBlobStore) Head(_ context.Context, _ string) (objectstore.ObjectInfo, error) {
	return objectstore.ObjectInfo{}, errors.New("not implemented")
}

func (f *fakeBlobStore) Delete(_ context.Context, _ string) error {
	return errors.New("not implemented")
}

func (f *fakeBlobStore) ListPrefix(_ context.Context, _ string) ([]objectstore.ObjectInfo, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeBlobStore) WriteJSONAtomic(_ context.Context, _ string, _ any) error {
	return errors.New("not implemented")
}

func TestReaderHandlesLargeLine(t *testing.T) {
	runID := uuid.New()
	line := fmt.Sprintf(
		`{"type":"assistant_message","timestamp":"2024-01-01T00:00:00Z","payload":{"content":"%s"}}`,
		strings.Repeat("a", 100_000),
	)
	data := []byte(line + "\n")
	store := &fakeBlobStore{
		data: map[string][]byte{
			"run/" + runID.String() + ".jsonl": data,
		},
	}

	items, err := NewReader(store).ReadRollout(context.Background(), runID)
	if err != nil {
		t.Fatalf("ReadRollout returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Type != "assistant_message" {
		t.Fatalf("unexpected type: %q", items[0].Type)
	}
}

func TestReaderReportsJSONError(t *testing.T) {
	runID := uuid.New()
	data := []byte(
		`{"type":"assistant_message","timestamp":"2024-01-01T00:00:00Z","payload":{"content":"ok"}}` + "\n" +
			`{"type": "assistant_message"` + "\n",
	)
	store := &fakeBlobStore{
		data: map[string][]byte{
			"run/" + runID.String() + ".jsonl": data,
		},
	}

	_, err := NewReader(store).ReadRollout(context.Background(), runID)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "parse rollout line 2") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReaderReadsSegmentedRollout(t *testing.T) {
	runID := uuid.New()
	store := &fakeBlobStore{
		data: map[string][]byte{
			manifestKey(runID):   []byte(`{"schema_version":1,"segments":["` + segmentKey(runID, 0) + `","` + segmentKey(runID, 1) + `"]}`),
			segmentKey(runID, 0): []byte(`{"type":"turn_start","timestamp":"2024-01-01T00:00:00Z","payload":{"turn_index":1}}` + "\n"),
			segmentKey(runID, 1): []byte(`{"type":"run_end","timestamp":"2024-01-01T00:00:01Z","payload":{"final_status":"completed"}}` + "\n"),
		},
	}

	items, err := NewReader(store).ReadRollout(context.Background(), runID)
	if err != nil {
		t.Fatalf("ReadRollout returned error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
}

func TestReaderFallsBackToLegacyWhenManifestEmpty(t *testing.T) {
	runID := uuid.New()
	store := &fakeBlobStore{
		data: map[string][]byte{
			manifestKey(runID):      []byte(`{"schema_version":1,"segments":[]}`),
			legacyRolloutKey(runID): []byte(`{"type":"run_end","timestamp":"2024-01-01T00:00:01Z","payload":{"final_status":"completed"}}` + "\n"),
		},
	}

	items, err := NewReader(store).ReadRollout(context.Background(), runID)
	if err != nil {
		t.Fatalf("ReadRollout returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
}
