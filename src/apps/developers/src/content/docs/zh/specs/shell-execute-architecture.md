---
---

# Shell Execute 设计方案

本文给出 Arkloop `shell_execute` 工具的完整规划与设计。目标不是在现有 `python_execute` 上继续堆参数，而是把 `shell_execute` 明确建模为“带会话的终端工具”。

本轮修订以**中文规格为准**，用于把 P0 已实现基线与 P1 / P2 的长期对象模型重新说清。英文镜像文档后续再补齐，不在这轮一起重写。

结论先行：

- `shell_execute` 必须是**有状态**、**可复用 session**、**基于 PTY** 的终端工具。
- `python_execute` 继续保持**一次性任务工具**语义，不再和 `shell_execute` 共享同一套 session 设计。
- Shell 会话状态与输出产物应当**分桶/分前缀治理**，不建议和 artifacts 混在同一个 bucket 语义层里。
- 需要把“隔离执行环境 session”和“终端 session”区分清楚，但 v1 对 LLM 只暴露一个默认 shell session，避免过度设计。

## 1. 背景与现状

当前实现里，`python_execute` 与 `shell_execute` 共享同一个 sandbox 执行链路：

- Worker 侧统一走 `src/services/worker/internal/tools/builtin/sandbox/executor.go`
- Sandbox 侧统一走 `POST /v1/exec`
- Guest Agent 侧统一走 `executeJob()`
- 当前 session 复用键直接使用 `run_id`
- 当前 artifact 只扫描 `/tmp/output`

现状能跑，但语义上有两个问题：

1. `python_execute` 和 `shell_execute` 被当成了同一类工具
   - 这会让 Python 也隐式共享 run 级 session
   - 与“Python 偏一次性、Shell 偏终端态”这个产品语义不一致

2. Shell 只有“执行命令”，没有“终端会话”
   - 没有 PTY
   - 没有 stdin 续写
   - 没有前后台态
   - 没有输出游标
   - 没有 checkpoint / restore

这正是当前设计的根本缺口。

## 2. 参考结论

参考 `Reverse/agent-reverse-site/packs` 中对 Codex 和 OpenClaw 的分析，可以提炼出几个有效约束：

### 2.1 Codex 侧值得借鉴的点

- `unified exec` 本质上是 **交互式 PTY 执行**，不是普通一次性 `sh -c`
- 有明确的超时、输出上限、kill + drain 机制
- 执行由 orchestration 层统一处理 approval / sandbox / policy
- 输出是流式与增量消费，不是无上限回填

### 2.2 OpenClaw 侧值得借鉴的点

- `exec` 是一个**真正的 session 化执行器**
- 有 background session 管理
- 有 supervisor 处理 overall timeout / no-output timeout
- 有 sandbox 内 workspace layout
- 有状态恢复与上下文治理意识

### 2.3 对 Arkloop 的落地启发

Arkloop 不需要照搬它们的全部工具面，但至少要拿到下面四个核心能力：

- PTY
- session 复用
- 增量输出读取
- 可恢复的文件系统状态

## 3. 目标与非目标

### 3.1 目标

- 提供一个对 Agent 友好的 shell 工具
- 默认自动复用同一 run 内的 shell session
- 支持交互式命令与 stdin 续写
- 支持命令中断、超时、输出游标、后台轮询
- 支持 shell 工作目录与文件系统状态恢复
- 支持将指定目录内容存入 S3
- 保持三端一致：Linux / macOS / Windows 开发环境都能工作

### 3.2 非目标

- 不恢复“正在运行中的进程树”到新 VM/容器
- 不持久化整个 `/tmp`
- 不把整个真实宿主 `home` 无筛选打包上传
- 不在 v1 里支持一个 run 内无限多个命名 shell session

## 4. 关键设计决策

## 4.1 `shell_execute` 与 `python_execute` 必须分流

### 决策

- `python_execute` 保持一次性执行语义
- `shell_execute` 升级为会话型终端语义
- 两者不再共用同一条 `/v1/exec` 协议

### 原因

Python 工具主要解决“运行一段代码并返回结果/产物”，而 Shell 工具主要解决“进入一个连续工作的终端环境”。
这两者在 session、输出、状态恢复、计费、产物管理上都不同。

### 具体落地

- 保留 `POST /v1/exec` 给 `python_execute`
- 新增一组 shell 专用接口给 `shell_execute`
- Worker 内部保留同一工具族没问题，但执行器必须拆开

## 4.2 `shell_execute` 默认只有一个 session，不先暴露多 session

### 决策

v1 中，同一个 run 只暴露一个默认 shell session：

- 内部键：`{run_id}/shell/default`
- 对 LLM：不要求传 `session_id`
- 自动 create-or-attach

### 原因

多 session 会直接把 schema、调度和恢复复杂度拉高一倍，但绝大多数 Agent 场景并不需要“多个 terminal tab”。
先把一个默认终端做好，才是第一性原理下最小且正确的方案。

### 未来扩展

如果后续确实需要多 session，再加：

- `session_name`
- 命名 session 上限
- session list / close API

但不放进 v1 首版。

## 4.3 S3 上必须区分“状态”和“产物”

### 决策

推荐使用同一套 S3 endpoint/credentials，但至少区分两个 bucket：

- `sandbox-artifacts`
  - 用户可见产物
  - 主要用于下载、展示、分享
- `sandbox-session-state`
  - shell session checkpoint
  - 仅内部 restore 使用

如果暂时不想新增 bucket，至少也必须使用完全独立的前缀和生命周期规则，但这只是退路，不是推荐方案。

### 原因

两类数据本质不同：

| 维度 | Artifacts | Session State |
|------|-----------|---------------|
| 面向对象 | 用户 | 系统内部 |
| 生命周期 | 偏长 | 偏短 |
| 下载方式 | API 暴露 | 内部 restore |
| 安全级别 | 可控可下载 | 更敏感 |
| 大小特征 | 小文件为主 | tar/manifest 为主 |
| 清理策略 | 业务保留 | TTL 自动淘汰 |

把这两类数据混到一个 bucket，会让权限、生命周期、审计、清理全部变脏。

## 4.4 不持久化真实 `/tmp`，只持久化受控目录

### 决策

定义 shell 的可持久化目录布局：

- `/workspace`：工作目录，持久化
- `/home/arkloop`：虚拟 home，持久化
- `/tmp/arkloop`：受控临时目录，可选持久化
- `/tmp/output`：产物目录，不并入 session state，而是单独走 artifacts 上传

### 原因

- 整个 `/tmp` 充满无意义中间文件、socket、锁文件、超大缓存
- 整个真实 home 会带来凭据泄露与垃圾文件放大问题
- 用户真正需要恢复的是“工作状态”，不是“所有运行时噪声”

### 推荐映射

- shell 登录后的 `HOME=/home/arkloop`
- 默认 `PWD=/workspace`
- 鼓励工具说明继续让模型把可展示文件写到 `/tmp/output`

## 5. 总体架构

```text
Worker
  └─ shell_execute executor
       └─ Sandbox Shell API
            └─ shell session manager
                 ├─ compute session manager（现有 VM/容器层）
                 ├─ PTY shell supervisor
                 ├─ checkpoint manager
                 └─ artifact collector
                        ├─ sandbox-artifacts
                        └─ sandbox-session-state
```

需要明确两层概念：

### 5.1 Compute Session

现有 `src/services/sandbox/internal/session` 管理的是隔离运行环境：

- Firecracker microVM 或 Docker 容器
- 空闲超时 / 最大存活时间
- org 绑定
- VM 获取与回收

这是“算力会话”。

### 5.2 Shell Session

新增的 Shell Session 管理的是终端语义：

- 一个默认 PTY shell 进程
- 当前 cwd
- 输出 ring buffer
- 前台命令状态
- checkpoint revision
- output cursor

这是“终端会话”。

v1 可把两者做成 1:1 绑定：

- 一个 shell session 对应一个 compute session
- 一个 run 只绑定一个默认 shell

这样实现最简单，也最稳定。

## 6. Worker 侧工具设计

## 6.1 工具语义

`shell_execute` 不再是“只接收一个 command 的一次性命令执行器”，而是一个动作型工具：

- `open`：显式创建 / 挂载 session
- `exec`：执行新命令
- `read`：读取增量输出
- `write`：向当前前台进程写 stdin
- `signal`：发送中断信号
- `close`：关闭 session 并触发 checkpoint

## 6.2 建议的 LLM Schema

```json
{
  "type": "object",
  "properties": {
    "action": {
      "type": "string",
      "enum": ["open", "exec", "read", "write", "signal", "close"]
    },
    "session_mode": {
      "type": "string",
      "enum": ["auto", "new", "resume", "fork"]
    },
    "session_ref": {
      "type": "string"
    },
    "from_session_ref": {
      "type": "string"
    },
    "share_scope": {
      "type": "string",
      "enum": ["run", "thread", "workspace", "org"]
    },
    "command": {
      "type": "string"
    },
    "input": {
      "type": "string"
    },
    "signal": {
      "type": "string",
      "enum": ["SIGINT", "SIGTERM", "SIGKILL"]
    },
    "cwd": {
      "type": "string"
    },
    "timeout_ms": {
      "type": "integer",
      "minimum": 1000,
      "maximum": 300000
    },
    "yield_time_ms": {
      "type": "integer",
      "minimum": 0,
      "maximum": 30000
    },
    "cursor": {
      "type": "integer",
      "minimum": 0
    }
  },
  "required": ["action"],
  "additionalProperties": false
}
```

