---
---

# Installer Bridge 与 Setup Chain 设计方案

本文定义 Arkloop 自托管部署的完整安装链路设计。覆盖 `setup.sh` 的职责边界、Installer Bridge 的分发与升级机制、用户从零到运行的全链路，以及需要解决的前端服务缺口。

结论先行：

- 自托管安装必须做到**一条命令可跑通**，但不能以牺牲可控性为代价。
- 保留三种安装方式：**one-liner**（curl pipe）、**release tarball**、**git clone**。去掉不必要的抽象层（不做 installer docker image，不做 brew/apt 包）。
- `setup.sh` 是唯一入口脚本，职责是 pre-flight → 配置生成 → 镜像拉取 → 服务启动 → 健康验证 → 引导提示。
- 前端应用（web / console）当前 compose.yaml 中缺失，必须补齐为独立 nginx 容器，不嵌入 Gateway。
- 升级链路不引入额外 CLI 工具，直接复用 `setup.sh upgrade` 子命令。

## 1. 安装方式

### 1.1 保留的三种方式

| 方式 | 命令 | 适用场景 |
|------|------|---------|
| **one-liner** | `curl -fsSL https://get.arkloop.ai \| bash` | 快速试用，最低摩擦 |
| **release tarball** | 从 GitHub Releases 下载 `arkloop-vX.Y.Z.tar.gz`，解压后运行 `./setup.sh` | 版本锁定，离线预备，生产部署 |
| **git clone** | `git clone` + `./setup.sh` | 开发者，需要阅读/修改源码 |

三种方式最终都收敛到同一个 `setup.sh`，行为一致。

### 1.2 不做的方式

| 方式 | 不做原因 |
|------|---------|
| Homebrew / apt 包 | 过度封装，Arkloop 不是单二进制，是一组 Docker 服务 |
| Helm Chart | Kubernetes 部署是独立课题，不在 setup.sh 范围内，后续单独规划 |
| Installer Docker image | 多一层抽象但没有实质收益，用户机器上已经有 Docker |

### 1.3 one-liner 引导流程

```
curl -fsSL https://get.arkloop.ai | bash
```

CDN 返回的是一个 **bootstrap stub**（约 50 行），不是完整 setup.sh。stub 的职责：

1. 检测 `curl`/`wget` 和 `tar` 是否可用
2. 从 GitHub Releases 下载当前最新 release tarball（或用户指定版本：`curl ... | ARKLOOP_VERSION=1.2.3 bash`）
3. 解压到 `$ARKLOOP_HOME`（默认 `./arkloop`）
4. exec 到解压后的 `setup.sh`

这样 setup.sh 始终从 tarball 中运行，版本一致性有保证。stub 自身无状态、无副作用、可审计。

## 2. Release Tarball 内容

```
arkloop-v1.2.3/
  setup.sh                    # 主安装脚本
  compose.yaml                # 生产 compose
  compose.dev.yaml            # 开发 overlay
  .env.example                # 环境变量模板
  config/
    openviking/ov.conf.example
    sandbox/templates.json
  VERSION                     # 版本号（纯文本，如 1.2.3）
  CHECKSUMS.sha256            # 校验和
```

不包含源码、不包含 Go/Node 工具链。镜像从 registry 拉取，不在用户机器上构建。

## 3. setup.sh 设计

### 3.1 子命令

```bash
./setup.sh              # 等同于 ./setup.sh install
./setup.sh install      # 完整安装流程
./setup.sh upgrade      # 升级到新版本
./setup.sh status       # 服务状态概览
./setup.sh doctor       # 诊断检查
./setup.sh uninstall    # 停止服务并清理
./setup.sh env-gen      # 仅生成 .env（不启动服务）
```

### 3.2 install 流程（6 个 phase）

```
Phase 0: Self-check
Phase 1: Pre-flight
Phase 2: Configuration
Phase 3: Image pull
Phase 4: Service startup
Phase 5: Post-install
```

#### Phase 0: Self-check

