# Tools 与 Skills 规范（草案）

本文定义 Arkloop 的工具（Tools）与技能（Skills）边界、存储方式与运行约束，目标是同时满足：可审计、可扩展、低上下文开销、可商业化交付。

## 1. 术语与边界

### 1.1 Tool（工具）
Tool 是“可调用的底层能力”，必须具备：
- 严格的输入/输出 schema（便于校验、审计与回放）
- 明确的安全边界（权限、预算、网络与文件访问）
- 可观测性（trace_id、耗时、成本、错误分类）

Tool 一律在服务端实现与执行（`src/services/*`），前端不持有 System Prompt、提供商密钥与工具执行权限。

### 1.2 Skill（技能）
Skill 是“可版本化的流程/配方”，核心用途是把一段稳定的业务流程沉淀为可复用资产：
- 编排：把多个工具调用组织成可执行流程
- 约束：输出格式、风险边界、审计要求、预算上限
- 交付：可签名、可分发、可灰度、可回滚

Skill 不等价于“长 prompt 贴在上下文里”。运行时应按需加载模板与引用材料，避免挤占主对话上下文。

## 2. 目录与归属（建议）

单仓（monorepo）建议把业务代码统一放在 `src/` 下：
- `src/packages/agent-core/`：Agent Loop 核心抽象（状态机/事件、ToolSpec、SkillSpec、调度策略）。纯逻辑层，不直接触网/读写文件/起进程。
- `src/services/api/`：鉴权、编排、审计落库、任务下发（不直接执行高危工具）。
- `src/services/worker/`：工具执行（web_fetch、shell、code_execute 等），并通过 sandbox 隔离与限权。
- `src/skills/`：内置 skills（随版本发布、可审阅与签名）。租户自定义/覆盖建议落数据库并走发布流程。

说明：`src/skills/` 主要承载“可销售/可交付”的资产；运行时通过 registry 加载，避免把实现逻辑散落在各处。

## 3. Tool 规范（最小集合）

每个 Tool 建议至少具备这些元数据（ToolSpec）：
- `name`/`version`：用于审计与兼容策略
- `description`：给模型看，但保持短且稳定
- `input_schema`/`output_schema`：JSON Schema（或同等能力）
- `required_scopes`：RBAC/ABAC 权限要求（租户/项目/数据域）
- `risk_level`：例如 `low/medium/high`
- `side_effects`：是否有副作用（写文件、发请求、执行命令等）
- `idempotent`、`cacheable`：用于并行与重试策略
- `timeout_ms`、`budget`：时间与成本上限
- `executor_policy`：推荐执行模型档位/是否允许子模型调用

审计建议记录：`tool_name`、`args_hash`、`caller`、`tenant_id`、`request_id/trace_id`、`start/end`、`result_hash`、`error_class`、`cost`。

## 4. Skill 包规范（最小集合）

每个 skill 建议一个独立目录，至少包含：
- `skill.yaml`：元数据与执行约束
- `prompt.md`：提示词模板（可分片，但避免在主对话中整段注入）

`skill.yaml` 建议字段（可按阶段逐步落地）：
- `id`、`version`、`title`、`description`、`tags`
- `input_schema`、`output_schema`
- `tool_allowlist`：该 skill 允许调用的工具集合
- `data_scopes`：允许访问的数据域/项目域
- `budgets`：tokens/时间/并发/每日额度等
- `executor`：`main` / `subagent` / `hybrid`
- `review_policy`：是否需要 review、触发条件（高风险输出/高成本工具/外部发送等）
- `signature`：可选（用于付费交付与防篡改）

## 5. run_skill 的上下文策略

run_skill 的关键不是“把对话全塞进去”，而是把上下文拆为可控输入：

### 5.1 主模型执行（main）
- 沿用“会话摘要 + 最近 N 轮 + 明确目标 + context_refs”
- `context_refs` 指向服务端可取回的材料（网页快照、附件、检索片段），避免原始大文本进入主上下文

### 5.2 子模型执行（subagent）
- 主模型先产出 `task_brief`：目标、边界、输出 schema、允许工具、必须引用的 `context_refs`
- 子模型只拿 `task_brief + 引用材料` 执行，适合 `web_fetch -> 抽取/总结/对比` 等高 token 场景

建议让 run_skill 返回结构化结果 + 引用来源（ref_id）+ trace_id，便于审计与复算。

## 6. Tools 的模型策略（大模型/小模型都能 tool-calling）

工具调用能力应由“调度策略”控制，而不是写死在某个模型上：
- 低风险、确定性强、可并行的工具（检索/解析/去噪）优先走子模型
- 高风险、有副作用的工具（shell、代码执行、外部发送）强制走更强模型 + review（或直接要求人工确认）
- 工具本身通过 `executor_policy` 声明“允许谁来调用”，调度器根据风险/预算/租户策略路由

## 7. 商业化与后台可编辑（建议路线）

把 skills 做成付费能力时，后台“可编辑”建议分阶段：
- Phase 1：只读内置 skills + 租户开关 + 版本号 + 全量审计
- Phase 2：租户可编辑（仅 prompt/参数层），但必须绑定 `tool_allowlist`、预算与发布流程（`draft -> review -> published`），支持回滚与 diff
- Phase 3：skills 包签名、分发渠道、行业包（律所/财务/内审）与定制交付（按 `skill_id@version` 交付）

核心原则：客户购买的是“可控的编排能力”，而不是“无限权限的 prompt 编辑器”。

相关测试建议见：`src/docs/testing-and-pytest.zh-CN.md`。
