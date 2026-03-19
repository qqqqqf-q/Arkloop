---
title: ACP Provider 接入架构设计（OpenCode 首实现）
description: Arkloop 接入 ACP Provider 的修订设计稿。保留 Desktop 与 SaaS 双路线，OpenCode 作为第一个落地 provider。
sidebarLabel: ACP Provider 架构
order: 123
---

# ACP Provider 接入架构设计（OpenCode 首实现）

本文给出 Arkloop 接入 ACP Provider 的修订方案。目标不是把 ACP 当作另一套 MCP，也不是继续把 ACP 绑定成 sandbox 的专属功能，而是把「ACP session runtime + host backend + 现有 Worker」串起来。OpenCode 作为第一个落地 provider，但不再被视为最终唯一实现。

结论先行：

- `ACP` 在 Arkloop 中应定义为 **代码 agent 接入层**，不是 sandbox 的附属功能。
- `ACP` 的一等对象是 **session**，不是 provider command；provider 只负责“谁来跑”，host 只负责“在哪跑”。
- 第一阶段 provider 仍然以 `OpenCode` 为主，启动命令直接使用 `opencode acp`。
- Desktop 使用 **Local Process Host**：直接在用户机器上拉起 provider command，认证和模型配置交给 provider 自己处理。
- SaaS 使用 **Sandbox Process Host**：在 sandbox 内托管 provider command，按需注入 LLM Proxy / session token，保护真实 API key。
- `ACP` 在第一阶段主要承担 **ensure session、run turn、流式更新、取消、关闭**；不追求一次性实现完整 `fs/*` 与 `terminal/*`。
- `MCP` 继续作为工具层存在，不与 ACP 平级竞争；第一阶段不要求把 Arkloop 全量 MCP 能力注入给 OpenCode。
- Arkloop 不额外维护第二条交互 shell；无论 Desktop 还是 SaaS，都优先使用 **非 PTY** 的 ACP 通道。
- Worker / Frontend / RunEvent / `acp_agent` Tool 共用同一套控制面；host 可以不同，但上层协议与事件模型保持一致。

---

## 0. 修正说明（2026-03）

当前仓库中的 ACP 实现已经完成第一批基础设施，但文档与运行时语义仍需要一次收敛：

- 已完成：sandbox ACP 子进程托管、sandbox service 会话管理、Worker ACP bridge、`acp_agent` 工具、`acp_status` 与会话复用基础。
- 已落地但未完全闭环：LLM Proxy API 端点与 token 结构已存在，但 Worker 运行态仍需补齐完整 wiring 和预算治理。
- 已完成但文档过时：运行时已支持 `local` / `sandbox` 两类 host，`acp_agent` 入参已从 `agent` 收敛为 `provider`。
- 当前最重要的修正：从“provider-first”继续收敛到“session-first”。也就是把 ACP 的主抽象从“启动哪个命令”改为“如何确保、驱动、取消、恢复一个 ACP session”。
- 当前最重要的验证缺口：真实 provider contract 仍需持续校准，尤其是 `cancel` / `permission` / `status` 等控制方法不能只靠文档猜测。
- 当前实现约束：在真实 contract 校准完成前，Worker 默认不把 `session/cancel` 与 `session/permission` 当稳定支持面；`cancel` 退化到 host/process stop，`permission_request` 只做观测与失败上报。

---

## 0.1 术语定锚

后续所有实现与评审都以以下术语为准：

- `session`：一次持续存在的 ACP 代码工作会话。它承载上下文、工作目录、agent 状态以及后续多次 turn 的连续性。
- `turn`：向已有 ACP session 发送的一次任务或提示。一个 `acp_agent` tool call 本质上对应一次 turn，而不是整个 session。
- `process`：承载 ACP 会话的底层命令进程或后端进程。它可能重启、替换，但不应直接等同于业务 session。
- `runtime handle`：Arkloop 持有的会话句柄，用于后续 `run turn`、`status`、`cancel`、`close`、`resume`。
- `runtime session key`：Arkloop 在 Worker 进程内定位一个可复用 runtime handle 的键，不等于 ACP 协议里的 `sessionId`。
- `provider`：具体 ACP agent 实现及其启动配置，例如 `acp.opencode`。它回答“谁来跑”。
- `host`：provider command 的承载位置，例如 `local` 或 `sandbox`。它回答“在哪跑”。

一句话总结：

- `provider` 解决“谁来跑”
- `host` 解决“在哪跑”
- `session` 解决“这次代码代理工作流本身如何持续、恢复、取消、复用”

## 1. 背景与问题定义

Arkloop 当前已经具备三类相关能力：

- Worker 具备完整的 RunEngine、工具编排与 MCP 发现链路
- Sandbox 具备持久化 shell、workspace 与环境同步能力
- 前端与 API 具备 Thread / Run / Event 这一套统一模型

这为接入 coding agent 提供了基础，但还缺少一块稳定拼图：

