# Sandbox Service -- Docker 后端部署指南

Sandbox 服务支持两种后端：Firecracker（Linux/KVM 微虚拟机）和 Docker（容器化执行）。Docker 后端适用于 macOS、Windows (WSL2) 开发环境以及无 KVM 支持的 OSS 自部署场景。

---

## 1. 构建 sandbox-agent 镜像

Docker 后端需要一个包含 Python 运行时和 sandbox-agent 二进制的容器镜像。

```bash
# 从项目根目录执行
docker build -f src/services/sandbox/Dockerfile.agent -t arkloop/sandbox-agent:latest .
```

验证镜像：

```bash
docker run --rm arkloop/sandbox-agent:latest sandbox-agent --help 2>&1 || true
# 应该看到 agent 启动输出（因为没有 terminal 会立刻退出，但说明二进制存在）
```

---

## 2. 启动 Docker Sandbox

### 方式 A：通过 Docker Compose（推荐）

```bash
# 使用 docker-sandbox profile 启动 sandbox 服务
docker compose --profile docker-sandbox up -d sandbox-docker

# 查看状态
docker compose --profile docker-sandbox ps sandbox-docker
docker compose --profile docker-sandbox logs sandbox-docker
```

### 方式 B：直接运行 sandbox 服务二进制

```bash
# 先编译 sandbox 服务
cd src/services/sandbox
go build -o sandbox-bin ./cmd/sandbox

# 设置环境变量后启动
export ARKLOOP_SANDBOX_PROVIDER=docker
export ARKLOOP_SANDBOX_DOCKER_IMAGE=arkloop/sandbox-agent:latest
export ARKLOOP_SANDBOX_SOCKET_DIR=/tmp/sandbox
export ARKLOOP_SANDBOX_TEMPLATES_PATH=""    # Docker 模式不需要模板

mkdir -p /tmp/sandbox
./sandbox-bin
```

---

## 3. Provider 切换

通过环境变量 `ARKLOOP_SANDBOX_PROVIDER` 控制：

| 值 | 后端 | 要求 |
|---|---|---|
| `firecracker` | Firecracker microVM（默认） | Linux + /dev/kvm + Firecracker 二进制 |
| `docker` | Docker 容器 | Docker Engine 运行中 |

切换只需修改该环境变量并重启 sandbox 服务。

如果使用 Docker Compose，通过 profile 选择：

```bash
# Firecracker 模式（Linux 生产环境）
docker compose --profile firecracker up -d sandbox

# Docker 模式（macOS / Windows / OSS 自部署）
docker compose --profile docker-sandbox up -d sandbox-docker
```

两个 profile 互斥，不要同时启动。

---

## 4. 配置参数

### 通用参数（两种后端都适用）

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `ARKLOOP_SANDBOX_ADDR` | `0.0.0.0:8002` | Sandbox HTTP 服务监听地址 |
| `ARKLOOP_SANDBOX_PROVIDER` | `firecracker` | 后端类型：`firecracker` / `docker` |
| `ARKLOOP_SANDBOX_MAX_SESSIONS` | `50` | 最大并发 session 数 |
| `ARKLOOP_SANDBOX_AGENT_PORT` | `8080` | Guest Agent 监听端口 |
| `ARKLOOP_SANDBOX_SOCKET_DIR` | `/run/sandbox` | 临时文件目录 |

### Warm Pool 参数

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `ARKLOOP_SANDBOX_WARM_LITE` | `3` | lite tier 预热实例数 |
| `ARKLOOP_SANDBOX_WARM_PRO` | `2` | pro tier 预热实例数 |
| `ARKLOOP_SANDBOX_WARM_ULTRA` | `1` | ultra tier 预热实例数 |
| `ARKLOOP_SANDBOX_REFILL_INTERVAL` | `5` | 预热补充检查间隔（秒） |
| `ARKLOOP_SANDBOX_REFILL_CONCURRENCY` | `2` | 预热补充最大并发数 |

### Session 超时参数

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `ARKLOOP_SANDBOX_IDLE_TIMEOUT_LITE` | `180` | lite session 空闲超时（秒） |
| `ARKLOOP_SANDBOX_IDLE_TIMEOUT_PRO` | `300` | pro session 空闲超时（秒） |
| `ARKLOOP_SANDBOX_IDLE_TIMEOUT_ULTRA` | `600` | ultra session 空闲超时（秒） |
| `ARKLOOP_SANDBOX_MAX_LIFETIME` | `1800` | session 最大存活时间（秒） |

