---
---

# Claw 设计方案

本文给出 Arkloop Claw 模式的完整规划与设计。Claw 是 Arkloop 的自主代理执行模式，定位对标 OpenClaw，在 Chat 模式之外提供「AI 自主行动」的完整能力——执行 shell 命令、浏览网页、管理文件、接入外部 channel、调用 MCP/Skills。

结论先行：

- Claw 是顶层模式（Chat / Claw 双 Tab），但后端复用现有 persona + pipeline 体系，不另起一套。
- 与 Chat 的核心区别在于 **sandbox 持久化**、**UI 形态**和 **system prompt**，不在后端架构层面。
- 本地 Desktop 是 Claw 的终态交付形态，云端 Claw 仅作为开发者 debug 工具，默认不对外开放。
- 不做 Coder（不卷 LSP / 代码编辑），Claw 通过调用 OpenCode 等外部工具完成编码任务。
- Skills / MCP 由现有体系提供，Claw 直接复用。
- Claw 的产品入口继续以 `project` 为主；`workspace` 表示用户在某个 `project` 下持续复用的默认工作区，底层由 `workspace_ref / session_ref` 承载。

## 1. 背景与定位

### 1.1 行业参照

| 产品 | 模式 | 特征 |
|------|------|------|
| Claude Desktop | Chat / Cowork / Code 三 Tab | Cowork 面向非技术用户做文件管理、数据处理 |
| OpenClaw | 单一 Agent 模式 | 全渠道接入（Telegram / Discord / WhatsApp），Skills + MCP，本地优先 |
| Codex CLI | 终端 Agent | 纯编码场景，sandbox + approval workflow |

Arkloop Claw 的定位偏向 OpenClaw：全功能 Agent，不局限于编码或纯工作任务。Chat 已经融入了 Manus 级别的能力（搜索、代码执行、浏览器），Claw 是 Agent 的更上一级——从「AI 回应用户」变为「AI 在环境中自主行动」。

### 1.2 Chat vs Claw

```
Chat = 对话（用户主导，AI 回应）
Claw = 行动（AI 自主执行，用户监督）
```

### 1.3 核心对象关系

为避免 `project`、`workspace`、`thread`、`run` 混淆，Claw 首版统一按下面的语义理解：

| 对象 | 主要面向 | 作用 |
|------|----------|------|
| `project` | 用户 | 长期任务容器，是 Desktop 中的主入口 |
| `workspace` | 用户 + 系统 | `project` 下默认工作区，对应用户看到的 Working Folder |
| `thread` | 用户 | 对话记录，承载任务往返消息 |
| `run` | 系统 | 单次执行单元 |
| `session` | 系统 | 附着到 `workspace` 的活会话，可回收、可恢复 |

补充约束：

- 首版 UI 不单独暴露复杂的 workspace 管理界面，用户主要操作 `project`
- 同用户在同一 `project` 下的多个 `thread` 默认复用同一 `workspace`
- `session` 不是长期业务对象，只是 `workspace` 的运行时 attach 点

## 2. 架构概览

```
                      ┌─────────────────────────────────────┐
                      │          Arkloop Frontend            │
                      │  ┌──────────┐    ┌──────────────┐   │
                      │  │   Chat   │    │     Claw     │   │
                      │  │  (对话)   │    │  (自主执行)   │   │
                      │  └────┬─────┘    └──────┬───────┘   │
                      └───────┼─────────────────┼───────────┘
                              │                 │
                   ┌──────────▼─────────────────▼──────────┐
                   │            API（共用）                  │
                   │  Thread / Run / Event 模型不变          │
                   └──────────────────┬────────────────────┘
                                      │
                   ┌──────────────────▼────────────────────┐
                   │           Worker（共用）                │
                   │  Pipeline middleware 不变               │
                   │  Claw persona：                        │
                   │    - 不同的 system prompt              │
                   │    - 持久化 workspace sandbox          │
                   │    - tool allowlist 更开放             │
                   └──────────────────┬────────────────────┘
                                      │
                ┌─────────────────────┼─────────────────────┐
                │                     │                     │
     ┌──────────▼──────┐  ┌──────────▼──────┐  ┌──────────▼──────┐
     │  Cloud Sandbox   │  │  Docker Sandbox │  │  macOS VM       │
     │  (Firecracker)   │  │  (Docker)       │  │  (Vz.framework) │
     │  云端 debug      │  │  Linux Desktop  │  │  macOS Desktop   │
     └─────────────────┘  └─────────────────┘  └─────────────────┘
```

### 2.1 与 Chat 的共用关系

Claw 不是一个独立系统，它复用 Chat 的全部后端基础设施：

