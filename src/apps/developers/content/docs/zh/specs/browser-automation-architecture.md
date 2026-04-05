---
title: "Browser Automation 设计方案"
---
本文给出 Arkloop 浏览器自动化能力的完整规划与设计。核心思路是将浏览器作为 Sandbox 的一个新 Tier 接入，复用现有 session 管理、checkpoint/restore、pool 等基础设施，而非新建独立微服务。

结论先行：

- 选用 Vercel Labs `agent-browser` 作为浏览器自动化引擎，它是 Rust CLI + Node.js daemon 架构，天然适配 Arkloop 的工具分发模型。
- 浏览器作为 Sandbox 的 `browser` tier 接入，使用独立 Docker 镜像（Node + Chromium，不含 Python），资源效率等同独立服务。
- Worker 侧暴露单个 `browser` 工具，接受 CLI 命令字符串，而非 24 个独立工具定义。System prompt 成本约 400 tokens 而非 6000+。
- Session 管理完全复用现有 `share_scope` 模型（run / thread / workspace / org），checkpoint 自动覆盖浏览器状态。
- 计费采用混合模式：session 基础费 + 超时累计。
- 用户体验第一阶段采用 Screenshot 快照，后续视需求决定是否升级 VNC 实时流。

## 1. 背景与动机

当前 Arkloop 的 web 能力仅限于静态内容获取：

- `web_fetch`：HTTP 请求 + 内容提取（Jina / Firecrawl / 基础 HTTP）
- `web_search`：搜索引擎结果（Tavily / Searxng）

这两个工具无法处理：

1. **JavaScript 渲染页面**：SPA 应用、动态加载内容
2. **交互式操作**：登录、表单填写、多步流程
3. **复杂浏览场景**：多 Tab、Cookie 管理、网络请求监控
4. **前端调试**：控制台日志、网络瀑布图、DOM 检查

用户已经在请求"帮我走一遍页面流程""复现前端 bug""登录某个后台系统"等场景，这些都需要真实浏览器能力。

## 2. 竞品分析

### 2.1 可选引擎

| 项目 | 类型 | 语言 | 核心特点 | 评估 |
|------|------|------|----------|------|
| **Vercel agent-browser** | CLI | Rust + Node.js | ref 语义自动化，低 token 消耗，session 隔离，50+ 命令 | 最优选 |
| Stagehand (Browserbase) | SDK | TypeScript | act/extract/observe API，需集成 LLM | 可选但偏重 |
| Browser Use | SDK | Python | 视觉识别 + 推理 | 语言栈不匹配 |
| Lightpanda | 引擎 | Zig | 极低内存，兼容 Playwright 协议 | 太早期 |
| Playwright MCP | MCP Server | TypeScript | 标准 MCP 协议，24+ 工具 | 工具定义 token 成本高 |

### 2.2 业界做法

| 产品 | 浏览器接入方式 | 工具粒度 | 展示方式 |
|------|--------------|----------|----------|
| Claude Code | Chrome 扩展（`--chrome`）或 Playwright MCP | 多个独立工具 | 真实浏览器可见 |
| Codex | Skills + Playwright MCP，sandbox 内执行 | 多个独立工具 | sandbox 内截图 |
| Manus | Browser Use (Python)，云端 sandbox | 封装较高 | VNC 实时流 + Take Over |

### 2.3 选择 agent-browser 的理由

1. **CLI 接口天然契合 Arkloop 的工具分发模型**：Worker 现有的 `DispatchingExecutor` 通过命令调用工具，agent-browser 就是 CLI。Worker 的 `browser` 工具本质是 `sandbox.exec("agent-browser <cmd>")`，零适配成本。

2. **ref 语义自动化**：用 accessibility tree 生成 `@e1, @e2` 引用，比 CSS selector 稳定，token 消耗比 Playwright MCP 低约 82%。

3. **单工具 + CLI 命令模式**：不需要定义 24 个独立工具的 JSON Schema，一个 `browser` 工具 + 命令速查表 ≈ 400 tokens。

4. **Session 隔离内建**：每个 `--session <id>` 拥有独立 cookies、storage、导航状态，天然支持多租户。

5. **Rust CLI + Node.js daemon**：冷启动快，常驻 daemon 避免重复初始化 Chromium。

## 3. 目标与非目标

### 3.1 目标

