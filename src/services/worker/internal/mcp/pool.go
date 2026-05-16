package mcp

import (
	"context"
	"sync"
)

type Client interface {
	ListTools(ctx context.Context, timeoutMs int) ([]Tool, error)
	CallTool(ctx context.Context, name string, arguments map[string]any, timeoutMs int) (ToolCallResult, error)
	IsHealthy(ctx context.Context) bool
	Close() error
}

type Pool struct {
	mu        sync.Mutex
	clients   map[string]Client
	authStore AuthStore
}

type BorrowMeta struct {
	Reused bool
}

type PoolOption func(*Pool)

func WithAuthStore(store AuthStore) PoolOption {
	return func(p *Pool) {
		p.authStore = store
	}
}

func NewPool(options ...PoolOption) *Pool {
	p := &Pool{clients: map[string]Client{}}
	for _, option := range options {
		if option != nil {
			option(p)
		}
	}
	return p
}

func (p *Pool) Borrow(ctx context.Context, server ServerConfig) (Client, error) {
	client, _, err := p.BorrowWithMeta(ctx, server)
	return client, err
}

func (p *Pool) BorrowWithMeta(ctx context.Context, server ServerConfig) (Client, BorrowMeta, error) {
	key := poolKey(server.AccountID, server.ServerID)

	// Fast path: check for a healthy cached client under lock.
	p.mu.Lock()
	cached := p.clients[key]
	p.mu.Unlock()

	if cached != nil && cached.IsHealthy(ctx) {
		return cached, BorrowMeta{Reused: true}, nil
	}

	// Slow path: build a new client outside the lock.
	if cached != nil {
		_ = cached.Close()
	}

	newClient, err := newSDKClient(ctx, server, p.authStore)
	if err != nil {
		return nil, BorrowMeta{}, err
	}

	// Install the new client. If another goroutine installed one first, close ours.
	p.mu.Lock()
	existing := p.clients[key]
	if existing != nil && existing.IsHealthy(ctx) {
		p.mu.Unlock()
		_ = newClient.Close()
		return existing, BorrowMeta{Reused: true}, nil
	}
	if existing != nil {
		_ = existing.Close()
	}
	p.clients[key] = newClient
	p.mu.Unlock()

	return newClient, BorrowMeta{}, nil
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