| 维度 | Chat | Claw |
|------|------|------|
| API 模型 | Thread -> Message -> Run -> Event | 相同 |
| Worker Pipeline | 相同的 middleware 链 | 相同 |
| Persona 体系 | normal / extended-search | claw（新增 persona） |
| System Prompt | 对话优化 | 任务执行 + 自主决策优化 |
| Sandbox 生命周期 | 按 Run 创建，用完销毁 | 持久化，用户绑定 |
| Tool 权限 | 受限（按 persona denylist） | 更开放（shell, fs, browser, mcp） |
| UI 形态 | 聊天气泡 | 对话 + 右侧任务状态面板 |

核心后端区别：**Sandbox 持久化**和 **Claw persona** 的 system prompt / tool policy。Pipeline、Event 模型、SSE 推送全部复用。

## 3. 前端设计

### 3.1 整体布局

参考 Claude Cowork 的三栏布局。顶部居中放置模式切换 Tab：

```
┌─────────────────────────────────────────────────────────────────────────┐
│  ← →                    [ Chat ]  [ Claw ]                             │
├────────────┬────────────────────────────────────┬──────────────────────┤
│            │                                    │                      │
│  + 新任务   │  任务标题 ▼                         │  Progress       ▽   │
│  搜索       │                                    │  ✓ — ✓ — ○          │
│  定时任务   │     ┌──────────────┐               │                      │
│  自定义     │     │  用户消息      │               │                      │
│            │     └──────────────┘               │  Working folder  ▷   │
│  最近       │                                    │                      │
│  ┌────────┐│  Thought process >                 │  Context         ▽   │
│  │当前任务 ││                                    │  ┌────┐ ┌────┐      │
│  │        ││  AI 回复内容                        │  │文件 │ │工具 │      │
│  └────────┘│  - 步骤 1 ...                      │  └────┘ └────┘      │
│            │  - 步骤 2 ...                      │                      │
│            │  - 步骤 3 ...                      │                      │
│            │                                    │                      │
│            │                                    │                      │
│            │                                    │                      │
│ 任务仅在本地 │  ┌──────────────────────────────┐  │                      │
│ 运行，不跨设 │  │ Reply...          Model ▼  ↑ │  │                      │
│ 备同步       │  └──────────────────────────────┘  │                      │
├────────────┴────────────────────────────────────┴──────────────────────┤
│  用户头像  用户名                                                        │
└─────────────────────────────────────────────────────────────────────────┘
```

### 3.2 三栏说明

**左栏：任务列表**

- 「+ 新任务」按钮（对应创建新 Thread，mode=claw）
- 搜索、定时任务（Scheduled）、自定义（Customize）
- 最近任务列表
- 底部提示：任务仅在本地运行，不跨设备同步（Desktop 模式）
- 云端 debug 模式下此提示隐藏

**中栏：对话区**

- 顶部任务标题（Thread title，可编辑/下拉切换）
- 对话式交互：用户消息 + AI 回复，与 Chat 模式相同的消息渲染
- 「Thought process」可折叠区域，展示 Agent 的推理过程
- AI 回复中内嵌工具调用结果（shell 输出、文件操作、浏览器截图等）
- 底部输入框：Reply 输入 + 模型选择器 + 发送按钮

**右栏：任务状态面板**

- **Progress**：任务进度指示器，由 Agent 的 todo/plan 工具驱动（✓ — ✓ — ○ 形式）
- **Working folder**：当前工作目录，点击展开 sandbox 文件树
- **Context**：本次任务使用的工具和引用文件的缩略图/列表

### 3.3 与 Chat 模式的 UI 差异

| 维度 | Chat | Claw |
|------|------|------|
| 左栏 | 对话列表（New chat） | 任务列表（New task） |
| 中栏 | 对话消息流 | 对话消息流（相同） |
| 右栏 | 无（或极简） | Progress / Working folder / Context |
| 顶部 Tab | Chat 高亮 | Claw 高亮 |
| 输入框 | 对话输入 | 任务输入（Reply） |

中栏几乎完全复用 Chat 的消息渲染组件。核心新增工作量在右栏的三个面板。

### 3.4 右栏面板详细设计

#### Progress

数据来源：Agent 在执行过程中调用 todo/plan 工具写入的任务拆分和完成状态。

```
Progress                                  ▽
┌──────────────────────────────────────────┐
│  ✓ 扫描 ~/Downloads 目录                  │
│  │                                       │
│  ✓ 按文件类型创建子目录                     │
│  │                                       │
│  ○ 移动文件到对应目录                       │
│  │                                       │
│  ○ 生成整理报告                            │
└──────────────────────────────────────────┘
```