- 提供一个对 Agent 友好的浏览器工具
- 支持完整的网页交互：导航、点击、输入、表单填写、截图
- 支持多 Tab 管理
- 支持开发者调试场景：JS 执行、控制台日志、网络请求
- 支持 Cookie / Storage 管理与跨 session 恢复
- 支持 LLM 自主触发和用户显式触发两种模式
- Default browser session 模型：不指定 session 时自动复用默认浏览器
- 计费能力：按 session 基础费 + 超时收积分
- 三端一致：Linux / macOS / Windows 开发环境都能工作

### 3.2 非目标

- 不在 Phase 1 实现 VNC 实时流（后续视需求决定）
- 不实现 Tool Discovery / 动态工具加载（归入全局 Tool Search 规划）
- 不恢复"正在运行中的浏览器进程"到新容器（只恢复 cookies/storage）
- 不在浏览器容器里预装 Python / 完整 sandbox 工具链
- 不支持浏览器扩展安装（Phase 1）

## 4. 关键设计决策

### 4.1 浏览器作为 Sandbox Browser Tier，而非独立服务

#### 决策

不新建 Browser Service 微服务，而是在现有 Sandbox Service 上新增 `browser` tier：

```text
Worker → Sandbox Service
           ├── tier=lite    → arkloop-sandbox-lite    (Python, 基础工具)
           ├── tier=pro     → arkloop-sandbox-pro     (Python+Node, 全量工具)
           └── tier=browser → arkloop-sandbox-browser  (Node+Chromium+agent-browser)
```

#### 理由

1. **资源效率等同独立服务**：browser tier 使用独立 Docker 镜像，不包含 Python / pip 等无用负载，内存开销与独立服务一致（~250MB）。

2. **零重复基础设施**：session 管理、warm pool、checkpoint/restore、idle timeout、share_scope 全部复用。一个新微服务需要重新实现这些。

3. **Checkpoint 自动覆盖浏览器状态**：Sandbox 的 checkpoint 是整个文件系统的 tar.zst 归档，agent-browser 的 session 数据（cookies, storage, profiles）在文件系统内，自动被快照，不需要额外的 cookie 导出/导入逻辑。

4. **代码执行与浏览器可联动**：如果 Agent 需要在 sandbox 里写脚本处理从浏览器下载的文件，同一容器内直接操作，无需跨服务传输。（注：此场景可通过 Agent 协调两个 session 实现，不是必须同容器。）

5. **部署极简**：不用额外跑一个服务。自部署用户改一个 Docker 镜像 + 加一行 tier 配置。

#### 资源对比

| 维度 | 独立 Browser Service | Sandbox Browser Tier |
|------|---------------------|---------------------|
| Browser 容器内存 | ~250MB | ~250MB |
| 新增代码量 | 大（全新服务） | 小（Dockerfile + tier 配置） |
| Session 管理 | 重新实现 | 复用 |
| Checkpoint/恢复 | 自建 cookie 导出 | 文件系统自动覆盖 |
| 部署复杂度 | 新服务+新部署 | 无新增 |
| 资源浪费 | 无 | 无（独立镜像，不含 Python） |

### 4.2 单工具 + CLI 命令参数，而非多工具定义

#### 决策

Worker 侧暴露单个 `browser` 工具，接受 agent-browser CLI 命令字符串。

#### 理由

24 个独立工具的 JSON Schema 定义约占 4000-8000 tokens system prompt。单工具 + 命令速查表约 400 tokens，节省 90%+。

agent-browser 的 CLI 语法本身就是为 LLM 设计的——`verb [args]` 的自然语言风格，LLM 几乎不需要额外学习。

#### 对比

| 方案 | System Prompt 成本 | LLM 调用准确率 | 实现复杂度 |
|------|-------------------|---------------|-----------|
| 24 个独立工具 | ~6000 tokens | 高（类型安全） | 中（每个工具一个 executor） |
| 单工具 + CLI 命令 | ~400 tokens | 中高（CLI 语法足够清晰） | 低（一个 executor + 命令透传） |
| Bash 统一（融入 sandbox） | 0（复用 exec_command） | 中（需 LLM 知道 CLI） | 低但耦合 |

### 4.3 Default Browser Session 模型

#### 决策

复用 Sandbox 的 default session 设计逻辑：