- 确认 bash 版本 >= 4.0（macOS 默认 bash 3.2，需要提示用户用 `/bin/bash` 或安装新版）
- 如果检测到旧版 bash 且系统有 `/usr/bin/env bash`，给出明确报错而非静默失败
- 检测是否通过 pipe 运行（`[ -t 0 ]`），pipe 模式下禁用交互式提问，走全自动

#### Phase 1: Pre-flight

检测项和判定逻辑：

| 检测项 | 最低要求 | 检测方法 | 失败策略 |
|--------|---------|---------|---------|
| OS | Linux / macOS / WSL2 | `uname -s`, `/proc/version` | 硬失败 |
| Architecture | amd64 / arm64 | `uname -m` | 硬失败（仅支持这两个） |
| Docker Engine | 24.0+ | `docker version --format '{{.Server.Version}}'` | 硬失败 |
| Docker Compose | v2 plugin | `docker compose version` | 硬失败 |
| CPU | >= 2 cores | `nproc` / `sysctl -n hw.ncpu` | 警告 |
| Memory | >= 4 GiB | `/proc/meminfo` / `sysctl -n hw.memsize` | 警告 |
| Disk | >= 10 GiB free | `df` | 警告 |
| Port 8000 | 未被占用 | `ss -tlnp` / `lsof -i` | 警告（可在 Phase 2 改端口） |
| Port 9000 | 未被占用 | 同上 | 警告 |
| KVM | `/dev/kvm` 可用 | `test -c /dev/kvm && test -w /dev/kvm` | 仅用于决定 sandbox provider |
| Docker socket | 检测用户态路径 | 按优先级探测多个路径（见下文） | sandbox docker mode 专用 |
| Network | 能访问 ghcr.io | `curl -fsS --connect-timeout 5 https://ghcr.io` | 警告（离线模式需提前准备镜像） |

Docker socket 探测优先级：

```
Linux rootless: /run/user/$(id -u)/docker.sock
macOS Docker Desktop: $HOME/.docker/run/docker.sock
Linux root: /var/run/docker.sock (不推荐，给出安全提示)
WSL2: /mnt/wsl/docker-desktop/shared-sockets/guest-services/docker.sock
```

#### Phase 2: Configuration

**交互模式**（检测到 TTY 时默认启用，`--non-interactive` 关闭）：

```
[1/5] Base URL (访问地址)
      用户输入域名或 IP，默认 http://localhost:8000
      影响：ARKLOOP_APP_BASE_URL、前端构建时的 API endpoint

[2/5] 端口配置
      Gateway: 8000 (默认)
      MinIO Console: 9001 (默认)
      用户可修改，脚本检测端口冲突

[3/5] 可选组件
      [x] Sandbox (代码执行) — 自动选择 Firecracker/Docker
      [x] Browser (网页浏览) — 需要额外 ~500MB 镜像
      [x] OpenViking (长期记忆) — 需要额外 ~200MB 镜像
      用户可取消勾选，减少资源占用

[4/5] LLM Provider（至少配一个，否则系统无法推理）
      选择 Provider: OpenAI / Anthropic / OpenAI-compatible
      输入 API Key
      输入 Model name (有默认值)
      -> 写入 ARKLOOP_PROVIDER_ROUTING_JSON

[5/5] 确认
      展示配置摘要，确认后继续
```

**非交互模式**：

从环境变量或 `--config /path/to/config.yaml` 读取所有值。缺少必填项则硬失败并列出缺失项。

config.yaml 格式：

```yaml
base_url: https://arkloop.example.com
gateway_port: 8000
components:
  sandbox: true
  browser: true
  openviking: true
llm:
  provider: openai
  api_key: sk-xxx
  model: gpt-4o
```

**密钥生成**：

所有密钥在 Phase 2 自动生成，不需要用户操心：

```bash
ARKLOOP_POSTGRES_PASSWORD=$(openssl rand -base64 24)
ARKLOOP_REDIS_PASSWORD=$(openssl rand -base64 24)
ARKLOOP_S3_SECRET_KEY=$(openssl rand -base64 24)
ARKLOOP_AUTH_JWT_SECRET=$(openssl rand -base64 48)
ARKLOOP_ENCRYPTION_KEY=$(openssl rand -hex 32)
ARKLOOP_SANDBOX_AUTH_TOKEN=$(openssl rand -hex 32)
```