- 当前 sandbox 主要暴露 Arkloop 自身 action，不是面向代码代理的会话协议
- MCP 只解决“工具如何接入”，不解决“代码代理如何被托管、如何与 workspace 交互”
- 若直接把 OpenCode 当 shell 命令塞进现有交互 PTY，会把协议流、进程流与终端流混在一起，后续很难维护

因此需要新增一层明确边界：

- Arkloop 负责 **任务编排与治理**
- OpenCode 负责 **代码代理执行**
- Sandbox 负责 **工作环境**
- ACP 负责 **Arkloop 与 OpenCode 的会话通信**

---

## 2. 设计目标

### 2.1 目标

- 让 Arkloop 能把 code-heavy 任务委托给 OpenCode 执行
- 让 OpenCode 作为第一个 ACP provider，在 Desktop 与 SaaS 两条线路都能接入
- 让 provider 在各自 host 中直接读写 workspace、执行测试和命令
- 保持 Arkloop 现有 Thread / Run / Event 模型不变
- 避免为首版引入过多新协议面和双重 authority
- 为 Desktop 与 SaaS 保留两条 host backend，但共用同一套 ACP 控制面
- 让后续接入 Codex CLI、Claude Agent、Gemini CLI 等 provider 时，不需要重写 Worker 主链路

### 2.2 非目标

- 不在第一阶段实现完整 ACP 全能力兼容
- 不在第一阶段重构 Arkloop 全部 MCP 子系统
- 不在第一阶段做 IDE 级文件细粒度授权
- 不在第一阶段一次性完成多个 provider 的产品化接入，但 provider 规格必须预留多实现
- 不把 OpenCode 设计成 MCP Server

---

## 3. 核心判断

### 3.1 ACP 不是 MCP 的替代品，但在 Arkloop 中通过 Tool 暴露

两者职责不同：

- `ACP`：代码编辑器 / 上层控制面 与 coding agent 之间的**会话协议**，管理完整的 agent session 生命周期
- `MCP`：agent 可调用的工具、资源与提示词协议，每次调用是无状态的函数调用

因此正确关系是：

- `ACP` 在上层，负责会话（session ensure / new、prompt turn、终态观测、cancel、close）
- `MCP` 在下层，负责工具（单次函数调用）

在 Arkloop 中，ACP 通过 `acp_agent` 工具暴露给 LLM。这不是把 ACP “硬塞成一组伪工具调用”  `acp_agent` 是一个正式的 builtin tool，内部封装了完整的 ACP Bridge，管理 session 创建、prompt 发送、update 轮询与结果收集。LLM 看到的是一次工具调用，Bridge 内部处理的是一段完整的 ACP 会话。

关键区分：

- `acp_agent` tool：LLM 发起委托 -> Bridge 管理完整 session -> 返回结果
- 普通 MCP tool：LLM 发起调用 -> 执行单个函数 -> 返回结果

### 3.2 第一阶段不需要手写 file runtime，但必须抽出 host 边界

当 provider 与 `workspace` 共置于同一个 host 时：

- Desktop 路线下，provider 直接使用用户本机文件系统与工具链
- SaaS 路线下，provider 直接使用 sandbox 内文件系统与工具链
- Arkloop 不需要先实现 ACP `fs/read_text_file`、`fs/write_text_file` 才能跑通首版

这意味着第一阶段的 ACP 可以做减法：

- 必须：`session/new`、`session/prompt`、`session/update`、`session/cancel`
- 可选：权限确认、mode 同步
- 暂缓：`fs/*`、`terminal/*`、`loadSession`

### 3.3 不要再开第二条 shell，也不要把 host 写死为 sandbox

Arkloop 不应再为 provider 额外维护一条“给 agent 用的交互 shell”。

更合适的模型是：

- Desktop：一个本地 provider 进程
- SaaS：一个 sandbox session + 一个 provider 子进程
- 一条 ACP over stdio 的控制通道
- provider 自己在 host 内拉起需要的命令进程

这样可以避免：

- PTY prompt 噪音污染 ACP 流
- shell marker 与 JSON 协议串流互相干扰
- 两套终端上下文不一致
- 多层锁与生命周期错位

### 3.4 ACP 在 Arkloop 中是一个 Tool

这是实现阶段确立的核心决策：**ACP 不是一个独立的执行器路径，而是 LLM 可调用的工具**。

原始设计假设 RunEngine 根据 persona/mode 配置将 run 路由到 `agent.acp` 执行器，绕过 LLM 直接进入 code-agent 路径。实现中发现这不符合 Arkloop 的编排哲学：

- Arkloop LLM 是编排者/领导者，OpenCode 是专业工人
- LLM 应该自主判断何时、以什么任务描述委托给 code agent
- 与 `web_search`、`exec_command` 等工具的调用方式一致

因此实际实现为：

- 工具名：`acp_agent`
- 参数：`task`（必填，任务描述）、`provider`（可选，默认 `acp.opencode`）
- 从 LLM 的 tool 接口视角看，它表现为一次独立调用；但这次调用命中并驱动的是一个可复用 session
- 内部由 ACP Bridge 管理完整的 session 生命周期（创建 -> prompt -> 轮询 update -> 收集结果）

