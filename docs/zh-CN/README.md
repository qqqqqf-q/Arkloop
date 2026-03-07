<p align="center">
  <img src="https://img.shields.io/badge/Arkloop-AI%20Agent%20Platform-000000?style=for-the-badge&labelColor=000000" alt="Arkloop" />
</p>

<p align="center">
  <a href="https://arkloop.ai">Arkloop Cloud</a> &middot;
  <a href="#self-hosting">Self-hosting</a> &middot;
  <a href="https://docs.arkloop.ai">Documentation</a>
</p>

<p align="center">
  <a href="../../LICENSE"><img alt="License" src="https://img.shields.io/badge/license-Arkloop%20License-blue"></a>
  <a href="https://github.com/qqqqqf/Arkloop/graphs/commit-activity"><img alt="Commits" src="https://img.shields.io/github/commit-activity/m/qqqqqf/Arkloop?labelColor=%2332b583&color=%2312b76a"></a>
  <a href="https://github.com/qqqqqf/Arkloop/issues"><img alt="Issues" src="https://img.shields.io/github/issues-search?query=repo%3Aqqqqqf%2FArkloop%20is%3Aclosed&label=issues%20closed&labelColor=%237d89b0&color=%235d6b98"></a>
  <a href="https://twitter.com/intent/follow?screen_name=qqqqqf_"><img alt="Follow on X" src="https://img.shields.io/twitter/follow/qqqqqf_?logo=X&color=%20%23f5f5f5"></a>
</p>

<p align="center">
  <a href="../../README.md"><img alt="English" src="https://img.shields.io/badge/English-d9d9d9"></a>
  <a href="./README.md"><img alt="简体中文" src="https://img.shields.io/badge/简体中文-blue"></a>
</p>

Arkloop 是一个开源 AI 智能体平台，将自主任务执行、实时智能搜索和安全沙箱工作空间融合为一体。并且接入了 Memory 等功能来实现情感支持。它结合了 Manus 风格的自主代理、Perplexity 级别的搜索综合能力，以及云原生基础设施。

## 快速开始

### Arkloop Cloud

最快的上手方式 -- 零配置，全托管。

[立即体验 Arkloop Cloud](https://arkloop.cn)

### 自托管部署

> 系统要求：已安装 Docker 和 Docker Compose，2+ CPU 核心，4+ GiB 内存。

```bash
git clone https://github.com/qqqqqf/Arkloop.git
cd Arkloop
cp .env.example .env
# 编辑 .env -- 设置密码、API Key 和 LLM 凭证
docker compose up -d
```

所有服务就绪后，通过 `http://localhost:8000` 访问 Web 界面。

如果需要宿主机调试端口，例如 PostgreSQL、API、Browser、Sandbox 或 OpenViking，请显式叠加开发覆盖文件：

```bash
docker compose -f compose.yaml -f compose.dev.yaml up -d
```

详细的配置、环境变量和生产部署指南请参考[文档](https://docs.arkloop.ai)。

## 核心功能

**1. Agent Loop**
自主多步骤执行，集成规划、推理和工具编排。智能体在对话间维护持久记忆 -- 系统级约束、长期事实和会话上下文。

**2. 智能搜索**
深度网络搜索，将多个来源综合为带引用的结构化回答。不是搜索 API 的简单封装 -- 它阅读、推理、回应。

**3. 沙箱代码执行**
基于 Firecracker 微虚拟机或 Docker 容器的隔离执行环境。支持 Python、数据分析、图表生成和文件操作，具有严格的资源限制。

**4. 浏览器自动化**
无头浏览器控制，作为原生智能体工具集成。通过 Playwright 实现网页交互、数据提取和截图抓取。

**5. 自定义 Persona**
定义专业化的智能体配置，包含独立的系统提示词、工具集和行为层级。在通用、研究和领域专用模式间切换。

**6. 多模型支持**
集成 OpenAI、Anthropic 以及任何 OpenAI 兼容提供商。智能重试、速率限制处理和提供商级别的响应缓存。

**7. 企业管理控制台**
管理仪表板，用于用户管理、Persona 配置、LLM 凭证管理、用量分析、审计日志和功能开关。

## 架构

| 服务 | 技术栈 | 职责 |
|------|--------|------|
| API | Go | 认证、资源管理、RBAC、审计日志 |
| Gateway | Go | 反向代理、速率限制、风控评分、Geo-IP |
| Worker | Go | 任务执行、LLM 路由、工具调度、Persona 管理 |
| Sandbox | Go | Firecracker 微虚拟机或 Docker 容器中的代码执行 |
| Browser | Node.js | 基于 Playwright 的无头浏览器自动化 |
| Web | React / TypeScript | 面向用户的聊天界面 |
| Console | React / TypeScript | 平台管理仪表板 |

基础设施：PostgreSQL + PgBouncer、Redis、MinIO（S3 兼容存储）、OpenViking（向量记忆）。

## Star

如果你觉得 Arkloop 有用，给个 Star 吧 -- 帮助更多人发现这个项目。

<!-- Star GIF will be added here -->

## 贡献

欢迎贡献。查看 [CONTRIBUTING.md](./CONTRIBUTING.md) 了解参与方式。

## 贡献者

<a href="https://github.com/qqqqqf/Arkloop/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=qqqqqf/Arkloop" />
</a>

## 安全

报告安全漏洞请发送邮件至 security@arkloop.cn，而非公开 Issue。详情见 [SECURITY.md](../../SECURITY.md)。

## 许可证

基于 [Arkloop License](../../LICENSE)（修改版 Apache License 2.0），附加条件：

- **多租户限制**：未经书面授权，不得使用源码运营多租户 SaaS。
- **品牌保护**：不得移除或修改前端组件中的 LOGO 和版权信息。
