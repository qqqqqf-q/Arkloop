---
---

# Installer Bridge 与自部署安装架构

本文重新定义 Arkloop 自部署安装链路，目标是降低选择成本、收紧权限边界，并给 Agent、CLI、Console 三种入口提供同一套安装语义。

## 1. 设计目标

本文定义 Arkloop 的完整安装链路，覆盖从 `setup.sh` 到 Installer Bridge 的所有环节。

本文解决以下问题：

1. 自部署用户如何安装 Arkloop（setup.sh + installation.md + Agent）
2. SaaS 用户如何自行部署
3. 哪些模块默认安装，哪些模块可选
4. 基础设施默认配置策略（PostgreSQL / Redis / Storage 的精简方案）
5. Console 能管到哪里，不能管到哪里
6. Installer Bridge 的最小职责
7. 首次安装后的管理员初始化方案

本文不解决这些问题：

- Docker Desktop 如何跨平台自动安装
- 所有第三方系统的内部高级配置如何统一抽象
- 所有更新流程的一键无人值守回滚
- 任意宿主机命令执行

## 2. 先拍板的原则

### 2.1 默认安装必须只有一个答案

自部署默认安装档位固定为：

```text
standard = core + gateway + console-lite
```

含义：

- 用户第一次安装不需要理解全部模块
- 安装完成后，用户立刻有可访问入口
- 后续扩展模块通过 Console 或 Agent 增量安装

### 2.2 模块分层必须清晰

安装问题不能混在一起谈。必须分为四层：

1. 宿主前置条件
2. Arkloop 核心栈
3. Arkloop 可选模块
4. 外部系统内部配置

其中：

- 前三层是 Arkloop 的安装问题
- 第四层通常不是 Arkloop 的安装问题，只能算接入或初始化问题

### 2.3 Console 是控制面，不是宿主执行器

Console 可以：

- 展示状态
- 收集参数
- 发起安装意图
- 展示日志和健康检查
- 生成给 Agent 或 CLI 的操作入口

Console 不应直接：

- 操作宿主 Docker socket
- 拿 root 权限执行命令
- 充当任意命令入口

### 2.4 setup.sh 必须很薄，但允许问答

`setup.sh` 仍然可以做问答，但问答只能是同一套安装 parser 的一个输入前端。

`setup.sh` 负责：

- pre-flight
- 生成初始 `.env`
- 通过 parser 接收安装选择
- 启动默认档位或指定档位
- 基础诊断
- 升级和卸载的兜底入口

`setup.sh` 不负责：

- 把安装规则散落成一堆 bash if/else 特例
- 直接承载第三方 provider 内部高级配置
- 形成只给人类可用、不给 Agent 可用的交互逻辑

换句话说：

- 普通用户可以通过 `setup.sh` 回答问题安装
- Agent 也必须能通过同一个 parser 的 CLI 参数完成安装

### 2.5 installation.md 作为 Agent 优先入口

对自部署用户，优先推荐：

```text
installation.md + Agent
```

原因：

- 复杂分支问答更适合 Agent 处理
- Agent 更适合处理异常、重试和解释错误
- 可以显著减少 shell 菜单复杂度

但 `setup.sh` 仍然保留，作为稳定托底执行器。

## 3. 默认模块策略

### 3.1 Core

以下模块属于核心栈，必须安装：

| 模块 | 说明 |
|------|------|
| `postgres` | 主数据库 |
| `redis` | 队列与缓存 |
| `migrate` | 数据迁移 |
| `api` | 控制面 API |
| `worker` | 执行面 |

说明：

- `redis` 视为平台基座，不作为用户决策项
- `pgbouncer` 移出核心栈，自部署小规模场景下直连 PostgreSQL 即可
- `seaweedfs` / S3 存储移出核心栈，默认使用本地文件系统

### 3.1.1 自部署基础设施优化策略

自部署场景的核心目标：最小化资源消耗和运维复杂度。

#### PostgreSQL 精简配置

自部署默认安装不启用 PGBouncer，PostgreSQL 直连。配置目标是把连接数和内存压到最低：

```text
# setup.sh 生成的默认 PostgreSQL 配置
max_connections = 50              # 默认 100，自部署压到 50
shared_buffers = 128MB            # 默认 128MB，保持不变
work_mem = 4MB                    # 默认 4MB，保持不变
effective_cache_size = 256MB      # 压低
maintenance_work_mem = 64MB       # 压低
```

API 侧对应调整：

```text
ARKLOOP_API_DB_POOL_MAX_CONNS=10          # 默认 32，自部署压到 10
ARKLOOP_API_DB_POOL_MIN_CONNS=0           # 保持 0
ARKLOOP_API_DB_DIRECT_POOL_MAX_CONNS=3    # 默认 10，自部署压到 3
```

PGBouncer 作为可选模块，仅在用户显式选择或并发量超过阈值时启用。

#### Redis 单实例策略

Redis 仅作为平台级基础设施，单实例服务所有场景：

- API 队列与缓存
- Gateway 的 rate limiting 与动态配置

不再单独部署 `redis_gateway` 实例。原来的 Gateway Redis 隔离设计在自部署场景下带来不必要的复杂度，单实例通过 Redis database number 做逻辑隔离即可：

```text
ARKLOOP_REDIS_URL=redis://:${password}@redis:6379/0          # API + Worker
ARKLOOP_GATEWAY_REDIS_URL=redis://:${password}@redis:6379/1   # Gateway
```

SaaS 或高并发场景仍可通过 `.env` 配置独立 Redis 实例。

#### 存储默认策略

默认使用本地文件系统（filesystem backend），已实现 failsafe：

