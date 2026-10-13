# Agent Core 移植 Spec

## 目标

把 `@earendil-works/pi-agent-core` 的源码逐文件移植为 Go 包 `worker/internal/agentcore`，行为与源码一致。移植完成后，再写适配层把它接进 Arkloop，替换现有 `worker/internal/agent/loop.go`。

移植与适配是两个独立阶段，顺序不可颠倒：先得到一个可以和源码逐函数对照、通过源码测试的 Go 版本，再谈接入。

## 来源

- 仓库：`github.com/earendil-works/pi`
- 固定 commit：`3daa20280f6a7c7cbe0935680c9a38fdc3863005`(`@earendil-works/pi-agent-core` 1.2.0)
- 移植范围：

| 源文件 | 行数 | 处理 |
|---|---|---|
| `packages/agent/src/agent-loop.ts` | 949 | 全量移植 |
| `packages/agent/src/agent.ts` | 613 | 全量移植 |
| `packages/agent/src/types.ts` | 539 | 全量移植(类型) |
| `packages/agent/src/stream-fn.ts` | 20 | 移植为 `StreamFn` 类型与默认值注入 |
| `packages/agent/src/proxy.ts` | — | 不移植(远程代理，用不到) |
| `packages/ai/src/utils/transcript.ts` | 237 | 只移植 agent-loop / agent 实际调用的函数：`normalizeContext`、`getCurrentTools`、`getToolStateChanges`、`toToolDeclaration`、`createInitialSystemMessage`、`getCurrentSystemMessage`、`getCurrentSystemPrompt`，以及它们的内部依赖 |
| `packages/ai/src/utils/validation.ts` | 350 | 移植 `validateToolArguments` 及其内部依赖(含参数类型强转 `coercePrimitiveByType` 等，强转语义影响模型传参的容错，必须一致) |
| `packages/ai/src/utils/event-stream.ts` | 135 | 不移植，Go 用 channel / 回调表达 |
| `packages/ai/src/types.ts` | 893 | 只移植上述函数与 agent 类型引用到的消息、内容块、工具、usage 类型 |
| `packages/agent/test/agent-loop.test.ts` | 34 例 | 全量移植为 Go 测试 |
| `packages/agent/test/agent.test.ts` | 30 例 | 全量移植为 Go 测试 |

`pi-ai` 的 provider 实现(`api/*`、`providers/*`、`auth/*`、模型目录)不在范围内。LLM 调用通过 `StreamFn` 注入，适配阶段由 Arkloop 的 `llm.Gateway` 提供。

## 移植规则

以下规则是硬约束，目的是保证移植结果和源码在语义上一一对应，而不是名字相似、实现另写。

1. **逐文件、逐函数对应。** 每个 TS 源文件对应一个 Go 文件(`agent-loop.ts` → `agent_loop.go`)。每个 TS 函数对应一个 Go 函数，函数名按 Go 命名规则转换(`runLoop` → `runLoop`，导出的转为大写开头)，不合并、不拆分、不新增源码中没有的函数。确需的 Go 辅助函数(如类型断言)集中放在 `go_helpers.go`，并注明原因。
2. **控制流照搬。** 循环、分支、早退、提前 return 的结构和顺序与源码一致。不"顺手优化"，不改条件判断的顺序，不删看似多余的分支。
3. **函数头部注明来源。** 每个移植函数上方一行注释：`// port: agent-loop.ts runLoop`。源码里的注释原样翻译保留。
4. **语言差异的唯一映射表。** TS 特性只按下表映射，表外情况先停下来补充本表再继续：

| TS | Go |
|---|---|
| `async` / `await` | 同步调用，传入 `context.Context` |
| `AbortSignal` / `signal?.aborted` | `ctx` / `ctx.Err() != nil` |
| `Promise.all` 并行执行工具 | goroutine + `sync.WaitGroup`，结果按源码顺序收集 |
| `for await (const event of response)` | `StreamFn` 回调逐事件处理 |
| 可选回调 `config.x?.()` | 字段为 nil 时跳过 |
| 联合类型(`AgentMessage`、内容块) | 接口 + 具体类型，按 `role` / `type` 区分 |
| `undefined` 表示"未设置" | 指针或零值 + `ok` 布尔，按字段语义选，在类型定义处注明 |
| `throw` / `try-catch` | `error` 返回值；源码 catch 后转为错误结果的地方，Go 同样转为错误结果 |
| typebox schema 校验 | 使用 `github.com/google/jsonschema-go`(已是 worker 间接依赖)，校验失败的错误文本格式与源码一致 |

