# Arkloop 插件工作区设计

## 目标
- 在 `web` 桌面端主应用中引入插件机制。
- 插件入口放在全局左侧菜单中。
- 点击插件后，允许插件接管 `Sidebar` 右侧的整个工作区。
- 插件支持三种展示方式：内部 Web UI、嵌入浏览器容器、混合模式。
- 首期仅支持内建插件，不引入远程动态加载。

## 非目标
- 首期不支持插件替换整个应用壳层。
- 首期不支持远程分发、安装或卸载插件包。
- 首期不支持 Web 浏览器版完整等价能力；桌面端优先。

## 术语
- `App Shell`：应用壳层，包含全局 provider、窗口环境与外层布局。
- `Global Frame`：全局框架，包含左侧 `Sidebar` 与右侧工作区骨架。
- `Workspace Host`：`Sidebar` 右侧的整块工作区，默认承载聊天与设置页，也可被插件接管。
- `Plugin Workspace`：由插件宿主控制的工作区内容。
- `Sidecar`：工作区内的附加容器，首期主要指桌面端嵌入式浏览器容器。

## 设计原则
- 插件接管范围限定为 `Workspace Host`，不接管 `App Shell`。
- 菜单、路由、展示模式解耦。
- Electron 仅提供容器能力，不理解插件业务。
- 插件与浏览器容器之间通过会话映射层协作，不直接互相依赖。
- 首期优先最小可用能力，并为远程清单和更多容器类型预留接口。

## 壳层模型

```text
App Shell
├─ Global Frame
│  ├─ Sidebar
│  └─ Workspace Host
│     ├─ Default Workspace
│     └─ Plugin Workspace
└─ Desktop Container Capability
   └─ Electron BrowserView / Browser Tabs
```

- `Sidebar` 始终保留，插件从左侧菜单进入。
- `Workspace Host` 是插件可替换的最大边界。
- `Desktop Container Capability` 通过现有 `BrowserView` 与前端 `BrowserTabsProvider` 复用。

## 插件模型

```ts
type PluginShellMode = "plugin-main" | "plugin-workspace"

type PluginPresentation = "route" | "embedded-browser" | "hybrid"

type PluginDefinition = {
  id: string
  title: string
  desktopOnly?: boolean
  nav: {
    section: "primary" | "tools" | "workspace"
    order: number
    icon?: string
  }
  shell: {
    mode: PluginShellMode
  }
  presentation: {
    default: PluginPresentation
    supported: PluginPresentation[]
  }
  surfaces: {
    mount?: React.ComponentType<PluginComponentProps>
    resolveBrowserUrl?: (ctx: PluginContext) => Promise<string> | string
    browserPlacement?: "main" | "sidecar"
  }
  guards?: {
    featureFlag?: string
    desktopRequired?: boolean
  }
  lifecycle?: {
    onActivate?: (ctx: PluginActivateContext) => Promise<void> | void
    onDeactivate?: (ctx: PluginDeactivateContext) => Promise<void> | void
  }
}
```

### 语义
- `shell.mode = plugin-main`：插件替换主内容区，但沿用默认工作区框架。
- `shell.mode = plugin-workspace`：插件接管整个右侧工作区，自定义头部、分栏和 sidecar 布局。
- `presentation = route`：渲染 Arkloop 内部 React UI。
- `presentation = embedded-browser`：渲染浏览器容器，主区或 sidecar 由 `browserPlacement` 决定。
- `presentation = hybrid`：同时使用插件 React UI 与浏览器容器。

## 运行时组件

### PluginRegistry
- 保存首期内建插件定义。
- 负责插件发现、排序、过滤与 guards 判断。
- 后续可替换为 `manifest + implementation` 合并模型。

### PluginRuntime
- 管理当前激活插件、展示模式与轻量 UI 状态。
- 暴露统一入口 `openPlugin(pluginId)`。
- 负责插件切换时的生命周期调用。

