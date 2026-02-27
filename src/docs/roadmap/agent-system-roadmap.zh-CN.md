# Agent System 路线图

本文是 Arkloop Agent System 的薄片式执行路线。基于 Phase 6.5（Console Management）与 Phase 7（性能可扩展性）的并行进展，Agent System 作为独立方向单独展开规划。

关联文档：
- 架构重构路线：`src/docs/roadmap/architecture-refactor-roadmap.zh-CN.md`
- 开发路线：`src/docs/roadmap/development-roadmap.zh-CN.md`

---

## 0. 当前代码基线

Agent 系统相关的已有能力（截止本文写作时）：

**执行核心（`src/services/worker/`）：**
- `agent/loop.go`：完整 AgentLoop，支持多轮迭代、tool_call 调度、cancel signal、max_iterations。
- `pipeline/`：中间件链架构（CancelGuard → InputLoader → Entitlement → MCPDiscovery → AgentConfig → SkillResolution → Routing → ToolBuild → AgentLoopHandler），各阶段职责独立。
- `skills/`：Skill 注册表，支持文件系统（`src/skills/`）和 DB（`skills` 表）双源加载；`Definition` 包含 id、version、prompt、tool_allowlist、budgets。
- `mcp/pool.go`：MCP 连接池，按 `orgID:serverID` 键控，支持 stdio 和 HTTP 传输。
- `routing/`：Provider 路由，支持显式 `route_id`、`when` 条件匹配、default fallback，路由配置支持环境变量和 DB 双源。
- `pipeline/mw_agent_config.go`：AgentConfig 继承链（thread → project → org），解析 system_prompt、model、tool_policy 等。

**当前架构图：**
```
API → Worker
       ├── Pipeline 中间件链
       │     └── terminal: AgentLoopHandler（硬编码 agent.NewLoop）
       ├── Skills（YAML + DB）
       ├── MCP Pool（连接复用）
       └── ProviderRouter（model 路由）
```

**尚未存在的能力：**
- Executor 策略抽象（不同 Agent 类型的不同执行行为）
- Skill 级别的 model/route 绑定（`preferred_credential`）
- Memory System（OpenViking 接入）
- Sandbox（Firecracker microVM 隔离执行）
- Human-in-the-loop（mid-loop 用户输入注入）
- Sub-agent spawning
- Browser Service（Headless Browser，Playwright 无头浏览器服务 + Worker 侧 BrowserToolExecutor）

---

## 1. 总纲：核心问题、困惑点与难点

在规划执行之前，先明确我们在对话中识别出的所有问题，分三类梳理。

### 1.1 架构问题（需要决策的结构性问题）

**A1 — Executor 同质化：所有 Agent 都跑同一个 Loop**

当前所有 Skill 走同一条执行路径（`agent.NewLoop`），只能通过 system_prompt、tool_allowlist、max_iterations 做差异化。但 Lite/Pro/Ultra 的差距远不止参数：
- Lite：线性、轻量、不允许 mid-loop 用户输入
- Ultra：多阶段规划、周期性方向校验、支持暂停等待人工确认
- Thread Summary：先分类再执行，根本不需要 tool loop

如果把这些逻辑写进同一个 Loop，结果是 if-else 爆炸，不可维护。

**A2 — Skill 定义能力有限，无法绑定执行策略**

`skills.Definition` 只描述"这个 Agent 知道什么"（prompt、tools），没有字段描述"这个 Agent 怎么执行"。新增 Agent 类型 = 修改 Loop 代码。应当让 Skill 声明自己的 `executor_type`，Loop 只是众多 Executor 之一。

**A3 — Model 选择与 Skill/Tier 脱节**

Lite/Pro/Ultra 对应不同的 LLM（haiku/sonnet/opus），但 Skill 定义里没有 `preferred_credential`，`AgentConfig.Model` 虽然存在但从未被路由层读取。Model 的选择完全依赖外部传入 `route_id`，Skill 本身无法声明偏好。

**A4 — Sandbox 层级不清**

Sandbox（代码执行隔离）和 Worker 之间的关系没有明确边界。Sandbox 崩溃、逃逸是否会影响 Worker？是否应该是独立服务？与 Browser Service（无头浏览器）的关系如何隔离？

**A5 — MCP 每次 run 都触发 DB 查询**

`MCPDiscoveryMiddleware` 每次 run 都调用 `DiscoverFromDB`，Pool 虽然复用连接，但 DB 查询未缓存。stdio 进程挂了也没有健康检查和重连机制。高并发下这是个隐患。

### 1.2 功能缺失（产品层面的 Gap）

**F1 — 无 Memory System**

Agent 没有跨 session 记忆能力，每次对话都从零开始。计划接入 OpenViking（字节跳动火山引擎 Viking 团队开源，`volcengine/OpenViking`）——专为 Agent 设计的上下文数据库，用 `viking://` 虚拟文件系统范式统一管理记忆/资源/技能，支持 L0/L1/L2 分层按需加载和目录递归检索，优于传统扁平向量存储。OpenViking 部署为独立 Python HTTP 服务，Go Worker 通过 `MemoryProvider` 接口调用，不能硬编码到具体实现。

**F2 — 无 Human-in-the-loop**

长任务无法在中途校正方向。Ultra 需要在每 N 轮迭代后发出 `run.input_requested`，前端呈现确认框，用户提交后继续执行。当前 Loop 只有 `CancelSignal`，没有 pause/inject 语义。

**F3 — 无 Sandbox（代码执行）**

`code_execute`、`shell` 等工具无法在 Worker 进程内执行（安全边界要求隔离）。需要独立 Sandbox 服务 + `SandboxToolExecutor` 实现 `ToolExecutor` 协议。

**F4 — 无 Sub-agent Spawning**

Ultra 模式需要能调度子 Agent（如"搜索子任务"并行执行）。当前 Loop 是单线程串行的，没有父子关系追踪和并行调度能力。

**F5 — 无 Cost Budget 执行侧限制**

`RunContext.ToolBudget` 字段已预留，但 Loop 里没有任何 token budget 消耗追踪和超限终止逻辑。大任务可能无限跑下去。

**F6 — Thread Summary 等内置任务无路由能力**

Thread Summary（生成会话标题）根据内容类型需要走不同逻辑（技术向/情感向），但当前没有 classify-then-execute 的执行模式。

**F7 — Thinking 内容与主输出混合，前端无法区分渲染**

当前存在两类 thinking 内容会直接显示在对话气泡里：
- **LLM 原生 thinking**：使用支持推理的模型（DeepSeek R1 以 `<think>` 标签嵌入、Anthropic extended thinking 以 `type: "thinking"` block 返回）时，当前 Gateway 适配器对 thinking 块的处理是：Anthropic 适配器（非流式）在 `parseAnthropicMessage` 里遇到 `type != "text" && type != "tool_use"` 时静默跳过；OpenAI chatCompletions（流式）对 `<think>` 标签无特殊处理直接作为 `content_delta` 透传；前端 `ChatPage.tsx:200-204` 处理 `message.delta` 时也未检查 `channel` 字段，导致 thinking token 直接拼入 `assistantDraft`，出现在对话气泡里。
- **Agent 执行过程的中间步骤**：Multi-round 迭代的每轮 LLM 请求、Ultra 的方向校验、工具调用链等，这些"执行过程"缺乏前端可解析的结构，要么全显示、要么全隐藏，无法做折叠控件。

`StreamMessageDelta.Channel` 字段已在 Go contract 中定义但从未被填充或被前端读取。`run.segment.start/end` 事件对不存在。

### 1.3 工程难点（实现时的技术挑战）

**E1 — Sandbox：Firecracker + 轻量调度器**

SaaS 后端环境有分布式存储基础设施，宿主机支持 KVM，直接上 Firecracker microVM，不走 gVisor 过渡路径。技术要点：
- Firecracker 每个 session 对应一个 microVM，崩溃不影响 Worker
- 快照（mem.snap + disk.snap）存 MinIO（`sandbox-snapshots/{template_id}/`），复用已有 S3 基础设施
- 需要自己的轻量调度器（warm pool + best-of-k 节点选择），不引入 Kubernetes

**E2 — Human-in-the-loop 的通信机制**

Loop 如何"暂停并等待"？需要扩展现有的 Postgres LISTEN/NOTIFY 机制（已有 cancel 信号路径），新增 `run.input_provided` 事件类型。前端需要新的 UI 状态（awaiting_input）和提交接口。

**E3 — OpenViking 的接入方式**

OpenViking 是 Python 项目（Embedded/HTTP/CLI 三种模式），无 Go SDK。对 Arkloop 来说唯一合理的接入是：将其作为独立 HTTP 服务运行（Docker/compose），Worker 通过 HTTP 调用；并通过 `MemoryProvider` 接口隔离实现细节，避免硬编码到具体 Provider。

调研后确认的关键点：
- 官方容器镜像：`ghcr.io/volcengine/openviking:main`，默认端口 `1933`，健康检查 `/health`、就绪探针 `/ready`。
- HTTP API：统一前缀 `/api/v1/`（检索 `/search/find|search`、分层内容 `/content/abstract|overview|read`、会话 `/sessions/*`）。
- 多租户：`ov.conf.server.root_api_key` 启用认证；使用 ROOT key 时可通过 header 覆盖身份（`X-OpenViking-Account`/`X-OpenViking-User`/`X-OpenViking-Agent`），便于后端做 org/user/agent 隔离且不必先落地 user key 生命周期管理。
- URI 是“所见即所得”映射（不会自动注入 user/agent space）：Worker 必须构造带 space 的 URI（例如 `viking://user/{user_id}/memories/`），否则会写进共享路径导致串租。

**E4 — Executor 注册表的类型安全**

`executor_config` 是 `map[string]any`，各 Executor 自己解析。需要在 Worker 启动时做配置校验，而不是等到运行时才报错。

**E5 — Browser Service 接入**

Agent 需要无头浏览器能力（网页浏览、交互、内容提取、截图），但当前没有对应的服务和 Worker 侧 `ToolExecutor`。需要独立的 Browser Service（Node.js + Playwright）提供 HTTP API，Worker 通过 `BrowserToolExecutor` 调用。关键技术挑战：多租户 session 隔离（cookie/localStorage 按 thread 持久化）、BrowserContext 池化与内存管理、SSRF 防护（拦截内网地址访问）。

**E6 — Thinking 渲染的两层问题**

第一层是 Gateway 合规性问题：`StreamMessageDelta.Channel` 已定义但从未填充——Anthropic 非流式路径丢弃 thinking blocks，OpenAI chatCompletions 流式路径不处理 `<think>` 标签，导致 thinking 内容要么消失要么混入主输出。第二层是前端渲染问题：`message.delta` channel 字段从未被读取，`run.segment.start/end` 事件不存在，前端对 Lite 的浏览器截图内嵌展示和 Ultra 的默认隐藏无法区分——这两种 display 策略需要后端主动声明。

**E7 — Tool Provider 注册碎片化**

`web_search` 的 SearXNG/Tavily 后端、`web_fetch` 的 Jina/Firecrawl/Basic 后端，当前全部通过单一 env var 全局切换，无法做到：per-org 激活不同后端、Console 里可视化管理和配置（API Key、Base URL）、Skill 声明偏好某个特定后端。根本原因是 `AgentToolSpec.Name` 同时承担了"内部注册键"和"LLM 暴露名"两个职责，导致无法区分同一 LLM 名下的多个实现。

---

## 2. Phase 总览

| Phase | 编号 | 主题 | 执行模式 | 前置依赖 |
|---|---|---|---|---|
| AS-1 | Executor 策略层 | 引入 AgentExecutor 接口 + 注册表；AS-1 完成后，Lite/Pro/Ultra 新增 = 写 YAML + 可选新建 executor 文件 | 顺序 | 无 |
| AS-2 | Skill 路由绑定 | Skill 声明 `preferred_credential`，接入 `AgentConfig.Model` → 路由层（均按凭证名称匹配） | 顺序 | AS-1 |
| AS-3 | Human-in-the-loop | RunContext 加 WaitForInput 钩子；Ultra executor 可用，Lite 零成本 | 顺序 | AS-1 |
| AS-3.5 | Orchestrator 执行层 | Skill 绑定 AgentConfig；Lua executor；父子 Run 跨实例调度 | 顺序 | AS-1, AS-3 |
| AS-4 | MCP 健康与缓存 | DiscoverFromDB 缓存 + Pool 健康检查/重连 | 并行（可与 AS-1 同步） | 无 |
| AS-5 | Memory System | MemoryProvider 接口 + OpenViking 适配器 + Pipeline 接入（已完成）+ Memory Tool + 记忆提炼 + 测试 | 顺序 | AS-1 |
| AS-6 | Sandbox 服务 | Firecracker 快照 + MinIO 持久化 + Warm Pool + 空闲回收 + SandboxToolExecutor + Console 监控 | 独立（可并行启动） | 无 |
| AS-7 | Browser Service | Node.js + Playwright 无头浏览器服务 + thread 级 session 持久化 + BrowserToolExecutor 接入 Worker | 独立（可并行） | 无 |
| AS-8 | Cost Budget 执行侧 | Loop 内 token 消耗追踪 + 超限终止 | 顺序 | AS-1 |
| AS-9 | Sub-agent Spawning | `spawn_agent` tool + 父子 run 关系追踪 | 顺序 | AS-1, AS-3 |
| AS-10 | Thinking 展示协议 | LLM 原生 thinking channel 分离 + Agent 段落事件 + 前端折叠渲染 | 两个独立子轨道 | AS-10.1/2 独立；AS-10.3/4 依赖 AS-1 |
| AS-11 | Tool Provider 管理 | AgentToolSpec.LlmName 字段 + 每工具多后端注册 + per-org 激活配置 + Console 管理页 | 顺序，四层 | 无（完全独立） |

---

## 3. AS-1 — Executor 策略层

**目标**：引入 `AgentExecutor` 接口和注册表，将硬编码的 `agent.NewLoop` 调用替换为 dispatch 机制。Skill 通过 `executor_type` 字段声明自己的执行策略。

**AS-1 完成后的效果**：新增任何 Agent 行为（Lite、Pro、Ultra、新的内置任务）只需：
1. 在 `src/skills/agent-xxx/` 写 `skill.yaml`（声明 `executor_type`、`preferred_credential`、`tool_allowlist`）和 `prompt.md`
2. 如果执行逻辑有差异，新建一个 `executor/xxx.go` 文件并注册
3. 如果只是 prompt/model/tool 不同，`"agent.simple"` 作为 executor_type 即可复用

这是架构保证的"低摩擦扩展路径"，Pro/Ultra 的具体行为是 Research 问题，不是架构问题。

**解决的问题**：A1、A2、F6

### AS-1.1 — AgentExecutor 接口 + 注册表

- **新建** `src/services/worker/internal/executor/interface.go`：

```go
type AgentExecutor interface {
    Execute(
        ctx context.Context,
        rc *pipeline.RunContext,
        emitter events.Emitter,
        yield func(events.RunEvent) error,
    ) error
}

type Factory func(config map[string]any) (AgentExecutor, error)
```

- **新建** `src/services/worker/internal/executor/registry.go`：`Registry`，`Register(type, factory)`，`Build(type, config)`。
- Worker 启动时（composition root）注册内置 Executor。

**验收**：`go test ./internal/executor/...` 注册 / 构建 / 未知类型返回错误。

### AS-1.2 — SimpleExecutor（Lite / Pro 默认路径）

- **新建** `src/services/worker/internal/executor/simple.go`：
  - 封装现有 `agent.NewLoop` + `handler_agent_loop.go` 里的 eventWriter 逻辑。
  - 与当前行为完全一致，是"从现有代码提取"而非"新写功能"。
- 注册类型：`"agent.simple"`，无 config 参数。

**验收**：现有全部 `go test ./...` 通过，无行为变化。

### AS-1.3 — ClassifyRouteExecutor（Thread Summary / Auto 路由）

- **新建** `src/services/worker/internal/executor/classify_route.go`：
  1. 用 `rc.Gateway` 做一次轻量分类 LLM call（不需要 tools）。
  2. 按分类结果在 `ExecutorConfig.Routes` 里选 prompt override。
  3. 用选定 prompt 做一次 single-shot LLM call，写事件，结束。
- 注册类型：`"task.classify_route"`。
- `executor_config` schema：`routes: map[string]RouteConfig`，每个 `RouteConfig` 含 `prompt_override`、可选 `model_override`。

**验收**：单测 mock Gateway，验证 classify → route 选择 → prompt 覆盖逻辑正确。

### AS-1.4 — Skill 定义加 executor_type + executor_config

- **修改** `src/services/worker/internal/skills/registry.go`：`Definition` 增加 `ExecutorType string`、`ExecutorConfig map[string]any`。
- **修改** `src/services/worker/internal/skills/loader.go`：解析 YAML 中的 `executor_type`（可选，默认 `"agent.simple"`）和 `executor_config`（可选，默认 `{}`）。
- **验收**：无 `executor_type` 的现有 skill.yaml 加载正常（backward compatible）。

### AS-1.5 — 替换 handler_agent_loop.go 为 dispatch

