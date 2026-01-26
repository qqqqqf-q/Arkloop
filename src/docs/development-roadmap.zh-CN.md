# 项目开发路线（从 0 到可用）

目标：把 Arkloop 这种“工具执行 + 审计 + 多租户 + 流式”系统拆成可落地的阶段，每个阶段都能独立验收，不靠“一句 prompt 把项目生成完”。

## 1. 先确定不可变的工程约束

这些约束越早定死，返工越少：
- 事件模型（Run Events）是唯一真相：推送、审计、回放全部复用。
- 分层：`agent-core`（纯逻辑）/ `api`（鉴权与编排）/ `worker`（工具执行）三层边界稳定。
- 外部依赖（模型/公网）默认不可用：测试必须支持 stub 或录制/重放。
- 数据库：PostgreSQL（生产唯一目标；开发环境同生产，避免双栈差异）。
- 安全默认拒绝：高风险工具默认关，靠 allowlist + policy + 审计逐步放开。

## 2. 研发方法：纵切优先，薄片迭代

不要先把“所有模块都搭出来再连线”，而是做一条条可验收纵切：
- 每条纵切都必须能通过命令行调用（CLI/脚本），并能在测试里被回放验证。
- 每条纵切都必须产出稳定的 schema（API 响应、事件结构、错误码），后续只增不破。

推荐的验收口径：
- 功能：是否完成一次 run 并产生事件流
- 安全：是否记录了审计字段，是否有权限/预算拦截
- 可测：pytest 是否能稳定复现（不连公网、不依赖真实模型）

## 3. 阶段规划（建议）

### Phase 0：仓库与工程骨架（1 周内）

验收：
- 目录结构明确（文档已在 `src/docs` 约定）
- 最小运行方式与最小测试入口确定（例如 `make test`/`python -m pytest`）
- 日志与 trace_id 贯穿（即使先是 stdout，也要有结构化字段）
  - logger 默认输出 JSON（便于 Loki/ELK 采集），并包含 `trace_id`
  - API 错误模型与响应 Header 都返回 `trace_id`（便于前端/CLI 报错关联）
  - 工具参数/结果默认只记录 hash（避免敏感明文进日志）
  - 约定见：`src/docs/logging-and-observability.zh-CN.md`

### Phase 1：最小可跑 Agent Loop（2–4 周）

目标：能从 CLI 发起对话，拿到流式输出，且全过程可审计可回放。

最小纵切：
- 数据模型：`threads/messages/runs/run_events`
- API：`POST /threads/{id}/messages`、`POST /threads/{id}/runs`、`GET /runs/{id}/events`（SSE）
- Provider：先 stub/录制回放，保证流式 `message.delta` 稳定
- 策略：最小的 allowlist/预算/权限拦截（先做成“能拦截且有审计”）

验收：
- 同一 run 在断线后可从 `after_seq` 继续拉取事件
- 事件序列能解释整个执行链路（至少：started、delta、completed/failed）

### Phase 2：Tools 与 Worker（4–8 周）

目标：把工具调用这条链路做成“可控、可测试、可审计”的系统能力。

建议顺序：
1) ToolSpec（输入/输出 schema、risk_level、required_scopes、side_effects）
2) tool broker（API 编排 -> worker 执行 -> 结果回传）
3) 先上低风险工具（只读/纯计算），再上高风险工具（shell/code_execute/web_fetch）
4) 对高风险工具引入 review（人工/更强模型审核）与强制审计

验收：
- 任意工具调用都能在审计里看到：谁调用、参数摘要、结果摘要、成本、错误分类
- policy 能拦截越权/危险参数，并形成稳定错误码

### Phase 3：组织后台（并行推进）

目标：让企业真实可用（权限、审计、配额、计费、导出）。

核心模块：
- RBAC/ABAC：组织、角色、权限、数据域
- 审计与导出：查询、过滤、保留策略、导出任务
- 订阅与配额：用量聚合、倍率、预算上限、告警

### Phase 4：前端（在 Phase 1 后就可启动）

原则：后端先把“事件流与错误码”稳定下来，前端只做消费与展示。

建议先做两个页面：
- Chat：threads/messages/runs + SSE 渲染
- Console：审计列表 + run 详情（从事件还原过程）

## 4. 工作分解建议（按职责切）

把“大项目”拆成稳定的团队协作界面：
- `agent-core`：状态机、事件生成、tool/skill 抽象（纯逻辑，可单测）
- `api`：鉴权、权限校验、run 编排、SSE 推送
- `worker`：工具执行与 sandbox（高风险边界集中在这里）
- `data`：仓储接口与迁移（PostgreSQL）
- `observability`：日志、trace、审计查询、成本统计

## 5. 文档与测试如何同步

建议把“会变”的实现细节和“不变”的协议分开：
- `src/docs/api-and-sse.zh-CN.md`：协议与端点（偏稳定）
- `src/docs/skills-and-tools.zh-CN.md`：Tool/Skill 规范（偏稳定）
- `src/docs/testing-and-pytest.zh-CN.md`：测试原则（偏稳定）
- 具体实现细节写在代码内的 README 或模块 docstring（可变）