```text
ARKLOOP_STORAGE_BACKEND=filesystem
ARKLOOP_STORAGE_ROOT=/var/lib/arkloop/storage
```

S3 兼容存储（SeaweedFS / MinIO / AWS S3）作为可选模块。原因：

- 自部署用户通常不需要对象存储的水平扩展能力
- 本地文件系统已经覆盖沙箱产物、会话状态等核心存储需求
- 减少一个有状态服务意味着更低的运维负担

### 3.2 Standard 默认入口

以下模块属于默认自部署入口：

| 模块 | 默认值 | 说明 |
|------|--------|------|
| `gateway` | 开启 | 默认对外入口 |
| `console-lite` | 开启 | 自部署默认控制台 |
| `console` | 关闭 | 复杂 Console，增量安装 |

说明：

- `gateway` 在产品层面默认开启。虽然引入一层反代有性能开销，但它隐藏了内部服务拓扑，并提供 rate limiting、geo-IP、风险评分等安全能力，自部署场景下去掉 Gateway 会有安全风险
- `console-lite` 是自部署默认控制台
- `console` 只作为升级选项，不进入首次安装主流程
- `console-lite` 中必须提供"安装完整 Console"的入口，这是 Installer Bridge 的核心使用场景之一。安装一个 Console 和安装一个 OpenViking 在架构上是同一种操作

### 3.3 Optional 模块

| 模块 | 默认值 | 说明 |
|------|--------|------|
| `openviking` | 关闭 | 记忆系统，可选扩展 |
| `sandbox-docker` | 关闭 | macOS / Windows / 无 KVM 环境 |
| `sandbox-firecracker` | 关闭 | Linux + KVM 环境 |
| `browser` | 关闭 | 浏览器模块，作为 Sandbox 的 `browser` tier 接入 |
| `pgbouncer` | 关闭 | 连接池，高并发场景按需启用 |
| `seaweedfs` | 关闭 | S3 兼容对象存储，大规模或分布式场景按需启用 |
| `searxng` | 关闭 | 自建搜索引擎实例，替代 Tavily API |
| `firecrawl` | 关闭 | 自建网页抓取实例，替代 Jina/Basic fetch（SearXNG 可能自带，需调研） |

说明：

- `sandbox-docker` 与 `sandbox-firecracker` 互斥
- `openviking` 不进入首次安装默认路径
- `browser` 不进入首次安装默认路径。已决定作为 Sandbox 的 `browser` tier 接入，使用独立 Docker 镜像（Node + Chromium），通过 `agent-browser` 引擎驱动（参见 browser-automation-architecture 文档）
- `pgbouncer` 仅在自部署并发量超过 PostgreSQL 直连能力时按需启用
- `seaweedfs` 仅在需要 S3 兼容或分布式存储时按需启用
- `searxng` + `firecrawl` 是 web-search / web-fetch 的自建替代方案。Worker 代码已支持 SearXNG 作为 web-search provider 和 Firecrawl 作为 web-fetch provider，这里要做的是部署层面的集成。安装后可以避免第三方 API 依赖和成本，但会带来额外的性能消耗、IP 风险（爬虫 IP 暴露）和维护成本。安装路径中必须向用户明确提示这些 trade-off
- 需调研 SearXNG 是否已内置 Firecrawl 功能（即 SearXNG 单独安装是否同时覆盖 search + fetch），如果是则 `firecrawl` 模块可能不需要独立部署

### 3.4 不暴露给用户的内部细节

这些属于实现细节，不作为首次安装问题抛给用户：

| 项 | 策略 |
|----|------|
| Redis database number 分配 | 对用户透明，由模块实现决定 |
| Compose 具体 service 名 | 对用户透明 |
| PostgreSQL 连接参数调优 | setup.sh 按 profile 自动生成 |
| 存储后端自动检测逻辑 | 对用户透明 |

用户只决定“是否启用某个能力”，不决定“内部怎么实现”。

## 4. 安装边界

### 4.1 三类边界

#### A. 宿主前置条件

典型例子：

- Docker Desktop
- WSL2
- Linux 上的 `/dev/kvm`
- rootless Docker socket

策略：

- Arkloop 只做检测、提示、恢复流程
- 默认不承诺跨平台自动安装这些前置依赖

#### B. Arkloop 管理的模块生命周期

典型例子：

- `openviking` 容器是否存在
- `sandbox-docker` 是否启动
- `console` 是否已安装
- Arkloop 自己的升级、重启、健康检查

策略：

- 这是 Installer Bridge 的核心职责
- 这部分可以做成一键安装 / 一键升级 / 一键重建

#### C. 外部系统的内部配置

典型例子：

- OpenViking 的 embedding model 细项
- 其他 memory system 的 provider 专有参数
- 外部系统自己的租户、索引、向量维度、rerank 参数

策略：

- Arkloop 不做统一抽象
- Arkloop 最多只做“默认初始化”
- 高级配置仍由外部系统自己管理

这条边界必须守住，否则 Bridge 会膨胀成“统一管理所有外部系统后台”的怪物。

## 4.2 Arkloop SaaS 部署路径

此段落针对 Arkloop 自身的 SaaS 部署场景（即 Arkloop 官方运营的 SaaS 服务），不涉及第三方使用 Arkloop 提供 SaaS 服务（License 不允许）。

SaaS 部署与标准自部署使用同一套安装基础，但存在以下差异：

### 4.2.1 差异点