#### Working folder

展示 sandbox 内的文件树，支持点击查看文件内容。

```
Working folder                            ▽
┌──────────────────────────────────────────┐
│  ~/workspace                             │
│  ├── sorted/                             │
│  │   ├── pdf/                            │
│  │   ├── images/                         │
│  │   └── docs/                           │
│  ├── report.md                           │
│  └── ...                                 │
└──────────────────────────────────────────┘
```

#### Context

展示本次任务中 Agent 使用的工具和引用的文件。

```
Context                                   ▽
┌──────────────────────────────────────────┐
│  Tools: shell_execute, file_read         │
│                                          │
│  Files:                                  │
│  ┌─────┐ ┌─────┐ ┌─────┐               │
│  │.pdf │ │.xlsx│ │.md  │               │
│  └─────┘ └─────┘ └─────┘               │
└──────────────────────────────────────────┘
```

### 3.5 前端实现路径

1. `App.tsx` 增加顶层 mode 状态（chat / claw）和顶部居中 Tab 切换
2. `src/components/` 下新增 `ClawPage.tsx` 作为 Claw 模式的根组件（三栏布局）
3. 左栏：复用/改造现有 ThreadList 组件，增加 mode=claw 过滤和「新任务」入口
4. 中栏：复用现有消息渲染组件（ThinkingBlock、CodeExecutionCard 等），几乎无需改动
5. 右栏：新增三个面板组件 `ProgressPanel`、`WorkingFolderPanel`、`ContextPanel`
6. Claw 的 Thread 列表使用同一个 `/v1/threads` API，通过 query param 过滤 `mode=claw`
7. 复用现有 `useSSE` hook 接收 Run 事件流

## 4. 云端 Claw（Debug 模式）

云端 Claw 面向开发者，用于调试 Claw 前端和 agent 逻辑，无需本地安装 Desktop。

### 4.1 与 Chat Sandbox 的区别

| 维度 | Chat Sandbox | Claw Sandbox |
|------|-------------|-------------|
| 生命周期 | 随 Run 创建，Run 结束销毁 | 绑定用户在 `project` 下的默认工作区，跨 Run 保留 |
| 状态 | 无状态 | 持久化文件系统 |
| 关联实体 | run_id | `project_id` 解析出的 `workspace_ref` |
| timeout | 按执行超时 | 独立的 idle timeout + max lifetime |

### 4.2 Workspace API

```
GET    /v1/projects/{project_id}/workspace            # 当前 project 的默认 workspace 详情
GET    /v1/projects/{project_id}/workspace/files      # 文件树
GET    /v1/projects/{project_id}/workspace/file?path= # 文件内容
```

Workspace 数据口径：

| 字段 | 类型 | 说明 |
|------|------|------|
| project_id | UUID | 用户可见的长期任务容器 |
| workspace_ref | TEXT | 默认工作区稳定引用 |
| owner_user_id | UUID | 工作区所有者 |
| status | enum | active / idle / unavailable |
| last_used_at | timestamp | 最近使用时间 |

Thread 首版不直接绑定 `workspace_id`。`POST /v1/threads` 继续只绑定 `project_id`，后端在 Run 执行前根据 `project_id` 解析默认 `workspace_ref`。同一 `workspace` 下可以有多个 `thread`，但这层关系由 `project -> workspace` 默认绑定决定，不要求前端手工传递 `workspace_id`。

### 4.3 访问控制

- 默认通过 feature flag 关闭
- 面向开发者按需开启
- 后续可考虑对外开放，但不在首版范围

## 5. Desktop 应用

Desktop 是 Claw 的终态交付形态。

### 5.1 架构

```
┌────────────────────────────────────────────┐
│              Desktop App Shell             │
│          （Tauri / Electron，待定）          │
│                                            │
│  ┌──────────────────────────────────────┐  │
│  │          Frontend（Web 复用）         │  │
│  │      Chat / Claw UI（同一份代码）     │  │
│  └─────────────────┬────────────────────┘  │
│                    │ HTTP                   │
│  ┌─────────────────▼────────────────────┐  │
│  │         Embedded Backend             │  │
│  │  ┌──────┐  ┌────────┐  ┌─────────┐  │  │
│  │  │ API  │  │ Worker │  │ Sandbox │  │  │
│  │  │(lite)│  │        │  │ Adapter │  │  │
│  │  └──┬───┘  └────┬───┘  └────┬────┘  │  │
│  │     │           │           │        │  │
│  │  ┌──▼───────────▼───────────▼─────┐  │  │
│  │  │           SQLite               │  │  │
│  │  └───────────────────────────────┘  │  │
│  └──────────────────────────────────────┘  │
└────────────────────────────────────────────┘
         │
         ▼ （本地文件系统访问）
    用户的电脑
```

