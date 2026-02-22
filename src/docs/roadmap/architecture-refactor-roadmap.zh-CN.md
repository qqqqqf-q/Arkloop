# 架构重构路线图（从审计报告到目标架构）

本文是 Arkloop 架构重构的薄片式执行路线。每个薄片（Slice）可独立验收，按 Phase 分组推进。

关联文档：
- 审计报告：`src/docs/architecture/architecture-problems.zh-CN.md`
- 目标架构：`src/docs/architecture/architecture-design-v2.zh-CN.md`
- 开发路线：`src/docs/roadmap/development-roadmap.zh-CN.md`

## 0. 当前代码基线

重构的起点是已完成的仓库现状，不能脱离实际代码凭空设计：

**API 服务（`src/services/api/`）：**
- Go `net/http`，11 个 goose migration（截止 `00011`）
- 表：`orgs`（3 字段）、`users`（3 字段 + `tokens_invalid_before`）、`user_credentials`（login/password_hash）、`org_memberships`（role TEXT）、`threads`、`messages`（content TEXT）、`runs`（status TEXT 无 CHECK）、`run_events`（seq 通过行级锁分配）、`audit_logs`、`jobs`
- 鉴权：JWT Bearer + `tokens_invalid_before` 全局失效策略
- 端点：auth（login/register/refresh/logout/me）、threads CRUD、messages CRUD、runs（create/cancel/get/list/events SSE）

**Worker 服务（`src/services/worker/`）：**
- `consumer/loop.go`：`SKIP LOCKED` 消费 + heartbeat + advisory lock 去重
- `runengine/v1.go`：Agent Loop 多轮执行，200ms 批提交 + LISTEN/NOTIFY 取消信号
- `routing/config.go`：`ARKLOOP_PROVIDER_ROUTING_JSON` 环境变量加载凭证/路由
- `mcp/`：stdio 传输 + 内存连接池，配置从 `ARKLOOP_MCP_CONFIG_FILE` 加载
- `skills/`：从文件系统 `src/skills/` 加载 `skill.yaml + prompt.md`
- `tools/`：内置工具（echo/noop/web_search/web_fetch）+ MCP 工具

**基础设施：**
- `compose.yaml`：仅 PostgreSQL 16-alpine
- 无 Redis、无对象存储、无 Gateway、无可观测性

## 1. 重构入手策略

审计报告列出了 7 类 P0 + 10 类 P1 + 11 类 P2 问题。不能同时开工。入手原则如下：

**先修地基，再盖楼：**
- 数据库 schema 的缺陷（users 缺 email、runs 缺生命周期字段、run_events 行锁热点）是所有上层功能的前提。schema 改错了后续全部返工。
- Redis 是 Gateway 限流、Worker 分布式锁、SSE 跨实例广播的共同依赖。不引入 Redis，Gateway 和实时推送改造都做不了。

**先内后外：**
- 先修复数据模型和内部契约（schema + Worker 事件写入路径），再做外部暴露面（Gateway、Webhooks、API Keys）。
- 先修 Worker 执行路径的结构性问题（MCP 远端化、Skills 入库、凭证入库），再做 Console 管理界面。

**先阻塞后增强：**
- P0 问题（阻塞扩展）优先于 P1（功能缺失）优先于 P2（规模化后暴露）。
- 但同一 Phase 内按依赖关系排列，而不是严格按 P0/P1/P2 排。

## 2. Phase 总览

| Phase | 主题 | 核心目标 | 涉及审计问题 |
|---|---|---|---|
| Phase 1 | 数据模型修正 | 修复 schema 的结构性缺陷，为后续所有功能打地基 | P0: users 缺 email; P0: run_events 行锁; P1: runs 缺生命周期字段; P1: messages 不支持多模态; P1: 无软删除; P1: runs.status 不可信 |
| Phase 2 | 基础设施引入 | 引入 Redis + 对象存储，消除单点依赖 | P0: 没有 Redis |
| Phase 3 | Worker 执行路径重构 | MCP 远端化 + Skills 入库 + 凭证入库 + 事件写入优化 | P0: MCP stdio 绑定; P0: Worker 调度盲目; P1: Skills 绑定文件系统; P1: LLM 凭证锁死 env var; P1: SSE 双重延迟 |
| Phase 4 | Gateway + 安全基础 | 独立 Gateway 层 + secrets 统一管理 + API Keys | P0: 没有 Gateway 层; P1: 没有 secrets 表; P2: 没有 API Key 管理 |
| Phase 5 | 组织模型与权限 | org 邀请 + RBAC + teams/projects 层级 | P1: 没有 org 邀请; P2: RBAC 过于简陋; P2: 没有 projects/teams 层级 |
| Phase 6 | 企业级能力 | Webhooks + 审计补全 + 通知 + Feature Flags | P1: 没有 Webhooks; P1: 审计日志缺 IP; P2: 没有通知系统; P2: 没有 Feature Flags |

---

## 3. Phase 1 -- 数据模型修正

目标：修复数据库 schema 的结构性缺陷。这是整个重构的地基，不碰业务代码逻辑，只做 migration + repository 层适配。

### R10 -- users 表补全（email + status + soft delete）

- **关联审计**：二.2.1「用户身份是残缺的」；P0「users 表没有 email」
- **关联目标架构**：六.6.1 `ALTER TABLE users ADD COLUMN email/status/deleted_at/...`
- **目标**：`users` 表增加 `email`、`email_verified_at`、`status`、`deleted_at`、`avatar_url`、`locale`、`timezone`、`last_login_at`。现有数据不受影响（全部 nullable 或带默认值）。
- **关键点**：
  - `email` 先 nullable（现有用户没有 email），后续注册流程再改为必填。
  - `status` 默认 `'active'`，CHECK 约束 `('active', 'suspended', 'deleted')`。
  - `deleted_at` 配合 `uq_users_email` 唯一索引要加 `WHERE deleted_at IS NULL` 条件。
  - `User` struct（`src/services/api/internal/data/users_repo.go`）需要同步扩展字段。
  - `auth.Service` 的 `AuthenticateUser` 需要检查 `status != 'suspended'`，suspended 用户拒绝登录。
- **具体改动范围**：
  - 新建 `src/services/api/internal/migrate/migrations/00012_users_add_email_status.sql`
  - 修改 `src/services/api/internal/data/users_repo.go`：`User` struct 增加字段，`Create`/`GetByID` 更新 SELECT/INSERT。
  - 修改 `src/services/api/internal/auth/service.go`：登录时校验 `status`。
  - 修改 `src/services/api/internal/migrate/migrate.go`：`ExpectedVersion` 递增。
- **验收**：
  - `go test -tags integration ./internal/migrate/...`：migration up/down 无报错。
  - `go test -tags integration ./internal/http/...`：auth 测试通过（现有用户 email=NULL 不影响登录）。
  - 手工：`psql` 确认 `\d users` 新字段存在且约束生效。

