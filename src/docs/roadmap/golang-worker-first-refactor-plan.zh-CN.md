# Go 重构路线（Worker 先行，薄片设计）

本文给出 Arkloop 从 Python 逐步重构为 Go 的可行性评估与完整迁移计划，按“薄片（Vertical Slice）”推进，先做 Worker，再做 API。

目标不是一次性重写，而是在每个薄片都可上线、可回滚、可测的前提下，稳定把运行时迁移到 Go。

## 0. 可行性结论

结论：**可行，但必须双栈渐进，不建议一次性重写**。

可行性判断（Worker 先行）：
- 架构边界已经具备：API 默认通过 `QueuedRunExecutor` 投递任务，Worker 独立消费 `jobs`。
- 事件模型已经具备语言无关性：SSE 只回放 `run_events`，不依赖执行语言。
- 队列语义已经稳定：`lease/heartbeat/ack/nack + lease_token + advisory lock` 的行为在现有测试里有覆盖。

主要难点：
- 当前 `RunEngine` 及其依赖（`agent_core`、`llm_gateway`、`mcp`、`skill_runtime`）都在 Python 生态，直接重写成本高。
- 测试体系当前以 pytest 为主，Go 侧需要补齐契约测试与跨语言集成测试。

综合判断：
- 如果采用“Go Worker + Python 执行桥接”的过渡策略，风险可控，整体可行。
- 如果要求“第一步就把 Worker 全量能力原生 Go 化”，风险高，不建议。

## 1. 当前基线（与可迁移边界）

### 1.1 已有稳定边界

- 任务投递入口：`src/services/api/run_executor.py`
  - API 默认模式是 `worker`，只负责 enqueue。
- Worker 组合根：`src/services/worker/composition.py`
  - 运行时依赖集中组装，便于替换实现。
- 消费循环与并发语义：`src/services/worker/consumer_loop.py`
  - 包含租约、心跳、ack/nack、advisory lock。
- 队列协议与 PG 实现：`src/packages/job_queue/protocol.py`、`src/packages/job_queue/pg_queue.py`
  - 协议清晰，适合直接在 Go 对齐重写。
- 事件回放链路：`src/services/api/v1.py` + `run_events`
  - 只要 `run_events` 语义不变，API 与前端可以保持稳定。

### 1.2 迁移阻力最大区域

- 运行引擎：`src/services/api/run_engine.py`
- Agent 主循环与工具执行：`src/packages/agent_core/*`
- Provider 网关：`src/packages/llm_gateway/*`
- MCP 与 Skill 运行时：`src/packages/mcp/*`、`src/packages/skill_runtime/*`

这些模块强依赖 Python 生态，建议分阶段替换，不要在第一批薄片同时动。

## 2. 迁移不变量（必须冻结）

以下契约在 Go 迁移过程中必须保持不变，否则前端、API、审计、回放会连锁破坏：

1) `jobs.payload_json` 协议（至少包含）：
- `v`
- `job_id`
- `type`
- `trace_id`
- `org_id`
- `run_id`
- `payload`

2) `run_events` 序列语义：
- `seq` 在单个 `run_id` 内严格递增
- 终态事件唯一语义：`run.completed` / `run.failed` / `run.cancelled`

3) 队列租约语义：
- 仅持有匹配 `lease_token` 的消费者可 `ack/nack/heartbeat`
- 失租约必须触发 `JobLeaseLostError` 等价行为

4) 幂等语义：
- 相同 `run_id` 并发 job 仍由 advisory lock 限制为单次执行

## 3. 目标架构（先 Worker，后全仓 Go）

阶段目标：
- 阶段 A：Go Worker 稳定接管队列消费与执行编排。
- 阶段 B：逐步把执行内核从 Python 迁到 Go。
- 阶段 C：API 逐步迁移到 Go，最终形成统一 Go 服务栈。

迁移期间允许双栈：
- Python API + Go Worker
- Python 执行内核（桥接）+ Go Worker
- Go API + Go Worker（最终）

### 3.1 目录设计与命名策略

迁移期建议目录（双栈并行）：
- Python Worker 保持不动：`src/services/worker/`
- Go Worker 独立目录：`src/services/worker_go/`

`src/services/worker_go/` 建议最小结构：

