# 项目开发路线（从仓库现状到企业可用）

目标：把 Arkloop 这种“工具执行 + 审计 + 多租户 + 流式”系统拆成一条条可验收纵切；每一步都按「AI 一次可完成」的工作量拆分，并且能在 pytest 里回放验证，不靠“堆模块”碰运气。

## 0. 当前仓库状态（作为基线）

当前仓库已经具备一条“可跑通的最小纵切”，并且有集成测试兜底：
- API：登录/注册、`/me`、threads/messages、创建 run、SSE 拉取 run_events（断线可续传）。
- 数据：已迁移 `orgs/users/org_memberships/user_credentials/threads/messages/runs/run_events/audit_logs` 等核心表。
- 运行：默认是 in-process stub executor 往 `run_events` 写 `message.delta`/`run.completed`（便于稳定测试与前端演示）。
- 前端：`src/apps/web/` 是端到端演示 UI（登录→发消息→创建 run→消费 SSE 事件并渲染）。
- 测试：包含 integration pytest（验证 SSE 回放与 worker trace_id 恢复）。

## 1. 不变量与决策记录（先写死，减少返工）

这些是后续所有功能的“地基”，越早固定越省时间：

### 1.1 数据与边界
- `run_events` 是唯一真相：推送、审计、回放都基于同一事件表，不另起“运行日志”体系。
- 生产数据库以 PostgreSQL 为唯一目标；短期不拆“管理库/业务库”，先在同一 PG 内按表域与 DB 账号权限隔离（必要时用 schema）。
- 敏感明文范围默认覆盖：messages 内容、附件/网页快照正文、`run_events.data_json`（工具参数/结果摘要等）。

### 1.2 前后端与鉴权
- SSE 采用 Fetch 流式 + `Authorization: Bearer ...`（与普通 API 同鉴权），续传以 `after_seq` 为唯一游标。
- Web 与 Console 短期按“同域同站共享登录态”实现（同一个 token 存储/刷新策略），允许从 Web 跳转到 Console 时直接复用登录态。

### 1.3 平台（SaaS）优先策略
- 优先交付 SaaS 形态：平台管理能力、全局模型目录/路由、审计与安全策略先行；自部署能力后置。
- 平台管理员与 org 管理员是两类不同权限域：同一个 Console 里路由/菜单可共存，但权限点必须隔离（`platform.*` 与 `org.*`）。

### 1.4 Provider / Key / BYOK（你已明确的选择）
- Provider 路由按“路由规则”决定：模型/场景/预算/风险等共同参与，BYOK 只是 credential 来源之一，不是硬编码优先级。
- 创建「Provider 凭证（API）」时必须选择 `provider_kind=openai|anthropic`（行业通用做法）；下游始终调用同一个 Gateway 接口，上游由凭证与路由自动适配。
- Key 来源必须同时支持：
  - 环境变量注入（适合平台全局 key 或自部署）
  - 数据库加密存储（适合 SaaS、BYOK、轮换）
- 为了兼容第三方与多端点：
  - `base_url` 必须可配置（OpenAI-compatible 第三方通常只需改 base_url）。
  - OpenAI 侧需要同时兼容 `chat_completions` 与 `responses`，并由凭证（或路由）选择 `openai_api_mode=auto|responses|chat_completions`。
- 第三方的“少数特殊参数”（例如额外 header/query/timeout）允许通过「高级 JSON 配置」提供，但必须：
  - 只允许白名单键（例如 `extra_headers`、`extra_query`、`timeout_ms`）
  - 严禁在 JSON 中写入任何 secret（secret 只能走 env/db/vault）
  - 有大小/字段数上限，并把变更写入审计（至少存 hash + actor + trace_id）
- BYOK 需要有，但必须由后台开启 org 权限（org 未开启时不允许录入 BYOK 凭证，也不允许路由到 BYOK）。
- 平台管理员查看明文：默认允许，但必须可被关闭（全局策略 + org 覆盖均可做）；无论是否允许，都要求强审计可追溯。
- Azure OpenAI 等“协议相近但不完全一致”的接入不作为 v1 目标；后期可以新增一个 provider（而不是强行塞进 OpenAI-compatible）。
- Vault：作为长期路线；前期不实现 Vault 兼容层代码，但在数据模型与服务边界上保留“迁移点”（见后续 P9x）。

## 2. 任务拆分规则（Pxx 粒度）

每个 Pxx 任务必须满足：
- 单独可验收：有清晰输入输出与验收口径，不依赖“未来模块”才能证明正确。
- 尽量不触网：对 provider/web_fetch 等不稳定依赖，优先 stub 或 record/replay。
- 强调稳定契约：API 响应、事件结构、错误码、权限点是“后续只增不破”的资产。

每个 Pxx 统一写清：
- 目标：这一小步做完，系统新增了什么稳定能力
- 关键点：最容易踩坑/最需要提前决定的部分
- 依赖：如果需要先完成某些 Pxx，写清楚（避免“后做的地基反过来阻塞前面的功能”）
- 验收：unit/integration 需要测什么；手工怎么验收（如果必要）

## 3. 路线图（按“AI 一次可完成”的小步拆分）

说明：下面分成两条主线并行推进：
- 主线 A：让产品能“真实回答”（Provider + Run + Chat 体验）
- 主线 B：让企业能“真正管理”（Console + 权限 + 审计 + Key 管控）

推荐执行顺序（不强制，但能减少互相阻塞）：
- 主线 A：P30 → P31 → P32 → P33 → P34 → P35 → P35.1 → P35.2 → P35.3 → P35.4 → P35.5 → P35.6 → P36
- 主线 B：P40 → P41 → P42 → P60 → P43 → P62 → P44 → P45 → P46

### 3.1 已完成（仓库现状对应的 Pxx）

这些能力已在仓库里，建议保留为里程碑记录：

#### P20 — run_events 事件模型 + SSE 回放（已完成）
- 目标：一次 run 的全过程能落库为事件序列，并通过 SSE 回放/续传。
- 验收：integration pytest 覆盖断线续传与事件结构稳定性。

#### P21 — JWT 登录/注册 + 最小审计（已完成）
- 目标：用户可登录/注册，关键操作写入审计（至少登录成功/失败、注册）。
- 验收：integration pytest 覆盖登录与受保护接口访问。

#### P22 — agent-core 只定义接口与事件生成（已完成）
- 目标：`agent-core`（代码目录 `src/packages/agent_core/`）定义 `AgentRunner` 接口：输入上下文，输出标准化事件序列（async generator）。
- 要求：
  - `agent-core` 不依赖 logging；过程用事件表达。
  - 事件 schema 与 `run_events` 表字段一致或可一一映射。
- 验收：
  - unit pytest：固定输入下产出事件序列结构正确（不测自然语言逐字）。

#### P23 — tool spec + allowlist/policy.denied（已完成）
- 目标：实现 ToolSpec 与 allowlist 校验；禁止的工具调用产出 `policy.denied` 事件。
- 验收：
  - unit pytest：越权工具被拒绝且错误码稳定；事件可回放。

#### P24 — worker 骨架：从 job payload 恢复 trace_id 上下文（已完成）
- 目标：`src/services/worker/` 接收 job payload，恢复 `trace_id/org_id/run_id` 上下文，写 `run_events`。
- 验收：
  - integration pytest：模拟投递 job，worker 写入事件带正确 trace_id 并可从 SSE 回放。

#### P25 — Web 端到端演示 UI（已完成）
- 目标：前端能登录/注册，并以 SSE 方式展示一次 run 的事件流。
- 验收：手工：浏览器里可看到 `message.delta`/`run.completed` 等事件。

### 3.2 主线 A：Provider + Chat（先让产品“能回答”）