### Docker 后端专用

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `ARKLOOP_SANDBOX_DOCKER_IMAGE` | `arkloop/sandbox-agent:latest` | sandbox-agent 容器镜像 |

### Firecracker 后端专用

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `ARKLOOP_FIRECRACKER_BIN` | `/usr/bin/firecracker` | Firecracker 二进制路径 |
| `ARKLOOP_SANDBOX_KERNEL_IMAGE` | `/opt/sandbox/vmlinux` | 内核镜像路径 |
| `ARKLOOP_SANDBOX_ROOTFS` | `/opt/sandbox/rootfs.ext4` | rootfs 路径 |
| `ARKLOOP_SANDBOX_BOOT_TIMEOUT_SECONDS` | `30` | VM 启动超时（秒） |
| `ARKLOOP_SANDBOX_TEMPLATES_PATH` | `/opt/sandbox/templates.json` | 模板注册文件路径 |

### S3 / 产物存储（可选）

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `ARKLOOP_S3_ENDPOINT` | （空） | MinIO / S3 端点，留空则不启用产物上传 |
| `ARKLOOP_S3_ACCESS_KEY` | （空） | S3 Access Key |
| `ARKLOOP_S3_SECRET_KEY` | （空） | S3 Secret Key |

---

## 5. Tier 资源映射

每个 session 根据 tier 分配不同资源：

| Tier | CPU | 内存 |
|---|---|---|
| `lite` | 1 核 | 256 MB |
| `pro` | 1 核 | 1024 MB |
| `ultra` | 2 核 | 4096 MB |

Docker 后端额外安全限制：
- PID 上限 256
- `cap-drop=ALL`（移除所有 Linux capabilities）
- `no-new-privileges`（禁止提权）
- 端口绑定到 `127.0.0.1`

---

## 6. 测试

### 健康检查

```bash
curl http://localhost:8002/healthz
```

### 执行代码

```bash
# Python
curl -X POST http://localhost:8002/v1/exec \
  -H 'Content-Type: application/json' \
  -d '{
    "session_id": "test-001",
    "tier": "lite",
    "language": "python",
    "code": "print(\"hello from docker sandbox\")",
    "timeout_ms": 10000
  }'

# Shell
curl -X POST http://localhost:8002/v1/exec \
  -H 'Content-Type: application/json' \
  -d '{
    "session_id": "test-001",
    "language": "shell",
    "code": "uname -a && python3 --version",
    "timeout_ms": 5000
  }'
```

### 查看统计

```bash
curl http://localhost:8002/v1/stats
```

### 删除 session

```bash
curl -X DELETE http://localhost:8002/v1/sessions/test-001
```

---

## 7. 故障排查

**sandbox 服务启动失败 "docker daemon unreachable"**
- 确认 Docker Engine 正在运行：`docker info`
- 如果在容器中运行，确认挂载了 Docker socket：`-v /var/run/docker.sock:/var/run/docker.sock`

**"docker agent not ready" 超时**
- 确认 sandbox-agent 镜像存在：`docker images arkloop/sandbox-agent`
- 手动测试镜像：`docker run --rm -e SANDBOX_AGENT_LISTEN=tcp -p 8080:8080 arkloop/sandbox-agent:latest`
- 检查容器日志：`docker logs <container_id>`

**exec 返回错误**
- 确认 `language` 为 `python` 或 `shell`
- 确认 `code` 不为空
- 检查 sandbox 服务日志

---

## 8. Docker Compose 架构说明

Docker 模式下，`sandbox-docker` 服务使用 `network_mode: host`，原因：

sandbox 服务通过 Docker socket 在宿主机上创建 sandbox-agent 容器，这些容器的端口绑定在宿主机 `127.0.0.1` 上。sandbox 服务需要通过这些端口与 agent 通信，如果 sandbox 服务在普通 Docker 网络中运行，无法访问宿主机 `127.0.0.1` 上的端口。使用 host 网络模式解决此问题。

Firecracker 模式不受此限制（通过 vsock 通信，不经过网络层）。
