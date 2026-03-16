//go:build !desktop

package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"

	sharedtoolruntime "arkloop/services/shared/toolruntime"
	memorypkg "arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/memory/openviking"
	sandboxtool "arkloop/services/worker/internal/tools/builtin/sandbox"
	memorytool "arkloop/services/worker/internal/tools/memory"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SandboxExecutorFactory struct {
	mu   sync.Mutex
	pool *pgxpool.Pool
	byID map[string]*sandboxtool.ToolExecutor
}

func NewSandboxExecutorFactory(pool *pgxpool.Pool) *SandboxExecutorFactory {
	return &SandboxExecutorFactory{pool: pool, byID: map[string]*sandboxtool.ToolExecutor{}}
}

func (f *SandboxExecutorFactory) Resolve(snapshot sharedtoolruntime.RuntimeSnapshot) *sandboxtool.ToolExecutor {
	if f == nil || snapshot.SandboxBaseURL == "" {
		return nil
	}
	key := snapshot.SandboxBaseURL + "|" + snapshot.SandboxAuthToken
	f.mu.Lock()
	defer f.mu.Unlock()
	if executor := f.byID[key]; executor != nil {
		return executor
	}
	executor := sandboxtool.NewToolExecutorWithPool(snapshot.SandboxBaseURL, snapshot.SandboxAuthToken, f.pool)
	f.byID[key] = executor
	return executor
}

type MemoryProviderFactory struct {
	mu   sync.Mutex
	byID map[string]memorypkg.MemoryProvider
}

func NewMemoryProviderFactory() *MemoryProviderFactory {
	return &MemoryProviderFactory{byID: map[string]memorypkg.MemoryProvider{}}
}

func (f *MemoryProviderFactory) Resolve(snapshot sharedtoolruntime.RuntimeSnapshot) memorypkg.MemoryProvider {
	if f == nil || snapshot.MemoryBaseURL == "" || snapshot.MemoryRootAPIKey == "" {
		return nil
	}
	key := snapshot.MemoryBaseURL + "|" + hashString(snapshot.MemoryRootAPIKey)
	f.mu.Lock()
	defer f.mu.Unlock()
	if provider := f.byID[key]; provider != nil {
		return provider
	}
	provider := openviking.NewProvider(openviking.Config{
		BaseURL:    snapshot.MemoryBaseURL,
		RootAPIKey: snapshot.MemoryRootAPIKey,
	})
	f.byID[key] = provider
	return provider
}

type MemorySnapshotAppender interface {
	AppendMemoryLine(ctx context.Context, pool *pgxpool.Pool, accountID, userID uuid.UUID, agentID, line string) error
}

type MemoryExecutorFactory struct {
	mu        sync.Mutex
	pool      *pgxpool.Pool
	snapshots MemorySnapshotAppender
	byID      map[string]*memorytool.ToolExecutor
}

func NewMemoryExecutorFactory(pool *pgxpool.Pool, snapshots MemorySnapshotAppender) *MemoryExecutorFactory {
	return &MemoryExecutorFactory{pool: pool, snapshots: snapshots, byID: map[string]*memorytool.ToolExecutor{}}
}

func (f *MemoryExecutorFactory) Resolve(snapshot sharedtoolruntime.RuntimeSnapshot, provider memorypkg.MemoryProvider) *memorytool.ToolExecutor {
	if f == nil || provider == nil || snapshot.MemoryBaseURL == "" || snapshot.MemoryRootAPIKey == "" {
		return nil
	}
	key := snapshot.MemoryBaseURL + "|" + hashString(snapshot.MemoryRootAPIKey)
	f.mu.Lock()
	defer f.mu.Unlock()
	if executor := f.byID[key]; executor != nil {
		return executor
	}
	executor := memorytool.NewToolExecutor(provider, f.pool, f.snapshots)
	f.byID[key] = executor
	return executor
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