- 不指定 session → 自动复用/创建 default browser（带登录态、cookies）
- 同一 workspace 下跨 thread 可以复用 workspace 级 browser state
- 不同 workspace 默认隔离 cookies / storage / profile 数据

#### 类比

| 概念 | Shell | Browser |
|------|-------|---------|
| Default session | 默认终端（保持 cwd、env） | 默认浏览器（保持 cookies、登录态） |
| New session | 新终端 tab | 无痕窗口 |
| Checkpoint 内容 | cwd + env + 文件系统 | cookies + storage + 当前 URL + tab 列表 |
| Restore | 恢复 env + cwd | 注入 cookies + storage |

#### 实现

在 worker 的 browser session 解析链上，browser 工具走同样的优先级：

1. Run-local default → 复用同一 run 内的 browser session
2. Thread-level binding → 复用 thread 绑定的 browser session
3. Workspace-level binding → 复用 workspace 绑定的 browser session
4. 创建新 session → 有 thread 时绑定 thread，否则绑定 workspace

`shell_sessions` 表通过 `session_type` 区分 shell 与 browser。browser tool 对外不暴露 `session_ref`，默认 session 的创建和恢复都由后端自动处理。

### 4.4 计费采用混合模式

#### 决策

浏览器操作的计费分两部分：

1. **Session 基础费**：创建 browser session 时收取固定积分
2. **超时累计费**：session 存活超过基础时长后，按分钟累计

#### 理由

浏览器操作不涉及 LLM token 消耗（LLM 调用独立计费），但占用真实计算资源（Chromium 进程 + 内存）。

纯按操作计费过于碎片化（每次 click 收费体验差）；纯按时长计费不够精确（idle session 也收费不合理）。混合模式兼顾两者。

#### 具体参数（建议值，通过 entitlement 配置）

| 计费项 | 默认值 | 配置键 |
|--------|--------|--------|
| Session 基础费 | 5 credits | `browser.session_open_base_fee` |
| 基础免费时长 | 3 min | `browser.session_free_duration_s` |
| 超时每分钟 | 2 credits/min | `browser.session_overtime_per_min` |
| 单次 session 上限 | 50 credits | `browser.session_max_credits` |

这些值通过 `shared/entitlement` 的多层解析机制配置（platform default → plan entitlement → org override）。

## 5. 总体架构

```text
Worker
  └─ browser tool executor
       └─ session orchestrator（复用现有 sandbox orchestrator）
            └─ Sandbox Service
                 └─ browser tier pool
                      └─ Docker container (arkloop-sandbox-browser)
                           ├─ agent-browser daemon (Node.js, 常驻)
                           ├─ Chromium (headless)
                           └─ session data (~/.agent-browser/)
```

### 5.1 Sandbox Browser Tier

browser tier 与 lite/pro tier 共享同一套管理逻辑：

- **Pool**：独立的 warm pool（`ready["browser"]`），独立 refiller goroutine
- **Session Manager**：相同的 idle timeout / lifetime timer 机制
- **Checkpoint**：相同的文件系统 tar.zst 快照，自动包含 agent-browser session 数据
- **Destroy**：相同的 `DestroyVM()` 流程

#### 资源配置

| 配置项 | 值 | 说明 |
|--------|------|------|
| `ARKLOOP_SANDBOX_WARM_POOL_BROWSER` | 1 | warm pool 数量 |
| `sandbox.idle_timeout_browser_s` | 120 | idle timeout（比 code 短，因为更吃资源） |
| `sandbox.max_lifetime_browser_s` | 600 | 最大存活时间 |
| Container memory limit | 512MB | Chromium idle ~150MB + 页面活动 ~200MB + buffer |
| Container CPU limit | 1.0 | 单核足够 headless 浏览 |

### 5.2 Docker 镜像设计

```dockerfile
# arkloop-sandbox-browser
FROM node:22-slim

# Chromium 依赖
RUN apt-get update && apt-get install -y \
    chromium \
    fonts-noto-cjk \
    fonts-noto-color-emoji \
    --no-install-recommends && \
    rm -rf /var/lib/apt/lists/*

# agent-browser
RUN npm install -g agent-browser && \
    agent-browser install

# Guest Agent（复用现有 sandbox guest agent）
COPY guest-agent /usr/local/bin/guest-agent

# 工作目录
WORKDIR /workspace

ENTRYPOINT ["guest-agent"]
```

