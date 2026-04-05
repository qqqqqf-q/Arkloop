---
title: "Web App Mobile Adaptation"
---
Arkloop Web App 的移动端适配方案。覆盖响应式布局重构、触控交互优化、PWA 支持与原生壳技术选型。

适配目标为 Web 端聊天界面（`src/apps/web/`），Android/iOS 通过 WebView 壳复用同一套 Web 代码。

---

## 1. 现状分析

### 1.1 已有基础

| 基础设施 | 状态 | 说明 |
|----------|------|------|
| Tailwind CSS v4 | 已接入 | 通过 `@tailwindcss/vite`，支持 `sm:/md:/lg:` 断点 |
| Viewport Meta | 已配置 | `width=device-width, initial-scale=1.0` |
| Sidebar 折叠 | 已实现 | `sidebarCollapsed` 状态控制 `w-0` / `w-[304px]` |
| 主题系统 | 已实现 | CSS 变量 + `prefers-color-scheme`，60+ 自定义属性 |
| 动画框架 | 已接入 | Framer Motion，支持手势 API |

### 1.2 缺失部分

| 项目 | 现状 | 影响 |
|------|------|------|
| 响应式断点 | 仅 DashboardPage 有 `lg:grid-cols-4`，其余组件无断点类 | 所有布局在窄屏下溢出 |
| 移动导航模式 | 无 hamburger menu / drawer | Sidebar 占 40-50% 移动视口 |
| 触控适配 | 图标按钮 8-16px，线程项 9px padding | 低于 WCAG 44x44px 最低触控目标 |
| 右侧面板 | 固定 420-560px | 超出移动屏幕宽度 |
| 设置弹窗 | 固定 832x624px | 任何移动设备都放不下 |
| 虚拟键盘 | `body { overflow: hidden }` + `h-screen` | iOS 键盘弹出时内容被遮挡 |
| PWA | 无 Service Worker、无 manifest | 不支持离线或安装 |

### 1.3 移动端关键尺寸约束

```
iPhone SE:      375 x 667
iPhone 15:      393 x 852
iPhone 15 Pro Max: 430 x 932
Pixel 8:        412 x 915
iPad Mini:      768 x 1024
```

核心约束：最小适配宽度 **375px**，安全内容区约 **343px**（375 - 16*2 padding）。

---

## 2. 适配策略

### 2.1 断点体系

沿用 Tailwind CSS 默认断点，以移动优先（mobile-first）方式编写：

| 断点 | 宽度 | 设备类型 | 布局策略 |
|------|------|----------|----------|
| 默认 | 0-639px | 手机 | 单列，Sidebar 为 Drawer，面板为 Bottom Sheet |
| `sm` | 640-767px | 大屏手机 / 小平板 | 同上，间距稍宽 |
| `md` | 768-1023px | 平板 | 可选常驻 Sidebar（窄版），面板仍为 Bottom Sheet |
| `lg` | 1024px+ | 桌面 | 当前布局（Sidebar + Chat + Panel 三栏） |

关键分界线：**`lg:1024px`** -- 低于此值切换为移动布局模式。

### 2.2 布局模式切换

**桌面（lg+）-- 维持现有布局：**

```
┌──────────┬──────────────────────┬──────────┐
│ Sidebar  │     Chat Messages    │  Panel   │
│ 240-304  │   max-w-[756px]      │ 420-560  │
│          │   + ChatInput        │          │
└──────────┴──────────────────────┴──────────┘
```

**移动（< lg）-- 全屏单列：**

```
┌──────────────────────┐
│ Header (compact)     │  48px，含 hamburger + 标题 + 操作按钮
├──────────────────────┤
│                      │
│   Chat Messages      │  flex-1, 全宽, scroll
│                      │
├──────────────────────┤
│   ChatInput          │  sticky bottom, 全宽
└──────────────────────┘

Sidebar    → 左侧 Drawer (overlay, 85vw max-w-[320px])
右侧面板   → Bottom Sheet (snap points: 40% / 85% / 100%)
设置弹窗   → 全屏 Modal
```

### 2.3 触控规范