### R11 -- orgs 表补全（owner + status + settings）

- **关联审计**：二.2.2「组织模型过于简陋」
- **关联目标架构**：六.6.2 `ALTER TABLE orgs ADD COLUMN owner_user_id/status/settings_json/...`
- **目标**：`orgs` 增加 `owner_user_id`、`status`、`country`、`timezone`、`logo_url`、`settings_json`、`deleted_at`。
- **关键点**：
  - `owner_user_id` nullable（现有 org 没有 owner，后续手动回填或在注册时自动设置）。
  - `settings_json` 默认 `'{}'::jsonb`，为后续 BYOK 开关、MFA 强制等 org 级配置预留。
  - `status` CHECK `('active', 'suspended')`。
  - `Org` struct（`src/services/api/internal/data/orgs_repo.go`）同步扩展。
- **具体改动范围**：
  - 新建 migration `00013_orgs_add_owner_status.sql`
  - 修改 `orgs_repo.go`
- **验收**：
  - `go test -tags integration ./internal/migrate/...`
  - 手工：`\d orgs` 确认新字段。

### R12 -- runs 表补全（生命周期 + 成本 + status CHECK）

- **关联审计**：二.2.4「Run 表缺少关键生命周期字段」；P1「runs.status 不可信」
- **关联目标架构**：六.6.6 `ALTER TABLE runs ADD COLUMN completed_at/failed_at/duration_ms/total_input_tokens/...`
- **目标**：`runs` 增加 `parent_run_id`、`status_updated_at`、`completed_at`、`failed_at`、`duration_ms`、`total_input_tokens`、`total_output_tokens`、`total_cost_usd`、`model`、`skill_id`、`deleted_at`。为 `status` 加 CHECK 约束。
- **关键点**：
  - 现有 `runs.status` 全部为 `'running'`（Worker 写入后从不更新）。migration 需要先把已有的非法状态修正后再加 CHECK。
  - `migration/00011_fix_stale_running_runs.sql` 已存在相关逻辑，本次在其基础上扩展。
  - CHECK 允许的值：`('running', 'completed', 'failed', 'cancelled', 'cancelling')`。
  - `Run` struct（`runs_repo.go`）同步扩展。
  - Worker 的 `eventWriter` 在写入终态事件（`run.completed`/`run.failed`/`run.cancelled`）时，需要同步 `UPDATE runs SET status=$1, completed_at/failed_at=now(), duration_ms=... WHERE id=$2`。这是 **R30**（Phase 3）的工作，R12 只做 schema。
- **具体改动范围**：
  - 新建 migration `00014_runs_add_lifecycle_fields.sql`
  - 修改 `runs_repo.go`：`Run` struct 增加字段；新增 `UpdateRunTerminalStatus` 方法（供 Worker 调用）。
- **验收**：
  - `go test -tags integration ./internal/migrate/...`
  - `go test -tags integration ./internal/http/...`：现有 runs API 测试通过。
  - 手工：对已有 `running` 状态的 run 不受影响；手动 INSERT 非法 status 被拒绝。

### R13 -- run_events 序号分配改为 PostgreSQL sequence

- **关联审计**：二.2.5「事件表有三个定时炸弹 -- 第一个：行级锁热点」；P0
- **关联目标架构**：六.6.7 `CREATE SEQUENCE run_events_seq_global`
- **目标**：消除 `UPDATE runs SET next_event_seq = next_event_seq + 1` 的行级锁争用，改用 PostgreSQL 全局 sequence。
- **关键点**：
  - 创建 `run_events_seq_global` sequence。
  - 修改 `run_events.seq` 的 DEFAULT 为 `nextval('run_events_seq_global')`。
  - `runs.next_event_seq` 字段不再需要，但不急着 DROP（先保留，Phase 完成后清理）。
  - **API 侧**：`RunEventRepository.allocateSeq()`（`runs_repo.go`）改为 `SELECT nextval('run_events_seq_global')`。
  - **Worker 侧**：`data.RunEventsRepository.AppendEvent()`（`src/services/worker/internal/data/`）同样改为使用 sequence。
  - sequence 是全局递增的，不再保证同一 run 内的 seq 连续。SSE 的 `after_seq` 语义不变（大于某个 seq 的所有事件），但前端不应假设 seq 连续。
  - 需要检查前端 SSE 消费逻辑是否依赖 seq 连续性（当前 `after_seq` 只做 `> $1` 比较，不依赖连续）。
- **具体改动范围**：
  - 新建 migration `00015_run_events_use_sequence.sql`：创建 sequence，设置 START 为当前 `run_events.seq` 最大值 + 1。
  - 修改 `src/services/api/internal/data/runs_repo.go`：`allocateSeq` 改为 `SELECT nextval`。
  - 修改 `src/services/worker/internal/data/`：同步修改事件写入逻辑。
- **验收**：
  - `go test -tags integration ./...`（api + worker）
  - 手工：启动 API + Worker，创建 run，SSE 事件流 seq 单调递增且不出现行锁等待。
  - 压测验证（可选）：并发创建 10 个 run，观察 `pg_stat_activity` 不出现 `wait_event_type = Lock` 在 `runs` 表上。

### R14 -- messages 增加 content_json（多模态预留）

- **关联审计**：二.2.3「消息表不支持多模态」；P1
- **关联目标架构**：六.6.5 `ALTER TABLE messages ADD COLUMN content_json JSONB`
- **目标**：`messages` 增加 `content_json`、`metadata_json`、`deleted_at`、`token_count`。现有 `content TEXT` 保留不动（向后兼容）。
- **关键点**：
  - `content_json` nullable。当 `content_json` 为 NULL 时，读取 `content`（纯文本）；当 `content_json` 非 NULL 时，优先使用 `content_json`。
  - `content_json` 格式参考 Anthropic Messages API（目标架构六.6.5 已定义）。
  - 本步只做 schema + repository 层适配。Worker 写入 assistant 消息时仍写 `content TEXT`（后续 R31 改为写 `content_json`）。
  - `Message` struct（`messages_repo.go`）增加 `ContentJSON`、`MetadataJSON`、`DeletedAt`、`TokenCount` 字段。
  - `ListByThread` 的 WHERE 条件增加 `AND deleted_at IS NULL`。
- **具体改动范围**：
  - 新建 migration `00016_messages_add_content_json.sql`
  - 修改 `src/services/api/internal/data/messages_repo.go`
  - 修改 `src/services/worker/internal/data/`（如果 Worker 有独立的 messages repo）
- **验收**：
  - `go test -tags integration ./...`
  - 手工：现有消息（content TEXT）正常显示；新消息写入 content_json 后 API 可正确返回。

### R15 -- threads / runs / messages 统一软删除