### 5.2 运行模式

Desktop 支持三种后端连接模式：

| 模式 | 描述 | 本地需要部署的 |
|------|------|---------------|
| **SaaS** | 设置 base URL 连接 Arkloop 云服务 | 仅前端 shell |
| **Self-hosted** | 设置 base URL 连接自部署的服务（VPS + setup.sh） | 仅前端 shell |
| **Local** | 内嵌后端，全部本地运行 | 无（自带） |

SaaS 和 Self-hosted 模式下，Desktop 只是一个带 Claw UI 的壳，连接远端 API。
Local 模式下，Desktop 内嵌精简版后端，全部本地运行。

### 5.3 基础设施降级（Local 模式）

Local 模式需要去除重量级依赖：

#### PostgreSQL -> SQLite

- `src/services/shared/` 抽象 database adapter interface
- 现有 repository 层（`internal/data/`）实现 PostgreSQL adapter
- 新增 SQLite adapter，实现相同接口
- migration 系统适配 SQLite 语法差异（无 `ALTER TYPE`、无 `CONCURRENTLY` 等）
- Desktop 启动时自动执行 SQLite migration

#### Redis -> 进程内替代

Redis 在 Arkloop 中承担三个角色，Local 模式下全部用进程内实现替代：

| 角色 | 云端实现 | Desktop 替代 |
|------|----------|-------------|
| 消息队列（job dispatch） | Redis List | Go channel |
| Pub/Sub（SSE 事件推送） | Redis Pub/Sub | 进程内 event bus |
| 并发限制（run slots） | Redis SETNX | sync.Mutex / atomic |

Desktop 场景是单用户单进程，不需要分布式协调，进程内实现足够。

#### 对象存储

| 云端 | Desktop |
|------|---------|
| MinIO / S3 | 本地文件系统（`~/.arkloop/storage/`） |

现有 `objectstore` 包已支持 filesystem backend，无需额外改动。

### 5.4 Desktop 框架选型

待定。候选：

| 框架 | 优势 | 劣势 |
|------|------|------|
| **Tauri** | 轻量（Rust），包体小，安全沙箱好 | Rust 生态，Go 服务需作为 sidecar |
| **Electron** | 成熟稳定，生态丰富，社区方案多 | 包体大（~150MB+），内存占用高 |

两种方案下 Go 后端都是独立进程，通过 HTTP 与前端通信。区别在于 Desktop shell 层用 Rust 还是 Node.js 管理进程生命周期。

## 6. Sandbox 跨平台扩展

现有 sandbox 支持 Firecracker（Linux + KVM）和 Docker（通用）。Desktop 需要扩展：

| 平台 | 方案 | 说明 |
|------|------|------|
| Linux（有 KVM） | Firecracker | 现有实现 |
| Linux（无 KVM）| Docker | 现有实现 |
| macOS | Apple Virtualization.framework | 轻量 VM，无需 Docker |
| macOS（备选）| Docker Desktop | 用户已安装 Docker 时复用 |
| Windows | WSL2 + Docker | 利用 WSL2 内置 |
| Windows（备选）| Hyper-V | 原生虚拟化 |

### 6.1 macOS Virtualization.framework 适配

`src/services/sandbox/` 新增 provider：

```
internal/
├── firecracker/    # 现有
├── docker/         # 现有
└── vz/             # 新增：Apple Virtualization.framework
    ├── provider.go
    ├── vm.go
    └── network.go
```

- 实现 `sandbox.Provider` 接口（与 Firecracker / Docker 同级）
- 使用 Code Signing Entitlements 获取虚拟化权限
- VM 内运行与 Firecracker 相同的 rootfs，保证行为一致
- 通过 `virtio-vsock` 与宿主通信

### 6.2 Provider 自动选择

```go
func ResolveProvider() string {
    if runtime.GOOS == "darwin" {
        if vzAvailable() {
            return ProviderVz
        }
        return ProviderDocker
    }
    if runtime.GOOS == "linux" && kvmAvailable() {
        return ProviderFirecracker
    }
    return ProviderDocker
}
```

## 7. 实现路线

### Phase 1：云端 Claw + 前端

目标：在现有云端架构上跑通 Claw 模式的完整链路，验证前端设计和 agent 逻辑。

#### 1.1 Thread mode 字段

**需求**：Thread 需要区分 Chat 和 Claw 两种模式，以隔离显示和行为。