| 维度 | 标准自部署 | SaaS 部署 |
|------|-----------|-----------|
| 认证 | 本地注册 + bootstrap admin | 对接已有身份系统（OAuth / SSO） |
| 计费 | 无 credit 系统 | credit deduction 启用 |
| 模型 | 用户自行配置 API Key | 平台统一管理 provider |
| 域名 | localhost 或内网 | 公网暴露 + TLS |
| 升级 | 手动 `setup.sh upgrade` | CI/CD pipeline 自动部署 |
| 规模 | 单机 | 可能需要多实例 + PGBouncer + S3 |

### 4.2.2 setup.sh 的 SaaS 模式

`setup.sh install` 增加 `--mode` 参数：

```bash
./setup.sh install --mode self-hosted    # 默认，标准自部署
./setup.sh install --mode saas           # Arkloop SaaS 部署
```

`--mode saas` 的差异行为：

- Gateway 的 rate limiting 和 Turnstile 默认开启
- `.env` 中生成 SaaS 相关配置占位（如 `ARKLOOP_TURNSTILE_SITE_KEY`）
- Console Lite 中 credit 相关页面不隐藏
- 不自动生成 bootstrap admin URL（SaaS 场景由部署流水线处理）
- PGBouncer 默认启用（SaaS 并发量需要连接池）
- S3 存储默认启用（SaaS 需要持久化对象存储）

### 4.2.3 当前缺口

现有 setup.sh 设计主要面向标准自部署。SaaS 部署还需要补充：

- Gateway 公网暴露时的安全加固配置模板
- TLS 终结配置指引（Caddy / Nginx / Cloudflare Tunnel）
- 自动化部署集成（CI/CD pipeline 调用 `setup.sh --non-interactive`）
- 多实例水平扩展指引

这些内容属于后续 PR 的范围，本文只记录需求边界。

## 5. 统一安装语义

安装入口可以有多个，但必须共用同一套语义。

### 5.1 单一真源

建议只保留一份模块定义：

```text
install/modules.yaml   # 模块定义、依赖、平台约束、能力集、默认值
```

安装选择结果不额外落一份 `.env.install`。

相反，`setup.sh` 内部维护统一 parser，允许三种输入源：

1. 交互式问答
2. CLI flags
3. 非交互模式默认值

语义：

- `install/modules.yaml` 描述系统“能装什么”
- `setup parser` 负责解析“这次要装什么”

modules.yaml 通过 profile 标记支持不同部署模式的默认差异：

```yaml
# 示例结构
modules:
  pgbouncer:
    category: optional
    profiles:
      self-hosted:
        default: false
      saas:
        default: true
  seaweedfs:
    category: optional
    profiles:
      self-hosted:
        default: false
      saas:
        default: true
```

`--mode` 参数决定激活哪个 profile，profile 影响默认值，但用户仍可通过 CLI flags 覆盖。

这样 Agent、CLI、Console 都不会各自维护一套判断逻辑。

### 5.2 四个入口

| 入口 | 角色 | 是否必须 |
|------|------|----------|
| `installation.md` | Agent 问答与异常处理入口 | 是 |
| `setup.sh` | 本地稳定执行器与 parser 宿主 | 是 |
| Console | 状态页、增量安装、引导页 | 是 |
| Installer Bridge | 真一键执行器 | 是 |

本文按最终目标架构书写，因此 Bridge 视为明确要实现的组件。

开发过渡期允许：

- Console 先生成命令或 Prompt
- Agent / CLI 先执行安装

但 roadmap 应以 Bridge 存在为目标，而不是把“没有 Bridge”当成长期形态。

## 6. installation.md 的职责

`installation.md` 是 Agent 入口，不是普通宣传文档。

它应该只做三件事：

1. 告诉 Agent 如何探测环境
2. 告诉 Agent 该问用户哪些问题
3. 告诉 Agent 如何把答案转换成 `setup.sh` 的 parser 参数并调用 `setup.sh`

### 6.1 Agent 固定提问顺序

问题必须固定、很少、可判定：

1. 你要默认自部署还是高级部署（对应 `--profile standard|full`）
2. 部署模式：标准自部署还是 SaaS 部署（对应 `--mode self-hosted|saas`，仅 Arkloop 官方 SaaS）
3. 是否启用记忆系统（对应 `--memory none|openviking`）
4. 是否启用代码执行能力（对应 `--sandbox none|docker|firecracker`）
5. 如果启用代码执行，Agent 自动探测平台是否支持 `firecracker`，否则改用 `docker`
6. 是否需要自建搜索和抓取能力（对应 `--web-tools builtin|self-hosted`）
7. 是否安装复杂 Console（对应 `--console lite|full`）
8. 是否启用浏览器模块（对应 `--browser off|on`）

关于第 6 个问题的补充说明：

- `builtin` 表示使用第三方 API（Tavily + Firecrawl/Jina），零运维但有 API 成本
- `self-hosted` 表示安装 `searxng` + `firecrawl`，无 API 依赖但有额外资源消耗、IP 暴露风险和爬取速度限制
- Agent 必须在此处向用户说明两者的 trade-off

注意：

- 不要先问用户几十个技术细节
- 不要把实现细节暴露成选择题
- 例如 Redis database 分配、compose profile 名称、内部端口细分，都不应进入提问流程

### 6.2 Agent 输出结果

Agent 最终应该调用 `setup.sh` 的 parser 参数，而不是维护额外的安装结果文件。

例如：

```bash
./setup.sh install \
  --profile standard \
  --mode self-hosted \
  --memory none \
  --sandbox none \
  --console lite \
  --browser off \
  --web-tools builtin \
  --non-interactive
```

或：

```bash
./setup.sh install \
  --profile full \
  --mode self-hosted \
  --memory openviking \
  --sandbox docker \
  --console full \
  --browser off \
  --web-tools self-hosted \
  --non-interactive
```