- **修改** `src/services/worker/internal/pipeline/handler_agent_loop.go`：
  - 从 `rc.SkillDefinition` 取 `ExecutorType`（默认 `"agent.simple"`）和 `ExecutorConfig`。
  - 从 `executorRegistry.Build(type, config)` 获取 executor。
  - 调用 `executor.Execute(ctx, rc, emitter, yield)` 替换原来的 `loop.Run` 调用。
- `executorRegistry` 通过 `EngineV1Deps` 注入。

**验收**：全量 `go test ./...` 通过，端到端 run 执行行为与重构前完全一致。

**执行顺序**：AS-1.1 → AS-1.2 → AS-1.4 → AS-1.5（主线），AS-1.3 可并行写但在 AS-1.5 合入后集成。

---

## 4. AS-2 — Skill 路由绑定

**目标**：让 Skill 能声明偏好的 LLM route，让 `AgentConfig.Model` 真正参与路由决策，建立完整的 model 选择优先级链。

**解决的问题**：A3

### AS-2.1 — Skill 加 preferred_credential

- **修改** `src/services/worker/internal/skills/registry.go`：`Definition` 增加 `PreferredCredential *string`（凭证显示名称）。
- **修改** `src/services/worker/internal/skills/loader.go`：解析 YAML 字段 `preferred_credential`（可选）；DB 查询列名为 `preferred_credential`。
- **修改** `src/services/worker/internal/pipeline/context.go`：`RunContext` 增加 `PreferredCredentialName string`。
- **修改** `src/services/worker/internal/pipeline/mw_skill_resolution.go`：解析完 Skill 后，若 `def.PreferredCredential != nil`，则设置：

```go
rc.PreferredCredentialName = *def.PreferredCredential
```

**验收**：单测：skill 有 `preferred_credential` 时，`rc.PreferredCredentialName` 正确设置；skill 没有时为空；用户显式传 `route_id` 不受影响。

### AS-2.2 — AgentConfig.Model 接入路由层（已完成）

- `AgentConfig.Model` 和 `Skill.preferred_credential` 均存储凭证显示名称（`llm_credentials.name`），语义统一。
- **路由决策**在 `mw_routing.go` 中通过 `ProviderRoutingConfig.GetHighestPriorityRouteByCredentialName(name)` 实现（不是 `FindRouteByModel`，不匹配 `route.Model`）。

**优先级链（最终）：**
```
InputJSON["route_id"]          ← 用户显式 route ID → Decide() 直接处理
   ↓ 无
Skill.preferred_credential     ← rc.PreferredCredentialName → 凭证名称查找
   ↓ 无匹配
AgentConfig.Model              → 凭证名称查找（与上述共用同一路径）
   ↓ 无匹配 / 无 DB 路由
Decide() fallback              ← 静态路由 / 默认路由
```

**Skill YAML 示例（完成 AS-2 后）：**
```yaml
id: agent-ultra
version: "1.0"
title: "Ultra"
executor_type: agent.interactive
preferred_credential: "my-anthropic"
budgets:
  max_iterations: 25
```

**执行顺序**：AS-2.1 → AS-2.2，可在 AS-1.5 合入后并行推进。

---

## 5. AS-3 — Human-in-the-loop

**目标**：Ultra Executor 支持在指定迭代边界暂停，等待用户从前端注入消息后继续执行。

**解决的问题**：F2

### AS-3.1 — RunContext 加 WaitForInput 钩子

- **修改** `src/services/worker/internal/pipeline/context.go`：

```go
// WaitForInput 非 nil 时，Executor 在 CheckInAt 边界调用此函数。
// 返回 ("", false) 表示超时或不注入；返回 (text, true) 则将 text 作为 user message 注入。
WaitForInput func(ctx context.Context) (string, bool)
CheckInAt    func(iter int) bool
```

- 默认值为 `nil`（Lite/Pro 不触发，零开销）。

### AS-3.2 — run.input_requested / run.input_provided 事件类型

- **修改** `src/services/worker/internal/pipeline/handler_agent_loop.go`（或共享常量文件）：新增事件类型常量 `run.input_requested`、`run.input_provided`。
- **修改** API 端：
  - 新增端点 `POST /v1/runs/{run_id}/input`，接收 `{"content": "..."}` body。
  - 写入 `run_events`（type=`run.input_provided`）并触发 `pg_notify`。
- **修改** `src/services/worker/internal/pipeline/mw_cancel_guard.go`（或新建 mw）：`LISTEN run_events:{runID}`，收到 `run.input_provided` 时通过 channel 传递 text。

### AS-3.3 — InteractiveExecutor

- **新建** `src/services/worker/internal/executor/interactive.go`：
  - 内嵌 `SimpleExecutor` 的循环逻辑（复用，不重复写）。
  - 在每轮迭代结束后：若 `CheckInAt(iter)` 返回 true，emit `run.input_requested` 事件，调用 `WaitForInput(ctx)` 阻塞。
  - 收到输入后将其作为 user message 注入 messages 切片，继续下一轮。
- 注册类型：`"agent.interactive"`。
- `executor_config` schema：`check_in_every int`（默认 5），`max_wait_seconds int`（默认 300）。

### AS-3.4 — Pipeline 装配 WaitForInput

- **修改** `src/services/worker/internal/pipeline/handler_agent_loop.go`（或新建 mw）：在 `executor.Execute` 调用前，若 executor 类型是 `"agent.interactive"`，构造并注入 `WaitForInput` 函数到 `rc`。
- `WaitForInput` 实现通过已注册的 Postgres LISTEN channel 拿数据，超时返回 `("", false)`。

**执行顺序**：AS-3.1 → AS-3.2（API 端和 Worker 端可并行）→ AS-3.3 → AS-3.4

**依赖**：AS-1.2（InteractiveExecutor 内嵌 Simple 逻辑）

---

## 5.5. AS-3.5 — Orchestrator 执行层

**目标**：为 Skill 提供真正的多 Agent 编排能力。三件事必须同时落地才能形成闭环：Skill 显式绑定 AgentConfig（解决模型选择问题）、Lua executor（解决编排逻辑描述问题）、父子 Run 跨实例异步调度（解决执行隔离和性能问题）。

**解决的问题**：Ultra 无法在内部使用小模型做子任务；Agent 编排逻辑写死在 Go 代码里导致扩展成本高；Sub-agent 阻塞父 run 连接。

**依赖**：AS-1（ExecutorRegistry）、AS-3（WaitForInput 机制，父子 run 等待子任务完成时复用该挂起模型）

---

### AS-3.5.1 — Skill 绑定 AgentConfig

**目标**：Skill 可以通过 `agent_config` 字段（AgentConfig 名称）显式指定使用哪个 AgentConfig，覆盖继承链解析结果。这是子任务使用不同模型的关键前提。

**当前问题**：Skill 不知道自己的上层 AgentConfig，AgentConfig 由 thread → project → org 继承链自动解析。Ultra skill 内部的 Lua 子任务和主任务会用同一个 AgentConfig，无法让子任务使用更便宜的模型。

**DB schema**：无变更，AgentConfig 已有 `name` 字段，查询按 name + org_id 匹配。

**Skill YAML 新增字段**：

```yaml
id: ultra
version: "1"
agent_config: "Gemini Flash"    # 按名称引用 AgentConfig；nil 则走继承链
budgets:
  max_iterations: 20
  max_output_tokens: 8192
  temperature: 0.7
```

**代码变更**：

`src/services/worker/internal/skills/registry.go`：`Definition` 加 `AgentConfigName *string` 字段。

`src/services/worker/internal/skills/loader.go`：YAML 解析 `agent_config` 字段；`LoadFromDB` scan `agent_config_name` 列（需 migration 加列）。

`src/services/api/internal/migrate/migrations/00061_skills_add_agent_config_name.sql`：

```sql
ALTER TABLE skills ADD COLUMN agent_config_name TEXT;
```

`src/services/worker/internal/pipeline/mw_skill_resolution.go`：解析出 `AgentConfigName` 后，若非 nil，执行按名称重新查 AgentConfig 的逻辑，覆盖 `rc.AgentConfig`。

```go
if def.AgentConfigName != nil {
    ac, name, err := loadAgentConfigByName(ctx, dbPool, *def.AgentConfigName, rc.Run.OrgID)
    if err == nil && ac != nil {
        rc.AgentConfig = ac
        rc.AgentConfigName = name
    }
}
```

`mw_agent_config.go`：提取 `loadAgentConfigByName(ctx, pool, name, orgID)` 辅助函数，供上面调用。

**执行顺序**：migration → registry.go → loader.go → mw_skill_resolution.go → mw_agent_config.go 提取辅助函数

---

### AS-3.5.2 — 父子 Run 跨实例异步调度

**目标**：父 Run 调度子 Run 时，父 Run 挂起不占 DB 连接，子 Run 可被任意 Worker 实例执行，完成后通过 Redis Pub/Sub 唤醒父 Run。

**当前 AS-9.2 的问题**：`spawn_agent` tool 用 LISTEN/NOTIFY 同步等待，父 Run 挂起期间占着 pgx 直连，大量并发子任务会耗尽直连池。

**架构设计**：

```
父 Run 执行到 agent.run_child() 调用
  → 在 runs 表创建子 Run（parent_run_id = 父 Run ID，status = pending）
  → 父 Run 订阅 Redis channel: "run.child.{child_run_id}.done"
  → 父 Run goroutine 挂起在 Redis Subscribe（释放 DB 连接）
  → 子 Run 被任意 Worker 实例的调度器捡起执行
  → 子 Run 完成后向 Redis 发布 "run.child.{child_run_id}.done"，payload = 子 Run 的最终输出
  → 父 Run 被唤醒，拿到 payload，继续执行
  → 子 Run 结果同时写入 run_events 表（持久化，Console 可查）
```

**资源消耗模型**：父 Run 挂起时只占 1 goroutine + 1 Redis subscription，不占 DB 连接。Redis subscription 在 go-redis 里是共享连接的（multiplexed），1000 个挂起的父 Run 不会开 1000 条 Redis 连接。

**DB schema**：

```sql
-- migration 00062
ALTER TABLE runs ADD COLUMN parent_run_id UUID REFERENCES runs(id);
CREATE INDEX idx_runs_parent_run_id ON runs(parent_run_id) WHERE parent_run_id IS NOT NULL;
```

**新增文件**：`src/services/worker/internal/runengine/child_run.go`

```go
// SpawnChildRun 创建子 Run 并异步等待其完成。
// 父 Run 挂起期间不持有 DB 连接。
// ctx 取消时立即返回 error，子 Run 继续执行直到超时。
func SpawnChildRun(ctx context.Context, rc *pipeline.RunContext, skillID string, input string) (string, error)
```

内部：创建子 Run → 发布到 worker 调度队列（复用现有 `runqueue`）→ Redis Subscribe 等待完成事件 → 返回输出文本。

**Lua binding**：

```lua
-- Lua 脚本中调用
local result, err = agent.run(skill_id, input_text)
-- 底层映射到 SpawnChildRun
```

**Console 展示**：API `GET /v1/runs?parent_run_id={id}` 返回子 Run 列表；Run 详情页展示父子树形结构，子 Run 默认折叠。

**执行顺序**：migration(00062) → child_run.go → RunContext 暴露 SpawnChildRun → Lua binding（等 AS-3.5.3 完成后接入）

---

### AS-3.5.3 — Lua Executor

**目标**：引入 `agent.lua` executor type，Skill 的 `executor_config.script` 字段包含 Lua 脚本，脚本通过 Go binding 访问 Agent 能力，实现可编程编排逻辑。

