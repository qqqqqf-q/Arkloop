// Package runlimit 提供账号级并发 run 槽位计数。
// desktop 单进程部署下进程内计数即为全局计数，直接以内存实现。
package runlimit

import (
	"sync"
	"time"
)

const KeyPrefix = "arkloop:account:active_runs:"

const defaultTTL = 24 * time.Hour

type localCounter struct {
	count     int64
	expiresAt time.Time
}

type localCounterStore struct {
	mu         sync.Mutex
	entries    map[string]localCounter
	now        func() time.Time
	defaultTTL time.Duration
}

var counters = localCounterStore{
	entries:    make(map[string]localCounter),
	now:        time.Now,
	defaultTTL: defaultTTL,
}

// TryAcquire 为 account 获取一个并发 run 槽，达到 maxRuns 时拒绝。
// maxRuns <= 0 表示不限制。
func TryAcquire(key string, maxRuns int64) bool {
	if maxRuns <= 0 {
		return true
	}
	return counters.tryAcquire(key, maxRuns)
}

// Release 释放一个并发 run 槽，计数不低于 0。
func Release(key string) {
	counters.release(key)
}

// Key 根据 accountID 字符串构建计数 key。
func Key(accountID string) string {
	return KeyPrefix + accountID
}

// Set 直接设置 account 的活跃 run 计数（用于 SyncFromDB 修正漂移）。
func Set(key string, count int64) {
	counters.set(key, count, defaultTTL)
}

func (s *localCounterStore) tryAcquire(key string, maxRuns int64) bool {
	if maxRuns <= 0 {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entry := s.current(key)
	if entry.count >= maxRuns {
		return false
	}
	entry.count++
	entry.expiresAt = s.now().Add(s.defaultTTL)
	s.entries[key] = entry
	return true
}

func (s *localCounterStore) release(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := s.current(key)
	if entry.count <= 1 {
		delete(s.entries, key)
		return
	}
	entry.count--
	entry.expiresAt = s.now().Add(s.defaultTTL)
	s.entries[key] = entry
}

func (s *localCounterStore) set(key string, count int64, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if count <= 0 {
		delete(s.entries, key)
		return
	}
	entry := localCounter{count: count, expiresAt: s.now().Add(ttl)}
	s.entries[key] = entry
}

func (s *localCounterStore) current(key string) localCounter {
	entry, ok := s.entries[key]
	if !ok {
		return localCounter{}
	}
	if !entry.expiresAt.IsZero() && !entry.expiresAt.After(s.now()) {
		delete(s.entries, key)
		return localCounter{}
	}
	return entry
}
