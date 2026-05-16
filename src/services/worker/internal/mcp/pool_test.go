package mcp

import (
	"context"
	"os"
	"testing"
	"time"

	sharedmcpinstall "arkloop/services/shared/mcpinstall"
)

func TestPoolBorrowRebuildsOnStdioDisconnect(t *testing.T) {
	configPath := writeTestMcpConfig(t, map[string]any{"callTimeoutMs": 1000})
	t.Setenv(mcpConfigFileEnv, configPath)

	server := sharedmcpinstall.ServerConfig{
		ServerID:      "demo",
		Transport:     "stdio",
		Command:       os.Args[0],
		Args:          []string{"-test.run", "^TestMcpServerProcess$"},
		Env:           map[string]string{testMcpServerEnv: "1"},
		CallTimeoutMs: 1000,
	}

	pool := NewPool()
	t.Cleanup(pool.CloseAll)

	ctx := context.Background()

	client1, err := pool.Borrow(ctx, server)
	if err != nil {
		t.Fatalf("first borrow failed: %v", err)
	}
	if _, err := client1.ListTools(ctx, 1000); err != nil {
		t.Fatalf("ListTools on client1 failed: %v", err)
	}

	if !client1.IsHealthy(ctx) {
		t.Fatal("expected client1 to be healthy")
	}

	_ = client1.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !client1.IsHealthy(ctx) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if client1.IsHealthy(ctx) {
		t.Fatal("expected client1 to be unhealthy after close")
	}

	client2, err := pool.Borrow(ctx, server)
	if err != nil {
		t.Fatalf("second borrow failed: %v", err)
	}
	if client2 == client1 {
		t.Fatal("expected a new client, got the same instance")
	}

	tools, err := client2.ListTools(ctx, 1000)
	if err != nil {
		t.Fatalf("ListTools on client2 failed: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("expected tools from new client")
	}
}

func TestSdkClientIsHealthy(t *testing.T) {
	server := sharedmcpinstall.ServerConfig{
		ServerID:      "demo",
		Transport:     "stdio",
		Command:       os.Args[0],
		Args:          []string{"-test.run", "^TestMcpServerProcess$"},
		Env:           map[string]string{testMcpServerEnv: "1"},
		CallTimeoutMs: 1000,
	}

	ctx := context.Background()

	t.Run("fresh client is healthy", func(t *testing.T) {
		c, err := newSDKClient(ctx, server, nil)
		if err != nil {
			t.Fatalf("newSDKClient failed: %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })
		if !c.IsHealthy(ctx) {
			t.Fatal("fresh client should be healthy")
		}
	})

	t.Run("closed client is unhealthy", func(t *testing.T) {
		c, err := newSDKClient(ctx, server, nil)
		if err != nil {
			t.Fatalf("newSDKClient failed: %v", err)
		}
		_ = c.Close()
		if c.IsHealthy(ctx) {
			t.Fatal("closed client should be unhealthy")
		}
	})
}