**选型确认**：使用 [gopher-lua](https://github.com/yuin/gopher-lua)（纯 Go Lua 5.1 解释器）。Lua 是嵌入式脚本，底层完全是 Go 执行，无 CGO，无额外进程，性能开销主要来自 binding call，而非 Lua 本身。Lua 脚本只负责描述"做什么、以什么顺序"，所有重活（LLM 调用、工具执行、DB 写入）在 Go 层。

**Skill YAML 示例**：

```yaml
id: research_agent
executor_type: agent.lua
executor_config:
  script: |
    local prompt = context.get("user_prompt")

    -- 第一步：用小模型分解任务
    local plan, err = agent.run("lite_planner", "把以下任务分解成3个子问题：\n" .. prompt)
    if err then return end

    -- 第二步：并行执行子问题（顺序版本，并行在 AS-3.5.4 扩展）
    local results = {}
    for i, sub in ipairs(parse_list(plan)) do
      local r, e = agent.run("pro", sub)
      if not e then results[i] = r end
    end

    -- 第三步：用大模型综合
    local synthesis = table.concat(results, "\n\n")
    local final, err = agent.run("ultra_synthesis", "综合以下研究结果：\n" .. synthesis)
    context.set_output(final)
```

**Go binding 接口设计**：

```go
// LuaRuntime 暴露给 Lua 脚本的 Go API
type LuaRuntime struct {
    rc    *pipeline.RunContext
    yield func(events.RunEvent) error
}

// 注册到 Lua state 的函数
// agent.run(skill_id, input) -> (output, err)
// agent.classify(prompt, labels) -> (label, err)   轻量分类，不创建子 Run
// tools.call(name, args_json) -> (result_json, err)
// context.get(key) -> value
// context.set_output(text)                          写入最终输出
// memory.search(query) -> results_json              依赖 AS-5
```

**执行隔离**：每个 Run 创建独立的 `*lua.LState`，不共享，无需加锁。`LState` 在 Execute() 返回后 GC。

**超时控制**：通过 `luar` 或手动在每个 binding call 里检查 `ctx.Done()`，Lua 脚本无法无限循环。每次 binding call 返回前检查 ctx，若取消则注入 Lua error。

**新增文件**：`src/services/worker/internal/executor/lua.go`

```go
func NewLuaExecutor(config map[string]any) (pipeline.AgentExecutor, error)

// LuaExecutor 实现 AgentExecutor 接口
type LuaExecutor struct {
    script string
}
```

注册到 `DefaultExecutorRegistry()`：

```go
_ = reg.Register("agent.lua", NewLuaExecutor)
```

**依赖**：AS-3.5.2（`agent.run` binding 内部调用 `SpawnChildRun`）

**执行顺序**：lua.go（executor skeleton）→ Go binding 设计 → 接入 SpawnChildRun → 沙箱超时机制 → 测试

---

### AS-3.5.4 — 并行子任务原语（可选，后续扩展）

**目标**：Lua 脚本可以并行启动多个子 Run，等待所有完成后聚合结果。

```lua
local tasks = {
  { skill = "lite", input = chunk_1 },
  { skill = "lite", input = chunk_2 },
  { skill = "lite", input = chunk_3 },
}
local results, errors = agent.run_parallel(tasks)
```

**实现**：Go 层开 N 个 goroutine 并行调用 `SpawnChildRun`，用 `sync.WaitGroup` 聚合结果，返回结果数组给 Lua。子任务间无依赖关系，全部在 Redis Pub/Sub 层等待完成信号。

**此子节点依赖 AS-3.5.2 和 AS-3.5.3 完全完成后才开始。**

---

### AS-3.5 执行顺序

```
AS-3.5.1（Skill 绑定 AgentConfig）
  独立，可最先完成，改动集中在 mw_skill_resolution.go

AS-3.5.2（父子 Run 跨实例调度）
  migration 00062 → child_run.go → API 暴露 parent_run_id

AS-3.5.3（Lua Executor）
  依赖 AS-3.5.2（agent.run binding）
  lua.go → binding 设计 → SpawnChildRun 接入 → 测试

AS-3.5.4（并行原语）
  依赖 AS-3.5.2 + AS-3.5.3 完全完成
```

**AS-3.5.1 与 AS-3.5.2 可并行执行，AS-3.5.3 等 AS-3.5.2 完成后启动。**

---

## 6. AS-4 — MCP 健康与缓存

**目标**：减少每次 run 的 MCP 发现开销，修复 Pool 无健康检查导致 dead connection 的问题。

**解决的问题**：A5

**可与 AS-1 并行执行。**

### AS-4.1 — DiscoverFromDB 结果缓存

- **修改** `src/services/worker/internal/mcp/` 或 `pipeline/mw_mcp_discovery.go`：
  - 按 `orgID` 缓存 `DiscoverFromDB` 结果，TTL 60 秒（可配置）。
  - 缓存使用 `sync.Map` + 时间戳，Worker 进程内全局。
  - MCP 配置变更（Console 保存）触发 `pg_notify("mcp_config_changed", orgID)`，Worker 收到后主动 invalidate 该 org 的缓存。

**验收**：并发 10 个同 org 的 run 只触发 1 次 DB 查询（缓存命中）。

### AS-4.2 — Pool 健康检查 + 重连

- **修改** `src/services/worker/internal/mcp/pool.go`：
  - `Borrow` 时调用 `client.IsHealthy(ctx)`（定义为发 `ping` 或检查进程存活）。
  - 不健康时关闭旧 client，从 map 里删除，重建新 client。
  - stdio client 检查子进程是否仍在运行（`cmd.ProcessState` 或 keepalive ping）。

**验收**：单测模拟 stdio 进程退出，下次 `Borrow` 时自动重建，不返回 dead connection。

---

## 7. AS-5 — Memory System

**目标**：Agent 具备跨 thread / 跨 session 的长期记忆能力；接入 OpenViking（`volcengine/OpenViking`）作为第一个实现；通过 `MemoryProvider` 保持可替换（未来可接入其他 Memory 系统，不让 Worker 绑死在 OpenViking 上）。

**关于 OpenViking（基于 `/Users/qqqqqf/Documents/OpenViking` 调研）**：
- 形态：Python 包 + FastAPI Server；支持 Embedded / HTTP / CLI。
- 部署：官方镜像 `ghcr.io/volcengine/openviking:main`（默认启动 `openviking-server`，端口 `1933`）。
- HTTP API（常用）：
  - 检索：`POST /api/v1/search/find`（纯语义召回）、`POST /api/v1/search/search`（带 session 上下文的意图分析检索）
  - 分层读取：`GET /api/v1/content/abstract|overview|read?uri=...`（L0/L1/L2）
  - 会话：`POST /api/v1/sessions/{id}/messages` + `POST /api/v1/sessions/{id}/commit`（归档 + 提取长期记忆）
- 多租户：`ov.conf.server.root_api_key` 启用认证；ROOT key 路线下可用 header 指定身份（`X-OpenViking-Account`/`X-OpenViking-User`/`X-OpenViking-Agent`）。Arkloop 先走 ROOT key + header，避免把“OpenViking user key 发放/轮转/存储”提前塞进 AS-5 的关键路径。
- URI 规则：OpenViking 的 URI 是“所见即所得”映射（不会自动注入 user/agent space）。Worker 必须构造带 space 的 URI，否则会写进共享路径导致串租：
  - user space：`viking://user/{user_id}/...`
  - agent space：`viking://agent/{agent_space}/...`，其中 `agent_space = md5(user_id + agent_id)[:12]`
  - session space：`viking://session/{session_id}/...`（HTTP API 只用 `session_id`；内部存储含 user_space 但对 HTTP 客户端透明）
- 记忆分类：commit 会从会话消息中提取 6 类记忆（profile/preferences/entities/events/cases/patterns），写入 user/agent space 并建立向量索引。

**解决的问题**：F1

**依赖**：AS-1

### AS-5.0 — 缺项检查（落地前必须补齐）

- Worker 目前 `data.Run` 未加载 `runs.created_by_user_id`，Memory 必须按用户隔离：需要把 `created_by_user_id` 注入 `RunContext`（为 NULL 时直接跳过 Memory，避免写进 `default` user）。
- MemoryMiddleware 在 `next` 之后需要拿到“本次 run 产生的 assistant 最终回复”，否则无法把对话写入 OpenViking：
  - 要么在 `handler_agent_loop.go` 把最终文本写回 `RunContext`；
  - 要么在 middleware 里查询 `messages` 表取最新一条 assistant。
- 需要统一 `agent_id` 约定：默认用 `SkillDefinition.ID`（并收敛字符集到 `[a-zA-Z0-9_-]`），否则 OpenViking `UserIdentifier` 校验会拒绝。
- 需要 secrets/配置承载 OpenViking base_url 与 root key（生产），不要把密钥落在代码或 UI 文案里。

### AS-5.1 — MemoryProvider 接口（Worker 侧抽象）

- **新建** `src/services/worker/internal/memory/provider.go`：

```go
type MemoryIdentity struct {
    OrgID   uuid.UUID // -> OpenViking account_id
    UserID  uuid.UUID // -> OpenViking user_id
    AgentID string    // -> OpenViking agent_id（建议=skill_id）
}

type MemoryScope string

const (
    MemoryScopeUser  MemoryScope = "user"
    MemoryScopeAgent MemoryScope = "agent"
)

type MemoryHit struct {
    URI         string
    Abstract    string  // L0
    Score       float64
    MatchReason string
    IsLeaf      bool
}

type MemoryLayer string

const (
    MemoryLayerAbstract MemoryLayer = "abstract" // L0
    MemoryLayerOverview MemoryLayer = "overview" // L1
    MemoryLayerRead     MemoryLayer = "read"     // L2
)

type MemoryMessage struct {
    Role    string // "user" | "assistant"
    Content string // 先走 simple mode；后续再扩展 parts（context/tool）
}

type MemoryProvider interface {
    // 语义检索：返回 L0（abstract）+ URI；必要时再按 URI 拉 L1/L2
    Find(ctx context.Context, ident MemoryIdentity, scope MemoryScope, query string, limit int) ([]MemoryHit, error)

    // 读取分层内容（L0/L1/L2）
    Content(ctx context.Context, ident MemoryIdentity, uri string, layer MemoryLayer) (string, error)

    // 会话写入与 commit：用于触发长期记忆提取（由 OpenViking 内部完成压缩/归档/抽取）
    AppendSessionMessages(ctx context.Context, ident MemoryIdentity, sessionID string, msgs []MemoryMessage) error
    CommitSession(ctx context.Context, ident MemoryIdentity, sessionID string) error
}
```

接口刻意不暴露 OpenViking 的全部 FS 操作（ls/glob/grep/mv/rm）。AS-5 只围绕“记忆注入 + 记忆提取”两条主链路；高级能力后续用 Tool 暴露即可。

### AS-5.2 — OpenViking 服务部署（compose）

- **修改** `compose.yaml`：新增 `openviking` 服务，优先使用官方镜像，避免在 Arkloop 构建链里引入 Python/C++/Go 混编依赖。

关键点：
- 镜像：`ghcr.io/volcengine/openviking:main`
- 端口：`1933`
- 配置文件：挂载 `ov.conf` 到容器 `/app/ov.conf`
- 数据目录：挂载持久化卷到 `/app/data`
- 健康检查：`GET /health`；就绪探针可用 `GET /ready`

Worker 侧配置（DB 主，ENV 兜底，与 email/LLM 凭证同模式）：
- **主路径**：`platform_settings` 表，key 前缀 `openviking.*`
  - `openviking.base_url`（如 `http://openviking:1933`）
  - `openviking.root_api_key`（与 `ov.conf.server.root_api_key` 一致；未启用 auth 时留空）
- **兜底**：ENV `ARKLOOP_OPENVIKING_BASE_URL` / `ARKLOOP_OPENVIKING_ROOT_API_KEY`（本地开发 / 无 DB 时）

`ov.conf` 至少需要（JSON 格式）：

```json
{
  "storage": {
    "agfs":    { "backend": "local", "path": "/app/data" },
    "vectordb": { "backend": "local", "path": "/app/data" }
  },
  "embedding": { "dense": { "provider": "...", "api_key": "...", "model": "...", "dimension": 1024 } },
  "vlm":       { "provider": "...", "api_key": "...", "model": "..." },
  "rerank":    { "provider": "...", "api_key": "...", "model": "..." }
}
```

`embedding`/`vlm` 必须配置否则 find/search/commit 无法工作；`rerank` 可选（缺少时仅用向量相似度排序）。`storage` 必须包含 `backend` 字段，否则启动失败。

### AS-5.3 — OpenViking 适配器（HTTP Client）

- **新建** `src/services/worker/internal/memory/openviking/client.go`：通过 OpenViking HTTP API 实现 `MemoryProvider`。
- **新建** `src/services/worker/internal/memory/openviking/config.go`：配置加载，DB 主（`platform_settings.openviking.*`）+ ENV 兜底（参考 `email/config_db.go` 模式）。
- ROOT key 路线的 HTTP 请求头（`base_url` 和 `root_api_key` 从上述配置加载，运行时无需 ENV 直接存在）：
  - `X-API-Key: ${ARKLOOP_OPENVIKING_ROOT_API_KEY}`
  - `X-OpenViking-Account: <org_id>`
  - `X-OpenViking-User: <user_id>`
  - `X-OpenViking-Agent: <agent_id>`
- 端点映射（以 OpenViking 当前实现为准）：
  - Find → `POST /api/v1/search/find`
  - Content → `GET /api/v1/content/abstract|overview|read?uri=...`
  - AppendSessionMessages → `POST /api/v1/sessions/{session_id}/messages`
  - CommitSession → `POST /api/v1/sessions/{session_id}/commit`

实现要点：
- 由适配器负责把 `MemoryScope` 转成正确的 `target_uri`（带 user/agent space 段），避免上层忘记拼 space 导致串租。
- HTTP 超时必须有上限（防止卡住 run），并支持可配置的重试（仅限幂等读接口）。
- OpenViking 不可用/超时：不影响 run 主流程；降级为“无记忆”并写结构化 warn（日志只包含数据和错误状态）。

### AS-5.4 — MemoryMiddleware + Pipeline 接入

- **新建** `src/services/worker/internal/pipeline/mw_memory.go`，插入位置：`SkillResolutionMiddleware` 之后、`RoutingMiddleware` 之前（保证拿到 `skill_id` 作为 `agent_id`，但不影响路由选择）。

运行时行为：

1) `next` 之前（注入）
- 取本轮用户输入（`rc.Messages` 最后一条 role=user 的文本）作为 query。
- 调用 `Find`：
  - `Find(..., MemoryScopeUser, ...)`：用于注入用户偏好、实体、事件等
  - `Find(..., MemoryScopeAgent, ...)`：用于注入 cases/patterns（可按 tier 开关，默认可先关）
- 将 topK 的 `Abstract` 组织成紧凑的 memory block，追加到 `rc.SystemPrompt` 末尾（不覆盖 skill prompt）。
- 当命中条目是目录且分数很高时，可再拉一次 `Content(..., MemoryLayerOverview)`（L1）做更丰富注入；默认只注入 L0，避免 token 暴涨。

2) `next` 之后（提取，异步）
- 将本次 run 的 user message + assistant 最终回复写入 OpenViking session（`session_id = thread_id`，保证同一 thread 可持续累积并被 OpenViking 压缩归档）。
- 调用 `CommitSession(thread_id)` 触发归档 + 长期记忆提取。
- commit 不阻塞 run 返回（goroutine + context with timeout）。

- **修改** `src/services/worker/internal/runengine/v1.go`：
  - `MemoryMiddleware` 插入 `SkillResolutionMiddleware` 之后。
  - `EngineV1Deps` 新增：`MemoryProvider memory.MemoryProvider`（nil 时跳过整个 middleware）。

### 0. AS-5.0~5.4 代码基线（已完成）

AS-5.0~5.4 已全部完成，以下能力已存在：

- **MemoryProvider 接口**（`internal/memory/provider.go`）：Find/Content/AppendSessionMessages/CommitSession，MemoryIdentity 按 org/user/agent 三级隔离。
- **OpenViking HTTP Client**（`internal/memory/openviking/`）：完整实现 MemoryProvider，ROOT key 多租户 header，scopeURI 自动拼装防串租，幂等读接口带退避重试，4xx 不重试。配置 DB 主 + ENV 兜底。
- **MemoryMiddleware**（`internal/pipeline/mw_memory.go`）：run 前自动 Find 注入 system prompt（`<memory>` block），高分非叶节点额外拉 L1；run 后 goroutine 异步 AppendSessionMessages + CommitSession。
- **Pipeline 集成**（`runengine/v1.go`）：MemoryMiddleware 插入 SkillResolution 之后、Routing 之前。MemoryProvider nil 时整个 middleware no-op。
- **Lua binding**（`executor/lua.go`）：`memory.search` 已注册但为 **stub**（返回空数组）。
- **compose.yaml**：openviking 服务已部署。

**当前架构的根本性偏差**：

AS-5.0~5.4 将 Memory 定位为"被动的对话录制器"——middleware 自动注入/归档，Agent 自身无法主动读写记忆。这只解决了最浅层需求（"上次聊了什么"）。

Memory 的正确定位是 **Agent 的主动认知工具**：

1. Agent 通过 tool call 主动 search/read/write/forget 记忆，而不是只依赖 middleware 自动注入。
2. 编排层（Lua/Orchestrator）在流程任意节点调用 Memory，而不是只在 run 前后触发。
3. 写入的内容是结构化知识（"用户偏好中文"、"用户的技术栈是 Go + React"、"上次用 Sandbox 跑 Python 分析 CSV 的结论"），不是原始对话文本。
4. 非对话数据是一等公民：用户环境信息、安装的 Skill 列表、Sandbox/Browser 执行结论——不是"对话"但都是 Agent 理解用户的关键上下文。

Session 归档（AppendSessionMessages + CommitSession）保留——OpenViking 的 commit 会从原始对话中自动提取 6 类记忆，这是记忆的来源之一。但 Agent 主动写入的结构化知识是更高优先级的记忆来源。

**目标架构**：

```
Memory System
  ├── Tool 层（一等公民）：Agent 通过 tool call 主动 search/read/write/forget
  ├── 编排层 binding：Lua 脚本通过 memory.search/read/write 直接调用
  ├── Pipeline middleware（角色修正）：
  │     - run 前：自动注入相关记忆（不变）
  │     - run 后：session 归档（不变）+ 触发记忆提炼（新增，见 AS-5.7）
  └── Session 归档（保留）：原始对话存 OpenViking session，commit 自动提取记忆
```

### AS-5.5 — MemoryProvider 扩展：Write + Delete

当前 MemoryProvider 只有读（Find/Content）和 session 写入（AppendSessionMessages），缺少 Agent 主动写入结构化知识的能力。

#### 5.5.1 — 接口扩展

**修改** `internal/memory/provider.go`：

```go
// MemoryEntry 是一条主动写入的结构化记忆。
type MemoryEntry struct {
    URI      string            // 存储路径，如 "viking://user/{id}/facts/prefers_chinese"
    Content  string            // 记忆正文（纯文本）
    Metadata map[string]string // 可选元数据：source_run_id, category, confidence 等
}

// MemoryCategory 预定义的记忆分类，与 OpenViking 的 6 类记忆对齐。
type MemoryCategory string

const (
    MemoryCategoryProfile     MemoryCategory = "profile"     // 用户基础信息
    MemoryCategoryPreference  MemoryCategory = "preferences" // 偏好设置
    MemoryCategoryEntity      MemoryCategory = "entities"    // 关键实体（人/项目/技术栈）
    MemoryCategoryEvent       MemoryCategory = "events"      // 事件记录
    MemoryCategoryCase        MemoryCategory = "cases"       // 执行案例（Sandbox/Browser 结论）
    MemoryCategoryPattern     MemoryCategory = "patterns"    // 行为模式
)

type MemoryProvider interface {
    // === 已有 ===
    Find(ctx context.Context, ident MemoryIdentity, scope MemoryScope, query string, limit int) ([]MemoryHit, error)
    Content(ctx context.Context, ident MemoryIdentity, uri string, layer MemoryLayer) (string, error)
    AppendSessionMessages(ctx context.Context, ident MemoryIdentity, sessionID string, msgs []MemoryMessage) error
    CommitSession(ctx context.Context, ident MemoryIdentity, sessionID string) error

    // === 新增 ===
    // Write 主动写入一条结构化记忆到指定 scope。
    // URI 由调用方构造（适配器校验格式），内容会被 OpenViking 建立向量索引。
    Write(ctx context.Context, ident MemoryIdentity, scope MemoryScope, entry MemoryEntry) error

    // Delete 删除指定 URI 的记忆。
    Delete(ctx context.Context, ident MemoryIdentity, uri string) error
}
```

#### 5.5.2 — OpenViking 适配器扩展

**修改** `internal/memory/openviking/client.go`，新增：

- `Write` → **OpenViking HTTP API 当前无直接写入 `user://` / `agent://` scope 的端点**。
  - `PUT /api/v1/content/write` 不存在；`POST /api/v1/resources` 只接受 `resources` scope（源码 `resource_service.py` 明确拒绝 user/agent scope）。
  - 唯一 HTTP 写路径：通过 `AppendSessionMessages` + `CommitSession`（OpenViking 在 commit 时自动提取结构化记忆写入 user/agent space）。
  - 结论：**主动写入结构化记忆必须改为"写消息 + commit"范式**，而不是直接写 URI。
  - 如果 OpenViking 未来开放写入接口，在此更新。
- `Delete` → OpenViking `DELETE /api/v1/fs?uri={uri}&recursive={bool}`（filesystem 路由，**不是** content 路由）。

#### 5.5.3 — URI 构造辅助

**新建** `internal/memory/uri.go`：

```go
// BuildURI 构造标准的 memory URI，确保格式统一且不会串租。
// 例：BuildURI(MemoryScopeUser, ident, MemoryCategoryPreference, "language")
//   → "viking://user/{user_id}/preferences/language"
func BuildURI(scope MemoryScope, ident MemoryIdentity, category MemoryCategory, key string) string
```

上层（Tool/Lua/middleware）通过 `BuildURI` 构造 URI，不直接拼字符串。

**验收**：Write 写入后 Find 可检索到；Delete 后 Find 不再返回。

### AS-5.6 — Memory Tool（Agent 主动调用）

**目标**：Agent 在 loop 中通过 tool call 主动读写记忆，而不是只依赖 middleware 被动注入。

#### 5.6.1 — Tool 定义

**新建** `src/services/worker/internal/tools/memory/`：

**memory_search** — Agent 主动检索记忆：