## 6.3 工具行为约束

### `open`

- `session_mode=new`：显式创建一个新 session，并返回 `session_ref`
- `session_mode=resume`：基于 `session_ref` 挂载已有 session；若 live session 已失效，则尝试 checkpoint restore
- `session_mode=fork`：基于 `from_session_ref` 的最新 checkpoint 创建分支 session
- `session_mode=auto`：优先复用当前 context 绑定的默认 session，不存在则新建

### `exec`

- 若 `session_mode=auto` 且 session 不存在，则 create + restore + start shell
- 若 `session_mode=new`，则先开新 session 再执行命令
- 若 `session_mode=resume`，则必须提供 `session_ref`
- 若 session 空闲，则注入命令并读取输出到 `yield_time_ms`
- 若 session 正忙，则返回 `shell.session_busy`

### `read`

- 不发送新命令
- 从 `cursor` 后读取增量输出
- 用于轮询长任务或交互程序输出

### `write`

- 仅当 session 正忙时允许
- 把 `input` 写到当前前台 PTY
- 可用于 `python`, `mysql`, `ssh`, `top`, `git commit` 等交互程序

### `signal`

- 发送到前台进程组，而不是只发给 shell 本身
- 默认主要支持 `SIGINT`

### `close`

- 强制 checkpoint
- 销毁 compute session
- 释放本地状态

## 6.4 返回结构

```json
{
  "status": "idle",
  "reused": true,
  "session_ref": "shref_01J...",
  "session_scope": "thread",
  "restored": false,
  "cwd": "/workspace/api",
  "output": "...",
  "cursor": 1824,
  "exit_code": 0,
  "running": false,
  "timed_out": false,
  "truncated": false,
  "artifacts": [
    {
      "key": "org/run/shell/default/12/report.txt",
      "filename": "report.txt",
      "size": 1204,
      "mime_type": "text/plain"
    }
  ]
}
```

说明：

- PTY 模式下输出以 `output` 为准，不再强求 `stdout/stderr` 分离
- `cursor` 是增量读取锚点
- `running=true` 表示前台进程仍未退出，需要后续 `read` 或 `write`
- `session_ref` 是对模型可见的引用句柄，不是底层 live session ID

## 7. Sandbox API 设计

建议新增 Shell 专用 API，而不是继续挤进 `/v1/exec`。

## 7.1 内部接口

- `POST /v1/shell/open`
- `POST /v1/shell/exec`
- `POST /v1/shell/read`
- `POST /v1/shell/write`
- `POST /v1/shell/signal`
- `POST /v1/shell/checkpoint`
- `DELETE /v1/shell/session/{id}`

v1 也可以简化成单入口 `POST /v1/shell` + `action`，但服务内实现仍建议按动作拆 handler。

## 7.2 请求体核心字段

```json
{
  "session_id": "run-uuid/shell/default",
  "org_id": "org-uuid",
  "tier": "pro",
  "action": "exec",
  "command": "npm test",
  "input": "",
  "cwd": "/workspace",
  "cursor": 0,
  "timeout_ms": 30000,
  "yield_time_ms": 1000
}
```

## 7.3 Sandbox 内部模块建议

新增：

- `src/services/sandbox/internal/shell/manager.go`
- `src/services/sandbox/internal/shell/session.go`
- `src/services/sandbox/internal/shell/checkpoint.go`
- `src/services/sandbox/internal/http/shell.go`

保留：

- `src/services/sandbox/internal/session/*` 继续管 compute session
- `src/services/sandbox/internal/http/exec.go` 继续服务 Python 一次性执行

## 8. Guest Agent 设计

## 8.1 必须引入 PTY

当前 guest agent 通过 `exec.CommandContext(..., "/bin/sh", "-c", code)` 跑 shell，这不是真正终端。

`shell_execute` 必须新增 PTY 能力：

- 使用 Go PTY 库启动 `/bin/bash -i` 或 `/bin/sh`
- 维持一个长期存活的 shell 进程
- 可向 PTY 写入命令/输入
- 可从 PTY 增量读取输出

## 8.2 推荐状态机

Shell session 仅需三态：

- `idle`
- `running`
- `closed`

状态转移：

- `open` -> `idle`
- `exec` -> `running`
- 命令结束 -> `idle`
- `close` -> `closed`

## 8.3 命令包裹策略

为了拿到 exit code 和最终 cwd，向空闲 shell 注入命令时，使用 marker 包裹：

```sh
printf '__ARK_BEGIN__<id>\n'
<user command>
ark_rc=$?
printf '\n__ARK_END__<id>__RC=%s__PWD=%s\n' "$ark_rc" "$PWD"
```

注意点：

- marker 需要唯一且低碰撞
- 需要安全转义，避免被用户命令截断
- 对交互式命令也成立，只是 end marker 会在进程退出后才出现

## 8.4 输出缓存

每个 shell session 维护一个 ring buffer：

- 总缓存上限：1 MiB
- 单次返回上限：64 KiB
- 通过 `cursor` 返回增量片段
- 超限时设置 `truncated=true`

这样能避免把整个终端历史反复塞回模型上下文。

## 8.5 超时与中断

- `timeout_ms`：本次动作等待上限
- 超时后不直接销毁整个 shell session
- 超时动作默认先发 `SIGINT`
- 若 2 秒内未退出，再升级 `SIGKILL`
- kill 后继续 drain 一小段输出，再回包

## 9. Session 复用与恢复

## 9.1 复用策略

v1 采用最简单且稳定的规则：

- 同一 `run_id` 的所有 `shell_execute` 调用，复用同一个 default shell session
- 同一 `run_id` 的 `python_execute` 不复用 shell session
- `python_execute` 也不应继续默认复用 run 级 session

## 9.2 checkpoint 触发时机

### 同步触发

- `close`
- idle eviction 前
- max lifetime eviction 前

### 异步触发

- 每次命令执行完成后，如果文件系统 dirty，则 debounce 后做 checkpoint

建议 debounce 3~5 秒，避免一串小命令频繁上传。

## 9.3 restore 时机

首次 `shell_execute`：

1. 先查 live session
2. 没有则查 `sandbox-session-state`
3. 有 checkpoint 则 restore 到新 compute session
4. 启动默认 shell 并恢复 `cwd`

## 9.4 不恢复运行中进程

这个边界必须明确：

- 能恢复文件系统
- 能恢复 cwd
- 能恢复 shell 历史和环境文件
- 不能恢复之前还在跑的后台进程或前台交互进程

也就是说，checkpoint/restore 是“工作区恢复”，不是“进程快照恢复”。

## 9.5 AI 能不能主动开新 Session

可以，而且应该支持，但必须是**显式语义**，不能完全靠系统暗推。

推荐规则：

- 默认情况下：`session_mode=auto`
  - 同一 context 内优先复用默认 session
  - 不存在则创建一个默认 session
- 当模型明确想开新环境时：`session_mode=new`
  - 返回新的 `session_ref`
  - 后续模型可继续持有并显式复用
- 当模型想接回旧环境时：`session_mode=resume` + `session_ref`
- 当模型想从旧环境“复制一份分支”时：`session_mode=fork` + `from_session_ref`

这四个模式把“新建、默认复用、显式复用、分叉”都覆盖了。

## 9.6 为什么不能把底层 Session ID 直接暴露给模型

因为真正的 live session ID 是易失态资源标识，不适合成为跨 context 的长期引用：

- 它可能已经被 idle reclaim 回收
- 它可能绑定在某个 sandbox 实例内，跨节点不可直接挂载
- 它不适合直接放进 memory，安全边界太粗
- 它表达不了“live attach”和“checkpoint restore”这两种不同复用语义

因此需要区分四层标识：

- `sandbox carrier`
  - 临时计算载体
  - 负责运行 guest、挂载文件树、承载 live 进程
  - 不是长期身份对象

- `live_session_id`
  - 仅 sandbox 内部使用
  - 指向当前活着的 PTY / compute session
- `session_ref`
  - 对模型暴露的稳定引用
  - 指向一个 shell session 记录
- `profile_ref`
  - 对用户长期环境暴露的稳定引用
  - 承载 `/home/arkloop`、skills、用户缓存与个人工具配置
- `workspace_ref`
  - 对项目/任务长期工作树暴露的稳定引用
  - 承载 `/workspace` 下的 repo、上传目录、生成代码与项目缓存

模型、memory、workspace 文件里都不应该存 `live_session_id`，而应该存 `session_ref`、`profile_ref`、`workspace_ref` 这些稳定引用。

## 9.7 跨 Context / 跨 Thread 复用怎么做

这里要区分两种复用：

### 热复用

- 目标：直接接回一个还活着的终端
- 条件：对应 live session 仍存在，且调用方有权限
- 效果：保留当前 shell、cwd、前台命令状态、ring buffer cursor

### 冷复用

- 目标：live session 已不存在，但恢复它的工作区
- 条件：存在最新 checkpoint
- 效果：恢复 guest 挂载视图下的文件系统、cwd、shell 启动状态
- 不恢复旧进程树

