---
title: Sub-agent 协作架构设计
description: Arkloop Sub-agent 协作架构设计稿，覆盖对象模型、状态机、上下文继承、控制平面、预算治理与可观测性。
sidebarLabel: Sub-agent 协作架构
order: 121
---

# Sub-agent 协作架构设计

本文不是源码解读，而是面向 Arkloop 的复刻设计稿。目标不是“再加一个 `spawn_agent` 工具”，而是把子 Agent 提升为系统内的一等协作实体。

本文只描述 Arkloop 需要具备的架构能力、机制边界与实施方式，不依赖外部项目命名或代码结构。

---

## 1. 设计目标

Arkloop 当前已经具备“父 run 创建子 run，并同步等待结果”的基础能力，但这还不是完整的多 Agent 协作系统。本文设计的目标是把现有能力扩展为一套统一的子 Agent 架构，满足以下要求：

- 子 Agent 是稳定的协作对象，而不是一次匿名工具调用
- 父 Agent 可以创建、继续交互、等待、恢复、关闭子 Agent
- 上下文继承是显式策略，而不是隐式副作用
- 状态、结果、预算、权限、可观测性彼此解耦
- 内部系统流程与模型工具面共享同一套控制平面

对应的业务价值：

- 主 Agent 负责编排，子 Agent 负责隔离执行
- 支持并行探索、审查、汇总、批处理等模式
- 支持跨回合恢复和中断后继续
- 支持未来的 DAG 形态执行，而不局限于单层父子调用

---

## 2. 非目标

本文不覆盖以下内容：

- 不设计新的 LLM Provider 路由算法
- 不设计新的 Persona DSL
- 不设计前端最终视觉稿
- 不复用外部项目的提示词、文案或协议文本
- 不要求一次性替换 Arkloop 现有所有 run 执行链

---

## 3. 核心判断

Arkloop 要复刻的不是某个 `spawn_agent(...)` 函数，而是下面四层能力：

1. **Sub-agent Core**：子 Agent 的身份、状态、生命周期
2. **Control Plane**：创建、发送输入、等待、恢复、关闭、取消
3. **Context Inheritance Layer**：上下文继承与隔离策略
4. **Observability Layer**：状态、事件、成本、父子关系、调试信息

只有把这四层补齐，子 Agent 才是系统级能力；否则仍然只是“run 内部递归调用 run”。

---

## 4. 现状问题

当前 Arkloop 的已有能力：

- Worker 在 `RunContext.SpawnChildRun` 可用时，按需注入 `spawn_agent`
- `spawn_agent` 接收 `persona_id + input`，返回结构化子 Agent 句柄供后续控制原语继续协作
- Lua 侧支持 `agent.spawn(...)`、`agent.send(...)`、`agent.wait(...)`、`agent.resume(...)`、`agent.close(...)`
- 父子 run 之间通过 Redis Pub/Sub 传递终态结果

当前主要不足：

- 子 Agent 没有稳定的独立身份，只有 child run
- `wait` 仍只支持单个子 Agent，不支持 `any/all` 批量等待聚合
- Lua 侧暂不直接暴露 `interrupt` 原语，仍通过 `send(..., { interrupt = true })` 表达抢占续跑
- 上下文继承只是一条纯文本消息，附件、多模态、历史和运行态继承太弱
- 治理只覆盖局部并发，没有全局深度、配额、背压、取消传播模型
- Console 无法展示完整的子 Agent 图谱与协作轨迹

---

## 5. 系统术语

为避免后续实现时概念混乱，统一术语如下：

### 5.1 Run

- 一次执行实例
- 由 `runs` 表驱动
- 是执行面对象

### 5.2 Sub-agent

- 一个可被父 Agent 控制的协作实体
- 可以对应一个或多个 run 执行实例
- 是协作面对象

### 5.3 Parent Run

- 发起子 Agent 的 run

### 5.4 Parent Thread

- 发起子 Agent 的对话线程
- 用于上下文继承、UI 展示与回溯

### 5.5 Root Run

- 一条协作树的根 run
- 预算与治理默认以 root run 聚合

### 5.6 Role

- 子 Agent 的执行角色
- 例如：`worker`、`explorer`、`reviewer`、`synthesizer`

### 5.7 Context Mode

- 子 Agent 的上下文继承策略
- 决定它看见哪些历史、附件、工作区和运行时信息

---

## 6. 设计原则

### 6.1 子 Agent 是实体，不是函数结果

- `spawn_agent` 返回 `agent_id`
- 结果输出是后续状态的一部分，不是唯一产物

### 6.2 状态与结果分离

- 状态用于编排同步
- 结果用于业务消费
- 不把两者塞进单个字符串

### 6.3 上下文继承必须显式声明

