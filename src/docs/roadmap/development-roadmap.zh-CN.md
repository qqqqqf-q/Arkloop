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

#### P36 — Web Chat MVP（从演示页走向可用页）
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

## 4. 长期路线（先写迁移点，不写兼容性代码）

下面是“企业管理/平台管理的一堆功能”的长期清单；每个都建议按 Pxx 继续拆小步：

### 4.1 Vault 集成（后期）

#### P90 — 引入 Vault（只在真正需要时做）
- 目标：把“密钥来源”从 env/db 扩展到 Vault（或同类 KMS），并完成无停机迁移方案设计。
- 关键点：
  - 不提前写一套复杂插件系统；到这一步再抽象“secret backend”边界即可。
  - 迁移必须可回滚：Vault 不可用时能降级到 db/env（按策略）。

### 4.2 Worker/队列与高风险工具（后期）

#### P100 — tool broker：API 编排 → worker 执行 → 事件回传
#### P101 — 低风险工具 1（纯计算/只读），跑通 `tool.call/tool.result`
#### P102 — 高风险工具 1（web_fetch/shell/code_execute），引入 review 与强审计

### 4.3 企业后台完整能力（后期）

建议按“可运营”优先级继续拆：
- 订阅与配额：预算、倍率、用量聚合、告警
- 导出与删除：导出任务、保留策略、可审计的删除
- SSO/2FA：OIDC/SAML、设备管理、登录策略
- 数据治理：附件/快照分层、加密策略、密钥轮换
