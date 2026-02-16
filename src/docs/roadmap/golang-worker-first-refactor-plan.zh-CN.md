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

### WG-02 PG 队列协议对齐（lease/ack/nack）

- 目标：用 Go 对齐 `SqlAlchemyPgJobQueue` 的核心语义。
- 改动：
  - 实现 `lease/heartbeat/ack/nack`。
  - 对齐重试退避与 dead-letter 条件。
  - 对齐 `lease_token` 校验逻辑。
- 验收：
  - 契约测试通过。
  - 并发租约不出现重复消费。
- 回滚：
  - 通过开关停用 Go Worker，恢复 Python Worker 消费。

### WG-03 消费循环对齐（并发/心跳/advisory lock）

- 目标：对齐 `WorkerConsumerLoop` 的并发和去重语义。
- 改动：
  - 实现 `concurrency/poll/lease/heartbeat` 参数。
  - 加入 `pg_try_advisory_lock` 防重复执行。
  - 实现失心跳与失租约时的 nack/中止策略。
- 验收：
  - 重复 job 并发场景下，单 run 仅执行一次。
  - 心跳中断后 job 正确回队或终止。
- 回滚：
  - 只需切回 Python Worker。

### WG-04 执行桥接协议（Go Worker -> Python Engine）

- 目标：在不重写 RunEngine 的前提下，让 Go Worker 能执行真实任务。
- 改动：
  - 定义最小桥接协议（建议 gRPC 或本地 HTTP）：`ExecuteRun(run_id, trace_id)`。
  - Python 侧新增轻量执行适配层，内部复用现有 `RunEngine`。
- 验收：
  - Go Worker 可消费真实 job，并驱动 Python 引擎产出完整 `run_events`。
- 回滚：
  - 关闭桥接开关，回到 Python Worker 全量处理。

### WG-05 灰度路由（按 job_type/比例切流）

- 目标：让 Go Worker 可灰度，不做一次性总切换。
- 改动：
  - 增加 job 路由策略（例如独立 `job_type` 或按比例投递）。
  - 建立对照监控：吞吐、失败率、平均执行时长、重试率。
- 验收：
  - 5% -> 20% -> 50% -> 100% 灰度期间指标不劣化。
- 回滚：
  - 秒级回切到 Python Worker 路径。

### WG-06 Go Worker 全量接管（桥接模式）

- 目标：Go Worker 接管所有消费，但执行内核仍可先走 Python。
- 改动：
  - 生产默认消费者改为 Go Worker。
  - Python Worker 保留为冷备。
- 验收：
  - 稳定运行一个发布周期，无关键故障。
- 回滚：
  - 切换消费者开关恢复 Python Worker。

### WG-07 原生 Go RunEngine v1（先不做复杂工具）

- 目标：开始去桥接，先实现最小原生执行闭环。
- 改动：
  - Go 实现 run 读取、取消检查、事件写入、message 归并。
  - v1 仅支持无工具或 `echo/noop`。
- 验收：
  - 与桥接模式对比，事件主干结构一致。
- 回滚：
  - 对指定 route 或 job_type 切回桥接执行。

### WG-08 原生 Provider + Tool 框架

- 目标：替换 Python 的核心执行依赖。
- 改动：
  - Go 实现 OpenAI/Anthropic 最小网关适配。
  - Go 实现 Tool Registry/Allowlist/Policy。
  - 先迁移低风险工具：`echo`、`noop`、`web_search`、`web_fetch`。
- 验收：
  - 关键工具链路端到端通过。
  - 错误分类与审计字段保持兼容。
- 回滚：
  - 单工具级别可回退到桥接执行。

### WG-09 MCP/Skill 迁移策略收口

- 目标：处理最复杂的 Python 特有能力。
- 改动（两选一，建议先 A 后 B）：
  - A：MCP/Skill 继续由 Python sidecar 提供，Go 只做编排。
  - B：逐步原生化 MCP 会话池与 Skill runtime。
- 验收：
  - 关键 MCP 工具与 skill 调用可稳定运行。
- 回滚：
  - 保留 sidecar 路径作为长期兜底。

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
