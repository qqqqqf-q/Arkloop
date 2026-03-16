//go:build darwin

package vz

import (
	"context"
	"os"
	"testing"
	"time"

	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
)

const (
	testKernelPath = "/tmp/vz-test/vmlinux"
	testInitrdPath = "/tmp/vz-test/initramfs-custom.gz"
	testRootfsPath = "/tmp/vz-test/rootfs-full/python3.12.ext4"
)

func skipIfNoAssets(t *testing.T) {
	t.Helper()
	for _, p := range []string{testKernelPath, testRootfsPath} {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			t.Skipf("asset not found at %s; run 'make setup-vm-dev' first", p)
		}
	}
}

func newTestPool(t *testing.T) *Pool {
	t.Helper()
	socketDir := t.TempDir()
	logger := logging.NewJSONLogger("vz-test", os.Stdout)

	initrd := ""
	if _, err := os.Stat(testInitrdPath); err == nil {
		initrd = testInitrdPath
	}

	return New(Config{
		WarmSizes:             map[string]int{},
		RefillIntervalSeconds: 60,
		MaxRefillConcurrency:  1,
		KernelImagePath:       testKernelPath,
		InitrdPath:            initrd,
		RootfsPath:            testRootfsPath,
		SocketBaseDir:         socketDir,
		BootTimeoutSeconds:    60,
		GuestAgentPort:        8080,
		Logger:                logger,
	})
}