```go
var SearchSpec = tools.AgentToolSpec{
    Name: "memory_search", Version: "1",
    Description: "search long-term memory for relevant information",
    RiskLevel: tools.RiskLevelLow, SideEffects: false,
}

var SearchLlmSpec = llm.ToolSpec{
    Name: "memory_search",
    Description: stringPtr("search your long-term memory about the user or past interactions"),
    JSONSchema: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "query": map[string]any{"type": "string", "minLength": 1},
            "scope": map[string]any{"type": "string", "enum": []string{"user", "agent"}},
            "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20},
        },
        "required":             []string{"query"},
        "additionalProperties": false,
    },
}
```

**memory_read** — 按 URI 读取详细内容（L1/L2）：

```go
var ReadLlmSpec = llm.ToolSpec{
    Name: "memory_read",
    Description: stringPtr("read detailed content of a memory entry by URI"),
    JSONSchema: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "uri":   map[string]any{"type": "string"},
            "depth": map[string]any{"type": "string", "enum": []string{"overview", "full"}},
        },
        "required":             []string{"uri"},
        "additionalProperties": false,
    },
}
```

**memory_write** — Agent 主动写入结构化知识：

```go
var WriteLlmSpec = llm.ToolSpec{
    Name: "memory_write",
    Description: stringPtr("store a piece of knowledge in long-term memory for future reference"),
    JSONSchema: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "category": map[string]any{"type": "string", "enum": []string{
                "profile", "preferences", "entities", "events", "cases", "patterns",
            }},
            "key":     map[string]any{"type": "string", "minLength": 1, "pattern": "^[a-zA-Z0-9_\\-\\.]+$"},
            "content": map[string]any{"type": "string", "minLength": 1},
            "scope":   map[string]any{"type": "string", "enum": []string{"user", "agent"}},
        },
        "required":             []string{"category", "key", "content"},
        "additionalProperties": false,
    },
}
```

**memory_forget** — 删除记忆：

```go
var ForgetLlmSpec = llm.ToolSpec{
    Name: "memory_forget",
    Description: stringPtr("remove a specific memory entry"),
    JSONSchema: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "uri": map[string]any{"type": "string"},
        },
        "required":             []string{"uri"},
        "additionalProperties": false,
    },
}
```

#### 5.6.2 — MemoryToolExecutor 实现

**新建** `src/services/worker/internal/tools/memory/executor.go`：

```go
type MemoryToolExecutor struct {
    provider memory.MemoryProvider
}
```

实现 `tools.Executor`，按 toolName 分发：

- `memory_search`：构造 MemoryIdentity（从 ExecutionContext 获取 org_id/user_id/agent_id）→ `provider.Find` → 返回 hits 列表（URI + abstract + score）
- `memory_read`：`provider.Content` → 返回 L1（overview）或 L2（full）内容
- `memory_write`：`memory.BuildURI(scope, ident, category, key)` → `provider.Write` → 返回写入的 URI
- `memory_forget`：`provider.Delete` → 返回确认

**MemoryIdentity 获取**：需要扩展 `tools.ExecutionContext`，增加 `UserID *uuid.UUID` 和 `AgentID string` 字段（由 pipeline 注入），或者 MemoryToolExecutor 在构造时持有一个 identity resolver。

**条件注册**：与 Sandbox/Browser 一致——MemoryProvider 非 nil 时才注册 memory 工具。

#### 5.6.3 — Lua binding 接入真实 Provider

**修改** `internal/executor/lua.go`：

将 `memory.search` stub 替换为真实调用：

```go
func (rt *luaRuntime) memorySearch(L *lua.LState) int {
    if rt.rc.MemoryProvider == nil {
        L.Push(lua.LString("[]"))
        L.Push(lua.LNil)
        return 2
    }
    query := L.CheckString(1)
    // ... 构造 ident，调用 provider.Find，将 hits 序列化为 JSON ...
}
```

新增 `memory.read`、`memory.write`、`memory.forget` binding，映射到 MemoryProvider 的对应方法。

**RunContext 扩展**：增加 `MemoryProvider memory.MemoryProvider` 字段（由 EngineV1 注入），Lua runtime 通过 `rt.rc.MemoryProvider` 访问。

**验收**：Lua 脚本中 `memory.search("用户偏好")` 返回真实结果；`memory.write("preferences", "language", "中文")` 写入后可被 search 检索到。

### AS-5.7 — 记忆提炼管线（Tool 输出 + 环境信息 → Memory）

**目标**：run 结束后不仅归档原始对话，还将有价值的 tool 输出、环境信息、执行结论提炼为结构化记忆写入 OpenViking。

#### 5.7.1 — 核心思路

记忆提炼不是简单的"把 tool result 存进去"——那样会产生大量低价值噪音。正确的做法是：**让 LLM 判断哪些信息值得长期记忆，提炼后再写入。**

```
run 结束
  → 收集本次 run 的上下文：
      - user message
      - assistant output
      - tool calls（name + args + result 摘要）
      - 环境信息变更（新安装的 Skill、Sandbox 环境信息等）
  → 用轻量 LLM（lite 级别）做一次记忆提炼：
      system_prompt: "从以下对话和工具执行结果中，提取值得长期记忆的结构化知识点。"
      输出格式: [{category, key, content, scope}]
  → 对每条提炼结果调用 provider.Write
  → 同时保留原有的 session 归档（CommitSession）
```

#### 5.7.2 — MemoryMiddleware 改造

**修改** `internal/pipeline/mw_memory.go`，在 `next` 之后增加记忆提炼步骤：

```go
// run 后：session 归档 + 记忆提炼（均异步）
if userQuery != "" && assistantOutput != "" {
    // 1. session 归档（保持不变）
    go commitMemoryAsync(ident, provider, sessionID, msgs)

    // 2. 记忆提炼（新增，需要 tool call 记录）
    if rc.ToolCallRecords != nil && len(rc.ToolCallRecords) > 0 {
        go distillMemoryAsync(ident, provider, rc.Gateway, userQuery, assistantOutput, rc.ToolCallRecords)
    }
}
```

**RunContext 扩展**：新增 `ToolCallRecords []ToolCallRecord` 字段，AgentLoop 每次 tool call 完成后追加记录（tool_name + args 摘要 + result 摘要，做长度截断）。

#### 5.7.3 — 环境信息主动写入

以下场景触发环境信息写入 Memory（不需要 LLM 提炼，直接写结构化数据）：

| 触发点 | category | key | content 示例 | scope |
|---|---|---|---|---|
| 用户首次使用（注册后第一次 run） | profile | system_info | `{os, browser, locale, timezone}` | user |
| 用户安装新 Skill | entities | skill_{skill_id} | Skill 的 display_name + description | user |
| Sandbox 首次执行 | entities | sandbox_env | Python 版本、已安装包列表 | agent |
| Browser 登录某网站 | events | browser_login_{domain} | 登录时间 + 域名 | agent |

这些写入由各自的 ToolExecutor 在执行后触发（不在 middleware 里），通过 `ExecutionContext` 中的 MemoryProvider 引用调用 `Write`。

**不过度设计**：初期只做"Skill 安装"和"Sandbox 首次执行"两个触发点。其他触发点按需添加，不要一次性全写。

#### 5.7.4 — distillMemoryAsync 实现

**新建** `internal/pipeline/memory_distill.go`：

```go
func distillMemoryAsync(
    ident memory.MemoryIdentity,
    provider memory.MemoryProvider,
    gateway llm.Gateway,
    userQuery, assistantOutput string,
    toolRecords []ToolCallRecord,
)
```

流程：
1. 构造 distill prompt（包含本次对话的 user/assistant/tool 摘要）
2. 用 gateway 做一次 single-shot LLM call（不需要 tools，轻量模型即可）
3. 解析 LLM 输出为 `[{category, key, content, scope}]`
4. 对每条调用 `provider.Write`
5. 整个过程异步，超时 30s，失败只 warn 不影响主流程

**成本控制**：
- 不是每次 run 都触发 distill——只有 tool call 数量 >= 2 或对话轮数 >= 3 时才触发（简单对话不值得额外花一次 LLM call）
- 使用最便宜的模型（lite tier 对应的 route）
- distill prompt 控制在 2000 token 以内（tool result 截断到 200 字/条）

### AS-5.8 — Memory 测试

#### 5.8.1 — 单测

- **mw_memory_test.go**：mock MemoryProvider，验证：
  - provider nil 时 middleware 为 no-op
  - UserID nil 时跳过
  - Find 返回结果时 SystemPrompt 正确追加 `<memory>` block
  - run 后 AppendSessionMessages + CommitSession 被调用
  - 高分非叶节点额外拉 L1

- **openviking/client_test.go**：httptest 模拟 OpenViking API，验证：
  - Find 正确构造 request body（特别是 target_uri 拼装）
  - Content 正确处理 string/object 两种返回格式
  - AppendSessionMessages 逐条发送
  - Write/Delete 正确调用
  - 重试逻辑：5xx 重试、4xx 不重试、ctx 取消终止
  - 多租户 header 正确设置

- **memory/uri_test.go**：验证 BuildURI 各种 scope/category/key 组合的输出格式。

- **tools/memory/executor_test.go**：mock MemoryProvider，验证 4 个 tool 的参数解析和结果映射。

#### 5.8.2 — 集成测试

- **OpenViking 真实调用测试**（需要 OpenViking 服务运行，可标记 `//go:build integration`）：
  - Write → Find → Content → Delete 完整链路
  - 多租户隔离：org A 写入的记忆 org B 查不到
  - Session 归档：AppendSessionMessages → CommitSession → Find 能检索到提取的记忆
  - 并发安全：10 个 goroutine 同时 Find 不 panic

#### 5.8.3 — Lua binding 测试

- **executor/lua_test.go** 扩展：mock MemoryProvider 注入 RunContext，验证 `memory.search`/`memory.write`/`memory.read`/`memory.forget` 返回正确结果。

**执行顺序**：

```
AS-5.5（Provider 扩展 Write/Delete）
  → AS-5.6（Memory Tool + Lua binding）← 可与 5.5 并行写 Tool 框架，5.5 完成后接入
  → AS-5.7（记忆提炼管线）← 依赖 5.5 的 Write + LLM Gateway
  → AS-5.8（测试）← 覆盖 5.5~5.7 全部新增代码

AS-5.5 和 AS-5.6 可并行推进；AS-5.7 依赖 AS-5.5；AS-5.8 在所有功能完成后执行。
```

---

## 8. AS-6 — Sandbox 服务

**目标**：实现代码执行隔离环境，支持 Agent 运行 shell/Python 代码，崩溃不影响 Worker。

**技术选型**：直接上 Firecracker microVM（宿主机 KVM 环境已满足），不走 gVisor 过渡。Firecracker 提供 VM 级隔离（独立内核），快照能力支持 < 1s resume，适合高并发 Agent 场景。

**解决的问题**：A4、F3

**可完全独立并行推进，不依赖 AS-1~5。**

### 0. 代码基线（AS-6.1 已完成）

AS-6.1 已完成，以下能力已存在于 `src/services/sandbox/`：

**服务层（`internal/app/`）：** HTTP server 骨架、Config 从 ENV 加载（地址/firecracker 路径/kernel/rootfs/socket dir/boot timeout/agent port/max sessions）、graceful shutdown、recover middleware。

**Session 管理（`internal/session/`）：** `Manager` 线程安全管理所有活跃 Session（`sync.Mutex` + `map[string]*Session`）。`GetOrCreate` 按 session_id 获取或创建 microVM；`Delete` 停止并销毁；`CloseAll` 在服务关闭时终止所有 VM。创建流程：启动 Firecracker 进程 → 等待 API socket → 通过 HTTP API 配置 machine-config/boot-source/drives/vsock → InstanceStart → 等待 Guest Agent 就绪（ping 探测）。

**Firecracker client（`internal/firecracker/`）：** 通过 Unix domain socket 封装 Firecracker HTTP API（PUT /machine-config、/boot-source、/drives/{id}、/vsock、/actions）。Tier 配置：lite 1vCPU/256MB、pro 1vCPU/1GB、ultra 2vCPU/4GB。

**Guest Agent（`agent/main.go`）：** 编译为静态二进制置于 rootfs 内，通过 vsock 端口 8080 监听。接收 JSON 格式 ExecJob（language/code/timeout_ms），fork python3/sh 执行，返回 stdout/stderr/exit_code。Python 代码写入临时文件执行（避免 -c 转义问题）。

**HTTP API（`internal/http/`）：** `POST /v1/exec`（session_id/tier/language/code/timeout_ms → stdout/stderr/exit_code/duration_ms）、`DELETE /v1/sessions/{id}`、`GET /healthz`。

**当前缺失（AS-6.2~6.5 要解决的）：**
- 无快照能力（每次冷启动，boot 延迟 ~5-30s）
- 无 warm pool（首次请求必须等待 VM 启动完毕）
- 无 MinIO 集成（无持久化存储）
- Worker 侧无 ToolExecutor（Worker 无法调用 Sandbox）
- 无 session 超时回收（空闲 VM 永远不释放，只能显式 DELETE 或服务重启）
- 无 rootfs 模板管理（单一硬编码 rootfs 路径，无法按需切换 Python/Node/Rust 等环境）
- 无可观测性（无 metrics，healthz 只返回 session 计数）
- 无 Console 管理

### AS-6.2 — Firecracker 快照 + MinIO 持久化

**目标**：消除冷启动延迟。首次启动 microVM 并就绪后打快照，后续从快照 resume（< 1s），大幅降低 Agent 等待时间。

#### 6.2.1 — 快照生命周期

**概念模型：**

```
Template = rootfs + kernel + tier 的组合
  例：python3.12-lite  = python3.12.ext4 + vmlinux + lite tier
      python3.12-pro   = python3.12.ext4 + vmlinux + pro tier
      node20-lite      = node20.ext4     + vmlinux + lite tier

Snapshot = 某个 Template 的 microVM 在 Guest Agent 就绪后的完整内存 + 磁盘状态
  存储路径：MinIO sandbox-snapshots/{template_id}/mem.snap + disk.snap
```

快照的生命周期：

1. **创建**（首次或显式触发）：
   - 冷启动一个 microVM（按 Template 的 rootfs + kernel + tier 配置）
   - 等待 Guest Agent 就绪（ping 探测通过）
   - 调用 Firecracker `PATCH /vm` 暂停 VM
   - 调用 Firecracker `PUT /snapshot/create`（`snapshot_type: "Full"`，`snapshot_path` 和 `mem_file_path` 指向本地临时目录）
   - 上传 `mem.snap` + `disk.snap` 到 MinIO `sandbox-snapshots/{template_id}/`
   - 销毁 microVM（快照已持久化，VM 本身不再需要）

2. **恢复**（每次 GetOrCreate 需要新 VM 时）：
   - 从 MinIO 下载 `mem.snap` + `disk.snap` 到本地临时目录
   - 启动 Firecracker 进程（空的，不配置 machine-config/boot-source）
   - 调用 Firecracker `PUT /snapshot/load`（`snapshot_path` + `mem_backend` 指向本地文件）
   - 调用 `PATCH /vm` 恢复运行
   - Guest Agent 立即可用（不需要再等 boot）

3. **更新**（rootfs 变更或定期刷新）：
   - 用新 rootfs 重新执行创建流程
   - 上传到同一 MinIO 路径（覆盖）
   - 正在运行的 VM 不受影响，下次从快照恢复时用新版本

#### 6.2.2 — Firecracker Client 扩展

**修改** `internal/firecracker/client.go`，新增快照相关 API：

```go
// Pause 暂停 microVM（打快照前必须暂停）。
func (c *Client) Pause(ctx context.Context) error

// Resume 恢复 microVM 运行。
func (c *Client) Resume(ctx context.Context) error

// CreateSnapshot 创建全量快照。
func (c *Client) CreateSnapshot(ctx context.Context, snapshotPath, memFilePath string) error

// LoadSnapshot 从快照恢复 microVM。
func (c *Client) LoadSnapshot(ctx context.Context, snapshotPath, memBackendPath string, resumeVM bool) error
```

对应的 Firecracker API：
- Pause/Resume → `PATCH /vm`（`state: "Paused"` / `state: "Resumed"`）
- CreateSnapshot → `PUT /snapshot/create`（`snapshot_type: "Full"`）
- LoadSnapshot → `PUT /snapshot/load`（`enable_diff_snapshots: false`）

#### 6.2.3 — MinIO 存储层

**新建** `internal/storage/minio.go`：

```go
type SnapshotStore interface {
    // Upload 将本地快照文件上传到对象存储。
    Upload(ctx context.Context, templateID string, memPath, diskPath string) error

    // Download 将快照文件下载到本地临时目录，返回本地路径。
    Download(ctx context.Context, templateID string) (memPath, diskPath string, err error)

    // Exists 检查某 template 的快照是否存在。
    Exists(ctx context.Context, templateID string) (bool, error)
}
```

