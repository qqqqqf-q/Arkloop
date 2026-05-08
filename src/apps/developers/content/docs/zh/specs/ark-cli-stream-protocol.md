---
title: "Ark CLI 客户端对接协议"
---
本文面向“其他客户端如何连接 Ark CLI”这一场景，重点描述客户端应如何调用 `ark` 命令、读取 `stdout`、区分 `stderr`、解析 `stream-json` 输出，并将其映射到类似 Claude Code 的 `thinking`、`todo`、工具调用和普通正文语义。

本文的目标不是解释 Arkloop 后端 HTTP/SSE 细节，而是定义 **Ark CLI 作为进程接口时，对外表现出的可消费协议**。

## 1. 推荐接入方式

对于程序化客户端，推荐优先使用：

```bash
ark run --output-format stream-json ...
```

原因：

- `text` 适合人看，不适合稳定机器解析
- `json` 只在结束时返回一条结果，不适合流式 UI
- `stream-json` 是最适合“外部客户端接入 Ark CLI”的格式

## 2. Ark CLI 作为进程接口的模型

对外部客户端来说，Ark CLI 可以视为一个短生命周期子进程：

1. 客户端启动 `ark` 进程
2. 客户端通过命令行参数、文件或 `stdin` 传入 prompt
3. `ark` 在内部连接 Arkloop API 并执行 run
4. `ark` 持续向 `stdout` 输出 JSON 行
5. `ark` 在结束时输出一条最终 `result` 行并退出

因此，**你要对接的是 `ark` 的标准输出协议，而不是直接对接 Arkloop 后端 SSE**。

## 3. 标准输入输出约定

### 3.1 `stdout`

`stdout` 是机器可消费输出：

- `--output-format text`: 人类可读文本
- `--output-format json`: 单条 JSON 结果
- `--output-format stream-json`: 多条 JSON Lines

程序化客户端应只把 `stdout` 作为协议流读取源。

### 3.2 `stderr`

`stderr` 主要是人类提示和错误信息，**不应作为机器协议的一部分**。

例如：

- usage 提示
- 交互式 chat 提示符
- 某些本地运行提示

建议：

- `stdout` 用于协议解析
- `stderr` 仅用于日志、调试或展示给开发者

### 3.3 退出码

推荐按下面方式理解退出码：

- `0`: 成功完成
- `1`: 运行失败
- `2`: 用法错误或参数错误

对于 `json` / `stream-json` 模式，即使失败，CLI 也会尽量在退出前输出一条最终 `result` 行。

## 4. 适合客户端调用的命令

### 4.1 单次运行

最核心命令：

```bash
ark run --output-format stream-json "请总结这个仓库的结构"
```

常用参数：

- `--host`: Arkloop API 地址
- `--token`: Bearer Token
- `--timeout`: 超时时间
- `--persona`: 指定 persona
- `--model`: 指定模型
- `--work-dir`: 指定工作目录
- `--reasoning`: 指定 reasoning mode
- `--thread`: 复用现有 thread
- `--prompt-file`: 从文件或 `stdin` 读取 prompt

### 4.2 状态查询

适合机器读取：

```bash
ark status --output-format json
```

### 4.3 模型列表

```bash
ark models list --output-format json
```

### 4.4 Persona 列表

```bash
ark personas list --output-format json
```

### 4.5 Session 列表

```bash
ark sessions list --output-format json
```

说明：

- 这些命令的 `json` 输出适合“配置选择器”或“客户端初始化”
- 真正需要流式渲染的核心命令仍然是 `ark run --output-format stream-json`

## 5. Prompt 输入方式

### 5.1 直接参数

```bash
ark run --output-format stream-json "hello"
```

适合短 prompt。

### 5.2 文件输入

```bash
ark run --output-format stream-json --prompt-file prompt.txt
```

适合长 prompt。

### 5.3 从标准输入读取

```bash
cat prompt.txt | ark run --output-format stream-json --prompt-file -
```

或：

```bash
cat prompt.txt | ark run --output-format stream-json
```

适合程序化管道输入。

## 6. `stream-json` 协议总览

`stream-json` 使用 **JSON Lines** 形式输出，每行一个完整 JSON 对象，例如：