- **关联审计**：二.2.7「所有删除都是硬删除」；P1
- **关联目标架构**：六.6.4 `threads ADD COLUMN deleted_at`；六.6.6 `runs ADD COLUMN deleted_at`
- **目标**：`threads` 增加 `deleted_at`。（`messages.deleted_at` 在 R14 已加；`runs.deleted_at` 在 R12 已加。）所有 SELECT 查询加 `WHERE deleted_at IS NULL`。
- **关键点**：
  - `threads.deleted_at` nullable，默认 NULL。
  - `threads` 同时增加 `project_id`（nullable，为 Phase 5 teams/projects 预留，此步不建外键）。
  - 删除操作改为 `UPDATE ... SET deleted_at = now()`，不再物理删除。
  - 审计日志记录 soft delete 操作。
  - `threads_repo.go` 所有 SELECT 追加 `AND deleted_at IS NULL`。
- **具体改动范围**：
  - 新建 migration `00017_threads_add_soft_delete.sql`
  - 修改 `threads_repo.go`
- **验收**：
  - `go test -tags integration ./...`
  - 手工：`DELETE` API（如果有）改为 soft delete；列表接口不返回已删除项。

### R16 -- audit_logs 补全（IP + User-Agent + 变更状态）

- **关联审计**：五.5.2「审计日志缺少关键信息」；P1
- **关联目标架构**：六.6.19 `ALTER TABLE audit_logs ADD COLUMN ip_address/user_agent/api_key_id/before_state_json/after_state_json`
- **目标**：`audit_logs` 增加 `ip_address INET`、`user_agent TEXT`、`api_key_id UUID`、`before_state_json JSONB`、`after_state_json JSONB`。
- **关键点**：
  - 全部 nullable（现有审计记录不受影响）。
  - `audit.Writer` 的写入方法需要扩展入参，从 HTTP request 中提取 IP + User-Agent。
  - 中间件 `middleware.go` 在 context 中注入 `RemoteAddr` + `User-Agent`，`audit.Writer` 从 context 读取。
  - `before_state_json`/`after_state_json` 在有状态变更的操作时由 handler 主动填写（不强制所有操作都填）。
- **具体改动范围**：
  - 新建 migration `00018_audit_logs_add_ip_ua_state.sql`
  - 修改 `src/services/api/internal/audit/`：扩展写入方法。
  - 修改 `src/services/api/internal/http/middleware.go`：context 注入。
- **验收**：
  - `go test -tags integration ./...`
  - 手工：登录后查看 `audit_logs` 表，确认 `ip_address` 和 `user_agent` 已填充。

---

## 4. Phase 2 -- 基础设施引入

目标：引入 Redis 和对象存储（MinIO），为 Gateway、实时推送、分布式锁、缓存等后续改造提供基础设施。

### R20 -- compose.yaml 引入 Redis

- **关联审计**：P0「没有 Redis」
- **关联目标架构**：七「Redis 规范」；十三「compose.yaml 目标」
- **目标**：`compose.yaml` 新增 Redis service，Go 服务引入 `go-redis` 依赖并验证连通性。
- **关键点**：
  - Redis 7.x-alpine，端口 6379，持久化策略 appendonly（开发环境用）。
  - 新增 `ARKLOOP_REDIS_URL` 环境变量（格式 `redis://:password@host:port/db`）。
  - API 和 Worker 的 `go.mod` 各加 `github.com/redis/go-redis/v9`。
  - 先只做连通性验证（ping），不在业务逻辑中使用。
  - 健康检查：`redis-cli ping`。
- **具体改动范围**：
  - 修改 `compose.yaml`：新增 `redis` service。
  - 新建 `src/services/api/internal/data/redis.go`：连接池初始化 + Ping。
  - 新建 `src/services/worker/internal/data/redis.go`：同上。
  - `.env.example` 新增 `ARKLOOP_REDIS_URL`。
- **验收**：
  - `docker compose up -d` 后 `docker compose exec redis redis-cli ping` 返回 PONG。
  - `go test -tags integration ./...`（包含 Redis ping 测试）。

### R21 -- compose.yaml 引入 MinIO（对象存储）

- **关联审计**：P2「messages 大内容无限存 DB」
- **关联目标架构**：八「对象存储」；十三 `compose.yaml` 新增 `minio`
- **目标**：`compose.yaml` 新增 MinIO service，验证 S3 兼容接口可用。
- **关键点**：
  - MinIO 最新稳定版，console 端口 9001，API 端口 9000。
  - 新增 `ARKLOOP_S3_ENDPOINT`、`ARKLOOP_S3_ACCESS_KEY`、`ARKLOOP_S3_SECRET_KEY` 环境变量。
  - 本步只验证连通性和 bucket 创建，不在业务中使用。
  - Go 侧引入 `github.com/minio/minio-go/v7` 或 `github.com/aws/aws-sdk-go-v2/service/s3`（选 S3 兼容接口，生产可无缝切换 AWS/GCS/OSS）。
- **具体改动范围**：
  - 修改 `compose.yaml`
  - `.env.example` 新增 S3 相关变量
  - 新建 `src/services/api/internal/data/s3.go`（或 `objectstore.go`）：初始化 + 创建 bucket + 上传/下载测试
- **验收**：
  - `docker compose up -d` 后访问 MinIO console `http://localhost:9001` 可登录。
  - `go test -tags integration ./...`（包含 S3 连通性测试）。

---

## 5. Phase 3 -- Worker 执行路径重构

目标：解决 Worker 侧的结构性问题 -- MCP 远端化、Skills 入库、凭证从环境变量迁移到数据库、事件写入路径优化、runs.status 同步更新。

### R30 -- Worker 写入终态时同步更新 runs 状态

- **关联审计**：二.2.4「runs.status 不可信」；P1
- **依赖**：R12（runs 表已有新字段）
- **目标**：Worker 在写入 `run.completed` / `run.failed` / `run.cancelled` 事件时，同步执行 `UPDATE runs SET status=..., completed_at/failed_at=now(), duration_ms=..., total_input_tokens=..., total_output_tokens=..., total_cost_usd=...`。
- **关键点**：
  - 更新在同一个事务内完成（`eventWriter.Append` 写事件和更新 runs 在同一个 tx 中）。
  - `duration_ms` = `now() - runs.created_at` 的毫秒数。
  - `total_input_tokens` / `total_output_tokens` / `total_cost_usd` 从 LLM 响应的 usage 字段汇总（当前 `LlmStreamRunCompleted.Usage` 已有）。需要在 `eventWriter` 或 `AgentLoop` 中累加多轮的 usage。
  - `model` / `skill_id` 在 `run.started` 时从 `inputJSON` 写入。
  - 更新完成后，`runs.status` 成为可信字段，API 查询不再需要扫描 `run_events` 推导状态。