实现要点：
- 使用 MinIO Go SDK（`github.com/minio/minio-go/v7`），复用已有的 `ARKLOOP_S3_ACCESS_KEY`/`ARKLOOP_S3_SECRET_KEY`/`ARKLOOP_S3_ENDPOINT` 配置。
- Bucket：`sandbox-snapshots`（服务启动时 `MakeBucket` 确保存在）。
- 对象路径：`{template_id}/mem.snap`、`{template_id}/disk.snap`。
- 下载到本地临时目录 `{socket_base_dir}/_snapshots/{template_id}/`，下载后校验文件大小。
- 本地缓存：下载过的快照文件保留在本地，下次直接使用（避免每次 resume 都走网络）。通过 ETag 或 Last-Modified 判断是否需要重新下载。

#### 6.2.4 — Template 注册表

**新建** `internal/template/registry.go`：

```go
type Template struct {
    ID              string   // 如 "python3.12-lite"
    KernelImagePath string   // 内核路径（可共用）
    RootfsPath      string   // rootfs 路径（每个 template 不同）
    Tier            string   // "lite" | "pro" | "ultra"
    Languages       []string // 支持的语言：["python", "shell"]
}

type Registry struct {
    templates map[string]Template
}
```

初期 template 由配置文件定义（`config/sandbox/templates.json`），不走 DB。后续 AS-6.5 做 Console 管理时再考虑入库。

**配置文件示例**（`config/sandbox/templates.json`）：

```json
[
  {
    "id": "python3.12-lite",
    "kernel_image_path": "/opt/sandbox/vmlinux",
    "rootfs_path": "/opt/sandbox/rootfs/python3.12.ext4",
    "tier": "lite",
    "languages": ["python", "shell"]
  },
  {
    "id": "python3.12-pro",
    "kernel_image_path": "/opt/sandbox/vmlinux",
    "rootfs_path": "/opt/sandbox/rootfs/python3.12.ext4",
    "tier": "pro",
    "languages": ["python", "shell"]
  },
  {
    "id": "python3.12-ultra",
    "kernel_image_path": "/opt/sandbox/vmlinux",
    "rootfs_path": "/opt/sandbox/rootfs/python3.12.ext4",
    "tier": "ultra",
    "languages": ["python", "shell"]
  }
]
```

同一个 rootfs 可被不同 tier 的 template 引用（资源配额不同，rootfs 相同）。

#### 6.2.5 — 快照预制工具

**新建** `cmd/snapshot-builder/main.go`：CLI 工具，用于手动创建/更新快照。

```
Usage:
  snapshot-builder create --template python3.12-lite
  snapshot-builder create --all
  snapshot-builder list
```

流程：读取 templates.json → 冷启动 microVM → 等待 Agent 就绪 → 打快照 → 上传 MinIO → 销毁 VM。

服务启动时也自动检查：对每个注册的 template，若 MinIO 上无快照，则自动执行一次创建（阻塞启动，确保 warm pool 有快照可用）。

#### 6.2.6 — Manager 改造：优先从快照恢复

**修改** `internal/session/manager.go` 的 `create` 方法：

```
create(ctx, sessionID, tier) 的新流程：
  1. 从 Template Registry 查找匹配 tier 的 template（默认 python3.12-{tier}）
  2. 检查 SnapshotStore.Exists(template_id)
     - 存在 → 走 resume 路径：Download → 启动 Firecracker → LoadSnapshot → Resume → 就绪
     - 不存在 → 走冷启动路径（现有逻辑）：Configure → Start → waitForAgent
  3. resume 失败 → 降级到冷启动（快照可能损坏）
```

**性能预期**：冷启动 5-30s → 快照恢复 < 1s。

**验收**：快照存在时，`POST /v1/exec` 的首次 session 创建延迟 < 1.5s（含 MinIO 本地缓存命中）；快照不存在时自动降级到冷启动且不报错。

### AS-6.3 — Sandbox Controller（Warm Pool + 生命周期管理）

**目标**：消除按需创建的延迟——即使有快照恢复（< 1s），在高并发场景下仍然需要预热的 VM 立即可用。同时解决空闲 VM 不回收的问题。

#### 6.3.1 — Warm Pool 设计

**新建** `internal/pool/warm_pool.go`：

```go
type WarmPoolConfig struct {
    // 各 tier 的预热数量。为 0 表示不预热该 tier。
    WarmSizes map[string]int // {"lite": 3, "pro": 2, "ultra": 1}

    // 补充策略
    RefillIntervalSeconds int  // 检查补充间隔（默认 5s）
    MaxRefillConcurrency  int  // 同时补充的最大并发数（默认 2）
}

type WarmPool struct {
    config    WarmPoolConfig
    store     storage.SnapshotStore
    templates template.Registry
    ready     map[string]chan *Session // tier → buffered channel of pre-warmed sessions
}

// Acquire 从 warm pool 取一个就绪的 VM，立即返回。
// pool 为空时降级到按需创建（从快照恢复或冷启动）。
func (p *WarmPool) Acquire(ctx context.Context, tier string) (*Session, error)

// Release 销毁 VM 并异步触发补充。
func (p *WarmPool) Release(ctx context.Context, sessionID string)

// Drain 停止补充，等待所有预热 VM 关闭（graceful shutdown 时调用）。
func (p *WarmPool) Drain(ctx context.Context)
```

**运行机制：**

```
服务启动
  → 读取 WarmPoolConfig
  → 为每个 tier 启动一个后台 goroutine（refiller）
  → refiller 循环：检查 ready channel 长度 < warm_size → 从快照创建新 VM → 放入 channel

请求到来（GetOrCreate）
  → 先尝试从 ready[tier] channel 非阻塞 receive
    → 成功 → 将预热 VM 绑定到 sessionID，立即返回（0ms 延迟）
    → 失败（pool 为空）→ 按需创建（快照恢复 < 1s / 冷启动 5-30s）

Session 释放（Delete）
  → 销毁 VM
  → refiller 下一轮检查时自动补充
```

**配置加载**（新增 ENV）：

```
ARKLOOP_SANDBOX_WARM_LITE=3        # lite tier 预热数量
ARKLOOP_SANDBOX_WARM_PRO=2         # pro tier 预热数量
ARKLOOP_SANDBOX_WARM_ULTRA=1       # ultra tier 预热数量
ARKLOOP_SANDBOX_REFILL_INTERVAL=5  # 补充检查间隔（秒）
ARKLOOP_SANDBOX_REFILL_CONCURRENCY=2
```

#### 6.3.2 — Session 空闲超时回收

**当前问题**：session 创建后永不自动释放，只能由 Worker 显式 `DELETE` 或服务重启时 `CloseAll`。如果 Worker 崩溃或忘记清理，VM 永远泄漏。

**修改** `internal/session/manager.go`：

```go
type Session struct {
    // ...现有字段...
    LastActiveAt time.Time           // 最近一次 Exec 调用时间
    IdleTimeout  time.Duration       // 空闲超时（默认 5min，可按 tier 配置）
    MaxLifetime  time.Duration       // 最大存活时间（默认 30min，防止无限占用）
    idleTimer    *time.Timer         // 空闲超时 timer
}
```

**机制：**
- 每次 `Exec` 调用后重置 `LastActiveAt` 和 `idleTimer`。
- `idleTimer` 到期 → Manager 自动调用 `Delete(sessionID)`，释放 VM，触发 warm pool 补充。
- `MaxLifetime` 到期 → 无论是否活跃，强制销毁（防止单个 session 无限占用资源）。
- 超时时间可按 tier 差异化：lite 3min、pro 5min、ultra 10min（ultra 任务通常更长）。

**新增 ENV：**

```
ARKLOOP_SANDBOX_IDLE_TIMEOUT_LITE=180      # 秒
ARKLOOP_SANDBOX_IDLE_TIMEOUT_PRO=300
ARKLOOP_SANDBOX_IDLE_TIMEOUT_ULTRA=600
ARKLOOP_SANDBOX_MAX_LIFETIME=1800          # 秒，所有 tier 统一
```

#### 6.3.3 — Manager 重构：集成 WarmPool

**修改** `internal/session/manager.go`：将现有的 `create` 逻辑委托给 WarmPool。

```
现有：Manager 直接管理 sessions map + Firecracker 进程
改为：Manager 持有 WarmPool 引用，WarmPool 负责 VM 的创建/预热/回收

GetOrCreate(sessionID, tier):
  1. sessions map 命中 → 返回（现有逻辑不变）
  2. 未命中 → pool.Acquire(tier) → 绑定 sessionID → 启动 idle timer → 放入 sessions map

Delete(sessionID):
  1. 从 sessions map 移除
  2. 停止 idle timer
  3. pool.Release(sessionID)（销毁 VM + 触发补充）
```

**验收**：
- warm pool 有预热 VM 时，`POST /v1/exec` 首次 session 创建延迟 < 50ms。
- 空闲超时后 session 自动回收，`ActiveCount()` 减少。
- MaxLifetime 到期后 session 被强制回收。
- 服务 graceful shutdown 时，所有预热 VM 和活跃 VM 正确关闭。

#### 6.3.4 — 可观测性

**修改** `internal/http/handler.go`，新增 `GET /v1/stats` 端点：

```json
{
  "active_sessions": 5,
  "sessions_by_tier": {"lite": 3, "pro": 1, "ultra": 1},
  "warm_pool": {
    "lite":  {"ready": 2, "target": 3},
    "pro":   {"ready": 2, "target": 2},
    "ultra": {"ready": 0, "target": 1}
  },
  "templates": [
    {"id": "python3.12-lite", "snapshot_exists": true, "snapshot_size_mb": 128},
    {"id": "python3.12-pro",  "snapshot_exists": true, "snapshot_size_mb": 256}
  ],
  "total_created": 142,
  "total_destroyed": 137,
  "total_timeout_reclaimed": 8
}
```

**同时修改** `GET /healthz`，增加 readiness 语义：

```json
// 服务启动但 warm pool 未就绪（正在创建快照/预热）
{"status": "starting", "sessions": 0, "warm_pool_ready": false}

// 正常运行
{"status": "ok", "sessions": 5, "warm_pool_ready": true}

// 接近容量上限（active >= 80% max）
{"status": "pressure", "sessions": 40, "max_sessions": 50, "warm_pool_ready": true}
```

### AS-6.4 — SandboxToolExecutor 接入 Worker

**目标**：Worker 通过 `tools.Executor` 接口调用 Sandbox 服务，Agent 获得 `code_execute` 和 `shell_execute` 工具能力。

#### 6.4.1 — AgentToolSpec + LlmSpec 定义

**新建** `src/services/worker/internal/tools/sandbox/spec.go`：

```go
var (
    CodeExecuteSpec = tools.AgentToolSpec{
        Name:        "code_execute",
        Version:     "1",
        Description: "execute Python code in isolated sandbox and return output",
        RiskLevel:   tools.RiskLevelHigh,
        SideEffects: true,
    }
    ShellExecuteSpec = tools.AgentToolSpec{
        Name:        "shell_execute",
        Version:     "1",
        Description: "execute shell commands in isolated sandbox and return output",
        RiskLevel:   tools.RiskLevelHigh,
        SideEffects: true,
    }
)

var CodeExecuteLlmSpec = llm.ToolSpec{
    Name:        "code_execute",
    Description: stringPtr("execute Python code in an isolated sandbox environment"),
    JSONSchema: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "code":       map[string]any{"type": "string", "minLength": 1},
            "timeout_ms": map[string]any{"type": "integer", "minimum": 1000, "maximum": 300000},
        },
        "required":             []string{"code"},
        "additionalProperties": false,
    },
}

var ShellExecuteLlmSpec = llm.ToolSpec{
    Name:        "shell_execute",
    Description: stringPtr("execute shell commands in an isolated sandbox environment"),
    JSONSchema: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "command":    map[string]any{"type": "string", "minLength": 1},
            "timeout_ms": map[string]any{"type": "integer", "minimum": 1000, "maximum": 300000},
        },
        "required":             []string{"command"},
        "additionalProperties": false,
    },
}
```

#### 6.4.2 — SandboxToolExecutor 实现

**新建** `src/services/worker/internal/tools/sandbox/executor.go`：

```go
type SandboxToolExecutor struct {
    baseURL    string
    httpClient *http.Client
}
```

实现 `tools.Executor` 接口，核心逻辑：

1. **session_id 确定**：默认取 `ExecutionContext.RunID` 的字符串形式。同一个 run 内的多次 tool call 复用同一个 sandbox session（Agent 可能先 `code_execute` 安装包，再 `code_execute` 使用包，环境需要延续）。

2. **tier 确定**：从 `ExecutionContext.Budget` 中读取 `"sandbox_tier"`（由 Skill budgets 声明）。未声明时默认 `"lite"`。

3. **HTTP 调用**：构造 `ExecRequest` → `POST {baseURL}/v1/exec` → 解析 `ExecResponse`。

4. **结果映射**：
   - `exit_code == 0` → `ExecutionResult.ResultJSON = {"stdout": "...", "stderr": "...", "exit_code": 0, "duration_ms": N}`
   - `exit_code != 0` → 同上（不是 error，agent 需要看到 stderr 来调试代码）
   - HTTP 调用失败 → `ExecutionError{ErrorClass: "tool.sandbox_error", Message: ...}`
   - Sandbox 服务不可用 → `ExecutionError{ErrorClass: "tool.sandbox_unavailable", Message: ...}`

5. **stdout/stderr 截断**：Sandbox 执行结果可能很长（如打印大数据集），超过阈值（默认 32KB）时截断并追加 `\n... (truncated, total N bytes)`，避免 LLM context 爆炸。

6. **工具名分发**：
   - `code_execute` → language = `"python"`，code = args["code"]
   - `shell_execute` → language = `"shell"`，code = args["command"]

#### 6.4.3 — 注册到 builtin

**修改** `src/services/worker/internal/tools/builtin/builtin.go`：

```go
func AgentSpecs() []tools.AgentToolSpec {
    specs := []tools.AgentToolSpec{
        EchoAgentSpec, NoopAgentSpec,
        websearch.AgentSpec, webfetch.AgentSpec,
    }
    if sandbox.IsConfigured() {
        specs = append(specs, sandbox.CodeExecuteSpec, sandbox.ShellExecuteSpec)
    }
    return specs
}
```

`sandbox.IsConfigured()` 检查 `ARKLOOP_SANDBOX_BASE_URL` 是否非空。未配置时 sandbox 工具不注册（Worker 正常启动，只是没有代码执行能力）。与 Browser Service（AS-7.6）的条件注册模式一致。

#### 6.4.4 — Session 生命周期与 Worker 的协调

session_id = run_id 意味着：
- 同一 run 内的多次 tool call 共享环境（安装包后可以用）
- run 结束后需要清理 session

**Pipeline 层清理**：在 `handler_agent_loop.go` 的 executor 执行完毕后（无论成功/失败/取消），发 `DELETE {baseURL}/v1/sessions/{run_id}` 清理 sandbox session。用 `context.WithTimeout(context.Background(), 5*time.Second)` 防止清理阻塞 run 完成。

```go
// handler_agent_loop.go 末尾
if sandboxBaseURL != "" {
    cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    sandbox.CleanupSession(cleanupCtx, sandboxBaseURL, rc.Run.ID.String())
}
```

如果 Worker 崩溃导致 DELETE 未发出 → Sandbox 服务的 idle timeout（AS-6.3.2）兜底回收，不会泄漏。

**验收**：
- Agent 调用 `code_execute` 安装包 → 同 run 内再次调用 `code_execute` 使用包 → 成功。
- run 结束后 session 被清理。
- Worker 崩溃后 idle timeout 自动回收。
- Sandbox 服务未部署时，Worker 正常启动，code_execute 不出现在 tool 列表中。

### AS-6.5 — Console 监控面板（扩展路径）

**目标**：在 Console 中提供 Sandbox 运维监控页面。

**定位**：纯观察窗口 + 模板管理。Sandbox 的生命周期由 Worker tool 调用驱动，Console 不提供"手动开 Sandbox"的操作。

**不阻塞 AS-6.1~6.4，可延后执行。**

#### 6.5.1 — API 端点

Sandbox 服务暴露的 `GET /v1/stats` 已提供完整数据。API 服务新增代理端点，将 Console 请求转发到 Sandbox 服务：

```
GET /v1/admin/sandbox/stats      → 代理到 Sandbox GET /v1/stats
GET /v1/admin/sandbox/templates  → 返回 templates.json 内容
POST /v1/admin/sandbox/snapshots/{template_id}/rebuild  → 触发快照重建
```

认证：仅 admin 角色可访问（复用现有 RBAC）。

#### 6.5.2 — Console UI

在 Console 的 Infrastructure / Sandbox 下新增页面：

**概览卡片区：**
- 活跃 Sessions：N / MaxSessions
- Warm Pool 状态：各 tier 就绪数 / 目标数
- 资源消耗：总 vCPU 和内存占用（按 tier 计算）

**Session 列表：**（来自 stats 接口扩展，需 AS-6.3.4 暴露 session 列表）
- session_id、tier、创建时间、最后活跃时间、关联 run_id
- 操作：强制终止（DELETE）

**模板管理：**
- 列出所有注册的 template（id、tier、languages、快照状态、快照大小）
- 操作：重建快照（POST rebuild）
- 未来扩展：上传自定义 rootfs、创建新 template

#### 6.5.3 — 告警集成

Sandbox 服务的 `GET /healthz` 返回 `"pressure"` 状态时（active >= 80% max），通过现有的通知系统（`notifications` 表）向 admin 发送告警。

