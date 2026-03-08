---
---

# Installer Bridge 与自部署安装架构

本文重新定义 Arkloop 自部署安装链路，目标是降低选择成本、收紧权限边界，并给 Agent、CLI、Console 三种入口提供同一套安装语义。

## 1. 设计目标

本文只解决四件事：

1. 自部署用户如何安装 Arkloop
2. 哪些模块默认安装，哪些模块可选
3. Console 能管到哪里，不能管到哪里
4. Installer Bridge 是否需要，以及它的最小职责

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
| `pgbouncer` | 连接池 |
| `redis` | 队列与缓存 |
| `seaweedfs` | S3 兼容对象存储 |
| `migrate` | 数据迁移 |
| `api` | 控制面 API |
| `worker` | 执行面 |

说明：

- `seaweedfs` 视为平台基座，不再作为用户决策项
- `redis` 视为平台基座，不再作为用户决策项

### 3.2 Standard 默认入口

以下模块属于默认自部署入口：

| 模块 | 默认值 | 说明 |
|------|--------|------|
| `gateway` | 开启 | 默认对外入口 |
| `console-lite` | 开启 | 自部署默认控制台 |
| `console` | 关闭 | 复杂 Console，增量安装 |

说明：

- `gateway` 在产品层面默认开启
- `console-lite` 是自部署默认控制台
- `console` 只作为升级选项，不进入首次安装主流程

### 3.3 Optional 模块

| 模块 | 默认值 | 说明 |
|------|--------|------|
| `openviking` | 关闭 | 记忆系统，可选扩展 |
| `sandbox-docker` | 关闭 | macOS / Windows / 无 KVM 环境 |
| `sandbox-firecracker` | 关闭 | Linux + KVM 环境 |
| `browser` | 关闭 | 浏览器模块，可选扩展 |

说明：

- `sandbox-docker` 与 `sandbox-firecracker` 互斥
- `openviking` 不进入首次安装默认路径
- `browser` 不进入首次安装默认路径

### 3.4 不暴露给用户的内部细节

这些属于实现细节，不作为首次安装问题抛给用户：

| 项 | 策略 |
|----|------|
| `redis_gateway` 是否独立 | 对用户透明，由模块实现决定 |
| `gateway` 依赖的 Redis 拆分策略 | 对用户透明 |
| Compose 具体 service 名 | 对用户透明 |

用户只决定“是否启用网关能力”，不决定“网关内部 Redis 拆不拆”。

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

1. 你要默认自部署还是高级部署
2. 是否启用记忆系统
3. 是否启用代码执行能力
4. 如果启用代码执行，当前平台是否支持 `firecracker`，否则改用 `docker`
5. 是否安装复杂 Console
6. 是否启用浏览器模块

注意：

- 不要先问用户几十个技术细节
- 不要把实现细节暴露成选择题
- 例如 `redis_gateway`、compose profile 名称、内部端口细分，都不应进入提问流程

### 6.2 Agent 输出结果

Agent 最终应该调用 `setup.sh` 的 parser 参数，而不是维护额外的安装结果文件。

例如：

```bash
./setup.sh install \
  --profile standard \
  --memory none \
  --sandbox none \
  --console lite \
  --browser off \
  --non-interactive
```

或：

```bash
./setup.sh install \
  --profile full \
  --memory openviking \
  --sandbox docker \
  --console full \
  --browser off \
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
  --memory none|openviking \
  --sandbox none|docker|firecracker \
  --console lite|full \
  --browser off|on \
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
2. 输出一个临时 URL，例如：`http://localhost:8000/bootstrap/<token>`
3. 用户通过该 URL 创建首个管理员账号或设置管理员密码
4. token 使用一次即失效
5. token 超时自动失效
6. 一旦系统已有 `platform_admin`，该入口不再生成

这是首次安装体验的一部分，不应要求用户手动查数据库或额外编辑 `.env`。

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
{ "action": "configure_connection", "params": { "base_url": "http://openviking:1933", "api_key": "***" } }
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

## 13. 推荐落地顺序

### 第一步：收紧模块定义

先增加：

- `install/modules.yaml`

并定义统一 setup parser：

- 交互问答输入
- CLI 参数输入
- 默认值与校验规则

把“模块、依赖、平台约束、默认值、能力集”收敛到同一处，把“安装选择解析”收敛到同一个 parser。

### 第二步：收紧 setup.sh

让 `setup.sh` 只承担：

- `install`
- `doctor`
- `status`
- `upgrade`
- `uninstall`

同时补上：

- 交互问答入口
- 非交互 parser 参数入口
- 首次管理员初始化临时 URL

并默认安装 `standard`。

### 第三步：补 installation.md

让 Agent 能基于固定问题生成 `setup.sh install ...` 参数，再调用 `setup.sh`。

### 第四步：最小版 Bridge

最小版至少支持：

- 平台探测
- OpenViking 安装
- OpenViking 连接配置
- OpenViking 默认初始化
- Sandbox 启用与默认初始化
- Browser 安装与健康检查
- `console` 模块安装与打开入口
- Arkloop 自身升级

### 第五步：Console 先做状态页

先做：

- 模块状态
- Sandbox / Browser / Full Console 三个入口按钮
- 复制命令
- 复制给 Agent 的 Prompt
- 健康检查结果

Bridge 上线后，再把“复制命令”逐步替换为真一键动作。

### 第六步：再扩展其他模块

在 OpenViking 和 Sandbox 路径稳定后，再接 Browser、更多 memory system、更多第三方模块。

## 14. 最终结论

Installer Bridge 是值得做的，但不该成为第一天就承诺包打天下的大系统。

更稳的路线是：

1. 先把安装语义统一
2. 先把 `setup.sh` 收薄，并统一 parser
3. 先把首次管理员初始化临时入口补齐
4. 先让 `installation.md` 成为 Agent 主入口
5. 把 Bridge 作为正式组件实现进 roadmap
6. 再让 Console 从状态页升级为真一键执行面板

对 memory system，Arkloop 的边界必须明确：

- Arkloop 管安装、连接、运行状态、默认初始化
- Arkloop 不统一接管外部系统的完整模型配置

这条边界如果不守住，后续 OpenViking、其他 memory system、Firecrawl 一类模块都会把 Bridge 拖进持续膨胀的特例泥潭。
