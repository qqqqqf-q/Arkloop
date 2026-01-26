# Arkloop（AgentLoop）— 开发与使用说明

本目录（`src/docs`）用于存放工程文档与使用说明。
商业/法律相关文档请放在仓库根目录的 `docs/`。

## 0. 本地启动（API）

由于工程代码全部位于 `src/` 下，从仓库根目录启动时需要把 `src` 加到模块搜索路径：

- 推荐（跨平台）：`python -m uvicorn services.api.main:configure_app --factory --app-dir src --host 127.0.0.1 --port 8000`
- Windows 备选（PowerShell）：`$env:PYTHONPATH="src"; python -m uvicorn services.api.main:configure_app --factory --host 127.0.0.1 --port 8000`
- Linux/macOS 备选（bash/zsh）：`PYTHONPATH=src python -m uvicorn services.api.main:configure_app --factory --host 127.0.0.1 --port 8000`

说明：当前结构化 JSON 只覆盖 Arkloop 应用日志；uvicorn 自身的启动/访问日志仍由 uvicorn 默认配置控制。

## 0.1 本地启动（PostgreSQL / docker compose）

一次性准备（生成本机私有配置，不要提交到仓库）：
- Windows（PowerShell）：`Copy-Item .env.example .env`
- Linux/macOS：`cp .env.example .env`

编辑 `.env`，至少设置 `POSTGRES_PASSWORD`（并保持 `DATABASE_URL` 与其一致）。
Windows：先启动 Docker Desktop（Linux Engine）再执行下述命令。

启动 / 停止：
- 启动：`docker compose up -d`
- 停止：`docker compose down`
- 清理数据（会删除本地 volume）：`docker compose down -v`

连通性检查（不依赖本机安装 psql）：
- `docker compose exec -T postgres psql -U arkloop -d arkloop -c "select 1;"`

应用侧 `ARKLOOP_DATABASE_URL`（优先）：
- 把 `.env` 里的 `ARKLOOP_DATABASE_URL`（或兼容的 `DATABASE_URL`）配到你运行 API 的环境变量里（IDE Run Config 或终端）
  - Windows（PowerShell）：`$env:ARKLOOP_DATABASE_URL="postgresql+asyncpg://..."; python -m uvicorn services.api.main:configure_app --factory --app-dir src`
  - Linux/macOS：`ARKLOOP_DATABASE_URL="postgresql+asyncpg://..." python -m uvicorn services.api.main:configure_app --factory --app-dir src`

## 1. 项目概述

Arkloop 是一个面向企业场景的「Agent Loop」系统，包含：
- 面向用户的聊天式产品（类 Claude.ai 交互）
- 面向组织的后台管理（安全、审计、计费与配额）
- 后端智能体运行时（记忆、工具、Review、模型路由）

目标客户以初创企业与律所为主（默认非强合规行业），但仍把安全与审计作为一等公民。

## 2. 产品交付形态（商业模式）

Arkloop 不是“纯 SaaS”，规划支持多形态交付：

### 2.1 SaaS 订阅（Web）
- 托管式 Web 应用，按个人/团队订阅。
- 适合快速上线、集中更新。

### 2.2 线下机器（数据本地 + 云端模型）
- 客户运行本地机器/一体机，业务数据留在本地。
- LLM 调用走云端模型，支持两种方式：
  - **BYOK**：客户自备 OpenAI/Anthropic 等模型厂商 Key，由客户与厂商建立付费关系。
  - **托管网关（Managed Gateway）**：客户使用 Arkloop 发放的 Token，由 Arkloop 网关路由到模型厂商；厂商 Key 不下发到客户侧。
- 所有交付都应绑定协议（按形态选择 EULA/NDA/DPA 等）。

### 2.3 线下 GPU（完全离线）
- 客户本地 GPU 推理（vLLM 等运行时）。
- 在离线模式下，Prompt 与输入输出无法从技术上“绝对保密”，保护主要依赖：定价策略 + 许可条款 + 长期维护与持续升级。

## 3. 核心概念：Agent Loop

### 3.1 记忆与系统约束
- **System Prompt**：规范/格式/策略约束，输出风格与安全规则
- **长期记忆**：稳定的用户/组织事实（姓名、职位、公司等）
- **短期记忆**：最近对话与正在推进的任务
- **项目记忆**：案件/项目维度的知识；与通用记忆分离，但不意味着完全隔离

### 3.2 工具（规划/支持）
- Web 搜索（web search + web fetch）
- 记忆检索（memory search）
- 代码执行（如 Excel/Word 操作、数学工具）
- Shell（受控/沙箱执行）
- Subagent：处理超长材料（法律文献、web fetch 等，可用其他模型）
- PDF watcher：文档阅读/入库辅助

### 3.3 运行模式
- **Plan Mode**：输出可执行方案，而不是空问答
- **Review Agent**：更强模型 + 最短上下文，对单次输出做置信度校验
  - 优化方向：把长对话拆成短单元做 Review

### 3.4 Provider Cache（成本/延迟）
高频计算优先使用模型提供商的缓存能力，或在应用层做缓存（必须有清晰的隐私边界与严谨的缓存键设计）。