```json
{"type":"message.delta","seq":1,"content_delta":"我先检查一下代码结构。","role":"assistant"}
{"type":"tool.call","seq":2,"tool_name":"Task","tool_call_id":"call_1","arguments":{"description":"搜索代码","query":"查找 ACP 实现"}}
{"type":"tool.result","seq":3,"tool_name":"Task","tool_call_id":"call_1","result":{"ok":true}}
{"type":"run.completed","seq":4}
{"type":"result","thread_id":"thread_xxx","run_id":"run_xxx","status":"completed","result":"我先检查一下代码结构。","duration_ms":812,"tool_calls":1,"is_error":false}
```

注意：

- 每一行都是独立 JSON
- 不要假设有数组包裹
- 不要按 SSE 帧解析
- 最终一定会有一条 `type: "result"` 的总结行，除非进程在更早阶段异常崩溃

## 7. `stream-json` 输出结构

### 7.1 中间事件行

对于中间流式事件，Ark CLI 会把内部事件投影并拍平为：

```json
{
  "type": "message.delta",
  "seq": 12,
  "...事件字段": "..."
}
```

固定字段：

- `type`: 事件类型
- `seq`: 事件顺序号

可选字段：

- `tool_name`
- 以及事件原始 `data` 中的所有字段

重要说明：

- Ark CLI 的 `stream-json` **不是原始后端 SSE Envelope**
- 它不会默认携带 `event_id`、`run_id`、`ts` 这些外层字段
- 它会把事件里的 `data` 直接拍平到当前 JSON 行

也就是说，后端 SSE 原本是：

```json
{
  "type": "message.delta",
  "seq": 12,
  "data": {
    "content_delta": "abc",
    "role": "assistant"
  }
}
```

到了 Ark CLI `stream-json`，对客户端看到的是：

```json
{
  "type": "message.delta",
  "seq": 12,
  "content_delta": "abc",
  "role": "assistant"
}
```

### 7.2 最终结果行

在运行结束时，CLI 会输出一条统一的总结结果：

```json
{
  "type": "result",
  "thread_id": "thread_xxx",
  "run_id": "run_xxx",
  "status": "completed",
  "result": "最终拼接出来的 assistant 正文",
  "duration_ms": 812,
  "tool_calls": 1,
  "is_error": false
}
```

失败时示例：

```json
{
  "type": "result",
  "thread_id": "thread_xxx",
  "run_id": "run_xxx",
  "status": "failed",
  "result": "部分输出内容",
  "duration_ms": 812,
  "tool_calls": 1,
  "is_error": true,
  "error": "provider timeout"
}
```

字段说明：

- `thread_id`: 本次 run 所属会话
- `run_id`: 本次 run 的 ID
- `status`: 终态
- `result`: CLI 内部拼接出的最终 assistant 文本
- `duration_ms`: 本次执行耗时
- `tool_calls`: 工具调用次数
- `is_error`: 是否失败
- `error`: 失败时的错误信息

## 8. 完整事件类型清单

`stream-json` 会把 Arkloop 后端的原始 run event 直接转成 JSON 行，因此**事件类型集合不是封闭的**。一方面，内置 worker 会发出一批固定事件；另一方面，Lua/扩展执行器也可能发出自定义事件。

下面分三层说明：

- **CLI 合成事件**：不是后端事件，而是 CLI 自己补出的结算行
- **代码中已确认的内置事件**：当前仓库里能明确找到发射或消费证据
- **开放集合事件**：调试/扩展/自定义执行器可能继续增加

### 8.1 CLI 合成事件

CLI 自己额外合成 1 种事件：

| 类型 | 来源 | 说明 |
|------|------|------|
| `result` | CLI 本地合成 | 运行结束后的统一结算行 |

### 8.2 代码中已确认的内置事件

下表是当前代码库中已确认会出现在 `stream-json` 中，或者明确被 CLI/Web 当作 run event 处理的类型：