- 禁止默认“全继承”
- 禁止默认“完全不继承”
- 必须能表达多种继承模式

### 6.4 控制平面与执行平面分离

- Control Plane 管理身份、生命周期、治理
- Run Engine 负责执行具体 run

### 6.5 内部流程与模型工具共用内核

- review、memory consolidate、批量 worker 等内部流程
- 与模型主动调用的子 Agent
- 底层应共享同一套 Sub-agent Core

---

## 7. 总体架构

```
Parent Run
  |
  |  spawn/send/wait/resume/close
  v
Sub-agent Control Plane
  |
  +--> Sub-agent Registry / State Store
  |
  +--> Context Inheritance Layer
  |
  +--> Child Run Planner
  |
  +--> Run Queue / Worker Engine
  |
  +--> Event Bus / Status Stream
  |
  +--> Console / API Read Model
```

### 7.1 分层职责

| 层 | 职责 |
|----|------|
| Sub-agent Core | 定义身份、状态机、关系模型 |
| Control Plane | 提供 spawn/send/wait/resume/close 等能力 |
| Context Layer | 生成子 Agent 可见上下文 |
| Child Run Planner | 生成具体 child run 输入与执行参数 |
| Run Engine | 执行 Agent Loop、工具、Persona |
| Event Layer | 状态广播、事件存储、进度通知 |
| Read Model | Console / API 展示父子关系、成本与轨迹 |

---

## 8. 对象模型

### 8.1 Sub-agent 核心对象

建议在系统中引入一等概念 `SubAgent`。最小字段如下：

```ts
type SubAgent = {
  id: string
  parentRunId: string
  parentThreadId: string
  rootRunId: string
  rootThreadId: string
  depth: number
  role: string | null
  personaId: string | null
  nickname: string | null
  sourceType: 'thread_spawn' | 'review' | 'memory_consolidation' | 'agent_job' | 'other'
  contextMode: 'isolated' | 'fork_recent' | 'fork_thread' | 'fork_selected' | 'shared_workspace_only'
  status: 'created' | 'queued' | 'running' | 'waiting_input' | 'completed' | 'failed' | 'cancelled' | 'closed' | 'resumable'
  currentRunId: string | null
  lastCompletedRunId: string | null
  lastOutputRef: string | null
  lastError: string | null
  createdAt: string
  startedAt: string | null
  completedAt: string | null
  closedAt: string | null
}
```

### 8.2 为什么不能只用 runs 表

如果只用 `runs.parent_run_id` 表示子 Agent：

- 无法表示“同一个子 Agent 后续又被继续输入一次”
- 无法表达 `resume` 与 `close`
- 无法为子 Agent 聚合成本、事件和最终状态
- 无法把“协作身份”与“执行实例”分开

因此必须区分：

- `run`：一次执行实例
- `sub_agent`：协作对象

### 8.3 Child Run 实例对象

当 Control Plane 决定调度子 Agent 时，会为其创建具体执行实例：

```ts
type SubAgentRun = {
  subAgentId: string
  runId: string
  sequence: number
  triggerType: 'spawn' | 'send_input' | 'resume' | 'retry'
  inputRef: string
  contextSnapshotRef: string | null
}
```

这样同一个子 Agent 可以关联多次 run。

---

## 9. Source 模型

Arkloop 当前只有 `parent_run_id`，不够表达协作语义。需要引入显式的 `source` 抽象。

建议统一为：

```ts
type AgentSource =
  | { type: 'user_run' }
  | { type: 'thread_spawn', parentThreadId: string, parentRunId: string, depth: number, role?: string, nickname?: string }
  | { type: 'review' }
  | { type: 'memory_consolidation' }
  | { type: 'agent_job', jobId: string }
  | { type: 'other', label: string }
```

### 9.1 设计要求

- source 必须持久化
- source 必须可用于 API 过滤
- source 必须进入 Console 展示层
- source 必须能驱动治理和恢复策略

---

## 10. 状态机设计

### 10.1 状态定义

| 状态 | 说明 |
|------|------|
| `created` | 已创建子 Agent 身份，但尚未入队 |
| `queued` | 已生成待执行 run，等待 worker 处理 |
| `running` | 当前有活跃 run 正在执行 |
| `waiting_input` | 子 Agent 需要新的输入才能继续 |
| `completed` | 最近一次执行已成功结束 |
| `failed` | 最近一次执行失败 |
| `cancelled` | 最近一次执行被取消 |
| `closed` | 子 Agent 已被显式关闭，不再接受新输入 |
| `resumable` | 当前不活跃，但持久化历史足够，可恢复 |

### 10.2 允许的转换

```text
created -> queued -> running
running -> waiting_input
waiting_input -> queued -> running
running -> completed
running -> failed
running -> cancelled
completed -> closed
failed -> closed
cancelled -> closed
completed -> resumable
failed -> resumable
cancelled -> resumable
resumable -> queued -> running
```