- **具体改动范围**：
  - 修改 `src/services/worker/internal/runengine/v1.go`：`eventWriter` 在写入终态事件时调用 `RunsRepository.UpdateTerminalStatus`。
  - 修改 `src/services/worker/internal/data/`：新增 `UpdateTerminalStatus` 方法（或在 API 的 data 包中如果共享）。
  - 修改 `src/services/worker/internal/agent/`：累加多轮 usage。
- **验收**：
  - `go test -tags integration ./...`（worker）
  - 手工：创建 run 后等待完成，查询 `SELECT status, completed_at, duration_ms, total_input_tokens FROM runs WHERE id = $1` 确认字段已更新。

### R31 -- 消除 SSE 双重延迟

- **关联审计**：四「实时推送链路是假的」；P1「SSE 双重延迟」
- **关联目标架构**：五「实时推送」；五.5.1 `pg_notify('run_events:{run_id}', seq)`
- **目标**：消除 Worker 200ms 批提交 + API 250ms 轮询的双重延迟。
- **关键点**：
  - **Worker 侧**：`message.delta` 等流式事件不再攒批，每个事件立即提交（或至少每个事件立即 `pg_notify`，批提交间隔缩短到 50ms 以内）。
  - **API 侧**：SSE handler 改为 `LISTEN run_events:{run_id}` 驱动，不再 250ms 轮询。收到 NOTIFY 后立即查库推送。
  - 性能权衡：delta 事件每个都单独 commit 会增加 PG 负载。折中方案是保留批提交但将间隔从 200ms 降到 50ms，同时每次 commit 后立即 `pg_notify`。API 侧用 LISTEN 替代轮询。
  - 多副本 API：Phase 2 引入 Redis 后，`pg_notify` 只能到达一个 API 实例（LISTEN 的那个）。需要 Redis Pub/Sub 做跨实例广播。本步先做单实例优化（`pg_notify` + LISTEN），多实例广播在 R20 Redis 引入后补充。
- **具体改动范围**：
  - 修改 `src/services/worker/internal/runengine/v1.go`：`eventCommitMaxInterval` 从 200ms 降到 50ms；commit 后执行 `pg_notify`。
  - 修改 `src/services/api/internal/http/v1_runs.go`：SSE handler 从轮询改为 LISTEN 驱动。
- **验收**：
  - 手工：浏览器中观察流式输出，token 到达间隔明显缩短（从批量到达变为接近逐 token 到达）。
  - `go test -tags integration ./...`

### R32 -- secrets 统一管理表

- **关联审计**：五.5.1「没有 secrets 表」；P1
- **关联目标架构**：六.6.8 `CREATE TABLE secrets`；九「凭证加密」
- **目标**：创建 `secrets` 表，实现应用层加密（AES-256-GCM），为后续 MCP auth token、LLM API Key 的入库存储提供统一基础。
- **关键点**：
  - 主密钥从 `ARKLOOP_ENCRYPTION_KEY` 环境变量读取（32 字节 hex）。
  - `secrets` 表字段：`id`、`org_id`、`name`、`encrypted_value`、`key_version`、`created_at`、`updated_at`、`rotated_at`。
  - `encrypted_value` 格式：`base64(nonce + ciphertext + tag)`。
  - `key_version` 支持密钥轮换：解密时根据 `key_version` 选择对应密钥。
  - 新建 `src/services/api/internal/crypto/` 包：`Encrypt(plaintext, keyVersion) -> ciphertext`、`Decrypt(ciphertext, keyVersion) -> plaintext`。
  - 新建 `src/services/api/internal/data/secrets_repo.go`：CRUD + 按 org_id + name 唯一查找。
- **具体改动范围**：
  - 新建 migration `00019_create_secrets.sql`
  - 新建 `src/services/api/internal/crypto/envelope.go`
  - 新建 `src/services/api/internal/data/secrets_repo.go`
- **验证**：
  - `go test ./internal/crypto/...`：加密/解密 round-trip；无密钥时解密失败；新旧版本兼容。
  - `go test -tags integration ./internal/data/...`：secrets CRUD。

### R33 -- LLM 凭证入库（从环境变量迁移到数据库）

- **关联审计**：三.3.3「LLM 凭证锁死在环境变量里」；P1
- **关联目标架构**：六.6.10 `CREATE TABLE llm_credentials` + `llm_routes`
- **依赖**：R32（secrets 表 + 加密能力）
- **目标**：创建 `llm_credentials` 和 `llm_routes` 表。Worker 的 `routing/config.go` 支持从数据库加载路由配置（同时保留环境变量作为 fallback）。
- **关键点**：
  - `llm_credentials` 的 `secret_id` 关联 `secrets` 表，API Key 加密存储。
  - 环境变量 `ARKLOOP_PROVIDER_ROUTING_JSON` 保留为 fallback（当数据库中无路由配置时使用）。
  - Worker 启动时优先从数据库加载路由；数据库无配置则回退到环境变量。
  - API 侧新增管理端点（P44 Console 凭证管理的后端部分）：
    - `POST /v1/llm-credentials`
    - `GET /v1/llm-credentials`
    - `DELETE /v1/llm-credentials/{id}`
  - 安全：API 响应永远不返回明文 API Key（只返回 key_prefix + 掩码）。
- **具体改动范围**：
  - 新建 migration `00020_create_llm_credentials_routes.sql`
  - 新建 `src/services/api/internal/data/llm_credentials_repo.go`
  - 新建 `src/services/api/internal/data/llm_routes_repo.go`
  - 修改 `src/services/worker/internal/routing/config.go`：支持从 DB 加载。
  - 新建 `src/services/api/internal/http/v1_llm_credentials.go`
- **验收**：
  - `go test -tags integration ./...`
  - 手工：通过 API 创建 LLM 凭证 -> Worker 使用该凭证执行 run -> 成功。

### R34 -- MCP 配置入库 + Remote HTTP/SSE 传输

- **关联审计**：三.3.1「MCP 是单机 IDE 的设计，不是 SaaS 的设计」；P0
- **关联目标架构**：六.6.9 `CREATE TABLE mcp_configs`；十「MCP 架构」
- **依赖**：R32（secrets 表 + 加密能力）
- **目标**：创建 `mcp_configs` 表，MCP 配置从数据库加载（保留文件系统 fallback），支持 Remote HTTP/SSE 传输。
- **关键点**：
  - `mcp_configs` 支持三种 transport：`http_sse`、`streamable_http`、`stdio`。
  - `auth_secret_id` 关联 `secrets` 表（Bearer token 加密存储）。
  - `mcp/config.go` 的 `LoadConfigFromEnv()` 保留为 fallback；新增 `LoadConfigFromDB(ctx, pool, orgID)` 从数据库加载。
  - Worker 执行 Run 时按 org_id 加载该 org 的 MCP 配置。
  - `mcp/pool.go` 的 key 从 `server_id` 改为 `(org_id, server_id)`，避免跨租户共享。
  - 新增 Remote HTTP 传输的 MCP client 实现（当前只有 stdio）。
  - API 侧新增管理端点：
    - `POST /v1/mcp-configs`
    - `GET /v1/mcp-configs`
    - `PATCH /v1/mcp-configs/{id}`
    - `DELETE /v1/mcp-configs/{id}`