```text
src/services/worker_go/
  go.mod
  go.sum
  cmd/
    worker/
      main.go
  internal/
    app/               # 组装根（配置、依赖注入）
    queue/             # PG lease/ack/nack/heartbeat
    consumer/          # 并发消费 loop + advisory lock
    executor/          # 执行桥接与后续原生 RunEngine
    observability/     # trace/log/metrics
```

命名收口策略（避免中途频繁改名）：
- WG-01 ~ WG-09 期间，固定使用 `worker_go`，避免双栈阶段路径抖动。
- WG-10 完成且通过稳定观察窗口后，可以把 `worker_go` 改名为 `worker`。
- 改名前先清理旧 Python Worker 入口，确保同一仓库只保留一个“生产 worker”目录含义。
- 改名属于收口动作，不影响对外 API 契约；应单独作为一次可回滚变更提交。

## 4. 薄片计划（Worker 先行）

说明：每个薄片都要求“独立可验收、可回滚、默认不破坏生产路径”。

### WG-00 契约冻结与基线回归（已完成）

- 目标：把队列与事件不变量固化成可执行契约。
- 改动：
  - 提炼现有关键集成测试场景为“迁移基线用例清单”。
  - 输出事件序列 golden 样例（run.started -> worker.job.received -> ... -> run.completed）。
  - 产出基线文档：`src/docs/specs/go-worker-migration-baseline.zh-CN.md`。
  - 产出 golden 样例：`src/tests/contracts/golden/run-events/run_execute_success.v1.json`。
  - 产出校验用例：`src/tests/integration/test_worker_migration_baseline_integration.py`。
- 验收：
  - 基线测试在当前 Python 实现 100% 通过。
- 回滚：
  - 无需回滚，仅文档与测试资产。

### WG-01 Go Worker 工程骨架（已完成）

- 目标：建立可运行的 Go Worker 最小骨架，不接管流量。
- 改动：
  - 新建 Go 模块（建议 `src/services/worker_go/`）。
  - 实现配置加载与日志字段规范（对齐 `trace_id/org_id/run_id/job_id`）。
  - 支持 Linux/Windows/macOS 构建与本地运行。
  - 产出骨架代码：`src/services/worker_go/cmd/worker/main.go`、`src/services/worker_go/internal/app/*`。
  - 产出说明文档：`src/services/worker_go/README.zh-CN.md`。
- 验收：
  - `go test ./...` 基本通过。
  - Worker 可启动并优雅退出（不消费任务）。
- 回滚：
  - 不切流量，直接停用 Go Worker 进程。

### WG-02 PG 队列协议对齐（lease/ack/nack）（已完成）

- 目标：用 Go 对齐 `SqlAlchemyPgJobQueue` 的核心语义。
- 改动：
  - 实现 `lease/heartbeat/ack/nack`。
  - 对齐重试退避与 dead-letter 条件。
  - 对齐 `lease_token` 校验逻辑。
  - 产出协议定义：`src/services/worker_go/internal/queue/protocol.go`、`src/services/worker_go/internal/queue/retry.go`。
  - 产出 PG 实现：`src/services/worker_go/internal/queue/pg_queue.go`。
  - 产出契约测试：`src/services/worker_go/internal/queue/pg_queue_contract_test.go`。
- 验收：
  - 契约测试通过。
  - 并发租约不出现重复消费。
- 回滚：
  - 通过开关停用 Go Worker，恢复 Python Worker 消费。

### WG-03 消费循环对齐（并发/心跳/advisory lock）（已完成）

- 目标：对齐 `WorkerConsumerLoop` 的并发和去重语义。
- 改动：
  - 实现 `concurrency/poll/lease/heartbeat` 参数。
  - 加入 `pg_try_advisory_lock` 防重复执行。
  - 实现失心跳与失租约时的 nack/中止策略。
  - 产出消费循环：`src/services/worker_go/internal/consumer/config.go`、`src/services/worker_go/internal/consumer/loop.go`。
  - 产出锁实现：`src/services/worker_go/internal/consumer/advisory_lock.go`。
  - 产出执行器占位：`src/services/worker_go/internal/executor/noop.go`。
  - 产出测试：`src/services/worker_go/internal/consumer/loop_test.go`、`src/services/worker_go/internal/consumer/loop_integration_test.go`。
- 验收：
  - 重复 job 并发场景下，单 run 仅执行一次。
  - 心跳中断后 job 正确回队或终止。
- 回滚：
  - 只需切回 Python Worker。

### WG-04 执行桥接协议（Go Worker -> Python Engine）（已完成）