### 10.3 不允许的转换

- `closed -> running`
- `closed -> waiting_input`
- `created -> completed`
- `failed -> running`（必须经过 `resume` 或 `retry`）

### 10.4 设计约束

- 状态机以 `sub_agent` 为主，而不是单个 run
- 每次 run 的终态都要回写到 `sub_agent.status`
- `sub_agent.currentRunId` 在 `running` 期间必须非空

---

## 11. 控制平面 API 设计

### 11.1 内部接口

Worker 内部统一暴露如下接口：

```go
type SubAgentControl interface {
    Spawn(ctx context.Context, req SpawnSubAgentRequest) (SpawnSubAgentResult, error)
    SendInput(ctx context.Context, req SendSubAgentInputRequest) (SendSubAgentInputResult, error)
    Wait(ctx context.Context, req WaitSubAgentRequest) (WaitSubAgentResult, error)
    Resume(ctx context.Context, req ResumeSubAgentRequest) (ResumeSubAgentResult, error)
    Close(ctx context.Context, req CloseSubAgentRequest) (CloseSubAgentResult, error)
    Interrupt(ctx context.Context, req InterruptSubAgentRequest) error
    GetStatus(ctx context.Context, id string) (SubAgentStatusSnapshot, error)
    ListChildren(ctx context.Context, parentRunID string) ([]SubAgentStatusSnapshot, error)
}
```

### 11.2 模型工具面

模型工具层不应直接操作 run，而应操作 Sub-agent Control：

- `spawn_agent`
- `send_input`
- `wait_agent`
- `resume_agent`
- `close_agent`

### 11.3 Lua 绑定

Lua 侧建议拆成两层：

#### 低层原语

- `agent.spawn({ persona_id, input, context_mode?, role?, nickname?, inherit? }) -> (status, err)`
- `agent.send(id, input, { interrupt? }) -> (status, err)`
- `agent.wait(id, timeout_ms?) -> (status, err)`
- `agent.close(id) -> (status, err)`
- `agent.resume(id) -> (status, err)`

Lua 侧直接暴露控制平面原语，返回结构化状态表，不再保留 `agent.run` / `agent.run_parallel` 同步语法糖。

---

## 12. spawn 设计

### 12.1 输入模型

建议 `spawn_agent` 的请求体至少支持：

```json
{
  "persona_id": "search-output@1",
  "role": "worker",
  "nickname": "Atlas",
  "context_mode": "fork_recent",
  "inherit": {
    "messages": true,
    "attachments": true,
    "workspace": true,
    "skills": true,
    "runtime": true,
    "memory_scope": "same_user"
  },
  "input": "把这些搜索笔记整理成最终回答"
}
```

### 12.2 spawn 过程

Control Plane 需要完成以下步骤：

1. 校验父 run 是否允许继续派生子 Agent
2. 计算并校验递归深度
3. 预占活跃子 Agent 配额
4. 分配子 Agent 身份与可选昵称
5. 生成上下文快照
6. 生成首个 child run 输入
7. 写入 `sub_agents`
8. 写入 `sub_agent_events`：`sub_agent.spawn_requested`
9. 创建 child run
10. 更新 `sub_agent.currentRunId`
11. 写入 `sub_agent_events`：`sub_agent.spawned`
12. 返回 `{ agent_id, status, nickname }`

### 12.3 spawn 的返回值

不要只返回文本。建议返回：

```json
{
  "agent_id": "agt_123",
  "status": "queued",
  "nickname": "Atlas",
  "run_id": "run_456"
}
```

---

## 13. send_input 设计

### 13.1 语义

- 向已存在的子 Agent 追加新的任务输入
- 如果子 Agent 正在运行，可选择：
  - 排队
  - 中断当前执行后切换新输入
  - 拒绝发送

### 13.2 请求模型

```json
{
  "id": "agt_123",
  "input": "再补充一下反例和风险",
  "interrupt": false
}
```

### 13.3 行为规则

- `status == closed`：拒绝
- `status == queued|running && interrupt == false`：进入持久化 pending input 队列，不立即创建新 run
- `status == queued|running && interrupt == true`：输入以高优先级插队、发送中断请求，当前 run 终态后自动续跑
- `status == completed|failed|cancelled|resumable|waiting_input`：直接创建新 run
- pending queue 按 `priority DESC, seq ASC` 消费，同一轮续跑会把当前批次输入用双换行合并成一条新的 user message

---

## 14. wait 设计

### 14.1 设计目的

- 让父 Agent 能等待一个或多个子 Agent 到达终态
- 防止忙等
- 支持“任一完成即返回”的并行编排

### 14.2 请求模型