| 类型 | 分类 | 说明 |
|------|------|------|
| `run.started` | 生命周期 | run 创建完成并开始 |
| `run.completed` | 生命周期 | 正常完成 |
| `run.failed` | 生命周期 | 失败结束 |
| `run.cancel_requested` | 生命周期 | 收到取消请求，但尚未终止 |
| `run.cancelled` | 生命周期 | 已取消 |
| `run.interrupted` | 生命周期 | 运行被中断 |
| `run.input_requested` | Human-in-the-loop | 等待用户输入 |
| `run.input_provided` | Human-in-the-loop | 用户输入已提交 |
| `message.delta` | 消息流 | assistant 文本或 thinking 增量 |
| `tool.call.delta` | 工具调用 | 工具参数流式增量 |
| `tool.call` | 工具调用 | 完整工具调用 |
| `tool.result` | 工具调用 | 工具执行结果 |
| `todo.updated` | Todo | 待办列表更新 |
| `run.segment.start` | 分段 | 某一段开始 |
| `run.segment.end` | 分段 | 某一段结束 |
| `run.route.selected` | Agent Loop | 路由已选择 |
| `run.context_compact` | Agent Loop | 上下文压缩/裁剪 |
| `run.llm.retry` | Agent Loop | 可重试错误后的重试事件 |
| `run.provider_fallback` | Agent Loop | provider 或 API mode 回退 |
| `run.quirk_learned` | Agent Loop | 运行中学到 provider quirk |
| `run.idle_heartbeat` | Agent Loop | 空闲心跳事件 |
| `run.mcp_discovery` | Agent Loop | MCP 发现过程状态 |
| `run.prompt_cache_debug` | 调试 | prompt cache 调试信息 |
| `run.steering_injected` | Agent Loop | steering 注入事件 |
| `llm.request` | 调试 | 上游请求 payload，通常需显式开启 debug |
| `llm.response.chunk` | 调试 | 上游流式原始 chunk，通常需显式开启 debug |
| `llm.turn.completed` | Agent Loop | 单轮 LLM turn 完成 |
| `policy.denied` | 策略 | 工具调用被策略拒绝 |
| `security.tool_injection.detected` | 安全 | 工具注入攻击检测 |
| `thread.collaboration_mode.updated` | 线程状态 | 协作模式切换 |
| `memory.write.queued` | 记忆 | 记忆写入已排队 |
| `memory.write.completed` | 记忆 | 记忆写入完成 |
| `memory.write.failed` | 记忆 | 记忆写入失败 |
| `notebook.snapshot.read_failed` | Notebook | 快照读取失败 |

### 8.3 开放集合与兼容性建议

需要特别注意：

- **不要在客户端里把事件类型写死成固定枚举**
- 对于未识别类型，建议保留原始 JSON 并做降级展示
- Lua 执行器可以发出自定义事件，例如自定义 `run.segment.start.kind`

因此，对接实现最好分两层：

1. 识别并结构化处理你关心的核心事件
2. 对未知事件保留 raw passthrough 能力

## 9. 每个关键事件的字段定义

下面是客户端最需要重点解析的事件。

### 9.1 `message.delta`

普通正文：

```json
{
  "type": "message.delta",
  "seq": 1,
  "content_delta": "正在分析仓库结构。",
  "role": "assistant"
}
```

thinking 增量：

```json
{
  "type": "message.delta",
  "seq": 2,
  "content_delta": "先搜索 ACP 与兼容层关键字。",
  "role": "assistant",
  "channel": "thinking"
}
```

字段：

- `content_delta`: 本次新增文本
- `role`: 当前通常为 `assistant`
- `channel`: 可选字符串

`channel` 取值说明：

- **当前代码里确认到的内置取值**
  - 未设置：普通 assistant 正文
  - `"thinking"`：思考/推理增量
- **未确认到的内置取值**
  - 没有找到 `"main"`、`"text"`、`"analysis"` 之类的固定内置枚举
- **协议层面**
  - 底层结构允许任意字符串；因此客户端不应假设只会出现两个值

推荐解析：

- `channel == "thinking"` -> 归入 thinking/reasoning 流
- 其它情况 -> 归入正文流

### 9.2 `run.segment.start`

示例：

```json
{
  "type": "run.segment.start",
  "seq": 10,
  "segment_id": "seg_1",
  "kind": "planning_round",
  "display": {
    "mode": "collapsed",
    "label": "第 1 轮规划"
  }
}
```

字段：

- `segment_id`: 段 ID
- `kind`: 段类型
- `display.mode`: UI 建议模式，常见为 `visible` / `collapsed` / `hidden`
- `display.label`: 人类可读标题

`kind` 取值说明：

- **当前代码里确认到的内置/已消费值**
  - `planning_round`
  - `search_planning`
  - `search_queries`
  - `search_reviewing`
- **协议层面**
  - `kind` 也是开放字符串，不保证是封闭枚举

推荐解析：

- 把它当成“分组/折叠块提示”
- 即使遇到未知 `kind`，也不应报错，只需按通用 segment 渲染