这意味着不存在 "executor mode"（`agent.acp`），不需要修改 RunEngine 路由逻辑，不需要 persona 配置决定执行路径。LLM 自己决定。

---

## 4. 总体架构

```text
LLM Agent Loop
  │
  ├─ tool call: web_search(...)
  ├─ tool call: exec_command(...)
  │
  └─ tool call: acp_agent(task="...", provider="acp.opencode")
       │
       ▼
  ACP Runtime (session ensure, provider resolution, event mapping)
       │
       ├─ Desktop: Local Process Host
       │      ▼
       │   OpenCode / Codex / Claude / Gemini command
       │
       └─ SaaS: Sandbox Process Host
              ▼
         Sandbox ACP endpoints (acp_start, acp_write, acp_read, acp_stop, acp_wait)
              ▼
         OpenCode process (ACP over stdio)
```

完整调用链：

1. LLM 在 agent loop 中决定调用 `acp_agent(task="实现用户认证模块", provider="acp.opencode")`
2. `acptool/executor.go` 解析 provider、host 与 session key
3. ACP Runtime 先 `ensure session`，再确定当前 turn 运行在哪个 host
4. Runtime 启动或复用 provider command，例如 `opencode acp`
5. Bridge 向该 session 发送 turn
6. Bridge 轮询 `session/update`，将 update 映射为 `run_events`
7. 收集终态结果，返回给 LLM

### 4.1 关键分层

| 层 | 职责 |
|----|------|
| Frontend | 展示 run 过程、终态结果、取消与错误 |
| LLM Agent Loop | 决定何时委托任务给 code agent |
| acp_agent Tool | 封装 ACP Bridge 调用，对 LLM 屏蔽 session 细节 |
| ACP Runtime | 解析 session、provider、host，并管理 session 生命周期 |
| ACP Bridge | 管理协议收发、发送 prompt、聚合 update |
| Local Process Host | Desktop 场景下直接拉起本地 provider command |
| Sandbox Process Host | SaaS 场景下通过 sandbox endpoint 托管 provider |
| OpenCode | 第一个落地 provider |
| MCP | 可选工具层，由 OpenCode 或 Arkloop 使用 |

---

## 5. 运行模型

### 5.1 会话创建

当 LLM 在 agent loop 中调用 `acp_agent(task="...", provider="acp.opencode")` 时：

1. `acptool/executor.go` 解析 provider 与 host，并计算当前 turn 命中的 session key
2. 解析 provider preset，确定启动命令（默认 `opencode acp --cwd <workspace>`）
3. 若 session 已存在，则绑定已有 runtime handle；若不存在，则创建新的 ACP session
4. Desktop 下由 Local Process Host 启动本地 command；SaaS 下由 Sandbox Process Host 启动子进程
5. Bridge 持有该 session 对应的 stdio 通道，为后续 turn 建立通信

这里要特别区分三层标识：

- `runtime session key`：Worker 内部缓存与复用 runtime handle 的 key。当前实现固定定义为 `run_id + provider_id + host_kind`
- `process id`：host 返回的底层子进程标识
- `protocol session id`：provider 在 `session/new` 后返回的 ACP `sessionId`

三者不能混用。后续所有实现都以这三层分离为前提。

### 5.2 prompt 执行

一次 `acp_agent` 工具调用的完整流程：

1. LLM 决定调用 `acp_agent(task="...", provider="acp.opencode")`
2. Tool executor 解析 session key、host 与 provider command
3. Runtime 确保当前 session 可用；必要时启动新的 ACP agent 进程
4. Bridge 向该 session 发送一个 turn
5. OpenCode 在对应 host 内执行（读取代码、修改文件、运行命令、生成总结）
6. Bridge 轮询 update，并以当前 turn 的明确终态是否已可观察为准继续或结束
7. Bridge 将中间 update 映射为 `run_events`，收集终态结果，返回给 tool executor
8. Tool executor 将结果返回给 LLM

### 5.3 取消与超时

Worker 保持现有 run 级治理不变：

- 触发取消时，只有在真实 contract 已显式校准后才尝试对当前 ACP session 发标准 `session/cancel`
- 若标准 cancel 未校准、发送失败、或发送后仍未观察到 turn 停止，则退化为 host/process 级 stop
- `run.cancelled` 只能在底层 turn 已停止，或 host process 已停止之后发出；不能先宣称 cancelled 再让底层继续跑
- `cancel` 与 `close` 语义分开：`cancel` 终止当前 turn，`close` 回收整个 session
- SaaS 下 sandbox session 的生命周期继续由 Sandbox Service 统一管理

### 5.4 Session-first 原则

从实现视角看，Arkloop 后续所有 ACP 代码都应遵守以下顺序：

1. 解析要命中的 ACP session
2. 确保 session 已存在并拿到 runtime handle
3. 在该 session 上发送本次 turn
4. 接收 update / status
5. 按需执行 cancel / close / resume

不推荐继续以“先启动 provider command，再顺手缓存一下复用”作为核心语义。复用只是优化，session 才是主抽象。