```json
{
  "ids": ["agt_123", "agt_456"],
  "timeout_ms": 30000,
  "mode": "any"
}
```

### 14.3 返回模型

```json
{
  "timed_out": false,
  "statuses": {
    "agt_123": { "status": "completed", "run_id": "run_1" }
  }
}
```

### 14.4 行为约束

- 设定最小超时下限，避免模型频繁 100ms 轮询
- `mode=any` 默认即可，后续可扩 `all`

---

## 15. resume 设计

### 15.1 设计目的

- 子 Agent 不应强依赖进程内内存
- 若 worker 重启或线程上下文被释放，仍可从持久化状态恢复

### 15.2 resume 成立条件

- `sub_agent.status in (completed, failed, cancelled, resumable)`
- 有可恢复的上下文快照或历史记录

### 15.3 resume 行为

- 重建运行时上下文
- 重新挂接当前状态订阅
- 创建新的 run 或恢复到 waiting 状态

---

## 16. close 设计

### 16.1 设计目的

- 显式结束协作实体
- 释放活跃子 Agent 配额
- 防止历史对象无限膨胀

### 16.2 行为规则

- 若正在运行：先请求中断或关闭当前 run
- 将 `sub_agent.status` 置为 `closed`
- 清空 `currentRunId`
- 写入 `sub_agent.closed`

### 16.3 close 与 delete 的区别

- `close` 是生命周期终结，不是数据删除
- 保留所有事件与输出，供审计和调试

---

## 17. 上下文继承层设计

### 17.1 设计原则

- 上下文继承是策略，不是副作用
- 继承维度要拆开，而不是单一布尔值

### 17.2 继承维度

| 维度 | 说明 |
|------|------|
| 消息历史 | 最近 N 轮或全量历史 |
| 附件 | 文本、文件、图片、多模态 part |
| 工作区 | cwd、profile、workspace_ref |
| 技能 | enabled skills、skill context |
| 运行态 | tool allowlist、sandbox policy、approval policy、route/model |
| 记忆 | 共享同一 user memory、只读、隔离 |
| 预算 | 继承、截断、重置 |

### 17.3 建议的 context mode

#### `isolated`

- 不继承历史
- 仅使用显式输入
- 适合纯工具 worker

#### `fork_recent`

- 继承最近 N 轮消息
- 继承附件与运行时上下文
- 适合局部接力

#### `fork_thread`

- 继承整个线程历史
- 用于需要完整背景的子 Agent

#### `fork_selected`

- 只继承上层挑选的消息与附件
- 适合严格受控上下文

#### `shared_workspace_only`

- 不继承消息历史
- 仅继承工作区与技能
- 适合工程执行型子 Agent

### 17.4 上下文闭包修补

当使用 `fork_thread` / `fork_recent` 时，必须修补父线程历史中的未闭合调用现场。

最小要求：

- 如果子 Agent 是由父 Agent 某次协作调用触发的
- 子 Agent 看到的历史中，必须包含“父线程已经成功发起该协作”的闭包信息

否则子 Agent 容易继承到逻辑不完整的上下文。

---

## 18. Persona / Role 叠加模型

Arkloop 当前子 run 主要通过 `persona_id` 驱动。为了让协作层与执行层解耦，建议将 `persona` 和 `role` 拆开。

### 18.1 Persona

- 决定基础提示词与执行器
- 例如 `search-output@1`

### 18.2 Role

- 决定协作行为偏好
- 例如：
  - `worker`
  - `explorer`
  - `reviewer`
  - `synthesizer`

### 18.3 叠加顺序

```text
Base Persona Config
  -> Role Overrides
  -> Parent Runtime Overrides
  -> Child Explicit Overrides
```

这样可以避免把协作风格写死在 Persona 里。

---

## 19. 预算与权限模型

### 19.1 预算分层

建议引入三层预算：

- Root Budget：整棵协作树总预算
- Child Budget：单个子 Agent 的预算上限
- Descendant Budget：某个子 Agent 的子树预算上限

### 19.2 预算维度

- reasoning iterations
- tool continuation budget
- max parallel children
- token budget
- cost budget

### 19.3 权限继承规则

默认规则：

- 子 Agent 不能比父 Agent 拥有更宽的工具权限
- 子 Agent 的 sandbox / approval policy 不能比父 Agent 更宽松
- 可以变得更严格，但不能更开放

### 19.4 需要禁止的行为

- 子 Agent 自行提升工具 allowlist
- 子 Agent 覆盖组织级 budget 上限
- 子 Agent 通过 resume 绕开配额检查

---

## 20. 并发与背压治理

### 20.1 限制项

系统至少需要以下限制：

- `max_subagent_depth`
- `max_active_subagents_per_root_run`
- `max_parallel_children_per_run`
- `max_descendant_count_per_root_run`
- `max_pending_child_runs_per_root_run`