| 规范项 | 要求 |
|--------|------|
| 最小触控目标 | 44x44px（WCAG 2.1 AA） |
| 按钮间距 | >= 8px |
| 文字最小字号 | 16px（防止 iOS 自动缩放） |
| 可点击行项 | 最小高度 48px |
| 滑动手势 | 左滑关闭 Drawer、下拉关闭 Bottom Sheet |

---

## 3. 组件适配方案

### 3.1 AppLayout

**现状：** `flex h-screen overflow-hidden`，Sidebar 和 Main 并排。

**适配：**

- 引入 `useMediaQuery` Hook（或 Tailwind 断点检测）区分移动/桌面模式
- 移动模式下 Sidebar 不参与 flex 布局，改为 fixed overlay Drawer
- Main 区域 `w-full`，不再被 Sidebar 挤压
- 移动 Header 条：左侧 hamburger 图标触发 Drawer，居中显示当前上下文，右侧放新建对话 + 无痕模式开关

```
// 移动
<div className="flex h-[100dvh] flex-col">
  <MobileHeader
    onMenuClick={toggleDrawer}
    onNewChat={handleNewChat}
    incognito={incognito}
    onIncognitoToggle={toggleIncognito}
  />
  <main className="flex-1 overflow-hidden">
    <Outlet />
  </main>
  <SidebarDrawer open={drawerOpen} onClose={closeDrawer} />
</div>

// 桌面 (lg+)
<div className="flex h-screen overflow-hidden">
  <Sidebar />
  <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
    <Outlet />
  </main>
</div>
```

关键点：移动端使用 `100dvh`（dynamic viewport height）替代 `h-screen`，解决 iOS Safari 地址栏收缩问题。

### 3.2 Sidebar → Drawer

**现状：** 固定宽度 240-304px，内含线程列表、导航按钮、用户信息。

**适配：**

- 移动端渲染为 Drawer 组件（`position: fixed; inset: 0`）
- Drawer 宽度：`w-[85vw] max-w-[320px]`
- 进入动画：`translateX(-100%) → translateX(0)`，280ms ease-out
- 背景遮罩：`bg-black/40`，点击关闭
- 左滑手势关闭（Framer Motion `useDragControls`）
- 线程列表项高度调整为 48px，padding 增加到 `px-3 py-3`
- 导航按钮区域改为一行四个图标，44x44 触控区

### 3.3 ChatPage

**现状：** flex row 布局，左侧消息流（max-w-[756px]）+ 右侧面板（420-560px）。

**适配：**

- 移动端去掉 `max-w-[756px]`，消息流全宽（带 `px-4` padding）
- 右侧面板采用混合模式：
  - Sources Panel / Code Execution Panel → Bottom Sheet（从底部滑出）
    - Snap points：40%（预览）、85%（展开）
    - 下拉手势关闭
  - Document Panel → 全屏 Modal（文档需要完整阅读空间，返回按钮关闭）
- Header 简化：移动端只保留核心操作（模型选择、附件），次要操作收入 `...` 菜单
- 消息滚动区域保持 `overflow-y: auto`，添加 `-webkit-overflow-scrolling: touch`

### 3.4 ChatInput

**现状：** `max-w-[840px]`，含附件网格、textarea、操作按钮行。

**适配：**

- 移除 `max-w-[840px]` 限制，改为 `w-full px-3`
- Textarea 固定在底部，使用 `position: sticky; bottom: 0`
- 附件卡片从 120x120 调整为 `w-20 h-20`（80px），移动端一行最多 4 个
- 操作按钮行：保留核心按钮（附件、发送），Persona 选择器移入长按/菜单
- 发送按钮尺寸 44x44px
- 语音录制：移动端使用全宽波形条，录制按钮居中放大

虚拟键盘处理：
- 监听 `visualViewport.resize` 事件
- 键盘弹出时动态调整 input 容器位置
- 使用 `env(safe-area-inset-bottom)` 适配 iPhone 底部安全区

### 3.5 WelcomePage

**现状：** 居中布局，40px 标题，675px 宽度限制，`pt-[16vh]` 顶部留白。

**适配：**

- 标题字号：`text-[40px] lg:text-[40px]`，移动端降为 `text-2xl`（24px）
- 顶部留白：`pt-[8vh] lg:pt-[16vh]`
- ChatInput 宽度跟随容器，移除固定 max-width
- FreePlan Badge 下拉框宽度改为 `w-[min(300px,calc(100vw-32px))]`