### 9.3 `tool.call`

示例：

```json
{
  "type": "tool.call",
  "seq": 21,
  "tool_name": "TodoWrite",
  "tool_call_id": "call_1",
  "resolved_tool_name": "TodoWrite",
  "arguments": {
    "todos": [
      {
        "id": "t1",
        "content": "搜索 Ark CLI 协议",
        "status": "completed"
      }
    ]
  },
  "display_description": "更新待办列表"
}
```

常见字段：

- `tool_call_id`
- `tool_name`
- `resolved_tool_name`
- `arguments`
- `display_description`
- `args_hash`

### 9.4 `tool.call.delta`

示例：

```json
{
  "type": "tool.call.delta",
  "seq": 20,
  "tool_name": "TodoWrite",
  "tool_call_index": 0,
  "tool_call_id": "call_1",
  "arguments_delta": "{\"todos\":[{\"id\":\"t1\""
}
```

常见字段：

- `tool_call_index`
- `tool_call_id`
- `tool_name`
- `arguments_delta`

注意：

- `tool_call_id` 可能暂时为空
- 推荐用 `tool_call_id` 或 `tool_call_index` 做输入缓冲关联

### 9.5 `tool.result`

成功示例：

```json
{
  "type": "tool.result",
  "seq": 22,
  "tool_name": "TodoWrite",
  "tool_call_id": "call_1",
  "display_description": "更新待办列表",
  "result": {
    "count": 1,
    "completed_count": 1,
    "total_count": 1
  }
}
```

失败示例：

```json
{
  "type": "tool.result",
  "seq": 23,
  "tool_name": "TodoWrite",
  "tool_call_id": "call_1",
  "error": {
    "error_class": "tool.args_invalid",
    "message": "parameter todos is required"
  }
}
```

常见字段：

- `tool_call_id`
- `tool_name`
- `display_description`
- `result`
- `error`
- `usage`
- `cost`

### 9.6 `todo.updated`

示例：

```json
{
  "type": "todo.updated",
  "seq": 30,
  "todos": [
    {
      "id": "t1",
      "content": "搜索 Ark CLI 协议",
      "status": "completed"
    },
    {
      "id": "t2",
      "content": "整理 stream-json 结构",
      "status": "in_progress",
      "active_form": "正在整理 stream-json 结构"
    }
  ],
  "old_todos": [],
  "changes": [],
  "completed_count": 1,
  "total_count": 2
}
```

常见字段：

- `todos`
- `old_todos`
- `changes`
- `completed_count`
- `total_count`

`todos[]` 单项字段：

- `id`
- `content`
- `status`: `pending` / `in_progress` / `completed` / `cancelled`
- `active_form`: 可选

### 9.7 `result`

这是 CLI 本地合成事件，不是后端原始事件。

```json
{
  "type": "result",
  "thread_id": "thread_xxx",
  "run_id": "run_xxx",
  "status": "completed",
  "result": "最终拼接出来的 assistant 正文",
  "duration_ms": 812,
  "tool_calls": 1,
  "is_error": false
}
```

字段说明：

- **固定存在**
  - `type`
  - `thread_id`
  - `run_id`
  - `status`
  - `result`
  - `duration_ms`
  - `tool_calls`
  - `is_error`
- **当前唯一确认的可选字段**
  - `error`

也就是说，当前代码里 `result` 并没有更多隐藏可选字段。

## 10. thinking / reasoning 的输出方式

结论先说：

- **当前没有发现 `thinking.start` / `thinking.end` 这类独立事件**
- **thinking 内容就是 `message.delta + channel: "thinking"`**

因此客户端不应等待独立的 thinking 事件，而应在解析 `message.delta` 时直接根据 `channel` 做分流。

推荐策略：

- `message.delta` 且 `channel == "thinking"` -> thinking 流
- `message.delta` 且无 `channel` -> 正文流
- 若未来出现新的 thinking 相关事件类型，按未知事件降级处理即可

## 11. 错误与重试事件格式

### 11.1 rate limit / 可重试错误的表现方式

当前没有单独叫“rate limit 事件”的类型。

Arkloop 在遇到 provider 429、408、425、5xx 等可重试错误时，典型表现是：

1. 内部把错误分类为 `provider.retryable`
2. 发出 `run.llm.retry`
3. 等待退避时间
4. 再次尝试本轮 LLM 调用
5. 若仍失败且超过最大次数，最后才落到 `run.failed`