- `threads` 表增加 `mode` 字段（enum: `chat` / `claw`，默认 `chat`）
- `POST /v1/threads` 请求体增加可选 `mode` 参数
- `GET /v1/threads` 增加 `mode` query param 用于过滤
- 现有 Chat 流程不受影响（mode 默认 chat）
- migration 脚本处理存量数据（全部标记为 chat）

#### 1.2 Claw Persona

**需求**：Claw 模式使用专用 persona，定义不同的 system prompt 和 tool 权限。

- 新增 persona 记录 `claw`，配置：
  - `executor_type`: `agent.simple`（复用现有简单 agent 循环）
  - `system_prompt`: 任务执行导向（强调自主决策、分步执行、进度汇报），参考 Anthropic cowork prompt 的骨架，但重写为 Arkloop 语境；删除所有 MCP / Claude / 不存在工具的描述
  - `tool_allowlist`: `web_search`、`web_fetch`、`exec_command`、`write_stdin`、`python_execute`、`browser`、`document_write`、`memory_search`、`memory_read`、`conversation_search`、`spawn_agent`、`summarize_thread`、`timeline_title`
  - `tool_denylist`: 空；但 Phase 1 不开放 `memory_write` / `memory_forget`
  - `temperature`: `0.4`
  - `max_output_tokens`: `12288`
  - `tool_continuation_budget`: `128`
- repo 资产落在 `src/personas/claw/`，作为 builtin persona 随服务启动自动加载
- `user_selectable: false`，因此不出现在普通 Chat 的 persona selector 中
- persona 的 tool budget 和 soft limits 需要独立配置（Claw 任务通常更长）
- system prompt 中要求 Agent 在执行多步任务时持续更新 `timeline_title`，并在必要时先给出简短执行计划
- API 默认绑定规则：`mode=claw` 的 thread 在创建 run 时，如果请求未显式传 `persona_id`，后端自动注入 `persona_id=claw`；若显式传入其他 persona，则保留调用方值用于调试/覆写

#### 1.3 前端模式切换

**需求**：顶部居中的 Chat / Claw Tab，切换后加载不同的页面布局。

- 顶部导航栏居中放置 `[ Chat ] [ Claw ]` 两个 Tab
- 当前选中 Tab 高亮样式
- mode 状态持久化到 localStorage，刷新后恢复
- 路由方案：`/chat/*` 和 `/claw/*`，Tab 切换时跳转
- Chat 模式下的所有现有功能和布局不变

#### 1.4 Claw 页面三栏布局

**需求**：Claw 模式的主页面，三栏布局。

- **左栏**（~260px，可折叠）：
  - 「+ 新任务」按钮，点击创建 mode=claw 的 Thread
  - 搜索框（过滤任务列表）
  - 最近任务列表（复用 ThreadList 组件，传入 mode=claw 过滤）
  - 任务项显示：标题 + 状态指示 + 右键菜单（删除/重命名）
- **中栏**（弹性宽度）：
  - 顶部：任务标题（可编辑）+ 下拉菜单
  - 主体：消息流（复用 Chat 的消息渲染组件）
  - 底部：输入框（Reply placeholder）+ 模型选择器 + 发送按钮
  - 消息组件复用：ThinkingBlock、CodeExecutionCard、ExecutionCard、SearchTimeline 等全部复用
- **右栏**（~300px，可折叠）：
  - 三个可折叠面板的容器（详见 1.5-1.7）
  - 无任务时显示空状态引导

#### 1.5 右栏：Progress 面板

**需求**：展示当前任务的执行进度。

- 数据来源：Agent 在执行过程中调用 todo/plan 工具写入的步骤和状态
- 工具调用结果通过 SSE event 推送到前端
- 进度显示：步骤列表 + 每步状态（✓ 完成 / ● 进行中 / ○ 待执行）
- 步骤之间用连线连接，形成 pipeline 视觉
- 无进度数据时显示占位文案
- Agent 未调用 todo 工具时，面板保持空状态，不强制
- todo/plan 工具的 tool definition 需要冻结（创建/更新/删除/列表四个 action）

#### 1.6 右栏：Working Folder 面板

**需求**：展示 sandbox 内的文件树。

- 数据来源：`GET /v1/projects/{project_id}/workspace/files?path=`（Workspace API，见 1.9）
- 树形展示，支持展开/折叠目录
- 点击文件可预览内容（文本文件直接显示，二进制文件显示类型信息）
- 文件树定时轮询刷新（或由 SSE event 触发刷新）
- 无 workspace 时显示引导（设置 working folder）

#### 1.7 右栏：Context 面板

**需求**：展示本次任务中 Agent 使用的工具和引用的文件。

