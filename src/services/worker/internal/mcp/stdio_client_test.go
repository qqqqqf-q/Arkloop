package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/tools"
)

const testMcpServerEnv = "ARKLOOP_TEST_MCP_SERVER"

func TestMcpServerProcess(t *testing.T) {
	if os.Getenv(testMcpServerEnv) != "1" {
		t.Skip("only used for subprocess MCP server")
	}
	runTestMcpServer()
}

func runTestMcpServer() {
	reader := bufio.NewScanner(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	defer func() { _ = writer.Flush() }()

	for reader.Scan() {
		line := reader.Bytes()
		if len(line) == 0 {
			continue
		}

		var payload any
		if err := json.Unmarshal(line, &payload); err != nil {
			continue
		}
		obj, ok := payload.(map[string]any)
		if !ok {
			continue
		}

		method, _ := obj["method"].(string)
		params, _ := obj["params"].(map[string]any)

		rawID, hasID := obj["id"]
		if !hasID {
			continue
		}

		switch method {
		case "initialize":
			writeResult(writer, rawID, map[string]any{
				"protocolVersion": defaultProtocolVersion,
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "test_mcp", "version": "0"},
			})
		case "tools/list":
			writeResult(writer, rawID, map[string]any{
				"tools": []any{
					map[string]any{
						"name":        "echo",
						"description": "echo back",
						"inputSchema": map[string]any{"type": "object"},
					},
					map[string]any{
						"name":        "slow",
						"description": "sleep a bit",
						"inputSchema": map[string]any{"type": "object"},
					},
					map[string]any{
						"name":        "rpc_error",
						"description": "always fail",
						"inputSchema": map[string]any{"type": "object"},
					},
				},
			})
		case "tools/call":
			toolName, _ := params["name"].(string)
			args, _ := params["arguments"].(map[string]any)

			switch toolName {
			case "echo":
				text, _ := args["text"].(string)
				writeResult(writer, rawID, map[string]any{
					"content": []any{
						map[string]any{"type": "text", "text": text},
					},
					"isError": false,
				})
			case "slow":
				time.Sleep(200 * time.Millisecond)
				writeResult(writer, rawID, map[string]any{
					"content": []any{
						map[string]any{"type": "text", "text": "done"},
					},
					"isError": false,
				})
			case "rpc_error":
				writeError(writer, rawID, map[string]any{
					"code":    -32000,
					"message": "rpc failed",
					"data":    map[string]any{"tool": toolName},
				})
			default:
				writeError(writer, rawID, map[string]any{
					"code":    -32601,
					"message": "unknown tool",
				})
			}
		default:
			writeError(writer, rawID, map[string]any{
				"code":    -32601,
				"message": "unknown method",
			})
		}
	}
}

func writeResult(w *bufio.Writer, id any, result map[string]any) {
	resp := map[string]any{
		"jsonrpc": rpcVersion,
		"id":      id,
		"result":  result,
	}
	raw, _ := json.Marshal(resp)
	_, _ = w.Write(append(raw, '\n'))
	_ = w.Flush()
}

func writeError(w *bufio.Writer, id any, errObj map[string]any) {
	resp := map[string]any{
		"jsonrpc": rpcVersion,
		"id":      id,
		"error":   errObj,
	}
	raw, _ := json.Marshal(resp)
	_, _ = w.Write(append(raw, '\n'))
	_ = w.Flush()
}

func TestDiscoverFromEnvRegistersToolsAndExecutes(t *testing.T) {
	configPath := writeTestMcpConfig(t, map[string]any{"callTimeoutMs": 1000})
	t.Setenv(mcpConfigFileEnv, configPath)

	pool := NewPool()
	t.Cleanup(pool.CloseAll)

	reg, err := DiscoverFromEnv(context.Background(), pool)
	if err != nil {
		t.Fatalf("DiscoverFromEnv failed: %v", err)
	}

	toolName := "mcp__demo__echo"
	executor := reg.Executors[toolName]
	if executor == nil {
		t.Fatalf("expected executor bound: %s", toolName)
	}

	result := executor.Execute(
		context.Background(),
		toolName,
		map[string]any{"text": "hi"},
		tools.ExecutionContext{Emitter: events.NewEmitter("trace")},
		"",
	)
	if result.Error != nil {
		t.Fatalf("unexpected execution error: %+v", result.Error)
	}
	content, ok := result.ResultJSON["content"].([]map[string]any)
	if !ok || len(content) != 1 {
		t.Fatalf("unexpected content: %#v", result.ResultJSON["content"])
	}
	if content[0]["text"] != "hi" {
		t.Fatalf("unexpected echo result: %#v", content[0])
	}
}

func TestToolExecutorTimeout(t *testing.T) {
	configPath := writeTestMcpConfig(t, map[string]any{"callTimeoutMs": 200})
	t.Setenv(mcpConfigFileEnv, configPath)

	pool := NewPool()
	t.Cleanup(pool.CloseAll)

	reg, err := DiscoverFromEnv(context.Background(), pool)
	if err != nil {
		t.Fatalf("DiscoverFromEnv failed: %v", err)
	}

	toolName := "mcp__demo__slow"
	executor := reg.Executors[toolName]
	if executor == nil {
		t.Fatalf("expected executor bound: %s", toolName)
	}

	timeout := 20
	result := executor.Execute(
		context.Background(),
		toolName,
		map[string]any{},
		tools.ExecutionContext{
			TimeoutMs: &timeout,
			Emitter:   events.NewEmitter("trace"),
		},
		"",
	)
	if result.Error == nil || result.Error.ErrorClass != ErrorClassMcpTimeout {
		t.Fatalf("expected timeout error, got: %+v", result.Error)
	}
}

func TestToolExecutorRpcError(t *testing.T) {
	configPath := writeTestMcpConfig(t, map[string]any{"callTimeoutMs": 1000})
	t.Setenv(mcpConfigFileEnv, configPath)

	pool := NewPool()
	t.Cleanup(pool.CloseAll)

	reg, err := DiscoverFromEnv(context.Background(), pool)
	if err != nil {
		t.Fatalf("DiscoverFromEnv failed: %v", err)
	}

	toolName := "mcp__demo__rpc_error"
	executor := reg.Executors[toolName]
	if executor == nil {
		t.Fatalf("expected executor bound: %s", toolName)
	}

	result := executor.Execute(
		context.Background(),
		toolName,
		map[string]any{},
		tools.ExecutionContext{Emitter: events.NewEmitter("trace")},
		"",
	)
	if result.Error == nil || result.Error.ErrorClass != ErrorClassMcpRpcError {
		t.Fatalf("expected rpc error, got: %+v", result.Error)
	}
}

func writeTestMcpConfig(t *testing.T, overrides map[string]any) string {
	t.Helper()

	payload := map[string]any{
		"mcpServers": map[string]any{
			"demo": map[string]any{
				"transport":     "stdio",
				"command":       os.Args[0],
				"args":          []any{"-test.run", "^TestMcpServerProcess$"},
				"env":           map[string]any{testMcpServerEnv: "1"},
				"callTimeoutMs": 200,
			},
		},
	}
	for key, value := range overrides {
		payload["mcpServers"].(map[string]any)["demo"].(map[string]any)[key] = value
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal config failed: %v", err)
	}

	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, encoded, 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	return path
}