**执行顺序**：AS-6.2 → AS-6.3 → AS-6.4 → AS-6.5（AS-6.5 可延后）；AS-6.2 和 AS-6.3 可并行推进（6.3 的 WarmPool 依赖 6.2 的 SnapshotStore，但 WarmPool 的框架代码可以先写，mock SnapshotStore 测试）

---

## 9. AS-7 — Browser Service

**目标**：提供独立的无头浏览器服务，让 Agent 具备网页浏览、交互、内容提取、截图能力。服务基于 Node.js + Playwright，通过 HTTP API 暴露给 Worker 调用。

**解决的问题**：E5（Agent 无法浏览和分析网页）

**可完全独立并行推进，不依赖 AS-1~6。**

### 0. 技术选型

**结论：Node.js + Playwright，独立 HTTP 服务。**

候选方案对比：

| 维度 | Node.js + Playwright | playwright-go | chromedp | Rod |
|---|---|---|---|---|
| 浏览器支持 | Chromium/Firefox/WebKit | 同左（包装层） | Chromium only | Chromium only |
| 运行时 | Node.js（原生） | Go → 底层仍启动 Node.js 进程 | 纯 Go | 纯 Go |
| BrowserContext 隔离 | 原生支持 | 同左 | 无（手动管理） | 无原生概念 |
| storageState 导入导出 | 原生 API | 同左 | 手动实现 | 手动实现 |
| 维护状态 | Microsoft 官方维护 | 社区维护，正在找新维护者 | 稳定但低活跃 | 社区较小 |
| 已知生产问题 | 成熟 | v0.5200 data race、回调死锁、凭证泄露 | 无重大问题 | 社区较小 |
| Docker 镜像 | 官方 `mcr.microsoft.com/playwright` | 无官方镜像 | 需自建 | 需自建 |

选择 Playwright 的关键理由：

1. **playwright-go 是伪命题**。底层仍启动 Node.js Playwright 进程，通过 JSON-RPC 通信，并不消除 Node.js 依赖，反而多了一层 Go→Node.js 的 IPC 开销和社区维护风险（社区版 v0.5200 存在 data race #566、回调死锁 #574、凭证内存泄露 #556）。
2. **BrowserContext 是核心需求**。多租户 session 隔离 + cookie/localStorage 持久化需要 Playwright 的 BrowserContext 原生支持——每个 context 是独立的浏览器 profile，`storageState()` 一行代码导出/导入全部状态。chromedp 和 Rod 没有这个抽象。
3. **独立 HTTP 服务，语言无所谓**。Browser Service 和 Worker 通过 HTTP 通信，Worker 不关心它是 Node.js 还是 Go（同 OpenViking 是 Python 服务）。
4. **官方 Docker 镜像**。`mcr.microsoft.com/playwright:v1.50.0-noble` 预装 Chromium 及全部系统依赖（字体、GPU 库、sandbox 配置），省去大量基础设施工作。

### 1. 架构总览

**与 Sandbox（AS-6）的关系：完全独立，不合并。**

| 维度 | Sandbox（AS-6） | Browser（AS-7） |
|---|---|---|
| 用途 | 代码执行（Python/Shell） | 网页浏览与分析 |
| 隔离级别 | VM 级（Firecracker microVM） | 进程级（Playwright BrowserContext） |
| 技术栈 | Go + Firecracker | Node.js + Playwright |
| 状态持久化 | 快照（mem.snap + disk.snap） | storageState（cookie + localStorage） |
| 资源配额 | 按 tier（256MB~4GB） | 按 context（统一，~50MB） |
| 网络隔离 | VM 内部网络 | route 拦截内网地址 |

两个服务独立部署，各自有 pool，各自的崩溃不互相影响，也不影响 Worker。如果未来有"在 Sandbox 里生成 HTML → 用 Browser 渲染截图"的场景，通过 Worker 层编排（Worker 先调 Sandbox → 拿到 HTML → 调 Browser 渲染），不需要直接互联。

**服务拓扑：**

```
Worker
  └── BrowserToolExecutor ──HTTP──► Browser Service（Node.js + Playwright）
                                       │
                                       ├── Playwright Browser Pool（共享 Chromium 实例）
                                       │     └── BrowserContext per session（按需创建/销毁）
                                       │
                                       ├── MinIO（storageState 持久化 + 截图存储）
                                       │     ├── browser-sessions/{org_id}/{session_id}/state.json
                                       │     └── browser-screenshots/{run_id}/{step}.png
                                       │
                                       └── 网络策略（route 拦截 10.0.0.0/8 等内网地址）
```

### 2. Session 隔离模型

**核心设计：thread 级持久 session，不是 run 级一次性隔离。**

同一 thread 的多个 run 共享 browser session（cookie、localStorage 自动延续），这是最自然的行为——用户在同一个对话里让 agent 先登录再操作，期望状态是连续的。

```
Thread A:
  Run 1 → browser_navigate("https://example.com/login") → 登录，获得 cookie
  Run 2 → browser_navigate("https://example.com/dashboard") → 仍然登录状态
  Run 3 → browser_session_close() → 显式清除

Thread B:                          ← 完全隔离，看不到 Thread A 的 cookie
  Run 1 → browser_navigate("https://example.com") → 未登录状态
```

**Session 规则：**

1. **默认 session_id = thread_id**。同一 thread 的所有 run 共享同一个 browser session。Worker 在 HTTP header `X-Session-ID` 中传入 thread_id。
2. **storageState 持久化**。每次 browser 操作完成后，自动调用 `browserContext.storageState()` 导出 cookie + localStorage，存到 MinIO（`browser-sessions/{org_id}/{session_id}/state.json`）。下次同 session_id 的请求到来时，从 MinIO 加载 state 创建 BrowserContext。
3. **BrowserContext 不常驻内存**。Browser Service 不会为每个 session 保持活着的 BrowserContext：
   - 请求到来 → 从 MinIO 加载 storageState → 创建 BrowserContext → 执行操作 → 导出 storageState → 关闭 BrowserContext
   - 短时间内同 session 有连续请求（agent 多步操作）→ 保持 BrowserContext alive，idle 超时后（60s）自动关闭并持久化
4. **显式生命周期控制**。agent 可调用 `browser_session_close` 清除 session 状态；也可传 `fresh_session: true` 参数强制创建新的空白 session。
5. **跨 thread 不共享**。默认不允许跨 thread 复用 session。如果未来有需求（"继续用上次对话的浏览器"），可通过显式传 `session_id` 实现，但这是扩展路径，初期不做。

### 3. 内容展示策略

**给用户看截图，给 LLM 看结构化数据。不传 HTML/JS 让客户端渲染。**

理由：
- **安全性**：不可信网页的完整 HTML+JS 传给用户浏览器 = XSS。截图是图片，无执行风险。
- **一致性**：LLM 分析的是 Playwright 渲染后的页面状态，截图展示的也是同一个状态。传 HTML 让用户浏览器重新渲染可能因 CORS、资源加载失败、浏览器差异导致不一致。

**数据流：**

```
Browser Service 返回给 Worker：
  screenshot_url   → 存 MinIO，返回临时 URL（前端 <img> 渲染）
  page_url         → 页面实际 URL
  page_title       → 页面标题
  content_text     → 清洗后的文本（给 LLM 理解页面内容）
  accessibility_tree → 无障碍树紧凑格式（给 LLM 定位元素交互）

Worker → 前端（run event）：
  screenshot_url + page_url + page_title（用户看到截图卡片）

Worker → LLM（tool result）：
  content_text + accessibility_tree（LLM 理解页面并决定下一步操作）
```

前端利用 AS-10.B 的 segment 机制渲染为带截图的浏览器操作卡片（kind = `"browser_view"`），Lite 默认 `visible`（截图直接展示），Ultra 默认 `collapsed`（折叠，手动展开查看截图）。

### AS-7.1 — Browser Service 骨架

- **新建** `src/services/browser/`：独立 Node.js + TypeScript 服务。
- 目录结构：

```
src/services/browser/
  ├── src/
  │   ├── index.ts                  # 启动入口
  │   ├── config.ts                 # 环境变量配置
  │   ├── server.ts                 # HTTP server（node:http）
  │   ├── pool/
  │   │   ├── browser-pool.ts       # Chromium 实例池
  │   │   └── session-manager.ts    # session 生命周期管理
  │   ├── handlers/
  │   │   ├── navigate.ts           # POST /v1/navigate
  │   │   ├── interact.ts           # POST /v1/interact
  │   │   ├── extract.ts            # POST /v1/extract
  │   │   ├── screenshot.ts         # POST /v1/screenshot
  │   │   └── session.ts            # DELETE /v1/sessions/:id
  │   ├── security/
  │   │   └── network-filter.ts     # 拦截内网请求（SSRF 防护）
  │   └── storage/
  │       └── minio-client.ts       # MinIO 读写
  ├── Dockerfile
  ├── package.json
  └── tsconfig.json
```

- Dockerfile 基于 `mcr.microsoft.com/playwright:v1.50.0-noble`（预装 Chromium 及全部系统依赖）。
- HTTP 入口使用 `node:http`（不引入 Express/Fastify 等额外框架，保持轻量）。
- 健康检查：`GET /healthz`。

**验收**：`docker build` 成功，`GET /healthz` 返回 200。

### AS-7.2 — Browser Pool

Browser Service 内部的 Chromium 实例池，管理浏览器生命周期和资源：

```
Browser Pool
  ├── browsers: Chromium[]           # 1~N 个共享浏览器实例
  ├── active_contexts: Map<session_id, {context, last_active, timer}>
  └── config:
        max_browsers: 3              # 最大浏览器实例数（可配置）
        max_contexts_per_browser: 20 # 每个浏览器最大 context 数
        context_idle_timeout: 60s    # context 空闲超时，到期持久化并关闭
        context_max_lifetime: 30min  # context 最大存活时间（防内存泄漏）
```

**策略：**
- 请求到来 → 查 `active_contexts[session_id]`
  - 命中 → 复用，重置 idle timer
  - 未命中 → 从 MinIO 加载 storageState → 选负载最低的 browser 实例 → `browser.newContext({storageState})` → 放入 active_contexts
- idle timeout 触发 → `context.storageState()` 导出到 MinIO → `context.close()`
- context_max_lifetime 到期 → 强制关闭并重建（Playwright 长时间运行有内存泄漏，定期回收是最佳实践）
- 浏览器实例内存监控：单实例 RSS 超过阈值（可配置，默认 1GB）时，drain 该实例上的所有 context（持久化后关闭），销毁实例，创建新实例。

**验收**：并发 10 个不同 session 的请求，context 正确隔离；idle timeout 后 context 自动关闭。

### AS-7.3 — Session Manager（storageState 持久化）

管理 session 的 storageState 读写和生命周期：

- **MinIO 存储路径**：`browser-sessions/{org_id}/{session_id}/state.json`。
- **加载**：session 首次请求到来时，从 MinIO 读取 `state.json`（不存在 → 空白 session）。
- **保存**：每次 handler 执行完成后，调用 `context.storageState()` 写回 MinIO（覆盖写，最终一致）。
- **删除**：`DELETE /v1/sessions/:id` 删除 MinIO 上的 `state.json` 并关闭活跃 context。
- **fresh_session**：`navigate` 请求携带 `fresh_session: true` 时，忽略已有 storageState，从空白开始，操作完成后覆盖写新 state。
- **TTL**：MinIO 上的 session state 设置 lifecycle rule，超过 7 天未更新的 state 自动清理（防止无限积累）。

**验收**：session A 登录后关闭 context → 下次请求 session A 时 cookie 仍然存在；`fresh_session: true` 时 cookie 被清除。

### AS-7.4 — Handler 实现

所有 handler 共用 HTTP header 协议：

```
X-Session-ID: {thread_id}     # browser session 标识
X-Org-ID: {org_id}            # 租户隔离键
X-Run-ID: {run_id}            # 截图存储路径 + 关联
```

#### POST /v1/navigate

```json
// Request
{
  "url": "https://example.com",
  "wait_until": "networkidle",       // "load" | "domcontentloaded" | "networkidle"（默认 "load"）
  "timeout_ms": 30000,               // 导航超时（默认 30000）
  "fresh_session": false             // true = 忽略已有 storageState
}

// Response
{
  "page_url": "https://example.com/redirected",
  "page_title": "Example Domain",
  "screenshot_url": "https://minio.../browser-screenshots/{run_id}/1.png",
  "content_text": "...(清洗后的页面文本)",
  "accessibility_tree": "...(无障碍树紧凑格式)"
}
```

实现要点：
- `page.goto(url, {waitUntil, timeout})` 导航。
- 导航完成后立即 `page.screenshot()` 并上传 MinIO，返回 URL。
- `content_text` 通过 `page.innerText('body')` 提取后截断到合理长度（默认 8000 字符，可配置）。
- `accessibility_tree` 通过 `page.accessibility.snapshot()` 获取，格式化为紧凑的文本表示（减少 token 消耗）。

#### POST /v1/interact

```json
// Request
{
  "action": "click",                 // "click" | "type" | "scroll" | "select" | "hover"
  "selector": "#login-button",       // CSS selector 或 text selector（如 "text=Login"）
  "value": "",                       // type 时的输入文本 / select 时的选项值
  "coordinates": null,               // 备选：{x, y} 坐标点击（selector 和 coordinates 二选一）
  "timeout_ms": 10000
}

// Response（同 navigate 格式）
{
  "page_url": "...",
  "page_title": "...",
  "screenshot_url": "...",
  "content_text": "...",
  "accessibility_tree": "..."
}
```

实现要点：
- `click` → `page.click(selector)` 或 `page.mouse.click(x, y)`
- `type` → `page.fill(selector, value)` 或 `page.type(selector, value)`（fill 用于表单字段，type 用于逐字输入）
- `scroll` → `page.mouse.wheel(0, deltaY)` 或 `page.evaluate(() => window.scrollBy(0, 500))`
- `select` → `page.selectOption(selector, value)`
- `hover` → `page.hover(selector)`
- 交互后等待 navigation 或 network idle（自动判断），然后截图。

#### POST /v1/extract

```json
// Request
{
  "mode": "text",                    // "text" | "accessibility" | "html_clean"
  "selector": null                   // null = 整页；CSS selector = 局部提取
}

// Response
{
  "content": "...(提取的内容)",
  "word_count": 1234
}
```

- `text` → `page.innerText(selector || 'body')`
- `accessibility` → `page.accessibility.snapshot()`
- `html_clean` → `page.content()` 后清洗（去掉 script/style/svg/noscript，保留语义结构）

#### POST /v1/screenshot

```json
// Request
{
  "full_page": false,                // true = 全页截图（长图）
  "selector": null,                  // 局部截图
  "quality": 80                      // JPEG 质量（0~100，默认 80）
}

// Response
{
  "screenshot_url": "...",
  "width": 1280,
  "height": 720
}
```

#### DELETE /v1/sessions/:id

清除 session 的 storageState（MinIO 上的 state.json）、关闭活跃 context、删除关联截图。

**验收**：完整流程测试——navigate → interact(type) → interact(click) → extract → screenshot → session close。

### AS-7.5 — SSRF 防护（网络安全）

Browser Service 必须阻止 agent 通过浏览器访问内部网络。在每个 BrowserContext 创建后注册 route 拦截：

```typescript
await context.route('**/*', (route) => {
  const url = new URL(route.request().url());
  if (isBlockedTarget(url.hostname)) {
    route.abort('blockedbyclient');
    return;
  }
  route.continue();
});
```

**拦截规则：**
- `10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16`（RFC1918 私有地址）
- `169.254.0.0/16`（link-local）
- `127.0.0.0/8`、`::1`、`localhost`
- Docker 内部服务名（`postgres`、`redis`、`minio`、`openviking`、`api`、`worker`、`gateway`、`pgbouncer`）——通过配置项 `BROWSER_BLOCKED_HOSTS` 注入，默认包含 compose.yaml 中的所有服务名
- `metadata.google.internal`、`169.254.169.254`（云平台元数据服务）

**DNS 解析防护**：仅拦截 URL hostname 不够（攻击者可注册域名解析到内网 IP）。在 route handler 中对非 IP hostname 做 DNS 预解析，检查解析结果是否落入拦截范围。

**验收**：agent 尝试 `browser_navigate("http://redis:6379")` → 返回 `tool.network_blocked` 错误；尝试 `browser_navigate("http://169.254.169.254/latest/meta-data/")` → 同样被拦截。

### AS-7.6 — Worker 侧 BrowserToolExecutor

- **新建** `src/services/worker/internal/tools/browser/`：

**`spec.go`** — AgentToolSpec + LlmSpec 定义：