这里的“恢复工作区”要理解成：

- 恢复某个 `profile_ref` 投影出的 `/home/arkloop`
- 恢复某个 `workspace_ref` 投影出的 `/workspace`
- 必要时恢复受控的 `/tmp/arkloop`

而不是“恢复一整台持久 sandbox”。

跨 thread 真正可依赖的是**冷复用优先、热复用命中算加速**。

也就是说，跨 context 的正确语义不是“永远连回原进程”，而是：

1. 用 `session_ref` 找 session registry
2. 若 live session 还活着，则 attach
3. 若 live session 已失效但 checkpoint 存在，则 restore
4. 若两者都不存在，则返回 expired/not_found

## 9.8 Memory 和 Workspace 能不能存 Session

能，但存的应该是**可恢复引用**，不是 live session ID。

### (a) 写入 Memory

可行，但只适合作为“召回线索”，不适合作为唯一真相来源。

原因：

- memory 检索是语义召回，不是强一致 KV
- 模型可能记到过期 ref
- 可能召回到不再有权限的 ref

因此 memory 里适合存：

- `session_ref`
- `profile_ref`
- `workspace_ref`
- 使用说明，例如“这个 workspace 是 billing 仓库”“这个 profile 装了内部技能集”

但真正 lookup 仍要回到 session registry。

### (b) 写入 Workspace

也可行，而且比 memory 更适合作为确定性入口。

可以在 workspace 或 profile 对应的机器可读元数据里维护入口，例如：

```json
{
  "default_profile_ref": "pref_01J...",
  "default_shell_session_ref": "shref_01J...",
  "default_workspace_ref": "wsref_01J..."
}
```

这样下次同一个 agent 读取稳定引用目录时，可以明确知道：

- 当前用户环境是谁
- 当前默认工作树是谁
- 当前默认 shell 环境是谁

但这里也一样：

- 文件里存的是 `session_ref` / `profile_ref` / `workspace_ref`
- 真实状态仍以 registry 为准

## 9.9 这对 Execute 定义的直接影响

这会直接影响 `shell_execute` 的定义：它不能只定义成“执行一条 shell 命令”，而必须定义成“在某个长期环境投影下，对某个 session 引用执行动作”。

因此从协议层，`shell_execute` 未来至少要保留这些能力：

- 指定是自动复用还是新建
- 指定显式 `session_ref`
- 指定从哪个旧 session 分叉
- 返回新的 `session_ref`

所以即使 v1 UI 和默认策略仍然主打“当前 thread 一个默认 shell”，协议层也必须预留：

- `session_mode`
- `session_ref`
- `from_session_ref`
- `share_scope`

这样后续接 memory / profile / workspace / cross-thread reuse 时，不需要再推翻工具定义。

## 9.10 Shell Session Orchestrator（中间层）

到这里为止，`shell_execute` 已经不只是一个 sandbox 工具，而是一条独立的执行链路。
因此还需要一个明确的中间层：`Shell Session Orchestrator`。

这一层的职责不是执行命令，而是做“引用解析 + 权限校验 + 复用决策 + 并发控制 + 结果治理”。

它对应的参考物是：

- Codex 的 `ToolOrchestrator`
  - 把 approval、sandbox、retry 从具体工具实现里抽走
- OpenClaw 的 policy pipeline / `before_tool_call` / supervisor
  - 把工具调用前后的治理逻辑从 exec 本体里抽走

Arkloop 如果没有这一层，后续会把逻辑错误地堆进：

- worker executor
- sandbox HTTP handler
- guest agent

这样很快就会失控。

### 推荐职责边界

`Shell Session Orchestrator` 负责：

- 解析 `session_mode` / `session_ref` / `from_session_ref`
- 解析默认绑定（run / thread / workspace）
- 校验 `share_scope` 和 ACL
- 决定是 `attach live`、`restore checkpoint`、`new` 还是 `fork`
- 申请 / 续租 / 释放 session lease
- 调用 sandbox shell API
- 统一返回 `resolved_via`、`restored`、`reused` 等执行元数据
- 在返回模型前做输出裁剪与结构化摘要

### 建议放置位置

- Worker 侧：
  - `src/services/worker/internal/tools/builtin/shell/orchestrator.go`
  - `src/services/worker/internal/tools/builtin/shell/registry.go`
  - `src/services/worker/internal/tools/builtin/shell/lease.go`

`shell_execute` executor 自己应尽量薄，只负责参数校验和 orchestrator 调用。

## 9.11 Session Registry（真相源）

如果未来要支持跨 thread、跨 context、跨时段复用，那么 Memory 和 Workspace 都不能作为唯一真相来源。
真正的真相来源必须是 `Session Registry`。

### 最小表结构建议

建议新增一张最小表，例如 `shell_sessions`：

- `id`
- `session_ref`
- `workspace_ref`
- `org_id`
- `project_id`
- `thread_id`
- `run_id`
- `share_scope`
- `state`
- `live_session_id`
- `live_node_id`
- `latest_checkpoint_rev`
- `default_binding_key`
- `lease_owner`
- `lease_expires_at`
- `last_used_at`
- `created_at`
- `updated_at`
- `metadata_json`
- `last_error`

### 字段语义

- `session_ref`
  - 对模型暴露的稳定引用
  - 全局唯一
- `workspace_ref`
  - 对工作区暴露的稳定引用
  - 多个 session 可以共享一个 workspace_ref
- `live_session_id`
  - 当前活着的 sandbox shell session 标识
  - 可为空
- `default_binding_key`
  - 用来表示“谁的默认 shell”
  - 例如 `run:{run_id}`、`thread:{thread_id}`、`workspace:{workspace_ref}`
- `state`
  - 建议枚举：`ready`、`busy`、`checkpointing`、`restoring`、`expired`、`closed`

### 最小约束

- `session_ref` 唯一
- `org_id + default_binding_key` 可索引
- `state != closed` 的记录可以被复用
- `latest_checkpoint_rev` 为空时表示还不能冷恢复

### 为什么一定要有 Registry

因为它解决了三个问题：

- 模型持有的是引用，不是资源实例
- Memory / Workspace 只能提供线索，真正 attach/restore 要回到真相源
- 可以把“活体状态”和“持久状态”统一映射到一个稳定引用上

## 9.12 默认绑定解析规则

`session_mode=auto` 必须有固定的解析顺序，不能每个调用点各猜各的。

推荐优先级：

1. 若传了显式 `session_ref`，按显式引用处理
2. 若 `session_mode=fork`，必须使用 `from_session_ref`
3. 命中当前 `run` 的默认 session
4. 命中当前 `thread` 的默认 session
5. 命中当前 `workspace` 的默认 session
6. 都未命中，则创建一个新 session

### 严格语义

- `resume`
  - 找不到就返回 `shell.session_not_found` 或 `shell.session_expired`
  - 不允许偷偷新建
- `fork`
  - 找不到源 session 或源 checkpoint 时直接报错
- `auto`
  - 唯一允许“找不到时自动新建”的模式

### 建议回包字段

为了方便上层调试与后续策略学习，建议增加：

- `resolved_via`
  - `explicit_resume`
  - `run_default`
  - `thread_default`
  - `workspace_default`
  - `new_session`
  - `fork_from_checkpoint`
- `attached_live`
- `restored_from_checkpoint`

## 9.13 Lease 与并发模型

终端和 Python 最大的不同，是 PTY 本质上是一个共享且有顺序语义的交互资源。

因此 shell session 必须有 lease。

### 核心规则

- 同一时刻只允许一个 writer 持有 session lease
- `exec` / `write` / `signal` / `close` 需要 writer lease
- `read` 可以不拿 writer lease，但必须做 ACL 校验
- busy 状态下的第二个 writer 请求默认返回 `shell.session_busy`
- 如果业务想保留两边工作，应引导走 `fork`

### lease 字段

最小需要：

- `lease_owner`
- `lease_expires_at`
- `lease_epoch`

### lease 行为

- `exec` 开始时申请 lease
- 命令运行期间周期性 heartbeat 续租
- 命令结束后释放 lease，或缩短为短租约
- 调用方崩溃时，lease 到期后可被回收

### 为什么这一步不能省

不做 lease，会直接出现：

- 两个 thread 同时往同一个 PTY 写入
- 输出互相污染
- 中断信号打到别人命令上
- “复用了旧 session，但行为完全错乱”

这类问题一旦出现，后面几乎无法靠补丁补回来。

## 9.14 `share_scope` 与 ACL 规则

`share_scope` 不能只是一个展示字段，它必须进入权限模型。

### 建议语义

- `run`
  - 仅当前 run 可见
- `thread`
  - 同一 thread 下后续 run 可见
- `workspace`
  - 同一 workspace / project 下多个 thread 可见
- `org`
  - 组织内共享
  - 默认应通过配置关闭

### ACL 判定建议

当请求方想 attach 一个 `session_ref` 时，至少校验：

- `org_id` 是否一致
- 若 `share_scope=run`，`run_id` 是否一致
- 若 `share_scope=thread`，`thread_id` 是否一致
- 若 `share_scope=workspace`，是否属于同一 workspace/project
- 若 `share_scope=org`，调用方是否具备组织内共享权限