关键点：
- 不含 Python / pip / 编译工具链 → 镜像更小，内存更少
- 包含 CJK 字体和 emoji 字体 → 中日韩页面正常渲染
- Guest Agent 复用现有 sandbox 的 guest agent → 统一执行命令接口

### 5.3 agent-browser 在容器内的生命周期

```text
容器启动
  → Guest Agent 启动
  → 等待 Worker 调用

首次 browser 命令到达
  → Guest Agent exec: "agent-browser navigate https://..."
  → agent-browser CLI 启动 daemon（如果未运行）
  → daemon 启动 Chromium headless
  → 执行命令并返回结果

后续命令
  → daemon 已常驻，直接执行（无冷启动）

Idle timeout 触发
  → Session Manager 回收
  → beforeDelete → checkpoint（tar.zst 包含 ~/.agent-browser/ session 数据）
  → DestroyVM → 容器销毁

下次使用（跨 run / thread）
  → 新容器从 pool 获取
  → restore checkpoint → 文件系统恢复（包含 cookies/storage）
  → agent-browser daemon 启动，读取已恢复的 session 数据
  → 继续使用
```

## 6. Worker 侧工具设计

### 6.1 工具语义

`browser` 是一个单命令入口工具，接受 agent-browser CLI 命令字符串。Worker 侧实现极其简单：解析命令 → 路由到 browser tier session → exec → 返回结果。

### 6.2 LLM Schema

```json
{
  "name": "browser",
  "description": "Execute browser automation commands. Opens and controls a headless browser for web navigation, interaction, and inspection.",
  "parameters": {
    "type": "object",
    "properties": {
      "command": {
        "type": "string",
        "description": "agent-browser CLI command to execute"
      },
      "yield_time_ms": {
        "type": "integer",
        "description": "Optional. Time to wait for navigation or rendering to settle before returning."
      }
    },
    "required": ["command"]
  }
}
```

### 6.3 命令速查（随工具定义下发到 system prompt）

```text
Browser Commands:
  navigate <url>             Open URL in browser
  snapshot                   Get page structure (accessibility tree with @ref IDs)
  screenshot                 Capture page screenshot
  click <ref>                Click element (e.g. @e3)
  type <ref> <text>          Type into input element
  fill <json>                Batch fill form fields: {"@e1":"val","@e2":"val"}
  select <ref> <value>       Select dropdown option
  scroll up|down|to <ref>    Scroll page
  press <key>                Press keyboard key (Enter, Escape, Tab, etc.)
  hover <ref>                Hover over element
  drag <from_ref> <to_ref>   Drag and drop
  upload <ref> <path>        Upload file to input
  dialog accept|dismiss      Handle alert/confirm/prompt dialog
  back / forward             Navigate history
  tab list                   List open tabs
  tab new <url>              Open new tab
  tab select <id>            Switch to tab
  tab close <id>             Close tab
  evaluate <js_code>         Execute JavaScript in page context
  console                    Get console messages
  network                    Get network request log
  cookie get [domain]        Get cookies
  cookie set <json>          Set cookies
  close                      Close browser session
```

### 6.4 返回结构

```json
{
  "status": "ok | error",
  "output": "command output (snapshot tree, console log, etc.)",
  "screenshot": "base64 PNG (only for screenshot command)",
  "error": "error message if status=error"
}
```

对于 `screenshot` 命令，返回中包含 base64 编码的 PNG 图片。前端通过 SSE 事件流中的 `tool_use_result` 事件接收并渲染。

### 6.5 Worker 侧实现

```go
// internal/tools/builtin/sandbox/executor.go

type BrowserExecutor struct {
    sandbox SandboxClient
    orch    *sessionOrchestrator
}

func (e *BrowserExecutor) Execute(ctx context.Context, params BrowserParams) (*Result, error) {
    // 1. 自动解析 browser session
    sessionRef, err := e.orch.resolveBrowserSession(ctx, params)
    if err != nil {
        return nil, err
    }

    // 2. 构造 CLI 命令
    cmd := fmt.Sprintf("agent-browser --session %s %s", sessionRef, params.Command)

    // 3. 调用 sandbox exec（tier=browser）
    resp, err := e.sandbox.Exec(ctx, sessionRef, cmd, "browser")
    if err != nil {
        return nil, err
    }

    // 4. 返回结果
    return &Result{
        Status:     resp.Status,
        Output:     resp.Stdout,
        Screenshot: extractScreenshotIfAny(resp),
        Error:      resp.Stderr,
    }, nil
}
```

