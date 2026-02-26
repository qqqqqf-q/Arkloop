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
- Playground ToolExecutor 适配层（Playground 服务已有，缺 Worker 侧接入）

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

Sandbox（代码执行隔离）和 Worker 之间的关系没有明确边界。Sandbox 崩溃、逃逸是否会影响 Worker？是否应该是独立服务？与 Playground（browser 工具）的关系如何隔离？

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

**E5 — Playground ToolExecutor 适配**

Playground 服务已开发完成，但 Worker 侧没有对应的 `ToolExecutor` 实现将其接入 `ToolRegistry`。Playground 的工具（browser、enhanced web search 等）需要通过 HTTP 协议适配到现有 `tools.Executor` 接口，配置和 MCP Remote HTTP 类似，但协议可能不同（取决于 Playground 服务的 API 设计）。

**E6 — Thinking 渲染的两层问题**

第一层是 Gateway 合规性问题：`StreamMessageDelta.Channel` 已定义但从未填充——Anthropic 非流式路径丢弃 thinking blocks，OpenAI chatCompletions 流式路径不处理 `<think>` 标签，导致 thinking 内容要么消失要么混入主输出。第二层是前端渲染问题：`message.delta` channel 字段从未被读取，`run.segment.start/end` 事件不存在，前端对 Lite 的 Playground 内嵌窗口和 Ultra 的默认隐藏无法区分——这两种 display 策略需要后端主动声明。

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
| AS-5 | Memory System | MemoryProvider 接口 + OpenViking 适配器 + Pipeline 接入 + compose.yaml 服务 | 顺序 | AS-1 |
| AS-6 | Sandbox 服务 | Firecracker microVM + Sandbox Controller（warm pool）+ MinIO 快照 + SandboxToolExecutor | 独立（可并行启动） | 无 |
| AS-7 | Playground 接入 | PlaygroundToolExecutor 适配 Playground 服务 HTTP API，注册到 Worker ToolRegistry | 独立（可并行） | 无 |
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
  - session space：`viking://session/{user_id}/{session_id}/...`
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

Worker 侧新增 env：
- `ARKLOOP_OPENVIKING_BASE_URL`（Docker 内建议 `http://openviking:1933`）
- `ARKLOOP_OPENVIKING_ROOT_API_KEY`（与 `ov.conf.server.root_api_key` 一致；未启用 auth 时可为空）

`ov.conf` 至少需要：
- `storage.agfs.path=/app/data`
- `storage.vectordb.path=/app/data`
- `embedding`/`vlm`/`rerank` 的 provider 配置（否则 find/search/commit 可能无法工作）

### AS-5.3 — OpenViking 适配器（HTTP Client）

- **新建** `src/services/worker/internal/memory/openviking/client.go`：通过 OpenViking HTTP API 实现 `MemoryProvider`。
- ROOT key 路线的 HTTP 请求头：
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

**OpenViking 高级能力的暴露方式**：`ls`、`read`、`abstract`、`overview` 等原生能力，后续可作为独立 Agent Tool 注册到 `ToolRegistry`，让 Agent 主动查询上下文库，而不仅限于被动注入。这是 AS-5 的扩展路径，不是初期必要项。

**执行顺序**：AS-5.0 → AS-5.1 → AS-5.2 → AS-5.3 → AS-5.4

---

## 8. AS-6 — Sandbox 服务

**目标**：实现代码执行隔离环境，支持 Agent 运行 shell/Python 代码，崩溃不影响 Worker。

**技术选型**：直接上 Firecracker microVM（宿主机 KVM 环境已满足），不走 gVisor 过渡。Firecracker 提供 VM 级隔离（独立内核），快照能力支持 < 1s resume，适合高并发 Agent 场景。

**解决的问题**：A4、F3

**可完全独立并行推进，不依赖 AS-1~5。**

### AS-6.1 — Sandbox 服务骨架 + Firecracker 接入

