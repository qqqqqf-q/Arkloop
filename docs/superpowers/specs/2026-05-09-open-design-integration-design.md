# Open Design 正式打包集成设计

## 背景

Arkloop Web 侧已经具备基础插件壳、插件工作区路由以及桌面嵌入浏览器能力；Open Design 侧则提供了适合正式集成的 headless 打包产物与运行约定：

- 运行入口为 `resources/bin/node` + `bundle/node_modules/@open-design/packaged/dist/headless.mjs`
- 运行时根目录位于 `~/.arkloop/integrations/open-design`
- 进程启动完成后会写入 `data/namespaces/default/runtime/web-root.json`
- `web-root.json` 中的 `url` 是 Open Design Web UI 的真实入口

当前 Arkloop 仓库中还能看到一条未接完的桌面“managed local app”设计线索，但它基于开发态双进程 `daemon`/`web` 模型，与本次目标不一致。本设计明确以正式打包产物集成为准，不再以本地仓库联调方案作为主路径。

## 目标

- 将 Open Design 作为 Arkloop 桌面端内建的 `workspace` 插件入口暴露
- 由 Arkloop 桌面端负责启动、保活、停止 Open Design headless runtime
- 在 Arkloop 主工作区内承载 Open Design Web UI，而不是复用右侧 browser panel
- 基于 `web-root.json` 做就绪检测与服务地址发现
- 提供首版可用的 loading / failed / retry 体验，保证“能稳定打开和关闭”

## 非目标

- 不实现 Arkloop -> Open Design HTTP client
- 不实现 Open Design MCP 接入
- 不实现 Open Design 产物的自动构建、自动下载、自动更新
- 不实现多呈现模式切换；首版固定为主工作区完整承载
- 不兼容本地源码仓库双进程开发态 launcher 作为正式路径

## 用户体验

用户在 Arkloop 侧边栏的 workspace 区域看到 `Open Design` 插件入口。点击后：

1. Web 侧激活 Open Design 插件页
2. 桌面主进程确保 Open Design runtime 已启动
3. 启动成功后，桌面主工作区显示 Open Design 的 Web UI
4. 若启动中则显示 loading shell
5. 若启动失败或安装缺失则显示错误 shell，并提供重试入口

关闭或切换离开该插件时：

- 主区隐藏 Open Design 对应 `BrowserView`
- Open Design runtime 默认继续保活，避免频繁冷启动
- 桌面应用退出时统一停止 Open Design runtime

## 方案选择

### 方案 A：复用现有插件 browser panel

把 Open Design 当成普通 `embedded-browser` 插件，仅向现有浏览器面板注入 URL。

优点：

- 改动范围小
- 复用现有插件浏览器会话逻辑

缺点：

- 语义不匹配，Open Design 是完整应用而不是辅助浏览页
- 当前 panel 设计偏右侧附属视图，不适合作为主工作区完整承载
- 生命周期和错误态会与插件浏览器会话耦合，后续扩展困难

### 方案 B：workspace 插件 + 桌面受管应用主区承载

Web 侧仍使用现有插件体系提供入口，但 Open Design 的启动、就绪检测和主区渲染由桌面主进程托管。

优点：

- 与 Open Design headless 打包约定完全对齐
- 生命周期、日志、错误处理边界清晰
- 后续扩展 HTTP/MCP 接入时运行时归属稳定
- 与“完整应用嵌入 Arkloop 工作区”的产品语义一致

缺点：

- 需要补齐桌面主进程受管应用能力
- Web 与 Electron 主进程之间需要增加一层状态桥接

### 方案 C：只接后台 runtime

只做 headless 运行时和服务发现，不暴露 UI。

优点：

- 实现最快

缺点：

- 无法满足当前“工作区插件页”目标
- 后续仍需再做一轮 UI 集成

### 结论

采用方案 B。

## 架构概览

### 1. Web 插件入口层

在 `src/apps/web/src/plugins/registry.ts` 新增 `open-design` 内建插件定义：

- `nav.section = 'workspace'`
- 标题为 `Open Design`
- 首版视作桌面专属入口
- 不暴露 presentation 切换按钮

