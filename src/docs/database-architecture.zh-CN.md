# 数据库架构与数据模型（PostgreSQL 优先）

本文用于把 Arkloop 的数据库边界、核心表与权限/审计/计费的“不变量”先定下来，避免后续在多租户、共享、导出与审计上反复返工。

范围：
- 仅讨论架构与数据模型（不涉及 ORM/代码实现）
- 生产形态以 PostgreSQL 作为唯一目标后端

## 1. 术语：`org` 是不是“公司”？

`org` 在系统里更准确的含义是：**租户边界（tenant boundary）**。

在多数商业场景下，一个 `org` 往往对应“一家公司/一个客户/一个账单主体”，因此产品层面可以称作“公司/组织/租户”，但技术层面应把它当作：
- 数据隔离边界（权限、导出、删除、保留策略）
- 审计边界（日志归属与追责范围）
- 计费与配额边界（预算、倍率、用量报表）

公司下面的部门/小组不建议做成“嵌套 org”（会显著放大权限与计费复杂度），用 `team` 表达即可。

## 2. 顶层结构：`org / team / project` 自上而下

### 2.1 `org`（租户/公司）
- 负责：隔离、策略（保留期/快照/加密）、账单默认归属、管理员域
- 典型角色：`org_admin`、`security_admin`、`billing_admin`

### 2.2 `team`（组织内小组）
- 负责：组织内部的权限分组与可见性
- 典型用途：项目成员管理、同组可见策略、组级预算/倍率覆盖

### 2.3 `project`（协作域/案件）
`project` 非强制存在：允许“个人聊天（无 project）”与“项目聊天（有 project）”并存。

核心原则：
- 成员加入必须显式化（成员列表/邀请链接）
- `org_admin` 可按权限越权访问（并强审计）

## 3. 用户模型：一个人可加入多个 `org`

系统默认支持“一个自然人多个组织（多租户成员）”。同时允许“组织托管账号”（由组织创建/回收）作为策略层能力，而不是换一套数据模型：
- `users` 表达“人”的主体
- `org_memberships` 表达加入关系
- 认证身份（密码/OIDC/SSO）用独立的 `auth_identities` 表承载，避免把认证形态写死在用户表

## 4. `project` 支持跨 `org/team` 加入：建议采用 Owner-Org（主组织）模型

你希望 `project` 可能允许多个 org 或 team 加入协作。为了避免导出/删除/保留期/计费归属冲突，建议采用：

### 4.1 Owner-Org 模型（建议）
- `projects.owner_org_id` 固定为主组织（责任方）
- 其他 org/team/user 以“协作方成员”加入（获得访问权，但不改变数据的默认归属）
- `project` 内的核心数据（threads/messages/runs/events/attachments）默认归属 owner_org

这样能把这些规则固定下来：
- 数据保留/删除：以 owner_org 的策略为准（协作方仅按授权导出/查看）
- 审计：责任明确（谁能看、谁能导出、谁能删、谁能配置策略）
- 计费：默认记到“发起 run 的 org”或“project.owner_org”（二选一，但必须写死并可追溯）

### 4.2 Multi-Org 归属模型（不建议早期做）
允许同一 project 的数据同时归属多个 org，会导致：
- 保留期冲突
- 删除权冲突
- 导出权冲突
- 计费分账与对账复杂度大幅上升

## 5. 可见性与“管理员可看明文”

你已明确：管理员允许查看消息明文；同事默认不可互看，除非通过配置/共享加入。

建议把它拆成三条不可变约束：

1) 默认最小可见：
   - 个人聊天：仅 thread owner 可见
   - 项目聊天：仅 project 成员可见
2) “能看明文”是独立权限位（例如 `messages.read_any`），不要默认下放给所有 admin
3) 任何越权查看/导出/策略变更都必须落 `audit_logs`（谁、何时、范围、来源、trace_id）

## 6. 核心表（建议最小闭环）

以下是 Phase 1~2 的核心表集合（命名可调整），重点是字段语义与约束：

### 6.1 租户与成员
- `orgs`
- `users`
- `org_memberships`
- `teams`
- `team_memberships`

### 6.2 项目与会话
- `projects`（含 `owner_org_id`）
- `project_memberships`（支持 subject 为 user/team/org，含 role）
- `project_invite_links`（分享链接是“授予 membership 的手段”，不是 membership 本身）
- `threads`
- `thread_participants`（个人/项目/共享统一靠 participants/ACL 表达）
- `messages`（存最终归并内容；流式细节放事件表）

### 6.3 运行与事件（唯一真相）
- `runs`
- `run_events`
  - `seq`：run 内单调递增（断线续传/回放依赖它）
  - `type`：稳定事件类型（started/delta/tool.call/denied/failed/...）
  - `data_json`：可变 payload
  - 关键可检索字段抽列：例如 `tool_name`、`error_class`、`cost_usd`、`duration_ms`

## 7. 计费、倍率与快照（不要“分死”，但要“可追溯”）

原则：倍率/单价可以按 org/team/user/project 多层覆盖，但用量落库必须记录“当时生效的快照”，否则未来改价会导致历史账单不可复算。

建议最小组件：
- `rate_cards`：按模型的基础单价（可版本化）
- `pricing_rules`：覆盖规则（org/team/user/project，含优先级）
- `usage_records`：实际用量与成本（含 `effective_multiplier`、`effective_unit_price`、`pricing_version`）

## 8. 导出/导入（需要提前为 ID 与版本留口子）

你要求支持导出导入，这意味着：
- 主键建议使用 UUID/ULID（跨环境迁移更稳定）
- 导出包必须带 `package_version`（对应 schema 版本）
- 导入需要 `id_mapping`（避免冲突并保持引用完整性）
- 导出任务必须可审计（谁导出、范围、产物引用、过期删除）

建议最小组件：
- `export_jobs`
- `import_jobs`

## 9. 附件、网页快照与保留策略

### 9.1 附件（必须保存，但可分层存储与保留期）
- 附件内容建议落对象存储（或本地存储），DB 仅存元数据与引用
- 支持热/冷分层（hot/cold tier）与 `retention_until`

### 9.2 网页快照（可选保存正文，但必须留“可保存的可能性”）
建议把 org 级策略做成可配置项：
- `web_snapshot_policy = off | metadata_only | full_content`
  - `metadata_only`：记录 url、抓取时间、hash、响应摘要
  - `full_content`：保存当时版本正文/渲染结果（用于审计/回放）

对应数据模型建议拆为：
- `web_fetch_logs`：每次抓取的元数据（必有）
- `web_snapshots`：正文内容（按策略可选）

## 10. 当前已确定的决策（记录）

- 数据库：生产形态以 PostgreSQL 为唯一目标后端
- 用户：一个人可加入多个 org
- project：非强制；成员加入显式化；允许分享链接；`org_admin` 可按权限越权
- 管理员查看明文：允许，但“看明文”应是独立权限位，且必须强审计
- 附件：保存，支持保留期与冷存储
- 网页快照：支持按 org 策略开启保存当时版本

## 11. 待拍板（会影响全局）

1) SaaS 形态下是否允许不同客户 org 之间共享 project？
   - 建议默认不允许；仅允许同一企业账户/同一交付实例内的 org 互邀
2) run 的默认计费归属：记到“发起人所在 org”还是“project.owner_org”？
   - 两种都合理，但必须在策略里定死并在用量表记录快照