生成后写入 `.env`，并在终端输出提示用户备份。

#### Phase 3: Image pull

```bash
# 核心镜像（始终拉取）
ghcr.io/qqqqqf/arkloop/api:v1.2.3
ghcr.io/qqqqqf/arkloop/gateway:v1.2.3
ghcr.io/qqqqqf/arkloop/worker:v1.2.3
ghcr.io/qqqqqf/arkloop/web:v1.2.3
ghcr.io/qqqqqf/arkloop/console:v1.2.3

# 基础设施（来自上游）
postgres:16-alpine
redis:7-alpine
minio/minio:latest
edoburu/pgbouncer:latest

# 可选镜像（根据 Phase 2 选择）
ghcr.io/qqqqqf/arkloop/sandbox:v1.2.3
ghcr.io/qqqqqf/arkloop/sandbox-agent:v1.2.3
ghcr.io/qqqqqf/arkloop/browser:v1.2.3
ghcr.io/volcengine/openviking:v0.1.18.dev0
```

拉取时显示进度条。支持 `--offline` 模式跳过拉取（要求用户提前 `docker load` 镜像）。

#### Phase 4: Service startup

```bash
# 1. 确定需要的 compose profiles
PROFILES=""
if [ "$SANDBOX_MODE" = "firecracker" ]; then
  PROFILES="$PROFILES --profile firecracker"
elif [ "$SANDBOX_MODE" = "docker" ]; then
  PROFILES="$PROFILES --profile docker-sandbox"
fi

# 2. 启动
docker compose $PROFILES up -d

# 3. 逐服务等待 healthy
# 按依赖顺序检查: postgres → pgbouncer → migrate → api → gateway → web/console
# 每个服务最多等待 120s，超时报错并输出该服务日志
```

健康检查进度展示：

```
[ok] postgres          healthy (2.1s)
[ok] redis             healthy (1.3s)
[ok] redis_gateway     healthy (1.2s)
[ok] minio             healthy (3.5s)
[ok] pgbouncer         healthy (1.8s)
[ok] migrate           completed (4.2s)
[ok] api               healthy (6.1s)
[ok] gateway           healthy (2.3s)
[ok] web               healthy (1.5s)
[ok] console           healthy (1.5s)
[ok] worker            running (3.2s)
[ok] openviking        healthy (8.1s)
[ok] browser           healthy (5.4s)
[ok] sandbox-docker    healthy (4.7s)
```

#### Phase 5: Post-install

```
Arkloop is running.

  Web UI:      http://localhost:8000
  Console:     http://localhost:8000/console
  MinIO:       http://localhost:9001
  API:         http://localhost:8000/v1

Next steps:
  1. 访问 Web UI 注册第一个账号
  2. 获取 user_id: curl -s http://localhost:8000/v1/auth/me -H "Authorization: Bearer <token>" | jq .id
  3. 设置管理员: echo 'ARKLOOP_BOOTSTRAP_PLATFORM_ADMIN=<user_id>' >> .env && docker compose restart api
  4. 用管理员登录 Console，配置 LLM Provider 和其他选项

Configuration saved to: /path/to/.env
Backup this file. Losing it means losing access to encrypted data.
```

### 3.3 upgrade 流程

```bash
./setup.sh upgrade [--version 1.3.0]
```

1. 读取当前 `VERSION` 文件
2. 不指定版本时，从 GitHub API 查询最新 release
3. 下载新版 tarball，解压到临时目录
4. 对比 `compose.yaml` 差异，如果有 breaking change 则提示用户确认
5. 备份当前 `.env` 和 `compose.yaml`
6. 拉取新版镜像
7. `docker compose down`（保留 volumes）
8. 替换 `compose.yaml`、`setup.sh`、`config/`
9. 保留用户 `.env`（不覆盖）
10. `docker compose up -d`（migrate 自动跑）
11. 健康检查
12. 输出变更摘要

