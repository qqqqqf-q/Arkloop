# 产品概览

## Arkloop 是什么

Arkloop 是一个**个人开源项目**：**本地优先**的对话式 AI Agent 平台。整个后端是一个**单进程嵌入式 Go 运行时**（API + Worker 以库形式内嵌），存储是本地 **SQLite** 数据库加文件系统——没有 Postgres、Redis 或消息队列等外部基础设施。

产品形态有两种，共用同一套运行时：

- **Desktop**：Electron 桌面应用（`src/apps/desktop`），内嵌 Go 运行时与 Web 界面，开箱即用。
- **Headless CLI**：`ark` 命令行（`src/services/cli/cmd/ark`），`ark web` 不开桌面窗口即可启动同一套本地运行时，对外提供 Web 界面和本地 API。

仓库为单体仓：Go 服务在 `src/services/`，前端在 `src/apps/`，Persona 模板在 `src/personas/`。

## 成熟阶段与预期

当前为 **Early Access / Alpha**：版本可能不稳定，存在 bug、数据风险、API 变更或未完成功能。精力优先桌面端稳定，只修阻塞性问题。

## 分发与文档入口

- **桌面客户端**：GitHub Releases，支持 macOS / Linux / Windows；安装包内含完整运行时，无需额外配置；更新走 Releases 通道。
- **CLI**：桌面端首启可安装 `ark`；也可经 Homebrew（`brew install qqqqqf-q/arkloop/arkloop`）或 AUR（`arkloop-bin` / `arkloop-git`）安装。
- **本帮助**（`arkloop_help`）：与当前 **Worker/Desktop 二进制同版本** 打包的**精简事实**，回答「Arkloop 是什么、架构如何、桌面怎么配」时应**优先查本工具**，勿凭模型记忆杜撰技术细节。

## 源码与维护者社交

- **GitHub 仓库**：https://github.com/qqqqqf-q/Arkloop
- **维护者 X（Twitter）**：https://x.com/qqqqqf_

用户问「仓库在哪 / 作者推特」时，若已调用 `arkloop_help`，可从本节引用。

## 核心能力（与 README 对齐）

1. **多模型路由**：OpenAI、Anthropic、Gemini 及任何 OpenAI 兼容接口；用户自带密钥（BYOK），按优先级自动路由。
2. **Agent 运行时**：内建工具（文件、Shell、Web 搜索/抓取、代码执行等）+ **MCP** 服务器 + **ClawHub Skills**（兼容 OpenClaw `SKILL.md`）；支持子 Agent 派生与定时任务。
3. **记忆**：默认 **Notebook** 纯文本笔记；可选 **Nowledge** 语义记忆（外部服务）；也可整体关闭。两个子系统相互独立（见 `architecture` 文档记忆小节）。
4. **渠道**：**Telegram、Discord、QQ、飞书、微信** 机器人接入，与 Web 共用同一条 Agent 管道；支持定时 heartbeat 运行（详见 `channels_telegram` 文档）。
5. **Persona**：每个 Agent 可独立配置系统提示、工具白名单、预算与执行器类型（`agent.simple` / `agent.interactive` / `task.classify_route`）。

## 内置 Persona 类型（仓库模板）

以下对应 `src/personas/*/persona.yaml`，执行器均为 `agent.simple`。

| id | 用途摘要 |
|----|----------|
| `normal` | 通用对话，含多种工具；`core_tools` 含 **`arkloop_help`**，便于答复产品概念时拉取本知识库；模板默认启用 heartbeat |
| `work` | 多步任务、偏自主的执行风格 |
| `platform` | 平台管理专用，处理配置、Agent 创建等管理操作 |
| `summarizer` | 系统内置摘要（标题/结果摘要） |
| `impression-builder` | 系统级记忆画像构建 |
| `suggestion-builder` | 系统级建议生成 |
| `activity-recorder-builder` | 系统级活动记录整理，写入 Memory |
| `sticker-builder` | 系统级 Telegram sticker 描述生成 |
| `extended-search` / `search-output` | 联网搜索与搜索输出整理 |

## 与普通「聊天应用」的差异（概念）

- Arkloop 提供 **可配置 Persona**、**工具白名单**、**中间件流水线**、**Skills/MCP 扩展**，而不是单一固定聊天接口。
- **单用户**：桌面/单机运行时使用固定种子用户，local-only 认证（desktop token 自动注入 + 可选本机密码）；没有注册、邀请、团队等多账户生命周期。
- 渠道场景下记忆归属 **bot owner**：群友不需要 Arkloop 账号，持久化记忆数据归 bot owner 所有（见 `channels_telegram` 文档）。