#### P30 — LLM Gateway 内部契约（不触网）（已完成）
- 目标：定义“提供商无关”的内部请求/响应/错误/成本模型，作为 API 与 Provider 的稳定边界。
- 关键点：
  - 明确 streaming 的最小事件集合：至少能映射到 `message.delta`、`run.failed`、`run.completed`，并为后续 tool-calling 预留事件类型（不要求 v1 执行工具）。
  - 错误分类要稳定：`provider.*`（可重试/不可重试）、`budget.*`、`policy.*`、`internal.*`。
  - 成本与用量字段先占位（后续计费/配额都要用）。
  - 内部契约要能覆盖两类上游差异：OpenAI（chat completions / responses）与 Anthropic（messages/streaming）。
- 依赖：无
- 验收：
  - unit pytest：给定 stub stream，能生成稳定的 `message.delta` 事件序列与最终状态事件（见 `src/tests/unit/test_llm_gateway_contract.py`）。

#### P31 — 把 stub executor 收敛到“runner/gateway”路径（已完成）
- 目标：让“执行器写事件”的逻辑归一：executor 只编排，事件由 runner/gateway 产出并写入 `run_events`。
- 关键点：
  - 保留现有 stub 能力（测试稳定性不能退化）。
  - 为后续接入真实 provider 做好替换点：只替换 gateway/adapter，不改事件消费者（SSE/前端）。
- 依赖：P30
- 验收：
  - integration pytest：原有 stub 场景仍能稳定产出 delta 与 completed。

#### P32 — OpenAI Adapter v1（兼容 chat completions + responses）
- 目标：实现 OpenAI provider 的 streaming 适配器，统一输出内部事件（再映射到 `message.delta/run.*`），并支持 OpenAI-compatible 第三方 `base_url`。
- 关键点：
  - 同时支持两条上游路径：`chat_completions` 与 `responses`（由 `openai_api_mode` 决定，可选 `auto|responses|chat_completions`）。
  - 兼容第三方：`base_url` 必须可配置；`auto` 模式下如 `responses` 不可用可回退到 `chat_completions`（回退行为要可观测、可审计）。
  - Key 永不下发前端；日志与事件不写明文 key。
  - 超时/重试要保守（先保证“不会挂死 run”，再谈吞吐）。
  - 失败必须落 `run.failed`，并带稳定错误码与 `trace_id`。
- 依赖：P30、P31
- 验收：
  - unit pytest（离线）：`src/tests/unit/test_openai_llm_gateway.py` 覆盖 chat_completions、responses 与 auto 回退。
  - integration pytest：默认仍走 stub（不连公网）；另提供可选的“手工验收步骤”用于本地连真 provider（不进 CI）。
  - 手工（连真 provider）：
    - 设置环境变量（建议 `base_url` 带 `/v1`；例如官方 `https://api.openai.com/v1`）：
      - `ARKLOOP_OPENAI_API_KEY=...`
      - `ARKLOOP_OPENAI_BASE_URL=https://api.openai.com/v1`（可选）
      - `ARKLOOP_OPENAI_API_MODE=responses`（或 `chat_completions`）
    - 本地执行（示例以 PowerShell 为主；Linux/macOS 把 `$env:XXX=...` 换成 `export XXX=...`）：
      - `.\.venv\Scripts\python -c "exec('import anyio\\nfrom packages.llm_gateway.openai import OpenAiGatewayConfig, OpenAiLlmGateway\\nfrom packages.llm_gateway import LlmGatewayRequest, LlmMessage, LlmTextPart\\ncfg=OpenAiGatewayConfig.from_env(required=True)\\ngw=OpenAiLlmGateway(config=cfg)\\nreq=LlmGatewayRequest(model=\"gpt-4.1-mini\", messages=[LlmMessage(role=\"user\", content=[LlmTextPart(text=\"hello\")])])\\nasync def main():\\n  async for e in gw.stream(request=req):\\n    print(type(e).__name__, getattr(e, \"content_delta\", \"\"), getattr(getattr(e, \"usage\", None), \"total_tokens\", None))\\nanyio.run(main)\\n')"`。

#### P33 — Anthropic Adapter v1（messages streaming）
- 目标：实现 Anthropic provider 的 streaming 适配器，统一输出内部事件（再映射到 `message.delta/run.*`），并支持 `base_url`。
- 关键点：
  - 先只做“能流式回答”的最小能力；tool-calling 事件先占位，不强行在 v1 做工具执行。
  - 允许通过「高级 JSON 配置」传入少数第三方必需参数（header/query/timeout），但必须白名单校验且不得包含 secret。
  - 失败必须落 `run.failed`，并带稳定错误码与 `trace_id`。
- 依赖：P30、P31
- 验收：
  - unit pytest（离线）：`src/tests/unit/test_anthropic_llm_gateway.py` 覆盖 messages streaming、错误返回、以及高级 JSON 配置白名单校验。
  - integration pytest：默认仍走 stub（不连公网）；另提供可选的“手工验收步骤”用于本地连真 provider（不进 CI）。
  - 手工（连真 provider）：
    - 设置环境变量（建议 `base_url` 带 `/v1`；例如官方 `https://api.anthropic.com/v1`）：
      - `ARKLOOP_ANTHROPIC_API_KEY=...`
      - `ARKLOOP_ANTHROPIC_BASE_URL=https://api.anthropic.com/v1`（可选）
      - `ARKLOOP_ANTHROPIC_VERSION=2023-06-01`（可选）
      - `ARKLOOP_ANTHROPIC_ADVANCED_JSON={"extra_headers":{...},"extra_query":{...},"timeout_ms":5000}`（可选；严禁在 JSON 中写入任何 secret）
    - 本地执行（示例以 PowerShell 为主；Linux/macOS 把 `$env:XXX=...` 换成 `export XXX=...`）：
      - `.\.venv\Scripts\python -c "exec('import anyio\\nfrom packages.llm_gateway.anthropic import AnthropicGatewayConfig, AnthropicLlmGateway\\nfrom packages.llm_gateway import LlmGatewayRequest, LlmMessage, LlmTextPart\\ncfg=AnthropicGatewayConfig.from_env(required=True)\\ngw=AnthropicLlmGateway(config=cfg)\\nreq=LlmGatewayRequest(model=\"claude-xxx\", messages=[LlmMessage(role=\"user\", content=[LlmTextPart(text=\"hello\")])], max_output_tokens=128)\\nasync def main():\\n  async for e in gw.stream(request=req):\\n    print(type(e).__name__, getattr(e, \"content_delta\", \"\"), getattr(getattr(e, \"usage\", None), \"total_tokens\", None))\\nanyio.run(main)\\n')"`。

#### P34 — Provider 路由规则 v1（openai/anthropic + base_url + openai_api_mode）(已完成)
- 目标：实现最小可用的路由规则引擎：同一个 run 根据输入与策略选择 provider、模型与凭证，并把上游差异隐藏在 adapter 后面。
- 关键点：
  - 路由规则是配置，不是写死 if/else；v1 先保证“可配置、可测试、可回放”，UI 可以后置。
  - 凭证维度必须包含：`provider_kind`、`base_url`、（OpenAI 专用）`openai_api_mode`、（可选）高级 JSON 配置。
  - BYOK 不是默认可用：只有 org 开启 BYOK 权限后，路由才允许选择 org 级凭证。
- 依赖：P32、P33（至少一个 provider adapter）、P43（BYOK 开关）、P44（凭证）
- 验收：
  - unit pytest：给定固定规则与 org 设置，路由选择可预测、可回放。
  - integration pytest：未开启 BYOK 的 org 无法使用 BYOK credential（返回稳定错误码/事件）。