Agent 不应该直接重写 Compose 规则本身。

## 7. setup.sh 的职责边界

### 7.1 必须做的事

`setup.sh` 只做以下命令：

```bash
./setup.sh install
./setup.sh doctor
./setup.sh status
./setup.sh upgrade
./setup.sh uninstall
```

### 7.2 install 行为

`./setup.sh install` 支持两种入口：

- 交互模式：用户回答几个固定问题
- 非交互模式：Agent 或高级用户直接传 parser 参数

建议的 parser 参数：

```bash
./setup.sh install \
  --profile standard|full \
  --mode self-hosted|saas \
  --memory none|openviking \
  --sandbox none|docker|firecracker \
  --console lite|full \
  --browser off|on \
  --web-tools builtin|self-hosted \
  --gateway on|off \
  --non-interactive
```

交互模式与非交互模式必须走同一套校验与默认值逻辑。

默认行为：

1. 检测宿主条件
2. 生成缺失密钥
3. 解析问答结果或 CLI 参数
4. 生成或补全 `.env`
5. 启动目标模块集合
6. 等待健康检查通过
7. 打印入口地址
8. 打印下一步提示

### 7.3 首次安装后的管理员初始化入口

第一次安装成功后，如果系统中还不存在 `platform_admin`，`setup.sh` 应生成一次性初始化入口。

目标：

- 不要求用户先自行注册账号再回填 user_id
- 首次部署后立刻能完成管理员创建或密码设置

建议行为：

1. 安装完成后生成一次性 bootstrap token
2. 输出一个临时 URL，例如：`http://localhost:19000/bootstrap/<token>`
3. 用户通过该 URL 创建首个管理员账号或设置管理员密码
4. token 使用一次即失效
5. token 超时自动失效
6. 一旦系统已有 `platform_admin`，该入口不再生成

这是首次安装体验的一部分，不应要求用户手动查数据库或额外编辑 `.env`。

#### 7.3.1 当前实现缺口

当前代码中的 admin bootstrap 机制通过环境变量 `ARKLOOP_BOOTSTRAP_PLATFORM_ADMIN` 实现，流程是：

1. 用户先注册一个普通账号
2. 拿到该账号的 UUID
3. 手动写入 `.env` 的 `ARKLOOP_BOOTSTRAP_PLATFORM_ADMIN` 字段
4. 重启 API 服务触发 `bootstrapPlatformAdminOnce()`
5. 该函数检查 `platform_settings["bootstrap.platform_admin.user_id"]` 是否存在
6. 不存在则将指定用户提升为 `platform_admin`

问题：

- 用户需要先注册、查 UUID、改配置、重启服务，体验极差
- 对非技术用户来说几乎不可能完成
- 没有 Web UI 入口，强依赖手动操作

#### 7.3.2 目标实现

需要新增 API 端点和对应的前端页面：

API 端点：

```text
POST /v1/bootstrap/init          # 生成 bootstrap token（仅 setup.sh 调用）
GET  /v1/bootstrap/verify/:token # 验证 token 有效性
POST /v1/bootstrap/setup         # 通过 token 创建管理员（username + password）
```

流程：

1. `setup.sh install` 完成后，调用 `POST /v1/bootstrap/init` 生成 token
2. token 存入 `platform_settings["bootstrap.token"]`，附带过期时间
3. `setup.sh` 在终端打印 `http://localhost:19000/bootstrap/<token>`
4. 用户访问该 URL，Console Lite 渲染管理员设置页面
5. 用户填写 username + password，提交到 `POST /v1/bootstrap/setup`
6. 后端创建用户、赋予 `platform_admin` 角色、写入 bootstrap marker、销毁 token
7. 后续访问 bootstrap URL 返回 404

安全约束：

- token 默认 30 分钟过期
- 只允许使用一次
- 如果系统已有 `platform_admin`，`/v1/bootstrap/init` 返回 409
- token 不经 Gateway 缓存

### 7.4 doctor 行为

`./setup.sh doctor` 只做检测，不做修改。

至少输出这些结果：

- 平台类型：Linux / macOS / WSL2
- Docker 是否可用
- Compose 是否可用
- Docker socket 路径
- 是否检测到 KVM
- 默认端口是否冲突
- 当前已启动模块

### 7.5 不要做成人工专用大菜单

`setup.sh` 可以承载问答，但问答必须只是 parser 的一个前端。

原因：

- 多模块、多平台分支在 bash 中很难维护
- 以后 Bridge 上线后，问答逻辑仍需要复用同一套规则
- Agent 需要可编程调用入口，不能只能“模拟按键”

## 8. Installer Bridge 的定位

### 8.1 为什么必须要 Bridge

Bridge 是目标架构中的正式组件，不是可有可无的补丁。

原因只有一个：

```text
Console 需要从“状态面板”升级为“真正的一键执行面板”
```

如果你要：

- 点击安装 OpenViking
- 点击安装 Sandbox
- 点击安装 Browser
- 点击切换到复杂 Console
- 点击升级 Arkloop
- 在 Console 内看到实时进度

那么就需要 Bridge。

所以本文把 Bridge 作为明确需求写入；开发顺序可以后置，但需求本身不能省略。

### 8.2 Bridge 的核心职责

Bridge 只负责 Arkloop 管理范围内的动作：

- 检测宿主环境
- 拉镜像、启动模块、停止模块、重启模块
- 读取和写入 Arkloop 的本地安装状态
- 写 Arkloop 自己的连接配置
- 健康检查
- 升级 Arkloop 自己的组件

Bridge 不负责：