### 3.6 SettingsModal

**现状：** 固定 832x624px，左侧导航 + 右侧内容双栏。

**适配：**

- 移动端改为全屏 Modal（`inset: 0`）
- 取消双栏布局，改为单栏 + 顶部 Tab Bar
- Tab Bar：Account / Settings / Credits 三个标签，横向排列
- 内容区全宽滚动
- 返回按钮放在左上角，关闭按钮右上角

### 3.7 AuthPage

**现状：** 居中卡片，36px 输入框高度，13px 标签字号。

**适配：**

- 输入框高度调整为 48px（移动端），font-size 16px（防止 iOS 缩放）
- 标签字号调整为 14px
- 按钮高度 48px
- 表单卡片 `w-full max-w-[400px] px-5`
- OTP 输入支持 `autocomplete="one-time-code"` 属性

### 3.8 MessageBubble

**现状：** 消息内容含 Markdown 渲染、代码块、LaTeX 公式。

**适配：**

- 代码块：`overflow-x: auto` 保持不变，增加 `font-size: 13px` 移动端
- 表格：外层 `overflow-x: auto` 容器，表格最小宽度不设限
- 图片：`max-width: 100%` + `height: auto`
- 长公式：允许水平滚动，容器加 `scroll-snap-type: x mandatory`
- 操作按钮（复制/重新生成等）：移动端尺寸调整为 36x36px

### 3.9 SharePage

**现状：** 公开分享页面 `/s/:token`。

**适配：**

- 作为独立访问入口，移动端优先设计
- 全宽单列布局，无 Sidebar
- 消息气泡与 ChatPage 复用同一套响应式样式

---

## 4. CSS 架构调整

### 4.1 index.css 改动

新增移动端基础样式：

```css
/* 安全区适配 */
:root {
  --safe-area-bottom: env(safe-area-inset-bottom, 0px);
  --safe-area-top: env(safe-area-inset-top, 0px);
}

/* 移动端 viewport 修正 */
html {
  height: 100%;
}

body {
  min-height: 100dvh;
}

/* 移动端滚动优化 */
@supports (-webkit-touch-callout: none) {
  .scroll-container {
    -webkit-overflow-scrolling: touch;
  }
}
```

### 4.2 共享工具类

在 Tailwind 层面增加项目级工具类（通过 `@layer utilities`）：

```css
@layer utilities {
  .touch-target {
    min-width: 44px;
    min-height: 44px;
  }
  .safe-bottom {
    padding-bottom: env(safe-area-inset-bottom, 0px);
  }
}
```

### 4.3 动画适配

移动端降低动画复杂度以保证流畅性：

```css
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    animation-duration: 0.01ms !important;
    transition-duration: 0.01ms !important;
  }
}
```

对于不支持 `prefers-reduced-motion` 的场景，在移动断点内简化：
- Modal 进入动画：从 `scale(0.97) → scale(1)` 简化为 `translateY → 0`
- Panel 切换：从宽度渐变改为 opacity 切换
- Drawer：保留 `translateX` 动画（符合移动端心智模型）

---

## 5. 基础设施组件

适配过程需要新增以下基础组件（放入 `src/apps/web/src/components/mobile/`）：

| 组件 | 用途 | 依赖 |
|------|------|------|
| `Drawer` | 侧边栏容器，支持手势关闭 | Framer Motion |
| `BottomSheet` | 底部弹出面板，支持 snap points + 手势 | Framer Motion |
| `MobileHeader` | 移动端顶部导航条（hamburger + 标题 + 新建对话 + 无痕开关） | - |
| `useViewportSize` | 动态追踪视口尺寸（含虚拟键盘） | `visualViewport` API |
| `useBreakpoint` | 返回当前断点标识，用于条件渲染 | `matchMedia` |

### 5.1 useBreakpoint Hook

```typescript
type Breakpoint = 'mobile' | 'tablet' | 'desktop'

function useBreakpoint(): Breakpoint {
  // matchMedia('(min-width: 1024px)') → desktop
  // matchMedia('(min-width: 768px)')  → tablet
  // else                              → mobile
}
```