## 4. 前端信息架构

### 4.1 用户侧（Web）
- 通用聊天
- 法律咨询
- 财务审计
- 项目/案件（会话分组）
- 最近聊天

体验方向：类 Claude.ai 官网布局，精致、简洁，并带少量自然动画。

### 4.2 后台管理（企业 Admin/Console）
后台不是“一个小设置页”，至少应覆盖：
- 组织与账号：租户/组织、用户、角色、权限、登录策略
- 审计与安全：审计日志、访问日志、导出日志、保留策略
- 数据治理：加密策略、密钥管理、数据导出、删除
- 订阅与配额：订阅管理、额度/预算、用量报表
- AI 运维：提供商配置、模型路由、限流、告警可见性
- 数据检查：数据一致性检查、流水线健康、索引状态、工具执行历史

## 5. 后端技术方向

### 5.1 技术栈
- 后端：Python（推荐 FastAPI）
- 数据库：PostgreSQL（本地部署 + SaaS；开发环境同生产）
- Redis：后期引入（缓存、限流、队列）
- 前端：React + Tailwind（用户侧 + 后台）

### 5.2 前后端边界
前端不得包含 System Prompt、模型厂商 Key、Review 规则。
Prompt 拼装与模型调用必须在服务端（或受控网关）完成。

## 6. API Call 管线（OpenAI + Anthropic）

即便初期实现很简单，也建议从架构上做“提供商无关”的管线：
- 统一的内部请求/响应结构（内部 schema）
- Provider 适配器：`OpenAIAdapter`、`AnthropicAdapter`
- 统一的流式接口（API 层 SSE/WebSocket；底层对接各 Provider streaming）
- 统一的错误分类与重试策略

原则：先做一次统一封装，后续全局都依赖封装，避免 Provider 特有调用散落全项目。

## 7. 模型价格计算（Tokens + 倍率）

价格与成本计算属于后台/后端；一线员工在前台只看到：
`模型` + `描述` + `倍率`。

### 7.1 成本模型
为每个模型存基础单价（示例单位：USD / 1M tokens）：
- `input_price_per_1m`
- `output_price_per_1m`

计算方式：
- `base_cost_usd = input_tokens/1_000_000 * input_price_per_1m + output_tokens/1_000_000 * output_price_per_1m`
- `final_cost_usd = base_cost_usd * multiplier`

可选（仅后台可见）：
- 汇率换算
- 计费取整规则
- provider cache 折扣
- 运营成本系数（overhead）

### 7.2 倍率的用途
倍率是业务控制阀：
- 不同客户等级
- 不同产品线（法律 vs 财务）
- 不同交付形态（云端 vs 线下）
- 高端模型的风险溢价

## 8. 仓库目录（建议形态）

建议采用 monorepo 结构，并约定“工程代码全部位于 `src/` 下”。仓库根目录只保留少量工程配置与 `docs/`（商业/法律文档）：

- `src/apps/web/`：用户侧 Web
- `src/apps/console/`：后台管理
- `src/services/api/`：后端 API（FastAPI）
- `src/services/worker/`：异步任务（索引、入库、长文档处理）
- `src/packages/api-client/`：从 OpenAPI 生成的 TS client/types
- `src/packages/ui/`：共享 UI 组件
- `src/packages/shared/`：共享工具与跨端类型
- `docs/`：商业/法律文档（EULA/NDA/DPA）
- `src/docs/`：工程文档（本文）

## 9. 路线图（工程计划）

### Phase 0 — 文档与边界先行
- 明确交付形态与数据边界
- 定义后台域模型、审计要求、定价/倍率模型
- 定义 OpenAI/Anthropic provider 管线接口
- 日志与可观测性约定：`src/docs/logging-and-observability.zh-CN.md`

### Phase 1 — 最小可交付纵切（Vertical Slice）
- 鉴权（tenant/user/role）
- 会话 + 审计日志
- 先接入一个 provider + 流式输出
- 后台：模型目录（模型/描述/倍率）

### Phase 2 — Agent Loop 核心落地
- 记忆层（长/短/项目）+ 检索
- 工具执行协议 + 沙箱
- Review agent（按风险触发）

### Phase 3 — 线下交付
- 一体机打包与离线友好存储
- BYOK 与托管网关切换
- 授权/激活流程 + 更新通道

## 10. 说明与边界

- 本文不构成法律意见；协议与授权策略请在 `docs/` 维护并经专业审阅。
- 对完全离线 GPU 部署，不承诺“Prompt 绝对保密”；把价值放在产品体验、工程能力、服务与持续升级。

## 11. 进一步阅读（工程文档）

- 后端 API 与 SSE（Phase 1 规范草案）：`src/docs/api-and-sse.zh-CN.md`
- 项目开发路线（从 0 到可用）：`src/docs/development-roadmap.zh-CN.md`
- Tools 与 Skills 规范：`src/docs/skills-and-tools.zh-CN.md`
- pytest 与测试策略：`src/docs/testing-and-pytest.zh-CN.md`
- 日志与可观测性：`src/docs/logging-and-observability.zh-CN.md`
- 数据库架构与数据模型：`src/docs/database-architecture.zh-CN.md`