```
Upgraded: v1.2.3 → v1.3.0

Changed images:
  api:     v1.2.3 → v1.3.0
  worker:  v1.2.3 → v1.3.0
  web:     v1.2.3 → v1.3.0

Migrations applied: 3 new
All services healthy.
```

**回滚**：upgrade 前的备份存放在 `.arkloop-backup/v1.2.3/`，用户可手动恢复。不提供自动回滚命令（复杂度不值得，数据库迁移一般不可逆）。

### 3.4 doctor 流程

```bash
./setup.sh doctor
```

检查运行中实例的状态：

```
[ok] Docker Engine:     27.1.0
[ok] Docker Compose:    v2.29.0
[ok] postgres:          healthy, 1.2GB data
[ok] redis:             healthy, 23MB used
[ok] api:               healthy, uptime 3d 12h
[ok] gateway:           healthy, uptime 3d 12h
[ok] worker:            running, 4 goroutines
[ok] web:               healthy
[warn] sandbox-docker:  unhealthy — last health check failed 30s ago
[ok] minio:             healthy, 4.1GB used
[skip] openviking:      not running (disabled)
[skip] browser:         not running (disabled)

Issues found: 1
  sandbox-docker: Container is unhealthy. Recent logs:
    2024-01-15T10:23:45Z ERROR docker socket not accessible
  Fix: Check ARKLOOP_SANDBOX_DOCKER_SOCKET_PATH in .env
```

### 3.5 uninstall 流程

```bash
./setup.sh uninstall
```

交互确认后：

1. `docker compose down`
2. 询问是否删除数据卷（`docker compose down -v`）
3. 询问是否删除生成的 `.env`
4. 询问是否删除拉取的镜像
5. 输出完成信息

### 3.6 脚本约束

- POSIX sh 兼容为目标，但允许依赖 bash 4+（macOS 用户需 `/usr/local/bin/bash` 或通过 Homebrew 安装）
- 不依赖 Python、Node.js、Go 或任何非系统工具
- 唯一硬依赖：`bash`、`docker`、`curl`（或 `wget`）、`openssl`、`tar`
- 所有输出使用纯 ASCII，不使用 emoji，不使用 ANSI 颜色转义（除非检测到支持且非 pipe 模式）
- 幂等：重复执行 `setup.sh install` 不会破坏已有数据（检测到已运行的实例时提示 upgrade）

## 4. 前端服务缺口

当前 `compose.yaml` 中**没有 web 和 console 服务**。Gateway 是纯反向代理，不 serve 静态文件。自托管用户无法访问前端界面。

### 4.1 方案：独立 nginx 容器

为 `web` 和 `console` 各创建一个 Dockerfile，构建为 nginx 容器：

```dockerfile
# src/apps/web/Dockerfile
FROM node:20-alpine AS builder
WORKDIR /app
COPY pnpm-lock.yaml pnpm-workspace.yaml package.json ./
COPY src/apps/shared/package.json src/apps/shared/
COPY src/apps/web/package.json src/apps/web/
RUN corepack enable pnpm && pnpm install --frozen-lockfile
COPY src/apps/shared src/apps/shared
COPY src/apps/web src/apps/web
RUN cd src/apps/web && pnpm build

FROM nginx:alpine
COPY --from=builder /app/src/apps/web/dist /usr/share/nginx/html
COPY src/apps/web/nginx.conf /etc/nginx/conf.d/default.conf
```

nginx.conf 要点：
- SPA fallback（所有非文件请求 → index.html）
- `/v1` 反向代理到 Gateway（或由上层负载均衡处理）
- 运行时环境变量注入（构建时写入 `window.__ARKLOOP_CONFIG__`，或用 entrypoint 脚本替换占位符）

### 4.2 compose.yaml 新增服务

