# Open Design → ArkLoop 集成执行计划

**Date:** 2026-05-19  
**Branch:** feat/part2aegntClient  
**Related:** [OD Design Spec](../../../open-design/docs/superpowers/specs/2026-05-19-port-to-arkloop-design.md) | [OD Prompt Assembly Flow](../../../open-design/docs/prompt-assembly-flow.md)

---

## 架构目标

```
迁移前 (OD 全栈):
  OD Web UI (聊天 + 预览) → OD Daemon (提示词 + Agent 管理 + 状态)
  → CLI Agent → LLM

迁移后:
  ArkLoop (智能层)              OD (资源层)
  ├── Persona 管理              ├── Daemon (项目/文件 CRUD)
  ├── Prompt Assembly           ├── MCP Server (tools + resources)
  ├── Agent Loop                └── Web UI (只读预览面板)
  ├── LLM 多模型路由
  ├── Memory/OpenViking
  └── Tool 执行 & MCP 客户端
```

核心原则：**ArkLoop 不直接访问 OD DB，所有上下文通过 MCP Resources 获取**。

---

## Phase 0: MCP Annotations 支持 ✅ 已完成

| 文件 | 改动 |
|------|------|
| `src/services/worker/internal/llm/contract.go` | `ToolAnnotations` 类型 + `ToolSpec.Annotations` + `ToJSON()` 序列化 |
| `src/services/worker/internal/mcp/types.go` | `Tool.Meta` + `Tool.Annotations` 字段 |
| `src/services/worker/internal/mcp/sdk_client.go` | `convertAnnotations()` 从 SDK Tool 提取 annotations |
| `src/services/worker/internal/tools/spec.go` | `AgentToolSpec.Annotations` 字段 |
| `src/services/worker/internal/mcp/registry.go` | 传递 annotations + 根据 `readOnlyHint` 修正 `RiskLevel`/`SideEffects` |
| `src/services/worker/internal/mcp/annotations_test.go` | 12 个单元测试 |

**验证:** `go build ./... && go test ./internal/mcp/... ./internal/llm/... ./internal/tools/...` ✅

---

## Phase 1: MCP ext-apps 基础支持（cherry-pick from development）

从 `development` 分支 cherry-pick MCP Apps（SEP-1865）支持代码：

### Worker 侧

| 文件 | 改动 | 说明 |
|------|------|------|
| `mcp/types.go` | +16 | `Resource`、`ResourceContent` 类型 |
| `mcp/sdk_client.go` | +73 | `ListResources()`、`ReadResource()`、ext-apps capability 协商 (`io.modelcontextprotocol/ui`) |
| `mcp/pool.go` | +26 | `ArtifactStore` 注入、Client interface 加 `ListResources`/`ReadResource` |
| `mcp/registry.go` | +20 | `isToolVisibleToModel()` visibility 过滤、`extractToolResourceURI()` 提取 `_meta.ui.resourceUri` |
| `mcp/executor.go` | +194 | MCP resource HTML fetch → artifact 上传、CSP 提取 (`_meta.ui.csp`)、`resourceContentAttachment()` |
| `llm/contract.go` | +63 | `PartTypeResource` 内容类型支持 |
| `messagecontent/content.go` | +35 | `PartTypeResource`、`AttachmentRef.URI` |
| `context_compact.go` | ~10 | 裁剪时 strip MCP resources |
| `mw_input_loader.go` | ~10 | Resources 输入注入 |

### Web 侧

| 文件 | 改动 | 说明 |
|------|------|------|
| `McpAppIframe.tsx` | **新文件** 290 行 | sandboxed iframe + postMessage 通信 + 主题同步 + resize |
| `ArtifactIframe.tsx` | +116 | MCP app 渲染模式 + CSP 配置支持 |

### 关键流程