### 6.6 触发方式

支持两种触发模式：

1. **LLM 自主决定**：LLM 在 agent loop 中根据任务需求自行调用 `browser` 工具。工具作为 persona 的 allowed tools 之一。

2. **用户显式触发**：用户在消息中发送 URL 或明确要求"打开浏览器"，LLM 据此调用 browser 工具。

两者在技术实现上没有区别——都是 LLM 在 agent loop 中决定调用 `browser` 工具。区别仅在 persona 的 system prompt 引导。

## 7. Session 管理

### 7.1 复用现有 share_scope 模型

Browser session 完全复用 sandbox 的 session 管理体系：

| Scope | 语义 | 行为 |
|-------|------|------|
| `run` | 同一 run 内复用 | run 结束后 cleanup |
| `thread` | 同一 thread 跨 run 复用 | 通过 checkpoint 恢复 |
| `workspace` | 同一 workspace 跨 thread 复用 | 通过 checkpoint 恢复 |
| `org` | 组织级共享 | 通过 checkpoint 恢复 |

### 7.2 数据库变更

`shell_sessions` 表新增字段：

```sql
ALTER TABLE shell_sessions ADD COLUMN session_type TEXT NOT NULL DEFAULT 'shell';
-- 值: 'shell' | 'browser'
```

`default_shell_session_bindings` 表同样增加：

```sql
ALTER TABLE default_shell_session_bindings ADD COLUMN session_type TEXT NOT NULL DEFAULT 'shell';
```

查询时通过 `session_type` 过滤，shell 和 browser 的 default binding 互不干扰。

### 7.3 Default Browser Session

和 shell 的 default session 类比：

```text
首次调用 browser 工具（未指定 session_ref）:
  1. resolveBrowserSession(sessionType=browser)
  2. 查 run-local default → 无
  3. 查 thread binding → 无
  4. 查 workspace binding → 无
  5. 有 thread 时创建 thread 级 browser session，否则创建 workspace 级 browser session
  6. 返回 sessionRef

后续调用（同一 run，未指定 session_ref）:
  1. resolveBrowserSession(sessionType=browser)
  2. 查 run-local default → 命中
  3. 复用同一 session

后续调用（跨 run，同一 thread）:
  1. 查 run-local default → 无
  2. 查 thread binding → 命中
  3. 复用 thread 级 browser session

后续调用（跨 thread，同一 workspace）:
  1. 查 run-local default → 无
  2. 查 thread binding → 无
  3. 查 workspace binding → 命中
  4. 复用 workspace 级 browser session
```

`browser` 对外不暴露 `session_ref` 或 `share_scope` 参数。默认 session 的创建、复用、等待、恢复全部由后端处理。

### 7.4 Checkpoint 与 Restore

#### Checkpoint（session 回收前自动执行）

Sandbox 现有的 checkpoint 机制会：
1. 将容器文件系统打包为 tar.zst
2. 记录 manifest（cwd, env vars, 元数据）
3. 上传到 S3

agent-browser 的 session 数据存储在 `~/.agent-browser/` 目录下，作为 `browser_state` scope 持久化，绑定到 `workspace_ref`，包含：
- cookies
- localStorage / sessionStorage
- 浏览器 profile 数据

这些数据在文件系统内，flush/checkpoint 时自动包含，不需要额外的导出逻辑。同一 workspace 会恢复同一份 browser state，不同 workspace 默认不共享登录态。

#### Restore（新 session 创建时，如有 checkpoint）

1. Sandbox 从 S3 下载 checkpoint 并解压到新容器
2. 文件系统恢复，包含 `~/.agent-browser/` 目录
3. agent-browser daemon 启动时自动读取已恢复的 session 数据
4. 浏览器打开时已包含之前的 cookies 和 storage

这比独立 Browser Service 的方案优雅得多——后者需要自建 cookie 导出/导入 API。

## 8. 用户体验

### 8.1 Phase 1：Screenshot 快照

每次浏览器操作后，Worker 自动执行 `agent-browser screenshot` 并将 base64 PNG 通过 SSE 事件流下发到前端。

#### 技术细节

