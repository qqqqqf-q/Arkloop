---
---

# Console Lite Design Specification

Console Lite 是 Arkloop 面向自部署用户和小型团队的精简管理面板。复用现有 Console 的 UI 组件库和 `@arkloop/shared` 包，对接现有后端 API，无需后端改动。

## 设计原则

1. **扁平导航** -- 7 个页面，一级菜单，无分组折叠
2. **同层设计** -- 不做嵌套配置（如凭证内嵌路由），一个实体一个表单
3. **Scope 固定** -- 所有写入操作 scope 固定为 `platform`，前端不暴露 scope 选择器
4. **无 Advanced JSON** -- 不提供任何 JSON 编辑器入口
5. **组件复用** -- DataTable、FormField、Modal、PageHeader、Toast 等直接从现有 Console 复用
6. **中英双语** -- 所有文案通过 `locales/zh-CN.ts` 和 `locales/en.ts` 管理，复用现有 Console 的 `LocaleContext` 机制
7. **信息克制** -- 表单默认只展示必填项，高级参数折叠隐藏
8. **无积分概念** -- 自部署场景下积分系统无意义，部署脚本设置 `credit.deduction_policy = {"tiers":[{"multiplier":0}]}`，Lite 界面不暴露任何积分相关配置

## 项目结构

```
src/apps/console-lite/
  index.html
  vite.config.ts
  tsconfig.json
  package.json                    # 依赖 @arkloop/shared
  src/
    main.tsx
    App.tsx                       # 路由定义
    api/                          # API client（复用 @arkloop/shared 的 apiFetch）
    components/                   # 复用 + Lite 专有组件
    layouts/
      LiteLayout.tsx              # 侧边栏 + Outlet
    pages/
      DashboardPage.tsx
      AgentsPage.tsx
      ModelsPage.tsx
      ToolsPage.tsx
      MemoryPage.tsx
      RunsPage.tsx
      SettingsPage.tsx
    locales/
      zh-CN.ts
      en.ts
```

## 导航结构

```
Arkloop [Lite]

  Dashboard        LayoutDashboard
  Agents           Sparkles
  Models           KeyRound
  Tools            Wrench
  Memory           BrainCircuit
  Runs             Play
  Settings         Settings
```

侧边栏宽度 220px，不可折叠。底部显示用户名 + 退出按钮。无分组标题，无 ChevronDown。

i18n 导航标签:

| Key | zh-CN | en |
|---|---|---|
| nav.dashboard | 仪表盘 | Dashboard |
| nav.agents | 智能体 | Agents |
| nav.models | 模型管理 | Models |
| nav.tools | 工具 | Tools |
| nav.memory | 记忆 | Memory |
| nav.runs | 运行记录 | Runs |
| nav.settings | 设置 | Settings |

---

## Page 1: Dashboard

**用途**: 平台概览 + Token 用量

**API**:
- `GET /v1/admin/dashboard` -> `DashboardData`
- `GET /v1/admin/usage/daily?start={30天前}&end={今天}` -> `DailyUsage[]`

**布局**:

```
┌─────────────────────────────────────────────────┐
│  Dashboard                          [Refresh]   │
├────────┬────────┬────────┬─────────────────────┤
│ Runs   │ Runs   │ Input  │ Output              │
│ Total  │ Today  │ Tokens │ Tokens              │
├────────┴────────┴────────┴─────────────────────┤
│                                                 │
│  Token Usage (30 Days)                          │
│  ┌─────────────────────────────────────────┐   │
│  │  Simple bar chart / area chart          │   │
│  │  X: date, Y: tokens                    │   │
│  │  Two series: input / output             │   │
│  └─────────────────────────────────────────┘   │
│                                                 │
└─────────────────────────────────────────────────┘
```

**指标卡片**: 4 列网格，每个卡片显示标签 + 数值。数值使用 `toLocaleString()` 格式化。

**图表**: 使用轻量图表库（推荐 recharts 或 Chart.js）。30 天每日 Token 用量。仅此一个图表，不做更多。

---

## Page 2: Agents

**用途**: Agent 管理。Arkloop 的 Agent 直接映射到一个 Persona；Persona 自身携带 prompt、工具约束、运行预算以及 model selector。

### 数据映射

前端的一个 "Agent" = 后端的一个 Persona。

| Lite 字段 | 后端 Persona 字段 |
|---|---|
| Name | `display_name` |
| System Prompt | `prompt_md` |
| Model | `model`（model selector，格式 `provider_name^model_name`） |
| Tools | `tool_allowlist` / `tool_denylist` |
| Is Active | `is_active` |
| (Advanced) Temperature | `budgets.temperature` |
| (Advanced) Max Output Tokens | `budgets.max_output_tokens` |
| (Advanced) Reasoning Mode | `reasoning_mode` |
| (Advanced) Prompt Cache Control | `prompt_cache_control` |