- 数据来源：从 Run events 中提取 tool_call 和 file 引用
- 工具列表：显示本次 Run 中调用过的工具名称
- 文件列表：显示被读写的文件，缩略图或图标形式
- 随 SSE event 实时更新
- 首版可以做简化实现：仅从 event stream 中提取，不做额外 API

#### 1.8 Workspace 数据模型

**需求**：定义 Claw 在产品层的默认工作区语义，并与现有执行模型对齐。

- 首版不新增独立 `workspaces` 主表，不以 `sandbox_session_id` 作为长期对象主键
- `workspace` 在产品层表示“用户在某个 `project` 下可持续复用的默认工作区”
- 底层复用现有 `profile_ref / workspace_ref / workspace_registries / default_workspace_bindings / shell_sessions`
- 同用户 + 同 `project` 下多个 `thread` 复用同一 `workspace_ref`
- 无 `project_id` 时回落到 thread 级 workspace，作为兼容路径
- 不同用户即使处于同一 `project`，默认仍按 profile 隔离

#### 1.9 Workspace API

**需求**：提供基于 `project` 视角的 Workspace 查询与文件操作 API。

- `GET /v1/projects/{project_id}/workspace` - 获取默认 workspace 详情（`workspace_ref`、status、最近使用时间、活动 session 摘要）
- `GET /v1/projects/{project_id}/workspace/files?path=` - 获取指定目录的文件列表
- `GET /v1/projects/{project_id}/workspace/file?path=` - 获取文件内容
- 首版不引入独立 `POST /v1/workspaces` / `DELETE /v1/workspaces/{id}` CRUD，默认 workspace 由 `project_id` 解析
- Phase 1 云端 debug 可以先复用现有基于 `run_id` 的 workspace 文件读取链路，前端对外仍按 `project` 视角封装
- API 层复用现有 auth/RBAC，`project` 与解析出的 `workspace` 必须属于当前用户/组织

#### 1.10 Sandbox Session 持久化

**需求**：Claw 的工作区状态需要跨 Run 保留，session 只负责附着与恢复。

- 长期持久化对象是 `workspace_ref` 对应的文件系统状态，不是 live sandbox session
- `session_ref` / live session 只是 `workspace` 的 attach 点，可回收、可恢复、可替换
- session idle 或被回收时，将 `/workspace` 状态刷入 object store，并更新 `latest_manifest_rev`
- 新 Run 进入同一 `project` 时，优先恢复该 `workspace_ref` 对应的环境
- workspace status 由 registry 与 live session 聚合得出，首版对外统一为 `active / idle / unavailable`
- Desktop Local 复用同一套 `workspace_ref` 语义，云端 debug 只是 provider 与部署形态不同

#### 1.11 云端 Claw Feature Flag

**需求**：云端 Claw 默认关闭，仅对开发者开放。

- 新增 feature flag `claw_enabled`
- 前端根据 feature flag 决定是否显示 Claw Tab
- API 层面：mode=claw 的请求在 flag 关闭时返回 403
- `claw_enabled` 为平台级默认值开关，默认关闭
- admin console 继续通过现有 Feature Flags 页面管理此 flag
- `claw_enabled` 不支持 org override

---

### Phase 2：基础设施抽象

目标：为 Desktop Local 模式做准备，将 PostgreSQL、Redis 等重依赖抽象为可替换的接口。

#### 2.1 Database Adapter Interface

**需求**：定义统一的数据库访问接口，使 PostgreSQL 和 SQLite 可互换。

- 在 `src/services/shared/` 中定义 database adapter interface
- 接口覆盖：连接管理、migration、事务、CRUD
- 现有各 service 的 `internal/data/` 层实现 PostgreSQL adapter（封装现有代码）
- 不改变现有 repository 的外部 API，只抽象底层连接
- 需要处理的 SQL 方言差异清单：`RETURNING`、`ON CONFLICT`、`jsonb`、序列/自增、时间函数

#### 2.2 SQLite Adapter

**需求**：实现 SQLite 版本的 database adapter。

- 使用 `modernc.org/sqlite`（纯 Go，无 CGO 依赖）
- 实现 2.1 定义的 adapter interface
- migration 文件维护 SQLite 版本（与 PostgreSQL migration 并行，不共用）
- 处理 SQLite 限制：单写者、无 `ALTER COLUMN`、有限的并发
- Desktop 启动时自动检测并执行 pending migration
- 数据模型简化：去掉云端专用表（orgs、subscriptions、credits 等），保留核心表（threads、messages、runs、run_events、workspaces、users）

#### 2.3 Job Queue 进程内实现

**需求**：替代 Redis List 的 job dispatch 功能。