- Viewport 固定为 1280x720，`deviceScaleFactor: 2`（生成 2560x1440 的清晰截图）
- 前端使用固定 16:9 比例容器渲染截图，`object-fit: contain`
- 截图作为 `tool_use_result` 事件的附件字段下发
- 不让截图自适应聊天气泡宽度，而是给一个固定比例的"浏览器窗口"容器

#### 截图时机

不是每个命令都截图。只在以下命令后自动截图：

- `navigate` / `back` / `forward`：页面变化
- `click`：可能触发导航或 UI 变化
- `type` + submit：表单提交后
- `scroll`：滚动后的新视图
- `tab select`：切换 tab 后

`snapshot`、`console`、`network`、`cookie get` 等纯数据命令不截图。

### 8.2 Phase 2（后续）：VNC 实时流

如果 Screenshot 无法满足需求（如需要 Take Over 接管能力），可升级为 VNC 实时流：

- 容器内加装 Xvfb + x11vnc + websockify
- 前端嵌入 noVNC 客户端
- 通过 WebSocket 实时推送浏览器画面

此方案工程量显著更大（WebSocket 路由、session 亲和性、安全隔离），不在 Phase 1 范围内。

## 9. 安全考虑

### 9.1 网络隔离

browser tier 容器的网络策略需要特别考虑：

- 代码执行的 sandbox（lite/pro）可能限制外部网络访问
- 浏览器 **必须** 能访问外部网络（否则无意义）
- browser tier 应连接到 `_egress` 网络（和 pro tier 一致）

### 9.2 资源限制

- 内存上限 512MB（防止 Chromium 内存泄漏拖垮宿主）
- 最大存活时间 600s（防止忘关的 session）
- 同一 org 并发 browser session 数量上限（通过 entitlement 配置）

### 9.3 敏感数据

- Checkpoint 中可能包含用户在浏览器中输入的密码、Cookie 等敏感信息
- 需确保 S3 checkpoint 路径严格按 org 隔离
- Checkpoint 数据应有 TTL 自动清理策略（复用 `sandbox-session-state` bucket 的现有策略）

### 9.4 恶意网站防护

- Chromium 运行在容器沙箱内，即使遇到恶意网站也不会影响宿主
- 可选：配置 Chromium 安全策略禁止 file:// 协议、限制下载路径

## 10. 计费实现

### 10.1 计费触发点

```text
创建 browser session:
  → 扣除 session_open_base_fee (5 credits)
  → 记录 session_start_time

session 回收时:
  → 计算存活时长
  → 超过 free_duration 的部分按 overtime_per_min 计费
  → 累计不超过 session_max_credits
  → 写入 usage_records
```

### 10.2 数据表扩展

复用现有 `usage_records` 表，新增 usage_type：

```sql
-- usage_records 现有结构已支持扩展
-- 新增 usage_type = 'browser_session'
INSERT INTO usage_records (org_id, run_id, usage_type, cost_usd, metadata)
VALUES (?, ?, 'browser_session', ?, '{"duration_s": 180, "credits": 7}');
```

### 10.3 Entitlement 配置键

| 键 | 默认值 | 说明 |
|----|--------|------|
| `browser.enabled` | false | 是否启用浏览器能力 |
| `browser.session_open_base_fee` | 5 | 开启 session 基础费 |
| `browser.session_free_duration_s` | 180 | 免费时长（秒） |
| `browser.session_overtime_per_min` | 2 | 超时每分钟积分 |
| `browser.session_max_credits` | 50 | 单次 session 积分上限 |
| `browser.max_concurrent_sessions` | 2 | 同一 org 并发 session 上限 |

## 11. 可观测性

### 11.1 结构化日志

```json
{
  "service": "worker",
  "tool": "browser",
  "command": "navigate",
  "session_ref": "brw_abc123",
  "org_id": "org_xyz",
  "run_id": "run_456",
  "duration_ms": 2340,
  "status": "ok"
}
```

### 11.2 Metrics

| 指标 | 类型 | 说明 |
|------|------|------|
| `browser.session.active` | Gauge | 当前活跃 browser session 数 |
| `browser.session.created_total` | Counter | 创建的 session 总数 |
| `browser.session.reclaimed_total` | Counter | 回收的 session 总数 |
| `browser.command.duration_ms` | Histogram | 命令执行耗时 |
| `browser.pool.ready` | Gauge | warm pool 可用容器数 |

## 12. Landing Roadmap

按 PR 拆分，每个 PR 可独立合并和部署。