### 创建流程

1. 前端收集表单数据
2. `POST /v1/lite/agents` 创建简化 Agent 视图
3. 服务端只落一条 Persona，并把模型、推理模式、prompt cache 等字段直接写入 Persona

### 列表视图

**API**:
- `GET /v1/lite/agents` -> Agent 列表
- 服务端返回已展开的 Persona 视图，无需前端二次关联

```
┌─────────────────────────────────────────────────┐
│  Agents                          [+ New Agent]  │
├─────────────────────────────────────────────────┤
│                                                 │
│  ┌─ Card ────────────────────────────────────┐  │
│  │  Agent Name                       [Active]│  │
│  │  Model: gpt-4o                             │  │
│  │                          [Edit] [Delete]   │  │
│  └────────────────────────────────────────────┘  │
│                                                 │
│  ┌─ Card ────────────────────────────────────┐  │
│  │  ...                                       │  │
│  └────────────────────────────────────────────┘  │
│                                                 │
└─────────────────────────────────────────────────┘
```

使用卡片布局，非表格。每张卡片仅显示：Name、Model、状态标签（Active）、操作按钮。不在卡片上显示 Temperature、Tools 等细节。

### 创建/编辑表单（Modal）

```
┌─ New Agent ──────────────────────────────────────┐
│                                                   │
│  Name *                                           │
│  [________________________]                       │
│                                                   │
│  Model *                                          │
│  [dropdown: from Models page entries       v]     │
│                                                   │
│  System Prompt *                                  │
│  ┌──────────────────────────────────────────┐    │
│  │                                          │    │
│  │  (textarea, min 4 rows)                  │    │
│  │                                          │    │
│  └──────────────────────────────────────────┘    │
│                                                   │
│  Tools                                            │
│  [x] web_search                                   │
│  [x] web_fetch                                    │
│  [ ] code_execute                                 │
│                                                   │
│  [ ] Set as default agent                         │
│                                                   │
│  ▸ Advanced                                       │
│  ┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄  │
│  (collapsed by default, click to expand)          │
│  │  Temperature            [===O========] 0.7    │
│  │  Max Output Tokens      [           ]         │
│  │  Reasoning Mode         [disabled      v]     │
│  ┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄  │
│                                                   │
│                        [Cancel]  [Save]           │
└───────────────────────────────────────────────────┘
```

**字段说明**:

| 字段 | 类型 | 必填 | 位置 | 说明 |
|---|---|---|---|---|
| Name | text | Y | 主区域 | Agent 名称，唯一 |
| Model | select | Y | 主区域 | 下拉来源于 Models 页面的 LLM 条目，保存为 model selector |
| System Prompt | textarea | Y | 主区域 | 直接文本，不支持模板变量 |
| Tools | checkbox group | N | 主区域 | 列出所有已激活的 Tool Provider |
| Temperature | slider | N | Advanced | 范围 0-2，步长 0.1，默认 0.7 |
| Max Output Tokens | number | N | Advanced | 留空使用模型默认值 |
| Reasoning Mode | select | N | Advanced | disabled / enabled，默认 disabled |

Advanced 区域默认折叠，点击 "Advanced" 标题展开。未修改时使用默认值。

**不暴露的字段**（使用默认值）:
- `scope`: 固定 "platform"
- `persona_key`: 从 Name 自动生成 slug
- `version`: 固定 "1.0"
- `top_p`: 使用默认值
- `content_filter_level`: 使用默认值
- `prompt_cache_control`: 使用默认值
- `budgets`: 使用默认值
- `executor_type`: 使用默认值
- `executor_config`: 使用默认值
- `system_prompt_template_id`: 不使用（用 override 替代）

---

## Page 3: Models

**用途**: 统一管理 LLM Provider、Provider 下的模型列表，以及 ASR 模型配置。

### 顶部 Tab

```
[LLM]  [ASR]
```

两个 Tab，默认显示 LLM。

### LLM Tab

**核心简化**: 原有的 Credential + Routes 多层嵌套扁平化为单层 Model 条目。

#### 客户端类型映射

| 前端显示 | 后端 `provider` | 后端 `openai_api_mode` |
|---|---|---|
| OpenAI Response | `openai` | `responses` |
| OpenAI Chat Completion | `openai` | `chat_completions` |
| Anthropic Message | `anthropic` | (null) |

> Google Generative AI 暂不支持，待后端 Gateway provider 实现后再加入。