```go
var (
    NavigateSpec = tools.AgentToolSpec{
        Name: "browser_navigate", Version: "1",
        Description: "navigate to URL in headless browser, returns screenshot and page content",
        RiskLevel: tools.RiskLevelMedium, SideEffects: true,
    }
    InteractSpec = tools.AgentToolSpec{
        Name: "browser_interact", Version: "1",
        Description: "interact with page elements (click, type, scroll, select, hover)",
        RiskLevel: tools.RiskLevelMedium, SideEffects: true,
    }
    ExtractSpec = tools.AgentToolSpec{
        Name: "browser_extract", Version: "1",
        Description: "extract structured content from current page",
        RiskLevel: tools.RiskLevelLow, SideEffects: false,
    }
    ScreenshotSpec = tools.AgentToolSpec{
        Name: "browser_screenshot", Version: "1",
        Description: "take screenshot of current page",
        RiskLevel: tools.RiskLevelLow, SideEffects: false,
    }
    SessionCloseSpec = tools.AgentToolSpec{
        Name: "browser_session_close", Version: "1",
        Description: "close browser session and clear all state",
        RiskLevel: tools.RiskLevelLow, SideEffects: true,
    }
)
```

**`executor.go`** — 实现 `tools.Executor` 接口：

- 统一 HTTP client，调用 Browser Service 的对应 endpoint。
- session_id 取自 `ExecutionContext`（由 pipeline 注入，值为 `rc.Run.ThreadID`）。
- org_id 取自 `ExecutionContext.OrgID`。
- run_id 取自 `ExecutionContext.RunID`。
- 超时控制：`ExecutionContext.TimeoutMs` 透传到 Browser Service 的 `timeout_ms` 字段。
- 错误映射：Browser Service 返回 4xx/5xx → 映射到 `tools.ExecutionError`（`tool.browser_error`、`tool.network_blocked`、`tool.timeout`）。
- 配置：`ARKLOOP_BROWSER_BASE_URL`（如 `http://browser:3000`）。

**`executor.go` 中 session_id 获取方式**：需要扩展 `tools.ExecutionContext`，增加 `ThreadID` 字段（或从 `RunID` 查 DB 获取关联 thread，但前者更干净）。在 `pipeline/mw_tool_build.go` 中注入 `rc.Run.ThreadID` 到 `ExecutionContext`。

**LlmSpec 示例（navigate）：**

```go
var NavigateLlmSpec = llm.ToolSpec{
    Name:        "browser_navigate",
    Description: stringPtr("navigate to a URL in headless browser, returns screenshot and page content"),
    JSONSchema: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "url":           map[string]any{"type": "string", "format": "uri"},
            "wait_until":    map[string]any{"type": "string", "enum": []string{"load", "domcontentloaded", "networkidle"}},
            "fresh_session": map[string]any{"type": "boolean"},
        },
        "required":             []string{"url"},
        "additionalProperties": false,
    },
}
```

**注册到 builtin：**

- **修改** `src/services/worker/internal/tools/builtin/builtin.go`：将 browser 工具的 AgentSpec/LlmSpec/Executor 加入 `AgentSpecs()`、`LlmSpecs()`、`Executors()` 返回值。
- browser 工具仅在 `ARKLOOP_BROWSER_BASE_URL` 非空时注册（Browser Service 不部署时，Worker 正常启动，只是没有 browser 工具）。

**验收**：Browser Service 运行时，skill allowlist 包含 `browser_navigate` 的 agent 可以成功调用；Browser Service 未部署时，Worker 正常启动，browser 工具不出现在 tool 列表中。

### AS-7.7 — compose.yaml 接入 + 集成测试

- **修改** `compose.yaml`：新增 `browser` 服务：

```yaml
browser:
  build:
    context: .
    dockerfile: src/services/browser/Dockerfile
  restart: unless-stopped
  environment:
    BROWSER_PORT: "3000"
    BROWSER_MAX_BROWSERS: "${ARKLOOP_BROWSER_MAX_BROWSERS:-2}"
    BROWSER_MAX_CONTEXTS_PER_BROWSER: "${ARKLOOP_BROWSER_MAX_CONTEXTS:-20}"
    BROWSER_CONTEXT_IDLE_TIMEOUT_S: "60"
    BROWSER_CONTEXT_MAX_LIFETIME_S: "1800"
    BROWSER_MINIO_ENDPOINT: "minio:9000"
    BROWSER_MINIO_ACCESS_KEY: "${ARKLOOP_S3_ACCESS_KEY:-minioadmin}"
    BROWSER_MINIO_SECRET_KEY: "${ARKLOOP_S3_SECRET_KEY}"
    BROWSER_MINIO_BUCKET_SESSIONS: "browser-sessions"
    BROWSER_MINIO_BUCKET_SCREENSHOTS: "browser-screenshots"
    BROWSER_BLOCKED_HOSTS: "postgres,redis,minio,openviking,api,worker,gateway,pgbouncer"
  ports:
    - "${ARKLOOP_BROWSER_PORT:-3000}:3000"
  depends_on:
    minio:
      condition: service_healthy
  healthcheck:
    test: ["CMD-SHELL", "curl -fsS http://127.0.0.1:3000/healthz || exit 1"]
    interval: 10s
    timeout: 5s
    retries: 3
```

- **修改** Worker 服务环境变量：增加 `ARKLOOP_BROWSER_BASE_URL: "http://browser:3000"`。
- 集成测试：`docker compose up` → Worker 调用 `browser_navigate` → 验证截图存入 MinIO、storageState 持久化。

**执行顺序**：AS-7.1 → AS-7.2 → AS-7.3 → AS-7.4 → AS-7.5（可与 7.4 并行）→ AS-7.6 → AS-7.7

---

## 10. AS-8 — Cost Budget 执行侧

**目标**：Loop 内实时追踪 token 消耗，超出 `ToolBudget` 中定义的预算时主动终止 run。

**解决的问题**：F5

**依赖**：AS-1（budget 逻辑在 Executor 里实现）

### AS-8.1 — RunContext 加 token 消耗追踪

- **修改** `src/services/worker/internal/pipeline/context.go`：增加 `TokenBudget *int64`（从 Skill budgets 或 AgentConfig 解析）。
- **修改** `src/services/worker/internal/pipeline/mw_skill_resolution.go`：从 `def.Budgets.ToolBudget["max_tokens"]` 读取并写入 `rc.TokenBudget`。

### AS-8.2 — SimpleExecutor 内 token 消耗检查

- **修改** `src/services/worker/internal/executor/simple.go`（以及 InteractiveExecutor）：
  - 每轮 LLM response 后累计 `input_tokens + output_tokens`。
  - 若 `rc.TokenBudget != nil` 且累计消耗 >= budget：emit `run.failed`（error_class: `agent.token_budget_exceeded`），终止循环。

**验收**：单测：设置 budget=100 tokens，mock gateway 每轮返回 60 tokens，第二轮应触发终止。

---

## 11. AS-9 — Sub-agent Spawning

**目标**：Ultra Agent 能在 Loop 内调度子 Agent，子 run 完成后结果注入父 run 的 tool result。

**解决的问题**：F4

**依赖**：AS-1、AS-3（父子关系 + interrupt 机制）

### AS-9.1 — DB schema：runs 加 parent_run_id

- **新建 migration**：`ALTER TABLE runs ADD COLUMN parent_run_id uuid REFERENCES runs(id)`。
- nullable，普通 run 为 NULL，子 run 指向父 run ID。

### AS-9.2 — spawn_agent Tool

- **新建** `src/services/worker/internal/tools/spawn_agent/executor.go`：实现 `tools.Executor`。
- 工具参数：`{"skill_id": "agent-lite", "input": {...}}`。
- 执行逻辑：
  1. 创建子 run（设置 `parent_run_id = rc.Run.ID`）。
  2. 同步等待子 run 完成（通过 LISTEN/NOTIFY 或轮询）。
  3. 返回子 run 的 `completed` 事件 data 作为 tool result。
- 超时：由 `rc.ToolTimeoutMs` 控制，子 run 超时则取消并返回 error。

### AS-9.3 — Console / API 补充子 run 关系展示

- `GET /v1/runs/{run_id}` 返回中增加 `parent_run_id`、`child_run_ids`。
- 前端 run 详情页展示 agent 树结构（Ultra 特有）。

**执行顺序**：AS-9.1 → AS-9.2 → AS-9.3

---

## 12. AS-10 — Thinking 展示协议

**目标**：建立后端到前端的统一 thinking 渲染协议，分两个独立子轨道：

- **子轨道 A（Bug 修复）**：修复 LLM 原生 thinking 内容混入主输出的问题，通过 `channel` 字段在 Gateway 层正确分离。
- **子轨道 B（新能力）**：引入 `run.segment.start/end` 事件对，让 Executor 能主动向前端声明内容块的渲染策略（折叠/展开/隐藏）。

**解决的问题**：F7、E6

**依赖**：子轨道 A 完全独立；子轨道 B 的后端实现依赖 AS-1（Executor 才能调用 emitter 发 segment 事件），前端实现可独立并行。

---

### 子轨道 A — LLM 原生 Thinking Channel 分离

#### AS-10.A1 — Anthropic 适配器：识别并路由 thinking blocks

- **修改** `src/services/worker/internal/llm/anthropic.go`，`parseAnthropicMessage`：
  - 遇到 `type == "thinking"` 的 content block，从 `item["thinking"].(string)` 取出文本。
  - 单独返回 `thinkingText string`（与主 `content string` 分开）。
- **修改** `Stream` 方法：若 `thinkingText != ""`，先发一个 `channel: "thinking"` 的 `StreamMessageDelta`，再发主内容的 `StreamMessageDelta`（无 channel）。

```go
thinkingChannel := "thinking"
if thinkingText != "" {
    if err := yield(StreamMessageDelta{
        ContentDelta: thinkingText,
        Role:         "assistant",
        Channel:      &thinkingChannel,
    }); err != nil {
        return err
    }
}
```

#### AS-10.A2 — OpenAI chatCompletions 适配器：识别 `<think>` 标签

- **修改** `src/services/worker/internal/llm/openai.go`，streaming delta 处理路径：
  - 在累积 content delta 时，识别 `<think>` / `</think>` 边界（简单状态机，两个布尔 flag：`inThink`）。
  - `inThink == true` 的 delta 发 `channel: "thinking"` 的 `StreamMessageDelta`。
  - `inThink == false` 的 delta 发无 channel 的 `StreamMessageDelta`（正常路径）。
- 同样处理 `responses` API 路径（o1/o3 的 `reasoning_content`）。

#### AS-10.A3 — 前端：message.delta channel 过滤

- **修改** `src/apps/web/src/components/ChatPage.tsx:200-204`：

```typescript
if (event.type === 'message.delta') {
  const obj = event.data as { content_delta?: unknown; role?: unknown; channel?: unknown }
  if (obj.role != null && obj.role !== 'assistant') continue
  if (obj.channel === 'thinking') {
    // 暂时丢弃，AS-10.B3 完成后改为 setThinkingDraft
    continue
  }
  if (typeof obj.content_delta !== 'string' || !obj.content_delta) continue
  setAssistantDraft((prev) => prev + obj.content_delta)
  continue
}
```

**验收**：使用支持 thinking 的模型时，主对话气泡只显示正式回复，thinking token 不出现在气泡里。

---

### 子轨道 B — Agent 段落事件与前端折叠渲染

#### AS-10.B1 — Go contract 加 segment 事件类型

- **修改** `src/services/worker/internal/llm/contract.go`，新增两个类型：

```go
// 段落开始：后续事件直到 SegmentEnd 属于此段落
type StreamSegmentStart struct {
    SegmentID string
    Kind      string  // "thinking" | "planning_round" | "direction_check" | "tool_group"
    Display   SegmentDisplay
}

type SegmentDisplay struct {
    Mode  string  // "visible" | "collapsed" | "hidden"
    Label string  // 前端折叠块标题
}

// 段落结束
type StreamSegmentEnd struct {
    SegmentID string
}
```

- **修改** `handler_agent_loop.go` 的 `streamingEventTypes` 白名单，加入 `"run.segment.start"`、`"run.segment.end"`。
- **修改** `loop.go` 的 `StreamEvent` 处理 switch，加对应 case，发 `run.segment.start` / `run.segment.end` 事件。

#### AS-10.B2 — Executor 端的使用方式

Executor 代码里通过 emitter 直接发 segment 事件，不需要新接口：

```go
// InteractiveExecutor 或 SimpleExecutor 内部
segID := uuid.NewString()
rc.Emitter.Emit("run.segment.start", map[string]any{
    "segment_id": segID,
    "kind":       "planning_round",
    "display":    map[string]any{"mode": "collapsed", "label": "第 1 轮规划"},
}, nil, nil)

// ... 正常执行 Loop 一轮 ...

rc.Emitter.Emit("run.segment.end", map[string]any{"segment_id": segID}, nil, nil)
```

这不是新的接口，就是已有 emitter 发自定义事件。`kind` 的取值由 Executor 自己决定，前端按 kind 选样式。

#### AS-10.B3 — 前端：segment 渲染组件

- **新建** `src/apps/web/src/components/ThinkingBlock.tsx`：可折叠块，接收 `kind`、`label`、`mode`、内部事件列表作为 props。
- **修改** `ChatPage.tsx`：
  - 维护 `currentSegment` 状态（当前活跃的 segment ID + kind）。
  - 收到 `run.segment.start`：开始缓冲该 segment 的事件。
  - 收到 `run.segment.end`：提交 segment 到渲染列表。
  - `mode === "visible"` → 直接展开。
  - `mode === "collapsed"` → 渲染为可点击的 `<ThinkingBlock>`，默认折叠。
  - `mode === "hidden"` → 不渲染（事件仍写 DB）。
- Lite 的浏览器操作：segment kind = `"browser_view"`，mode = `"visible"`（截图直接展示）。
- Ultra 的浏览器操作和方向校验：mode = `"collapsed"`（默认折叠，手动展开查看截图）。

**验收**：
- Ultra 跑多轮时，每轮规划显示为可折叠的"第 N 轮规划"块，点击展开可看到工具调用链。
- Lite 调浏览器工具时，截图卡片直接内嵌显示。
- 方向校验块默认折叠，不干扰主对话流。

---

**执行顺序**：
```
子轨道 A：AS-10.A1 → AS-10.A2 → AS-10.A3（完全独立，Bug 修复优先）
子轨道 B：AS-1 完成后 → AS-10.B1 → AS-10.B2（后端）
                            AS-10.B3（前端，可与 B1/B2 并行）
```

---

## 13. AS-11 — Tool Provider 管理

**目标**：将同名工具的多个后端（web_search 的 SearXNG/Tavily、web_fetch 的 Jina/Firecrawl/Basic）显式注册为独立 Provider，支持 per-org 激活指定 Provider 并在 Console 里配置参数（API Key、Base URL）。

**解决的问题**：E7

**依赖**：完全独立，可与其他所有 AS 并行推进。

---

### AS-11.1 — AgentToolSpec 加 LlmName 字段 + 多后端注册

**概念模型**：
- **Tool Group**：以 `LlmName` 为键，是 LLM 看到的工具名（如 `web_search`）。一个 Group 内只有一个 Provider 在 run 时生效。
- **Provider**：具体实现，内部注册名（如 `web_search.tavily`）。`AgentToolSpec.Name` 是 Provider 键，`AgentToolSpec.LlmName` 是其所属 Group。

修改 `src/services/worker/internal/tools/spec.go`：
```go
type AgentToolSpec struct {
    Name    string    // 内部 Provider 键，allowlist 和 executor 绑定用此名
    LlmName string    // Tool Group 名，传给 LLM。空 → 与 Name 相同（向后兼容）
    // ...其余字段不变
}
```

修改 `tools/dispatch_executor.go`：
- `Bind()` 时若 `spec.LlmName != ""` 则建立反向索引 `llmNameIndex[spec.LlmName] = internalName`
- `Execute()` 时若 `executors[toolName]` 未找到，查 `llmNameIndex[toolName]` 后再找

修改 `pipeline/helpers.go` 的 `FilterToolSpecs`：
- allowlist 含 `web_search.tavily` → 发给 LLM 的 spec 用 `LlmName = "web_search"`（去重）

修改 `tools/builtin/web_search/executor.go`：
```go
// 保留向后兼容的 AgentSpec（无 LlmName，用 env var 选后端）
var AgentSpec = tools.AgentToolSpec{Name: "web_search", ...}

// 新增显式 Provider spec
var AgentSpecSearxng = tools.AgentToolSpec{
    Name: "web_search.searxng", LlmName: "web_search", ...}
var AgentSpecTavily = tools.AgentToolSpec{
    Name: "web_search.tavily", LlmName: "web_search", ...}
```

同样拆分 `web_fetch`：`web_fetch.jina`、`web_fetch.firecrawl`、`web_fetch.basic`，LlmName 均为 `web_fetch`。

**验收**：
- Skill YAML 写 `tool_allowlist: [web_search.tavily]`，LLM 收到的 tool spec 名为 `web_search`，执行时走 Tavily executor。
- Skill YAML 写 `tool_allowlist: [web_search]`（旧格式），行为与今天一致（env var 控制后端）。

---

### AS-11.2 — DB Schema：per-org Provider 激活与凭证存储

API key 等敏感值复用现有 `secrets` 表（与 `llm_credentials`、`asr_credentials`、`mcp_configs` 一致），不存入 `config_json`。

新建 migration：