### 重要原则

- Memory 召回到一个 `session_ref`，不代表一定有权限 attach
- Workspace 文件里写了一个 `session_ref`，也不代表一定能 attach
- 权限判断必须始终以 registry + 当前调用上下文为准

## 9.15 输出进入模型上下文的治理

这是最容易被低估、但长期最容易出问题的部分。

OpenClaw 在这一层做得比较完整：

- tool-result guard
- context pruning
- compaction safeguard
- provenance

Arkloop 的 `shell_execute` 也必须有自己的输出治理规则。

### 建议做两层输出

#### 运行层完整输出

用于：

- session ring buffer
- `read(cursor)` 增量拉取
- 调试与审计

#### 模型层可见输出

用于：

- 本次 tool result 回填给模型
- run event 持久化

这两层不应完全等价。

### 建议规则

- 单次 tool result 进入模型上下文的正文上限应小于内部 ring buffer 上限
- 超限时保留：
  - 尾部关键信息
  - exit code
  - cwd
  - 截断标志
  - 下一步如何继续读输出
- 历史 shell 输出不应在后续每轮都原样重放
- 当 shell 输出很长时，应允许把旧输出折叠为结构化摘要

### 建议返回结构中稳定保留这些字段

- `session_ref`
- `resolved_via`
- `cwd`
- `exit_code`
- `duration_ms`
- `running`
- `cursor`
- `truncated`
- `artifact_count`

### provenance 建议

每条 shell 执行记录都应有 provenance，至少包括：

- 哪个 `session_ref`
- 哪个 `profile_ref`
- 哪个 `workspace_ref`
- 哪个 `thread_id` / `run_id`
- 是否 `attach live`
- 是否 `restore checkpoint`
- 输出是否被截断或摘要化

这样未来做 compaction 或审计时，才不会丢失“这段输出从哪来”的信息。

## 9.16 `shell_execute` 的未来兼容边界

为了不在第二版再推翻第一版协议，建议现在就把边界钉死：

- 协议层允许显式 `open`
- 协议层允许显式 `session_mode`
- 协议层允许显式 `session_ref` / `from_session_ref`
- 系统内部保留 `profile_ref` / `workspace_ref`
- 执行前必须经过 orchestrator 和 registry，而不是直接打 sandbox

这几条一旦定下，后续无论是：

- thread 内默认复用
- cross-thread reuse
- 从 memory 召回旧 session
- 从 workspace 文件恢复默认 session
- 子 agent 分叉 shell 环境

都不需要推翻 `shell_execute` 的定义，只需要扩展中间层策略。

## 10. S3 状态设计

## 10.1 Bucket 规划

推荐：

- `sandbox-artifacts`
- `sandbox-session-state`
- 继续保留已有 `sandbox-snapshots`

三者职责分明：

- snapshots：模板级 VM 快照
- session-state：当前 run 级 shell checkpoint 状态
- artifacts：展示/下载产物

这里描述的是**当前 guest 快照布局**，不是最终长期对象身份模型。

也就是说：

- 当前实现里 `/workspace`、`/home/arkloop`、`/tmp/arkloop` 会一起进入 shell checkpoint
- 但长期看，它们不应继续被统称成一个“持久 sandbox”对象
- 后文的 P1 会把它们重新拆成 `profile_ref`、`workspace_ref`、`session_ref` 三层

## 10.2 session-state 对象布局

```text
sandbox-session-state/
  {org_id}/{run_id}/shell/default/
    latest.json
    checkpoints/
      000001/
        manifest.json
        home.tar.zst
        workspace.tar.zst
        temp.tar.zst
      000002/
        ...
```

`manifest.json` 包含：

- revision
- created_at
- cwd
- file digests
- tar sizes
- last_command_seq
- shell metadata version

## 10.3 artifacts 对象布局

```text
sandbox-artifacts/
  {org_id}/{run_id}/shell/default/{command_seq}/{filename}
```

这样每次命令产生的 artifacts 都是不可变的，不会把旧结果覆盖掉。

## 10.4 持久化目录清单

### 持久化

- `/workspace`
- `/home/arkloop`
- `/tmp/arkloop`（可配置开关）

### 不持久化

- `/tmp/output`（转 artifacts）
- `/proc`
- `/sys`
- `/dev`
- socket / fifo / device file

## 10.5 为什么不把 home 和 output 放同一个 bucket

因为两者不是同一类资产：

- `home` 是 session state
- `output` 是 artifacts

它们在权限、生命周期、清理、访问方式上完全不同。
把它们放在同一个 bucket 不是“简单”，而是把两个边界糊掉。

## 11. 安全设计

## 11.1 org 隔离

沿用当前 session org 绑定规则：

- shell session 创建时绑定 `org_id`
- restore 时必须校验 prefix 中的 org
- delete / close / read / write 全部做 org 校验

## 11.2 路径安全

checkpoint 与 restore 时：

- 仅允许白名单根目录
- 拒绝路径穿越
- 拒绝恢复到根外路径
- 过滤 symlink/hardlink 逃逸
- 丢弃 device / fifo / socket

## 11.3 环境变量安全

Shell 默认环境不能直接透传宿主敏感变量。

应保留最小集：

- `HOME`
- `PATH`（固定白名单）
- `LANG`
- `TERM`
- 必要的 Arkloop 内部标记

必须剔除：

- 云凭据
- 数据库连接串
- 服务间 token
- 宿主 PATH 覆盖

## 11.4 配额

建议配置项：

- session state 总大小上限
- 单目录 tar 大小上限
- 单 artifact 大小上限
- 单 session artifact 数量上限
- shell ring buffer 上限
- 每 org 并发 shell session 上限

## 12. 计费与资源治理

`shell_execute` 不应直接沿用纯一次性的 Python 计费语义。

建议：

- `session_open_base_fee`
- `command_runtime_fee`
- `restore_base_fee`

v1 可以先简化为：

- 每次 `exec` 仍按命令执行时长计费
- 首次 open / restore 加一次基础费
- idle 存活不单独计费，由 idle timeout 控制成本

这样足够简单，也不会扭曲使用方式。

## 13. 日志与观测

日志必须保持结构化，不能出现需求说明式自然语言。

建议核心字段：

- `org_id`
- `run_id`
- `shell_session_id`
- `action`
- `status`
- `duration_ms`
- `exit_code`
- `artifact_count`
- `checkpoint_revision`
- `restored`
- `timed_out`
- `buffer_truncated`

指标建议：

- shell session create/reuse/restore 次数
- checkpoint 成功率与耗时
- artifact 上传字节数
- active shell session 数
- idle reclaim 次数
- restore miss 次数

## 14. 迁移路径

## 阶段 1：语义拆分

- `python_execute` 继续走 `/v1/exec`
- `shell_execute` 切换到新协议
- Worker 不再用 `run_id` 给 Python 做隐式 session 复用

## 阶段 2：PTY Shell Session

- guest agent 引入 PTY
- sandbox service 增加 shell manager
- worker schema 改为 action 模型

## 阶段 3：Checkpoint / Restore

- 新增 `sandbox-session-state`
- 实现 manifest + tar.zst 上传
- idle close 前同步 checkpoint

## 阶段 4：Artifacts 增量化

- shell 命令完成后仅上传新增/变更文件
- artifacts key 带 `command_seq`

## 阶段 5：治理与打磨

- 配额
- 指标
- 日志
- 失败回退策略

## 15. 测试计划

必须覆盖：

### 单元测试

- shell action schema 校验
- busy/idle 状态机
- marker 解析
- cursor 增量读取
- timeout -> signal -> kill -> drain
- checkpoint manifest 生成
- restore 路径过滤

### 集成测试

- `cd` 后下一条命令 cwd 保持
- `export FOO=bar` 后可复用
- `touch /workspace/a.txt` 后 restore 仍存在
- `/tmp/output` 文件可上传为 artifacts
- idle reclaim 后再次 attach 能 restore
- org mismatch 被拒绝

### 回归测试

- `python_execute` 不再继承 shell 状态
- 现有 artifact API 仍可正常下载
- sandbox pool 行为不被破坏

## 16. 最终建议

最终建议很明确：

1. **把 `shell_execute` 从一次性命令执行器升级为默认单 session 的 PTY 终端工具**
2. **把 `python_execute` 保持为一次性工具，不再共享 shell session 语义**
3. **把 session state 和 artifacts 拆成两个 S3 bucket**
4. **只持久化 `/workspace`、虚拟 home、受控 temp，不持久化整个真实 `/tmp`**
5. **v1 不做多 terminal tab，不恢复进程，只恢复工作区状态**

这套方案和当前 Arkloop 架构是兼容的：

- 复用现有 compute session manager
- 复用现有 object store 能力
- 复用现有 artifact API
- 只在 shell 这条链路上引入 PTY、checkpoint、restore

它也和参考对象的核心经验一致：

- 学 Codex 的 PTY / 输出治理 / 中断回收
- 学 OpenClaw 的 session 化与 supervisor 思路
- 但不照搬它们的多 host、多模式复杂度

这已经足够支撑一个企业级、可恢复、可治理的 `shell_execute`。

## 17. 落地 Roadmap