- 目标：在不重写 `RunEngine` 的前提下，让 Go Worker 能执行真实任务。
- 改动：
  - 新增 Python bridge 服务：`src/services/worker_bridge/main.py`
    - `POST /internal/bridge/execute-run`（`Authorization: Bearer <token>`）
    - 内部复用 `services.worker.composition.create_worker(...)`，执行链路保持与 Python Worker 一致。
  - Go Worker 增加 HTTP bridge handler：`src/services/worker_go/internal/executor/py_bridge_http.go`
    - 通过 `ARKLOOP_WORKER_BRIDGE_URL` / `ARKLOOP_WORKER_BRIDGE_TOKEN` 转发 `payload_json`。
  - Go Worker 入口按环境变量选择 handler：`src/services/worker_go/cmd/worker/main.go`。
  - 产出 functional 测试：`src/tests/functional/test_go_worker_bridge_functional.py`（跨进程验证 Go->Python 桥接闭环）。
- 验收：
  - `python -m pytest -m functional src/tests/functional/test_go_worker_bridge_functional.py`
  - `cd src/services/worker_go && go test ./...`
- 回滚：
  - 取消 `ARKLOOP_WORKER_BRIDGE_URL`，Go Worker 回到 noop handler；或直接停用 Go Worker，恢复 Python Worker 消费。

### WG-05 灰度路由（按 job_type/比例切流）（已完成）

- 目标：让 Go Worker 可灰度，不做一次性总切换。
- 改动：
  - 增加 queue job_type 分流常量：
    - Python：`packages.job_queue.RUN_EXECUTE_QUEUE_JOB_TYPE_GO_BRIDGE`
    - Go：`worker_go/internal/queue.RunExecuteQueueJobTypeGoBridge`
  - API enqueue 增加灰度开关：`ARKLOOP_WORKER_GO_TRAFFIC_PERCENT`（0~100，按 `run_id` 分桶，确定性路由）。
    - 0：投递 `jobs.job_type=run.execute`
    - 100：投递 `jobs.job_type=run.execute.go_bridge`
    - `payload_json["type"]` 始终保持 `run.execute`（冻结契约不变）
  - Worker 增加 lease 过滤：`ARKLOOP_WORKER_QUEUE_JOB_TYPES`
    - Python Worker/Go Worker 均可配置消费的 `jobs.job_type` allowlist
  - advisory lock 去重判断改为基于 `payload_json["type"]`，避免 job_type 分流导致去重失效。
  - 补齐测试：
    - Python integration：`test_job_queue_lease_can_filter_by_job_type`
    - Go contract：`TestPgQueueLeaseCanFilterByJobType`
    - Python unit：`test_run_executor_go_traffic_routing.py`
- 验收：
  - 默认配置（0%）下所有 unit 测试通过。
  - `jobs.job_type` 过滤后仅消费目标类型，不误 ack/nack 其它类型。
- 回滚：
  - API 侧把 `ARKLOOP_WORKER_GO_TRAFFIC_PERCENT` 设为 0，恢复投递到 Python job_type。
  - Worker 侧把 `ARKLOOP_WORKER_QUEUE_JOB_TYPES` 调整为包含目标类型即可接管残留队列。

### WG-06 Go Worker 全量接管（桥接模式）（已完成）

- 目标：Go Worker 接管所有消费，但执行内核仍先走 Python bridge。
- 改动：
  - 通过环境变量完成“默认消费者”切换（避免在代码里硬编码生产路径）：
    - API：`ARKLOOP_WORKER_GO_TRAFFIC_PERCENT=100`（enqueue 统一投递 `jobs.job_type=run.execute.go_bridge`）
    - Go Worker：`ARKLOOP_WORKER_QUEUE_JOB_TYPES=run.execute.go_bridge`（只消费 go_bridge 队列 job_type）
    - Python bridge：保持运行（`ARKLOOP_WORKER_BRIDGE_TOKEN` 必填）
  - Python Worker 保留为冷备：
    - 冷备接管：`ARKLOOP_WORKER_QUEUE_JOB_TYPES=run.execute,run.execute.go_bridge`
  - 补齐示例配置与运行说明：
    - `.env.example` / `.env.test.example`
    - `docs/使用方式for human.md`
- 验收：
  - 桥接模式 functional 测试可跑通（WG04）。
  - 切换 `ARKLOOP_WORKER_GO_TRAFFIC_PERCENT` 后，队列路由可控且可回滚。