#### 列表视图

```
┌─────────────────────────────────────────────────┐
│  Models                          [+ New Model]  │
│  [LLM]  [ASR]                                   │
├─────────────────────────────────────────────────┤
│                                                 │
│  Name         Client Type              Actions  │
│  ─────────────────────────────────────────────  │
│  gpt-4o       OpenAI Response   [Default] [Edit]│
│  claude-4     Anthropic Message          [Edit] │
│  deepseek     OpenAI Completion          [Edit] │
│                                                 │
└─────────────────────────────────────────────────┘
```

使用 DataTable。列: Name / Client Type / Default 标签 / Actions。

#### 创建表单（Modal）

```
┌─ New LLM Model ──────────────────────────────────┐
│                                                   │
│  Client Type *                                    │
│  [dropdown:                                v]     │
│    OpenAI Response                                │
│    OpenAI Chat Completion                         │
│    Anthropic Message                              │
│                                                   │
│  Name *                                           │
│  [________________________]                       │
│                                                   │
│  API Key *                                        │
│  [••••••••••••••••••••••••]                       │
│                                                   │
│  Base URL                                         │
│  [________________________]                       │
│                                                   │
│  Model Name *                                     │
│  [________________________]                       │
│                                                   │
│  [ ] Set as default                               │
│                                                   │
│                        [Cancel]  [Save]           │
└───────────────────────────────────────────────────┘
```

**字段说明**:

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| Client Type | select | Y | 决定 provider + openai_api_mode |
| Name | text | Y | Provider 名称，唯一，也是 model selector 的前缀 |
| API Key | password | Y | 创建时必填，编辑时可选（不修改则保留原值） |
| Base URL | text | N | 自定义端点，兼容 OpenAI 格式的第三方服务填此项 |
| Model Name | text | Y | 实际调用的模型 ID |
| Is Default | checkbox | N | |

**后端调用流程**:

```
1. POST /v1/llm-providers
   body: {
     name,
     provider,
     api_key,
     base_url,
     openai_api_mode
   }
2. POST /v1/llm-providers/{providerId}/models
   body: {
     model,
     priority: 1,
     is_default: true,
     when: {},
     multiplier: 1.0
   }
```

**每个 Provider 至少配置一条模型记录**。用户只选择最终的 model selector，不需要理解内部路由概念。

**编辑时**: `PATCH /v1/llm-providers/{id}` 更新 Provider 字段，`PATCH /v1/llm-providers/{id}/models/{modelId}` 更新 Model Name。

### ASR Tab

#### 列表视图

DataTable。列: Name / Provider / Model / Default 标签 / Actions。

#### 创建表单（Modal）

```
┌─ New ASR Model ──────────────────────────────────┐
│                                                   │
│  Provider                                         │
│  [dropdown: Groq / OpenAI              v]         │
│                                                   │
│  Model                                            │
│  [dropdown: (根据 Provider 动态切换)    v]         │
│    Groq:    whisper-large-v3-turbo                │
│             whisper-large-v3                       │
│             distil-whisper-large-v3-en             │
│    OpenAI:  whisper-1                             │
│                                                   │
│  Name                                             │
│  [________________________]                       │
│                                                   │
│  API Key                                          │
│  [••••••••••••••••••••••••]                       │
│                                                   │
│  Base URL (optional)                              │
│  [________________________]                       │
│                                                   │
│  [ ] Set as default                               │
│                                                   │
│                        [Cancel]  [Save]           │
└───────────────────────────────────────────────────┘
```

**API**: 直接调用 `/v1/asr-credentials` CRUD，scope 固定 `platform`。

---

## Page 4: Tools

**用途**: 管理搜索和网页抓取工具的 Provider 配置。

**API**: `/v1/tool-providers?scope=platform`

### 布局

```
┌─────────────────────────────────────────────────┐
│  Tools                                          │
├─────────────────────────────────────────────────┤
│                                                 │
│  Web Search                                     │
│  ┌────────────────────────────────────────────┐ │
│  │  Tavily                                    │ │
│  │  Status: Active                [Configure] │ │
│  ├────────────────────────────────────────────┤ │
│  │  SearXNG                                   │ │
│  │  Status: Inactive              [Configure] │ │
│  └────────────────────────────────────────────┘ │
│                                                 │
│  Web Fetch                                      │
│  ┌────────────────────────────────────────────┐ │
│  │  Jina                                      │ │
│  │  Status: Active                [Configure] │ │
│  ├────────────────────────────────────────────┤ │
│  │  Firecrawl                                 │ │
│  │  Status: Inactive              [Configure] │ │
│  ├────────────────────────────────────────────┤ │
│  │  Basic (no key required)                   │ │
│  │  Status: Active                            │ │
│  └────────────────────────────────────────────┘ │
│                                                 │
└─────────────────────────────────────────────────┘
```