```
MCP Server: tools/list → tool 含 _meta.ui.resourceUri: "ui://xxx"
  │
  ▼
Worker executor (CallTool 后):
  1. extractToolResourceURI(tool) 检测 resourceUri
  2. client.ReadResource(resourceUri) 获取 HTML
  3. 从 resource._meta.ui.csp 提取 CSP 配置
  4. store.PutObject(key, html) → 上传为 artifact
  5. ToolCallResult.ResultJSON.resources[] 返回 {key, uri, mime_type, csp}
  │
  ▼
前端 SSE stream:
  1. 接收 tool_result 含 resources[]
  2. ArtifactIframe / McpAppIframe 在聊天流中内嵌渲染
  3. sandboxed iframe + postMessage 双向通信
  4. 主题 CSS 变量同步注入
```

### Cherry-pick 注意事项

- 旧代码使用手写协议 (`stdio_client.go`/`http_client.go`)，当前已迁移到 `go-sdk v1.6.0` (`sdk_client.go`)。需手动适配冲突。
- `context_compact.go`、`mw_input_loader.go`、`agent/loop.go` 的改动可单独 cherry-pick。
- 建议按文件粒度 cherry-pick：先 Worker 侧，后端通过后再 Web 侧。

---

## Phase 2: Project Context Mode

基于 `collaboration_mode` 模式，新增 Thread 级 OD 项目上下文机制。

### 2.1 Thread 表扩展

```sql
ALTER TABLE threads ADD COLUMN project_meta_context JSONB DEFAULT NULL;
```

- `NULL`：普通聊天模式
- `{"project_id": "proj_xxx"}`：Project 模式激活，自动注入项目上下文

### 2.2 Mode 自动激活

在 `handler_agent_loop.go` 中检测 `od_create_project` 成功：

```
tool_result 含 projectId + toolName == "od_create_project"
  → UPDATE threads
    SET project_meta_context = '{"project_id":"proj_xxx"}'
  → 下次 Run 自动激活 OD 上下文注入
```

对标现有 `applyThreadCollaborationModeEvent`。

### 2.3 上下文注入中间件

新文件 `mw_project_context.go`：

```go
func ApplyProjectContext(rc *RunContext) {
    ctx := rc.ProjectMetaContext
    if ctx == nil || ctx.ProjectID == "" {
        return // 非 Project 模式
    }

    // 1. 调用 OD MCP Resources 获取上下文
    metadataRules := readODResource("od://projects/" + ctx.ProjectID + "/metadata-rules")
    designSystem   := readODResource("od://projects/" + ctx.ProjectID + "/design-system")
    skillContent   := readODResource("od://projects/" + ctx.ProjectID + "/skill")

    // 2. 注入为 system_prefix PromptAssembly Segments
    rc.UpsertPromptSegment("od.role",         system_prefix, OD_BASE_ROLE_PROMPT)
    rc.UpsertPromptSegment("od.discovery",    system_prefix, OD_DISCOVERY_PROMPT)
    rc.UpsertPromptSegment("od.design_system",system_prefix, designSystem)
    rc.UpsertPromptSegment("od.skill",        system_prefix, skillContent)
    rc.UpsertPromptSegment("od.metadata",     system_prefix, metadataRules)
}
```

### 2.4 OD MCP Resources（在 OD 侧实现）

| Resource URI | 内容 | MIME |
|---|---|---|
| `od://projects/<id>/metadata-rules` | `renderMetadataBlock()` 输出 | `text/markdown` |
| `od://projects/<id>/design-system` | DESIGN.md + tokens.css | `text/markdown` |
| `od://projects/<id>/skill` | SKILL.md 内容 | `text/markdown` |

### 2.5 改动清单

| 文件 | 改动 | 预估 |
|------|------|------|
| migration *.sql | `threads.project_meta_context JSONB` | 0.25h |
| `threads_repo.go` | 字段定义 + Normalize | 0.5h |
| `v1_runs.go` | run.started 携带 `project_meta_context` | 0.5h |
| `context.go` | `RunContext.ProjectMetaContext` 字段 | 0.25h |
| `mw_input_loader.go` | 加载 `project_meta_context` 到 RunContext | 0.5h |
| `mw_project_context.go` | **新文件**：MCP Resource 加载 → PromptSegment 注入 | 1.5h |
| `handler_agent_loop.go` | 检测 `od_create_project` → 写 `project_meta_context` | 1h |