下面这份 roadmap 不是按“今天做什么、明天做什么”来排，而是按依赖关系来排。
顺序不要打乱：Phase 2 依赖 Phase 1 的 live PTY API，Phase 3 依赖 Phase 2 的默认 session 语义，Phase 4 依赖 Phase 3 的 state metadata，Phase 5 负责把前面四步收口成完整架构。

### Phase 1：Sandbox live PTY Shell API（已完成）

这一阶段对应最近提交：`19ccde43 feat(sandbox): add live pty shell api`。

- 已落地的核心内容：
  - `src/services/sandbox/agent/shell_controller.go`：新增 PTY shell 控制器，支持 `open / exec / read / write / signal / close`
  - `src/services/sandbox/internal/shell/manager.go`：新增 shell manager，负责 session 绑定、输出游标、artifact 收集、org 校验
  - `src/services/sandbox/internal/http/shell.go`：新增 `/v1/shell/*` HTTP API
  - `python_execute` 保持走原有 `/v1/exec`
  - 已补齐 PTY 行为与 manager 生命周期测试
- 这一阶段的交付边界：
  - sandbox 侧已经具备 live shell 基础设施
  - 但 Worker 和 LLM 还没有正式切到“会话型 shell 工具”语义
  - checkpoint / restore、state bucket、完整治理也都还没落地
- 完成标准：
  - sandbox API 能独立跑通 `open -> exec -> read/write/signal -> close`
  - `python_execute` 没有被拖进 PTY session 语义

### Phase 2：Worker 接入新协议，把 `shell_execute` 变成真正可用的会话工具

- 目标：
  - 让 Agent 通过 `shell_execute` 直接使用当前 live PTY API
  - 把 Shell 从“一次性命令”切成“默认单 session 的终端工具”
- 要改哪里：
  - `src/services/worker/internal/tools/builtin/sandbox/spec.go`
  - `src/services/worker/internal/tools/builtin/sandbox/executor.go`
  - `src/services/worker/internal/tools/builtin/sandbox/executor_test.go`
  - 面向开发者的工具说明与示例
- 怎么做：
  1. 把 `shell_execute` 的 LLM schema 改成 action 模型，至少支持 `open | exec | read | write | signal | close`
  2. Worker 内部固定默认 session key 为 `{run_id}/shell/default`，不要把 `session_id` 暴露给 LLM
  3. `shell_execute` 的请求正式改走 `/v1/shell/open`、`/v1/shell/exec`、`/v1/shell/read`、`/v1/shell/write`、`/v1/shell/signal`、`DELETE /v1/shell/session/{id}`
  4. 统一结果结构：`status`、`cwd`、`output`、`cursor`、`running`、`timed_out`、`truncated`、`exit_code`、`artifacts`
  5. 补齐工具使用约束，让 Agent 明白：长任务先 `exec` 再 `read`，交互提示走 `write`，中断走 `signal`
- 这一阶段不要做：
  - 不引入多 session
  - 不做 checkpoint / restore
  - 不做厚重兼容层；如果必须平滑切换，最多只保留一层很薄的 `command -> action=exec` 过渡
- 完成标准：
  - 同一 run 内多次调用 `shell_execute` 时，`cd`、`export`、交互式前台进程都能保持上下文
  - `python_execute` 继续保持一次性执行语义，不继承 shell 状态
  - Worker 侧单元测试与至少一条端到端集成链路跑通

### Phase 3：Checkpoint / Restore，把“会话”补成“可恢复会话”

- 目标：
  - compute session 因 idle timeout 或回收消失后，shell 工作状态仍能恢复
- 要改哪里：
  - `src/services/sandbox/internal/shell/`：新增 checkpoint / restore 组件
  - `src/services/sandbox/internal/session/manager.go`：增加 shell 回收钩子，或提供等价的 pre-delete 机制
  - 对象存储配置：增加 `sandbox-session-state` bucket，或至少增加严格隔离的 state 前缀
- 怎么做：
  1. 固定可持久化目录只包含 `/workspace`、`/home/arkloop`、`/tmp/arkloop`
  2. 定义 manifest，记录 revision、cwd、环境快照、`last_command_seq`、artifact metadata、shell metadata version
  3. 在 idle reclaim 和显式 `close` 前先做 checkpoint，再销毁 compute session
  4. 新 compute session 执行 `open` 时，如果命中最近 revision，则先 restore，再启动默认 shell
  5. restore 必须严格做白名单和路径安全校验，拒绝路径穿越、symlink/hardlink 逃逸、device/fifo/socket
- 关键原则：
  - 只恢复文件系统状态，不恢复进程树
  - checkpoint 失败不能静默吞掉，必须有结构化日志和明确状态
- 完成标准：
  - `touch /workspace/a.txt`、`export FOO=bar`、`cd /workspace/demo` 后，即使 session 被回收，再次 `open` 也能恢复
  - org mismatch、非法路径、损坏 manifest 都会被拒绝或安全降级

### Phase 4：Artifacts 与 Session State 彻底拆开，并把上传变成可持续的增量语义

- 目标：
  - 把“用户可见产物”和“内部恢复状态”从对象存储、命名和生命周期上彻底拆开
- 要改哪里：
  - `src/services/sandbox/internal/shell/artifacts.go`
  - shell metadata / manifest 结构
  - 对象存储配置与清理策略
- 怎么做：
  1. `sandbox-artifacts` 继续承载 `/tmp/output`
  2. `sandbox-session-state` 只承载 checkpoint
  3. artifact key 固定为 `{org_id}/{run_id}/shell/default/{command_seq}/{filename}`
  4. 把 `command_seq` 和已上传 artifact 版本信息写进 session metadata，避免 restore 后把旧产物重复上报
  5. 明确 TTL：session state 走短生命周期，artifacts 按业务保留
- 这一阶段解决的问题：
  - 产物下载与 session 恢复解耦
  - 权限边界清晰
  - restore 后不会把历史 `/tmp/output` 当成新结果再次上报
- 完成标准：
  - 同一 shell session 连续执行多条命令时，artifacts 只追加新结果
  - restore 后 `command_seq` 继续递增，历史结果保持不可变 key

### Phase 5：治理、观测、回归和发布收口

- 目标：
  - 把前四阶段从“功能可用”收口到“可长期运行”
- 要改哪里：
  - sandbox / worker 日志与 metrics
  - 配额配置
  - 集成测试与回归测试
  - 开发者文档、tool 使用示例、persona 指导
- 怎么做：
  1. 增加配额：session state 总大小、单 artifact 大小、artifact 数量、ring buffer、每 org 并发 shell session
  2. 补齐结构化日志字段：`org_id`、`run_id`、`shell_session_id`、`action`、`status`、`duration_ms`、`exit_code`、`artifact_count`、`checkpoint_revision`、`restored`、`timed_out`、`buffer_truncated`
  3. 补齐指标：shell open/reuse/restore、checkpoint 成功率、active shell sessions、idle reclaim、restore miss、artifact 上传字节数
  4. 跑完整验证：单元测试、集成测试、回归测试，覆盖 Linux / macOS / Windows 开发路径
  5. 更新开发者站文档和工具示例，让 Agent 清楚何时 `read`、何时 `write`、何时 `close`
- 整体完成标准：
  - `shell_execute` 已经是默认单 session 的 PTY 终端工具
  - `python_execute` 与 shell 会话完全分流
  - idle reclaim 后可以恢复工作区状态
  - artifacts 与 session state 已按边界分桶或分前缀治理
  - 配额、日志、指标、回归测试齐全，可以作为正式架构长期运行

### 任务闭环定义

到 Phase 5 结束，这个大任务才算完成。判断标准不是“API 已存在”，而是下面这条链路全部成立：

1. Agent 通过 `shell_execute` 进入默认 shell
2. 多次 `exec / read / write / signal` 共享同一终端上下文
3. compute session 回收后可以 restore 工作区状态
4. `/tmp/output` 产物稳定上传，且不会和 session state 混淆
5. 系统具备 org 隔离、配额、日志、指标与回归保障

## 18. 追加说明：在当前架构基础上继续演进的最终方案

本节是**追加说明**，不是对前文的覆盖性重写。

原因很明确：

- 前文 Phase 1 ~ Phase 5 描述的是当前 Arkloop `shell_execute` 已有实现与近期演进路径
- 这些内容仍然是理解现有代码、理解兼容边界、理解当前 run 级 shell 语义的基础
- 但如果 Arkloop 的最终目标不只是“同一 run 内复用 shell”，而是继续走向：
  - skills market
  - Git 仓库导入
  - 文件夹上传
  - 跨 run 持续工作
  - 用户长期个人环境
  - 多 agent / sub-agent 协作执行
  那么仅按前文 Phase 1 ~ Phase 5 直接外推，会继续留下更大的技术债

因此，从本节开始，需要把目标重新提升为：

**不是把 `shell_execute` 做成更强的一次性终端工具，而是把 Arkloop 做成一个以 `profile_ref / workspace_ref / session_ref` 为长期对象、以 unified exec 为执行内核、以多类 registry 为真相源的长期执行系统。**

这也是后续 P0 / P1 / P2 的真正定义基础。

---

## 19. 最终目标重新定义

### 19.1 真正的一等公民不是 shell，也不是“持久 sandbox”

