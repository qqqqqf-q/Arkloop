package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"arkloop/services/shared/objectstore"
)

type fakeOpener struct {
	store objectstore.Store
	err   error
	mu    sync.Mutex
	seen  []string
}

func (o *fakeOpener) Open(_ context.Context, bucket string) (objectstore.Store, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seen = append(o.seen, bucket)
	if o.err != nil {
		return nil, o.err
	}
	return o.store, nil
}

type fakeStore struct {
	mu       sync.Mutex
	objects  map[string][]byte
	meta     map[string]objectstore.ObjectInfo
	getCount map[string]int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		objects:  make(map[string][]byte),
		meta:     make(map[string]objectstore.ObjectInfo),
		getCount: make(map[string]int),
	}
}

func (s *fakeStore) Put(_ context.Context, key string, data []byte) error {
	return s.PutObject(context.Background(), key, data, objectstore.PutOptions{})
}

func (s *fakeStore) PutObject(_ context.Context, key string, data []byte, options objectstore.PutOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := append([]byte(nil), data...)
	s.objects[key] = cp
	s.meta[key] = objectstore.ObjectInfo{Key: key, Size: int64(len(cp)), ContentType: options.ContentType, ETag: fmt.Sprintf("etag-%d", len(cp))}
	return nil
}

func (s *fakeStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.objects[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	s.getCount[key]++
	return append([]byte(nil), value...), nil
}

func (s *fakeStore) GetWithContentType(ctx context.Context, key string) ([]byte, string, error) {
	data, err := s.Get(ctx, key)
	if err != nil {
		return nil, "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return data, s.meta[key].ContentType, nil
}

func (s *fakeStore) Head(_ context.Context, key string) (objectstore.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, ok := s.meta[key]
	if !ok {
		return objectstore.ObjectInfo{}, os.ErrNotExist
	}
	return info, nil
}

func (s *fakeStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	delete(s.meta, key)
	return nil
}

func (s *fakeStore) ListPrefix(_ context.Context, prefix string) ([]objectstore.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]objectstore.ObjectInfo, 0)
	for key, info := range s.meta {
		if len(prefix) == 0 || strings.HasPrefix(key, prefix) {
			items = append(items, info)
		}
	}
	return items, nil
}

func TestNewSnapshotStoreOpensSnapshotBucket(t *testing.T) {
	store := newFakeStore()
	opener := &fakeOpener{store: store}
	cacheDir := t.TempDir()

	_, err := NewSnapshotStore(context.Background(), opener, cacheDir)
	if err != nil {
		t.Fatalf("new snapshot store: %v", err)
	}
	if len(opener.seen) != 1 || opener.seen[0] != snapshotBucket {
		t.Fatalf("unexpected opened buckets: %#v", opener.seen)
	}
}

func TestObjectSnapshotStoreUploadExistsAndDownload(t *testing.T) {
	store := newFakeStore()
	snapshotStore := &ObjectSnapshotStore{store: store, cacheBaseDir: t.TempDir()}

	memPath := filepath.Join(t.TempDir(), "mem.snap")
	diskPath := filepath.Join(t.TempDir(), "disk.snap")
	if err := os.WriteFile(memPath, []byte("mem-data"), 0o600); err != nil {
		t.Fatalf("write mem fixture: %v", err)
	}
	if err := os.WriteFile(diskPath, []byte("disk-data"), 0o600); err != nil {
		t.Fatalf("write disk fixture: %v", err)
	}

	if err := snapshotStore.Upload(context.Background(), "tmpl-1", memPath, diskPath); err != nil {
		t.Fatalf("upload: %v", err)
	}
	exists, err := snapshotStore.Exists(context.Background(), "tmpl-1")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !exists {
		t.Fatal("expected snapshot to exist")
	}

	memLocal, diskLocal, err := snapshotStore.Download(context.Background(), "tmpl-1")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	memData, _ := os.ReadFile(memLocal)
	diskData, _ := os.ReadFile(diskLocal)
	if string(memData) != "mem-data" || string(diskData) != "disk-data" {
		t.Fatalf("unexpected downloaded data: mem=%q disk=%q", memData, diskData)
	}

	_, _, err = snapshotStore.Download(context.Background(), "tmpl-1")
	if err != nil {
		t.Fatalf("second download: %v", err)
	}
	if store.getCount["tmpl-1/mem.snap"] != 1 || store.getCount["tmpl-1/disk.snap"] != 1 {
		t.Fatalf("expected cache hit, getCount=%#v", store.getCount)
	}
}

func TestObjectSnapshotStoreExistsReturnsFalseOnMissingObject(t *testing.T) {
	snapshotStore := &ObjectSnapshotStore{store: newFakeStore(), cacheBaseDir: t.TempDir()}
	exists, err := snapshotStore.Exists(context.Background(), "missing")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if exists {
		t.Fatal("expected missing snapshot")
	}
}

func TestObjectSnapshotStoreWithFilesystemBackend(t *testing.T) {
	opener := objectstore.NewFilesystemOpener(t.TempDir())
	snapshotStore, err := NewSnapshotStore(context.Background(), opener, t.TempDir())
	if err != nil {
		t.Fatalf("new snapshot store: %v", err)
	}

	memPath := filepath.Join(t.TempDir(), "mem.snap")
	diskPath := filepath.Join(t.TempDir(), "disk.snap")
	if err := os.WriteFile(memPath, []byte("mem-data"), 0o600); err != nil {
		t.Fatalf("write mem fixture: %v", err)
	}
	if err := os.WriteFile(diskPath, []byte("disk-data"), 0o600); err != nil {
		t.Fatalf("write disk fixture: %v", err)
	}

	if err := snapshotStore.Upload(context.Background(), "tmpl-fs", memPath, diskPath); err != nil {
		t.Fatalf("upload: %v", err)
	}
	memLocal, diskLocal, err := snapshotStore.Download(context.Background(), "tmpl-fs")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	memData, _ := os.ReadFile(memLocal)
	diskData, _ := os.ReadFile(diskLocal)
	if string(memData) != "mem-data" || string(diskData) != "disk-data" {
		t.Fatalf("unexpected downloaded data: mem=%q disk=%q", memData, diskData)
	}
}