- 安装 Docker Desktop
- 统一管理所有第三方系统的内部模型配置
- 执行任意 shell

### 8.3 第一版最小 API

```text
GET  /healthz
GET  /v1/platform/detect
GET  /v1/modules
GET  /v1/modules/{id}
POST /v1/modules/{id}/actions
GET  /v1/operations/{id}/stream
POST /v1/system/upgrade
```

动作只接受结构化请求，例如：

```json
{ "action": "install" }
{ "action": "start" }
{ "action": "stop" }
{ "action": "restart" }
{ "action": "configure_connection", "params": { "base_url": "http://openviking:19010", "api_key": "***" } }
{ "action": "bootstrap_defaults" }
```

UI 可以根据模块与当前状态使用不同按钮文案，但底层仍然复用同一套模块动作语义：

- “安装 Sandbox” -> `POST /v1/modules/{sandbox-id}/actions` + `{ "action": "install" }`
- “安装 Browser” -> `POST /v1/modules/browser/actions` + `{ "action": "install" }`
- “切换到复杂 Console” -> 未安装时对 `console` 模块执行 `install`；已安装时直接打开 `console` 入口

Bridge 不接受：

```json
{ "action": "shell", "params": { "cmd": "docker exec ..." } }
```

### 8.4 安全约束

- 只监听本机回环地址或 Unix Socket
- 不经 Gateway 反代
- 不接受任意 shell
- 只允许访问 Arkloop 项目资源
- 所有写操作必须有审计日志
- 高风险动作需要显式确认

## 9. Memory System 的正确边界

这是本文最重要的约束。

### 9.1 Arkloop 管什么

Arkloop 管的是：

- 这个 memory provider 是否已安装
- 是否在运行
- Arkloop 是否能连接到它
- Arkloop 用什么运行时密钥接它
- 是否完成 Arkloop 所需的默认初始化

### 9.2 Arkloop 不管什么

Arkloop 不承诺统一管理这些东西：

- 所有 memory provider 的模型配置
- 所有 provider 的 embedding / rerank / index 细项
- 各 provider 私有的高级参数

也就是说，Bridge 不应该有一个通用接口叫：

```text
set_memory_model_config(provider, ...)
```

这类抽象会很快失真，因为不同 memory system 的内部语义根本不同。

### 9.3 允许的例外：bootstrap_defaults

Bridge 可以对少数 provider 提供：

```text
bootstrap_defaults
```

它的语义不是“全面接管 provider 后台配置”，而是：

```text
把 provider 初始化到 Arkloop 可用的默认状态
```

这一步只做默认值，不做完整后台管理。

### 9.4 OpenViking 的推荐策略

对 OpenViking，第一版只定义四个动作：

```text
memory.openviking.install
memory.openviking.configure_connection
memory.openviking.bootstrap_defaults
memory.openviking.health
```

语义：

- `install`：安装或启动 OpenViking 模块
- `configure_connection`：写 Arkloop 侧连接信息
- `bootstrap_defaults`：如果 OpenViking 支持，就写入 Arkloop 运行所需的默认配置
- `health`：验证 Arkloop 到 OpenViking 的可用性

不要定义：

```text
memory.openviking.set_model_config
```

因为这会把 Bridge 推向“外部系统后台”的方向。

### 9.4.1 OpenViking 与 Modal/Provider 系统集成调研

当前 OpenViking 的配置通过 `ov.conf` 文件管理（embedding provider、VLM provider、storage backend 等）。`bootstrap_defaults` 只负责写入 Arkloop 可用的默认值，不接管完整配置。

但这存在一个体验缺口：如果用户想修改 OpenViking 的 embedding model（比如切换到不同的 provider），当前只能手动编辑 `ov.conf` 并重启容器。

调研方向：

1. **OpenViking 是否支持运行时配置变更**：调研 OpenViking API 是否提供配置修改端点，使得 Arkloop 可以在不重启容器的情况下调整配置
2. **与 Arkloop Provider 系统对齐**：Arkloop 已有 Model Provider 管理（`platform_settings` 中的 `llm.*` 配置）。如果 OpenViking 的 embedding model 选择能接入 Arkloop 的 Provider 体系，用户就可以在 Console 的 Models 页面统一管理所有模型配置，而不是分别在两个地方操作
3. **配置传递机制**：如果 OpenViking 支持 API 配置变更，Bridge 可以增加一个 `configure` 动作，将 Arkloop 侧的 provider 配置同步到 OpenViking

约束：

- 这不等于“Arkloop 接管 OpenViking 的完整后台”
- 只同步 Arkloop 已知且需要的配置项（如 embedding provider / model）
- OpenViking 的高级参数（index 策略、rerank 权重等）仍由 OpenViking 自身管理
- 如果 OpenViking 不支持运行时配置变更，则此方向不推进，保持 `bootstrap_defaults` 即可

此调研为后续 PR 的前置任务。

### 9.5 Sandbox 的 bootstrap 语义

Sandbox 也应视为 `bootstrapSupported=true`。

原因不是因为 Sandbox 内部配置简单，而是因为 Arkloop 侧的默认初始化是明确且可判定的：

- 选择 provider
- 校验宿主前置条件
- 写 Arkloop 侧连接配置
- 启动对应模块
- 执行健康检查
- 验证 Worker 到 Sandbox 的连通性

其中：

- `sandbox-firecracker` 的 bootstrap 可能需要高权限确认
- `sandbox-docker` 的 bootstrap 前提是用户已经安装 Docker Desktop 或可用 Docker Engine

也就是说，`sandbox-docker` 的难点在宿主前置条件，不在 Arkloop 自身的 bootstrap。