- **新建** `src/services/sandbox/`：独立 Go 服务。
- 提供 HTTP 接口：`POST /v1/exec`（执行代码），`DELETE /v1/sessions/{id}`（释放 session）。
- 内部使用 Firecracker microVM：每个 session 对应一个 microVM 实例，崩溃不影响其他 session 或 Worker。
- 资源限制按 Tier 配置：

```go
type ExecRequest struct {
    SessionID string
    Tier      string // "lite" | "pro" | "ultra"
    Language  string // "python" | "shell"
    Code      string
    TimeoutMs int
}
```

Tier 对应资源配额（`lite`: 256MB/0.5CPU, `pro`: 1GB/1CPU, `ultra`: 4GB/2CPU）。

### AS-6.2 — Firecracker 快照 + MinIO

- 首次启动某 template（如 Python 基础环境）：microVM 就绪后打快照（`mem.snap` + `disk.snap`），上传至 MinIO `sandbox-snapshots/{template_id}/`。
- 后续同 template session：从 MinIO 下载快照 → Firecracker resume（< 1s），大幅减少冷启动开销。
- 复用已有 `compose.yaml` 中的 MinIO 实例（`ARKLOOP_S3_ACCESS_KEY` 等配置）。

### AS-6.3 — Sandbox Controller（Warm Pool）

- 服务内维护 warm pool：预先 resume N 个空 microVM，按 Tier 分池。
- `Acquire(runID, tier) → SessionHandle`：从对应 Tier pool 取一个，不足时从快照 resume 补充。
- `Release(runID)`：销毁 microVM，异步补充新的到 warm pool。
- 调度算法（横向扩展时）：best-of-k，从 k 个随机节点中选负载最低的，不引入 Kubernetes。

### AS-6.4 — SandboxToolExecutor 接入 Worker

- **新建** `src/services/worker/internal/tools/sandbox/executor.go`：实现 `tools.Executor`。
- 通过 HTTP 调用 Sandbox 服务 `/v1/exec`。
- 注册工具名：`code_execute`、`shell_execute`。
- 配置：`ARKLOOP_SANDBOX_BASE_URL`。

**执行顺序**：AS-6.1 → AS-6.2 → AS-6.3 → AS-6.4（AS-6.2 和 AS-6.3 可并行）

---

## 9. AS-7 — Playground 接入

**目标**：将已开发完成的 Playground 服务（browser、增强 web search 等工具）接入 Worker 的 `ToolRegistry`，让 Agent 可以调用。

**解决的问题**：Playground 服务存在但 Worker 侧缺少适配层。

**可完全独立并行推进。**

### AS-7.1 — 确认 Playground HTTP API 接口约定

- 与 Playground 服务约定工具调用接口（request/response 格式、认证方式、错误格式）。
- Playground 服务按 run 隔离：每次调用携带 `run_id`，Playground 负责资源清理。

### AS-7.2 — PlaygroundToolExecutor

- **新建** `src/services/worker/internal/tools/playground/executor.go`：实现 `tools.Executor`。
- 通过 HTTP 调用 Playground 服务，适配到 `tools.Executor` 协议。
- 注册工具名：`browser_navigate`、`browser_click`、`browser_extract` 等（按 Playground 实际暴露的工具）。
- 配置：`ARKLOOP_PLAYGROUND_BASE_URL`。

**执行顺序**：AS-7.1 → AS-7.2

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
- Lite 的 Playground 内嵌窗口：segment kind = `"playground_view"`，mode = `"visible"`（直接铺开）。
- Ultra 的 Playground 和方向校验：mode = `"collapsed"`（默认折叠，手动展开）。

**验收**：
- Ultra 跑多轮时，每轮规划显示为可折叠的"第 N 轮规划"块，点击展开可看到工具调用链。
- Lite 调 Playground 时，浏览器窗口直接内嵌显示。
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

## 14. 整体执行编排

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
  AS-5.0 → AS-5.1 → AS-5.2 → AS-5.3 → AS-5.4
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
  AS-6.1 → AS-6.2 → AS-6.3 → AS-6.4

并行轨道 5（独立适配）：
  AS-7.1 → AS-7.2

并行轨道 6（Bug 修复，完全独立，最高优先）：
  AS-10.A1 → AS-10.A2 → AS-10.A3