组件内通过 `useBreakpoint()` 决定渲染路径，而非在 JSX 里堆叠 Tailwind 断点类（避免桌面端加载移动组件的 DOM）。

---

## 6. PWA 支持

### 6.1 Web App Manifest

在 `public/manifest.json` 配置：

```json
{
  "name": "Arkloop",
  "short_name": "Arkloop",
  "start_url": "/",
  "display": "standalone",
  "background_color": "#0a0a0a",
  "theme_color": "#0a0a0a",
  "icons": [
    { "src": "/icon-192.png", "sizes": "192x192", "type": "image/png" },
    { "src": "/icon-512.png", "sizes": "512x512", "type": "image/png" },
    { "src": "/icon-maskable-512.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable" }
  ]
}
```

### 6.2 Service Worker 策略

使用 `vite-plugin-pwa`（Workbox 封装）实现：

| 资源类型 | 缓存策略 | 说明 |
|----------|----------|------|
| App Shell（HTML/JS/CSS） | Precache | 构建时生成缓存清单，版本更新时替换 |
| 静态资源（图片/字体） | Cache First | 首次加载后本地缓存 |
| API 请求（`/v1/*`） | Network Only | 实时数据不缓存 |
| SSE 连接 | 不缓存 | 流式数据无法缓存 |

### 6.3 离线消息持久化

使用 IndexedDB 存储历史对话数据，实现离线浏览能力：

| 存储项 | IndexedDB Store | 说明 |
|--------|----------------|------|
| 线程列表 | `threads` | id、title、updatedAt、persona |
| 消息列表 | `messages` | threadId 索引，完整消息内容 |
| 附件元数据 | `attachments` | 文件名、类型、大小，不缓存文件本体 |

同步策略：
- 在线时，API 响应写入 IndexedDB（write-through）
- 离线时，从 IndexedDB 读取并显示，顶部提示 "离线模式，显示缓存数据"
- 恢复在线后，与服务端增量同步（基于 `updatedAt` 时间戳）
- 存储上限：保留最近 200 个线程的消息，超出自动清理最旧数据

不缓存：SSE 流事件、运行中的 Tool Call 状态、附件文件本体。

### 6.3 安装提示

监听 `beforeinstallprompt` 事件，在设置页面提供 "安装到主屏幕" 按钮。不使用弹窗式安装提示（避免干扰用户）。

---

## 7. 原生壳方案

### 7.1 技术选型

| 方案 | 平台 | 优劣 |
|------|------|------|
| **Capacitor** | iOS + Android | Cordova 继任者，WebView 壳 + 原生桥，社区活跃，插件生态完善 |
| TWA (Trusted Web Activity) | Android | Chrome Custom Tab 封装 PWA，零原生代码，但仅限 Android |
| WKWebView 手动封装 | iOS | 完全自控，但需自行处理推送、Cookie、Deep Link |

**推荐：Capacitor**

理由：
- 一套代码同时产出 iOS/Android 壳
- 与现有 Vite 构建流程直接集成（`@capacitor/cli` + `npx cap sync`）
- 需要原生能力时可通过插件桥接（推送通知、生物认证、文件系统）
- 不侵入现有 Web 代码，壳工程独立维护

### 7.2 Capacitor 集成架构

壳工程独立为 `src/apps/mobile/`，引用 Web 的构建产物：

```
src/apps/mobile/
  ├── capacitor.config.ts       # webDir 指向 ../web/dist
  ├── package.json
  ├── android/                  # Android 壳工程（Capacitor 生成）
  └── ios/                      # iOS 壳工程（Capacitor 生成）
```

构建流程：

```
cd src/apps/web && pnpm build → cd src/apps/mobile && npx cap sync → Android Studio / Xcode 构建
```

### 7.3 原生桥接需求

| 能力 | 插件 | 优先级 |
|------|------|--------|
| 推送通知 | `@capacitor/push-notifications` | P1 |
| 状态栏控制 | `@capacitor/status-bar` | P1 |
| 安全区信息 | `@capacitor/safe-area` | P1 |
| 生物认证 | `@capacitor-community/biometric-auth` | P2 |
| 文件选择 | `@capacitor/filesystem` | P2 |
| 分享 | `@capacitor/share` | P3 |
| 触觉反馈 | `@capacitor/haptics` | P3 |