- **具体改动范围**：
  - 新建 migration `00021_create_mcp_configs.sql`
  - 新建 `src/services/api/internal/data/mcp_configs_repo.go`
  - 新建 `src/services/api/internal/http/v1_mcp_configs.go`
  - 修改 `src/services/worker/internal/mcp/config.go`：`LoadConfigFromDB`
  - 新建 `src/services/worker/internal/mcp/http_client.go`：Remote HTTP/SSE 传输
  - 修改 `src/services/worker/internal/mcp/pool.go`：key 改为 `(org_id, server_id)`
- **验收**：
  - `go test -tags integration ./...`
  - 手工：通过 API 创建 MCP 配置 -> Worker 使用远程 MCP Server 执行工具调用。

### R34.5 -- LLM 路由配置 per-run 动态加载

- **背景**：R33 实现了 Worker 启动时从 DB 一次性加载 LLM 路由配置，但 R34 的 MCP 工具已实现 per-run 动态加载。两者行为不一致：凭证/路由变更后必须重启 Worker 才能生效，而 MCP 配置变更无需重启。
- **目标**：将 LLM 路由配置的加载时机从「Worker 启动」改为「per-run 执行」，与 MCP 工具的动态加载模式对齐。
- **关键点**：
  - `EngineV1` 不再在构造时持有静态 `Router`，改为在 `Execute` 时按需从 DB 加载路由配置并构建 `ProviderRouter`。
  - 路由解析结果不跨 run 缓存（每个 run 独立查询），保证配置变更实时生效，无需重启 Worker。
  - `ComposeNativeEngine` 中的 `loadRoutingConfig` 仍保留，用于初始化 stub/env-only 路由配置作为 fallback。
  - `EngineV1Deps` 新增 `DBPool *pgxpool.Pool` 字段，供 `Execute` 内部按 run 加载路由。
  - 回退逻辑：DB 无可用路由时回退到 `EngineV1` 初始化时的静态路由配置（即当前 env var 加载结果）。
- **具体改动范围**：
  - 修改 `src/services/worker/internal/runengine/v1.go`：`Execute` 方法开始时调用 `routing.LoadRoutingConfigFromDB`，构建 per-run `ProviderRouter`；DB 失败时回退到 `e.router`（静态 fallback）。
  - 修改 `src/services/worker/internal/app/composition.go`：`EngineV1Deps` 注入 `DBPool`。
- **验收**：
  - `go test -tags integration ./...`（worker）
  - 手工：Worker 运行中通过 API 修改 LLM 凭证 -> 无需重启，下一个 run 立即使用新凭证。

### R35 -- Skills 入库

- **关联审计**：三.3.4「Skills 绑定在文件系统」；P1
- **关联目标架构**：六.6.11.3 `CREATE TABLE skills`
- **目标**：创建 `skills` 表，Skills 从数据库加载（保留文件系统 fallback）。
- **关键点**：
  - `skills` 表字段：`id`、`org_id`（NULL 表示全局 skill）、`skill_key`、`version`、`display_name`、`description`、`prompt_md`、`tool_allowlist`、`budgets_json`、`is_active`、`created_at`。
  - 唯一约束 `(org_id, skill_key, version)`。
  - Worker 的 `skills/loader.go` 保留文件系统加载作为 fallback（内置 skill）；新增从 DB 加载的路径。
  - DB 中的 org 级 skill 覆盖文件系统中的同名 skill。
  - API 侧新增管理端点（面向后续 Console）：
    - `POST /v1/skills`
    - `GET /v1/skills`
    - `PATCH /v1/skills/{id}`
- **具体改动范围**：
  - 新建 migration `00022_create_skills.sql`
  - 新建 `src/services/api/internal/data/skills_repo.go`
  - 新建 `src/services/api/internal/http/v1_skills.go`
  - 修改 `src/services/worker/internal/skills/loader.go`：`LoadFromDB` + fallback 逻辑
- **验收**：
  - `go test -tags integration ./...`
  - 手工：通过 API 创建 skill -> 使用该 skill_id 创建 run -> skill 约束生效。

### R36 -- Worker 注册与心跳 + 任务路由标签

- **关联审计**：三.3.2「Worker 调度是盲目的」；P0
- **关联目标架构**：六.6.17 `CREATE TABLE worker_registrations`；六.6.18 `jobs ADD COLUMN worker_tags`
- **依赖**：R20（Redis）
- **目标**：Worker 启动时注册到 Redis + 数据库，上报能力标签。任务调度按标签匹配。
- **关键点**：
  - Worker 启动时生成 `worker_id`（UUID），写入 Redis Hash `arkloop:worker:{worker_id}`（TTL 30s，每 10s 心跳续期）并同步写入 `worker_registrations` 表。
  - `worker_registrations` 字段：`id`、`worker_id`、`hostname`、`version`、`status`（active/draining/dead）、`capabilities JSONB`、`current_load`、`max_concurrency`、`heartbeat_at`、`registered_at`。
  - `jobs` 增加 `worker_tags TEXT[]`。Worker Lease 时 SQL 加 `worker_tags <@ $capabilities` 过滤。
  - Worker 停止时标记 `status = 'draining'` -> `'dead'`，Redis key DEL。
- **具体改动范围**：
  - 新建 migration `00023_create_worker_registrations.sql`
  - 新建 migration `00024_jobs_add_worker_tags.sql`
  - 新建 `src/services/worker/internal/registration/` 包
  - 修改 `src/services/worker/internal/consumer/loop.go`：Lease SQL 加 tag 过滤
  - 修改 `src/services/worker/cmd/worker/main.go`：启动/停止生命周期注册
- **验收**：
  - `go test -tags integration ./...`
  - 手工：启动 Worker，查看 `worker_registrations` 表有记录；`redis-cli hgetall arkloop:worker:{id}` 有数据；停止 Worker 后 status 变为 dead。

---

## 6. Phase 4 -- Gateway + 安全基础

目标：引入独立 Gateway 层，实现限流、IP 过滤、API Key 验证，建立外部访问的安全屏障。

### R40 -- Gateway 独立服务骨架