#### P35 — 把流式输出“沉淀成消息”（让 Chat 可用）(已完成)
- 目标：assistant 输出不只停留在 `run_events`，还要能沉淀到 `messages`（用于对话历史/刷新恢复）。
- 关键点：
  - `run_events` 仍是唯一真相；`messages` 是“可用视图”（materialized view），可由事件重建。
  - 需要定义“归并规则”：如何把多条 `message.delta` 聚合成最终 assistant 消息（含失败/取消边界）。
- 依赖：P31（稳定 runner 路径）+ P32/P33（至少一个真实 provider；stub 也必须可用）
- 验收：
  - integration pytest：完成 run 后，`GET /threads/{id}/messages` 能看到 assistant 消息。

#### P35.1 — Threads API 完整化 v1（列表/详情/标题更新）(已完成)
- 目标：补齐 Chat 必需的 threads 端点，让 Web/CLI 能稳定管理会话：
  - `GET /v1/threads`：列出当前用户可见的 threads（最小分页/排序）。
  - `GET /v1/threads/{thread_id}`：获取 thread 详情（用于刷新恢复与校验）。
  - `PATCH /v1/threads/{thread_id}`：更新 thread 的 `title`（可选但强烈建议，便于可用产品形态）。
- 关键点：
  - 列表排序必须稳定：默认按 `created_at desc, id desc`；分页建议用 cursor（例如 `before_created_at + before_id`），不要用 offset。
  - 多租户与权限边界要一致：只能访问当前 actor 的 org；早期可沿用 owner-only 策略，后续再扩展到 org 共享。
  - 错误要可定位：`threads.not_found`、`policy.denied` 等错误码保持稳定，并包含 `trace_id`。
- 依赖：P21（鉴权）+ 现有 threads/messages 表
- 验收：
  - integration pytest：创建多个 thread 后 `GET /v1/threads` 顺序稳定且只返回本 org 可见数据；`PATCH title` 后 `GET /v1/threads/{id}` 能读到更新结果。

#### P35.2 — Token Refresh v1（不引入 refresh token）(已完成)
- 目标：提供 `POST /v1/auth/refresh`，在 access token 仍有效时签发新 token，避免长会话中频繁重新登录。
- 关键点：
  - Phase 1 不引入 refresh token 与会话表：仅校验现有 bearer token + 用户存在，再签发新 token。
  - 审计与日志不记录 token 明文；只记录 user_id、trace_id 与操作类型。
  - 错误语义明确：过期/无效 token 统一 401，并返回稳定错误码与 `trace_id`。
- 依赖：P21（JWT 鉴权与审计）
- 验收：
  - unit pytest：有效 token 可 refresh 并获得新 token；过期/无效 token 返回 401 且 `code + trace_id` 可读。

#### P35.3 — Logout v1（明确 token 失效策略）(已完成)
- 目标：提供 `POST /v1/auth/logout`，并定义“登出后 token 何时失效”的稳定策略，保证 Web/CLI 行为一致且可审计。
- 关键点：
  - 不能只做前端清 token：需要服务端可验证的失效策略（例如 `users.tokens_invalid_before` 或 `users.token_version`），避免泄露 token 后无法止损。
  - Logout 必须幂等：重复调用不报错，并且审计可追溯（actor、user_id、trace_id）。
  - 不做 token 黑名单（存储与查询会膨胀）；优先用“用户级版本/时间戳”实现全局失效。
- 依赖：P21（JWT 鉴权与审计）
- 验收：
  - integration pytest：logout 后旧 token 访问受保护接口被拒绝；重新登录或 refresh 后的新 token 可正常访问。

#### P35.4 — Runs API 完整化 v1（run 详情读取）
- 目标：补齐 Web Chat 刷新恢复与排障所需的 run 读取端点：
  - `GET /v1/runs/{run_id}`：返回 run 元数据（`run_id/thread_id/status/created_at/...`），用于刷新后校验与展示。
- 关键点：
  - 权限边界与 `GET /v1/runs/{run_id}/events` 一致：同 org + owner-only；拒绝要写审计，并返回稳定错误码与 `trace_id`。
  - `runs.status` 必须可解释：优先由 `run_events` 推导（例如读取最后一个终态事件），或在写入终态事件时同步更新（二选一，写死并测试）。
  - 错误码稳定：`runs.not_found`、`policy.denied`、`auth.*`；响应与 header 都应包含 `trace_id`。
- 依赖：P20（run_events）+ P21（鉴权/审计）
- 验收：
  - integration pytest：创建 run 后可 `GET /v1/runs/{id}`；越权/跨 org 被拒绝且 `code + trace_id` 可读；不存在返回 404 `runs.not_found`。

#### P35.5 — Run Cancel v1（停止生成/幂等）(已完成)
- 目标：提供 `POST /v1/runs/{run_id}:cancel`，允许 Web/CLI 主动停止生成，并形成可回放事件。
- 关键点：
  - 取消必须幂等：重复调用不报错；对同一 run 的取消事件语义稳定（避免前端因重复事件产生抖动）。
  - 事件流同源：取消必须落 `run_events`（建议 `run.cancel_requested` + 最终 `run.cancelled`），并保证 `seq` 单调递增。
  - 尽量停止执行：executor/runner 需识别取消信号，至少保证取消后不再写入新的 `message.delta`。
  - 审计：记录 actor、run_id、trace_id；不记录敏感明文。
- 依赖：P20、P21、P31
- 验收：
  - integration pytest：对运行中的 run 调用 cancel 后，事件流出现取消事件且后续不再产生新 delta；重复 cancel 仍 200；无权限返回 403 且 `trace_id` 可定位。

#### P35.6 — Thread 最近 Run 查询 v1（刷新恢复增强，可选但建议）(已完成)
- 目标：提供“从 thread 找回 run”的后端能力，降低仅依赖前端本地持久化的脆弱性：
  - `GET /v1/threads/{thread_id}/runs?limit=...`：按时间倒序返回 run 列表（至少 `run_id/status/created_at`），用于刷新恢复与“是否仍在生成”的判断。
- 关键点：
  - 列表排序稳定：默认按 `created_at desc, id desc`；不使用 offset 分页。
  - 权限边界与 thread 一致；跨 org/跨 owner 不泄漏。
  - `status` 语义与 P35.4 保持一致（推导或同步更新，不能两套口径）。
- 依赖：P35.1 + P35.4
- 验收：
  - integration pytest：同 thread 创建多次 run 后顺序稳定；跨 thread/跨 org 不返回数据；错误码与 `trace_id` 可定位。

#### P36 — Web Chat MVP（从演示页走向可用页）(已完成)
- 目标：Web 侧提供最小可用聊天体验：线程、消息列表、输入框、流式渲染、刷新后恢复。
- 关键点：
  - SSE 断线重连用 `after_seq`，前端只做消费与展示，不拼接敏感策略。
  - 错误展示要能定位：至少显示 `code` + `trace_id`。
- 依赖：P35、P35.1、P35.2、P35.3、P35.4；建议完成 P35.5（可停止生成）与 P35.6（刷新恢复增强）
- 验收：
  - 手工：正常对话、断线重连、刷新恢复。
  - unit/组件测试：关键 hook（SSE parser、重连策略）有基础覆盖。

### 3.3 主线 B：Console（先让企业“能管”）

#### P40 — Console 路由与布局（同域复用登录态）
- 目标：在现有 Web 基础上加入 `/console` 路由与独立布局（或独立 app，但保持同域共享 token）。
- 关键点：
  - Console 与 Chat 的菜单/页面完全分离，避免“随手堆一个设置页”。
  - 权限不足时的 UX 要明确（不给入口 + 403 可读错误）。
- 依赖：P21（已有登录态与受保护 API）
- 验收：
  - 手工：登录后可进入 Console；退出登录两边一致失效。