---

## 6. ACP 采用面

### 6.1 第一阶段采用的 ACP 子集

第一阶段只依赖这几个核心能力：

- `session/new`
- `session/prompt`
- 能观察当前 turn 的明确终态
- `session/cancel`（仅在真实 provider contract 已校准时启用）

如 OpenCode 实现要求，可追加：

- `session/set_mode`

当前明确不作为稳定支持面的能力：

- `session/permission`
- 任何未校准即默认开启的自动批准逻辑

### 6.2 第一阶段暂缓的 ACP 能力

以下能力不作为接入前置：

- `fs/read_text_file`
- `fs/write_text_file`
- `terminal/create`
- `terminal/output`
- `session/load`
- 更复杂的 embedded context

原因很直接：

- 这些能力主要服务于“agent 与 workspace 分离”的部署方式
- 当前首版采用 `OpenCode` 与 workspace 共置于 sandbox 的方案，不需要先走 client 提供的文件与终端 authority

---

## 7. Sandbox 内的子进程模型

### 7.1 为什么不能复用现有 PTY Shell

现有 shell controller 面向交互式命令会话，适合：

- `exec_command`
- `write_stdin`
- transcript / marker / timeout 控制

但 `opencode acp` 是机器协议进程，要求：

- 干净的 stdin/stdout
- 无 shell prompt
- 无 marker
- 无 TTY 逃逸序列

因此应新增专用的“协议子进程”通道，而不是把 ACP 进程塞进现有 PTY shell。

### 7.2 建议新增的 guest action

在 `src/services/sandbox/agent/` 侧新增一组最小 action：

- `acp_start`
- `acp_write`
- `acp_read`
- `acp_stop`
- `acp_wait`
- `acp_debug_snapshot`（可选）

它们的职责是：

- 启动 `opencode acp`
- 维护 stdin / stdout / stderr / pid
- 以 cursor 方式返回增量输出
- 支持 Worker 中断、回收与排障

### 7.3 非 PTY 模式

建议使用普通 `pipe`，不分配 PTY：

- `stdin`：Arkloop 写入 ACP 请求
- `stdout`：Arkloop 读取 ACP 响应与 update
- `stderr`：用于日志与错误定位

这样本地 sandbox 与云端 sandbox 都能复用同一套实现。

---

## 8. OpenCode 与文件/终端的关系

### 8.1 文件系统

第一阶段中，OpenCode 是 sandbox 内的本地进程，因此：

- 直接读写 workspace
- 不要求 Arkloop 提供额外 file tool
- 不要求 ACP 先实现 `fs/*`

这也是本方案与“通用 ACP client 实现”的主要区别：

- 通用实现强调 client 侧 authority
- Arkloop 第一阶段强调 sandbox 内 co-located 执行

### 8.2 命令执行

OpenCode 直接在 sandbox 内执行命令：

- `git status`
- `go test ./...`
- `pnpm build`
- `rg` / `find` / `sed`

Arkloop 不需要额外给它提供第二条 terminal。

### 8.3 锁与并发

第一阶段采用简单且稳定的治理策略：

- 同一 sandbox code session 在任一时刻只允许一个活跃 prompt turn
- workspace 只有一个主写入者：当前 OpenCode session
- Arkloop 主 Agent 负责调度，不与 OpenCode 并行直接改同一份代码

这足以覆盖首版需求，避免过早引入文件锁与细粒度冲突控制。

---

## 9. 与 MCP 的关系

### 9.1 角色关系

本方案中：

- `OpenCode` 不是 MCP Server
- `ACP` 不是 MCP 的上位替身
- `MCP` 依然是工具层

### 9.2 第一阶段的 MCP 策略

第一阶段不要求把 Arkloop 现有 MCP 全量接入 OpenCode。建议按“最小可用”原则推进：

- Arkloop 主 Agent 继续保有现有 MCP 能力
- OpenCode 先只依赖本地 workspace 与本地命令
- 若确有收益，再按白名单注入少量 MCP server

适合第二阶段注入的 MCP 类型：

- sandbox 内可直启的 `stdio` MCP server
- sandbox 网络可访问的 `streamable_http` MCP server

不建议第一阶段直接做的事情：

- 把 Worker 侧所有 MCP executor 直接映射给 OpenCode
- 让多个 AI tool 同时拥有 workspace 写权限

### 9.3 MCP 升级范围

MCP 可以同步做小步升级，但不应阻塞 ACP 接入：

- `streamable_http` 作为默认远程传输
- 协议版本跟进到较新的 MCP 规范
- 补齐必要的授权与工具元数据

这是一条并行优化线，不是 OpenCode 首版接入的前置条件。

---

## 10. Desktop 与 SaaS 双路线

### 10.1 Desktop 路线

Desktop 不应强依赖 sandbox，也不应默认依赖 LLM Proxy：

- 直接拉起本地 provider command，例如 `opencode acp`
- provider 自己决定认证方式、模型选择与本地配置
- OpenCode 实测可通过 terminal auth 暴露 `opencode auth login`
- Desktop 只负责 session、事件流、权限边界与 UI 承载