```sql
CREATE TABLE tool_provider_configs (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    group_name    text NOT NULL,        -- "web_search" / "web_fetch"（LlmName）
    provider_name text NOT NULL,        -- "web_search.tavily"（AgentToolSpec.Name）
    is_active     boolean NOT NULL DEFAULT false,
    secret_id     uuid REFERENCES secrets(id) ON DELETE SET NULL,  -- API key 加密存储
    key_prefix    text,                 -- 前端展示用（如 "tvly-****1234"），不含完整密钥
    base_url      text,                 -- 自定义 endpoint（SearXNG、自部署 Firecrawl 等）
    config_json   jsonb NOT NULL DEFAULT '{}',  -- 仅非敏感参数（语言、超时等）
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE(org_id, provider_name)
);
-- 应用层保证：同一 org + group_name 最多一条 is_active = true
CREATE INDEX ON tool_provider_configs (org_id, group_name) WHERE is_active = true;
```

各 Provider 的字段使用：
| Provider | secret_id（API Key） | base_url | config_json |
|---|---|---|---|
| `web_search.tavily` | Tavily API Key | - | - |
| `web_search.searxng` | - | SearXNG 实例地址 | 语言等 |
| `web_fetch.jina` | Jina API Key | - | - |
| `web_fetch.firecrawl` | Firecrawl API Key | 自部署地址（可选） | - |
| `web_fetch.basic` | - | - | - |

Worker 读取时复用现有路径：`LEFT JOIN secrets ON s.id = c.secret_id` + `crypto.DecryptGCM`（同 `routing/config.go:423`）。

---

### AS-11.3 — Worker Pipeline：从 DB 注入 per-org Provider

新建 `src/services/worker/internal/pipeline/mw_tool_provider.go`：

- 在 Pipeline 靠前位置（MCPDiscovery 之后、ToolBuild 之前）插入。
- 从 DB 查询 `tool_provider_configs LEFT JOIN secrets WHERE org_id = rc.Run.OrgID AND is_active = true`，解密 API key。
- 对每条激活记录，用解密后的 key + base_url 构建对应的 Provider（覆盖 `rc.ToolExecutors[providerName]`）。
- 若 DB 无记录，不做任何事，回落到已有 env var 逻辑（backward compat）。

读取逻辑需要缓存（内存 TTL 或复用 MCP Discovery 的缓存策略），避免每个 run 都查 DB。

---

### AS-11.4 — Console API：Tool Provider 管理接口

新增接口（挂在现有 API 服务的 `/v1/tool-providers` 路径下）：

| 方法 | 路径 | 描述 |
|---|---|---|
| `GET` | `/v1/tool-providers` | 列出所有 Tool Groups 及其 Provider 状态（含是否激活、是否已配置） |
| `PUT` | `/v1/tool-providers/{group}/{provider}/activate` | 激活指定 Provider（原子操作：deactivate 同 group 内其他） |
| `PUT` | `/v1/tool-providers/{group}/{provider}/credential` | 写入 API Key（存入 secrets 表加密）、Base URL |
| `DELETE` | `/v1/tool-providers/{group}/{provider}/credential` | 删除凭证，回退到 env var |

Response 中 API Key 只返回 `key_prefix`（如 `"tvly-****1234"`），不返回完整值，与 `llm_credentials` 接口一致。

---

### AS-11.5 — Console UI：Tool Provider 管理页

在 Console 的 Settings / Tools 下新增管理页，布局：

```
web_search
  ● web_search.tavily    [激活]  API Key: tvly-****1234  [编辑] [停用]
  ○ web_search.searxng   [未激活]  Base URL: (未配置)  [配置] [激活]

web_fetch
  ● web_fetch.jina       [激活]  API Key: jina-****abcd  [编辑] [停用]
  ○ web_fetch.firecrawl  [未激活]  API Key: (未配置)  [配置] [激活]
  ○ web_fetch.basic      [无需配置]                   [激活]
```

规则：
- Group 内同时只有一个 Provider 激活，切换时自动停用当前激活项。
- 配置表单字段由各 Provider 静态声明（需要 API Key / Base URL / 无需配置），Console 后端注册各 Provider 的字段列表。
- 未配置但被激活的 Provider → run 时报 `tool.not_configured`，Console 里显示警告状态。
- 凭证写入走 `secrets` 表加密路径，与 LLM 凭证管理页完全一致的交互模式。

**执行顺序**：AS-11.1 → AS-11.2 → AS-11.3（后端全通路）→ AS-11.4 → AS-11.5（Console 层）

---

## 14. AS-12 — 可扩展性与性能基线

**目标**：在核心服务（Browser、Sandbox、Memory、MCP Pool、Worker）交付完成后，建立各服务的单节点容量上限，明确横向扩展路径，并通过基线压测验证架构不存在隐性瓶颈。

**依赖**：AS-4、AS-5、AS-6、AS-7 均已完成。

**不做什么**：本 Phase 不优化已知性能。基线的目的是定位瓶颈在哪，具体优化是后续迭代的事。

---

### AS-12.1 — Browser Service 横向扩展路径

Browser Service 是三个服务中状态最重的（BrowserContext 按 sessionId 挂载在进程内存中）。单节点天花板约 30-75 个并发 session（受 Chromium 内存限制，非代码瓶颈）。

多节点部署时，同一 sessionId 的请求必须路由到持有该 Context 的节点，否则需要从 MinIO 重新加载 storageState。两个可选方案，在本 Sub-phase 内做决策并文档化：

**方案 A — Session Affinity（低延迟，强一致）**
- 网关层按 sessionId 哈希到固定节点（例如 Nginx `hash $session_id consistent`）
- 节点宕机时由网关重路由，新节点从 MinIO 加载 storageState 冷恢复
- 适合 session 复用率高、页面交互密集的场景

**方案 B — Stateless Mode（无状态，弹性强）**
- 每次请求：load storageState → 创建临时 Context → 执行 → persist → 销毁
- 无需 affinity，任意节点可处理任意请求
- 单次请求延迟 +50-200ms（MinIO IO），但可无限横向扩
- 适合请求频率低、对延迟不敏感的场景

**交付物**：
- 在 `src/services/browser/README.zh-CN.md` 中记录选定方案及理由
- 方案 A：在 `compose.yaml` 补充 Nginx session affinity 示例配置
- 方案 B：`BrowserPool` 新增 stateless 模式（`BROWSER_STATELESS=true`），每次请求强制 `fresh_session=true`，执行后立即 `expireContext`

### AS-12.2 — Sandbox 多节点调度接口

当前 Sandbox 是单节点 HTTP 服务，`MaxSessions=50` 是硬上限（受宿主机 vCPU 限制）。Sandbox 本身无状态（快照在 MinIO，VM 一次性），适合横向扩展，但 Worker 侧当前是硬编码 HTTP 调用，扩展路径被堵死。

**方案**：在 Worker 侧抽象 `SandboxClient` 接口，将节点选择逻辑封装在实现层：

```go
// internal/sandbox/client.go
type Client interface {
    Acquire(ctx context.Context, tier string) (SessionHandle, error)
    Execute(ctx context.Context, handle SessionHandle, req ExecRequest) (ExecResult, error)
    Release(ctx context.Context, handle SessionHandle) error
}
```

- 单节点实现：直接调用已有 `sandbox` HTTP API
- 多节点实现（不在本 Phase 实现，但接口兼容）：Worker 持有 N 个 HTTP 端点，Acquire 时轮询各节点 `/health/stats` 取负载最低节点（best-of-k）

**交付物**：
- `src/services/worker/internal/sandbox/client.go`：接口定义 + 单节点实现
- `src/docs/architecture/sandbox-scaling.zh-CN.md`：多节点扩展路径文档

### AS-12.3 — MCP Pool 运行时指标暴露

AS-4 完成了 DB 查询缓存和健康检查/重连。但当前无运行时可观测性，无法判断连接池是否成为瓶颈。

- 在 MCP Pool 上新增 `Stats()` 方法，返回：活跃连接数、缓存命中/未命中计数、重连次数、挂起请求数
- 通过 Worker 的 `/health/stats` 端点聚合输出（与 Sandbox 的 stats 接口格式对齐）
- 暴露为结构化 JSON，便于 Prometheus scrape 或 Console 展示

### AS-12.4 — OpenViking 容量基线

OpenViking 是 Python HTTP 服务，单实例并发能力受 GIL + 向量检索 CPU 开销限制，需要实测而非估算。

**压测内容**：
- 并发检索：50/100/200 并发的 `/api/v1/search/find` 请求，记录 P50/P99 延迟和错误率
- 并发写入：模拟记忆提炼路径（`/api/v1/content/write`），验证底层向量索引无锁竞争导致的性能崩溃

**交付物**：
- 压测脚本（k6 或 Go test）放在 `src/services/worker/internal/memory/bench_test.go`
- 单实例 QPS 上限记录在 OpenViking 的部署文档中
- 如果底层向量存储不支持并发写，明确 single-writer 架构约束

### AS-12.5 — Worker DB 连接池配置暴露

Worker 是 Go 服务，goroutine 本身不是瓶颈，但 AgentLoop 每次迭代都持有 DB 连接（通过 pipeline 中间件），高并发下 DB 连接池先到达上限。

- 确认 `database/sql` 的 `SetMaxOpenConns` / `SetMaxIdleConns` 是否已通过环境变量暴露；若硬编码则改为可配置
- 压测：并发 50 个 run，观察 DB 连接池使用率、P99 请求排队时间
- 将推荐配置写入 `compose.yaml` 注释（`ARKLOOP_DB_MAX_OPEN_CONNS` 等）

**执行顺序**：AS-12.1、AS-12.2、AS-12.4、AS-12.5 可并行；AS-12.3 依赖 AS-4 完成。所有 Sub-phase 均不依赖 AS-9/10/11。

---

## 15. 整体执行编排

```
并行轨道 1（核心架构，有依赖链）：
  AS-1.1 → AS-1.2 → AS-1.4 → AS-1.5   ← 必须最先完成，不宜中断
           ↓
  AS-2.1 → AS-2.2
           ↓
  AS-3.1 → AS-3.2 → AS-3.3 → AS-3.4
           ↓
  AS-3.5.1（可与 AS-3.5.2 并行）
  AS-3.5.2 → AS-3.5.3 → AS-3.5.4
           ↓
  AS-5.0~5.4（已完成）→ AS-5.5 → AS-5.6 → AS-5.7 → AS-5.8
           ↓
  AS-8.1 → AS-8.2
           ↓
  AS-9.1 → AS-9.2 → AS-9.3
           ↓（AS-1 完成后）
  AS-10.B1 → AS-10.B2（后端 segment 事件）

并行轨道 2（独立，完成后并入轨道 1）：
  AS-1.3（ClassifyRouteExecutor）

并行轨道 3（完全独立）：
  AS-4.1 → AS-4.2

并行轨道 4（独立服务）：
  AS-6.2 → AS-6.3 → AS-6.4 → AS-6.5（AS-6.1 已完成；AS-6.5 可延后）

并行轨道 5（独立服务）：
  AS-7.1 → AS-7.2 → AS-7.3 → AS-7.4 → AS-7.5 → AS-7.6 → AS-7.7

并行轨道 6（Bug 修复，完全独立，最高优先）：
  AS-10.A1 → AS-10.A2 → AS-10.A3

并行轨道 7（前端，与轨道 6 同步或独立）：
  AS-10.B3（ThinkingBlock 组件，不依赖 AS-1）

并行轨道 8（完全独立，穿越后端+Console）：
  AS-11.1 → AS-11.2 → AS-11.3 → AS-11.4 → AS-11.5

并行轨道 9（AS-4/5/6/7 完成后启动，各 Sub-phase 可并行）：
  AS-12.1（Browser 横向扩展路径）
  AS-12.2（Sandbox 多节点接口）
  AS-12.3（MCP Pool 指标，依赖 AS-4）
  AS-12.4（OpenViking 容量基线）
  AS-12.5（Worker DB 连接池）
```

**关键路径**：AS-1 是所有其他 AS 的地基。AS-4、AS-6、AS-7、AS-11 完全独立，可以与 AS-1 同步推进。AS-5（OpenViking）需要等 AS-1 完成后在 Pipeline 里接入。**AS-10.A 是 Bug 修复，应最先完成，不阻塞其他轨道**。AS-12 是在核心服务全部交付后启动的压后轨道，不阻塞任何功能上线。

---

## 16. 不变量与决策记录

以下决策在本 Roadmap 内固定，不再重复讨论：

- **Sandbox 独立服务**：Worker 不直接执行不可信代码；Sandbox 和 Worker 通过 HTTP 协议通信；Sandbox 崩溃不影响 Worker。session_id = run_id（同一 run 内多次 tool call 共享环境）；run 结束后 Worker 显式清理 session，idle timeout 兜底回收。
- **Sandbox 技术路线**：直接上 Firecracker（KVM 环境已满足）；MinIO 存储快照（复用已有基础设施）；不走 gVisor 过渡。Template = rootfs + kernel + tier 的组合；快照恢复优先，冷启动降级。Warm Pool 按 tier 分池预热。
- **Sandbox Console 定位**：纯运维观察窗口 + 模板管理。不提供手动开 Sandbox 的操作，生命周期完全由 Worker tool 调用驱动。AS-6.5 为扩展路径，不阻塞核心功能上线。
- **Executor 注册表**：新增 Agent 类型 = 写 YAML + 可选新增 Go 文件 + 调用 `Register`，不修改 Loop 或 Pipeline。
- **Lite/Pro/Ultra 是 Research，不是架构**：架构只提供 executor_type 钩子、preferred_credential 绑定、tool_allowlist 约束。具体 prompt/model/tool 的选择是调优问题，不进入 Roadmap。
- **Memory 降级策略**：`MemoryProvider` 为 nil 时 Memory 功能静默关闭（middleware no-op + tool 不注册），run 正常执行，不报错。
- **Memory 是 Tool，不是录制器**：Agent 通过 memory_search/read/write/forget 主动读写记忆，编排层（Lua）通过 binding 直接调用。middleware 的自动注入/归档是辅助，不是唯一路径。写入的内容是结构化知识（facts/preferences/entities），不是原始对话文本。Session 归档保留，作为记忆来源之一。
- **记忆提炼由 LLM 驱动**：run 结束后用轻量 LLM 从 tool 输出中提取值得长期记忆的结构化知识点，不是简单地把 tool result 存进去。成本控制：只在 tool call >= 2 或对话轮数 >= 3 时触发，使用最便宜的模型，distill prompt 控制在 2000 token 以内。
- **OpenViking 部署方式**：独立 Python HTTP 服务，Go 通过 `MemoryProvider` 接口调用，OpenViking 自身配置独立管理。
- **Human-in-the-loop 通信机制**：复用已有 Postgres LISTEN/NOTIFY 路径，不引入新的消息队列。
- **Model 优先级链固定**：显式 route_id > Skill.preferred_credential > AgentConfig.Model > Default，不允许其他插入点。
- **Sub-agent 层级限制**：嵌套深度 ≤ 2，超限返回 `agent.max_depth_exceeded`。
- **Thinking 渲染协议**：`channel: "thinking"` 用于 LLM 原生 thinking 分流（不渲染进气泡）；`run.segment.start/end` 用于 Agent 级别的执行过程分组（折叠/展开/隐藏）；前端按 `kind` 选样式，后端不传 CSS 类名。Lite 的浏览器操作默认 `visible`（截图直接展示），Ultra 的规划轮和方向校验默认 `collapsed`（手动展开）。
- **Browser Service 独立部署**：Node.js + Playwright 独立 HTTP 服务，Worker 通过 `BrowserToolExecutor` 调用，崩溃不影响 Worker。session 按 thread_id 隔离，storageState（cookie + localStorage）持久化到 MinIO。BrowserContext 不常驻内存，idle 超时后自动持久化并关闭。SSRF 防护通过 Playwright route 拦截内网地址。不传 HTML/JS 给前端渲染，只传截图 + 结构化文本。
- **Tool Provider 双名机制**：`AgentToolSpec.Name` 是 Provider 键（allowlist 和 executor dispatch 用）；`AgentToolSpec.LlmName` 是 Group 名（LLM 看到的工具名）。同 Group 内 per-org 只允许一个 Provider 激活，应用层保证互斥。API Key 等敏感值走 `secrets` 表加密存储（与 LLM 凭证同一路径），不存 `config_json`；env var 是系统级默认，DB 配置优先。
- **Lua Executor 技术选型**：使用 gopher-lua（纯 Go Lua 5.1 解释器，无 CGO）。Lua 只描述编排逻辑，不实现工具；所有 tool 执行、LLM 调用、DB 写入均在 Go binding 层。每个 Run 独立 LState，不共享，无锁。
- **子 Run 等待机制**：父 Run 挂起时通过 Redis Pub/Sub 等待，不持有 DB 连接。子 Run 完成事件发布到 `run.child.{id}.done` channel，payload 含最终输出文本。父子 Run 均完整写入 run_events，Console 按 parent_run_id 聚合展示。
- **Skill 绑定 AgentConfig**：Skill 的 `agent_config` 字段（名称）覆盖继承链解析结果；nil 则走原有 thread → project → org → platform 链。绑定在 mw_skill_resolution.go 内完成，mw_agent_config.go 先于其执行，skill 覆盖后不回退。
