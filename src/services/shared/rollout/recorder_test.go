package rollout

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"

	"arkloop/services/shared/objectstore"

	"github.com/google/uuid"
)

type recorderBlobStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (s *recorderBlobStore) Put(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = map[string][]byte{}
	}
	s.data[key] = append([]byte(nil), value...)
	return nil
}

func (s *recorderBlobStore) PutIfAbsent(_ context.Context, _ string, _ []byte) (bool, error) {
	return false, nil
}

func (s *recorderBlobStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.data[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), value...), nil
}

func (s *recorderBlobStore) Head(_ context.Context, key string) (objectstore.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.data[key]
	if !ok {
		return objectstore.ObjectInfo{}, os.ErrNotExist
	}
	return objectstore.ObjectInfo{Key: key, Size: int64(len(value))}, nil
}

func (s *recorderBlobStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func (s *recorderBlobStore) ListPrefix(_ context.Context, _ string) ([]objectstore.ObjectInfo, error) {
	return nil, nil
}

func (s *recorderBlobStore) WriteJSONAtomic(ctx context.Context, key string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.Put(ctx, key, payload)
}

func TestRecorderCloseFlushesBufferedItems(t *testing.T) {
	store := &recorderBlobStore{}
	runID := uuid.New()
	rec := NewRecorder(store, runID)
	rec.Start(context.Background())

	for i := 0; i < 5; i++ {
		if err := rec.Append(context.Background(), RolloutItem{Type: "turn_start"}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := rec.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}

	items, err := NewReader(store).ReadRollout(context.Background(), runID)
	if err != nil {
		t.Fatalf("read rollout: %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("expected 5 items, got %d", len(items))
	}
}