### 7.4 平台检测

```typescript
import { Capacitor } from '@capacitor/core'

const isNative = Capacitor.isNativePlatform()
const platform = Capacitor.getPlatform() // 'web' | 'ios' | 'android'
```

Web 代码中通过平台检测按需启用原生功能（如推送注册、生物认证入口），非原生环境下回退到 Web API。

---

## 8. 测试策略

### 8.1 设备覆盖

| 设备 | 分辨率 | 覆盖场景 |
|------|--------|----------|
| iPhone SE (3rd) | 375x667 | 最小宽度基准 |
| iPhone 15 | 393x852 | 主流 iOS |
| Pixel 8 | 412x915 | 主流 Android |
| iPad Mini | 768x1024 | 平板断点 |
| Galaxy Fold (展开) | 717x952 | 折叠屏 |

### 8.2 测试维度

- **布局**：所有页面在 375px 宽度下无水平溢出
- **触控**：所有可交互元素 >= 44x44px
- **键盘**：虚拟键盘弹出时 ChatInput 可见且可用
- **手势**：Drawer 左滑关闭、Bottom Sheet 下拉关闭
- **离线**：断网后 App Shell 可加载，显示离线提示
- **安全区**：iPhone 底部 Home Indicator 不遮挡内容
- **横屏**：横屏模式下布局不崩溃（不要求最优体验）

---

## 9. 约束与决策记录

| 决策 | 选择 | 理由 |
|------|------|------|
| 最小适配宽度 | 375px | iPhone SE 市场份额仍然可观 |
| 布局切换断点 | 1024px (lg) | 低于此值桌面三栏布局不可用 |
| CSS 方案 | Tailwind 响应式类 + useBreakpoint 条件渲染 | 简单布局用 Tailwind 类，复杂结构差异用条件渲染 |
| 面板移动方案 | 混合：Sources/Code 用 Bottom Sheet，Document 用全屏 Modal | 短内容预览适合 Sheet，长文档需要全屏阅读空间 |
| 原生壳 | Capacitor | 唯一同时覆盖 iOS/Android 且与 Vite 集成良好的方案 |
| PWA 缓存 | App Shell 预缓存 + IndexedDB 离线消息 + API Network First | 保证离线可用，在线时实时数据优先 |
| 动画降级 | `prefers-reduced-motion` 优先 | 尊重系统无障碍设置 |

---

## 10. 执行路线图

路线图按依赖关系分阶段，每个阶段的产出可独立验证。

### Phase 0 -- 基础设施

搭建移动端适配所需的底层工具和组件，不改变现有功能行为。

| 任务 | 产出 | 验证标准 |
|------|------|----------|
| 实现 `useBreakpoint` Hook | `hooks/useBreakpoint.ts` | 在 375px / 768px / 1024px 视口下返回正确断点标识 |
| 实现 `useViewportSize` Hook | `hooks/useViewportSize.ts` | 虚拟键盘弹出时正确反映 `visualViewport` 尺寸 |
| 实现 `Drawer` 组件 | `components/mobile/Drawer.tsx` | 支持 `translateX` 进出动画 + 遮罩 + 左滑手势关闭 |
| 实现 `BottomSheet` 组件 | `components/mobile/BottomSheet.tsx` | 支持 40%/85% snap points + 下拉手势关闭 |
| CSS 基础：安全区变量 + `100dvh` | `index.css` 更新 | iPhone Safari 地址栏收缩时布局不跳动 |
| 新增 `.touch-target` / `.safe-bottom` 工具类 | `index.css` 更新 | 工具类可用 |

### Phase 1 -- 核心布局适配

改造主布局，使 Web App 在移动设备上可基本使用。

| 任务 | 产出 | 验证标准 |
|------|------|----------|
| AppLayout 移动/桌面双模式 | `layouts/AppLayout.tsx` 重构 | < 1024px 时渲染 MobileHeader + 全宽 Main；>= 1024px 维持现有布局 |
| MobileHeader 组件 | `components/mobile/MobileHeader.tsx` | 48px 高度，hamburger / 标题 / 新建对话 / 无痕开关 |
| Sidebar → Drawer 适配 | `components/Sidebar.tsx` 改造 | 移动端以 Drawer 形式打开，85vw / max-w-[320px]，线程项 48px 高度 |
| ChatPage 移除固定宽度 | `components/ChatPage.tsx` 改造 | 移动端消息流全宽（px-4 padding），无水平溢出 |
| ChatInput 底部 sticky | `components/ChatInput.tsx` 改造 | 移动端全宽，键盘弹出时输入框可见 |