Installer Bridge 在 Sandbox 场景下的职责是 Build 这个 Sandbox 镜像并启动服务，不负责安装 Docker Desktop。Docker Desktop 是宿主前置条件（Section 4.1 A 类边界），Installer Bridge 只假设 Docker Engine 已可用。

Sandbox 的 `docker compose up` 与 Bridge 的 `install` 动作本质上是同一操作，Bridge 只是提供了 Console 可调用的 HTTP 接口。

### 9.5.1 Browser 模块与 Sandbox 的关系

Browser Automation 的架构已决定（参见 browser-automation-architecture 文档）：

- Browser 作为 Sandbox 的 `browser` tier 接入，使用独立 Docker 镜像（Node + Chromium，不含 Python）
- 引擎选用 `agent-browser`（Rust CLI + Node.js daemon），Worker 侧暴露单个 `browser` 工具
- Session 管理复用现有 `share_scope` 模型（run / thread / workspace / org）
- Browser 的安装路径与 Sandbox 一致：前置条件是 Docker，Bridge 负责 Build 镜像和启动

### 9.6 root key 与 runtime key

OpenViking 这类系统如果存在 root/admin key，应按两层使用：

- 安装和初始化阶段：允许使用 root/admin key
- 运行阶段：优先使用 runtime key 或 scoped key

第一版可以暂时继续使用 root key 接入，但规格上要允许未来切到 runtime key。

## 10. 模块能力模型

建议每个可管理模块声明一组能力，而不是暴露大量特例逻辑。

```typescript
interface ModuleCapability {
  installable: boolean
  configurable: boolean
  healthcheck: boolean
  bootstrapSupported: boolean
  externalAdminSupported: boolean
  privilegedRequired: boolean
}
```

建议的首批模块能力：

| 模块 | installable | configurable | healthcheck | bootstrap | external admin | privileged |
|------|-------------|--------------|-------------|-----------|----------------|------------|
| `openviking` | true | true | true | true | true | false |
| `sandbox-docker` | true | true | true | true | false | false |
| `sandbox-firecracker` | true | true | true | true | false | true |
| `browser` | true | true | true | false | false | false |
| `console` | true | false | true | false | false | false |
| `pgbouncer` | true | true | true | false | false | false |
| `seaweedfs` | true | true | true | true | false | false |
| `searxng` | true | true | true | true | false | false |
| `firecrawl` | true | true | true | true | false | false |

`bootstrapSupported=true` 仅表示“支持默认初始化”，不表示“Arkloop 能接管完整后台配置”。

## 11. Console 里的安装体验

### 11.1 第一版

Console 先做成模块状态页，不强依赖 Bridge。

在 Lite 中，这个模块状态页放在 `Settings > System`。`Tools` 页只负责运行时 provider 与连接配置；如果 Sandbox / Browser / Memory 模块未安装，`Tools` 页只显示简短提示并引导用户前往 `Settings > System`。

每个模块只展示：

- 未安装
- 已安装但未连接
- 待初始化
- 运行中
- 已停止
- 异常

每个模块只提供最少动作：

- 安装
- 配置连接
- 初始化
- 启动
- 停止
- 重启
- 查看日志

具体按钮标签可以按产品语义显示，但动作模型不变：

- Sandbox：显示“安装 Sandbox”
- Browser：显示“安装 Browser”
- Full Console：显示“切换到复杂 Console”

其中 `Full Console` 不是特例流程，只是 `console` 模块的产品化按钮文案。

### 11.2 Bridge 不可用时的降级行为

在开发过渡期，或本机 Bridge 未运行时：

- “安装”按钮退化为“复制命令”
- “初始化”按钮退化为“复制给 Agent 的 Prompt”
- “升级”按钮退化为“显示升级命令”
- “切换到复杂 Console”按钮退化为“复制安装/打开 Console 的命令或 Prompt”

这样 Console 可以先上线，但不改变 Bridge 属于目标架构正式组件这一结论。

### 11.3 Lite 与 Full Console 的关系

- 自部署默认只安装 `console-lite`
- `console-lite` 中提供“切换到复杂 Console”的入口
- 如果已安装 `console`，则显示跳转入口
- 不在 Lite 中塞入大量解释性文案

## 12. 升级策略

升级也遵循同一边界。

### 12.1 第一版

第一版只支持：

- 检查当前版本
- 检查可升级版本
- 生成升级命令
- 生成给 Agent 的升级 Prompt

### 12.2 第二版

Bridge 上线后，再支持：

- 拉取新镜像
- 重建 Arkloop 模块
- 执行迁移
- 健康检查
- 失败时停在明确错误状态

第一版不要承诺“完全自动回滚”，避免过度设计。

## 13. Roadmap

以 Pull Request 为粒度拆分，每个 PR 是一个可独立合并的交付单元。

### PR1: 模块定义与 Compose 重构

范围：

- 创建 `install/modules.yaml`，定义所有模块的元数据、依赖、平台约束、能力集、默认值。SaaS 模式下的差异（如 pgbouncer/seaweedfs 默认启用）通过 profile 标记在 modules.yaml 中体现
- 重构 `compose.yaml`：移除 `redis_gateway` service，改为单 Redis 实例 + database number 隔离
- 重构 `compose.yaml`：`seaweedfs` 移入 `s3` profile（已有），确认 `pgbouncer` 在独立 profile 中
- 新增 `searxng` 和 `firecrawl`（视调研结果）的 compose profile。代码层面已有兼容，仅需部署层面集成
- 更新 `.env.example`：反映新的默认值（`ARKLOOP_STORAGE_BACKEND=filesystem`、`ARKLOOP_API_DB_POOL_MAX_CONNS=10`、`ARKLOOP_GATEWAY_REDIS_URL` 指向 db1）
- Gateway 代码适配：确认 `ARKLOOP_GATEWAY_REDIS_URL` fallback 到 `ARKLOOP_REDIS_URL` 时正常工作
- 调研 SearXNG 是否已内置网页抓取能力（决定 `firecrawl` 是否需要独立部署）