```yaml
web:
  build:
    context: .
    dockerfile: src/apps/web/Dockerfile
  restart: unless-stopped
  environment:
    ARKLOOP_API_BASE_URL: "${ARKLOOP_API_BASE_URL:-http://gateway:8000}"
  depends_on:
    gateway:
      condition: service_healthy
  healthcheck:
    test: ["CMD", "wget", "-qO-", "http://localhost:80/"]
    interval: 15s
    timeout: 3s
    retries: 5

console:
  build:
    context: .
    dockerfile: src/apps/console/Dockerfile
  restart: unless-stopped
  environment:
    ARKLOOP_API_BASE_URL: "${ARKLOOP_API_BASE_URL:-http://gateway:8000}"
  depends_on:
    gateway:
      condition: service_healthy
  healthcheck:
    test: ["CMD", "wget", "-qO-", "http://localhost:80/"]
    interval: 15s
    timeout: 3s
    retries: 5
```

### 4.3 Gateway 路由变更

Gateway 需要根据路径前缀分发请求：

| 路径 | 目标 |
|------|------|
| `/v1/*` | API (upstream) |
| `/console/*` | Console 容器 |
| `/*` | Web 容器 |

或者更简单的方案：Gateway 不变，在 compose 最外层加一个 nginx/caddy 做入口路由。但这会增加一层，不如直接让 Gateway 支持多 upstream（web + api + console）。

推荐方案：Gateway 增加静态文件路由能力，把 web/console 的构建产物挂载进去。这样 Gateway 既是 API 代理又是前端服务器，保持单入口。

```yaml
gateway:
  volumes:
    - web_dist:/usr/share/arkloop/web:ro
    - console_dist:/usr/share/arkloop/console:ro
```

具体选择取决于 Gateway 侧的改造成本。建议先用方案 A（独立 nginx）快速落地，后续视情况合并到 Gateway。

## 5. Installer Bridge 架构

"Installer Bridge" 指安装器自身的分发、版本管理和更新通道。

### 5.1 分发通道

```
GitHub Releases (source of truth)
    ├── arkloop-vX.Y.Z.tar.gz          # release tarball
    ├── arkloop-vX.Y.Z.tar.gz.sha256   # checksum
    └── setup-stub.sh                   # one-liner bootstrap stub

CDN (get.arkloop.ai)
    └── index.sh → 重定向到 GitHub Releases 的 setup-stub.sh
```

CDN 层只做重定向，不缓存脚本内容。版本发布后 CDN 自动指向最新 release。

### 5.2 镜像分发

所有服务镜像推送到 GHCR：

```
ghcr.io/qqqqqf/arkloop/api:v1.2.3
ghcr.io/qqqqqf/arkloop/api:latest
ghcr.io/qqqqqf/arkloop/gateway:v1.2.3
ghcr.io/qqqqqf/arkloop/worker:v1.2.3
ghcr.io/qqqqqf/arkloop/web:v1.2.3
ghcr.io/qqqqqf/arkloop/console:v1.2.3
ghcr.io/qqqqqf/arkloop/sandbox:v1.2.3
ghcr.io/qqqqqf/arkloop/sandbox-agent:v1.2.3
ghcr.io/qqqqqf/arkloop/browser:v1.2.3
```

每个镜像同时推送 `vX.Y.Z` 精确标签和 `latest` 标签。compose.yaml 中使用精确版本标签。

### 5.3 版本协议

`VERSION` 文件为纯文本，内容如 `1.2.3`。遵循 semver。

setup.sh 在 upgrade 时对比本地 VERSION 与远程 VERSION，决定是否需要升级以及升级路径。不支持跨大版本自动升级（v1 → v2），需要手动操作。

### 5.4 离线安装

支持完全离线的部署场景（内网/air-gapped 环境）：

```bash
# 在有网络的机器上准备离线包
./setup.sh pack --version 1.2.3 --output arkloop-offline-v1.2.3.tar.gz
```

pack 子命令：
1. 下载 release tarball
2. `docker pull` 所有镜像
3. `docker save` 导出为 tar
4. 打包为单个归档文件

```bash
# 在目标机器上
tar xzf arkloop-offline-v1.2.3.tar.gz
cd arkloop-offline-v1.2.3
./setup.sh install --offline
```

`--offline` 模式跳过网络检查，从本地 tar 文件 `docker load` 镜像。

## 6. 外部服务对接

自托管用户可能已有 PostgreSQL / Redis / S3 实例，不需要 Docker 中的内置实例。