- **关联审计**：一「流量入口：没有 Gateway 层」；P0
- **关联目标架构**：二「Gateway 层」
- **依赖**：R20（Redis）
- **目标**：新建 `src/services/gateway/` 独立 Go 服务，作为 API 的反向代理，承担 token 验证、限流、IP 过滤。
- **关键点**：
  - 技术选型：Go `net/http/httputil.ReverseProxy`，复用现有技术栈。
  - Gateway 是 stateless 的，不持有数据库连接（只访问 Redis）。
  - 启动时从环境变量读取 upstream API 地址。
  - 第一版只做透传（验证 Gateway -> API 链路可用），限流和 IP 过滤在后续 R41/R42 实现。
  - `compose.yaml` 新增 `gateway` service。
- **具体改动范围**：
  - 新建 `src/services/gateway/` 目录结构（`cmd/gateway/main.go`、`internal/proxy/`）
  - 修改 `compose.yaml`：新增 gateway service
- **验收**：
  - `docker compose up -d` 后通过 Gateway 端口访问 API，所有现有接口正常工作。
  - `go test ./...`（gateway）

### R41 -- Gateway 限流（Redis token bucket）

- **关联审计**：一「没有任何限流机制」
- **依赖**：R40 + R20
- **目标**：Gateway 按 org_id 实现 Redis token bucket 限流。
- **关键点**：
  - Redis key：`arkloop:ratelimit:{org_id}:{window}`（窗口可按秒/分/时）。
  - 限流策略通过环境变量配置（后续迁移到数据库）：默认每分钟 60 请求/org。
  - 超限返回 HTTP 429 + `Retry-After` header。
  - SSE 长连接不计入普通请求限流（建议按连接数限制）。
- **具体改动范围**：
  - 新建 `src/services/gateway/internal/ratelimit/`
  - 修改 proxy 中间件链
- **验收**：
  - `go test ./...`（包含限流逻辑单测）
  - 手工：短时间内大量请求，确认 429 返回且 `Retry-After` 合理。

### R42 -- Gateway IP 过滤

- **关联审计**：一「IP 过滤放在 API 服务里会绕过非标准路径」
- **关联目标架构**：六.6.13 `CREATE TABLE ip_rules`
- **依赖**：R40 + R20
- **目标**：Gateway 从 Redis 缓存加载 org 的 IP allowlist/blocklist，在请求转发前检查。
- **关键点**：
  - `ip_rules` 表字段：`id`、`org_id`、`type`（allowlist/blocklist）、`cidr CIDR`、`note`、`created_at`。
  - Redis 缓存 key：`arkloop:ip_rules:{org_id}`，TTL 5min。
  - 数据库变更时通过 API 端点触发缓存失效。
  - 默认无规则 = 全部放行。
- **具体改动范围**：
  - 新建 migration `00025_create_ip_rules.sql`
  - 新建 `src/services/gateway/internal/ipfilter/`
  - 新建 `src/services/api/internal/data/ip_rules_repo.go`
  - 新建 `src/services/api/internal/http/v1_ip_rules.go`
- **验收**：
  - `go test -tags integration ./...`
  - 手工：添加 blocklist 规则后，对应 IP 请求被拒绝。

### R43 -- API Key 管理

- **关联审计**：六「API Key 管理不存在」；P2
- **关联目标架构**：六.6.12 `CREATE TABLE api_keys`
- **依赖**：R40（Gateway 做 API Key 验证）
- **目标**：实现 API Key 的创建、查询、吊销、鉴权。
- **关键点**：
  - 创建时生成随机 key，返回明文（仅此一次）；存储 SHA-256 hash。
  - `key_prefix`：前 8 位，用于展示识别。
  - `scopes TEXT[]`：权限范围（初版可先全权限）。
  - Gateway 鉴权路径：`Authorization: Bearer ak-xxx` -> SHA-256 hash -> 查 `api_keys` 表（Redis 缓存 5min）。
  - 区分 JWT token 和 API Key：前缀 `ak-` 表示 API Key。
  - 审计：API Key 的创建、使用、吊销全部写审计日志。
- **具体改动范围**：
  - 新建 migration `00026_create_api_keys.sql`
  - 新建 `src/services/api/internal/data/api_keys_repo.go`
  - 新建 `src/services/api/internal/http/v1_api_keys.go`
  - 修改 Gateway 鉴权中间件：支持 API Key 路径
- **验收**：
  - `go test -tags integration ./...`
  - 手工：创建 API Key -> 用 API Key 访问接口 -> 吊销后被拒绝。

### R44 -- 并发 Run 限制

- **关联审计**：五.5.3「没有并发限制」；P2
- **关联目标架构**：二.2.1「并发 Run 限制：检查 org 当前活跃 run 数」
- **依赖**：R20（Redis）
- **目标**：限制单个 org 同时运行的 run 数量。
- **关键点**：
  - Redis counter：`arkloop:org:active_runs:{org_id}`。创建 run 时 INCR，run 终态时 DECR。
  - 默认限制：每 org 10 个并发 run（通过环境变量或 org settings 配置）。
  - 超限时 API 返回 HTTP 429 + 明确错误码 `runs.concurrent_limit_exceeded`。
  - Worker 终态写入时在同一事务中 DECR Redis counter（需要处理 Worker 崩溃导致的计数器泄漏，定期从数据库重新同步）。
- **具体改动范围**：
  - 修改 `src/services/api/internal/http/v1_runs.go`：创建 run 时检查 + INCR
  - 修改 `src/services/worker/internal/runengine/v1.go`：终态时 DECR
  - 新建 `src/services/api/internal/data/run_limiter.go`：封装 Redis 计数逻辑
- **验证**：
  - `go test -tags integration ./...`
  - 手工：快速创建超过限制数量的 run，确认被拒绝。

---

## 7. Phase 5 -- 组织模型与权限

目标：完善组织模型，建立邀请机制、RBAC 权限体系、teams/projects 层级结构。

### R50 -- org 邀请机制

- **关联审计**：二.2.2「没有邀请机制」；P1
- **关联目标架构**：六.6.2 `CREATE TABLE org_invitations`
- **目标**：创建 `org_invitations` 表，实现邀请链接的创建、接受、过期处理。
- **关键点**：
  - `org_invitations` 字段：`id`、`org_id`、`invited_by_user_id`、`email`、`role`、`token`（唯一随机字符串）、`expires_at`、`accepted_at`、`created_at`。
  - 邀请 token 有效期默认 7 天。
  - 接受邀请时自动创建 `org_memberships` 记录。
  - 审计：邀请创建、接受、过期全部写审计。
- **具体改动范围**：
  - 新建 migration `00027_create_org_invitations.sql`
  - 新建 `src/services/api/internal/data/org_invitations_repo.go`
  - 新建 `src/services/api/internal/http/v1_org_invitations.go`
- **验收**：
  - `go test -tags integration ./...`
  - 手工：创建邀请 -> 用 token 接受 -> 确认成为 org member。

### R51 -- RBAC 权限体系 v1

