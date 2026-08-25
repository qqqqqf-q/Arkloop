# 本地开发指南（仓库工作流）

> 本文面向从源码构建与贡献的开发者。Arkloop 是单进程嵌入式应用，**没有需要先行部署的服务端基础设施**；以下为本地开发的实际路径。

## 环境前置

- **Go**：1.26+（仓库根 `go.work` 统一管理 `src/services/` 下各模块）
- **Node**：20+，包管理 **pnpm**（仓库根 `pnpm install`）
- **Docker Compose**：仅用于可选模块（sandbox / SearXNG / Firecrawl），不是运行主程序的前提
- `.env`：从 **`.env.example`** 复制，按需填写；桌面/单机默认配置通常开箱即用（SQLite 自动迁移）

## 从源码运行（两种路径）

### 路径一：Desktop 应用开发

```bash
pnpm install                  # 仓库根目录
cd src/apps/desktop && pnpm dev
```

启动 Electron 壳 + 内嵌 Go 运行时的完整桌面应用。

### 路径二：Headless 运行（ark web）

```bash
cd src/apps/web && pnpm build            # 先构建 Web 前端产物
go run ./src/services/cli/cmd/ark web    # 从仓库根目录启动本地运行时
```

`ark web` 会自动发现 `src/apps/web/dist` 并对外提供 Web 界面与本地 API（默认 **web 19080 / api 19001 / bridge 19003**，可用 `--port` / `--api-port` / `--bridge-port` 覆盖）。数据落在 `~/.arkloop`（`ARKLOOP_DATA_DIR` 可覆盖），首次启动自动完成 SQLite 迁移。

### 仅前端开发

```bash
cd src/apps/web && pnpm dev
```

Vite 开发服务器（默认 **5173**）将 `/v1` 代理到本地 API（默认 **19001**）——需先有路径二启动的运行时。

## 可选模块（Docker）

根目录 `compose.yaml` 只含可选模块，经 profile 启用：

```bash
docker compose --profile docker-sandbox up -d   # Docker 容器沙箱（本机 19002）
docker compose --profile searxng up -d          # 自托管搜索（web_search provider）
docker compose --profile firecrawl up -d        # 自托管抓取（web_fetch provider）
```

模块由 Bridge 管理；不启用这些 profile 时主程序照常运行。

## 测试

```bash
# Go（按模块；desktop 相关测试需 -tags desktop）
cd src/services/shared && go test ./...
cd src/services/worker && go test ./...
cd src/services/desktop && go test -tags desktop ./...

# 前端
cd src/apps/web && pnpm test
cd src/apps/web && pnpm lint && pnpm type-check
```

## CI 本地脚本

```bash
bin/ci-local quick                                       # 快速本地检查
bin/ci-local act <migration-lint|go-lint|desktop-go|pnpm-ci>   # 用 act 复现 CI 任务
```

仓库 CI（`.github/workflows/`）：`ci.yml`（migration lint + Go lint 矩阵 + desktop 构建 + pnpm 检查），`build-desktop.yaml`（桌面端发版），另有 Homebrew / AUR 包更新工作流。