5. **事件序列不变。** `agent_start` / `turn_start` / `message_*` / `tool_execution_*` / `turn_end` / `agent_end` 的类型、顺序、字段与源码一致。
6. **不提前接入 Arkloop。** 移植阶段的 `agentcore` 包不 import `pipeline`、`llm`、`tools`、`data` 等任何 Arkloop 包。
7. **测试照搬。** 64 个源码测试逐个移植，测试名、输入、断言对应源码。不得为了通过而删减或放宽断言；如果某个测试依赖 JS 专属行为而无法成立，在测试里 `t.Skip` 并写明原因，汇总列在移植报告里。
8. **差异必须显式记录。** 无法照搬的地方，在 `agentcore/PORTING.md` 中按"源码位置 → Go 实现 → 原因"逐条登记。没有登记的偏差视为移植缺陷。

## 验收(移植阶段)

- `go test ./internal/agentcore/...` 全部通过；skip 数量与原因列在 `PORTING.md`
- 抽查：任取源码中一个函数，能在 Go 里找到 `// port:` 注释对应的函数，控制流可逐行对照
- `agentcore` 不依赖任何 Arkloop 内部包(`go list -deps` 检查)

## 适配阶段(移植验收后再细化)

适配层位于 `executor/`，把 Arkloop 的现有能力挂到 `agentcore` 的配置项上，不修改 `agentcore` 本身：

| agentcore 配置项 | Arkloop 来源 |
|---|---|
| `StreamFn` | `llm.Gateway.Stream`，事件转成 agentcore 的 assistant 消息事件 |
| `Tools` | 现有工具 specs，`Execute` 委托 `tools.DispatchingExecutor` |
| `PrepareRequest` / `TransformContext` | replacement compact、microcompact、prompt cache、heartbeat 请求改写、插件 before-model |
| `BeforeToolCall` / `AfterToolCall` | ask_user 接管、工具结果去重、web_search 编号、HookRuntime |
| `FinishTurn` | 预算(reasoning 迭代、成本)、heartbeat / end_reply 终止、pressure anchor 回写 |
| `GetSteeringMessages` / `GetFollowUpMessages` | `PollSteeringInput` |
| `Subscribe` 事件 | 翻译为现有 run event(`message.delta`、`tool.call`、`tool.result`、`llm.turn.completed` 等)，web/TUI/CLI 不改 |

如果某项 Arkloop 现有行为找不到对应的配置项，先在适配层用已有配置项组合实现；确实无法表达时再讨论，不在 `agentcore` 里加 Arkloop 专属逻辑。

### 消息模型转换层

适配阶段存在两套消息模型：`agentcore` 使用移植自 Pi 的 `AgentMessage` 及内容块类型，`llm/` 与 replacement compact 使用 `llm.Message` / `llm.Request`(`llm/contract.go`)。两者之间的转换集中在一个文件(`executor/agentcore_messages.go`)，只在两处使用：

1. `StreamFn`：Pi 上下文 → `llm.Request`；`llm.StreamEvent` → Pi assistant 消息事件
2. `PrepareRequest`：Pi 上下文 → `llm.Message` 交给 compact，改写结果转回 Pi 上下文

转换必须无损：thinking 内容与签名、工具调用 ID 与参数、图片、tool result 的错误标记、usage 字段往返一致。转换层单独测试，测试以往返相等为断言(Pi → llm → Pi 与 llm → Pi → llm)。

转换层是过渡结构，不在其上叠加业务逻辑。

## 收敛阶段

适配验收通过后，把 `llm/contract.go` 的消息模型改为与 Pi 一致(直接复用 `agentcore` 的消息类型)，`llm/` 各家 SDK 的 convert/parse 改为读写 Pi 类型，replacement compact 改为操作 Pi 类型。完成后删除转换层，全链路只剩一套消息模型。

这一阶段只改类型与字段映射，不改 `llm/` 的 SDK 调用、兼容与容错逻辑，也不改 compact 算法。

适配验收：现有 run event 协议保持兼容；replacement compact 落库行为不变；端到端跑通 work persona 的多次工具调用并触发 compact 的长任务、heartbeat 群聊一轮、ask_user 一轮。通过后删除 `agent/loop.go` 及其专属测试。

## 不在范围

- 重写 `llm/`：它基于各家官方 SDK(anthropic-sdk-go、openai-go、genai，Codex Responses 为手写 HTTP)，保留不动，只在收敛阶段换消息类型
- pi-ai 的 provider 实现
- 模型可调用的折叠区间展开工具