### PR 1: Sandbox Browser Tier 基础设施

**范围**：让 browser tier 容器能跑起来。

- 新增 `Dockerfile.browser`（Node + Chromium + agent-browser + Guest Agent）
- Sandbox pool 配置新增 browser tier（config 项、refiller、warm pool）
- Sandbox session manager 支持 browser tier 的 idle timeout / lifetime
- `compose.yaml` 新增 browser tier 构建配置
- 验证：手动在 browser tier 容器内执行 `agent-browser navigate` 成功

**涉及目录**：
- `src/services/sandbox/`
- `docker/` 或 `src/services/sandbox/Dockerfile.browser`

### PR 2: Worker Browser Tool 核心

**范围**：Worker 能调用 browser 工具。

- Worker 新增 `browser` tool executor（`internal/tools/builtin/sandbox/`）
- 复用 session orchestrator 添加 browser session 解析逻辑
- `shell_sessions` 表 migration：新增 `session_type` 字段
- `default_shell_session_bindings` migration：新增 `session_type` 字段
- 工具注册到 `DispatchingExecutor`
- 验证：通过 API 创建 run，LLM 调用 browser 工具成功打开网页

**涉及目录**：
- `src/services/worker/internal/tools/builtin/sandbox/`
- `src/services/worker/internal/tools/`（executor 注册）
- `src/services/api/internal/migrate/`（migration）

### PR 3: Screenshot 与前端展示

**范围**：用户能看到浏览器操作结果。

- Worker 在特定命令后自动追加 screenshot
- SSE 事件中 `tool_use_result` 携带 screenshot 字段
- Web 前端 chat 界面渲染浏览器截图（固定 16:9 容器）
- Console 管理后台可查看 browser session 状态

**涉及目录**：
- `src/services/worker/internal/pipeline/`（SSE 事件扩展）
- `src/apps/web/`（前端截图渲染组件）
- `src/apps/console/`（session 管理页面）

### PR 4: Session 持久化与 Checkpoint

**范围**：浏览器状态可跨 run/thread 恢复。

- browser session 的 checkpoint 验证（确认 agent-browser session 数据被正确归档）
- restore 后 cookies/storage 自动恢复验证
- thread-level / workspace-level binding 支持 browser session
- Default browser session 逻辑（`run -> thread -> workspace -> create`）
- browser_state 改为 workspace 级持久化，跨 workspace 默认隔离

**涉及目录**：
- `src/services/sandbox/internal/shell/`（checkpoint 兼容验证）
- `src/services/sandbox/internal/environment/`（browser_state flush/hydrate）
- `src/services/worker/internal/tools/builtin/sandbox/`（session 绑定逻辑）

### PR 5: 计费与 Entitlement

**范围**：浏览器使用正确计费。

- Browser session 计费逻辑（基础费 + 超时）
- Entitlement 配置键注册
- usage_records 扩展
- 并发 session 限制

**涉及目录**：
- `src/services/shared/creditpolicy/`
- `src/services/shared/entitlement/`
- `src/services/worker/internal/pipeline/`（计费写入）
- `src/services/api/internal/migrate/`（entitlement 默认值）

### PR 6: 完整功能与打磨

**范围**：补齐边缘场景，准备上线。

- 多 Tab 管理验证
- 开发者工具命令验证（evaluate, console, network）
- Cookie 管理命令验证
- 错误处理与重试
- 可观测性（日志、metrics）
- 文档更新（API docs, persona 模板更新）

**涉及目录**：
- 跨多个目录的集成测试和打磨

## 13. 后续演进方向

以下功能不在当前范围内，但作为后续演进方向记录：

### 13.1 Tool Discovery / 动态工具加载

全局优化，不仅服务 browser，也服务所有现有工具：

- Agent 初始只看到工具标题（几十个加起来几百字符）
- 需要使用时，通过 `tool_search` 加载完整 description
- 减少 system prompt token 消耗

### 13.2 VNC 实时流

- noVNC 嵌入前端
- Take Over 接管能力
- WebSocket 基础设施

### 13.3 浏览器扩展支持

- 允许安装 Chrome 扩展
- 用户自定义 browser profile

### 13.4 Browser + Sandbox 联动

- Agent 可以在同一容器内同时使用代码执行和浏览器
- 需要评估是否值得做"browser + code" 的混合 tier