### 20.2 背压策略

当队列或组织资源达到阈值时，允许策略如下：

- 拒绝新 spawn
- 将并行降级为串行
- 将低优先级子 Agent 延迟
- 仅允许 `resume/close`，不允许新 `spawn`

### 20.3 取消传播

建议支持三种取消传播策略：

- `cascade_all`
- `cascade_running_only`
- `detach_children`

默认使用 `cascade_running_only`。

---

## 21. 事件模型

### 21.1 事件必须是一等对象

不要只依赖 Redis Pub/Sub 结果串。需要持久化子 Agent 事件。

建议事件流：

- `sub_agent.spawn_requested`
- `sub_agent.spawned`
- `sub_agent.run_queued`
- `sub_agent.run_started`
- `sub_agent.input_sent`
- `sub_agent.wait_requested`
- `sub_agent.wait_resolved`
- `sub_agent.resumed`
- `sub_agent.interrupted`
- `sub_agent.completed`
- `sub_agent.failed`
- `sub_agent.cancelled`
- `sub_agent.closed`

### 21.2 事件作用

- 驱动 Console 时间线
- 驱动 API 状态回放
- 驱动调试与审计
- 驱动后续统计和成本聚合

---

## 22. 结果模型

建议统一输出结构：

```json
{
  "agent_id": "agt_123",
  "status": "completed",
  "output": "...",
  "output_truncated": false,
  "artifacts": [],
  "usage": {
    "input_tokens": 0,
    "output_tokens": 0,
    "cost_usd": 0
  },
  "citations": [],
  "tool_calls": [],
  "error": null
}
```

### 22.1 设计要求

- `output` 只是结果的一部分
- `usage` 必须可聚合到父 Agent 和 root run
- 若结果截断，必须显式标注 `output_truncated`

---

## 23. 存储设计建议

### 23.1 新表建议

建议新增：

- `sub_agents`
- `sub_agent_events`
- `sub_agent_inputs`
- `sub_agent_outputs`（可选，或复用 artifact / message store）

### 23.2 与 runs 的关系

`runs` 保留执行面职责：

- 具体 run 状态
- run events
- message 归档

`sub_agents` 负责协作面职责：

- 父子关系
- role / source / depth
- 当前状态
- 当前 run 指针
- 最终输出聚合

---

## 24. API 读模型建议

### 24.1 读取父 run 的子 Agent 列表

```text
GET /v1/runs/{id}/sub-agents
```

返回：

- 子 Agent 列表
- 当前状态
- role / persona / nickname
- 成本摘要
- 当前 run / 最近 run

### 24.2 读取单个子 Agent 详情

```text
GET /v1/sub-agents/{id}
```

返回：

- 基本信息
- 状态
- 历史事件
- 最近输出
- 子 run 列表

### 24.3 子 Agent 事件 SSE

```text
GET /v1/sub-agents/{id}/events
```

用于 Console 实时展示。

---

## 25. Console 设计要求

Console 至少支持以下视图：

### 25.1 Run DAG 视图

- 展示 root run 与其所有子 Agent
- 节点显示：role、persona、status、cost、时间

### 25.2 子 Agent 时间线视图

- 展示 spawn / input / wait / resume / close 等事件

### 25.3 输出与错误视图

- 展示最终输出
- 展示错误与最近 tool trace

### 25.4 治理指标视图

- 当前活跃子 Agent 数
- 总 descendants 数
- depth
- 累计 cost / tokens

---

## 26. 与现有 Arkloop 组件的接入方式

### 26.1 Run Engine

- 保留 `EngineV1.Execute(...)` 作为执行入口
- 不把协作治理继续塞进 `spawnChildRun(...)`

### 26.2 Pipeline

- `mw_spawn_agent` 改为调用 `SubAgentControl`
- `mw_tool_build` 继续从 allowlist 构造 LLM 工具面

### 26.3 Lua 执行器

- `agent.run` 迁移为语法糖
- 新增低层 `spawn/send/wait/resume/close`

### 26.4 API

- 新增只读调试与观测接口
- 是否对终端用户开放创建型 API 可后置

### 26.5 Redis / Queue

- Redis 只承担通知角色
- 事实源改为 DB 事件流

---

## 27. 向后兼容策略

### 27.1 兼容现有 `spawn_agent`

短期保留当前签名：

```json
{ "persona_id": "...", "input": "..." }
```

但内部实现改为：

- 创建 Sub-agent
- 创建 child run
- 等待终态
- 将结构化结果压缩成旧返回值

### 27.2 兼容现有 `agent.run`

保持 Lua 调用习惯不变，但内部改走新控制平面。

---

## 28. 失败与恢复策略

### 28.1 spawn 失败