实测记录（2026-03-19）：

- `opencode` 版本：本地命令可用，`opencode acp --help` 可正常返回
- ACP 启动命令：`opencode acp`
- 直接发送 `session/new` 可返回 `sessionId`
- 当前 contract 仍需继续校准：`session/cancel` 不能只按文档猜测控制方法，未校准前必须退化到 host/process stop
- 当前 contract 仍需继续校准：`session/permission` 不能默认自动批准，未校准前只能把 `permission_request` 视为观测信号而非可回写协议面

实现注意事项：

- provider 启动时可能读取用户本机配置和插件
- Desktop 接入应提供可选的干净配置目录或专用 provider profile，避免启动被插件更新阻塞

### 10.2 SaaS 路线

SaaS 路线必须保留，但职责要收敛为“受控 host + 安全治理”：

- 一样是 sandbox session
- 一样启动 `opencode acp`
- 一样通过 stdio 说 ACP
- 一样由 Worker 负责 run / event / cancel
- 仅在 SaaS 路线下注入 LLM Proxy / 临时 token，保护真实 API key

### 10.3 共享控制面

Desktop 与 SaaS 不需要共用同一个 host，但必须共用同一个控制面：

- 统一的 provider 规格
- 统一的 ACP Bridge 与事件映射
- 统一的 `acp_agent` Tool 语义
- 统一的 session / cancel / resume 抽象

---

## 11. 对现有代码的影响点

### 11.1 Sandbox

重点新增：

- `src/services/sandbox/agent/`：ACP 子进程 action 与管理逻辑
- `src/services/sandbox/internal/`：如有必要，抽出通用的 agent-process manager
- `src/services/sandbox/cmd/sandbox/`：接入 session 生命周期与回收治理

### 11.2 Worker

重点新增：

- `src/services/worker/internal/acp/`：ACP 传输层
  - `protocol.go`：ACP 协议类型定义（session/new, session/prompt, session/update 等）
  - `client.go`：ACP 客户端，封装 sandbox ACP endpoint 调用
  - `bridge.go`：ACP Bridge，管理完整 session 生命周期
- `src/services/worker/internal/tools/builtin/acptool/`：`acp_agent` builtin tool
  - `spec.go`：工具定义（AgentSpec + LlmSpec）
  - `executor.go`：工具执行器（sandbox 校验、Bridge 创建、session 运行、结果收集）
- 在 `builtin.go` 中注册 `acp_agent` 工具
- 不修改 RunEngine，不新增 executor 路由

### 11.3 Frontend / API

首版只要求：

- 展示 event stream
- 展示终态结果
- 支持取消

不要求首版就做：

- 独立的文件树
- 细粒度 diff 面板
- 终端实时 pane

---

## 12. 风险与边界

### 12.1 进程稳定性

`opencode acp` 是长驻子进程，需要处理：

- 启动失败
- 协议崩溃
- stdout 卡死
- 长时间无响应

因此必须在 sandbox-agent 和 Worker 两侧都具备超时与强杀策略。

### 12.2 版本兼容

ACP 与 OpenCode 的 CLI 版本会演进，因此要把以下信息写入能力探测：

- `opencode` 版本
- ACP 支持能力
- 启动参数支持情况
- 控制方法与事件格式的真实 contract

### 12.4 协议漂移风险

当前最需要避免的错误不是“host 选错”，而是“在未验证真实 provider contract 的情况下把协议写死进 Worker”。

必须区分三件事：

- 文档上写的 ACP 方法名
- 当前桥接层假设的方法名
- 真实 provider 实测支持的方法名

只有三者一致，才允许把控制方法固化为稳定 contract。否则应先通过验证脚本或集成测试收敛。

当前仓库中的落地方式：

- Worker `bridge` 层显式区分 `runtime_session_key`、`process_id`、`protocol_session_id`
- `acptool` 只缓存 worker 进程内的 runtime handle，不把它当作 ACP session 的持久事实来源
- 新增可选真实 contract 测试，使用 `ARKLOOP_RUN_ACP_CONTRACT=1` 显式开启，对本机 `opencode acp` 做最小链路校验
- contract test 的成功标准不是“stdout 上有任意 update”，而是“能观察到本次 turn 的明确终态”

### 12.3 权限与凭证

即便第一阶段不做复杂 file API，仍要控制：

- sandbox 内可见凭证范围
- workspace 边界
- 网络访问能力
- agent 可执行命令范围

这部分继续复用现有 sandbox 治理即可。

---

## 13. 演进路径

### 阶段一：最小闭环

- sandbox 内启动 `opencode acp`
- Worker 通过 ACP 发送 prompt
- OpenCode 直接读写 workspace、直接运行命令
- Arkloop 展示 update / result / cancel

### 阶段二：增强治理

- 增加权限确认
- 增加更稳定的 stderr / debug 采样
- 注入少量 MCP server
- 增加 session 复用与恢复

### 阶段三：抽象复用层

当出现以下需求时，再抽统一 Code Runtime：