---

## Phase 3: /preview 内嵌 & Direction Cards（MCP Apps 机制）

基于 Phase 1 的 MCP Apps 管道，OD 侧提供 HTML resource，ArkLoop 通过已有机制渲染。

### 3.1 Direction Cards（5 方向选择器）

```
od_select_direction:
  _meta.ui.resourceUri: "ui://directions/picker.html"

执行流程:
  LLM 调 od_select_direction
  → Worker executor fetch resource HTML
  → ArtifactIframe 在聊天流中内嵌渲染 5 张方向卡片
  → 用户点选 → postMessage → tool result 返回选择
```

### 3.2 /preview 预览

```
od_create_project:
  _meta.ui.resourceUri: "ui://projects/{projectId}/preview.html"

执行流程:
  LLM 调 od_create_project
  → Worker fetch preview HTML (内嵌 iframe 指向 OD daemon /preview/:id)
  → ArtifactIframe 在聊天流中内嵌渲染
  → OD daemon 提供实时预览更新
```

### 3.3 改动范围

| 侧 | 改动 | 说明 |
|----|------|------|
| OD MCP Server | tool 声明 `_meta.ui.resourceUri` + 提供 HTML resource | 新 |
| ArkLoop Worker | Phase 1 已覆盖（executor resource fetch + artifact upload） | 已完成 |
| ArkLoop Web | Phase 1 已覆盖（McpAppIframe + ArtifactIframe 内嵌渲染） | 已完成 |

---

## Phase 4: 集成验证

### 4.1 配置 ArkLoop 连接 OD

```sql
INSERT INTO mcp_configs (account_id, server_id, transport, command, args, env)
VALUES ('<account_id>', 'open-design', 'stdio', 'node',
  '["apps/daemon/src/mcp.ts"]',
  '{"OD_DAEMON_URL": "http://localhost:17573"}');
```

### 4.2 验证 checklist

- [ ] ArkLoop 能正确发现 OD 的 7 个 MCP tools
- [ ] MCP annotations 正确传递（只读 tools 无不必要确认）
- [ ] LLM 调 `od_create_project` 后 Thread 自动进入 Design Context Mode
- [ ] 后续 Run 自动加载项目 DS/Skill/Metadata 上下文
- [ ] Direction Cards HTML 在聊天流中内嵌渲染，选方向正常
- [ ] `/preview` 在聊天流中内嵌渲染，实时更新正常

### 4.3 错误场景

| 场景 | 预期行为 |
|------|----------|
| Daemon 未运行 | MCP tool 返回错误提示 "cannot reach OD daemon" |
| 项目不存在 | `readResource` 返回 `isError: true` |
| project_meta_context 为 NULL | 自动恢复普通聊天模式 |

---

## Phase 5: 可选增强（后续）

| 功能 | 描述 | 优先级 |
|------|------|--------|
| UI-only Tools 注册路径 | ArkLoop Web UI 直接调 MCP tools | P1 |
| Critique Theater | 多轮评审指令注入 | P2 |
| Direction Cards 装修 | 卡片 UI 带色板预览、字体栈 | P2（MCP Apps 方案已解决） |
| artifact 轮询同步 | OD 文件变更自动刷新 iframe | P2 |

---

## 改动文件总览

| Phase | 文件数 | 新增行 | 说明 |
|-------|--------|--------|------|
| Phase 0 | 6 | +217 | MCP Annotations 支持 ✅ |
| Phase 1 | 11 | +800 | MCP ext-apps 基础支持 |
| Phase 2 | 6 | +200 | Project Context Mode |
| Phase 3 | 0 (OD 侧) | - | 仅 OD MCP Server 改造 |
| **总计** | **23** | **~1217** | |

## 依赖关系

```
Phase 0 ✓ ──→ Phase 1 ──→ Phase 2 ──→ Phase 3 ──→ Phase 4
                              │
                              └──→ OD MCP Resources (OD 侧并行)
```