- **关联审计**：六「RBAC 只有一个 role TEXT 字段」；P2
- **关联目标架构**：六.6.3 `CREATE TABLE rbac_roles`
- **依赖**：无（可独立于 R50）
- **目标**：创建 `rbac_roles` 表 + 内置角色，建立权限点体系。
- **关键点**：
  - 内置角色（`is_system = true`）：`platform_admin`、`org_admin`、`org_member`。
  - 每个角色映射一组权限点字符串数组（`permissions TEXT[]`）。
  - 权限点命名空间：`platform.*`、`org.*`、`data.*`。
  - `org_memberships` 增加 `role_id UUID REFERENCES rbac_roles(id)`。
  - 中间件层：从 JWT claims 或 membership 查询角色 -> 校验权限点。
  - 第一版先做"基于内置角色的权限点校验"，不做可编辑角色（后续迁代）。
- **具体改动范围**：
  - 新建 migration `00028_create_rbac_roles.sql`
  - 新建 `src/services/api/internal/auth/permissions.go`：权限点常量
  - 新建 `src/services/api/internal/auth/roles.go`：内置角色定义
  - 修改 `src/services/api/internal/http/middleware.go`：权限校验中间件
- **验收**：
  - `go test ./internal/auth/...`
  - `go test -tags integration ./...`：非 admin 角色无法访问管理端点。

### R52 -- Teams 与 Projects 层级

- **关联审计**：二.2.6「没有层级结构」；P2
- **关联目标架构**：六.6.4 `CREATE TABLE teams` + `projects`
- **依赖**：R15（threads 已有 `project_id` 字段预留）
- **目标**：创建 `teams`、`team_memberships`、`projects` 表，建立 `org -> team -> project -> thread` 层级。
- **关键点**：
  - `teams`：`id`、`org_id`、`name`、`created_at`。
  - `team_memberships`：`team_id`、`user_id`、`role`，唯一 `(team_id, user_id)`。
  - `projects`：`id`、`org_id`、`team_id`（nullable）、`name`、`description`、`visibility`（private/team/org）、`deleted_at`、`created_at`。
  - `threads.project_id` 加外键 `REFERENCES projects(id)`。
  - API 端点：
    - `POST/GET /v1/teams`
    - `POST/GET /v1/projects`
    - `PATCH /v1/threads/{id}`：设置 `project_id`
  - 权限：project 级 visibility 控制谁能看到 thread。
- **具体改动范围**：
  - 新建 migration `00029_create_teams_projects.sql`
  - 新建 `src/services/api/internal/data/teams_repo.go`
  - 新建 `src/services/api/internal/data/projects_repo.go`
  - 新建 `src/services/api/internal/http/v1_teams.go`
  - 新建 `src/services/api/internal/http/v1_projects.go`
- **验收**：
  - `go test -tags integration ./...`
  - 手工：创建 team -> 创建 project -> 创建 thread 指定 project_id -> 查询 thread 只返回有权限的。

---

## 8. Phase 6 -- 企业级能力

目标：补齐企业级 SaaS 必需的 Webhooks、通知、Feature Flags、Agent 行为配置等能力。

### R60 -- Webhooks

- **关联审计**：六「Webhooks 不存在」；P1
- **关联目标架构**：六.6.15 `CREATE TABLE webhook_endpoints` + `webhook_deliveries`
- **目标**：实现 Webhook 端点注册 + 事件投递 + 重试机制。
- **关键点**：
  - `webhook_endpoints`：`id`、`org_id`、`url`、`signing_secret`、`events TEXT[]`（订阅的事件类型）、`enabled`、`created_at`。
  - `webhook_deliveries`：记录每次投递的状态、响应、重试次数。
  - 投递方式：Worker 写入终态事件后，同步投递一个 `webhook.deliver` job 到队列。独立的 Webhook delivery worker 消费并 POST 到目标 URL。
  - 签名：HMAC-SHA256，`X-Arkloop-Signature` header。
  - 重试策略：指数退避，最多 5 次。
  - 支持的事件类型 v1：`run.completed`、`run.failed`、`run.cancelled`。
- **具体改动范围**：
  - 新建 migration `00030_create_webhooks.sql`
  - 新建 `src/services/api/internal/data/webhooks_repo.go`
  - 新建 `src/services/api/internal/http/v1_webhooks.go`
  - 新建 `src/services/worker/internal/webhook/` 包：delivery worker
- **验收**：
  - `go test -tags integration ./...`
  - 手工：注册 Webhook -> 创建 run -> run 完成后目标 URL 收到 POST。

### R61 -- Agent 行为配置（system prompt + 模型参数 + 工具策略）

- **关联审计**：三.3.5「整个 Agent 行为配置层不存在」
- **关联目标架构**：六.6.11 `agent_configs` + `prompt_templates`
- **依赖**：R52（projects 层级，用于配置继承链）
- **目标**：创建 `prompt_templates`、`agent_configs` 表，实现 `平台默认 -> org 级 -> project 级 -> thread 级` 的配置继承链。
- **关键点**：
  - `prompt_templates`：`id`、`org_id`、`name`、`content`（支持 `{{variable}}` 插值）、`variables JSONB`、`is_default`、`version`、`published_at`。
  - `agent_configs`：`id`、`org_id`、`name`、`system_prompt_template_id`、`system_prompt_override`、`model`、`temperature`、`max_output_tokens`、`top_p`、`context_window_limit`、`tool_policy`、`tool_allowlist`、`tool_denylist`、`content_filter_level`、`safety_rules_json`、`project_id`、`skill_id`、`is_default`。
  - `threads` 增加 `agent_config_id`。
  - Worker 执行 Run 时按继承链解析最终配置（替代硬编码的 `defaultAgentMaxIterations = 10` 和 `threadMessageLimit = 200`）。
  - API 端点：
    - `POST/GET/PATCH /v1/prompt-templates`
    - `POST/GET/PATCH /v1/agent-configs`
- **具体改动范围**：
  - 新建 migration `00031_create_agent_configs.sql`
  - 新建 `src/services/api/internal/data/agent_configs_repo.go`
  - 新建 `src/services/api/internal/data/prompt_templates_repo.go`
  - 新建 `src/services/api/internal/http/v1_agent_configs.go`
  - 修改 `src/services/worker/internal/runengine/v1.go`：从 DB 加载配置替代硬编码
- **验收**：
  - `go test -tags integration ./...`
  - 手工：创建 org 级 agent_config（temperature=0.5）-> 创建 run -> 确认使用了自定义 temperature。

### R62 -- 订阅与用量 Schema