- 接入第二个 code agent
- UI 需要 client-side 文件 authority
- 本地 / 云端 / IDE 需要统一 file / terminal 面
- 需要 ACP `fs/*` 与 `terminal/*` 的标准兼容

此时再抽象，不会阻塞第一阶段交付。

---

## 14. 最终结论

Arkloop 接入 OpenCode 的第一阶段，最简单且最稳的方案是：

- OpenCode 直接运行在 sandbox 内
- Arkloop 通过 ACP over stdio 与它通信
- 不额外开第二条 shell
- 不在首版强行实现完整 file runtime
- MCP 保持工具层角色，按需注入

换句话说，第一阶段不是“造一个通用代码工作站协议层”，而是先打通一条最短闭环：

- Arkloop 负责编排
- OpenCode 负责编码
- Sandbox 负责环境
- ACP 负责会话

先把这条链打通，再决定何时抽更通用的 Code Runtime。

---

## 15. 实施 Roadmap（按 PR 拆分）

以下路线图按“可独立合并的 PR”颗粒度组织，目标是让 OpenCode + ACP 的第一阶段能稳定落地，同时避免把 MCP 重构、通用 file runtime 和多 agent 兼容层绑成一个超大改动。

### PR-1：文档与能力边界固化

目标：把方案定稿，避免后续实现阶段在“ACP 是不是只做 prompt”“要不要第二条 shell”“MCP 和 ACP 怎么分层”上反复摇摆。

交付：

- 新增 OpenCode + ACP 架构设计文档
- 将实施路线图并入设计文档
- 文档站侧栏接入
- 明确第一阶段采用 `co-located sandbox + ACP over stdio`

验收标准：

- 文档站可浏览
- 技术判断清晰：不做第二条 shell，不把 OpenCode 做成 MCP Server

### PR-7.0.1：Session-first 文档收敛与协议校准

目标：在继续推进 Desktop 与更多 provider 之前，把 `PR-7` 的核心抽象从 `provider-first` 收敛为 `session-first`，并补齐真实 provider contract 说明。

范围：

- 统一术语：
  - `session`
  - `turn`
  - `process`
  - `runtime handle`
  - `provider`
  - `host`
- 将文档中的 `agent` 参数全部收敛为 `provider`
- 补充 session 生命周期：
  - `ensure/init`
  - `run turn`
  - `status`
  - `cancel`
  - `close`
  - `resume/reuse`
- 新增真实 provider contract 记录与验证要求，避免继续按猜测写控制方法
- Worker 代码层同步收敛命名：
  - `runtime session key`
  - `process id`
  - `protocol session id`
- 增加显式开启的真实 `opencode acp` contract test，至少覆盖：
  - `session/new`
  - `session/prompt`
  - update / prompt result 的真实可观测形态

验收标准：

- 文档明确 `session` 是一等对象，`provider` 退回 backend config 层
- 文档与当前实现入参语义一致
- 代码中不再混用 run 级 session key、host process id 与 ACP protocol session id
- 真实 contract 校验不再依赖猜测；未验证的方法不宣称稳定支持
- 任何后续 `PR-7.1+` 都不再以“先讨论 provider command 配置”作为主线，而是以 session lifecycle 为主线

### PR-2：Sandbox 增加 ACP 子进程托管能力

目标：在 sandbox 内稳定启动并托管 `opencode acp`，但暂不接入 Worker。

范围：

- `src/services/sandbox/agent/` 新增 ACP 子进程管理逻辑
- 新增 action：
  - `acp_start`
  - `acp_write`
  - `acp_read`
  - `acp_stop`
  - `acp_wait`
- 使用 `pipe` 而不是 PTY
- 对 `stdout`、`stderr`、exit status 做基本缓存与治理

验收标准：

- 可以在 sandbox 内成功启动 `opencode acp --cwd <workspace>`
- 可以写入一条 ACP 请求并读取响应
- 可以在超时后停止子进程

### PR-3：Sandbox Service 接入 ACP 会话句柄

目标：把 guest 内 ACP 子进程包装成 sandbox service 可管理的会话对象。

范围：

- `src/services/sandbox/internal/` 增加 agent-process manager 或等价抽象
- 将 ACP 子进程生命周期绑定到 sandbox session
- 暴露 service 级接口：
  - `StartACPAgent`
  - `WriteACP`
  - `ReadACP`
  - `StopACPAgent`
  - `WaitACPAgent`
- 增加 session 回收、空闲超时、异常退出清理

验收标准：

- Sandbox Service 能独立管理 ACP 进程生命周期
- session 释放时不会残留孤儿进程

### PR-4：Worker 新增 ACP Bridge 传输层（已完成）

目标：建立 Worker 与 sandbox 内 ACP agent 的通信层。

范围：

- `src/services/worker/internal/acp/protocol.go`：ACP 协议类型定义
- `src/services/worker/internal/acp/client.go`：ACP 客户端，封装 sandbox endpoint 调用
- `src/services/worker/internal/acp/bridge.go`：ACP Bridge，管理 session 生命周期
- 支持最小 ACP turn 链路；控制方法需以真实 provider contract 为准持续校准