- 实现 `JobQueue` interface（现有 Redis 实现提取接口）
- 新增 `ChannelJobQueue`：基于 Go channel 的进程内实现
- 支持：Enqueue、Lease（非阻塞轮询 + 通知回调）、Ack、Nack/Retry
- 当队列总数超过阈值（默认 1000）时自动清理已完成/死信 job
- Desktop 场景下 concurrency=1 足够（单用户）
- 通过 build tag 或配置选择实现

#### 2.4 Event Bus 进程内实现

**需求**：替代 Redis Pub/Sub 的 SSE 事件推送功能。

- 实现 `EventBus` interface（现有 Redis Pub/Sub 提取接口）
- 新增 `LocalEventBus`：基于 Go channel + sync.RWMutex + map 的进程内实现
- 支持：Publish、Subscribe、Unsubscribe
- topic 粒度与现有 Redis channel 一致（per-run）
- SSE handler 订阅方式不变，仅底层切换

#### 2.5 Concurrency Limiter 进程内实现

**需求**：替代 Redis SETNX 的并发限制功能。

- 实现 `ConcurrencyLimiter` interface
- 新增 `LocalConcurrencyLimiter`：基于 sync.Mutex + map 的进程内计数器
- Desktop 场景下 slot 数固定（如 2），不需要分布式协调
- 与现有 `runlimit` 包对齐接口

#### 2.6 Build Tags 区分

**需求**：通过 Go build tags 区分云端和 Desktop 构建。

- 定义 build tag：`cloud`（默认）和 `desktop`
- 各 adapter 实现文件使用 build tag 条件编译
- Desktop build 排除：Redis 依赖、PostgreSQL driver、S3 SDK
- Desktop build 包含：SQLite driver、channel queue、local event bus
- Makefile / goreleaser 配置增加 desktop target

---

### Phase 3：Sandbox 跨平台

目标：扩展 sandbox provider，支持 macOS / Windows Desktop 场景。

#### 3.1 Provider Interface 审计

**需求**：确认现有 `sandbox.Provider` 接口是否足够通用，能承载新 provider。

- 审计现有接口方法：Create、Destroy、Exec、Shell、FileRead、FileWrite 等
- 确认 Firecracker 和 Docker provider 是否有泄漏的实现细节
- 如有不通用的部分，重构接口
- 输出：冻结的 Provider interface 定义

**状态：✅ 已完成**

审计发现旧 `VMPool` 接口存在三项泄漏：`Acquire` 返回 `*os.Process`（Firecracker 特有，Docker 返回 nil）；`DestroyVM` 接受 `*os.Process` 参数（Docker 忽略）；Manager 被迫维护 `procs map[string]*os.Process`。已全部修复。

**冻结的 Provider 接口定义**（`session/manager.go`）：

```go
// Provider 抽象隔离执行环境（microVM / 容器 / Vz VM）的获取与销毁。
// Firecracker、Docker、Vz 等后端均实现此接口。
type Provider interface {
    // Acquire 获取一个就绪的隔离执行环境，返回可用的 Session。
    // sessionID 由调用方指定，provider 必须将其设置为返回 Session 的 ID。
    Acquire(ctx context.Context, sessionID, tier string) (*Session, error)

    // Destroy 销毁 sessionID 对应的执行环境并释放所有关联资源。
    Destroy(sessionID string)

    // Ready 返回 provider 是否完成初始预热。
    Ready() bool

    // Stats 返回运行时统计。
    Stats() PoolStats

    // Drain 停止 provider 并销毁所有预热环境。Graceful shutdown 时调用。
    Drain(ctx context.Context)
}
```

**重构变更清单**：

| 变更 | 说明 |
|------|------|
| `VMPool` → `Provider` | 接口重命名，适用于 VM / 容器 / 任意后端 |
| `Acquire` 去掉 `*os.Process` | 进程管理下沉到各 provider 内部 |
| `Acquire` 增加 `sessionID` 参数 | provider 按 sessionID 注册资源，支持 Destroy 查找 |
| `DestroyVM` → `Destroy(sessionID)` | 按 sessionID 销毁，provider 内部持有资源映射 |
| Manager 删除 `procs` map | 不再持有 Firecracker 特有的进程引用 |
| WarmPool / Docker Pool 内部 `active` map | 各 provider 自行维护 sessionID → 资源的映射 |

#### 3.2 macOS Virtualization.framework Provider

**需求**：基于 Apple Virtualization.framework 实现 sandbox provider。

- `src/services/sandbox/internal/vz/` 新增 provider
- 实现冻结的 Provider interface
- VM 配置：CPU、内存、磁盘可配，默认值适合轻量任务
- 使用与 Firecracker 相同的 rootfs 镜像（保证 guest 环境一致）
- 网络：virtio-net + NAT（通过宿主网络出）
- 宿主通信：virtio-vsock（guest agent 通信）
- 需要 macOS 12+ 和对应 entitlements
- 冷启动性能目标：< 3 秒

