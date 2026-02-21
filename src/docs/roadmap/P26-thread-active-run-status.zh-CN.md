# P26 — Thread 运行状态指示器（侧边栏 Recents 转圈）

## 目标

当某个 thread 有正在执行的 run（status = 'running'）时，侧边栏 Recents 列表中对应条目显示转圈动画；run 结束（completed / failed / cancelled）后自动恢复正常状态。刷新页面或重新登录后状态依然准确。

## 问题背景

纯前端方案（在 `AppLayout` 维护 `runningThreadIds` Set）在初次加载时无法还原状态：

- `listThreads` 返回的 `ThreadResponse` 不含 run status
- `ChatPage` 只在用户打开某个 thread 时才调 `listThreadRuns`
- 如果同时存在多个正在运行的 thread，除当前打开的那个外，侧边栏对其余 thread 一无所知

**结论：必须由后端在 `listThreads` 响应中携带每条 thread 的活跃 run 信息。**

---

## 后端改动

### 涉及文件

| 文件 | 改动类型 |
|---|---|
| `src/services/api/internal/data/threads_repo.go` | 新增数据结构 + 修改查询 |
| `src/services/api/internal/http/v1_threads.go` | 修改响应结构 + 转换函数 |
| `src/services/api/internal/http/v1_threads_integration_test.go` | 新增测试用例 |

### 1. `threads_repo.go` — 新增 `ThreadWithActiveRun`，修改 `ListByOwner`

新增结构体：

```go
type ThreadWithActiveRun struct {
    Thread
    ActiveRunID *uuid.UUID // nil 表示当前无 running run
}
```

修改 `ListByOwner` 签名，返回 `[]ThreadWithActiveRun`。

查询改为 LEFT JOIN LATERAL `runs` 表，仅匹配 `status = 'running'`：

```sql
SELECT
    t.id, t.org_id, t.created_by_user_id, t.title, t.created_at,
    r.id AS active_run_id
FROM threads t
LEFT JOIN LATERAL (
    SELECT id FROM runs
    WHERE thread_id = t.id AND status = 'running'
    ORDER BY created_at DESC
    LIMIT 1
) r ON true
WHERE t.org_id = $1
  AND t.created_by_user_id = $2
  -- cursor conditions if present
ORDER BY t.created_at DESC, t.id DESC
LIMIT $n
```

LATERAL 子查询保证每条 thread 最多只关联一条 running run，不影响分页行为。

### 2. `v1_threads.go` — 响应结构加字段

```go
type threadResponse struct {
    ID              string  `json:"id"`
    OrgID           string  `json:"org_id"`
    CreatedByUserID *string `json:"created_by_user_id"`
    Title           *string `json:"title"`
    CreatedAt       string  `json:"created_at"`
    ActiveRunID     *string `json:"active_run_id"` // null 表示空闲
}
```

`toThreadResponse` 接受 `ThreadWithActiveRun`，将 `ActiveRunID` 映射为 `*string`（UUID.String()）。

`listThreads` handler 将 `[]ThreadWithActiveRun` 转为响应列表，其余逻辑不变。

---

## 前端改动

### 涉及文件

| 文件 | 改动类型 |
|---|---|
| `src/apps/web/src/api.ts` | `ThreadResponse` 加 `active_run_id` 字段 |
| `src/apps/web/src/layouts/AppLayout.tsx` | 维护 `runningThreadIds`，提供回调，从列表初始化 |
| `src/apps/web/src/components/Sidebar.tsx` | 接收 `runningThreadIds`，渲染转圈动画 |
| `src/apps/web/src/components/ChatPage.tsx` | run 开始/结束时通知 AppLayout |

### 1. `api.ts`

```ts
export type ThreadResponse = {
  id: string
  org_id: string
  created_by_user_id: string
  title: string | null
  created_at: string
  active_run_id: string | null  // 新增
}
```

### 2. `AppLayout.tsx` — 初始化 + 维护运行中集合

```ts
// 从 listThreads 响应初始化，刷新后可还原
const [runningThreadIds, setRunningThreadIds] = useState<Set<string>>(
  () => new Set(threadItems.filter(t => t.active_run_id != null).map(t => t.id))
)

const handleRunStarted = useCallback((threadId: string) => {
  setRunningThreadIds(prev => new Set(prev).add(threadId))
}, [])

const handleRunEnded = useCallback((threadId: string) => {
  setRunningThreadIds(prev => {
    const s = new Set(prev)
    s.delete(threadId)
    return s
  })
}, [])
```

Outlet context 补充三个字段：`onRunStarted`、`onRunEnded`、`runningThreadIds`。

### 3. `ChatPage.tsx` — 通知时机

| 时机 | 操作 |
|---|---|
| `createRun` 成功后 | `onRunStarted(threadId)` |
| SSE 收到 `run.completed` | `onRunEnded(threadId)` |
| SSE 收到 `run.cancelled` | `onRunEnded(threadId)` |
| SSE 收到 `run.failed` | `onRunEnded(threadId)` |
| mount 时检测到 `status = 'running'` 的已有 run | `onRunStarted(threadId)` |

### 4. `Sidebar.tsx` — 转圈动画

Props 新增 `runningThreadIds: Set<string>`，在 thread 列表项右侧条件渲染：

```tsx
{runningThreadIds.has(thread.id) && (
  <span className="ml-auto shrink-0 h-3 w-3 animate-spin rounded-full border border-[var(--c-text-muted)] border-t-transparent" />
)}
```

---

## 依赖

- P25（Web UI 已完成，Sidebar + AppLayout 结构已存在）
- `runs` 表已有 `status` 字段（现状已满足，无需 migration）

## 验收

**后端 integration test：**
- 有 running run 的 thread 返回 `active_run_id != null`
- 无 running run（或 run 已 completed）时返回 `active_run_id: null`
- 同一 thread 同时存在多条 run 时，只返回最新 running 的一条

**前端手工验收：**
- 发送消息后，侧边栏该 thread 立即出现转圈
- 输出结束（completed / cancelled / failed）后转圈消失
- 刷新页面后，正在运行的 thread 转圈状态正确还原
- 同时开启多个 thread 运行时，侧边栏多条同时显示转圈
