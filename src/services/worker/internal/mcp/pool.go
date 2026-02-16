package mcp

import (
	"context"
	"sync"
)

type Pool struct {
	mu       sync.Mutex
	clients  map[string]*StdioClient
}

func NewPool() *Pool {
	return &Pool{clients: map[string]*StdioClient{}}
}

func (p *Pool) Borrow(ctx context.Context, server ServerConfig) (*StdioClient, error) {
	_ = ctx
	p.mu.Lock()
	client := p.clients[server.ServerID]
	if client == nil {
		client = NewStdioClient(server)
		p.clients[server.ServerID] = client
	}
	p.mu.Unlock()
	return client, nil
}

func (p *Pool) CloseAll() {
	p.mu.Lock()
	clients := p.clients
	p.clients = map[string]*StdioClient{}
	p.mu.Unlock()

	for _, client := range clients {
		_ = client.Close()
	}
}