从长期看，Arkloop 需要承载的不是“一个会话型 bash”，也不是“一整台永久存在的 sandbox 电脑”，而是一组**稳定的长期对象**。

这组对象至少要同时支持：

- Agent 在同一长期环境里连续运行 shell / python / build / test / analysis
- 用户个人环境长期存在，不随某个 run 或某个项目目录消失
- 仓库通过 `git clone` 进入工作树，并可被不同 run 继续使用
- 本地文件夹通过上传进入工作树
- 生成的中间文件、缓存、脚本、配置能按边界落到正确位置
- shell session 被回收后，长期环境仍可继续使用

因此：

- shell session 不是最终持久对象
- checkpoint 也不是最终持久对象
- sandbox 只是临时计算载体
- **真正的一等公民是 `profile_ref`、`workspace_ref`、`session_ref` 这三类稳定对象**

### 19.2 长期对象模型：`profile_ref` / `workspace_ref` / `session_ref`

为避免后续继续把 guest 路径、长期资产与 live shell 混成一个名词，必须把长期模型固定拆开：

- `profile_ref`
  - 用户环境 profile 的稳定引用
  - 这里说的 profile 是**用户环境 profile**，不是 agent config profile
  - 承载 `/home/arkloop`、skills、用户缓存、git 配置与个人工具状态
- `workspace_ref`
  - 项目或任务工作树的稳定引用
  - 承载 `/workspace` 下的 repo、上传目录、代码生成结果与项目缓存
- `session_ref`
  - 活动终端与执行视角的稳定引用
  - 承载 PTY、前台进程、cwd、env、continuation 状态
- `sandbox carrier`
  - 临时 guest 载体
  - 负责把某个 `profile_ref` 和某个 `workspace_ref` 挂载成当前执行视图
  - 自己不是长期身份对象

这样 guest 内仍然保持固定路径：

- `/home/arkloop`
- `/workspace`
- `/tmp/arkloop`

变的是挂载来源，不是路径命名。

### 19.3 `shell_execute` 的未来定位

未来的 `shell_execute` 不应继续被理解为：

- “执行一条 shell 命令并返回结果”

而应被理解为：

- “在某个 `profile_ref + workspace_ref` 投影出的长期环境里，通过一个 `session_ref` 执行动作”

这意味着：

- shell 是 attach 点，不是长期资产中心
- session 是活动执行视角，不是工作区本体
- artifacts 是结果产物，不等于长期环境
- checkpoint 只是恢复机制的一环，不是唯一真相源

### 19.4 最终目标一句话版本

最终目标应定义为：

**Arkloop Execute System = 以 `profile_ref / workspace_ref / session_ref` 为核心对象的长期执行系统；`shell_execute` 只是该系统对 LLM 暴露的一种交互式执行入口。**

---

## 20. 正式执行模型：采用 unified exec 内核

### 20.1 推荐锁定的核心接口

综合 Codex / OpenClaw / Gemini CLI 等设计经验，Arkloop 后续内部正式执行内核应采用：

- `exec_command`
- `write_stdin`

其中：

- `exec_command`
  - 负责启动进程
  - 负责初始等待
  - 负责决定同步完成还是进入后台 session
- `write_stdin`
  - 负责后续 stdin 写入
  - 当 `chars=""` 或省略时，固定语义为 poll / long-poll

### 20.2 为什么不继续把未来能力堆在 `open|exec|read|write|signal|close`

当前 Arkloop 的 `shell_execute` 已经使用 action 模型，这是 Phase 2 的正确过渡。

但从长期看，如果还继续把后续能力直接堆到：

- `open`
- `exec`
- `read`
- `write`
- `signal`
- `close`

会产生几个问题：

1. 模型会继续把 shell 当作“手动轮询设备”
2. `read` 与 `poll` 的消费语义不够稳定，未来容易和 transcript / UI / audit 混淆
3. 工作区 attach / resume / fork / share_scope / lease 这些长期能力，不适合继续塞进已有 action 模型里硬扩展
4. Worker、Sandbox、Guest Agent 都会继续堆 orchestrator 逻辑，边界越来越脏

因此正确做法是：

- 对模型生态短期保留 `shell_execute`，保证兼容
- 但在 Worker ↔ Sandbox 的正式执行内核上，逐步收敛到 `exec_command + write_stdin`

### 20.3 对外兼容策略

短期对 LLM 仍可以暴露 `shell_execute`，但内部应通过薄适配层映射：

- `shell_execute(action=exec)` -> `exec_command`
- `shell_execute(action=write)` -> `write_stdin(chars=... )`
- `shell_execute(action=read)` -> `write_stdin(chars="")`
- `open / close / signal` 保留为兼容控制动作，但不再作为未来能力扩展的主要方向

这能保证：

- 当前功能不被推翻
- 未来执行模型能自然演进
- P1 / P2 不必继续建立在过时 action 语义上

---

## 21. 输出模型必须重构为三层

### 21.1 三层输出模型

正式执行内核必须固定保存三层输出：

- `pending_output`
  - 给工具调用消费的增量缓冲区
- `head_tail_transcript`
  - 给后续日志 / 调试 / UI / 审计查看的长期保留缓冲区
- `tail`
  - 给 running 状态快速摘要的最近窗口

### 21.2 为什么必须三层并存

只保留一种输出缓冲会产生严重问题：

- 只保留全量 transcript
  - poll 会不断重读旧输出
- 只保留 pending
  - 无法事后查历史与排查问题
- 只保留 tail
  - 大命令会丢失关键上下文

因此必须明确下面的不变量：

1. 新输出进入时，同时写入 `pending_output` 与 `head_tail_transcript`
2. 每次 `write_stdin(...)` / poll 只 drain `pending_output`
3. `head_tail_transcript` 永不因 poll 清空
4. `tail` 永远只做最近窗口摘要，不参与消费语义

### 21.3 明确拒绝的行为

后续实现中必须明确拒绝：

- 每次 poll 返回全量日志
- 让模型自己 `sleep` 然后再发 `read`
- 让模型自己判断“要不要重复读旧日志”
- 把 transcript 与 pending 消费语义混在一起

---

## 22. 等待模型必须改成服务端 notify，而不是模型 busy-poll

### 22.1 当前问题

当前系统虽然已经有 `exec -> read -> read` 的会话能力，但模型仍然需要自己不断判断：

- 是不是还在跑
- 要不要继续 read
- 当前这次 read 值不值得

这会导致：

- 主 loop 被 shell continuation 消耗
- 短命令也可能白白多消耗 1~2 轮
- 大量运行中任务表现为“模型自己 busy-poll”

### 22.2 正确方向

正式 unified exec 必须采用：

- 输出事件到达 -> notify
- 进程退出 -> notify
- `collect_output_until_deadline(session, deadline)` 内部等待 notify 或 deadline
- 允许极短 runtime 级兜底轮询，但只作为底层实现细节

### 22.3 必须固定的规则

- 不让模型自己 sleep 再 read
- poll 返回“自上次消费以来的新输出”
- 等待靠服务端 notify + deadline
- 退出后要有 trailing-output grace，避免最后一批输出丢失
- `yield_time_ms` 和 `timeout_ms` 必须服务端钳制

---

## 23. loop 预算模型必须分层，不能继续只靠 `max_iterations`

### 23.1 当前问题

目前 Arkloop 里：

- 真正的 reasoning turn
- shell `read`
- tool follow-up
- 一些短命令的补 read

都共享同一个 `max_iterations` 预算。

这会导致：

- shell-heavy 任务很容易在完成业务前就被打断
- “执行层 continuation”与“推理层 iteration”混为一谈
- 平台上的默认值一旦偏小，就会大面积误杀真实工作流

### 23.2 正确方向

必须把预算拆开，至少拆成：

- `reasoning_iterations`
  - 真正的模型主循环预算
- `tool_continuation_budget`
  - shell / browser / 其他长任务 continuation 预算
- `per_tool_soft_limits`
  - 单命令最多 continuation 次数
  - 单次 continuation 最长等待时间
  - 单工具输出预算上限

### 23.3 对 shell 的直接要求

对 shell 而言，必须做到：

- shell continuation 不与主 reasoning turn 完全同权
- shell 的 `poll` / `write` / `signal` 不应轻易直接打穿 agent loop
- 短命令应尽可能在首轮完成，少占 continuation 配额

---

## 24. 真正需要的稳定对象：`profile_ref`、`workspace_ref` 与 `session_ref`

### 24.1 `profile_ref`

首先要补上的不是 session，而是：

- `profile_ref`

要求：

- 对用户长期环境暴露稳定引用
- 承载 `/home/arkloop`、skills、用户缓存与个人工具配置
- 不随单个 run、单个 shell session、单个项目目录一起被清掉
- 默认与当前用户绑定，而不是与某个 run 绑定

### 24.2 `workspace_ref`

和 `profile_ref` 配套的，是：

- `workspace_ref`

要求：

- 对项目或任务工作树暴露稳定引用
- 不同 run 可继续复用同一个 workspace
- 多个 session 可以挂载到同一个 workspace
- Git 导入、文件夹上传、代码生成与项目缓存都归属 workspace
- workspace 只承载工作树与项目元数据，不默认拥有用户技能与个人 home

### 24.3 `session_ref`

`shell_execute` 的未来引用不应是 live session id，而应是：