### 6.1 setup.sh 交互支持

Phase 2 中增加选项：

```
[可选] 使用外部 PostgreSQL?
  > 是: 输入连接串 postgresql://user:pass@host:5432/dbname
  > 否: 使用内置 PostgreSQL (默认)

[可选] 使用外部 Redis?
  > 是: 输入连接串 redis://:pass@host:6379/0
  > 否: 使用内置 Redis (默认)

[可选] 使用外部 S3?
  > 是: 输入 endpoint / access_key / secret_key / bucket / region
  > 否: 使用内置 MinIO (默认)
```

选择外部服务时：
- 对应的 Docker 服务不启动（通过生成 compose override 排除）
- `.env` 中直接写入外部连接串
- pre-flight 验证外部服务的连通性

### 6.2 compose override 策略

setup.sh 根据用户选择生成 `compose.override.yaml`：

```yaml
# 使用外部 PostgreSQL + 外部 Redis 时生成
services:
  postgres:
    profiles: ["disabled"]
  pgbouncer:
    profiles: ["disabled"]
  redis:
    profiles: ["disabled"]
  redis_gateway:
    profiles: ["disabled"]
  api:
    depends_on:
      migrate:
        condition: service_completed_successfully
  migrate:
    depends_on: {}
```

## 7. TLS 与反向代理

### 7.1 内置 TLS（Caddy 方案）

对于需要 HTTPS 的自托管用户，在 Gateway 前面增加 Caddy 作为 TLS 终止层：

```yaml
caddy:
  image: caddy:2-alpine
  restart: unless-stopped
  ports:
    - "80:80"
    - "443:443"
  volumes:
    - ./Caddyfile:/etc/caddy/Caddyfile:ro
    - caddy_data:/data
  depends_on:
    gateway:
      condition: service_healthy
```

setup.sh Phase 2 中询问是否启用 HTTPS：

```
[可选] 启用 HTTPS?
  > 是: 输入域名 (Caddy 自动申请 Let's Encrypt 证书)
  > 否: 仅 HTTP (默认，适合内网/开发)
```

选择 HTTPS 时自动生成 Caddyfile 并加入 compose。Gateway 此时不对外暴露端口，所有流量经 Caddy 进入。

### 7.2 自有反向代理

用户如果已有 nginx / Traefik / CloudFlare Tunnel 等，直接代理到 Gateway:8000 即可。setup.sh 不做额外处理，文档中给出常见反向代理的配置示例。

## 8. Admin Bootstrap 自动化

当前 bootstrap 管理员的流程需要 3 步手动操作（注册 → 获取 ID → 写 env → 重启），对自托管用户不友好。

### 8.1 改进方案

setup.sh Phase 5 中提供自动化选项：

```
是否立即创建管理员账号?
  Email: admin@example.com
  Password: ********
```

通过调用 API 完成：
1. `POST /v1/auth/register` 创建账号
2. `POST /v1/auth/login` 获取 token
3. `GET /v1/auth/me` 获取 user_id
4. 写入 `ARKLOOP_BOOTSTRAP_PLATFORM_ADMIN=<user_id>` 到 `.env`
5. `docker compose restart api`
6. 等待 API healthy

这需要 API 已经启动且可访问。流程上在 Phase 4（服务启动）之后执行。

### 8.2 非交互模式

```bash
ARKLOOP_ADMIN_EMAIL=admin@example.com \
ARKLOOP_ADMIN_PASSWORD=securepassword \
./setup.sh install --non-interactive
```

## 9. 诊断与错误处理

### 9.1 失败时的诊断包

任何 Phase 失败时自动收集：

```
arkloop-diagnostic-20240115-102345/
  docker-info.txt          # docker info
  docker-ps.txt            # docker compose ps -a
  docker-logs/             # 每个服务最后 200 行日志
  env-sanitized.txt        # .env 脱敏版本（密码替换为 ***）
  system-info.txt          # uname, resources, ports
  setup-log.txt            # setup.sh 完整执行日志
```

自动打包为 `arkloop-diagnostic-*.tar.gz`，提示用户附到 issue 中。