前置依赖：无

### PR2: setup.sh 核心实现

范围：

- 实现 `setup.sh` 的 5 个命令：`install`、`doctor`、`status`、`upgrade`、`uninstall`
- 统一 parser：交互问答 + CLI flags + 默认值，三种输入共用同一套校验逻辑
- `install` 命令的完整参数集：`--profile`、`--mode`、`--memory`、`--sandbox`、`--console`、`--browser`、`--web-tools`、`--gateway`、`--non-interactive`
- pre-flight 检测（Docker、Compose、端口冲突、KVM）
- 密钥自动生成（JWT secret、Redis password、PostgreSQL password）
- `.env` 生成与补全
- 健康检查等待逻辑
- `doctor` 输出所有诊断项

前置依赖：PR1

### PR3: Admin Bootstrap Token 机制

范围：

- API 新增端点：`POST /v1/bootstrap/init`、`GET /v1/bootstrap/verify/:token`、`POST /v1/bootstrap/setup`
- `platform_settings` 中存储 bootstrap token + 过期时间
- Console Lite 新增 Bootstrap 页面（`/bootstrap/:token` 路由）
- 页面功能：设置管理员 username + password
- setup.sh 安装完成后调用 `/v1/bootstrap/init` 并打印 URL
- 兼容现有 `ARKLOOP_BOOTSTRAP_PLATFORM_ADMIN` 环境变量方式（不删除，作为 fallback）

前置依赖：PR2

### PR4: installation.md 与 Agent 安装入口

范围：

- 创建 `docs/installation.md`，面向 Agent 的结构化安装指引
- 包含：环境探测指令、固定问答列表、参数映射表、setup.sh 调用模板
- 参考 oh-my-opencode 的 installation.md 模式：For Humans 段 + For LLM Agents 段
- Agent 调用 `setup.sh install --non-interactive ...` 完成安装
- 异常处理指引（pre-flight 失败、端口冲突、Docker 不可用等常见问题）

前置依赖：PR2

### PR5: Console Lite 模块状态页

范围：

- Console Lite 新增 `Settings > System` 页面
- 模块状态展示：未安装 / 已安装未连接 / 待初始化 / 运行中 / 已停止 / 异常
- 每个可选模块的入口按钮（安装 / 配置连接 / 初始化 / 启停 / 查看日志）
- Bridge 不可用时降级为"复制命令"或"复制 Agent Prompt"
- Full Console 安装入口（"切换到复杂 Console"按钮）

前置依赖：PR3

### PR6: Installer Bridge 最小版

范围：

- Bridge 服务骨架（Go service，监听 loopback / Unix Socket）
- API 实现：`/healthz`、`/v1/platform/detect`、`/v1/modules`、`/v1/modules/{id}`、`/v1/modules/{id}/actions`、`/v1/operations/{id}/stream`
- 支持动作：`install`、`start`、`stop`、`restart`、`configure_connection`、`bootstrap_defaults`
- 第一批模块支持：`openviking`、`sandbox-docker`、`sandbox-firecracker`、`console`、`searxng`、`firecrawl`
- 安全约束：loopback only、审计日志、拒绝任意 shell
- Console Lite 的模块状态页对接 Bridge API（替换降级行为）

前置依赖：PR5

### PR7: OpenViking Provider 集成调研与实施

#### 调研结论

**运行时配置变更：不支持。** OpenViking 的配置（`ov.conf`）在启动时由 `OpenVikingConfigSingleton`（带 `threading.Lock`）一次性加载为线程安全的单例，之后不可变更。具体证据：

- ❌ 无配置修改 API 端点 — 96 个端点（13 个 Router）中，Admin 类端点仅覆盖账户/用户/角色/API Key 的 CRUD，不涉及任何配置变更
- ❌ 无热加载机制 — 无 file watcher、无 SIGHUP 处理、无 reload 端点
- ❌ 无运行时环境变量覆盖 — Embedding/VLM 配置仅在初始化时读取
- 配置解析链：`--config` 路径 → `OPENVIKING_CONFIG_FILE` 环境变量 → `~/.openviking/ov.conf` → `/etc/openviking/ov.conf`

**结论：必须采用「配置文件生成 + 容器重启」方案。**

#### OpenViking 配置架构

OpenViking 的配置系统为启动时单次加载模式：

1. **配置加载**：进程启动时按优先级链解析 `ov.conf`，构建 `OpenVikingConfigSingleton`
2. **服务初始化**：`OpenVikingService.__init__()` 阶段根据配置创建 Embedding 和 VLM 实例并缓存
3. **运行时锁定**：配置单例持有 `threading.Lock`，仅用于线程安全读取，不支持写入
4. **无插件架构**：`ParserRegistry.register_custom()` 支持解析器运行时扩展，但 Embedding/VLM Provider 不可插拔，变更需修改代码并重启

#### Embedding 模型能力

支持 4 个 Provider、8 种组合：

| Provider | 向量类型 | 说明 |
|---|---|---|
| OpenAI | Dense | 标准 OpenAI embedding API |
| Volcengine | Dense / Sparse / Hybrid | 火山引擎向量服务 |
| VikingDB | Dense / Sparse / Hybrid | 字节 VikingDB 原生向量 |
| Jina | Dense | Jina AI embedding |