#### P41 — 审计查询 v1（Console 可读的第一件事）
- 目标：提供审计日志查询 API + Console 列表页（过滤：时间范围、actor、action、target）。
- 关键点：
  - 审计字段必须支持溯源：谁、何时、做了什么、对哪个资源、trace_id。
  - 敏感字段默认只存 hash/摘要，避免审计表本身成为“明文泄露源”。
- 依赖：P21
- 验收：
  - integration pytest：写入审计后可查询到；权限不够查询被拒绝且审计可追踪。

#### P42 — Run 浏览与排障页（run 详情=事件时间线）
- 目标：Console 能查看 run 列表/详情，并把 `run_events` 以时间线方式展示（支持 SSE/回放）。
- 关键点：
  - 事件类型要分组展示：模型输出、工具调用、policy 拦截、失败原因。
  - 视图层不“理解业务”，只解释事件。
- 依赖：P20、P40
- 验收：
  - 手工：拿一个 run_id 能完整回放；断线可续传。

#### P43 — Org 安全设置 v1（BYOK 开关 + 平台支持访问开关）
- 目标：org 管理员（或平台管理员）可配置 org 级安全设置：
  - BYOK 是否启用
  - 是否允许平台管理员查看该 org 的敏感明文（默认允许，但可关闭）
- 关键点：
  - 这是“策略入口”，后续所有权限与路由都必须引用它，不能各模块各写一套判断。
- 依赖：P60（如果不做 RBAC v1，这一步也必须至少有明确的角色拦截与审计）
- 验收：
  - integration pytest：关闭平台访问后，平台管理员读取明文相关接口被拒绝且写审计。

#### P44 — Provider 凭证管理 v1（ENV / DB 两种来源）
- 目标：Console 提供凭证管理（录入/禁用/轮换/查看元数据），并支持两种 secret 来源：
  - ENV：只保存 env var 名称/引用，不保存明文
  - DB：保存加密后的密文（明文仅在创建/轮换时出现）
- 关键点：
  - 创建凭证必须选择 `provider_kind=openai|anthropic`，并配置 `base_url`（官方可用默认值，第三方直接填其网关地址）。
  - OpenAI 凭证必须配置 `openai_api_mode=auto|responses|chat_completions`（第三方常用 `chat_completions`；官方可用 `auto`）。
  - 支持“高级 JSON 配置”（默认隐藏）：
    - 仅允许白名单键（例如 `extra_headers`、`extra_query`、`timeout_ms`）
    - 禁止写入任何 secret（secret 只能走 env/db/vault）
    - 有大小/字段数上限，并写入审计（至少存 hash + actor + trace_id）
  - org 级 BYOK 凭证仅在 org 开启 BYOK 后可创建。
  - “显示明文 key”默认不提供；需要也只能走受控流程（并强审计）。
- 依赖：P62、P43
- 验收：
  - integration pytest：未开启 BYOK 时创建 BYOK 凭证被拒绝；启用后可创建并可用于路由。

#### P45 — 平台模型目录与路由配置 v1
- 目标：平台管理员可管理：
  - 模型目录（模型名、描述、状态、倍率/价格占位）
  - 路由规则（哪些场景用哪些模型/凭证来源）
- 关键点：
  - 先后端可配置，UI 可简化；规则必须可测试、可回放。
- 依赖：P34（路由引擎）或至少 P30（内部契约）
- 验收：
  - unit pytest：规则变更不破坏既有 schema；对同一输入路由结果确定。

#### P46 — 平台租户管理 v1（最小 SaaS 管控面）
- 目标：平台管理员可管理 org 生命周期（创建/停用/策略默认值），并能看到 org 的基础用量指标占位。
- 关键点：
  - 这一步先做“能管住”：安全策略默认值、BYOK 授权、平台访问开关默认值。
- 依赖：P60、P43
- 验收：
  - 手工：创建 org、设置默认策略、进入 org 视角验证生效。

### 3.4 安全与权限（支撑 SaaS 的“真企业”部分）

#### P60 — 权限点与 RBAC v1（先让 Console 权限可控）
- 目标：把“owner-only”策略升级为可扩展的权限点体系（最小实现即可），覆盖：
  - `platform.*`（平台级）
  - `org.*`（租户级）
  - `data.*`（敏感明文读取/导出等）
- 关键点：
  - 权限点必须是稳定字符串（用于审计与前端路由守卫）。
  - 早期可以用少量内置角色映射权限点，后续再做可编辑角色。
- 依赖：P21
- 验收：
  - unit pytest：关键权限点在策略中可预测；拒绝时错误码稳定。

#### P61 — 敏感明文访问审计与“可关闭”策略落地
- 目标：对敏感明文访问建立统一拦截与审计：
  - 当平台访问被 org 关闭时，平台管理员访问明文类接口一律拒绝并审计。
  - 当允许时，也要记录“谁看了什么范围”（避免事后追责无据）。
- 关键点：
  - 明文访问必须走统一入口（service/middleware），不要散落在每个 endpoint 里写判断。
- 依赖：P43、P60
- 验收：
  - integration pytest：允许/禁止两种状态下行为一致且可追踪。

#### P62 — 数据库加密存储 v1（仅做应用层加密，不做 Vault）
- 目标：实现“DB 存 key”的最小加密方案：
  - 主密钥来自环境变量（例如 `ARKLOOP_MASTER_KEY`）
  - DB 仅存密文 + 必要元数据（算法/版本/创建时间）
- 关键点：
  - 前期不做 Vault 兼容层，但要写清密钥轮换/迁移口径（见 P90）。
- 依赖：无
- 验收：
  - unit pytest：同一明文加密后不可逆读取（无主密钥无法解密）；轮换策略可测试（至少能解密旧版本）。

## 4. 下一阶段详细薄片（从当前代码到"真正可用的 Agent"）

当前代码到"真正可用"的主要缺口：
1. **Agent Loop 多轮循环**：`ProviderRoutedAgentRunner` 只做一次 LLM 调用就结束，不支持 tool_call -> tool_result -> 再次 LLM 的循环。
2. **Tool Execution**：`ToolSpec`/`ToolRegistry`/`ToolPolicyEnforcer` 框架已有，但没有真正的 tool executor 和内置工具。
3. **MCP 协议**：完全没有。
4. **Skills**：`src/docs/guides/skills-and-tools.zh-CN.md` 有规范草案，但代码完全没有。
5. **Console 管理后台**：API 侧只有 Chat 相关端点，缺少管理类端点和前端。
6. **前端产品化**：Web 前端停留在"演示 MVP"阶段（单文件 App.tsx），离可用产品有距离。

下面按四条主线拆成薄片。每条线内部有依赖顺序，跨线尽量减少阻塞。

**推荐执行顺序（主线 C 优先，这是产品核心价值）：**
- 主线 C（Agent Loop）先行：P50 -> P51 -> P52 -> P53 -> P54 -> P55 -> P56 -> P57 -> P58 -> P59
- 主线 B（Console 管理后台）跟进：P40 -> P41 -> P42 -> P60 -> P43 -> P62 -> P44 -> P45 -> P46
- 主线 D（前端产品化）穿插：可在 P53 之后开始（此时 tool_call 事件流已稳定）
- 主线 E（Vault/高级安全）最后

---

### 4.1 主线 C：Agent Loop 核心（让 Agent "能用工具"）

这是从"只能聊天"到"真正的 Agent"的关键路径。

#### P50 -- Agent Loop 多轮循环骨架