### 9.2 常见错误的内联修复建议

| 错误场景 | 建议 |
|---------|------|
| Docker 未安装 | 输出 `curl -fsSL https://get.docker.com \| sh` |
| 端口被占用 | 列出占用进程，提示修改 `.env` 中的端口 |
| 内存不足 | 建议关闭可选组件（sandbox/browser/openviking） |
| 镜像拉取失败 | 提示配置 Docker mirror，或使用 `--offline` |
| KVM 不可用 | 自动回退 Docker sandbox，无需用户干预 |
| 数据库迁移失败 | 输出 migrate 容器日志，提示检查数据库连接 |

## 10. 目录结构与文件约定

安装完成后的目录结构：

```
$ARKLOOP_HOME/                     # 默认 ./arkloop 或 /opt/arkloop
  setup.sh
  compose.yaml
  compose.override.yaml            # setup.sh 生成，用户可编辑
  .env                             # setup.sh 生成
  .env.example                     # 参考模板
  config/
    openviking/ov.conf
    sandbox/templates.json
    caddy/Caddyfile                # 启用 HTTPS 时生成
  VERSION
  .arkloop-backup/                 # upgrade 时的备份
    v1.2.3/
      compose.yaml
      .env
```

`.env` 和 `compose.override.yaml` 是用户态文件，upgrade 不覆盖。`compose.yaml` 是发行版文件，upgrade 会替换。

## 11. CI/CD Release Pipeline

release 流程（GitHub Actions）：

1. Tag push `vX.Y.Z` 触发
2. 构建所有服务镜像（multi-arch: amd64 + arm64）
3. 推送镜像到 GHCR
4. 打包 release tarball（compose.yaml + setup.sh + config + VERSION）
5. 计算 checksum
6. 创建 GitHub Release，上传 tarball 和 checksum
7. 更新 CDN 的 bootstrap stub 指向新版本

## 12. 安全考量

| 关注点 | 措施 |
|--------|------|
| curl pipe 安全 | bootstrap stub 从 HTTPS 下载，tarball 有 SHA256 校验 |
| 密钥存储 | `.env` 文件权限设为 `600`，setup.sh 自动执行 `chmod 600 .env` |
| Docker socket | 优先使用 rootless Docker 用户态 socket，拒绝 `/var/run/docker.sock`（给出安全警告） |
| 镜像完整性 | 使用精确版本标签，后续考虑引入 cosign 签名验证 |
| 网络隔离 | 内部服务不暴露端口到宿主机（除 Gateway），sandbox 使用专用网络 |
| 密钥轮换 | `setup.sh rotate-secrets` 子命令（后续实现） |

## 13. 与 compose.yaml 的 git clone 方式兼容

对于选择 `git clone` 安装方式的用户，setup.sh 必须能在仓库根目录正确运行。检测逻辑：

- 如果 `compose.yaml` 在当前目录 → 直接使用
- 如果在 `$ARKLOOP_HOME` 目录 → 从那里使用
- 如果都不存在 → 报错

git clone 方式与 tarball 方式的差异：
- git clone 包含源码，可以 `docker compose build` 而非 pull
- setup.sh 检测到 `.git` 目录时，提供 "build from source" 选项
- 非 git clone 方式始终从 registry pull 预构建镜像

## 14. 待确认事项

以下事项需要在实现前确认：

1. **前端服务化方案**：Gateway 内嵌静态文件 vs 独立 nginx 容器 vs 外部 CDN — 决定 compose.yaml 结构
2. **镜像 registry**：使用 GHCR 还是 Docker Hub — 影响 setup.sh 中的 pull 地址
3. **LLM Provider 初始化**：是否在 setup.sh 中完成，还是引导用户进 Console 配置 — 影响 Phase 2 复杂度
4. **最低 Docker 版本**：24.0 还是更低 — 影响 compose v2 plugin 依赖
5. **arm64 镜像**：是否第一版就支持 arm64 multi-arch — 影响 CI 构建成本
6. **Caddy TLS 是否内置**：是否作为可选组件进入 compose.yaml