- 回滚：
  - API 把 `ARKLOOP_WORKER_GO_TRAFFIC_PERCENT` 设为 0。
  - 启动 Python Worker（包含 go_bridge job_type），停用 Go Worker。

### WG-07 原生 Go RunEngine v1（先不做复杂工具）

- 目标：开始去桥接，先把“原生执行闭环”打通（读 run/写事件/归并 message/取消语义），为 WG-08/WG-09 的 Provider/Tool/MCP/Skill 原生化提供稳定底座。
- 改动：
  - 增加新的 Go 原生队列 job_type（仅用于迁移期分流；不改冻结契约 `payload_json["type"]=run.execute`）：
    - Python：新增 `packages.job_queue.RUN_EXECUTE_QUEUE_JOB_TYPE_GO_NATIVE="run.execute.go_native"`
    - Go：新增 `worker_go/internal/queue.RunExecuteQueueJobTypeGoNative`
  - API enqueue 增加“Go 执行器选择”硬切开关（不做百分比灰度；切换以发布/环境为单位）：
    - `ARKLOOP_WORKER_GO_EXECUTOR=bridge|native`（默认 `bridge`）
    - 当 API 侧已选择走 Go Worker（例如 `ARKLOOP_WORKER_GO_TRAFFIC_PERCENT=100`）时：
      - executor=bridge：投递 `jobs.job_type=run.execute.go_bridge`
      - executor=native：投递 `jobs.job_type=run.execute.go_native`
  - Go Worker 支持按 `lease.JobType` 选择执行器（迁移期允许同时消费 go_bridge/go_native，便于清理残留队列）：
    - `run.execute.go_bridge`：沿用 `PyBridgeHTTPHandler`
    - `run.execute.go_native`：新增 `NativeRunEngineV1Handler`
  - 原生执行器 v1（先不引入 Provider/Tools；只做 echo/noop 级闭环）：
    - 解析并校验 `jobs.payload_json`（至少 `org_id/run_id/trace_id/job_id/type`）
    - 对齐 Python Worker 的前置检查：
      - run 存在且 `run.org_id == payload.org_id`
      - run 已终态（completed/failed/cancelled）直接跳过（只 ack，不追加事件）
    - 写入 `worker.job.received` 事件（字段对齐 Python：`trace_id/job_id/job_type/org_id`）
    - 对齐取消语义：
      - 若发现 `run.cancel_requested`：立即追加 `run.cancelled` 并结束
      - 若已 `run.cancelled`：直接结束
    - 产出最小事件主干（与 golden 主干一致）：
      - `run.route.selected`（v1 可先写最小字段：`route_id`；WG-08 再对齐完整 provider 字段）
      - `message.delta`（至少 1 条；echo/noop 可用确定性文本分片）
      - `run.completed`
    - 归并 assistant delta，写入 `messages` 表（`role=assistant`、`created_by_user_id=NULL`）
    - Go 侧事件写入必须复用 `runs.next_event_seq` 分配 seq，保持严格单调递增与唯一约束（语义对齐 `SqlAlchemyRunEventRepository._allocate_seq`）。
  - 代码产出（建议路径，允许按实际拆分微调）：
    - `src/services/worker_go/internal/executor/native_v1.go`（Handler：前置检查 + worker.job.received + 调用 RunEngine）
    - `src/services/worker_go/internal/runengine/v1.go`（最小执行闭环：取消检查、事件写入、message 归并）
    - `src/services/worker_go/internal/data/run_events_repo.go`、`src/services/worker_go/internal/data/messages_repo.go`（可选：收拢 pgx SQL，避免散落）
  - 测试资产：
    - Go：RunEngine v1 的单元/集成测试（事件 seq、取消、message 归并）
    - Python functional：新增 `src/tests/functional/test_go_worker_native_functional.py`（对齐 golden 事件主干）
- 验收：
  - `cd src/services/worker_go && go test ./...`
  - `python -m pytest -m functional src/tests/functional/test_go_worker_native_functional.py`
  - 事件主干对齐：至少保证类型序列与 `run_execute_success.v1.json` 一致，且终态事件唯一。
- 回滚：
  - 把 `ARKLOOP_WORKER_GO_EXECUTOR` 切回 `bridge`，恢复 go_bridge 路径（迁移期兜底）。
  - 若队列里仍存在 `run.execute.go_native` 的残留 job：短期让 bridge worker 同时消费 `run.execute.go_bridge,run.execute.go_native` 清队列，完成后再收敛配置。

