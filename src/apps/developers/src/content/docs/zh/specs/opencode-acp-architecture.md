---
title: OpenCode + ACP 接入架构设计
description: Arkloop 接入 OpenCode 与 ACP 的第一阶段设计稿，覆盖运行位置、控制面、Sandbox 边界、MCP 关系与后续演进。
sidebarLabel: OpenCode + ACP 架构
order: 123
---

# OpenCode + ACP 接入架构设计

本文给出 Arkloop 接入 OpenCode 与 ACP 的设计方案。目标不是把 ACP 当作另一套 MCP，也不是在现有 sandbox shell 外再叠一层临时协议，而是用最小改动把「代码代理 + 本地 sandbox + 现有 Worker」串起来。

结论先行：

- 第一阶段采用 **co-located** 模式：`OpenCode` 进程直接运行在 sandbox 内，与 workspace 共处同一文件系统。
- `ACP` 在第一阶段主要承担 **prompt、session 控制、流式更新、取消**；不追求一次性实现完整 `fs/*` 与 `terminal/*`。
- `MCP` 继续作为工具层存在，不与 ACP 平级竞争；第一阶段不要求把 Arkloop 全量 MCP 能力注入给 OpenCode。
- Arkloop 不额外维护第二条交互 shell；应新增一条 **非 PTY 的 ACP 子进程通道** 来启动和托管 `opencode acp`。
- 本设计优先服务 **本地 sandbox / cowork** 形态；云端只保留架构兼容，不把远程 OpenCode 作为主交付目标。

---

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
- 让 OpenCode 在 sandbox 内直接读写 workspace、执行测试和命令
- 保持 Arkloop 现有 Thread / Run / Event 模型不变
- 避免为首版引入过多新协议面和双重 authority
- 为本地 sandbox / cowork 与云端 sandbox 复用同一套启动方式

### 2.2 非目标

- 不在第一阶段实现完整 ACP 全能力兼容
- 不在第一阶段重构 Arkloop 全部 MCP 子系统
- 不在第一阶段做 IDE 级文件细粒度授权
- 不在第一阶段做 OpenCode 之外的多 code-agent 兼容层
- 不把 OpenCode 设计成 MCP Server

---

## 3. 核心判断

### 3.1 ACP 不是 MCP 的替代品，但在 Arkloop 中通过 Tool 暴露

两者职责不同：

- `ACP`：代码编辑器 / 上层控制面 与 coding agent 之间的**会话协议**，管理完整的 agent session 生命周期
- `MCP`：agent 可调用的工具、资源与提示词协议，每次调用是无状态的函数调用

因此正确关系是：

- `ACP` 在上层，负责会话（session/new, session/prompt, session/update, session/cancel）
- `MCP` 在下层，负责工具（单次函数调用）

在 Arkloop 中，ACP 通过 `acp_agent` 工具暴露给 LLM。这不是把 ACP “硬塞成一组伪工具调用”——`acp_agent` 是一个正式的 builtin tool，内部封装了完整的 ACP Bridge，管理 session 创建、prompt 发送、update 轮询与结果收集。LLM 看到的是一次工具调用，Bridge 内部处理的是一段完整的 ACP 会话。

关键区分：

- `acp_agent` tool：LLM 发起委托 -> Bridge 管理完整 session -> 返回结果
- 普通 MCP tool：LLM 发起调用 -> 执行单个函数 -> 返回结果

### 3.2 第一阶段不需要手写 file runtime

由于 `OpenCode` 与 `workspace` 同处 sandbox：

- OpenCode 直接用本地 Linux 文件系统读写文件
- OpenCode 直接在 sandbox 内执行 `git`、`go test`、`pnpm`、`rg`
- Arkloop 不需要先实现 ACP `fs/read_text_file`、`fs/write_text_file` 才能跑通首版

这意味着第一阶段的 ACP 可以做减法：

