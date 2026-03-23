# 贡献指南

感谢你考虑为 Arkloop 做出贡献。本文档介绍贡献的流程和规范。

## 准备工作

### 前置条件

- Go 1.26+
- Node.js 20+，使用 pnpm
- Docker 和 Docker Compose
- PostgreSQL 16+（或使用 `docker compose up postgres`）

### 本地开发环境

```bash
git clone https://github.com/qqqqqf/Arkloop.git
cd Arkloop

# 启动最小基础设施
docker compose up -d postgres redis

# 可选：性能层（redis_gateway：可选 Gateway 热路径缓存，非默认配置）
docker compose --profile performance up -d pgbouncer redis_gateway

# 可选：S3 兼容对象存储
docker compose --profile s3 up -d seaweedfs

# 复制并配置环境变量
cp .env.example .env
# 编辑 .env，填入本地配置

# 后端（Go 服务）
cd src/services/api && go run . &
cd src/services/gateway && go run . &
cd src/services/worker && go run .

# 前端
cd src/apps/web && pnpm install && pnpm dev
cd src/apps/console && pnpm install && pnpm dev
```

### 项目结构

```
src/
  apps/
    web/          # 用户聊天界面（React）
    console/      # 管理仪表板（React）
    cli/          # CLI 参考客户端
    shared/       # 前端共享包
  services/
    api/          # 核心 REST API（Go）
    gateway/      # 反向代理（Go）
    worker/       # 任务执行引擎（Go）
    sandbox/      # 代码执行沙箱（Go）
    shared/       # Go 共享库
  personas/       # 智能体 Persona 模板
  docs/           # 技术文档（VitePress）
```

## 如何贡献

### 报告 Bug

在 [GitHub Issues](https://github.com/qqqqqf/Arkloop/issues) 提交 Issue，包含：

- 复现步骤
- 预期行为与实际行为
- 环境信息（操作系统、Docker 版本、浏览器）

### 功能建议

提交 Discussion 或 Issue，描述使用场景和提议的解决方案。比起抽象的功能请求，我们更倾向于具体的问题描述。

### 提交代码

1. Fork 仓库，从 `main` 创建功能分支。
2. 按照下方的代码规范进行修改。
3. 为你的修改编写或更新测试。
4. 运行 lint 和测试，确保没有破坏已有功能。
5. 提交 Pull Request，附上清晰的描述。

### 代码规范

**提交信息**

格式：

```
<type>(<scope>): <subject>
```

- 类型：`feat`、`fix`、`docs`、`refactor`、`test`、`build`、`ci`、`chore`
- 使用祈使句，首字母小写，结尾不加句号
- 一个提交只做一件事

**Go**

- 遵循标准 Go 规范和项目 lint 规则
- 保持函数简短、职责单一
- 显式处理所有错误

**TypeScript / React**

- 使用 TypeScript 严格模式
- 遵循现有的 Tailwind CSS 模式
- Lint：项目使用 ESLint 和 Prettier

**Python（Worker 内部）**

- 遵循 `pyproject.toml` 中定义的 Ruff 规则

### 运行测试

```bash
# 快速 CI 自检
bin/ci-local quick

# Go 集成测试
bin/ci-local integration

# 完整本地 CI
bin/ci-local full

# 模拟 GitHub Actions
bin/ci-local act go-check
bin/ci-local act typescript

# Go 单元测试
cd src/services/api && go test ./...
cd src/services/worker && go test ./...
cd src/services/gateway && go test ./...

# 前端测试
cd src/apps/web && pnpm test
cd src/apps/console && pnpm test

# 集成 / 冒烟测试
cd tests/smoke && go test ./...
```

日常推荐顺序：`bin/ci-local quick` -> `bin/ci-local integration` -> `bin/ci-local act <job>`。
`quick` 适合提交前自检，`integration` 适合数据库、repo、worker pipeline、webhook、runengine 一类改动，`act` 用来做接近 GitHub Actions 的补充验证。
`quick` 会自动安装前端依赖，因此首次运行会更慢。
当前不建议使用 `bin/ci-local act go-integration`，本地集成检查优先使用 `bin/ci-local integration`。

## 商标使用

Arkloop 名称、Logo 和品牌资产是 The Arkloop Authors 的商标。

- 你可以使用 Arkloop 名称来准确描述你与该项目的关系（例如"基于 Arkloop 构建"、"兼容 Arkloop"）。
- 未经书面许可，不得以暗示官方认可或隶属关系的方式使用 Arkloop 名称、Logo 或品牌资产。
- 如 [LICENSE](../../LICENSE) 所述，前端组件（`src/apps/web/` 和 `src/apps/console/`）必须保留原始 LOGO 和版权信息。

## 贡献者许可

提交贡献即表示你同意：

1. 项目维护者可以按照 [LICENSE](../../LICENSE) 中的规定调整开源许可条款。
2. 你贡献的代码可用于商业目的，包括云服务运营。

这些条款详见 Arkloop License 第 2 节。

## 问题

如有贡献相关疑问，请在 GitHub 上发起 Discussion 或联系维护者。
