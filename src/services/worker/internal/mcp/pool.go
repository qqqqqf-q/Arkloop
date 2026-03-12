package mcp

import (
	"context"
	"fmt"
	"sync"
)

// Client 是对单个 MCP Server 连接的抽象，stdio 和 HTTP 传输都实现此接口。
type Client interface {
	ListTools(ctx context.Context, timeoutMs int) ([]Tool, error)
	CallTool(ctx context.Context, name string, arguments map[string]any, timeoutMs int) (ToolCallResult, error)
	IsHealthy(ctx context.Context) bool
	Close() error
}

// Pool 持有按 (accountID, serverID) 键控的 MCP Client，用于跨 run 的连接复用。
// 全局（env 加载）的工具使用空 accountID（key 形如 ":serverID"）。
type Pool struct {
	mu      sync.Mutex
	clients map[string]Client
}

func NewPool() *Pool {
	return &Pool{clients: map[string]Client{}}
}

// Borrow 返回现有 client 或根据 transport 类型新建一个。
// 若现有 client 不健康（子进程已退出、被显式关闭等），关闭并重建。
func (p *Pool) Borrow(ctx context.Context, server ServerConfig) (Client, error) {
	key := poolKey(server.AccountID, server.ServerID)

	p.mu.Lock()
	defer p.mu.Unlock()

	if client := p.clients[key]; client != nil {
		if client.IsHealthy(ctx) {
			return client, nil
		}
		_ = client.Close()
		delete(p.clients, key)
	}

	var client Client
	switch server.Transport {
	case "stdio", "":
		client = NewStdioClient(server)
	case "http_sse", "streamable_http":
		var err error
		client, err = NewHTTPClient(server)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("mcp: unsupported transport: %s", server.Transport)
	}

	p.clients[key] = client
	return client, nil
}

func (p *Pool) CloseAll() {
	p.mu.Lock()
	clients := p.clients
	p.clients = map[string]Client{}
	p.mu.Unlock()

	for _, client := range clients {
		_ = client.Close()
	}
}

func poolKey(accountID, serverID string) string {
	return accountID + ":" + serverID
}