该插件不再走普通 `resolveBrowserUrl -> browser tabs` 模型，而是通过桌面桥接请求主进程确保受管应用运行，并由主进程把 Open Design UI 显示到主工作区。

### 2. 桌面受管应用层

在 Electron 主进程新增或恢复一套源码级 managed app 能力，最少包含：

- Open Design 安装目录解析
- headless 单进程启动与停止
- `web-root.json` 就绪探测
- 运行状态缓存
- 错误原因记录

这层只服务于 Open Design 首版也可以，但接口设计要允许后续承载其他本地集成应用。

### 3. 主区 BrowserView 承载层

在桌面主进程维护一个按 `appId` 复用的主区 `BrowserView` host：

- `show(appId, url, bounds)`
- `hide(appId)`
- `syncBounds(appId, bounds)`

Open Design 激活时主区显示对应 `BrowserView`，离开时隐藏但不销毁，以便后续快速恢复。

## 安装目录与运行约定

首版假设 Open Design 已由外部流程部署到：

```text
~/.arkloop/integrations/open-design/
  bundle/
  resources/
  data/
```

启动前必须校验以下关键路径存在：

- `resources/bin/node`
- `bundle/node_modules/@open-design/packaged/dist/headless.mjs`
- `resources/skills`
- `resources/design-systems`

若缺少关键文件，直接返回“未安装或安装不完整”错误态，不尝试启动。

## 进程模型

### 启动命令

Arkloop 启动 Open Design 使用单进程模式：

```bash
OD_NAMESPACE=default
OD_DATA_DIR=~/.arkloop/integrations/open-design/data
OD_RESOURCE_ROOT=~/.arkloop/integrations/open-design/resources
OD_WEB_OUTPUT_MODE=server

~/.arkloop/integrations/open-design/resources/bin/node \
  ~/.arkloop/integrations/open-design/bundle/node_modules/@open-design/packaged/dist/headless.mjs
```

### 生命周期

- 首次打开插件：若未运行则启动
- 再次进入插件：若已运行则复用
- 用户离开插件：仅隐藏主区 `BrowserView`
- 用户手动重试：必要时先 stop 再 restart
- Arkloop 桌面退出：发送 `SIGTERM`，超时后再强制结束

### 保活策略

首版采用“应用级保活”：

- Open Design 一旦启动，在 Arkloop 桌面生命周期内持续运行
- 避免用户频繁进入/退出插件造成冷启动抖动
- 后续如有资源压力，再增加空闲自动停止策略

## 就绪检测

### 探测方式

不使用老的双端口 HTTP health check，改为文件探测：

- 轮询 `OD_DATA_DIR/namespaces/default/runtime/web-root.json`
- 解析 JSON 并读取 `url`
- 验证 `url` 必须是合法 `http`/`https`

### 成功条件

满足以下条件才认为运行成功：

- `web-root.json` 已出现
- JSON 可解析
- `url` 存在且合法

### 失败条件

以下任一情况进入 failed 状态：

- 关键安装文件缺失
- 进程在 ready 前退出
- 超时仍未生成 `web-root.json`
- `web-root.json` 内容损坏或 `url` 非法

## 状态模型

桌面端对 Open Design 维护以下状态：

- `stopped`
- `starting`
- `running`
- `failed`

状态对象至少包含：

- `appId`
- `status`
- `pid`
- `webUrl`
- `lastError`
- `startedAt`

Web 侧只消费面向展示的最小状态：

- `idle`
- `loading`
- `ready`
- `error`

## Web 与桌面桥接

需要新增一组 desktop IPC / bridge 接口，示例职责如下：

- `ensureManagedApp(appId)`：确保应用已启动，返回状态与 `webUrl`
- `getManagedAppStatus(appId)`：返回当前运行状态
- `retryManagedApp(appId)`：失败后重新拉起
- `showManagedAppInMainArea(appId, url)`：把应用显示到主工作区
- `hideManagedAppInMainArea(appId)`：离开插件时隐藏主区视图

Web 侧插件页负责：

- 首次进入时调用 `ensureManagedApp('open-design')`
- 成功后请求显示主区应用
- 卸载或切换离开时请求隐藏主区应用
- 根据状态渲染 loading/error shell