- **关联审计**：六「订阅与计费不存在」
- **关联目标架构**：六.6.14 `CREATE TABLE plans/subscriptions/usage_records`
- **目标**：创建 `plans`、`subscriptions`、`usage_records` 表。Worker 在 Run 完成时自动写入 `usage_records`。
- **关键点**：
  - `usage_records`：每次 LLM 调用的 token 用量 + 成本，关联 `run_id` + `org_id`。
  - `plans` + `subscriptions`：schema 先建，计费逻辑后做。
  - Worker 终态写入时同时写 `usage_records`（从 R30 的 usage 累加数据中取）。
- **具体改动范围**：
  - 新建 migration `00032_create_billing_schema.sql`
  - 新建 `src/services/api/internal/data/usage_repo.go`
  - 修改 Worker 终态逻辑：写 `usage_records`
- **验收**：
  - `go test -tags integration ./...`
  - 手工：run 完成后 `SELECT * FROM usage_records WHERE run_id = $1` 有记录。

### R63 -- Feature Flags

- **关联审计**：六「Feature Flags 不存在」；P2
- **关联目标架构**：六.6.16 `CREATE TABLE feature_flags` + `org_feature_overrides`
- **依赖**：R20（Redis 缓存）
- **目标**：创建 `feature_flags` + `org_feature_overrides` 表，实现全局 + org 级 feature flag。
- **关键点**：
  - `feature_flags`：`key TEXT UNIQUE`、`description`、`default_value BOOLEAN`。
  - `org_feature_overrides`：`(org_id, flag_key) PRIMARY KEY`，覆盖全局默认。
  - Redis 缓存：`arkloop:feat:{org_id}:{flag}` TTL 5min。
  - Go 侧提供 `FeatureFlagService.IsEnabled(ctx, orgID, flagKey) bool`。
- **具体改动范围**：
  - 新建 migration `00033_create_feature_flags.sql`
  - 新建 `src/services/api/internal/data/feature_flags_repo.go`
  - 新建 `src/services/api/internal/http/v1_feature_flags.go`
- **验收**：
  - `go test -tags integration ./...`
  - 手工：创建 flag -> 设置 org override -> 验证生效。

### R64 -- 通知系统

- **关联审计**：六「通知系统不存在」；P2
- **关联目标架构**：六.6.20 `CREATE TABLE notifications`
- **目标**：创建 `notifications` 表，实现站内通知（run 完成通知）。
- **关键点**：
  - `notifications`：`id`、`user_id`、`org_id`、`type`、`title`、`body`、`payload_json`、`read_at`、`created_at`。
  - 创建时机：Worker 写入 run 终态事件后创建通知。
  - API 端点：
    - `GET /v1/notifications`（未读列表）
    - `PATCH /v1/notifications/{id}`（标记已读）
  - 未来可扩展为 email/push 通知。
- **具体改动范围**：
  - 新建 migration `00034_create_notifications.sql`
  - 新建 `src/services/api/internal/data/notifications_repo.go`
  - 新建 `src/services/api/internal/http/v1_notifications.go`
- **验收**：
  - `go test -tags integration ./...`
  - 手工：run 完成后查询通知列表有记录。

---

## 9. run_events 分区策略（跨 Phase 长期任务）

- **关联审计**：二.2.5「无分区无归档」；P0
- **关联目标架构**：六.6.7 `run_events_v2` 按月分区

run_events 的分区改造是一个破坏性变更，不宜在早期做。推荐策略：

1. **Phase 1（R13）**：先消除行锁热点（sequence），降低最紧急的性能风险。
2. **Phase 3 之后**：观察 run_events 表的增长速度。当行数超过 5000 万时启动分区改造。
3. **分区方案**：
   - 新建 `run_events_v2` 按 `ts` 分区（RANGE，每月一个分区）。
   - 数据迁移：后台 worker 逐批搬迁旧数据。
   - 应用层切换：双写一段时间后切读到 v2。
   - 旧表归档到对象存储后 DROP。
4. **成本字段抽列**：在 Phase 6（R62）的 `usage_records` 表中已独立存储 token 用量和费用，不再依赖从 `data_json` 聚合。

---

## 10. 执行顺序与依赖关系

```
Phase 1（地基）
  R10 → R11 → R12 → R13 → R14 → R15 → R16
  （全部是 migration + repository 层，互相无强依赖，可并行执行）

Phase 2（基础设施）
  R20 → R21
  （compose.yaml + 连通性验证）

Phase 3（Worker 重构）
  R30 → R31 → R32 → R33 → R34 → R34.5 → R35 → R36
  （R32 先行，R33/R34/R35 依赖 R32；R36 依赖 R20）

Phase 4（Gateway + 安全）
  R40 → R41 → R42 → R43 → R44
  （R40 先行，R41/R42 可并行，R43 在 R40 之后）

Phase 5（组织与权限）
  R50 → R51 → R52
  （R50/R51 可并行，R52 在 R15 之后）

Phase 6（企业级能力）
  R60 → R61 → R62 → R63 → R64
  （R61 依赖 R52；其余可并行）
```

Phase 1-3 是核心路径，Phase 4-6 可根据产品优先级调整顺序。同一 Phase 内无强依赖的薄片可并行执行。

---

## 11. 每个 Phase 的退出标准

| Phase | 退出标准 |
|---|---|
| Phase 1 | 全部 migration 通过 up/down；`go test -tags integration ./...` 全绿；现有 Web 功能不退化 |
| Phase 2 | `docker compose up -d` 三个服务（postgres/redis/minio）全部 healthy；连通性测试全绿 |
| Phase 3 | Worker 使用 DB 凭证/MCP 配置/Skills 执行 run；SSE 延迟 < 100ms；runs.status 可信 |
| Phase 4 | 所有流量经过 Gateway；限流生效；API Key 可创建/使用/吊销；IP 过滤生效 |
| Phase 5 | org 邀请流程完整；RBAC 权限点校验生效；projects 层级可用 |
| Phase 6 | Webhooks 投递可用；agent_configs 继承链生效；usage_records 自动写入 |

---

## 12. 风险与缓解

| 风险 | 缓解措施 |
|---|---|
| migration 导致数据丢失 | 全部 migration 提供 down 回滚脚本；上线前备份数据库 |
| run_events sequence 全局递增破坏前端 seq 连续性假设 | 确认前端只做 `> after_seq` 比较，不假设连续（已验证当前代码不依赖连续） |
| Redis 引入增加运维复杂度 | 开发环境用 compose 管理；生产环境前期可用 managed Redis |
| Gateway 引入后 SSE 长连接透传 | Gateway 用 `httputil.ReverseProxy` 自带的 Flush 支持；专门测试 SSE 透传场景 |
| MCP 远端化后延迟增加 | stdio fallback 保留；Remote MCP 走 HTTP keep-alive + 工具列表缓存到 Redis |
| 多个 migration 版本管理 | goose 的版本号严格递增；每个 Phase 完成后做一次 `ExpectedVersion` 对齐 |
