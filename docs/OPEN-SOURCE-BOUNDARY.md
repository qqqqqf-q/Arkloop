# Open Source Boundary

本文档定义 Arkloop 仓库的开源边界：哪些属于 OSS core，哪些是配置模板，哪些不应出现在公开仓库中。

## 目录分类

### OSS Core（全部公开）

| 路径 | 说明 |
|------|------|
| `src/services/api/` | API 服务 |
| `src/services/gateway/` | Gateway 反向代理 |
| `src/services/worker/` | Worker 执行引擎 |
| `src/services/sandbox/` | Sandbox 沙箱服务 |
| `src/services/browser/` | Browser 浏览器服务 |
| `src/services/shared/` | Go 共享库 |
| `src/apps/web/` | Web 前端（品牌保护见 LICENSE） |
| `src/apps/console/` | Console 管理后台（品牌保护见 LICENSE） |
| `src/apps/cli/` | CLI 参考客户端 |
| `src/apps/shared/` | 前端共享包 |
| `src/personas/` | Persona 模板 |
| `src/docs/` | 技术文档（VitePress） |
| `tests/` | 测试（含压力测试） |
| `config/sandbox/templates.json` | Sandbox 模板定义 |
| `config/openviking/ov.conf.example` | OpenViking 配置模板 |
| `compose.yaml` | Docker Compose 编排 |
| `compose.bench.yaml` | 压力测试编排 |
| `.github/workflows/` | CI 流水线 |
| `README.md` | 项目说明 |
| `CONTRIBUTING.md` | 贡献指南 |
| `CODE_OF_CONDUCT.md` | 行为准则 |
| `SECURITY.md` | 安全披露政策 |

### 配置模板（公开，不含真实值）

| 路径 | 说明 |
|------|------|
| `.env.example` | 环境变量模板（所有值为占位符） |
| `.env.test.example` | 测试环境变量模板 |
| `config/openviking/ov.conf.example` | OpenViking 配置模板 |

### 排除项（通过 .gitignore 或开源前清理）

| 路径 | 原因 | 处置 |
|------|------|------|
| `.env` / `.env.*`（非 example） | 真实密钥 | .gitignore |
| `config/openviking/ov.conf` | 真实 API Key | .gitignore |
| `.claude` / `CLAUDE.md` | AI IDE 私有配置 | .gitignore |
| `review.md` | AI 审阅规范（内部工具链） | .gitignore |
| `temp/` | 临时文件 | .gitignore |
| `.VSCodeCounter/` | 代码统计缓存 | .gitignore |
| `src/docs/.vitepress/dist/` | 构建产物 | .gitignore |
| `node_modules/` | 依赖 | .gitignore |

## 开源前清理检查项

- [x] git 历史中无真实 API Key / Token / 密码泄露
- [x] `.env` 文件均在 `.gitignore` 中
- [x] `config/openviking/ov.conf`（含 root_api_key）在 `.gitignore` 中
- [x] 无内网域名、私有镜像地址硬编码
- [x] 无个人本地路径硬编码（已清理 `/Users/qqqqqf/` 引用）
- [x] 文档 "内部" 标识已改为对外语境
- [x] `.dockerignore` 已创建，防止构建时泄露 `.env` / `.git/`
- [x] 商标使用规则（已在 CONTRIBUTING.md 中说明）

## 许可证边界

主许可证为 Arkloop License（modified Apache 2.0），附加条款：

1. **多租户限制**：不得用源码运营多租户 SaaS（一个 Organization = 一个 tenant）
2. **品牌保护**：不得移除 `src/apps/web/` 和 `src/apps/console/` 中的 LOGO 和版权信息

详见根目录 `LICENSE` 文件。
