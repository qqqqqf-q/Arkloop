//go:build darwin

package vz

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
)

func newTestConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		WarmSizes:             map[string]int{},
		RefillIntervalSeconds: 5,
		MaxRefillConcurrency:  2,
		SocketBaseDir:         t.TempDir(),
		Logger:                logging.NewJSONLogger("test", io.Discard),
	}
}

func TestPool_StartDrain_NoWarm(t *testing.T) {
	pool := New(newTestConfig(t))
	pool.Start()
	pool.Drain(context.Background())
	// Must complete without panic or hang.
}

func TestPool_Destroy_NotFound(t *testing.T) {
	pool := New(newTestConfig(t))

	pool.DestroyVM(nil, filepath.Join(t.TempDir(), "nonexistent-session"))

	stats := pool.Stats()
	if stats.TotalDestroyed != 1 {
		t.Errorf("expected TotalDestroyed=1, got %d", stats.TotalDestroyed)
	}
}

func TestPool_Destroy_Active(t *testing.T) {
	pool := New(newTestConfig(t))

	socketDir := filepath.Join(t.TempDir(), "test-session")
	if err := os.MkdirAll(socketDir, 0o700); err != nil {
		t.Fatal(err)
	}

	pool.mu.Lock()
	pool.active["test-session"] = &entry{
		session: &session.Session{
			ID:        "test-session",
			Tier:      "lite",
			SocketDir: socketDir,
		},
		vm: nil,
	}
	pool.mu.Unlock()

	pool.DestroyVM(nil, socketDir)

	pool.mu.Lock()
	_, exists := pool.active["test-session"]
	pool.mu.Unlock()

	if exists {
		t.Fatal("session should be removed from active map")
	}

	if _, err := os.Stat(socketDir); !os.IsNotExist(err) {
		t.Fatal("socketDir should be removed")
	}

	stats := pool.Stats()
	if stats.TotalDestroyed != 1 {
		t.Errorf("expected TotalDestroyed=1, got %d", stats.TotalDestroyed)
	}
}

func TestPool_Stats_AfterOperations(t *testing.T) {
	pool := New(newTestConfig(t))

	// Fresh pool should have zero counters.
	stats := pool.Stats()
	if stats.TotalCreated != 0 {
		t.Errorf("expected TotalCreated=0, got %d", stats.TotalCreated)
	}
	if stats.TotalDestroyed != 0 {
		t.Errorf("expected TotalDestroyed=0, got %d", stats.TotalDestroyed)
	}

	// Destroy two non-existent sessions.
	pool.DestroyVM(nil, "a")
	pool.DestroyVM(nil, "b")

	stats = pool.Stats()
	if stats.TotalDestroyed != 2 {
		t.Errorf("expected TotalDestroyed=2, got %d", stats.TotalDestroyed)
	}

	// Insert and destroy an active entry.
	socketDir := filepath.Join(t.TempDir(), "s1")
	if err := os.MkdirAll(socketDir, 0o700); err != nil {
		t.Fatal(err)
	}

	pool.mu.Lock()
	pool.active["s1"] = &entry{
		session: &session.Session{ID: "s1", Tier: "lite", SocketDir: socketDir},
		vm:      nil,
	}
	pool.mu.Unlock()

	pool.DestroyVM(nil, socketDir)

	stats = pool.Stats()
	if stats.TotalDestroyed != 3 {
		t.Errorf("expected TotalDestroyed=3, got %d", stats.TotalDestroyed)
	}
}

func TestPool_Drain_CleansActiveEntries(t *testing.T) {
	pool := New(newTestConfig(t))

	// Insert several active entries.
	dirs := make([]string, 3)
	for i, id := range []string{"a", "b", "c"} {
		dirs[i] = filepath.Join(t.TempDir(), id)
		if err := os.MkdirAll(dirs[i], 0o700); err != nil {
			t.Fatal(err)
		}
		pool.mu.Lock()
		pool.active[id] = &entry{
			session: &session.Session{ID: id, Tier: "lite", SocketDir: dirs[i]},
			vm:      nil,
		}
		pool.mu.Unlock()
	}

	pool.Start()
	pool.Drain(context.Background())

	// Active map should be empty.
	pool.mu.Lock()
	remaining := len(pool.active)
	pool.mu.Unlock()

	if remaining != 0 {
		t.Errorf("expected 0 active entries after Drain, got %d", remaining)
	}

	// All socket dirs should be cleaned up.
	for _, d := range dirs {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("socketDir %s should be removed after Drain", d)
		}
	}

	stats := pool.Stats()
	if stats.TotalDestroyed != 3 {
		t.Errorf("expected TotalDestroyed=3, got %d", stats.TotalDestroyed)
	}
}

