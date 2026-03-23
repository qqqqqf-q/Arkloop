<p align="center">
  <img src="https://cdn.nodeimage.com/i/WEaHFl5O8ZuWtaXykH4mOvJHxu8R3543.png" alt="Arkloop" />
</p>

<h3 align="center">干净、强大、属于你的 AI Agent 平台</h3>

<p align="center">
  <a href="../../README.md"><img alt="English" src="https://img.shields.io/badge/English-d9d9d9"></a>
  <a href="../../LICENSE"><img alt="License" src="https://img.shields.io/badge/license-Arkloop%20License-blue"></a>
  <a href="https://github.com/qqqqqf/Arkloop/graphs/commit-activity"><img alt="Commits" src="https://img.shields.io/github/commit-activity/m/qqqqqf/Arkloop?labelColor=%2332b583&color=%2312b76a"></a>
  <a href="https://github.com/qqqqqf/Arkloop/issues"><img alt="Issues closed" src="https://img.shields.io/github/issues-search?query=repo%3Aqqqqqf%2FArkloop%20is%3Aclosed&label=issues%20closed&labelColor=%237d89b0&color=%235d6b98"></a>
  <a href="https://twitter.com/intent/follow?screen_name=qqqqqf_"><img alt="Follow on X" src="https://img.shields.io/twitter/follow/qqqqqf_?logo=X&color=%20%23f5f5f5"></a>
</p>

---

Arkloop 是一个注重设计的开源 AI 智能体平台。多模型路由、沙箱执行、持久记忆 -- 所有能力都在一个干净的界面背后，不会糊你一脸。

提供**桌面应用**（macOS / Linux / Windows）和自托管服务器两种使用方式。

## 下载

从 [GitHub Releases](https://github.com/qqqqqf/Arkloop/releases) 下载最新版本。

桌面应用内置完整运行环境 -- 无需 Docker，无需配置，打开即用。

## 自托管部署

> 系统要求：Docker、Docker Compose、Python 3，2+ CPU 核心，4+ GiB 内存。

```bash
git clone https://github.com/qqqqqf/Arkloop.git
cd Arkloop
./setup.sh install
```

生产环境使用预构建镜像：

```bash
./setup.sh install --prod --non-interactive ...
```

完整配置选项参见[安装指南](../installation.md)。

## 功能

**桌面应用** -- 基于 Electron + Go Sidecar 的原生应用，完全本地运行，通过 GitHub Releases 自动更新。

**多模型路由** -- 集成 OpenAI、Anthropic 及任何 OpenAI 兼容接口。基于优先级的路由，自动处理限流与提供商级缓存。

**沙箱代码执行** -- Firecracker 微虚拟机（Linux）或 Docker 容器（macOS/Windows）。支持 Python、数据分析、图表生成，严格资源限制。

**持久记忆** -- 系统级约束、长期事实和会话上下文在对话间持久保留，由 OpenViking 向量记忆驱动。

**Prompt 注入防护** -- 语义级扫描，检测并拦截注入攻击。大多数同类产品不做的功能。

**渠道接入** -- 将智能体接入 Telegram，支持完整的媒体处理、群组上下文和速率限制。

**ACP 集成** -- Agent Communication Protocol 支持，在沙箱环境中实现智能体间协调。

**MCP 支持** -- Model Context Protocol 配置，通过外部工具扩展智能体能力。

**自定义 Persona** -- 定义专业化的智能体配置，包含独立的系统提示词、工具集和行为层级。支持 Lua 脚本自定义 Agent Loop。

**技能生态** -- 从 ClawHub 搜索与导入技能，兼容 OpenClaw `SKILL.md` 格式。导入时同步上游安全扫描状态。

**管理控制台** -- 用户管理、Persona 配置、LLM 凭证管理、用量分析、审计日志和功能开关。

## 架构

| 服务 | 技术栈 | 职责 |
|------|--------|------|
| API | Go | 认证、RBAC、资源管理、审计日志 |
| Gateway | Go | 反向代理、速率限制、风控评分、Geo-IP |
| Worker | Go | 任务执行、LLM 路由、工具调度、Agent Loop |
| Sandbox | Go | Firecracker 微虚拟机或 Docker 容器中的代码执行 |
| Desktop | Electron + Go | 原生桌面应用，内嵌 Sidecar |
| Web | React / TypeScript | 用户聊天界面 |
| Console | React / TypeScript | 管理仪表板 |

基础设施：PostgreSQL + PgBouncer、Redis、SeaweedFS（S3 兼容存储）或 filesystem（默认）、OpenViking（向量记忆）。

## 开发

```bash
# 快速本地 CI 检查
bin/ci-local quick

# Go 集成测试
bin/ci-local integration

# 完整检查
bin/ci-local full
```

提交规范和开发流程参见 [CONTRIBUTING.md](../../CONTRIBUTING.md)。

## 贡献

我们欢迎所有形式的贡献。

即使你不是开发者，只是一个普通用户 -- 如果你在使用中感到任何不舒服的地方，哪怕只是一点间距、一个颜色、一个很小很小的细节，或者是一个很大很大的方向，都可以直接[开一个 issue](https://github.com/qqqqqf/Arkloop/issues)。我们认真对待每一个体验细节，你的反馈会让所有人的体验变得更好。

## 贡献者

<a href="https://github.com/qqqqqf/Arkloop/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=qqqqqf/Arkloop" />
</a>

## 安全

报告安全漏洞请发送邮件至 qingf622@outlook.com，而非公开 Issue。详情见 [SECURITY.md](../../SECURITY.md)。

## 许可证

基于 [Arkloop License](../../LICENSE)（修改版 Apache License 2.0），附加条件：

- **多租户限制**：未经书面授权，不得使用源码运营多租户 SaaS。
- **品牌保护**：不得移除或修改前端组件中的 LOGO 和版权信息。
