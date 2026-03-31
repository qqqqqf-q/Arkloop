package rollout

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"arkloop/services/shared/objectstore"

	"github.com/google/uuid"
)

// Recorder 将 RolloutItem 异步写入 S3（JSONL 格式）。
// 线程安全，通过 buffered channel + flush goroutine 实现。
type Recorder struct {
	store    objectstore.BlobStore
	runID    uuid.UUID
	buf      chan RolloutItem
	flushReq chan chan error
	closed   chan struct{}
	wg       sync.WaitGroup
	mu       sync.Mutex
	flushed  bool
	segment  int
}

const recorderBufSize = 64 // channel buffer size

func NewRecorder(store objectstore.BlobStore, runID uuid.UUID) *Recorder {
	return &Recorder{
		store:    store,
		runID:    runID,
		buf:      make(chan RolloutItem, recorderBufSize),
		flushReq: make(chan chan error),
		closed:   make(chan struct{}),
	}
}

// Append 将一个 RolloutItem 异步写入 S3。不阻塞调用方。
func (r *Recorder) Append(ctx context.Context, item RolloutItem) error {
	r.mu.Lock()
	flushed := r.flushed
	r.mu.Unlock()
	if flushed {
		return nil
	}
	select {
	case r.buf <- item:
		return nil
	case <-r.closed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// AppendSync 同步写入（用于 run_end 等必须确认的条目）。
func (r *Recorder) AppendSync(ctx context.Context, item RolloutItem) error {
	if err := r.Append(ctx, item); err != nil {
		return err
	}
	reply := make(chan error, 1)
	select {
	case r.flushReq <- reply:
	case <-r.closed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-reply:
		return err
	case <-r.closed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Start 启动后台 flush goroutine。defer Recorder.Close() 调用。
func (r *Recorder) Start(ctx context.Context) {
	r.wg.Add(1)
	go r.flushLoop(ctx)
}

// Close 等待所有缓冲数据写入 S3，然后关闭。
func (r *Recorder) Close(ctx context.Context) error {
	close(r.closed)
	r.wg.Wait()
	return nil
}

func (r *Recorder) flushLoop(ctx context.Context) {
	defer r.wg.Done()
	var batch []RolloutItem
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.closed:
			batch = r.drainBuffered(batch)
			r.flushBatch(ctx, batch)
			r.markFlushed()
			return
		case <-ticker.C:
			r.flushBatch(ctx, batch)
			batch = nil
		case reply := <-r.flushReq:
			batch = r.drainBuffered(batch)
			err := r.flushItems(ctx, batch)
			batch = nil
			reply <- err
		case item := <-r.buf:
			batch = append(batch, item)
			if len(batch) >= recorderBufSize/2 {
				r.flushBatch(ctx, batch)
				batch = nil
			}
		}
	}
}

func (r *Recorder) flushBatch(ctx context.Context, batch []RolloutItem) {
	if len(batch) == 0 {
		return
	}
	_ = r.flushItems(ctx, batch)
}

func (r *Recorder) drainBuffered(batch []RolloutItem) []RolloutItem {
	for {
		select {
		case item := <-r.buf:
			batch = append(batch, item)
		default:
			return batch
		}
	}
}

func (r *Recorder) flushItems(ctx context.Context, items []RolloutItem) error {
	var data []byte
	for _, item := range items {
		enc, err := json.Marshal(item)
		if err != nil {
			continue
		}
		data = append(data, enc...)
		data = append(data, '\n')
	}
	if len(data) == 0 {
		return nil
	}

	r.mu.Lock()
	m, err := r.readManifest(ctx)
	if err != nil {
		r.mu.Unlock()
		return err
	}
	if r.segment < len(m.Segments) {
		r.segment = len(m.Segments)
	}
	segment := r.segment
	key := segmentKey(r.runID, segment)
	m.Segments = append(m.Segments, key)
	r.segment++

	if err := r.store.Put(ctx, key, data); err != nil {
		r.mu.Unlock()
		return err
	}
	if err := r.store.WriteJSONAtomic(ctx, manifestKey(r.runID), m); err != nil {
		r.mu.Unlock()
		return err
	}
	r.mu.Unlock()
	return nil
}

func (r *Recorder) readManifest(ctx context.Context) (manifest, error) {
	data, err := r.store.Get(ctx, manifestKey(r.runID))
	if err != nil {
		if objectstore.IsNotFound(err) {
			return manifest{SchemaVersion: 1}, nil
		}
		return manifest{}, err
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return manifest{}, err
	}
	if m.SchemaVersion == 0 {
		m.SchemaVersion = 1
	}
	return m, nil
}

func (r *Recorder) markFlushed() {
	r.mu.Lock()
	r.flushed = true
	r.mu.Unlock()
}