- 目标：改造 `ProviderRoutedAgentRunner`（或新建 `LoopingAgentRunner`），支持"LLM 返回 tool_call -> 执行 tool -> 把 tool_result 拼回 messages -> 再次调用 LLM"的循环，直到 LLM 不再返回 tool_call 或达到最大轮次。
- 现状分析：
  - `ProviderRoutedAgentRunner.run()` 当前流程：构造 `LlmGatewayRequest` -> 调用 `gateway.stream()` -> 逐事件 yield -> 结束。没有循环。
  - `LlmGatewayRequest` 已有 `tools: list[ToolSpec]` 字段但从未使用（`_build_request` 不填 tools）。
  - `LlmStreamToolCall` / `LlmStreamToolResult` 事件类型已在 contract 中定义。
  - `run_events_from_llm_stream` 已能正确映射 `tool.call` / `tool.result` 事件。
- 关键点：
  - 循环的终止条件：LLM 回复中不包含 tool_call / 达到 `max_iterations`（建议默认 10，可配置）/ 取消信号。
  - 每轮循环必须产出完整事件序列：`tool.call` -> `tool.result` -> （下一轮）`message.delta` -> ...
  - tool_call 的解析必须从 LLM 流式输出中正确提取（OpenAI 和 Anthropic 的 tool_call 格式不同，但 gateway 层已经统一成 `LlmStreamToolCall`）。
  - **不在这一步实现真正的 tool 执行**；先用一个 `StubToolExecutor` 返回固定结果，验证循环骨架正确。
- 具体改动范围：
  - `src/packages/agent_core/runner.py`：扩展 `AgentRunContext`，增加 `max_iterations`、`tool_specs` 字段。
  - 新建 `src/packages/agent_core/loop.py`：`AgentLoop` 类，封装多轮循环逻辑。
  - `src/services/api/provider_routed_runner.py`：`_build_request` 需要传入 `tools` 参数；`run()` 需要改为循环调用。
  - `src/packages/llm_gateway/contract.py`：可能需要增加 `LlmStreamTurnCompleted` 事件标记单轮结束（或复用现有 `LlmStreamRunCompleted` 仅在最终轮发出）。
- 依赖：无（在已完成代码上直接改）
- 验收：
  - unit pytest：给定 stub gateway（模拟返回 1 次 tool_call + 1 次纯文本），循环产出正确事件序列（tool.call -> tool.result -> message.delta -> run.completed）。
  - unit pytest：达到 max_iterations 时正确终止并产出 `run.failed`（error_class: `agent.max_iterations_exceeded`）。
  - unit pytest：循环中收到取消信号时正确终止。

#### P51 -- ToolExecutor 协议与注册机制

- 目标：定义 `ToolExecutor` 协议（给定 tool_name + args -> 返回结果 + 耗时 + 可能的错误），并将其与现有 `ToolRegistry` / `ToolPolicyEnforcer` 集成。
- 现状分析：
  - `ToolRegistry` 已有，管理 `ToolSpec` 的注册与查找。
  - `ToolPolicyEnforcer` 已有，做 allowlist 校验并产出 `policy.denied` 事件。
  - 缺少的是"通过 ToolRegistry 找到 tool -> 校验权限 -> 实际执行 -> 返回结果"这条链路。
- 关键点：
  - `ToolExecutor` 是一个 Protocol：`async def execute(*, tool_name: str, args: dict, context: ToolExecutionContext) -> ToolExecutionResult`。
  - `ToolExecutionContext` 包含：`run_id`、`trace_id`、`org_id`、`timeout_ms`、`budget`。
  - `ToolExecutionResult` 包含：`result_json`、`error`（可选）、`duration_ms`、`usage`（可选）。
  - 执行前必须经过 `ToolPolicyEnforcer.request_tool_call()` 校验。
  - 需要一个 `DispatchingToolExecutor`：根据 tool_name 从注册表分派到具体执行器。
- 具体改动范围：
  - 新建 `src/packages/agent_core/executor.py`：`ToolExecutor` 协议、`ToolExecutionContext`、`ToolExecutionResult`、`DispatchingToolExecutor`。
  - 修改 `src/packages/agent_core/__init__.py`：导出新类型。
  - 新建 `src/packages/agent_core/stub_executor.py`：`StubToolExecutor`（用于测试）。
- 依赖：P50（循环骨架需要调用 executor）
- 验收：
  - unit pytest：`DispatchingToolExecutor` 能正确分派到注册的具体执行器。
  - unit pytest：未注册的 tool 调用返回明确错误。
  - unit pytest：policy 拦截的 tool 不会到达 executor。

#### P52 -- Agent Loop 集成 ToolExecutor

- 目标：把 P50 的循环骨架与 P51 的 ToolExecutor 对接：LLM 返回 tool_call -> ToolPolicyEnforcer 校验 -> ToolExecutor 执行 -> 结果拼回 messages -> 下一轮。
- 关键点：
  - tool_result 必须以 `tool` role 的 message 格式拼回 `LlmGatewayRequest.messages`（OpenAI 和 Anthropic 都要求此格式）。
  - 一次 LLM 回复可能包含多个 tool_call（并行调用）；需要全部执行完再拼回。
  - 每个 tool_call/tool_result 都必须产出对应事件。
  - policy.denied 的 tool_call 也必须拼回 messages（告诉 LLM 这个工具被拒绝了）。
- 具体改动范围：
  - `src/packages/agent_core/loop.py`：完成 tool_call 收集 -> policy 校验 -> executor 执行 -> result 拼接 -> 下一轮的完整流程。
  - `src/packages/llm_gateway/contract.py`：可能需要增加 `LlmToolResultMessage` 或扩展 `LlmMessage` 支持 tool role。
  - `src/services/api/provider_routed_runner.py`：注入 `ToolExecutor` 和 `ToolPolicyEnforcer`。
- 依赖：P50 + P51
- 验收：
  - unit pytest：完整的多轮循环场景（stub gateway 模拟 2 次 tool_call + 最终纯文本回复）。
  - unit pytest：policy.denied 的 tool_call 被正确处理，LLM 收到拒绝信息并继续。
  - unit pytest：并行 tool_call 全部执行并正确拼回。
  - integration pytest：通过 API 创建 run，SSE 事件流包含完整的 tool.call / tool.result / message.delta 序列。

#### P53 -- LLM Gateway 支持 tool_call 流式解析

- 目标：确保 OpenAI 和 Anthropic 的 gateway 实现能正确从流式响应中解析 tool_call（function_call / tool_use），并映射为 `LlmStreamToolCall` 事件。
- 现状分析：
  - `LlmStreamToolCall` 数据结构已定义。
  - OpenAI adapter（`src/packages/llm_gateway/openai.py`，23KB）和 Anthropic adapter（`src/packages/llm_gateway/anthropic.py`，26KB）已经很大，需要确认是否已处理 tool_call 解析。
  - 需要确认 gateway `stream()` 方法是否在收到 tool_call 后正确产出 `LlmStreamToolCall` 而不是直接产出 `LlmStreamRunCompleted`。
- 关键点：
  - OpenAI chat_completions 的 tool_call 在 `delta.tool_calls` 中分片到达；需要缓冲并在完整后产出。
  - OpenAI responses API 的 tool_call 在 `output` 中以 `function_call` item 到达。
  - Anthropic 的 tool_use 在 `content_block_start` / `content_block_delta` / `content_block_stop` 中到达。
  - gateway 在收到 tool_call 后不应产出 `LlmStreamRunCompleted`，因为循环还没结束；应该产出 `LlmStreamToolCall` 然后等待外部提供 tool_result 并发起下一轮请求。
  - 这意味着 gateway 的 `stream()` 职责可能需要调整：每次 `stream()` 只处理一轮 LLM 调用，循环由上层 AgentLoop 驱动。