### WG-08 原生 Provider + Tool 框架

- 目标：让 Go 原生 RunEngine 具备“可用的 Provider + Tool 执行”能力，覆盖桥接模式的主干功能，为 WG-10 移除 python bridge 做准备。
- 改动：
  - Go 侧实现 Provider 路由（对齐 `packages.llm_routing` 的 JSON 配置与决策语义）：
    - 复用现有环境变量：`ARKLOOP_PROVIDER_ROUTING_JSON`
    - 输出 `run.route.selected` 事件字段尽量对齐 Python（至少包含 `route_id/model/provider_kind/scope/credential_id`）
  - Go 侧实现最小 LLM Gateway 抽象与 provider 适配：
    - OpenAI：先覆盖项目现有用法（与 `openai_api_mode` 兼容）
    - Anthropic：先覆盖流式
    - Stub provider：用于 CI/本地可重复测试（不依赖真实 key）
  - Go 侧实现 Agent Loop（对齐 `packages.agent_core.loop.AgentLoop` 的核心语义）：
    - 流式输出写 `message.delta`
    - LLM debug 事件（默认关闭）：`ARKLOOP_LLM_DEBUG_EVENTS=1` 时追加 `llm.request/llm.response.chunk`
    - tool call/result、终态 completed/failed、max_iterations 等边界条件行为对齐
  - Go 侧实现 Tool 框架（注册表 + allowlist + policy）：
    - allowlist 复用 `ARKLOOP_TOOL_ALLOWLIST`（语义与 Python 保持一致）
    - 事件对齐：`tool.call` / `tool.result` / `policy.denied` / `budget.exceeded`
  - 首批迁移低风险工具（与 Python builtin_tools 保持协议一致，优先复用同名 env）：
    - `echo`、`noop`
    - `web_search`（沿用 `ARKLOOP_WEB_SEARCH_PROVIDER` 等配置）
    - `web_fetch`（沿用 `ARKLOOP_WEB_FETCH_PROVIDER` 等配置）
  - 代码产出（建议路径）：
    - `src/services/worker_go/internal/routing/*`（provider routing config 解析 + decision）
    - `src/services/worker_go/internal/llm/*`（gateway 抽象 + openai/anthropic/stub）
    - `src/services/worker_go/internal/agent/loop.go`（Agent Loop）
    - `src/services/worker_go/internal/tools/*`（tool spec + executor）
- 验收：
  - `go test ./...` 全通过（含 provider/tool contract）
  - 端到端 run：SSE 回放可用，`run_events.seq` 单调递增，终态事件唯一
  - 错误分类与字段兼容：`error_class/tool_name/trace_id` 与 Python 保持一致语义
- 回滚：
  - WG-10 删除 bridge 前：把 `ARKLOOP_WORKER_GO_EXECUTOR` 切回 `bridge`，回到 Python engine 执行路径。

### WG-09 MCP/Skill 迁移策略收口

- 目标：解决最复杂的 Python 特有能力，并收敛到“Go Worker 单栈可运行”（不依赖 Python sidecar 才算完成）。
- 改动：
  - Skill runtime 原生化（对齐 `packages.skill_runtime` 行为与 error_class）：
    - 复用现有 skills 目录结构：`src/skills/<skill_id>/skill.yaml + prompt.md`
    - 对齐 skill 解析与校验：
      - `skill.not_found`、`skill.version_mismatch`、`skill.invalid_id` 等错误类型
    - 对齐注入策略：把 skill 的 `prompt_md/tool_allowlist/budgets` 注入到单次 run 的上下文
  - MCP 工具原生化（优先覆盖项目当前实际用法，避免过度设计）：
    - 复用现有配置入口：`ARKLOOP_MCP_CONFIG_FILE`（JSON schema 与 Python 保持一致）
    - 优先实现 `transport=stdio`（覆盖当前 `mcp.config.json` 的用法：spawn command + stdio 协议）
    - 实现最小 session pool（复用进程，避免每次 tool call 都启动新进程）
    - 把 MCP tools 注册进 Go ToolRegistry，并纳入 allowlist/policy 体系（统一审计与风险控制）
    - 超时语义对齐：遵循 `callTimeoutMs`（超时转为 `tool.result` 的 error_class，而不是静默失败）
  - 兼容性要求：
    - `run_events` 的事件类型、seq、终态语义保持不变量
    - `tool.call/tool.result` 需要可关联（`tool_call_id` 等字段不丢）
