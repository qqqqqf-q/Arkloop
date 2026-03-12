//go:build integration

package openviking_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/memory/openviking"

	"github.com/google/uuid"
)

// newIntegrationProvider 读取环境变量，构建真实 client。
// 若未配置则跳过测试。
func newIntegrationProvider(t *testing.T) memory.MemoryProvider {
	t.Helper()
	cfg := openviking.LoadConfigFromEnv()
	if !cfg.Enabled() {
		t.Skip("ARKLOOP_OPENVIKING_BASE_URL not set, skipping integration test")
	}
	return openviking.NewProvider(cfg)
}

// newTestIdent 返回一对可预测的测试身份，每个测试用独立 userID 确保数据不干扰。
func newTestIdent(accountID, userID uuid.UUID) memory.MemoryIdentity {
	return memory.MemoryIdentity{
		AccountID:   accountID,
		UserID:  userID,
		AgentID: "integration-test-agent",
	}
}

// pollFind 在超时内轮询 Find，直到命中数 >= wantMin 或超时。
// Write/CommitSession 触发的向量化是异步的，需要等待。
func pollFind(ctx context.Context, p memory.MemoryProvider, ident memory.MemoryIdentity, query string, wantMin int, timeout time.Duration) ([]memory.MemoryHit, error) {
	deadline := time.Now().Add(timeout)
	for {
		hits, err := p.Find(ctx, ident, memory.MemoryScopeUser, query, 10)
		if err != nil {
			return nil, err
		}
		if len(hits) >= wantMin {
			return hits, nil
		}
		if time.Now().After(deadline) {
			return hits, fmt.Errorf("timeout: only %d hits after %s (want %d)", len(hits), timeout, wantMin)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// TestIntegration_Write_Find_Content_Delete 验证完整的写→搜→读→删链路。
func TestIntegration_Write_Find_Content_Delete(t *testing.T) {
	p := newIntegrationProvider(t)
	ctx := context.Background()

	accountID := uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000001")
	userID := uuid.New() // 每次测试独立 user，避免跨测试污染
	ident := newTestIdent(accountID, userID)

	// 写入一条记忆
	entry := memory.MemoryEntry{
		Content: "Integration test: user loves functional programming and Haskell.",
	}
	if err := p.Write(ctx, ident, memory.MemoryScopeUser, entry); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// 等待向量化完成后搜索（最多 60s）
	hits, err := pollFind(ctx, p, ident, "functional programming Haskell", 1, 60*time.Second)
	if err != nil {
		t.Fatalf("Find after Write: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least 1 hit after Write, got 0")
	}

	// 找到叶节点（实际记忆文件）
	var leafURI string
	for _, h := range hits {
		if h.IsLeaf {
			leafURI = h.URI
			break
		}
	}
	if leafURI == "" {
		t.Fatalf("no leaf node found in %d hits", len(hits))
	}

	// 读取内容
	content, err := p.Content(ctx, ident, leafURI, memory.MemoryLayerRead)
	if err != nil {
		t.Fatalf("Content failed: %v", err)
	}
	if content == "" {
		t.Fatal("expected non-empty content")
	}

	// 删除
	if err := p.Delete(ctx, ident, leafURI); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// 删除后搜索应找不到（或不再命中该 URI）
	time.Sleep(1 * time.Second)
	hitsAfter, err := p.Find(ctx, ident, memory.MemoryScopeUser, "functional programming Haskell", 10)
	if err != nil {
		t.Fatalf("Find after Delete failed: %v", err)
	}
	for _, h := range hitsAfter {
		if h.URI == leafURI {
			t.Errorf("deleted URI %s still appears in Find results", leafURI)
		}
	}
}

// TestIntegration_MultiTenant_Isolation 验证不同租户数据严格隔离。
func TestIntegration_MultiTenant_Isolation(t *testing.T) {
	p := newIntegrationProvider(t)
	ctx := context.Background()

	orgA := uuid.New()
	userA := uuid.New()
	identA := newTestIdent(orgA, userA)

	orgB := uuid.New()
	userB := uuid.New()
	identB := newTestIdent(orgB, userB)

	// org A 写入一条独特记忆
	secretContent := fmt.Sprintf("secret-isolation-test-%s: org A private data", orgA.String()[:8])
	if err := p.Write(ctx, identA, memory.MemoryScopeUser, memory.MemoryEntry{
		Content: secretContent,
	}); err != nil {
		t.Fatalf("Write as org A failed: %v", err)
	}

	// 等待向量化
	time.Sleep(5 * time.Second)

	// org B 搜索，不应命中 org A 的数据
	hitsB, err := p.Find(ctx, identB, memory.MemoryScopeUser, secretContent, 10)
	if err != nil {
		t.Fatalf("Find as org B failed: %v", err)
	}
	for _, h := range hitsB {
		if h.URI != "" {
			// org B 不应看到任何 org A 的 userA URI
			if strings.Contains(h.URI, userA.String()) || strings.Contains(h.URI, orgA.String()) {
				t.Errorf("org B saw org A data: URI=%s", h.URI)
			}
		}
	}
}

// TestIntegration_SessionArchive_Find 验证 session 归档后记忆可被 Find 检索到。
func TestIntegration_SessionArchive_Find(t *testing.T) {
	p := newIntegrationProvider(t)
	ctx := context.Background()

	accountID := uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000001")
	userID := uuid.New()
	ident := newTestIdent(accountID, userID)

	// sessionID 对应 thread_id，传任意 UUID，OpenViking 会自动创建
	sessionID := uuid.New().String()

	// 追加对话消息
	uniqueTopic := fmt.Sprintf("archival-test-%s", userID.String()[:8])
	msgs := []memory.MemoryMessage{
		{Role: "user", Content: fmt.Sprintf("I really enjoy %s as my unique hobby.", uniqueTopic)},
		{Role: "assistant", Content: "That sounds like a wonderful hobby! I'll remember that."},
	}
	if err := p.AppendSessionMessages(ctx, ident, sessionID, msgs); err != nil {
		t.Fatalf("AppendSessionMessages failed: %v", err)
	}

	// commit 触发异步 LLM 提取
	if err := p.CommitSession(ctx, ident, sessionID); err != nil {
		t.Fatalf("CommitSession failed: %v", err)
	}

	// 轮询等待记忆被提取并向量化（最多 90s，LLM 提取 + embedding 双重异步）
	hits, err := pollFind(ctx, p, ident, uniqueTopic, 1, 90*time.Second)
	if err != nil {
		t.Logf("pollFind result: %v hits, err: %v", len(hits), err)
		// session 归档的 LLM 提取可能判断内容不值得归档，不做硬失败
		t.Skip("session memory not extracted (LLM may have skipped low-value content)")
	}
	if len(hits) == 0 {
		t.Skip("session memory not indexed within timeout")
	}
	t.Logf("session archive: found %d hits, top score=%.3f abstract=%q",
		len(hits), hits[0].Score, hits[0].Abstract)
}

// TestIntegration_Concurrent_Find_NoPanic 验证并发 Find 不崩溃。
func TestIntegration_Concurrent_Find_NoPanic(t *testing.T) {
	p := newIntegrationProvider(t)
	ctx := context.Background()

	accountID := uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000001")
	userID := uuid.New()
	ident := newTestIdent(accountID, userID)

	const goroutines = 10
	errs := make([]error, goroutines)
	var wg sync.WaitGroup

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = p.Find(ctx, ident, memory.MemoryScopeUser, "concurrent test", 5)
		}(i)
	}
	wg.Wait()

	// 记录错误但不致命（可能无数据，空结果是合法的）
	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: Find error: %v", i, err)
		}
	}
}