并行轨道 7（前端，与轨道 6 同步或独立）：
  AS-10.B3（ThinkingBlock 组件，不依赖 AS-1）

并行轨道 8（完全独立，穿越后端+Console）：
  AS-11.1 → AS-11.2 → AS-11.3 → AS-11.4 → AS-11.5
```

**关键路径**：AS-1 是所有其他 AS 的地基。AS-4、AS-6、AS-7、AS-11 完全独立，可以与 AS-1 同步推进。AS-5（OpenViking）需要等 AS-1 完成后在 Pipeline 里接入。**AS-10.A 是 Bug 修复，应最先完成，不阻塞其他轨道**。

---

## 15. 不变量与决策记录

以下决策在本 Roadmap 内固定，不再重复讨论：

- **Sandbox 独立服务**：Worker 不直接执行不可信代码；Sandbox 和 Worker 通过 HTTP 协议通信；Sandbox 崩溃不影响 Worker。
- **Sandbox 技术路线**：直接上 Firecracker（KVM 环境已满足）；MinIO 存储快照（复用已有基础设施）；不走 gVisor 过渡。
- **Executor 注册表**：新增 Agent 类型 = 写 YAML + 可选新增 Go 文件 + 调用 `Register`，不修改 Loop 或 Pipeline。
- **Lite/Pro/Ultra 是 Research，不是架构**：架构只提供 executor_type 钩子、preferred_credential 绑定、tool_allowlist 约束。具体 prompt/model/tool 的选择是调优问题，不进入 Roadmap。
- **Memory 降级策略**：`MemoryProvider` 为 nil 时 Memory 功能静默关闭，run 正常执行，不报错。
- **OpenViking 部署方式**：独立 Python HTTP 服务，Go 通过 `MemoryProvider` 接口调用，OpenViking 自身配置独立管理。
- **Human-in-the-loop 通信机制**：复用已有 Postgres LISTEN/NOTIFY 路径，不引入新的消息队列。
- **Model 优先级链固定**：显式 route_id > Skill.preferred_credential > AgentConfig.Model > Default，不允许其他插入点。
- **Sub-agent 层级限制**：嵌套深度 ≤ 2，超限返回 `agent.max_depth_exceeded`。
- **Thinking 渲染协议**：`channel: "thinking"` 用于 LLM 原生 thinking 分流（不渲染进气泡）；`run.segment.start/end` 用于 Agent 级别的执行过程分组（折叠/展开/隐藏）；前端按 `kind` 选样式，后端不传 CSS 类名。Lite 的 Playground 默认 `visible`（内嵌窗口），Ultra 的规划轮和方向校验默认 `collapsed`（手动展开）。
- **Tool Provider 双名机制**：`AgentToolSpec.Name` 是 Provider 键（allowlist 和 executor dispatch 用）；`AgentToolSpec.LlmName` 是 Group 名（LLM 看到的工具名）。同 Group 内 per-org 只允许一个 Provider 激活，应用层保证互斥。API Key 等敏感值走 `secrets` 表加密存储（与 LLM 凭证同一路径），不存 `config_json`；env var 是系统级默认，DB 配置优先。
- **Lua Executor 技术选型**：使用 gopher-lua（纯 Go Lua 5.1 解释器，无 CGO）。Lua 只描述编排逻辑，不实现工具；所有 tool 执行、LLM 调用、DB 写入均在 Go binding 层。每个 Run 独立 LState，不共享，无锁。
- **子 Run 等待机制**：父 Run 挂起时通过 Redis Pub/Sub 等待，不持有 DB 连接。子 Run 完成事件发布到 `run.child.{id}.done` channel，payload 含最终输出文本。父子 Run 均完整写入 run_events，Console 按 parent_run_id 聚合展示。
- **Skill 绑定 AgentConfig**：Skill 的 `agent_config` 字段（名称）覆盖继承链解析结果；nil 则走原有 thread → project → org → platform 链。绑定在 mw_skill_resolution.go 内完成，mw_agent_config.go 先于其执行，skill 覆盖后不回退。
