package mcp

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestPoolBorrowRebuildsOnStdioDisconnect 模拟 stdio 子进程退出后，
// 下次 Borrow 时 Pool 自动关闭旧 client 并返回新 client。
func TestPoolBorrowRebuildsOnStdioDisconnect(t *testing.T) {
	configPath := writeTestMcpConfig(t, map[string]any{"callTimeoutMs": 1000})
	t.Setenv(mcpConfigFileEnv, configPath)

	server := ServerConfig{
		ServerID:         "demo",
		Transport:        "stdio",
		Command:          os.Args[0],
		Args:             []string{"-test.run", "^TestMcpServerProcess$"},
		InheritParentEnv: true,
		Env:              map[string]string{testMcpServerEnv: "1"},
		CallTimeoutMs:    1000,
	}

	pool := NewPool()
	t.Cleanup(pool.CloseAll)

	ctx := context.Background()

	// 第一次 Borrow，得到 client1 并触发进程启动
	client1, err := pool.Borrow(ctx, server)
	if err != nil {
		t.Fatalf("first borrow failed: %v", err)
	}
	if _, err := client1.ListTools(ctx, 1000); err != nil {
		t.Fatalf("ListTools on client1 failed: %v", err)
	}

	// 确认 client1 是 StdioClient 且进程已启动
	sc, ok := client1.(*StdioClient)
	if !ok {
		t.Fatal("expected *StdioClient")
	}
	sc.mu.Lock()
	cmd := sc.cmd
	sc.mu.Unlock()
	if cmd == nil {
		t.Fatal("expected subprocess to be running")
	}

	// Kill 子进程，触发 readLoop → handleDisconnect → disconnected=true
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill subprocess: %v", err)
	}

	// 等待 readLoop 检测到 EOF 并调用 handleDisconnect
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !client1.IsHealthy(ctx) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if client1.IsHealthy(ctx) {
		t.Fatal("expected client1 to be unhealthy after process kill")
	}

	// 再次 Borrow，应返回新 client
	client2, err := pool.Borrow(ctx, server)
	if err != nil {
		t.Fatalf("second borrow failed: %v", err)
	}
	if client2 == client1 {
		t.Fatal("expected a new client, got the same instance")
	}

	// 新 client 可以正常工作
	tools, err := client2.ListTools(ctx, 1000)
	if err != nil {
		t.Fatalf("ListTools on client2 failed: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("expected tools from new client")
	}
}

// TestStdioClientIsHealthy 验证 IsHealthy 在各状态下的返回值。
func TestStdioClientIsHealthy(t *testing.T) {
	server := ServerConfig{
		ServerID:         "demo",
		Transport:        "stdio",
		Command:          os.Args[0],
		Args:             []string{"-test.run", "^TestMcpServerProcess$"},
		InheritParentEnv: true,
		Env:              map[string]string{testMcpServerEnv: "1"},
		CallTimeoutMs:    1000,
	}

	ctx := context.Background()

	t.Run("fresh client is healthy", func(t *testing.T) {
		c := NewStdioClient(server)
		if !c.IsHealthy(ctx) {
			t.Fatal("fresh client should be healthy")
		}
	})

	t.Run("closed client is unhealthy", func(t *testing.T) {
		c := NewStdioClient(server)
		_ = c.Close()
		if c.IsHealthy(ctx) {
			t.Fatal("closed client should be unhealthy")
		}
	})

	t.Run("disconnected client is unhealthy", func(t *testing.T) {
		c := NewStdioClient(server)
		// 启动进程
		if _, err := c.ListTools(ctx, 1000); err != nil {
			t.Fatalf("ListTools failed: %v", err)
		}
		if !c.IsHealthy(ctx) {
			t.Fatal("running client should be healthy")
		}

		// Kill 进程，等待 handleDisconnect
		c.mu.Lock()
		cmd := c.cmd
		c.mu.Unlock()
		if cmd != nil {
			_ = cmd.Process.Kill()
		}

		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if !c.IsHealthy(ctx) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if c.IsHealthy(ctx) {
			t.Fatal("killed client should be unhealthy")
		}
		_ = c.Close()
	})
}
