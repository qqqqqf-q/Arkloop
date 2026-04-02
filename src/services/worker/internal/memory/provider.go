package memory

import (
	"context"

	"github.com/google/uuid"
)

// MemoryIdentity 标识一次 memory 操作的调用方身份，用于多租户隔离。
type MemoryIdentity struct {
	AccountID      uuid.UUID
	UserID         uuid.UUID
	AgentID        string // 默认取 PersonaDefinition.ID，字符集 [a-zA-Z0-9_-]
	ExternalUserID string
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
	Abstract    string // L0 摘要
	Score       float64
	MatchReason string
	IsLeaf      bool
}

// MemoryLayer 对应分层内容读取深度。
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

// MemoryEntry 是一条主动写入的结构化记忆。
// Write 通过 session/commit 路径写入，后端根据 identity headers 决定存储空间，
// 因此不支持指定目标 URI。
type MemoryEntry struct {
	Content string // 记忆正文（纯文本）
}

// MemoryCategory 预定义的记忆分类。
type MemoryCategory string

const (
	MemoryCategoryProfile    MemoryCategory = "profile"     // 用户基础信息
	MemoryCategoryPreference MemoryCategory = "preferences" // 偏好设置
	MemoryCategoryEntity     MemoryCategory = "entities"    // 关键实体（人/项目/技术栈）
	MemoryCategoryEvent      MemoryCategory = "events"      // 事件记录
	MemoryCategoryCase       MemoryCategory = "cases"       // 执行案例（Sandbox/Browser 结论）
	MemoryCategoryPattern    MemoryCategory = "patterns"    // 行为模式
)

// MemoryProvider 是 Worker 侧的 Memory 抽象。
//
// 三条主链路：
//   - 记忆注入：Find + Content（run 之前，注入 system prompt）
//   - 记忆提取：AppendSessionMessages + CommitSession（run 之后，异步归档）
//   - 主动写入：Write + Delete（Agent tool call 直接操作记忆）
//
// 后端不可用时实现应降级为"无记忆"，不影响 run 主流程。
type MemoryProvider interface {
	// Find 语义检索，返回 L0 摘要 + URI；必要时再按 URI 拉 L1/L2。
	Find(ctx context.Context, ident MemoryIdentity, targetURI string, query string, limit int) ([]MemoryHit, error)

	// Content 读取分层内容（L0/L1/L2）。
	Content(ctx context.Context, ident MemoryIdentity, uri string, layer MemoryLayer) (string, error)

	// AppendSessionMessages 向 session 追加消息（sessionID = thread_id）。
	AppendSessionMessages(ctx context.Context, ident MemoryIdentity, sessionID string, msgs []MemoryMessage) error

	// CommitSession 触发会话归档与长期记忆提取。应在 goroutine 中异步调用，不阻塞 run 返回。
	CommitSession(ctx context.Context, ident MemoryIdentity, sessionID string) error

	// Write 主动写入一条结构化记忆，内容会被建立向量索引，之后可通过 Find 检索。
	// URI 由调用方通过 BuildURI 构造，适配器负责将 entry 路由到正确的 scope。
	Write(ctx context.Context, ident MemoryIdentity, scope MemoryScope, entry MemoryEntry) error

	// Delete 删除指定 URI 的记忆，同时从向量索引中移除。
	Delete(ctx context.Context, ident MemoryIdentity, uri string) error
}

// DesktopLocalMemoryWriteURI is implemented by the Desktop SQLite provider so memory_write can return the canonical URI.
type DesktopLocalMemoryWriteURI interface {
	WriteReturningURI(ctx context.Context, ident MemoryIdentity, scope MemoryScope, entry MemoryEntry) (uri string, err error)
}

// DesktopLocalMemoryEditURI is implemented by the Desktop SQLite provider so
// notebook_edit can update an existing local note by URI.
type DesktopLocalMemoryEditURI interface {
	UpdateByURI(ctx context.Context, ident MemoryIdentity, uri string, entry MemoryEntry) error
}