#### 3.3 Provider 自动选择

**需求**：根据运行环境自动选择最优 sandbox provider。

- 检测逻辑：OS -> 虚拟化能力 -> 已安装工具
- darwin + Vz available -> ProviderVz
- darwin + Docker available -> ProviderDocker
- linux + KVM available -> ProviderFirecracker
- linux + Docker available -> ProviderDocker
- windows + WSL2 + Docker -> ProviderDocker
- 用户可通过配置强制指定 provider（覆盖自动选择）
- 首次启动时检测并缓存结果

#### 3.4 Rootfs 跨平台构建

**需求**：rootfs 镜像需要能在 Firecracker / Vz / Docker 三种环境下使用。

- Firecracker：ext4 raw image（现有）
- Vz：同一 ext4 raw image（Vz 支持直接挂载）
- Docker：Dockerfile 基于同一基础构建（保证工具链一致）
- CI pipeline 增加 rootfs 构建和发布步骤
- Desktop 安装包内嵌 rootfs 或首次启动时下载

---

### Phase 4：Desktop 应用

目标：打包 Desktop 应用，交付完整的本地 Claw 体验。

#### 4.1 框架选型 POC

**需求**：确定 Desktop 框架（Tauri vs Electron），通过 POC 验证。

- POC 验证点：
  - Go sidecar 进程管理（启动、健康检查、重启、优雅退出）
  - 前端加载（本地 web 资源打包方式）
  - 包体大小对比
  - 内存占用对比
  - 原生菜单、系统托盘、通知集成
  - 自动更新机制可行性
- 输出：选型决策文档 + POC demo

#### 4.2 Go Backend Lite

**需求**：Desktop 版本的精简 Go 后端。

- 单二进制，内嵌 API + Worker + Sandbox adapter
- 使用 desktop build tag 编译（SQLite + channel queue + local event bus）
- 启动流程：初始化 SQLite -> 执行 migration -> 启动 HTTP server -> 启动 worker
- 监听 localhost 端口（如 127.0.0.1:19001），不暴露到网络
- 内嵌默认 persona 配置（不依赖数据库 seed）
- auth 简化：Desktop 单用户模式，跳过 JWT 验证（或使用固定 token）

#### 4.3 Desktop Shell 集成

**需求**：Desktop 框架与 Go 后端、前端的集成。

- 框架启动时：
  1. 启动 Go sidecar 进程
  2. 等待 health check 通过
  3. 加载前端页面（指向 localhost API）
- 框架退出时：
  1. 通知 Go 进程优雅退出
  2. 等待 sandbox session 保存状态
  3. 终止 Go 进程
- 系统托盘：最小化到托盘，后台保持运行
- 全局快捷键：快速唤起窗口

#### 4.4 Settings UI

**需求**：三种连接模式的配置界面。

- Settings 页面增加「连接模式」配置项
- SaaS：输入 base URL（默认 Arkloop 云服务地址）+ API Key
- Self-hosted：输入自部署的 base URL + API Key
- Local：无需配置，使用内嵌后端（显示本地状态：SQLite 路径、sandbox provider 等）
- 切换模式时：验证连接可用性 -> 保存 -> 重启相关服务
- 配置持久化到本地文件（`~/.arkloop/config.json`）

#### 4.5 安装包构建

**需求**：跨平台安装包。

- macOS：.dmg，包含 app bundle + Go binary + rootfs image
- Linux：.AppImage + .deb，包含前端 + Go binary + rootfs image
- Windows：.msi，包含前端 + Go binary（sandbox 依赖 WSL2 + Docker）
- CI pipeline：GitHub Actions 构建和发布
- 代码签名：macOS notarization + Windows Authenticode
- rootfs 首次启动下载（可选，减小安装包体积）

#### 4.6 自动更新

**需求**：Desktop 应用的自动更新机制。

- 检查更新：启动时 + 定期检查（GitHub Releases 或自建更新服务）
- 下载：增量更新优先，全量更新兜底
- 安装：提示用户 -> 下载 -> 替换 -> 重启
- Go 后端更新：替换二进制 + 执行 migration
- rootfs 更新：替换镜像文件

## 8. 非目标

- 不做 Code 模式（不卷 LSP、代码编辑器、diff viewer）
- 不在首版做云端 Claw 的公开开放
- 不在首版做每用户每 `project` 的多 workspace 管理（仅暴露一个默认 workspace）
- Skills / MCP 集成不在本文档范围，由团队独立推进