- 具体改动范围：
  - `src/packages/llm_gateway/openai.py`：确保 tool_call 解析 -> `LlmStreamToolCall` 映射正确。
  - `src/packages/llm_gateway/anthropic.py`：同上。
  - 可能需要调整 gateway 的 stream 结束语义：区分"LLM 回复完毕但还有 tool_call 待处理"和"真正完成"。
- 依赖：P52（需要上层循环来驱动多轮）
- 验收：
  - unit pytest：mock OpenAI chat_completions 流式响应（含 tool_calls delta），正确产出 `LlmStreamToolCall`。
  - unit pytest：mock OpenAI responses API 流式响应（含 function_call），正确产出 `LlmStreamToolCall`。
  - unit pytest：mock Anthropic 流式响应（含 tool_use blocks），正确产出 `LlmStreamToolCall`。
  - 手工验证（可选）：连接真实 provider，发送需要 tool 调用的请求，确认事件流正确。

#### P54 -- 内置工具 #1：echo / noop（最小验证）

- 目标：实现第一个真正的内置工具，用于端到端验证 Agent Loop 全链路。
- 关键点：
  - `echo` 工具：接收 `{ "text": "..." }`，返回 `{ "text": "..." }`。无副作用、无风险。
  - `noop` 工具：不做任何事，返回 `{ "ok": true }`。用于测试"工具注册但不执行"的场景。
  - 通过这两个工具验证从 LLM tool_call -> policy -> execute -> result -> LLM 的完整链路。
- 具体改动范围：
  - 新建 `src/packages/agent_core/builtin_tools/` 目录。
  - 新建 `src/packages/agent_core/builtin_tools/echo.py`。
  - 新建 `src/packages/agent_core/builtin_tools/noop.py`。
  - 在 `ProviderRoutedAgentRunner` 或配置层注册这些工具。
- 依赖：P52
- 验收：
  - integration pytest：创建 run（stub gateway 模拟 tool_call echo），SSE 事件流包含 tool.call(echo) -> tool.result -> message.delta -> run.completed。
  - unit pytest：echo 工具输入输出稳定。

#### P55 -- 内置工具 #2：web_search（只读、低风险）

- 目标：实现 web_search 工具，使 Agent 能搜索互联网。
- 关键点：
  - 输入：`{ "query": "...", "max_results": 5 }`。
  - 输出：`{ "results": [{ "title": "...", "url": "...", "snippet": "..." }] }`。
  - 后端选择搜索 API（建议 SearXNG 自部署或 Tavily/Serper 等付费 API）。
  - API key 通过环境变量注入，不落库、不下发前端。
  - risk_level: "low"，side_effects: false。
  - 必须有 timeout（建议 10 秒）和错误处理（搜索失败不能让整个 run 崩溃）。
- 具体改动范围：
  - 新建 `src/packages/agent_core/builtin_tools/web_search.py`。
  - 新建 `src/packages/agent_core/builtin_tools/web_search_config.py`（配置类，从环境变量读取 API key 和 base_url）。
- 依赖：P54（确认内置工具链路已通）
- 验收：
  - unit pytest：mock HTTP 响应，验证输入输出 schema 稳定。
  - unit pytest：搜索超时返回明确错误（不崩溃）。
  - 手工验证：连接真实搜索 API，Agent 能搜索并引用结果回答。

#### P56 -- 内置工具 #3：web_fetch（只读、中风险）

- 目标：实现 web_fetch 工具，使 Agent 能抓取网页内容。
- 关键点：
  - 输入：`{ "url": "...", "max_length": 50000 }`。
  - 输出：`{ "content": "...", "title": "...", "url": "...", "truncated": false }`。
  - URL 白名单/黑名单：禁止访问内网地址（127.0.0.1、10.x、192.168.x 等）。
  - 内容截断：超过 max_length 截断并标记 `truncated: true`。
  - HTML -> 纯文本/Markdown 转换（建议 readability 算法或 trafilatura）。
  - risk_level: "medium"，side_effects: false。
  - timeout: 15 秒。
- 具体改动范围：
  - 新建 `src/packages/agent_core/builtin_tools/web_fetch.py`。
  - 新建 `src/packages/agent_core/builtin_tools/url_policy.py`（URL 安全策略：内网地址黑名单）。
- 依赖：P54
- 验收：
  - unit pytest：内网地址被拒绝；截断逻辑正确；HTML 转文本基本可读。
  - unit pytest：超时返回明确错误。

#### P57 -- MCP 客户端协议适配 v1

- 目标：实现 MCP（Model Context Protocol）客户端，让 Agent Loop 能调用外部 MCP 工具服务器提供的工具。
- 现状分析：
  - 当前所有工具都是内置的（`builtin_tools/`）。MCP 允许外部进程暴露工具，Agent 通过 JSON-RPC over stdio/SSE 调用。
  - `ToolExecutor` 协议已有（P51），MCP 客户端本质上是另一个 `ToolExecutor` 实现。
- 关键点：
  - MCP 传输层 v1 先做 stdio（最简单：启动子进程，通过 stdin/stdout 通信）。
  - MCP 握手：`initialize` -> `initialized` -> `tools/list` -> 动态注册到 `ToolRegistry`。
  - MCP 工具调用：`tools/call` -> 返回结果。
  - MCP server 配置从环境变量/配置文件读取（类似 Claude Desktop 的 `claude_desktop_config.json`）。
  - 安全边界：MCP server 是子进程，需要限制其权限（文件访问、网络访问）。
  - 超时与错误处理：MCP server 无响应/崩溃不能让 run 挂死。
- 具体改动范围：
  - 新建 `src/packages/mcp/` 包。
  - 新建 `src/packages/mcp/client.py`：MCP 客户端（stdio 传输）。
  - 新建 `src/packages/mcp/config.py`：MCP server 配置。
  - 新建 `src/packages/mcp/executor.py`：`McpToolExecutor`（实现 `ToolExecutor` 协议）。
  - 新建 `src/packages/mcp/registry.py`：从 MCP server 发现工具并注册到 `ToolRegistry`。
- 依赖：P52（ToolExecutor 集成）
- 验收：
  - unit pytest：mock stdio 通信，验证 initialize -> tools/list -> tools/call 流程。
  - unit pytest：MCP server 超时/崩溃时返回明确错误。
  - integration pytest：启动一个简单的 MCP server（echo 工具），通过 Agent Loop 调用成功。

#### P58 -- MCP SSE 传输层

- 目标：在 P57 stdio 传输的基础上，增加 SSE 传输层（远程 MCP server）。
- 关键点：
  - SSE 传输：连接远程 MCP server 的 `/sse` 端点，通过 HTTP POST 发送请求。
  - 认证：支持 Bearer token 认证（MCP server 可能需要鉴权）。
  - 重连策略：SSE 断线后自动重连。
  - 配置中区分传输类型：`"transport": "stdio"` 或 `"transport": "sse"`。
- 具体改动范围：
  - `src/packages/mcp/client.py`：增加 SSE 传输实现。
  - `src/packages/mcp/config.py`：增加 SSE 传输相关配置。
- 依赖：P57
- 验收：
  - unit pytest：mock SSE 连接，验证请求/响应流程。
  - 手工验证：连接一个远程 MCP server，调用工具成功。

#### P59 -- Skill 运行时 v1（加载 + 执行 + 约束）

- 目标：实现 skill 的最小运行时：从 `src/skills/` 加载 skill 定义，注入 tool_allowlist 与预算约束，通过 AgentLoop 执行。
- 现状分析：
  - `src/docs/guides/skills-and-tools.zh-CN.md` 定义了 skill.yaml 和 prompt.md 的规范。
  - 代码中完全没有 skill 相关实现。