## 主工作区展示语义

Open Design 首版固定为“主工作区完整承载”，不进入现有插件三态切换：

- 不走 `Page / Hybrid / Browser` 切换按钮
- 不打开右侧全局 browser panel
- 不复用插件浏览器 tab session

这是因为 Open Design 是完整应用，不是插件自己的辅助浏览器页面。这样也能避免与现有插件浏览器会话状态互相干扰。

## 错误处理

### 未安装

条件：

- 运行目录缺失
- bundle/resources 关键文件不全

表现：

- Web 侧显示“Open Design 未安装或安装不完整”
- 提供“重试检测”按钮

### 启动失败

条件：

- 子进程提前退出
- ready 文件超时未出现
- ready 文件损坏

表现：

- Web 侧显示错误摘要
- 保留重试按钮
- 主区不显示空白 BrowserView

### URL 非法

条件：

- `web-root.json.url` 不是合法 `http/https`

表现：

- 标记为 runtime 异常
- 阻止加载到 Electron `BrowserView`

## 安全约束

- `web-root.json.url` 必须做协议白名单校验，仅允许 `http:` / `https:`
- Open Design 主区 `BrowserView` 继续保持 `contextIsolation: true`、`nodeIntegration: false`、`sandbox: true`
- Open Design Web UI 在主区承载时，外部链接跳转沿用 Arkloop 既有安全策略，不允许无白名单协议直传系统外跳

## 代码落点

### Web

- `src/apps/web/src/plugins/registry.ts`
- 新增 Open Design 插件定义与入口页
- 视需要新增插件 loading / error shell 组件

### Desktop Main

- `src/apps/desktop/src/main/ipc.ts`
- 新增 managed app IPC
- 新增 managed app runtime 相关模块
- 新增主区 BrowserView 与 managed app 的协调逻辑

### Desktop Types / Config

- 如需为后续扩展保留接口，可在 desktop 类型层增加 managed app 状态定义
- 首版不要求把 Open Design 安装路径写入用户配置；直接使用固定运行目录约定

## 数据流

### 激活流程

1. 用户点击 `Open Design` 插件入口
2. Web 插件页请求 `ensureManagedApp('open-design')`
3. Desktop main 校验安装目录
4. 若未运行则 spawn headless runtime
5. 轮询 `web-root.json`
6. 解析出 `webUrl`
7. Web 插件页请求 `showManagedAppInMainArea('open-design', webUrl)`
8. Desktop main 在主工作区显示对应 `BrowserView`
9. Web shell 切换到 ready 状态

### 离开流程

1. 用户切换离开 Open Design 插件
2. Web 插件页请求 `hideManagedAppInMainArea('open-design')`
3. Desktop main 隐藏对应 `BrowserView`
4. runtime 继续保活

## 测试策略

### 单元测试

桌面层重点覆盖：

- 安装目录解析
- 启动命令与环境变量拼装
- `web-root.json` 轮询与解析
- 超时、坏 JSON、非法 URL、进程提前退出
- `BrowserView` show/hide/syncBounds 行为

Web 层重点覆盖：

- 插件入口可见
- 插件页首次进入触发 `ensureManagedApp`
- loading / error / ready 三态渲染
- 离开插件时调用 hide 接口

### 手工验证

- 已安装 open-design 产物时可正常打开
- 重复进入插件不会重复拉起进程
- 切出插件后主区恢复 Arkloop 正常内容
- Arkloop 退出时 Open Design 进程被停止

## 里程碑拆分

### M1：桌面运行时能力

- 安装目录解析
- 单进程启动/停止
- `web-root.json` readiness
- 状态模型与 IPC

### M2：主区承载

- 主区 `BrowserView` host
- show/hide/syncBounds
- 与 Arkloop 主工作区布局联动

### M3：Web 插件入口

- 插件注册
- loading/error shell
- 进入/离开时与桌面 IPC 对接

## 未来扩展

后续可在不推翻本设计的前提下继续增加：

- Open Design HTTP client
- Open Design MCP server 接入
- 安装器 / 自动部署 / 自动升级
- 空闲自动停止策略
- 集成应用列表与统一 runtime 管理页面
