# 后端 API 与 SSE（Phase 1 规范草案）

本文目标：把“可跑通的最小纵切”先定死，避免后续因为流式、审计、多租户与鉴权形态反复返工。

范围：API 资源模型、端点建议、错误模型、SSE 事件规范、以及与鉴权/CLI 的配合方式。

## 1. 总原则（先写死）

- API 只做编排：前端/CLI 调用的是 API；工具执行在服务端受控环境（worker 或同进程 executor）。
- 事件是唯一真相：运行过程的一切都用 `run_events` 表达（推送、回放、审计都基于同一事件）。
- 流式优先：模型输出与工具调用过程统一通过 SSE 推送。
- 多租户先占位：所有写操作必须能归属到 `org_id`（租户）、`project_id`（案件/项目，可选但建议早期就有）。
- 强约束可测试：schema、allowlist、预算、权限、审计字段必须稳定，pytest 重点测这些不变量。

## 2. 资源模型（建议）

最小集合（Phase 1）：
- `orgs`：租户/组织
- `users`：用户（actor）
- `projects`：项目/案件（会话分组）
- `threads`：会话容器
- `messages`：用户/assistant 消息（最终内容或增量片段的归并结果）
- `runs`：一次 Agent Loop 执行实例（状态机）
- `run_events`：事件流（SSE 推送 + 审计落库 + 回放）

后续再补齐（Phase 2+）：
- `tools`：ToolSpec 注册表
- `skills`：SkillSpec/版本资产
- `context_refs`：引用材料（网页快照/附件/检索片段）
- `audit_logs` / `usage` / `exports`：企业后台所需

## 3. 端点设计（Phase 1 最小纵切）

### 3.1 认证与会话

建议先做服务端签发 token（便于本地部署与 CLI），同时为后续 OIDC/SSO 留接口。

- `POST /v1/auth/login`
- `POST /v1/auth/refresh`
- `POST /v1/auth/logout`
- `GET /v1/me`

### 3.2 项目与会话

- `POST /v1/projects`
- `GET /v1/projects`
- `POST /v1/threads`（可选带 `project_id`）
- `GET /v1/threads?project_id=...`
- `POST /v1/threads/{thread_id}/messages`（写入用户消息）
- `GET /v1/threads/{thread_id}/messages`

### 3.3 运行（Run）

Run 负责把「输入消息 + 上下文引用 + 约束策略」变成一次可审计的执行链路。

- `POST /v1/threads/{thread_id}/runs`
  - 典型入参：`mode`（chat/skill）、`skill_id@version`（可选）、`tool_allowlist`、`budgets`、`model_route`（可选）
- `GET /v1/runs/{run_id}`
- `POST /v1/runs/{run_id}:cancel`

### 3.4 事件流（SSE）

- `GET /v1/runs/{run_id}/events`（`Content-Type: text/event-stream`）

约定：
- 事件按 `seq` 单调递增，便于断线重连与回放。
- 支持游标：`?after_seq=123` 或使用 `Last-Event-ID`（二选一，建议游标更直观）。
- 服务端尽量保证“先落库、后推送”；做不到也要保证“至少可回放”（例如最终补写缺失事件）。

### 3.5 Review（可选，但建议早占位）

如果你在 Phase 1 就要引入“高风险工具/外部发送需要审核”，建议占位端点：

- `GET /v1/runs/{run_id}/review`
- `POST /v1/runs/{run_id}/review:approve`
- `POST /v1/runs/{run_id}/review:reject`

## 4. SSE 事件规范（建议最小集合）

事件字段（所有事件共用的 envelope）：
- `event_id`：全局唯一（uuid/ulid 均可）
- `run_id`：归属 run
- `seq`：run 内单调递增序号
- `ts`：服务端时间戳
- `type`：事件类型
- `data`：事件负载（按 type 变化）