- 关键点：
  - Skill 定义格式：`skill.yaml`（元数据 + 约束）+ `prompt.md`（提示词模板）。
  - Skill Registry：加载 `src/skills/` 下所有 skill 定义。
  - Skill Runner：在 AgentLoop 之上的一层封装，注入 skill 特有的 system prompt、tool_allowlist、budget 等。
  - 创建 run 时可指定 `skill_id`；如果指定了 skill，runner 必须遵守 skill 定义的约束。
  - v1 先做内置 skill（代码随版本发布），不做用户自定义 skill。
- 具体改动范围：
  - 新建 `src/packages/skill_runtime/` 包。
  - 新建 `src/packages/skill_runtime/schema.py`：`SkillDefinition` 数据类（对应 skill.yaml）。
  - 新建 `src/packages/skill_runtime/loader.py`：从目录加载 skill 定义。
  - 新建 `src/packages/skill_runtime/runner.py`：`SkillRunner`（包装 AgentLoop，注入 skill 约束）。
  - 新建 `src/skills/` 目录 + 至少一个示例 skill。
  - `src/services/api/v1.py`：`CreateRunRequest` 增加可选 `skill_id` 字段。
- 依赖：P52（AgentLoop 集成）
- 验收：
  - unit pytest：skill 加载正确；约束注入正确（tool_allowlist 生效、budget 生效）。
  - unit pytest：指定不存在的 skill_id 返回明确错误。
  - integration pytest：通过 API 创建带 skill_id 的 run，事件流符合 skill 约束。

---

### 4.2 主线 B：Console 管理后台（详细薄片）

> P40-P46 的大方向已在 3.3 节定义，这里补充执行细节。

#### P40 -- Console 路由与布局

- 具体改动范围：
  - 决策点：Console 是 `src/apps/console/`（独立 app）还是 `src/apps/web/` 内的 `/console` 路由？
    - 建议：先做独立 app（`src/apps/console/`），与 web 共享 `src/packages/ui/` 组件库；共享 token 存储策略。
  - 新建 `src/apps/console/` 骨架：Vite + React + TailwindCSS v4。
  - 实现布局：侧边栏导航（审计、运行、提供商、组织等菜单项）+ 主内容区。
  - 实现路由守卫：无 token 时跳转登录页；权限不足时显示 403。
- 依赖：P21
- 验收：
  - 手工：登录后能进入 Console 布局；退出登录后回到登录页。

#### P41 -- 审计查询 API + Console 列表页

- 具体改动范围：
  - 后端 `src/services/api/v1.py`：新增 `GET /v1/audit-logs`（分页、过滤：时间范围、actor_user_id、action、target_type、target_id）。
  - 后端 `src/services/api/v1.py`：新增 `GET /v1/audit-logs/{log_id}`（详情）。
  - `src/packages/data/audit_logs.py`：新增查询方法（当前只有写入）。
  - Console 前端：审计日志列表页 + 详情弹窗/页。
- 依赖：P40 + P21
- 验收：
  - integration pytest：写入审计后可查询到；过滤条件生效。
  - 手工：Console 能看到审计日志列表并过滤。

#### P42 -- Run 浏览与排障页

- 具体改动范围：
  - Console 前端：Run 列表页（跨 org 可选，按时间排序）+ Run 详情页（事件时间线）。
  - Run 详情页：复用 SSE 连接展示事件流；按事件类型分组和着色（message.delta、tool.call、policy.denied、run.failed 等）。
  - 可能需要后端新增 `GET /v1/runs`（全局 run 列表，平台管理员用）。
- 依赖：P40 + P20
- 验收：
  - 手工：选择一个 run，能看到完整的事件时间线。

#### P43 -- Org 安全设置 API

- 具体改动范围：
  - 新建数据表 `org_settings`（org_id, byok_enabled, platform_access_enabled, ...）。
  - Alembic 迁移。
  - 新建 `src/packages/data/org_settings.py`：Repository。
  - 后端 `src/services/api/v1.py`：
    - `GET /v1/orgs/{org_id}/settings`
    - `PATCH /v1/orgs/{org_id}/settings`（仅 org 管理员或平台管理员）
  - 审计：设置变更必须写审计日志。
- 依赖：P60（权限点）或至少有明确的角色拦截
- 验收：
  - integration pytest：关闭 BYOK 后尝试创建 BYOK 凭证被拒绝。
  - integration pytest：关闭平台访问后平台管理员读明文被拒绝。

#### P44 -- Provider 凭证管理 API

- 具体改动范围：
  - 新建数据表 `provider_credentials`（id, org_id, provider_kind, api_key_source, api_key_env, encrypted_api_key, base_url, openai_api_mode, advanced_json, status, ...）。
  - Alembic 迁移。
  - 新建 `src/packages/data/provider_credentials.py`：Repository。
  - 后端 `src/services/api/v1.py`：
    - `POST /v1/provider-credentials`（创建凭证）
    - `GET /v1/provider-credentials`（列表）
    - `GET /v1/provider-credentials/{id}`（详情，不返回明文 key）
    - `PATCH /v1/provider-credentials/{id}`（更新 base_url/advanced_json/status）
    - `DELETE /v1/provider-credentials/{id}`（标记禁用，不物理删除）
  - 与 P62（加密存储）集成：DB 来源的 key 加密存储。
  - 与 P43 集成：org 未开启 BYOK 时拒绝创建 org 级凭证。
- 依赖：P62 + P43
- 验收：
  - integration pytest：创建凭证 -> 列表可见 -> 更新 -> 禁用 -> 路由不再选择该凭证。
  - unit pytest：明文 key 不出现在 API 响应和日志中。

#### P45 -- 平台模型目录 API

- 具体改动范围：
  - 新建数据表 `model_catalog`（id, name, display_name, description, provider_kind, status, input_price_per_1m, output_price_per_1m, multiplier, ...）。
  - Alembic 迁移。
  - 新建 `src/packages/data/model_catalog.py`：Repository。
  - 后端 `src/services/api/v1.py`：
    - `GET /v1/models`（列表，面向前端用户，只返回可用模型 + 描述 + 倍率）
    - `POST /v1/admin/models`（平台管理员创建）
    - `PATCH /v1/admin/models/{id}`（平台管理员更新）
  - 与 `ProviderRouter` 集成：路由规则可引用 model_catalog。
- 依赖：P34（路由引擎）
- 验收：
  - integration pytest：创建模型 -> 路由能选择到该模型 -> 更新状态为 disabled -> 路由不再选择。

#### P46 -- 平台租户管理 API

- 具体改动范围：
  - 后端 `src/services/api/v1.py`：
    - `GET /v1/admin/orgs`（平台管理员列出所有 org）
    - `POST /v1/admin/orgs`（平台管理员创建 org）
    - `PATCH /v1/admin/orgs/{org_id}`（更新 org 名称/状态）
    - `GET /v1/admin/orgs/{org_id}/members`（成员列表）
  - 新建 `src/packages/data/identity.py` 扩展：增加 `list_all_orgs`、`list_members_by_org` 等查询方法。
- 依赖：P60 + P43
- 验收：
  - integration pytest：平台管理员可列出/创建 org；非平台管理员被拒绝。
  - integration pytest：停用 org 后该 org 用户无法登录（或有明确降级行为）。

---

### 4.3 主线 B 补充：安全与权限（详细薄片）

#### P60 -- 权限点与 RBAC v1

- 具体改动范围：
  - 新建 `src/packages/auth/permissions.py`：定义权限点常量（`platform.orgs.manage`、`org.settings.manage`、`org.threads.read`、`data.sensitive.read` 等）。
  - 新建 `src/packages/auth/roles.py`：内置角色定义（`platform_admin`、`org_admin`、`org_member`），每个角色映射一组权限点。
  - 修改 `src/services/api/authorization.py`：`Authorizer` 不再只做 owner-only 校验，改为基于权限点 + 角色映射的策略。
  - 修改 `src/packages/data/identity.py`：`org_memberships.role` 需要支持新角色。
  - 新建 Alembic 迁移：可能需要 `platform_roles` 表或直接在 org_memberships 中扩展。