按 Group 分节（Web Search / Web Fetch），每个 Provider 一行。

### Configure Modal

```
┌─ Configure Tavily ───────────────────────────────┐
│                                                   │
│  API Key                                          │
│  [••••••••••••••••••••••••]                       │
│                                                   │
│  Base URL (optional)                              │
│  [________________________]                       │
│                                                   │
│  [x] Active                                       │
│                                                   │
│             [Clear Credential] [Cancel]  [Save]   │
└───────────────────────────────────────────────────┘
```

字段按 `requires_api_key` / `requires_base_url` 条件显示。

**API 调用**:
- 激活: `PUT /v1/tool-providers/{group}/{provider}/activate`
- 停用: `PUT /v1/tool-providers/{group}/{provider}/deactivate`
- 设置凭证: `PUT /v1/tool-providers/{group}/{provider}/credential`
- 清除凭证: `DELETE /v1/tool-providers/{group}/{provider}/credential`

---

## Page 5: Memory

**用途**: OpenViking 记忆服务配置。

**API**: 读写 Platform Settings（`GET/PUT /v1/admin/settings`，key prefix `memory.*`）

### 布局

```
┌─────────────────────────────────────────────────┐
│  Memory                               [Save]    │
├─────────────────────────────────────────────────┤
│                                                 │
│  Provider                                       │
│  OpenViking (badge, 不可更改)                     │
│                                                 │
│  Base URL                                       │
│  [http://localhost:19010_______________]          │
│                                                 │
│  Root API Key                                   │
│  [••••••••••••••••••••••••]                     │
│                                                 │
└─────────────────────────────────────────────────┘
```

2 个字段，一个 Save 按钮。页面级保存，非 Modal。

Cost Per Commit 不展示（Lite 下积分系统已禁用，该字段无意义）。

---

## Page 6: Runs

**用途**: 查看对话运行记录。

**API**:
- `GET /v1/runs` -> Run 列表（分页）
- `GET /v1/runs/{id}` -> Run 详情 + Turns

### 列表视图

复用现有 Console 的 RunsPage + DataTable，精简列:

| 列 | 说明 |
|---|---|
| ID | 截断显示 |
| Agent | Persona display_name |
| Model | 使用的模型 |
| Status | completed / failed / running |
| Tokens | input + output 合计 |
| Time | 相对时间 |

### 详情面板

点击行展开侧边面板（复用现有 `RunDetailPanel` + `TurnView` 组件）。显示完整对话 Turns。

---

## Page 7: Settings

**用途**: 低频系统配置项归集。

### 布局

使用 Section 分块，纵向排列，页面级 Save。

```
┌─────────────────────────────────────────────────┐
│  Settings                             [Save]    │
├─────────────────────────────────────────────────┤
│                                                 │
│  ── General ──────────────────────────────────  │
│                                                 │
│  Title Summarizer Agent                         │
│  [dropdown: model selector list        v]       │
│                                                 │
│  ── Email ────────────────────────────────────  │
│                                                 │
│  Sender Address                                 │
│  [noreply@example.com__________________]        │
│                                                 │
│  ── Sandbox ──────────────────────────────────  │
│                                                 │
│  Provider        [dropdown: firecracker/docker] │
│  Base URL        [http://localhost:19002_______]  │
│  Docker Image    [________________________]     │
│  (Docker Image 仅 docker provider 时显示)        │
│                                                 │
└─────────────────────────────────────────────────┘
```

**API**: 统一读写 Platform Settings。

**Settings 各 Section 对应的 Platform Setting Keys**:

| Section | Key | 类型 |
|---|---|---|
| General | `title_summarizer.model` | string（model selector） |
| Email | `email.from_address` | string |
| Sandbox | `sandbox.provider` | string |
| Sandbox | `sandbox.base_url` | string |
| Sandbox | `sandbox.docker_image` | string |

Sandbox Section 中不暴露 warm pool、session timeout 等运维参数（使用默认值）。

### 积分处理

Lite 界面不暴露任何积分配置。部署脚本负责初始化以下 Platform Settings:

```json
{
  "credit.deduction_policy": "{\"tiers\":[{\"multiplier\":0}]}",
  "quota.runs_per_month": 999999,
  "quota.tokens_per_month": 999999999
}
```