事件类型建议：
- `run.started`
- `run.completed`
- `run.failed`（含 `error_class`/`message`）
- `message.delta`（模型流式增量；`data` 至少包含 `content_delta`、可选 `role`/`channel`）
- `tool.call`（`tool_name`、`args_hash`、`risk_level`、`required_scopes`、预算预估）
- `tool.result`（`result_hash`、`duration_ms`、`error_class`、`cost`）
- `policy.denied`（权限不足/参数非法/高危拦截）
- `budget.exceeded`
- `review.requested`
- `review.decision`

说明：
- 不追求逐字一致，但必须保证事件序列可解释、可核对、可回放。
- `tool.call/tool.result` 必须能关联（例如 `tool_call_id`），避免审计断链。

## 5. 错误模型（API 层）

建议统一错误响应（便于前端与 CLI 处理）：
- `code`：稳定的机器可读错误码（例如 `auth.invalid_credentials`）
- `message`：给人看的简短描述
- `details`：可选（字段校验错误、触发的 policy、trace_id 等）
- `trace_id`：全链路追踪 ID

建议同时在 HTTP Header 返回 `X-Trace-Id`，便于在“非 JSON 场景”（SSE/代理层报错/网关日志）里快速关联。

`trace_id` 建议默认由服务端生成；如需要与受信任上游（网关/负载均衡）对齐，可允许透传其 `X-Trace-Id`，但不要信任普通客户端自带的 `trace_id`。

建议分类：
- `auth.*`：鉴权/权限
- `validation.*`：schema 校验
- `policy.*`：策略拦截（允许前端展示“为什么被拒绝”）
- `budget.*`：预算/配额
- `provider.*`：模型提供商错误（需进一步细分可重试/不可重试）
- `internal.*`：未知内部错误（尽量不要泄露敏感细节）

## 6. SSE 与鉴权（需要提前定的现实约束）

浏览器原生 `EventSource` 不方便携带自定义 Header（常见是没法加 `Authorization`）。

可选策略（Phase 1 建议二选一）：
- Cookie 会话：SSE 走同源 Cookie（更适配 `EventSource`），配合 CSRF 防护与严格 SameSite 策略。
- Fetch 流式：不用 `EventSource`，改用 `fetch()` 读取 `text/event-stream`，这样可加 `Authorization: Bearer ...`；前端实现稍复杂但更统一，CLI 也一致。

当前约定（Phase 1 默认）：
- 采用 **Fetch 流式 + `Authorization: Bearer ...`**。
  - 理由：SSE 与普通 API 使用同一鉴权机制；Web/CLI 统一实现；避免 Cookie 会话带来的 CSRF 与同源约束复杂度。
  - 代价：前端需要实现 SSE 解析与断线重连（建议以 `after_seq` 作为唯一续传游标，避免依赖 `Last-Event-ID` 的浏览器差异）。

无论选哪种，都建议：
- `run_events` 本身不包含敏感明文（例如模型 key、系统 prompt 原文）；敏感内容通过服务端权限控制与脱敏策略处理。

## 7. 与 CLI/测试的配合（为什么 SSE-first 更省事）

- CLI：`POST runs` 后直接 `GET /runs/{id}/events` 消费事件即可，不需要复杂连接管理。
- pytest：用同一套事件 schema 做回放断言（工具调用是否在 allowlist、是否被 policy 拦截、是否记录了成本与 trace_id）。

## 8. Phase 1 建议的“最小可跑链路”

建议把工程推进拆成可验证的纵切：

1) `threads/messages/runs` 的数据模型 + `run_events` 落库
2) `POST /runs` 创建 run 并写入 `run.started`
3) SSE 推送事件（先推 `run.started`，再补 `message.delta` 的假数据也可以被测试替换为 stub provider，但不要用虚假业务逻辑糊弄）
4) 接入 provider stub（录制/重放或纯 mock），让 `message.delta` 真实来自“可控输出源”
5) 接入第一批低风险 tool（只读类），把 `tool.call/tool.result` 跑通并可审计

当这条链路稳定后，再扩展到高风险工具与 review 流程。
