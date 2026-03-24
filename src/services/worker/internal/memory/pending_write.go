package memory

import "sync"

// PendingWrite 表示一次待异步刷写到长期记忆的显式写入。
type PendingWrite struct {
	TaskID string
	Ident MemoryIdentity
	Scope MemoryScope
	Entry MemoryEntry
}

// PendingWriteBuffer 保存当前 run 内待刷写的 memory writes。
type PendingWriteBuffer struct {
	mu     sync.Mutex
	writes []PendingWrite
}

func NewPendingWriteBuffer() *PendingWriteBuffer {
	return &PendingWriteBuffer{}
}

func (b *PendingWriteBuffer) Append(write PendingWrite) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.writes = append(b.writes, write)
	b.mu.Unlock()
}

func (b *PendingWriteBuffer) Drain() []PendingWrite {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := append([]PendingWrite(nil), b.writes...)
	b.writes = nil
	return out
}

func (b *PendingWriteBuffer) Len() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.writes)
}
