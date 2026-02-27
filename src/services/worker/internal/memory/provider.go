package memory

import (
	"context"

	"github.com/google/uuid"
)

// MemoryIdentity 标识一次 memory 操作的调用方身份，用于多租户隔离。
type MemoryIdentity struct {
	OrgID   uuid.UUID // -> OpenViking account_id
	UserID  uuid.UUID // -> OpenViking user_id
	AgentID string    // -> OpenViking agent_id（默认取 SkillDefinition.ID，adapter 层 sanitize 到 [a-zA-Z0-9_-]）
}

// MemoryScope 控制检索/写入的命名空间。
type MemoryScope string

const (
	MemoryScopeUser  MemoryScope = "user"  // 用户偏好、实体、事件
	MemoryScopeAgent MemoryScope = "agent" // Agent 特有的 cases/patterns
)

// MemoryHit 是一次语义检索的单条结果。
type MemoryHit struct {
	URI         string
	Abstract    string  // L0 摘要
	Score       float64
	MatchReason string
	IsLeaf      bool
}

// MemoryLayer 对应 OpenViking 的分层内容读取深度。
type MemoryLayer string

const (
	MemoryLayerAbstract MemoryLayer = "abstract" // L0：一句话摘要
	MemoryLayerOverview MemoryLayer = "overview" // L1：章节级概要
	MemoryLayerRead     MemoryLayer = "read"     // L2：完整原文
)

// MemoryMessage 表示一条会话消息，用于写入和 commit。
type MemoryMessage struct {
	Role    string // "user" | "assistant"
	Content string
}

// MemoryProvider 是 Worker 侧的 Memory 抽象，屏蔽底层实现（当前为 OpenViking）。
//
// 两条主链路：
//   - 记忆注入：Find + Content（run 之前，注入 system prompt）
//   - 记忆提取：AppendSessionMessages + CommitSession（run 之后，异步归档）
//
// OpenViking 不可用时实现应降级为"无记忆"，不影响 run 主流程。
type MemoryProvider interface {
	// Find 语义检索，返回 L0 摘要 + URI；必要时再按 URI 拉 L1/L2。
	Find(ctx context.Context, ident MemoryIdentity, scope MemoryScope, query string, limit int) ([]MemoryHit, error)

	// Content 读取分层内容（L0/L1/L2）。
	Content(ctx context.Context, ident MemoryIdentity, uri string, layer MemoryLayer) (string, error)

	// AppendSessionMessages 向 OpenViking session 追加消息（sessionID = thread_id）。
	AppendSessionMessages(ctx context.Context, ident MemoryIdentity, sessionID string, msgs []MemoryMessage) error

	// CommitSession 触发会话归档与长期记忆提取。应在 goroutine 中异步调用，不阻塞 run 返回。
	CommitSession(ctx context.Context, ident MemoryIdentity, sessionID string) error
}