- 依赖：P21
- 验收：
  - unit pytest：`org_member` 不能执行 `org.settings.manage`；`org_admin` 可以。
  - unit pytest：权限不足时返回稳定的 `policy.denied` 错误码。

#### P61 -- 敏感明文访问审计

- 具体改动范围：
  - 新建 `src/services/api/sensitive_access.py`：统一的敏感明文访问拦截中间件。
  - 拦截逻辑：检查 org_settings.platform_access_enabled + actor 角色 -> 决定放行/拒绝 -> 无论结果都写审计。
  - 应用于：provider_credentials 明文查看、messages 内容导出等端点。
- 依赖：P43 + P60
- 验收：
  - integration pytest：org 关闭平台访问后平台管理员被拒绝且审计可查。
  - integration pytest：org 允许时访问成功且审计可查。

#### P62 -- 数据库加密存储 v1

- 具体改动范围：
  - 新建 `src/packages/crypto/` 包。
  - 新建 `src/packages/crypto/envelope.py`：应用层加密（AES-256-GCM），主密钥从 `ARKLOOP_MASTER_KEY` 环境变量读取。
  - 新建 `src/packages/crypto/key_version.py`：密钥版本管理（支持解密旧版本密文）。
  - 用于 `provider_credentials.encrypted_api_key` 字段。
- 依赖：无
- 验收：
  - unit pytest：加密 -> 解密 round-trip 正确。
  - unit pytest：无主密钥时解密失败并返回明确错误。
  - unit pytest：新版密钥加密、旧版密钥的密文仍可解密。

---

### 4.4 主线 D：前端产品化（从 MVP 到可用产品）

当前 Web 前端状况：App.tsx 一个文件 ~600 行，包含登录、会话列表、消息列表、SSE 消费、调试面板。功能基本完整但不是产品级。

#### P70 -- 前端状态管理

- 目标：引入轻量级状态管理（建议 zustand 或 jotai），把散落在 App.tsx 中的 useState 梳理为可维护的 store。
- 具体改动范围：
  - 新建 `src/apps/web/src/stores/`。
  - 拆分 store：`authStore`（token、me）、`threadStore`（threads、activeThreadId）、`chatStore`（messages、draft、assistantDraft）、`runStore`（activeRunId、sse 状态）。
  - 重构 App.tsx，减少到 <100 行（只做路由和顶层 layout）。
- 依赖：无
- 验收：
  - 现有功能不退化（登录、发消息、SSE、刷新恢复全部正常）。

#### P71 -- 前端组件拆分与路由

- 目标：把 App.tsx 中的 `AuthCard`、`ChatMvp`、sidebar、message list 等拆分为独立组件文件；引入 react-router。
- 具体改动范围：
  - 引入 react-router-dom。
  - 路由：`/` -> 聊天页、`/login` -> 登录页。
  - 组件拆分：
    - `src/apps/web/src/pages/ChatPage.tsx`
    - `src/apps/web/src/pages/LoginPage.tsx`
    - `src/apps/web/src/components/chat/MessageList.tsx`
    - `src/apps/web/src/components/chat/ChatInput.tsx`
    - `src/apps/web/src/components/chat/ThreadList.tsx`
    - `src/apps/web/src/components/layout/AppLayout.tsx`
- 依赖：P70
- 验收：
  - 现有功能不退化；路由切换流畅；刷新后能恢复状态。

#### P72 -- 前端 Tool Call 渲染

- 目标：在聊天消息流中渲染 tool_call / tool_result 事件（当 Agent Loop 支持工具调用后，前端需要能展示）。
- 具体改动范围：
  - 新建 `src/apps/web/src/components/chat/ToolCallBubble.tsx`：显示工具名称、参数摘要、执行状态（进行中/成功/失败）。
  - 新建 `src/apps/web/src/components/chat/ToolResultBubble.tsx`：显示工具返回结果摘要。
  - 修改消息流渲染逻辑：在 message.delta 之间插入 tool_call / tool_result 卡片。
  - policy.denied 事件用警告样式渲染。
- 依赖：P52（后端 Agent Loop 集成 ToolExecutor）+ P71（组件拆分）
- 验收：
  - 手工：发送需要工具调用的消息，能看到工具调用卡片 -> 结果卡片 -> 最终回复。
  - 手工：policy.denied 的工具调用显示为警告。

#### P73 -- 前端 Markdown 渲染

- 目标：assistant 消息支持 Markdown 渲染（代码块高亮、列表、链接等），提升可读性。
- 具体改动范围：
  - 引入 react-markdown + remark-gfm + rehype-highlight（或 shiki）。
  - 新建 `src/apps/web/src/components/chat/MarkdownContent.tsx`。
  - 替换消息中的 `whitespace-pre-wrap` 纯文本展示。
  - 流式渲染：在 assistantDraft 变化时实时渲染 Markdown（需要处理不完整 Markdown 的渲染闪烁问题）。
- 依赖：P71
- 验收：
  - 手工：代码块有语法高亮；列表正确渲染；链接可点击。
  - 手工：流式渲染无明显闪烁。

#### P74 -- 前端模型选择器

- 目标：用户发送消息时可选择模型（从 `GET /v1/models` 获取可用列表）。
- 具体改动范围：
  - 新建 `src/apps/web/src/components/chat/ModelSelector.tsx`。
  - 修改 `createRun` 调用：传入 `route_id`（对应所选模型的路由规则）。
  - 记住用户上次选择（localStorage）。
- 依赖：P45（平台模型目录 API）+ P71
- 验收：
  - 手工：下拉列表显示可用模型；选择后创建 run 使用对应模型。

---

### 4.5 主线 E：长期路线（先写迁移点，不写兼容性代码）

#### P90 -- Vault 集成（只在真正需要时做）
- 目标：把"密钥来源"从 env/db 扩展到 Vault（或同类 KMS），并完成无停机迁移方案设计。
- 关键点：
  - 不提前写一套复杂插件系统；到这一步再抽象"secret backend"边界即可。
  - 迁移必须可回滚：Vault 不可用时能降级到 db/env（按策略）。

#### P91 -- 高风险工具执行沙箱
- 目标：对 shell/code_execute 等高风险工具，实现沙箱执行环境（Docker container / gVisor / nsjail）。
- 关键点：
  - 文件系统隔离、网络限制、资源限额（CPU/内存/时间）。
  - 沙箱结果通过 stdout/stderr 收集，不直接访问主进程内存。

#### P92 -- Review Agent（高风险输出审核）
- 目标：在 AgentLoop 中插入 Review 步骤：当工具调用风险高或输出可能敏感时，由更强模型做置信度校验。
- 关键点：
  - Review 不是"人工审批"；是自动化的二次校验。
  - Review 结果作为事件落 `run_events`（`review.requested` / `review.decision`）。

#### P93 -- 记忆层（长期/短期/项目记忆）
- 目标：实现 Agent 的记忆检索能力：长期记忆（用户/组织事实）、短期记忆（最近对话摘要）、项目记忆（案件/项目知识）。

#### P94 -- 订阅与配额
- 目标：预算管理、倍率计算、用量聚合、额度告警。

#### P95 -- SSO/2FA
- 目标：OIDC/SAML 集成、设备管理、登录策略。

#### P96 -- 数据导出与删除
- 目标：可审计的数据导出任务、保留策略、合规删除。

#### P97 -- 离线部署包
- 目标：一体机打包、BYOK/托管网关切换、授权激活流程。