func TestIntegration_VMBoot(t *testing.T) {
	skipIfNoAssets(t)
	if os.Getenv("VZ_INTEGRATION") == "" {
		t.Skip("set VZ_INTEGRATION=1 to run VZ integration tests")
	}

	pool := newTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	t.Log("acquiring VM session...")
	sess, proc, err := pool.Acquire(ctx, session.TierLite)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	t.Logf("VM acquired: Tier=%s, SocketDir=%s", sess.Tier, sess.SocketDir)
	defer pool.DestroyVM(proc, sess.SocketDir)

	// Test 1: Simple shell command
	t.Log("executing 'echo hello'...")
	result, err := sess.Exec(ctx, session.ExecJob{
		Language:  "shell",
		Code:      "echo hello",
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d (stderr: %s)", result.ExitCode, result.Stderr)
	}
	if expected := "hello\n"; result.Stdout != expected {
		t.Errorf("expected stdout %q, got %q", expected, result.Stdout)
	}
	t.Logf("exec result: stdout=%q stderr=%q exit=%d", result.Stdout, result.Stderr, result.ExitCode)

	// Test 2: Verify architecture
	t.Log("executing 'uname -m'...")
	result2, err := sess.Exec(ctx, session.ExecJob{
		Language:  "shell",
		Code:      "uname -m",
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("uname exec failed: %v", err)
	}
	t.Logf("uname: %s", result2.Stdout)

	// Test 3: Pool stats
	stats := pool.Stats()
	if stats.TotalCreated != 1 {
		t.Errorf("expected TotalCreated=1, got %d", stats.TotalCreated)
	}

	t.Log("integration test passed!")
}

func TestIntegration_MultipleExec(t *testing.T) {
	skipIfNoAssets(t)
	if os.Getenv("VZ_INTEGRATION") == "" {
		t.Skip("set VZ_INTEGRATION=1 to run VZ integration tests")
	}

	pool := newTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	sess, proc, err := pool.Acquire(ctx, session.TierLite)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	defer pool.DestroyVM(proc, sess.SocketDir)

	commands := []struct {
		code     string
		expected string
	}{
		{"echo 'test1'", "test1\n"},
		{"echo 'test2'", "test2\n"},
		{"date +%s", ""},    // just check no error
		{"uname -s", "Linux\n"},
	}

	for i, cmd := range commands {
		result, err := sess.Exec(ctx, session.ExecJob{
			Language:  "shell",
			Code:      cmd.code,
			TimeoutMs: 5000,
		})
		if err != nil {
			t.Fatalf("command %d (%q) failed: %v", i, cmd.code, err)
		}
		if result.ExitCode != 0 {
			t.Errorf("command %d (%q): exit code %d, stderr: %s", i, cmd.code, result.ExitCode, result.Stderr)
		}
		if cmd.expected != "" && result.Stdout != cmd.expected {
			t.Errorf("command %d (%q): expected %q, got %q", i, cmd.code, cmd.expected, result.Stdout)
		}
		t.Logf("command %d: %q -> %q (exit %d)", i, cmd.code, result.Stdout, result.ExitCode)
	}
}

func TestIntegration_Python(t *testing.T) {
	skipIfNoAssets(t)
	if os.Getenv("VZ_INTEGRATION") == "" {
		t.Skip("set VZ_INTEGRATION=1 to run VZ integration tests")
	}

	pool := newTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	sess, proc, err := pool.Acquire(ctx, session.TierLite)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	defer pool.DestroyVM(proc, sess.SocketDir)

	result, err := sess.Exec(ctx, session.ExecJob{
		Language:  "python",
		Code:      "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')",
		TimeoutMs: 30000,
	})
	if err != nil {
		t.Fatalf("python version check failed: %v", err)
	}
	t.Logf("Python version: %s", result.Stdout)
	if result.ExitCode != 0 {
		t.Errorf("python exit code %d, stderr: %s", result.ExitCode, result.Stderr)
	}

	result2, err := sess.Exec(ctx, session.ExecJob{
		Language:  "python",
		Code:      "print(sum(range(1, 101)))",
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("python sum failed: %v", err)
	}
	if result2.Stdout != "5050\n" {
		t.Errorf("expected 5050, got %q", result2.Stdout)
	}
	t.Logf("Python sum(1..100) = %s", result2.Stdout)
}

func TestIntegration_WarmPool(t *testing.T) {
	skipIfNoAssets(t)
	if os.Getenv("VZ_INTEGRATION") == "" {
		t.Skip("set VZ_INTEGRATION=1 to run VZ integration tests")
	}

	socketDir := t.TempDir()
	logger := logging.NewJSONLogger("vz-warm", os.Stdout)

	initrd := ""
	if _, err := os.Stat(testInitrdPath); err == nil {
		initrd = testInitrdPath
	}

	pool := New(Config{
		WarmSizes:             map[string]int{session.TierLite: 1},
		RefillIntervalSeconds: 5,
		MaxRefillConcurrency:  1,
		KernelImagePath:       testKernelPath,
		InitrdPath:            initrd,
		RootfsPath:            testRootfsPath,
		SocketBaseDir:         socketDir,
		BootTimeoutSeconds:    60,
		GuestAgentPort:        8080,
		Logger:                logger,
	})
	pool.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pool.Drain(ctx)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	t.Log("waiting for warm pool to fill...")
	fillStart := time.Now()
	for !pool.Ready() {
		if time.Since(fillStart) > 90*time.Second {
			t.Fatal("warm pool did not fill within 90s")
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Logf("warm pool filled in %v", time.Since(fillStart))

	stats := pool.Stats()
	if stats.ReadyByTier[session.TierLite] != 1 {
		t.Errorf("expected 1 ready lite VM, got %d", stats.ReadyByTier[session.TierLite])
	}

	acquireStart := time.Now()
	sess, proc, err := pool.Acquire(ctx, session.TierLite)
	if err != nil {
		t.Fatalf("warm Acquire failed: %v", err)
	}
	t.Logf("warm Acquire took %v", time.Since(acquireStart))
	defer pool.DestroyVM(proc, sess.SocketDir)

	result, err := sess.Exec(ctx, session.ExecJob{
		Language:  "shell",
		Code:      "echo warm-pool-works",
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if result.Stdout != "warm-pool-works\n" {
		t.Errorf("expected 'warm-pool-works', got %q", result.Stdout)
	}

	// Cold acquire
	coldStart := time.Now()
	sess2, proc2, err := pool.Acquire(ctx, session.TierLite)
	if err != nil {
		t.Fatalf("cold Acquire failed: %v", err)
	}
	t.Logf("cold Acquire took %v", time.Since(coldStart))
	defer pool.DestroyVM(proc2, sess2.SocketDir)
}