也就是说，**你在 `stream-json` 中重点应关注的是 `run.llm.retry`，而不是某个单独的“rate limit 事件”名称**。

### 11.2 `run.llm.retry` 格式

字段：

- `attempt`: 当前第几次尝试
- `max_attempts`: 最大尝试次数
- `delay_ms`: 本次退避等待时长
- `error_class`: 上一轮错误分类，可选
- `message`: 上一轮错误消息，可选
- `llm_call_id`: 对应的 LLM 调用 ID，可选
- `details`: provider 细节，可选

示例：

```json
{
  "type": "run.llm.retry",
  "seq": 40,
  "attempt": 1,
  "max_attempts": 3,
  "delay_ms": 1000,
  "error_class": "provider.retryable",
  "message": "OpenAI network error",
  "llm_call_id": "llm_1",
  "details": {
    "status_code": 429
  }
}
```

### 11.3 退避策略

当前代码中的退避公式为指数退避：

```text
delay_ms = base_delay_ms * 2^(attempt-1)
```

并带 60 秒上限：

```text
max(delay_ms) = 60000
```

默认值：

- `base_delay_ms = 1000`
- `max_attempts = 1` 时表示不做额外重试

因此常见序列会是：

- 第 1 次重试：1000ms
- 第 2 次重试：2000ms
- 第 3 次重试：4000ms
- 依此类推，直到上限 60000ms

### 11.4 `run.provider_fallback`

除了重试，系统还可能做 provider / API mode 回退。

常见字段：

- `provider_kind`
- `from_api_mode`
- `to_api_mode`
- `reason`
- `status_code`

示例：

```json
{
  "type": "run.provider_fallback",
  "seq": 41,
  "provider_kind": "openai",
  "from_api_mode": "responses",
  "to_api_mode": "chat_completions",
  "reason": "responses_not_supported",
  "status_code": 404
}
```

### 11.5 失败终态

如果重试与回退都无法恢复，最终通常会出现：

```json
{
  "type": "run.failed",
  "seq": 50,
  "message": "upstream stream ended prematurely without completion",
  "error_class": "provider.retryable"
}
```

## 12. 流的生命周期

### 12.1 正常完成

最常见的正常顺序是：

1. `run.started`
2. 若干中间事件：
   `message.delta` / `tool.call.delta` / `tool.call` / `tool.result` / `todo.updated` / `run.segment.*`
3. `run.completed`
4. CLI 输出最终 `result`
5. 进程以退出码 `0` 结束

也就是说：

- **最后一个后端原始事件通常是 `run.completed`**
- **最后一个 CLI 输出事件通常是 `result`**

### 12.2 失败结束

常见顺序：

1. `run.started`
2. 若干中间事件
3. 可选：若干 `run.llm.retry`
4. 可选：`run.provider_fallback`
5. `run.failed`
6. CLI 输出最终 `result`
7. 进程以退出码 `1` 结束

### 12.3 取消或中断

常见顺序：

1. `run.started`
2. 可选：`run.cancel_requested`
3. `run.cancelled` 或 `run.interrupted`
4. CLI 输出最终 `result`
5. 进程以退出码 `1` 结束

### 12.4 客户端应该以什么为结束条件

推荐顺序：

1. 把 `run.completed` / `run.failed` / `run.cancelled` / `run.interrupted` 视为“流终态信号”
2. 继续读取，直到拿到最终 `type: "result"` 行
3. 再等待子进程退出

最稳妥的结束条件是：

- **收到 `result`**
- **且子进程退出**

## 13. 与 Claude Code 语义的映射

| Ark CLI `stream-json` 行 | 关键字段 | 建议映射 | 说明 |
|------|------|------|------|
| `message.delta` | `content_delta` | assistant 正文增量 | 默认正文 |
| `message.delta` | `channel=thinking` | thinking | 不是独立事件 |
| `todo.updated` | `todos[]` | todo list | 全量快照 |
| `tool.call.delta` | `arguments_delta` | tool input delta | 输入生成中 |
| `tool.call` | `arguments` | tool call | 完整输入 |
| `tool.result` | `result` | tool output | 成功结果 |
| `tool.result` | `error` | tool error | 失败结果 |
| `run.segment.start` | `segment_id/kind` | reasoning group start | 可折叠段开始 |
| `run.segment.end` | `segment_id` | reasoning group end | 可折叠段结束 |
| `run.llm.retry` | `attempt/delay_ms` | retry notice | 重试提示 |
| `run.provider_fallback` | `from_api_mode/to_api_mode` | fallback notice | provider 回退 |
| `run.completed` | - | finish(stop) | 流已完成 |
| `run.failed` | - | finish(error) | 流失败 |
| `run.cancelled` | - | abort(cancelled) | 已取消 |
| `run.interrupted` | - | abort(interrupted) | 已中断 |
| `result` | `status/result/error` | 最终摘要 | 结算行 |