**验证里程碑：** 在 iPhone SE (375px) 上完成一次完整对话（打开 App → 新建对话 → 发消息 → 收到回复 → 切换对话）。

### Phase 2 -- 面板与弹窗适配

右侧面板和 Modal 类组件的移动端改造。

| 任务 | 产出 | 验证标准 |
|------|------|----------|
| Sources Panel → Bottom Sheet | `ChatPage.tsx` 面板逻辑 | 从底部滑出，snap 40%/85%，下拉关闭 |
| Code Execution Panel → Bottom Sheet | 同上 | 同上 |
| Document Panel → 全屏 Modal | `ChatPage.tsx` + `DocumentPanel.tsx` | 全屏覆盖，左上角返回按钮关闭 |
| SettingsModal 全屏化 | `components/SettingsModal.tsx` | 移动端 `inset: 0`，单栏 + 顶部 Tab Bar |
| ShareModal 响应式 | `components/ShareModal.tsx` | 移动端宽度自适应 |

**验证里程碑：** 在移动设备上触发 Sources/Code/Document 面板，交互流畅，手势关闭正常。

### Phase 3 -- 页面细节打磨

各页面的移动端视觉和交互优化。

| 任务 | 产出 | 验证标准 |
|------|------|----------|
| WelcomePage 响应式 | 标题 / 间距 / Badge 下拉自适应 | 375px 下无溢出，标题 24px |
| AuthPage 触控优化 | 输入框 48px，标签 14px，OTP autocomplete | 表单在移动端可顺畅填写 |
| MessageBubble 适配 | 代码块 / 表格 / 图片 / 公式响应式 | 长代码块水平滚动，图片不超出 |
| 触控目标全局扫描 | 所有按钮 >= 44x44px | 自动化检测脚本通过 |
| ChatInput 附件卡片 | 80x80px，一行 4 个 | 4 张附件不换行 |
| 语音录制移动优化 | 全宽波形条 + 居中录制按钮 | 录制体验流畅 |

**验证里程碑：** 在 5 款目标设备上完整走通所有页面，无布局溢出、无触控死区。

### Phase 4 -- PWA + 离线

| 任务 | 产出 | 验证标准 |
|------|------|----------|
| Web App Manifest | `public/manifest.json` + 图标 | Chrome DevTools Application 面板无错误 |
| Service Worker（vite-plugin-pwa） | SW 配置 + 缓存策略 | 断网后 App Shell 可加载 |
| IndexedDB 离线存储 | `lib/offlineStore.ts` | 写入 200 线程 + 消息，离线时可浏览 |
| 离线/在线状态提示 | UI 顶部提示条 | 断网显示提示，恢复后自动消失 |
| 安装到主屏幕入口 | 设置页 "安装" 按钮 | `beforeinstallprompt` 触发后按钮可用 |

**验证里程碑：** 安装 PWA 到主屏幕 → 打开 → 断网 → 浏览历史对话 → 恢复网络 → 增量同步。

### Phase 5 -- 原生壳

| 任务 | 产出 | 验证标准 |
|------|------|----------|
| 初始化 `src/apps/mobile/` | Capacitor 项目 + config | `npx cap sync` 成功 |
| Android 壳构建 | `android/` 工程 | APK 安装后可正常使用 Web App |
| iOS 壳构建 | `ios/` 工程 | Xcode 构建后可在模拟器运行 |
| 推送通知集成 | `@capacitor/push-notifications` | FCM (Android) + APNs (iOS) 推送到达 |
| 状态栏 + 安全区适配 | `@capacitor/status-bar` + safe-area | 状态栏颜色跟随主题，内容不被刘海遮挡 |
| 平台检测 + 条件功能 | `lib/platform.ts` | `isNative` 正确返回，原生功能仅在壳内可用 |

**验证里程碑：** Android APK + iOS IPA 可安装并完成完整聊天流程，推送通知正常到达。