- `session_ref`

要求：

- 对模型暴露稳定引用
- 可映射到 live session
- 可映射到 checkpoint revision
- 支持 resume / restore / fork

### 24.4 为什么必须同时有三者

只有 `session_ref` 没有 `profile_ref` / `workspace_ref`，会导致：

- shell 成为被错误持久化的中心对象
- 用户环境与项目工作树都只能寄生在 session 上
- 跨 run 继续工作仍然只能靠 session 恢复硬撑

只有 `workspace_ref` 没有 `profile_ref`，又会导致：

- skills、git 配置、用户缓存无处安放
- 用户环境与项目目录被错误揉成一个对象

只有 `profile_ref` / `workspace_ref` 没有 `session_ref`，又会导致：

- 活动终端 attach / busy / signal / write 无法精确建模

因此：

- `profile_ref` 管用户长期环境
- `workspace_ref` 管项目长期工作树
- `session_ref` 管活动执行视角

---

## 25. Profile / Workspace / Session Registry 是必须的真相源

### 25.1 不能继续靠运行态猜默认绑定

未来要支持：

- 用户默认环境复用
- run 复用
- thread 复用
- workspace 复用
- org 共享
- sub-agent 协作

就不能继续靠：

- 当前 run 默认 shell
- 当前 live session 是否还在
- memory 里存一段文本提示

来猜系统状态。

真正的真相源必须是 registry。

### 25.2 最小 registry 要记录的内容

至少需要三类对象：

#### profile registry

- `profile_ref`
- `org_id`
- `owner_user_id`
- `default_workspace_ref`
- `installed_skill_set_ref` 或等价元数据
- `store_key`
- `last_used_at`
- `metadata_json`

#### shell / exec session registry

- `session_ref`
- `profile_ref`
- `workspace_ref`
- `org_id`
- `project_id`
- `thread_id`
- `run_id`
- `share_scope`
- `state`
- `live_session_id`
- `latest_checkpoint_rev`
- `default_binding_key`
- `lease_owner`
- `lease_expires_at`
- `last_used_at`
- `metadata_json`

#### workspace registry

- `workspace_ref`
- `org_id`
- `project_id`
- `owner_user_id`
- `share_scope`
- `store_key`
- `enabled_skill_refs` 或等价元数据
- `default_shell_session_ref`
- `size_bytes`
- `last_used_at`
- `metadata_json`

### 25.3 结论

后续所有 attach / restore / fork / share / market / import 能力，都必须以 registry 为准。

---

## 26. `share_scope` 与 ACL 必须正式进入模型

### 26.1 `share_scope` 不是展示字段

未来至少要支持：

- `run`
- `thread`
- `workspace`
- `org`

这不是仅用于返回给模型看的标记，而必须进入权限系统。

### 26.2 attach/resume 的 ACL 校验

当请求 attach 某个 `session_ref`、`workspace_ref` 或 `profile_ref` 时，至少校验：

- `org_id` 是否一致
- 若是 `profile_ref`，默认要求 `owner_user_id` 一致；未来如要共享，再引入显式授权
- 若 `share_scope=run`，`run_id` 是否一致
- 若 `share_scope=thread`，`thread_id` 是否一致
- 若 `share_scope=workspace`，是否属于同一 workspace / project
- 若 `share_scope=org`，调用方是否具备组织共享权限

### 26.3 为什么这一步必须前置

因为 skills、上传、git 导入、sub-agent 协作都会自然走到共享问题。
如果不先把 `share_scope + ACL` 做进设计，后面一定返工。

---

## 27. lease 与并发控制不能再省略

### 27.1 根问题

PTY 本质上是共享交互资源，不是普通无状态工具。

因此：

- 两个 writer 同时往同一个 PTY 写入是错误的
- 两个 agent 同时 attach 并写入同一个 session 也是错误的

### 27.2 必须固定的 lease 规则

- 同一时刻只允许一个 writer 持有 session lease
- `exec / write / signal / close` 需要 writer lease
- `read` 可以不拿 writer lease，但必须做 ACL 校验
- busy 状态下的第二个 writer 默认返回 `shell.session_busy`
- 并行工作应优先引导使用 `fork`

### 27.3 为什么这一步不能后补

如果不先做 lease，就会在 P2 里出现：

- skills 执行与主 agent 冲突
- sub-agent 互相污染输出
- signal 打到错误的命令
- session 虽然“被复用”，但行为完全错乱

---

## 28. skills / Git / 文件夹上传必须统一收敛到 `profile_ref` 与 `workspace_ref`

### 28.1 不能再把 skills 设计成 prompt 附件

未来的 skill 应该是一个**可安装到用户环境 profile、再由 workspace 启用的包**，而不是只靠 prompt 注入的说明文本。

推荐 skill layout：

- `skill.yaml`
- `SKILL.md`
- `scripts/`
- `assets/`
- `templates/`
- `requirements.txt`（可选）
- `package.json`（可选）

### 28.2 Git 入站与文件夹上传都要做，而且都落到 `workspace_ref`

后续必须同时支持：

- Git 入站
  - `git clone`
  - `git pull`
- 文件夹入站
  - `zip`
  - `tar`
  - `tar.gz`

但这两类能力最终都不应该成为独立孤岛，而应统一进入：

- `workspace_ref`
- `workspace-store`
- `workspace import pipeline`

### 28.3 统一结论

- skills market 安装 -> `profile_ref`
- workspace 只记录启用哪些 skill / 锁定哪些版本
- Git clone -> `workspace_ref`
- 文件夹上传 -> `workspace_ref`
- 生成代码 / 项目缓存 / 中间文件 -> `workspace_ref`

这几条必须统一建模，不能继续把用户环境与项目工作树揉成一个对象。

---

## 29. 存储层必须明确拆成四类，而不是继续混用

### 29.1 `artifacts`

职责：

- 用户可见结果
- `/tmp/output`
- 下载 / 展示 / 分享

### 29.2 `session-state`

职责：

- shell checkpoint
- PTY 恢复所需元数据
- 短 TTL

### 29.3 `workspace-store`

职责：

- `profiles/{profile_ref}`
  - `/home/arkloop` 的长期投影
  - 用户缓存、个人配置、git 配置
- `workspaces/{workspace_ref}`
  - `/workspace` 的长期投影
  - clone 仓库、上传目录、代码生成结果、项目缓存

### 29.4 `skill-store`

职责：

- 不可变 skill 包内容
- skill 版本与资源文件
- 供 profile 安装与 workspace 启用引用

### 29.5 关键原则

- `session-state` 过期不能删除 `workspace-store`
- `workspace-store` 不应被 shell checkpoint 生命周期拖累
- `skill-store` 不能与 `workspace-store` 的可变目录快照混在一起
- `artifacts` 不能继续与 `workspace-store`、`session-state` 或 `skill-store` 混在同一语义层

---

## 30. Docker / Firecracker backend 必须能力对齐

### 30.1 统一 guest baseline capability

无论最终执行后端是：

- Docker
- Firecracker

都必须至少保证同一套基础工具：

- shell
- python
- git
- curl
- wget
- jq
- grep / rg / find / sed / awk
- tar / zip / unzip / xz
- sqlite3
- ssh
- make
- file
- patch / diff

### 30.2 网络能力也必须配置化

外网能力后续要统一抽象为策略，而不是某个 backend 临时特例。

最小需要：

- `sandbox.allow_egress`
- 后续可扩展同类策略到 Firecracker backend
- 再向上叠加：
  - 域名 allowlist
  - 私网封锁
  - DNS 审计
  - credential 注入策略

---

## 31. 配置治理必须纳入正式范围，不再允许隐藏硬限制

这次问题已经证明：

- 如果关键限制只存在于系统默认值和底层 registry 中
- 而 Console 不可见
- 文档也没有把生效优先级讲清楚

那么系统就会出现“实际限制存在、操作者却不知道”的高摩擦体验。

后续必须明确：

1. 关键平台限制全部注册并暴露
2. Console 可配置 / 可查看 / 可回滚
3. 可显示配置来源：
   - `env`
   - `platform_db`
   - `org_db`
   - `default`
4. Persona / AgentConfig / Platform limit 的覆盖与钳制关系可解释

---

## 32. 新 Roadmap：P0 / P1 / P2

前文 Phase 1 ~ Phase 5 继续保留，作为**当前已实现架构与近期落地历史**。

但从本节开始，真正面向“最终 Execute System”的路线图，必须改为 P0 / P1 / P2。

### 32.1 P0：执行内核重构与现有技术债止血

P0 的目标不是继续给旧 action 接口打补丁，而是把底层执行模型改成能够承载 P1 / P2 的样子。

#### P0-1. 统一执行内核为 `exec_command + write_stdin`

要做：

- 在 runtime 内实现 unified exec session manager
- 定义 `exec_command`
- 定义 `write_stdin`
- 固定 `chars=""` 为 poll 语义
- 删除 `shell_execute` 与旧 action 模型，不保留兼容层

完成标准：

- shell 执行语义统一到 `exec_command + write_stdin`
- 对外只暴露 `exec_command + write_stdin`
- 未来 P1 / P2 不必继续依赖现有 `open|exec|read|write|signal|close` 的扩展

#### P0-2. 落地三层输出模型与 drain-on-poll