### PluginHostPage
- 作为统一插件路由入口，建议路径为 `/plugins/:pluginId`。
- 根据 `pluginId` 查找插件定义。
- 根据 `shell.mode` 与 `presentation` 选择渲染器。

### PluginBrowserSession
- 维护 `pluginId -> browserTabId` 映射。
- 负责恢复插件对应的浏览器会话。
- 决定关闭插件时是否复用或销毁浏览器 tab。

## 与现有架构的接入点

### 左侧菜单
- 在现有 `Sidebar` 中增加一个独立插件分组。
- `Sidebar` 不直接处理“跳路由还是开浏览器”，只调用 `openPlugin(pluginId)`。
- 插件相关 UI 逻辑应抽到 `PluginSidebarSection`，避免继续扩大 `Sidebar` 文件复杂度。

### 路由
- 在主应用路由树中新增统一插件入口 `/plugins/:pluginId`。
- 路由只表达“当前进入哪个插件”，不承载具体渲染模式决策。

### 工作区布局
- 将当前“聊天主区 + 可选浏览器面板”抽象为“工作区内容 + 可选 sidecar 容器”。
- 默认聊天工作区和插件工作区复用同一个 `Workspace Host` 外壳。
- 插件若声明 `plugin-workspace`，则其宿主负责整块右侧区域的编排。

### 桌面浏览器容器
- 继续复用现有 `BrowserTabsProvider`、`BrowserTabPage` 与 Electron `BrowserView` 管理器。
- 首期不新增新的桌面容器协议。
- 插件通过 `PluginBrowserSession` 间接使用浏览器容器能力。

## 展示模式

### Route
- 插件以普通 React 页面形式显示。
- 适合内部 UI 完整、无需外部网页承载的插件。

### Embedded Browser
- 插件打开桌面浏览器容器。
- 若 `browserPlacement = main`，浏览器容器直接占据插件工作区主表面。
- 若 `browserPlacement = sidecar`，主区可显示插件控制面板。

### Hybrid
- 插件主区显示 React UI。
- 插件 sidecar 显示浏览器容器。
- 适合“左侧控制台 + 右侧目标页面”一类工作台插件。

## 状态持久化
- 使用独立的插件运行时存储，不混入聊天线程状态。
- 建议持久化：
  - 最近打开的插件
  - 每个插件最后一次展示模式
  - 每个插件关联的 `browserTabId`
  - 插件级轻量界面状态

## 权限与降级
- 首期通过注册表字段控制 `desktopOnly` 与 `featureFlag`。
- 桌面端不可用时，可隐藏插件入口或展示“仅桌面端可用”占位页。
- 插件 guards 在注册表层优先判断，不把错误暴露到各插件组件内部。

## 实施阶段

### 迭代 1：插件框架
- 新增 `PluginRegistry`、`PluginRuntime`、`PluginHostPage`。
- 在 `Sidebar` 中接入插件菜单。
- 新增 `/plugins/:pluginId` 路由。
- 首期仅支持 `route` 插件。

### 迭代 2：浏览器容器接入
- 新增 `PluginBrowserSession`。
- 打通 `embedded-browser` 插件。
- 建立“一插件一浏览器会话”的复用策略。

### 迭代 3：混合工作台
- 抽象 `Workspace Host`，支持插件自定义右侧工作区布局。
- 打通 `hybrid` 插件。
- 支持插件模式切换和会话恢复。

## 风险
- `Sidebar` 已较重，若不抽出插件分组组件，后续维护风险高。
- 如果插件直接操作浏览器 tabs，会导致插件系统与桌面能力强耦合。
- 若不先定义工作区替换边界，后续容易把“插件页面”和“应用壳接管”混为一谈。

## 决策摘要
- 首期目标：桌面端、内建插件、工作区级替换。
- 最大替换边界：`Sidebar` 右侧的 `Workspace Host`。
- 默认策略：插件通过统一路由进入，通过宿主决定展示方式，通过会话层复用浏览器容器。