- 验收：
  - Skill：至少用 `src/skills/demo_no_tools` 跑通一条 run（能看到 skill prompt 生效且不调用工具）
  - MCP：用本地 fake MCP server（测试夹具）验证一次完整 tool call/result 闭环；同时验证 stdio transport 的跨平台可运行性
  - Go 单栈：在不启动 Python bridge/worker 的情况下，端到端 run 可完成（completed/failed/cancelled 均可测）
- 回滚：
  - 在 WG-10 删除 bridge 前，如 MCP/skill 原生化出现 P1/P2：允许短期回到 `ARKLOOP_WORKER_GO_EXECUTOR=bridge` 回避风险，但必须在 WG-10 前再次收敛回 Go 单栈。

### WG-10 下线 Python Worker

- 目标：Worker 迁移完成。
- 改动：
  - 移除 Python Worker 生产流量入口。
  - 保留短期应急文档与回切脚本。
  - 完成目录收口：把 `src/services/worker_go/` 重命名为 `src/services/worker/`（或保留 `worker_go`，二选一且写入 ADR）。
- 验收：
  - 连续两个发布周期无 P1/P2 级故障。
  - 仓库内只存在一个生产 Worker 入口目录定义，运维脚本与 CI 路径一致。
- 回滚：
  - 应急时按脚本切回桥接或 Python Worker（限时）。

## 5. 全仓 Go 计划（Worker 之后）

Worker 稳定后，再迁 API，避免双线同时高风险改造。

### GA-00 共享领域模型与契约包

- 目标：抽离 run/thread/auth 的领域结构，消除“语言绑定实现”。
- 验收：
  - Python/Go 双端都引用同一份契约文档与测试样例。

### GA-01 Go API 只读面先行

- 目标：迁移低风险读接口（如 health、runs events、threads list）。
- 验收：
  - 前端无需改协议即可切到 Go 读接口。

### GA-02 Go API 写接口迁移

- 目标：迁移 `threads/messages/runs` 写路径。
- 验收：
  - 创建会话、发消息、创建 run 全链路在 Go API 可用。

### GA-03 Go 鉴权与管理端迁移

- 目标：迁移 auth、审计、凭证管理等企业能力。
- 验收：
  - 登录、refresh、logout、审计查询行为与旧系统一致。

### GA-04 Python API 下线

- 目标：完成主服务 Go 化。
- 验收：
  - Go API + Go Worker 成为唯一生产路径。
  - Python 服务仅保留迁移工具或测试用途。

## 6. 里程碑与资源预估

以“2 名后端工程师 + 1 名测试/平台支持”为参考：

- Worker 接管（WG-00 ~ WG-06）：约 4~6 周
- Worker 原生执行能力（WG-07 ~ WG-10）：约 6~10 周
- API 迁移（GA-00 ~ GA-04）：约 8~12 周

总计：约 4~7 个月（取决于 MCP/Skill 原生化深度）。

## 7. 风险清单与降险策略

1) 事件不兼容导致前端异常
- 策略：事件 schema 契约测试 + golden 回放对比。

2) 队列语义偏差导致重复执行或消息丢失
- 策略：先做 WG-02/WG-03 对齐测试，再做灰度。

3) 桥接层成为性能瓶颈
- 策略：桥接只做过渡，WG-07 起分批原生化。

4) 双栈运维复杂度上升
- 策略：每个阶段都定义退出条件，避免长期双栈。

5) 跨平台差异
- 策略：CI 增加 Linux/macOS/Windows 三平台构建与基础集成测试。

## 8. 推荐落地顺序（可直接执行）

第一批（马上开始）：
1. WG-00 契约冻结
2. WG-01 Go 骨架
3. WG-02 PG 队列协议
4. WG-03 消费循环与 advisory lock

第二批（可上线灰度）：
5. WG-04 执行桥接
6. WG-05 灰度路由
7. WG-06 Go Worker 全量接管

第三批（去桥接、走向全 Go）：
8. WG-07/WG-08/WG-09/WG-10
9. GA-00 ~ GA-04

---

这套路线的核心是：**先把“消费与调度”迁到 Go，再逐步替换“执行内核”，最后迁 API**。这样每一步都可测、可回滚、可交付，风险最小。