要做：

- 实现 `pending_output`
- 实现 `head_tail_transcript`
- 实现 `tail`
- poll 默认只 drain `pending_output`
- transcript 与调试读取分离

完成标准：

- 连续两次 poll，第二次不重复第一次输出
- transcript 可独立查看
- shell 不再表现为“模型不断重读旧日志”

#### P0-3. 修复当前 shell continuation 浪费

要做：

- default shell 不存在时，`exec` 自动 create-or-attach
- 短命令在 `yield_time_ms` 内尽量等到结束，减少无意义补 read
- continuation 等待改成服务端 notify + deadline
- 增加 trailing-output grace

完成标准：

- 普通短命令大多数单轮完成
- shell-heavy 任务的主 loop 消耗显著下降

#### P0-4. loop 预算分层

要做：

- 将 `reasoning_iterations` 与 `tool_continuation_budget` 拆开
- shell `poll` 不再和主推理 loop 完全同权
- 增加 per-tool soft limits

完成标准：

- shell continuation 不再轻易打穿 agent loop
- `agent.reasoning_iterations_exceeded` 不再是 shell-heavy 任务的主要失败模式

#### P0-5. 配置治理与 Console 可见性补齐

要做：

- 暴露关键平台限制
- 暴露覆盖来源
- 暴露 Persona / AgentConfig / Platform limit 的钳制关系

完成标准：

- 不再出现“系统里有上限，但操作者完全不知道”的情况

#### P0-6. backend 能力对齐

要做：

- 固化 guest baseline capability
- Docker / Firecracker 能力尽量一致
- 网络策略配置化

完成标准：

- 两种 backend 在同一 prompt 下语义一致

### 32.2 P1：重定义长期对象模型，建立长期工作环境

P1 的目标是：把 Arkloop 从“run 级 shell”升级为“以 `profile_ref / workspace_ref / session_ref` 为核心对象的长期工作环境”。

#### P1-1. 定义长期对象模型，引入 `profile_ref + workspace_ref`

要做：

- 定义 `profile_ref`
- 定义 `workspace_ref`
- 固定 guest 路径与长期对象的映射关系：`/home/arkloop` 归 profile，`/workspace` 归 workspace
- 明确 sandbox 只是临时 carrier，不再被文档暗示成长期身份对象

完成标准：

- 不同 run 可以继续同一个 profile 与 workspace 组合
- skills 的归属不再含糊：安装归 profile，启用归 workspace 元数据
- shell session 只是长期环境的 attach 点

当前这版 P1-1 落地补充：

- `profile_ref = f(org_id, user_id | system)`，表示长期个人环境
- `workspace_ref` 通过默认绑定解析；当 `project_id` 存在时优先按 `(profile_ref, project_id)` 解析，否则回落到 `(profile_ref, thread_id)`
- 默认共享边界仍按 profile 隔离：同用户 + 同项目可复用默认 `workspace_ref`，不同用户 + 同项目默认仍是不同 `workspace_ref`
- `python_execute` 保持一次性进程语义，但与 shell 共同挂到同一组 `profile_ref / workspace_ref`
- 当前持久化语义是 `dirty + debounce flush + final flush` 的 best-effort 模型；能覆盖命令完成、显式关闭、超时回收与 run 结束清理，但**不承诺 crash-safe**

#### P1-2. 引入 `session_ref`

要做：

- 对模型暴露稳定 `session_ref`
- 支持 `auto / new / resume / fork`
- 不再对模型暴露 live session id

完成标准：

- attach / resume / fork 都基于稳定引用完成

#### P1-3. 建 Profile / Workspace / Session Registry

要做：

- 建 `Profile Registry / Workspace Registry / Session Registry`
- 管理默认绑定、latest checkpoint、live session 与长期对象元数据

完成标准：

- attach / restore / fork / share 都有唯一真相源

#### P1-4. 正式引入 `share_scope + ACL`

要做：

- `run / thread / workspace / org`
- `profile_ref` 默认按用户所有权校验
- attach / resume / fork 都经过 ACL 校验

完成标准：

- 共享语义不再靠约定，而是进入正式权限模型

#### P1-5. 引入 lease 与并发控制

要做：

- writer lease
- busy / fork 语义
- sub-agent 协作约束

完成标准：

- 多 agent 不会互相踩 PTY

### 32.3 P2：skills / 导入 / 工作区生态正式落地

P2 的目标是：把执行系统从“终端能力”升级为“可安装、可导入、可长期协作”的长期环境生态。

#### P2-1. skills market 采用 `skill-store + profile install + workspace enablement` 模型

要做：

- 统一 skill layout
- 建 skill-store
- skill 安装到 profile
- workspace 只声明启用与锁版本
- 模型自己读 `SKILL.md`、自己执行脚本

完成标准：

- skills market 不再是 prompt 集合，也不再把 skill 直接塞进 workspace
- 技能归属、启用关系与项目工作树彻底解耦

#### P2-2. Git 入站

要做：

- `git clone`
- `git pull`
- 后续私仓凭证与网络策略

完成标准：

- 仓库作为工作区资产长期存在

#### P2-3. 文件夹上传入站

要做：

- `zip / tar / tar.gz` 上传
- 安全解压导入到 workspace

完成标准：

- 上传目录与 clone 仓库统一落到 workspace 语义层

#### P2-4. 独立 `workspace-store`

要做：

- 建 `workspace-store`
- 建 `skill-store`
- 在 `workspace-store` 内部分 `profiles/` 与 `workspaces/` 命名空间
- 与 `artifacts` / `session-state` 完全拆开

完成标准：

- shell checkpoint 生命周期不会影响 profile 与 workspace 资产

#### P2-5. 工作区运维与调试能力

要做：

- session / workspace 列表
- kill / remove / read_log
- size / cleanup / metrics / audit

完成标准：

- 运维和调试不再依赖 run trace 猜系统行为

---

## 33. 旧 Phase 与新 P0 / P1 / P2 的关系

为了避免理解冲突，这里明确说明：

### 前文 Phase 1 ~ Phase 5 的定位

前文 Phase 1 ~ Phase 5 继续成立，但它们的定位应调整为：

- Phase 1 ~ Phase 4：构成当前 run 级 shell 的底层基线
- Phase 5：当前基线上的治理、观测、回归收口

它们解决的是：

- 同一 run 的默认 shell
- PTY 基础能力
- checkpoint / restore
- artifact / state 拆分
- 基本治理收口

它们**没有**解决的是：

- 用户长期环境到底由谁承载
- workspace 到底是不是整台持久 sandbox
- skills 究竟归 profile 还是归 workspace
- prompt / memory 里到底应注入什么稳定引用

### 新 P0 / P1 / P2 的定位

新 P0 / P1 / P2 解决的是：

- 如何避免沿当前 run 级设计继续外推形成更大技术债
- 如何让 Execute System 真正承载 skills market / git / upload / profile / workspace / sub-agent

换句话说：

- Phase 1 ~ 5 是**当前代码基线与历史依赖**
- P0 / P1 / P2 是**从当前基线继续演进到最终系统的正式路线图**

---

## 34. 明确拒绝的方向

为了避免后续继续滚技术债，下面这些方向应明确拒绝：

1. 继续让模型自己 sleep 再 read
2. 继续让 poll 返回全量日志
3. 继续让 shell continuation 与主 reasoning turns 共用同一个硬上限
4. 继续把 default shell 永久绑定在 `run_id` 语义上
5. 把 skills market 做成 prompt 附件系统，而不是 `skill-store + profile install + workspace enablement` 模型
6. 把上传目录、clone 仓库、skills 安装各自做一套存储语义
7. 继续把 shell session 误当成长期工作区的唯一持久对象
8. 把 `workspace` 写成“一整台持久 sandbox”
9. 把 live shell id 注入 prompt / memory / workspace 文件
10. 默认把 skills 所有权写成归 `workspace_ref`
11. 继续让 Docker / Firecracker backend 能力分叉
12. 继续允许关键平台限制只存在于 registry / 默认值里，而不在 Console 可见

---

## 35. 最终结论（追加版）

在保留前文 Phase 1 ~ Phase 5 的前提下，Arkloop 后续的真正方向必须明确为：

1. **底层执行内核收敛到 unified exec：`exec_command + write_stdin`**
2. **输出语义固定为 drain-on-poll，服务端 notify 驱动等待**
3. **loop 预算拆层，shell continuation 不再无差别消耗主 iteration**
4. **系统核心对象从 run 级 shell 升级为 `profile_ref / workspace_ref / session_ref` 模型**
5. **引入 `profile_ref`、`workspace_ref`、`session_ref`、registry、ACL、lease 作为正式基础设施**
6. **skills / Git / 文件夹上传 / 长期资产按 profile、workspace、session 三层重新归位**
7. **保留旧 shell_execute 作为兼容入口，但不再作为未来能力扩展的主内核**

如果不这样做，后续即使继续“把 shell 做强”，也只会在：

- skills market
- 仓库导入
- 上传目录
- 多 agent 协作
- 长期用户环境
- 长期工作树

这些场景里继续暴露更重的技术债。

因此，从现在开始，Arkloop 应把 `shell_execute` 视为整个 Execute System 的一个切入口，而不是最终边界。