- 未持久化成功：直接返回错误
- 已创建 `sub_agent` 但未入队：标记 `failed`

### 28.2 child run 失败

- `sub_agent.status = failed`
- 允许 `resume`
- 保留错误快照

### 28.3 worker 重启

- 活跃 child run 通过现有 run 恢复机制继续推进
- `sub_agent.currentRunId` 用于重新挂接状态

### 28.4 wait 超时

- 不改变子 Agent 状态
- 只返回 `timed_out = true`

---

## 29. 安全边界

### 29.1 权限不能上升

- 子 Agent 的工具权限、sandbox、approval policy 只能等于或小于父 Agent

### 29.2 预算不能逃逸

- 子 Agent 所有消耗要计入 root run 聚合

### 29.3 source 必须可信

- `source.type` 只能由后端控制平面写入
- 前端和模型不能伪造

### 29.4 内部标签不外泄

- 系统内部状态和持久化细节不能直接当 UI 文案输出

---

## 30. 成功标准

当以下条件同时满足时，可以认为 Arkloop 已具备可用的 Sub-agent 架构：

- 子 Agent 有独立身份与状态机
- `spawn/send/wait/resume/close` 全部可用
- Lua 与工具面共享同一控制平面
- child run 不再只是一次匿名同步调用
- 上下文继承可显式选择
- Console 可以展示父子 Agent 图谱
- 成本、预算、取消、并发、深度都可治理

---

## 31. 最终设计判断

Arkloop 不应该照搬某个外部实现模块，而应该落地下面这些设计逻辑：

- 子 Agent 是会话级协作对象
- 协作需要独立控制平面
- 上下文继承必须显式建模
- 生命周期不能等同于一次 run
- 状态、结果、预算、权限、可观测性必须分层

当前 Arkloop 已经有 child run 的雏形，这是好事。但下一步的重点不应再是继续加强 `spawn_agent` 单点，而是把它纳入完整的 Sub-agent 架构。

---

## 附录：实施路线图

# Sub-agent 架构复刻路线图

本文是 `Sub-agent 协作架构设计` 的实施路线图。目标是把设计拆成 AI 可逐步执行的改造序列，避免一次性重构过大。

本文只描述目标、边界、依赖、产物与验收，不包含工期、人力或排期。

---

## 1. 路线图原则

- 先补抽象，再补接口
- 先补控制平面，再补高级语法糖
- 先补事实源与状态机，再补 Console 展示
- 保持现有 `spawn_agent` / `agent.run` 可兼容运行
- 每个阶段都能独立验证，不依赖后续阶段才能落地

---

## 2. 总体阶段

建议拆成 6 个 Track：

- **Track A**：Sub-agent Core 与持久化模型
- **Track B**：Control Plane 与内部接口
- **Track C**：上下文继承与 child run 规划器
- **Track D**：工具面与 Lua 接口升级
- **Track E**：治理、预算、背压、取消传播
- **Track F**：API / Console 读模型与可观测性

依赖关系：

```text
Track A -> Track B -> Track C -> Track D
                     -> Track E
Track A + Track B + Track E -> Track F
```

---

## 3. Track A -- Sub-agent Core

### A1. 引入一等 Sub-agent 概念

**目标**

- 在 Worker 内部建立 `SubAgent` 抽象
- 明确区分协作实体与执行实例

**产物**

- `sub_agents` 持久化模型
- `SubAgentStatus` 枚举
- `SubAgentSource` 抽象
- `SubAgentRepository`

**最低字段**

- `id`
- `parent_run_id`
- `parent_thread_id`
- `root_run_id`
- `root_thread_id`
- `depth`
- `role`
- `persona_id`
- `nickname`
- `source_type`
- `context_mode`
- `status`
- `current_run_id`
- `last_completed_run_id`
- `last_output_ref`
- `last_error`
- `created_at`
- `started_at`
- `completed_at`
- `closed_at`

**验收**

- 系统可以在不执行 child run 的情况下，单独创建一个 `sub_agent` 记录
- `SubAgentStatus` 有完整状态机定义

### A2. 建立 sub_agent 事件流

**目标**

- 不再只依赖 Redis Pub/Sub 结果串
- 所有协作事件写入 DB

**产物**

- `sub_agent_events` 模型
- 事件 append helper
- 事件类型常量定义

**最低事件集**

- `sub_agent.spawn_requested`
- `sub_agent.spawned`
- `sub_agent.run_queued`
- `sub_agent.run_started`
- `sub_agent.input_sent`
- `sub_agent.wait_requested`
- `sub_agent.wait_resolved`
- `sub_agent.resumed`
- `sub_agent.interrupted`
- `sub_agent.completed`
- `sub_agent.failed`
- `sub_agent.cancelled`
- `sub_agent.closed`

**验收**