这让所有 run 零积分消耗，配额近乎无限。后端 entitlement middleware 的检查逻辑不变，但因为 multiplier=0 + 超高配额，实质上不会阻断任何操作。用户无需感知积分系统的存在。

---

## 裁剪清单

以下现有 Console 功能在 Lite 中 **不出现**:

| 类别 | 裁剪的页面/功能 | 理由 |
|---|---|---|
| Platform | Feature Flags, Users, Registration, Invite Codes, Redemption Codes, Credits Admin, Broadcasts, Email 管理 | 平台运营功能，自部署不需要 |
| Configuration | MCP Configs、Personas（合并到 Agents） | 合并或不暴露 |
| Billing | Plans, Subscriptions, Entitlements, Usage 明细页 | SaaS 计费功能 |
| Security | IP Rules, Captcha, Gateway Config, Access Log | 网关安全，自部署场景由基础设施处理 |
| Integration | API Keys, Webhooks | 后续可按需加回 |
| Organization | Members, Teams, Projects | 多租户组织管理 |
| Operations | Audit Logs, Reports | 企业审计 |

---

## 权限模型

- Lite Console 要求 `platform.admin` 权限，与 Admin Console 一致
- 登录流程复用现有 AuthPage（Email + Password / OTP）
- 登录后使用同一 JWT，同一 session

---

## 部署模式

两个前端 app 独立构建，独立部署：

```
# 仅部署 Lite（推荐自部署用户）
docker compose up -d api worker console-lite

# 两个都部署（高级用户）
docker compose up -d api worker console-lite console
```

后端始终是同一个 API 服务，无需区分。Gateway 按路由前缀分发：
- `/lite/` -> console-lite 静态文件
- `/console/` -> console 静态文件
- `/v1/` -> API 服务

---

## 实现优先级

1. **Models** -- 最核心，决定其他页面的数据来源
2. **Agents** -- 核心业务，依赖 Models
3. **Dashboard** -- 独立，无依赖
4. **Runs** -- 大量复用现有组件
5. **Tools** -- 独立，较简单
6. **Memory** -- 3 个字段
7. **Settings** -- 收尾整合

---

## i18n 结构

复用现有 Console 的 `LocaleContext` + `useLocale()` 机制。Lite 的 locale 文件结构:

```typescript
// locales/zh-CN.ts
export const zhCN = {
  nav: {
    dashboard: '仪表盘',
    agents: '智能体',
    models: '模型管理',
    tools: '工具',
    memory: '记忆',
    runs: '运行记录',
    settings: '设置',
  },
  dashboard: {
    title: '仪表盘',
    runsTotal: '总运行次数',
    runsToday: '今日运行',
    inputTokens: '输入 Tokens',
    outputTokens: '输出 Tokens',
    tokenUsage30d: '近 30 天 Token 用量',
    refresh: '刷新',
  },
  agents: {
    title: '智能体',
    newAgent: '新建智能体',
    name: '名称',
    model: '模型',
    systemPrompt: '系统提示词',
    tools: '工具',
    active: '启用',
    advanced: '高级设置',
    temperature: 'Temperature',
    maxOutputTokens: '最大输出 Tokens',
    reasoningMode: '推理模式',
    reasoningDisabled: '关闭',
    reasoningEnabled: '开启',
  },
  models: {
    title: '模型管理',
    newModel: '添加模型',
    llm: 'LLM',
    asr: 'ASR',
    clientType: '客户端类型',
    name: '名称',
    apiKey: 'API Key',
    baseUrl: 'Base URL',
    modelName: '模型名称',
    provider: '提供方',
  },
  tools: {
    title: '工具',
    configure: '配置',
    active: '已启用',
    inactive: '未启用',
    clearCredential: '清除凭证',
  },
  memory: {
    title: '记忆',
    baseUrl: 'Base URL',
    rootApiKey: 'Root API Key',
    costPerCommit: '每次提交消耗',
  },
  runs: {
    title: '运行记录',
    id: 'ID',
    agent: '智能体',
    model: '模型',
    status: '状态',
    tokens: 'Tokens',
    time: '时间',
  },
  settings: {
    title: '设置',
    general: '通用',
    titleSummarizer: '标题摘要智能体',
    email: '邮件',
    senderAddress: '发送地址',
    sandbox: '沙箱',
    sandboxProvider: '提供方',
    sandboxBaseUrl: 'Base URL',
    sandboxDockerImage: 'Docker 镜像',
  },
  common: {
    save: '保存',
    cancel: '取消',
    edit: '编辑',
    delete: '删除',
    confirm: '确认',
    loading: '加载中',
    default: '默认',
    signOut: '退出',
  },
}
```

`en.ts` 结构相同，值替换为英文。