func TestPool_Ready_EmptyWarmSizes(t *testing.T) {
	pool := New(newTestConfig(t))

	if !pool.Ready() {
		t.Error("pool with no warm sizes should report Ready")
	}
}

func TestNew_ChannelSizes(t *testing.T) {
	cfg := Config{
		WarmSizes: map[string]int{
			session.TierLite:    2,
			session.TierPro:     3,
			session.TierBrowser: 1,
		},
		MaxRefillConcurrency: 4,
		Logger:               logging.NewJSONLogger("test", io.Discard),
	}
	p := New(cfg)

	for tier, size := range cfg.WarmSizes {
		ch, ok := p.ready[tier]
		if !ok {
			t.Fatalf("expected ready channel for tier %q", tier)
		}
		if cap(ch) != size {
			t.Fatalf("tier %q: expected channel cap %d, got %d", tier, size, cap(ch))
		}
	}

	if len(p.active) != 0 {
		t.Fatalf("expected empty active map, got %d entries", len(p.active))
	}
	if cap(p.sem) != cfg.MaxRefillConcurrency {
		t.Fatalf("expected sem capacity %d, got %d", cfg.MaxRefillConcurrency, cap(p.sem))
	}
	if p.stop == nil {
		t.Fatal("expected stop channel to be created")
	}
}

func TestNew_ZeroWarmSizes(t *testing.T) {
	cfg := Config{
		WarmSizes: map[string]int{
			session.TierLite: 0,
			session.TierPro:  0,
		},
		MaxRefillConcurrency: 2,
		Logger:               logging.NewJSONLogger("test", io.Discard),
	}
	p := New(cfg)

	if len(p.ready) != 0 {
		t.Fatalf("expected empty ready map for zero warm sizes, got %d entries", len(p.ready))
	}
	if !p.Ready() {
		t.Fatal("expected Ready()=true when all warm sizes are zero")
	}
}

func TestStats_EmptyPool(t *testing.T) {
	cfg := Config{
		WarmSizes: map[string]int{
			session.TierLite: 2,
			session.TierPro:  1,
		},
		MaxRefillConcurrency: 2,
		Logger:               logging.NewJSONLogger("test", io.Discard),
	}
	p := New(cfg)
	stats := p.Stats()

	for tier, target := range cfg.WarmSizes {
		if stats.TargetByTier[tier] != target {
			t.Fatalf("tier %q: expected target %d, got %d", tier, target, stats.TargetByTier[tier])
		}
		if stats.ReadyByTier[tier] != 0 {
			t.Fatalf("tier %q: expected ready 0, got %d", tier, stats.ReadyByTier[tier])
		}
	}
	if stats.TotalCreated != 0 {
		t.Fatalf("expected TotalCreated=0, got %d", stats.TotalCreated)
	}
	if stats.TotalDestroyed != 0 {
		t.Fatalf("expected TotalDestroyed=0, got %d", stats.TotalDestroyed)
	}
}

func TestReady_EmptyPoolWithTargets(t *testing.T) {
	cfg := Config{
		WarmSizes: map[string]int{
			session.TierLite: 1,
		},
		MaxRefillConcurrency: 1,
		Logger:               logging.NewJSONLogger("test", io.Discard),
	}
	p := New(cfg)

	if p.Ready() {
		t.Fatal("expected Ready()=false when warm targets > 0 and pool is empty")
	}
}

func TestGenerateID(t *testing.T) {
	id := generateID()

	if !strings.HasPrefix(id, "vz-") {
		t.Fatalf("expected id to start with \"vz-\", got %q", id)
	}
	// "vz-" (3 chars) + 16 hex chars = 19 total
	if len(id) != 19 {
		t.Fatalf("expected id length 19, got %d (%q)", len(id), id)
	}

	id2 := generateID()
	if id == id2 {
		t.Fatalf("expected two generateID calls to produce different values, both returned %q", id)
	}
}

// Compile-time check: vzConn must implement net.Conn.
var _ net.Conn = (*vzConn)(nil)

func TestVsockAddr(t *testing.T) {
	a := vsockAddr{label: "local"}
	if a.Network() != "vsock" {
		t.Fatalf("expected Network()=\"vsock\", got %q", a.Network())
	}
	if a.String() != "vz-local" {
		t.Fatalf("expected String()=\"vz-local\", got %q", a.String())
	}
}

func TestCopyFile(t *testing.T) {
	content := []byte("hello vz sandbox")

	src, err := os.CreateTemp(t.TempDir(), "src-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.Write(content); err != nil {
		t.Fatal(err)
	}
	src.Close()

	dst := filepath.Join(t.TempDir(), "dst-copy")
	if err := copyFile(src.Name(), dst); err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("expected %q, got %q", content, got)
	}
}