验收标准：

- Bridge 能通过 sandbox endpoint 与 ACP agent 通信
- 支持 session 创建、prompt 发送、update 轮询、取消

### PR-5：`acp_agent` Builtin Tool（已完成）

目标：让 LLM 能通过工具调用将任务委托给 ACP coding agent。

范围：

- `src/services/worker/internal/tools/builtin/acptool/spec.go`：
  - `AgentSpec`：工具内部规格
  - `LlmSpec`：LLM 可见的工具描述
  - 工具名 `acp_agent`，参数：`task`（必填）、`provider`（可选，默认 `acp.opencode`）
- `src/services/worker/internal/tools/builtin/acptool/executor.go`：
  - 解析 session key、provider、host
  - 创建或绑定 ACP session
  - 运行当前 turn（prompt -> poll update -> collect result）
  - 将 update 映射为 `run_events`
  - 返回结果给 LLM
- 在 `builtin.go` 中注册

验收标准：

- LLM 能调用 `acp_agent` 工具
- 工具内部通过 ACP Bridge 完成完整 session 生命周期
- update 正确映射为 run_events
- 不修改 RunEngine 路由逻辑

### PR-6：Frontend 展示（延后）

目标：前端能看见 code-agent 执行过程。

当前状态：**延后至子代理 UI 统一迭代时处理**。

`acp_agent` 工具执行期间会产生 `run_events`，现有的 subagent UI 已能展示这些事件流。第一阶段不需要独立的 code-agent UI，待 subagent UI 统一增强时一并处理。

验收标准（延后）：

- 用户能从现有事件流中观察到 OpenCode 执行过程
- 支持取消

### PR-7：ACP Host 抽象与 Provider 规格

目标：把当前 `sandbox + opencode` 首版实现升级为通用 ACP provider runtime。

范围：

- 抽出 `Local Process Host` / `Sandbox Process Host`
- 定义 provider 规格：`id`、`command`、`args`、`auth_strategy`、`capabilities`
- `acp_agent` 从写死 `opencode` 改为 provider-first
- 但架构中心保持 `session-first`，provider 仅作为 runtime backend config
- 保持 Desktop 与 SaaS 共用上层协议与事件模型

验收标准：

- Worker 不再把 ACP 绑定为 sandbox 专属能力
- 新 provider 不需要重写 Worker 主链路

### PR-7.1：Desktop Local Process Host + OpenCode 首接入

目标：让 Desktop 先以 OpenCode 作为第一个 provider 真正跑通。

范围：

- Desktop 运行态补齐 ACP host/runtime，不再依赖 `SandboxBaseURL`
- 增加 OpenCode provider preset，启动命令固定为 `opencode acp`
- 允许保留 custom command provider，便于后续接入 Codex / Claude / Gemini
- 处理 Desktop provider 的干净配置目录与启动超时治理

验收标准：

- Desktop 可直接拉起 `opencode acp`
- 不依赖 LLM Proxy 也能完成认证与会话初始化
- 用户可以把 OpenCode 作为第一个 ACP provider 使用

### PR-7.5：SaaS LLM Proxy + Profile 映射

当前状态：**部分完成**

目标：让 sandbox 内的 ACP agent 能安全地调用 LLM，不暴露真实 API key。

**背景**：ACP agent（如 OpenCode）自带 LLM 调用能力，需要 API key 和 model 配置。在 SaaS 环境下，直接把 API key 注入 sandbox 环境变量有泄露风险。解决方案是让 ACP agent 通过 Arkloop 的 LLM 代理端点调用模型。

范围：

**LLM Proxy（API service）**：

- 在 API service (19001) 新增 `/v1/llm-proxy/chat/completions` 端点
- OpenAI-compatible API 格式，支持 SSE streaming
- Session token 验证：只接受 Arkloop 签发的临时 token
- 额度限制：单次 acp_agent 调用的 token 预算
- 模型白名单：只允许 profile 指定的模型，防止 sandbox 内切换
- 复用 Worker 已有的 LLM routing 和 key pool

**Profile 映射**：

- 与 spawn_agent 的 profile 机制统一
- Config key 格式：`spawn.profile.{name}` -> `provider^model`
- 三级优先级：org override > plan entitlement > platform default
- `acp_agent` 工具新增 `profile` 参数（可选）

**Session Token 生命周期**：

- acp_agent tool executor 在启动 Bridge 前签发临时 token
- Token 绑定到 run_id，随 run 结束自动失效
- Token 携带模型白名单和 token 预算

**调用链**：

```text
LLM -> acp_agent(task="...", profile="strong")
  -> Executor 解析 profile: "strong" -> "anthropic^claude-sonnet-4-5"
  -> 签发临时 token (绑定 run_id, model, budget)
  -> Bridge 启动 OpenCode:
     - OPENCODE_API_BASE=http://api:19001/v1/llm-proxy
     - OPENCODE_API_KEY=<临时 token>
     - OPENCODE_MODEL=claude-sonnet-4-5
  -> OpenCode 通过 proxy 调 LLM
  -> API proxy 验证 token + 转发到实际 provider
```