- 单个子 Agent 的完整生命周期可从 DB 事件恢复

**当前实现状态**

- 已落地事件：`sub_agent.spawn_requested`、`sub_agent.spawned`、`sub_agent.run_queued`、`sub_agent.run_started`、`sub_agent.completed`、`sub_agent.failed`、`sub_agent.cancelled`
- 已新增 `sub_agent_events` 持久化模型与统一 append helper
- `input_sent / wait_requested / wait_resolved / resumed / interrupted / closed` 依赖 Track B 控制原语落地后继续接入，不在 A2 内伪造事件

---

## 4. Track B -- Control Plane

### B1. 定义统一控制接口

**目标**

- 建立 `SubAgentControl` 作为唯一协作入口

**产物**

- `Spawn`
- `SendInput`
- `Wait`
- `Resume`
- `Close`
- `Interrupt`
- `GetStatus`
- `ListChildren`

**验收**

- `mw_spawn_agent` 与 Lua 都不再直接操作 `spawnChildRun`

### B2. 将 child run 创建逻辑下沉到 Control Plane

**目标**

- 当前 `spawnChildRun(...)` 改为内部执行原语
- 不再承担完整协作语义

**产物**

- `ChildRunPlanner`
- `SubAgentRunFactory`
- `SubAgentStateProjector`

**验收**

- spawn、send_input、resume 都通过同一条控制链创建 run

**当前实现状态**

- `ChildRunPlanner` / `SubAgentRunFactory` / `SubAgentStateProjector` 已落地到 `subagentctl`
- `spawn`、`send_input`、`resume` 已统一走 Control Plane 建 run
- `send_input` 已支持活跃态 pending queue、`interrupt=true` 插队、终态自动续跑
- run 终态到 sub_agent 状态投影已从 Pipeline 手写逻辑下沉到 `SubAgentStateProjector`

### B3. 兼容现有同步调用风格

**目标**

- 本仓库不实施旧同步返回兼容层

**产物**

- 无

**验收**

- 不新增旧 `spawn_agent -> output`、旧返回值适配器、旧 persona 专用兼容分支

---

## 5. Track C -- 上下文继承层

### C1. 建立 Context Snapshot 抽象

**目标**

- 统一收集父运行态与消息历史

**产物**

- `ContextSnapshot`
- `SnapshotBuilder`
- `SnapshotStorage`

**快照维度**

- 消息历史
- 附件与多模态 part
- cwd / profile / workspace
- enabled skills
- tool allowlist / denylist
- approval / sandbox policy
- route / model 信息
- memory scope

**验收**

- 子 Agent 创建时不再只依赖一条纯文本 user message

### C2. 实现 context mode

**目标**

- 支持多种上下文继承策略

**模式**

- `isolated`
- `fork_recent`
- `fork_thread`
- `fork_selected`
- `shared_workspace_only`

**验收**

- 每种模式都能生成不同的输入与快照结果

### C3. 补上下文闭包修补

**目标**

- fork 历史时补足协作调用闭包

**验收**

- 子 Agent 继承到的历史中不存在未闭合的 spawn 现场

---

## 6. Track D -- 工具面与 Lua 升级

### D1. 升级模型工具面

**目标**

- 从单个 `spawn_agent` 扩展到完整协作原语

**新增工具**

- `send_input`
- `wait_agent`
- `resume_agent`
- `close_agent`
- 可选：`interrupt_agent`

**验收**

- 模型可以创建子 Agent 后继续与其交互

### D2. Lua API 分层

**目标**

- Lua 直接暴露低层协作原语，移除旧同步语法糖

**新增 Lua 原语**

- `agent.spawn`
- `agent.send`
- `agent.wait`
- `agent.close`
- `agent.resume`

**验收**

- Lua 能创建、继续、等待、恢复、关闭子 Agent，且仓库内不再使用 `agent.run` / `agent.run_parallel`

### D3. Persona / Role 叠加

**目标**

- 子 Agent 支持 `persona + role` 叠加配置

**当前实现状态**

- child run 首事件会写入 `role`
- worker 的 persona 解析会按 `persona + role` 生成最终执行配置
- role 可覆盖 prompt 追加段、工具策略、budgets、model / credential / reasoning / prompt cache
- repo persona 与 DB persona 都支持 `roles` / `roles_json`

**验收**

- 同一 persona 可以在不同 role 下表现出不同协作风格

---

## 7. Track E -- 治理与安全边界

### E1. 深度与数量限制

**目标**

- 系统级限制递归扩张

**新增限制项**

- `max_subagent_depth`
- `max_active_subagents_per_root_run`
- `max_parallel_children_per_run`
- `max_descendant_count_per_root_run`
- `max_pending_child_runs_per_root_run`

**验收**

- 无论从工具面、Lua、内部流程调用，限制都一致生效