约束：

- Embedder 在 `OpenVikingService.__init__()` 阶段创建，缓存于 `_embedder` 字段
- 初始化后不可通过 API 变更，仅能通过修改 `ov.conf` 并重启生效
- Hybrid 模式同时生成 Dense + Sparse 向量，适合混合检索场景

#### VLM 能力

支持 3 个 Provider：

| Provider | 说明 |
|---|---|
| volcengine | 火山引擎视觉语言模型 |
| openai | OpenAI GPT-4V 等 |
| **litellm** | **通用代理层**，支持 15+ 后端（Claude、Gemini、Ollama、Azure、Bedrock 等） |

约束：

- VLM 为惰性单例，首次使用时创建并缓存于 `_vlm_instance`
- 不可通过 API 变更，需重启生效
- **litellm Provider 是关键优势**：作为 catch-all 代理层，可路由到绝大多数 LLM 后端，建议作为默认推荐选项

#### Arkloop 现有集成现状

- Arkloop Worker 使用 6 个 OV 端点：Find、Content、Sessions CRUD、Commit、FS Delete
- 认证方式：单一 root API Key（`X-API-Key` header）
- 多租户隔离：通过请求头传递（`X-OpenViking-Account`、`X-OpenViking-User`、`X-OpenViking-Agent`）
- 配置双通道：环境变量（`ARKLOOP_OPENVIKING_BASE_URL` / `ROOT_API_KEY`）+ 数据库（`tool_provider_configs` 表）
- Provider 目录已注册 `memory.openviking`（声明 `RequiresBaseURL` + `RequiresAPIKey`）
- Console 已有 `MemoryConfigPage.tsx` 管理 OV 连接设置

#### 推荐实施方案

由于运行时配置变更不可行，采用 **「Bridge 生成配置 + 容器重启」** 方案：

1. **Bridge `configure` 动作**：接收 Arkloop Provider 配置参数，渲染 `ov.conf` 模板并写入 OpenViking 容器的配置挂载卷
2. **容器重启触发**：Bridge 通过 Docker API 执行 OpenViking 容器 restart，使新配置生效
3. **Console Lite UI**：在 Models 页面暴露 Embedding 模型选择（Provider + 模型名）和 VLM 选择（推荐 litellm）
4. **litellm 优先策略**：VLM 配置默认推荐 litellm Provider，用户只需填写目标后端的 API Key 和模型名即可接入多种 LLM

配置变更流程：

```
Console Lite UI → Arkloop API (保存 provider config)
               → Bridge /v1/modules/openviking/actions { action: "configure" }
               → Bridge 渲染 ov.conf 模板 → 写入挂载卷
               → Bridge 调用 Docker API restart openviking 容器
               → Bridge 轮询 /health 确认 OV 恢复就绪
               → 返回操作结果
```

#### 具体实施范围

**Bridge 侧：**

- 实现 `openviking` 模块的 `configure` 动作处理器
- 实现 `ov.conf` 模板渲染逻辑（支持 Embedding Provider 选择、VLM Provider 选择、API Key 注入）
- 实现配置写入 → 容器重启 → 健康检查轮询的完整流程
- 配置变更审计日志记录

**Console Lite 侧：**

- Models 页面增加 Embedding 模型配置区域（Provider 下拉 + 模型名输入 + API Key）
- Models 页面增加 VLM 配置区域（Provider 下拉，litellm 为默认推荐 + 后端模型名 + API Key）
- 配置提交后调用 Bridge `configure` 动作，展示操作进度和结果
- 配置变更时提示用户「将触发 OpenViking 重启，期间记忆服务短暂不可用」

**配置模板：**

- 维护 `ov.conf` 的 Go template，覆盖 Embedding 和 VLM 配置段
- 保留用户自定义配置段不被覆盖（merge 策略）
- 配置校验：写入前验证必填字段和 Provider 合法性

前置依赖：PR6 + 本调研结论

### PR8: SaaS 部署模式

范围：

- setup.sh `--mode saas` 差异行为实现（PGBouncer 默认启用、S3 默认启用、Turnstile 默认开启）
- Gateway 公网安全加固配置模板
- TLS 终结配置指引文档
- CI/CD pipeline 集成指引

前置依赖：PR2

### PR9: 升级与系统维护

范围：

- Bridge `POST /v1/system/upgrade` 实现
- 版本检查、镜像拉取、模块重建、迁移执行、健康检查
- setup.sh `upgrade` 命令完整实现
- Console Lite 升级入口

前置依赖：PR6

## 14. 最终结论

Installer Bridge 是值得做的，但不该成为第一天就承诺包打天下的大系统。

更稳的路线是：

1. 先把模块定义和基础设施配置收敛（PR1）
2. 先把 `setup.sh` 做好，统一 parser（PR2）
3. 先把首次管理员初始化体验补齐（PR3）
4. 先让 `installation.md` 成为 Agent 主入口（PR4）
5. Console Lite 先做模块状态页，降级可用（PR5）
6. Bridge 作为正式组件实现（PR6）
7. 再逐步扩展 Provider 集成、SaaS 模式、系统维护（PR7-PR9）

对 memory system，Arkloop 的边界必须明确：

- Arkloop 管安装、连接、运行状态、默认初始化
- Arkloop 不统一接管外部系统的完整模型配置
- 允许的例外是：如果外部系统支持 API 配置变更，Arkloop 可以同步自身已知的配置项

这条边界如果不守住，后续 OpenViking、其他 memory system、Firecrawl 一类模块都会把 Bridge 拖进持续膨胀的特例泥潭。