**Desktop 直连**：Desktop 场景下默认跳过 proxy，直接使用 provider 自己的认证方式和本机配置。proxy 主要服务于 SaaS 和缺少本地凭据的场景。

验收标准：

- sandbox 内 ACP agent 能通过 proxy 完成 LLM 调用
- sandbox 内无真实 API key
- profile 映射正确解析
- 临时 token 随 run 结束失效

### PR-8：最小 MCP 注入（移除）

第一阶段不需要向 OpenCode 注入 MCP。OpenCode 在 sandbox 内拥有自己的工具链（文件读写、命令执行、git 操作等），可以直接操作 workspace。MCP 注入作为后续增强项，在确认有明确收益时再推进。

### PR-9：权限确认与治理增强

当前状态：**已完成**

目标：把 OpenCode 执行从“能跑”提升到“可控”。

范围：

- 支持 ACP 权限确认，如 OpenCode 实现需要
- 增加更细的 run 级审计字段：
  - 子进程启动参数
  - 版本信息
  - 最近错误摘要
- 增加更明确的 timeout、kill、grace period 配置

验收标准：

- 关键敏感操作可被中止
- 发生异常时能够快速定位是 sandbox、OpenCode 还是桥接层问题

### PR-10：恢复与复用能力

当前状态：**已完成第一版**

目标：让 code session 更接近持久化工作流，而不是一次性短进程。

范围：

- session 复用策略
- 断连后恢复句柄
- 更稳定的 stdout cursor / replay 机制
- 本地 sandbox 与云端 session 的统一恢复语义

验收标准：

- 同一个 code session 能跨多次 prompt 继续使用
- 中间断线不必强制重新起整个 sandbox

实现内容：

- **sandbox 状态查询端点**：新增 `acp_status` 端点，查询进程存活状态和 stdout cursor 位置
- **Bridge 多 prompt 支持**：`Bind()` 绑定已有进程、`RunPrompt()` 复用 session 发送新 prompt、`CheckAlive()` 健康检查、`State()` 导出状态
- **Worker runtime handle 注册表**：`Registry` 仅缓存当前 worker 进程内的 runtime handle，支持同一 `run_id + provider_id + host_kind` 上的 turn 复用
- **执行器复用逻辑**：`acp_agent` 工具优先复用已有 session，失败时自动回退到新建

后续补强：

- 将当前 `run_id` 级复用提升为 `workspace/thread/provider` 级复用
- 让 Desktop 与 SaaS 共用同一套恢复语义，而不是只复用 sandbox 句柄

### 延后项

以下内容明确不作为首阶段阻塞项：

- 完整 ACP `fs/*` 兼容
- 完整 ACP `terminal/*` 兼容
- 通用 Code Runtime 抽象
- 多 code-agent 适配层
- MCP 全量镜像到 OpenCode
- IDE 级文件树、diff、终端 pane

这些内容要等首条闭环稳定后，再决定是否推进。

### 推荐实施顺序

按依赖关系与实际进展：

1. PR-1 文档定稿（已完成）
2. PR-2 Sandbox 内 ACP 子进程托管（已完成）
3. PR-3 Sandbox Service 句柄化（已完成）
4. PR-4 Worker ACP Bridge 传输层（已完成）
5. PR-5 `acp_agent` builtin tool（已完成）
6. PR-6 前端展示（延后，复用 subagent UI）
7. PR-7 ACP Host 抽象与 Provider 规格
8. PR-7.0.1 Session-first 文档收敛与协议校准
9. PR-7.1 Desktop Local Process Host + OpenCode 首接入
10. 真实测试：验证 Desktop 完整链路（ACP session / `acp_agent` -> OpenCode -> 完成编码任务）
11. PR-7.5 SaaS LLM Proxy + Profile 映射补齐运行态 wiring
12. PR-9 权限确认与治理增强（已完成）
13. PR-10 恢复与复用（已完成第一版）
14. ~~PR-8 MCP 最小注入~~（移除，第一阶段不需要）

### 收敛标准

当满足以下条件时，可认为修订后的第一阶段完成：

- Arkloop 能在 Desktop 直接启动 OpenCode
- Arkloop 能在 SaaS sandbox 内启动 OpenCode
- LLM 能通过 `acp_agent` 工具自主决定何时委托任务给代码 agent
- Worker 能通过 ACP Bridge 发 prompt 并收回 update
- OpenCode 能直接读写 workspace、直接运行测试命令
- SaaS 路线下，ACP agent 通过 LLM Proxy 安全调用模型，sandbox 内无真实 API key
- UI 能通过现有事件流看到执行过程与终态结果
- Desktop 与 SaaS 共用同一套 ACP 控制面，但允许使用不同 host

到这一步，Arkloop 才算具备最小可用且方向正确的 ACP provider 能力。后续再决定是否补完整 ACP 面、更多 provider 适配与更深的 IDE 集成。
