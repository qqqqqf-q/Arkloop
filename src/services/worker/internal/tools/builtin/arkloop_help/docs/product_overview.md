# 产品概览

## Arkloop 是什么

Arkloop 是**开源**、面向**对话式 AI Agent** 的平台：托管运行时，集成 **工具执行**、**记忆**、**沙箱代码执行**、**多模型路由**。仓库为单体仓：Go 服务在 `src/services/`，前端在 `src/apps/`，Persona 模板在 `src/personas/`，默认编排见根目录 `compose.yaml`。

对外表述强调「设计取向」：**多模型、工具调用、代码执行、持久记忆**等能力与市面 AI 对话工具有重叠，但侧重**做得干净**；桌面端**开箱即用**（官方安装包捆绑运行时，无需自建 Docker 即可聊天）。

## 成熟阶段与预期

当前为 **Early Access / Alpha**：版本可能不稳定，存在 bug、数据风险、API 变更或未完成功能。自托管路径仍在演进中，Alpha 阶段**不保证**自建服务器场景的完整可用性；精力优先桌面稳定后，再完善服务端部署文档与体验。

## 分发与文档入口

- **桌面客户端**：GitHub Releases，支持 macOS / Linux / Windows；安装包内含完整运行时，官方描述为无需 Docker、无需额外配置即可使用；更新可走 Releases 通道。
- **在线文档（人类可读）**：英文指南 `https://arkloop.io/en/docs/guide`，中文 `https://arkloop.io/zh/docs/guide`（与 README 一致）。
- **本帮助**（`arkloop_help`）：与当前 **Worker/Desktop 二进制同版本** 打包的**精简事实**，回答「Arkloop 是什么、架构如何、桌面怎么配」时应**优先查本工具**，勿凭模型记忆杜撰技术细节。

## 源码与维护者社交

- **GitHub 仓库**：https://github.com/qqqqqf-q/Arkloop
- **维护者 X（Twitter）**：https://x.com/qqqqqf_

用户问「仓库在哪 / 作者推特」时，若已调用 `arkloop_help`，可从本节引用。

## 核心能力（与 README 对齐）

1. **多模型路由**：OpenAI、Anthropic 及兼容 API；支持按优先级自动路由与限流等策略（细节以控制台/路由配置为准）。
2. **沙箱执行**：代码隔离；生产常见 **Firecracker**（Linux），开发/桌面环境常见 **Docker** 容器。
3. **持久记忆**：长期事实与约束跨会话保留；实现上分 **OpenViking 语义记忆** 与 **Notebook 结构化笔记** 两个子系统（见内嵌 `architecture` 文档记忆小节）。
4. **Prompt 注入防护**：语义级扫描等安全约束（具体策略以部署配置为准）。
5. **渠道**：如 **Telegram** 等，消息进入与 Web 类似的 Worker 管道（详见 `channels_telegram` 文档）。
6. **Persona**：每个 Agent 可独立配置系统提示、工具白名单、预算等；支持 **Lua**（`agent.lua`）自定义循环逻辑。
7. **MCP**：Model Context Protocol 工具扩展支持。
8. **Skill**：可从 ClawHub 等导入，兼容 OpenClaw `SKILL.md` 格式。

## 内置 Persona 类型（仓库模板）

以下对应 `src/personas/*/persona.yaml`，实际线上可与数据库定义合并。

| id | 用途摘要 |
|----|----------|
| `normal` | 通用对话，含多种工具；默认配置中含 `heartbeat`（群聊定时心跳相关能力，详见架构文档） |
| `work` | 多步任务、偏自主的执行风格 |
| `platform` | 平台管理专用，`tool_allowlist` 仅 `platform_manage` |
| `summarizer` | 系统内置摘要（标题/结果摘要） |
| `impression-builder` | 系统级记忆画像构建 |
| `extended-search` / `search-output` | 联网搜索与搜索输出整理（可与 Lua executor 配合） |

另：`normal` 模板含 **`arkloop_help`** 于 `core_tools`，便于在答复产品概念时拉取本知识库。

## 与普通「聊天应用」的差异（概念）

- Arkloop 提供 **可配置 Persona**、**工具白名单**、**中间件流水线**、**Skills/MCP 扩展**，而不是单一固定聊天接口。
- **同用户下多 Persona 共享记忆归属**（绑定在 bot owner / User 维度），切换 Persona 不切换「谁的记忆」（频道场景见 Telegram 文档）。