- 必须：`session/new`、`session/prompt`、`session/update`、`session/cancel`
- 可选：权限确认、mode 同步
- 暂缓：`fs/*`、`terminal/*`、`loadSession`

### 3.3 不要再开第二条 shell

Arkloop 不应再为 OpenCode 额外维护一条“给 agent 用的交互 shell”。

更合适的模型是：

- 一个 sandbox session
- 一个 `opencode acp` 子进程
- 一条 ACP over stdio 的控制通道
- OpenCode 自己在 sandbox 内拉起需要的命令进程

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
- 参数：`task`（必填，任务描述）、`agent`（可选，默认 "opencode"，预留其他 ACP 兼容 agent）
- 从 LLM 视角是无状态的：发送任务，等待结果
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
  └─ tool call: acp_agent(task="...", agent="opencode")
       │
       ▼
  ACP Bridge (protocol, client, session management)
       │
       ▼
  Sandbox ACP endpoints (acp_start, acp_write, acp_read, acp_stop, acp_wait)
       │
       ▼
  OpenCode process (ACP over stdio)
       ├─ 直接访问 workspace
       ├─ 直接执行 git / test / build
       └─ 可按需连接 MCP tools
```

完整调用链：

1. LLM 在 agent loop 中决定调用 `acp_agent(task="实现用户认证模块", agent="opencode")`
2. `acptool/executor.go` 校验 sandbox 可用性，解析 agent 命令
3. 创建 ACP Bridge，通过 Sandbox Service 启动 `opencode acp` 子进程
4. Bridge 发送 `session/new` + `session/prompt`
5. Bridge 轮询 `session/update`，将 update 映射为 `run_events`
6. 收集终态结果，返回给 LLM

### 4.1 关键分层

| 层 | 职责 |
|----|------|
| Frontend | 展示 run 过程、终态结果、取消与错误 |
| LLM Agent Loop | 决定何时委托任务给 code agent |
| acp_agent Tool | 封装 ACP Bridge 调用，对 LLM 屏蔽 session 细节 |
| ACP Bridge | 管理 session 生命周期、发送 prompt、聚合 update |
| Sandbox Service | 分配 session、维护生命周期、桥接 guest agent |
| sandbox-agent | 启动并托管 `opencode acp` 进程 |
| OpenCode | 真正执行代码代理逻辑 |
| MCP | 可选工具层，由 OpenCode 或 Arkloop 使用 |

---

## 5. 运行模型

### 5.1 会话创建

当 LLM 在 agent loop 中调用 `acp_agent(task="...", agent="opencode")` 时：

1. `acptool/executor.go` 校验当前 run 是否有可用 sandbox
2. 解析 agent 参数，确定启动命令（默认 `opencode acp --cwd <workspace>`）
3. 创建 ACP Bridge 实例
4. Bridge 通过 Sandbox Service 启动 `opencode acp` 子进程
5. Bridge 持有该子进程的 stdio 通道，发送 `session/new` 创建 ACP 会话

### 5.2 prompt 执行

一次 `acp_agent` 工具调用的完整流程：

1. LLM 决定调用 `acp_agent(task="...", agent="opencode")`
2. Tool executor 校验 sandbox 可用性
3. Bridge 在 sandbox 内启动 ACP agent 进程
4. Bridge 发送 `session/new` + `session/prompt`
5. OpenCode 在 sandbox 内部执行（读取代码、修改文件、运行命令、生成总结）
6. Bridge 轮询 `session/update`，将 update 映射为 `run_events`
7. Bridge 收集终态结果，返回给 tool executor
8. Tool executor 将结果返回给 LLM

### 5.3 取消与超时

Worker 保持现有 run 级治理不变：

- 触发取消时，对应 ACP `session/cancel`
- 若 agent 进程未在宽限期内退出，sandbox-agent 强制终止子进程
- sandbox session 的生命周期继续由 Sandbox Service 统一管理

---

## 6. ACP 采用面

### 6.1 第一阶段采用的 ACP 子集

第一阶段只依赖这几个核心能力：

- `session/new`
- `session/prompt`
- `session/update`
- `session/cancel`

如 OpenCode 实现要求，可追加：

- `session/request_permission`
- `session/set_mode`

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

## 10. 本地 sandbox / cowork 与云端兼容

### 10.1 本地优先

本方案优先服务本地 sandbox / cowork：

- 本地工作目录更接近真实开发体验
- OpenCode 在本地更有价值
- 用户对文件与命令的预期与 CLI 一致

### 10.2 云端保持同构

虽然云端 OpenCode 不是主卖点，但架构上仍应保持同构：

- 一样是 sandbox session
- 一样启动 `opencode acp`
- 一样通过 stdio 说 ACP
- 一样由 Worker 负责 run / event / cancel

这样本地与云端不会分裂成两套系统。

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
- 支持 `session/new`、`session/prompt`、`session/update`、`session/cancel`

验收标准：

- Bridge 能通过 sandbox endpoint 与 ACP agent 通信
- 支持 session 创建、prompt 发送、update 轮询、取消

### PR-5：`acp_agent` Builtin Tool（已完成）

目标：让 LLM 能通过工具调用将任务委托给 ACP coding agent。

范围：

- `src/services/worker/internal/tools/builtin/acptool/spec.go`：
  - `AgentSpec`：工具内部规格
  - `LlmSpec`：LLM 可见的工具描述
  - 工具名 `acp_agent`，参数：`task`（必填）、`agent`（可选，默认 "opencode"）
- `src/services/worker/internal/tools/builtin/acptool/executor.go`：
  - 校验当前 run 是否有可用 sandbox
  - 解析 agent 参数，确定启动命令
  - 创建 ACP Bridge 实例
  - 运行完整 session（new -> prompt -> poll update -> collect result）
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

### PR-7：本地 sandbox / cowork 接入

目标：让本地运行形态也复用同一套 OpenCode 启动方式。

范围：

- 本地 sandbox provider 复用 ACP 子进程托管接口
- 本地工作目录与 `--cwd` 绑定
- 校验本地环境中的 `opencode` 可执行文件探测与版本信息
- 保持云端与本地的上层协议一致

验收标准：

- 本地 sandbox 可启动 OpenCode
- Worker 无需区分本地 / 云端两套 ACP 通道实现

### PR-8：最小 MCP 注入（移除）

第一阶段不需要向 OpenCode 注入 MCP。OpenCode 在 sandbox 内拥有自己的工具链（文件读写、命令执行、git 操作等），可以直接操作 workspace。MCP 注入作为后续增强项，在确认有明确收益时再推进。

### PR-9：权限确认与治理增强

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

目标：让 code session 更接近持久化工作流，而不是一次性短进程。

范围：

- session 复用策略
- 断连后恢复句柄
- 更稳定的 stdout cursor / replay 机制
- 本地 sandbox 与云端 session 的统一恢复语义

验收标准：

- 同一个 code session 能跨多次 prompt 继续使用
- 中间断线不必强制重新起整个 sandbox

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
7. PR-7 本地 sandbox / cowork 复用
8. ~~PR-8 MCP 最小注入~~（移除，第一阶段不需要）
9. PR-9 权限与治理增强
10. PR-10 恢复与复用

### 收敛标准

当满足以下条件时，可认为第一阶段完成：

- Arkloop 能在 sandbox 内启动 OpenCode
- LLM 能通过 `acp_agent` 工具自主决定何时委托任务给代码 agent
- Worker 能通过 ACP Bridge 发 prompt 并收回 update
- OpenCode 能直接读写 workspace、直接运行测试命令
- UI 能通过现有事件流看到执行过程与终态结果
- 本地 sandbox 与云端 sandbox 共用同一条 ACP 启动链路

到这一步，Arkloop 已具备最小可用的 code-agent 能力。后续再决定是否抽更通用的 runtime 与完整 ACP 面。