### E2. 预算模型

**目标**

- root run 与 child agent 的成本关系明确

**新增概念**

- root budget
- child budget
- descendant budget

**验收**

- 所有子 Agent 消耗都能回卷到 root run

### E3. 背压与调度降级

**目标**

- 防止 `run_parallel` 把系统拖垮

**策略**

- 并行降级串行
- spawn 拒绝
- 延迟低优先级任务
- 仅允许 resume/close

**验收**

- 当队列积压时，系统行为是可预测的

### E4. 取消传播策略

**目标**

- 父 run 取消时，子 Agent 行为明确

**策略**

- `cascade_all`
- `cascade_running_only`
- `detach_children`

**验收**

- 取消策略可配置且可观测

---

## 8. Track F -- API / Console / 可观测性

### F1. 新增只读 API

**目标**

- 先补调试与观测接口，再考虑是否对终端用户开放写接口

**接口建议**

- `GET /v1/runs/{id}/sub-agents`
- `GET /v1/sub-agents/{id}`
- `GET /v1/sub-agents/{id}/events`

**验收**

- Console 可以只靠 API 还原子 Agent 全部状态

### F2. Console DAG 视图

**目标**

- 展示 run 树与子 Agent 图谱

**最低节点信息**

- role
- persona
- status
- depth
- time
- cost
- output preview

**验收**

- 用户能在单页看清整条协作链

### F3. 事件时间线视图

**目标**

- 可回放 spawn、wait、resume、close 等关键事件

**验收**

- 失败时可以只靠时间线定位问题，不必直接进数据库

---

## 9. 向后兼容策略

当前仓库按单开发者场景推进，不保留旧 `spawn_agent -> output`、旧返回值适配器、旧 persona 兼容层。

- `spawn_agent` 保持结构化 handle 返回
- `agent.run` / `agent.run_parallel` 仅作为当前 Control Plane 的薄语法糖存在
- 需要同步调用时，直接在调用侧显式执行 `spawn + wait + collect`

---

## 10. 每个阶段的最小交付顺序

为了方便 AI 顺序实现，建议最小执行顺序如下：

1. 新建 `SubAgent` 模型与仓储
2. 新建 `sub_agent_events` 与 append helper
3. 将 `spawnChildRun` 的关键生命周期事件接入 `sub_agent_events`
4. 实现 `SubAgentControl.Spawn`
5. 将现有 `spawnChildRun` 收编到 `SubAgentControl`
6. 对齐 `spawn_agent` 结构化 handle
7. 实现 `GetStatus` 与 `ListChildren`
8. 实现 `SendInput`
9. 实现 `Wait`
10. 实现 `Close`
11. 实现 `Resume`
12. 抽离 `ContextSnapshot`
13. 实现 `context_mode`
14. Lua 原语升级
15. 预算与深度治理
16. API 读模型
17. Console 视图

---

## 11. 每个阶段的回归检查点

### 检查点 A

- 新旧 `spawn_agent` 都能创建 child run
- 不影响现有 run 执行链

### 检查点 B

- 子 Agent 终态与 run 终态一致
- A2 当前已覆盖 spawn / queue / run start / terminal 事件
- 剩余交互事件依赖 Track B 控制原语接入

### 检查点 C

- 兼容 Lua `agent.run` / `agent.run_parallel`
- 新增 `agent.spawn` 等接口可用

### 检查点 D

- 超深度、超并发、超预算都能被稳定拦截

### 检查点 E

- Console 能展示完整父子关系和状态流

---

## 12. 实施时需要避免的误区

- 不要把所有新字段继续塞进 `runs`
- 不要把 `spawn_agent` 做成越来越复杂的单函数
- 不要让 Redis Pub/Sub 成为唯一事实源
- 不要只做 UI 展示，不补状态机和控制平面
- 不要让子 Agent 权限比父 Agent 更宽
- 不要默认全量继承父上下文
- 不要在没有治理的前提下放开 `run_parallel`

---

## 13. 最终交付标准

当以下条件全部满足时，本路线图可视为完成：

- Arkloop 内部存在独立的 `SubAgent` 协作实体
- 模型工具面拥有完整的协作原语
- Lua 原语与工具面共享同一控制平面
- 子 Agent 状态机完整可恢复
- 上下文继承是显式策略
- 治理覆盖深度、并发、预算、背压、取消传播
- API 与 Console 可以完整观察协作图谱
- 现有 `spawn_agent` / `agent.run` 保持兼容

---

## 14. 推荐执行方式

后续让 AI 落地时，建议始终按以下顺序推进：

- 先做一个 Track
- 补对应测试
- 更新规格文档
- 再继续下一个 Track

不要跨 Track 大面积同时改动，否则协作状态机和兼容层很容易一起失控。