## 14. 客户端推荐解析策略

### 14.1 最小实现

至少支持：

- 按行读取 `stdout`
- 每行做 JSON 解析
- 处理 `message.delta`
- 处理最终 `result`
- 识别非零退出码

### 14.2 推荐实现

再支持：

- `message.delta.channel == "thinking"`
- `todo.updated`
- `tool.call`
- `tool.result`
- `run.llm.retry`
- `run.provider_fallback`

### 14.3 高保真实现

最后支持：

- `tool.call.delta`
- `run.segment.start/end`
- `run.started`
- 其它原始事件的 raw passthrough

## 15. 推荐的客户端内部数据模型

建议先把 Ark CLI 输出投影到你自己的中间层，而不是直接把原始 JSON 行绑在 UI 上：

```ts
type ArkCliChunk =
  | { type: "text-delta"; text: string }
  | { type: "thinking-delta"; text: string }
  | { type: "todo"; todos: TodoItem[] }
  | { type: "tool-input-delta"; toolCallId?: string; toolCallIndex?: number; toolName?: string; text: string }
  | { type: "tool-call"; toolCallId: string; toolName: string; input: unknown; description?: string }
  | { type: "tool-result"; toolCallId?: string; toolName?: string; output?: unknown; error?: unknown }
  | { type: "segment-start"; segmentId: string; kind?: string; label?: string }
  | { type: "segment-end"; segmentId: string }
  | { type: "retry"; attempt: number; maxAttempts: number; delayMs: number; message?: string; errorClass?: string }
  | { type: "provider-fallback"; providerKind?: string; fromApiMode?: string; toApiMode?: string; reason?: string }
  | { type: "raw"; rawType: string; payload: Record<string, unknown> }
  | { type: "finish"; reason: "stop" | "error" | "cancelled" | "interrupted" }
  | { type: "result"; status: string; text: string; error?: string; threadId: string; runId: string }
```

这样更容易兼容未来协议扩展。

## 16. 对接注意事项

- 只把 `stdout` 作为机器协议流
- 不要把 `text` 模式当成稳定接口
- `stream-json` 是 JSON Lines，不是一个 JSON 数组
- 终态事件不等于结束，仍应继续读取最终 `result`
- `tool.call.delta` 可能先于 `tool.call`
- `thinking` 不是独立类型，而是 `message.delta.channel`
- `channel` 与 `segment.kind` 都应视为开放字符串
- 未识别事件不要报错，保留 raw 更稳妥

## 17. 后端实现背景

为了理解这些 JSON 行从哪里来，可以知道 Ark CLI 在内部做了这些事情：

1. 创建或复用 thread
2. 写入用户消息
3. 启动 run
4. 订阅后端 SSE
5. 把 SSE 事件转换成 `stream-json` 行输出到 `stdout`

但对“其他客户端连接 Ark CLI”来说，这些都属于内部实现细节。客户端通常不需要直接处理后端 SSE，只需要处理 CLI 输出即可。

## 18. 参考实现位置

- CLI 命令入口：`src/services/cli/cmd/ark/main.go`
- `stream-json` 输出投影：`src/services/cli/cmd/ark/main.go`
- CLI 运行主流程：`src/services/cli/internal/runner/run.go`
- CLI SSE Reader：`src/services/cli/internal/sse/reader.go`
- CLI 文本渲染器：`src/services/cli/internal/renderer/terminal.go`
- 后端 Runs SSE：`src/services/api/internal/http/conversationapi/v1_runs.go`
- Worker Agent Loop：`src/services/worker/internal/agent/loop.go`

如果后续要进一步稳定客户端协议，建议把本文件视为“Ark CLI stdout 协议”基准，并优先保持 `stream-json` 的兼容性。
