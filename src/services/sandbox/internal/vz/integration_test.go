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
	for _, p := range []string{testKernelPath, testInitrdPath, testRootfsPath} {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			t.Skipf("asset not found at %s; build VM assets first", p)
		}
	}
}

func newTestPool(t *testing.T) *Pool {
	t.Helper()
	socketDir := t.TempDir()
	logger := logging.NewJSONLogger("vz-test", os.Stdout)
	return New(Config{
		WarmSizes:             map[string]int{},
		RefillIntervalSeconds: 60,
		MaxRefillConcurrency:  1,
		KernelImagePath:       testKernelPath,
		InitrdPath:            testInitrdPath,
		RootfsPath:            testRootfsPath,
		SocketBaseDir:         socketDir,
		BootTimeoutSeconds:    30,
		GuestAgentPort:        8080,
		Logger:                logger,
	})
}

func TestIntegration_VMBoot(t *testing.T) {
	skipIfNoAssets(t)
	if os.Getenv("VZ_INTEGRATION") == "" {
		t.Skip("set VZ_INTEGRATION=1 to run Vz integration tests")
	}

	pool := newTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Log("acquiring VM session...")
	sess, err := pool.Acquire(ctx, "test-integration-1", session.TierLite)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	t.Logf("VM acquired: ID=%s, Tier=%s", sess.ID, sess.Tier)
	defer pool.Destroy(sess.ID)

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
	if result2.Stdout != "aarch64\n" {
		t.Errorf("expected aarch64, got %q", result2.Stdout)
	}

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
		t.Skip("set VZ_INTEGRATION=1 to run Vz integration tests")
	}

	pool := newTestPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	sess, err := pool.Acquire(ctx, "test-multi-exec", session.TierLite)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	defer pool.Destroy(sess.ID)

	commands := []struct {
		code     string
		expected string
	}{
		{"echo 'test1'", "test1\n"},
		{"echo 'test2'", "test2\n"},
		{"date +%s", ""},   // just check it doesn't error
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
		t.Skip("set VZ_INTEGRATION=1 to run Vz integration tests")
	}

	pool := newTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	sess, err := pool.Acquire(ctx, "test-python", session.TierLite)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	defer pool.Destroy(sess.ID)

	// Python version check (first Python exec may be slow due to cold start)
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

	// Python computation
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

	// Node.js check
	result3, err := sess.Exec(ctx, session.ExecJob{
		Language:  "shell",
		Code:      "node --version",
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("node version check failed: %v", err)
	}
	t.Logf("Node.js version: %s", result3.Stdout)
}
