# 客户端右侧浏览器容器 Spec

## Why
当前客户端已经支持主体顶部 Tab Bar，但浏览器仍与 `chat/work` 共用同一条业务导航和主内容区域。将浏览器改为从右侧扩展出的独立容器，可以让聊天工作区与网页浏览区并排存在，减少彼此干扰，并让浏览器自身拥有独立的页签管理区。

## What Changes
- 保持主体顶部 Bar 中的 `chat/work` 导航，但不再直接混排浏览器 Tab
- 将主体顶部 Bar 右侧的 `+` 改为“打开右侧浏览器容器”的按钮
- 浏览器界面改为从右侧扩展出的独立容器，与左侧 `chat/work` 主内容并排显示
- 浏览器容器顶部左侧提供 `+` 按钮，用于新增浏览器 Tab
- 浏览器容器内部支持多个浏览器 Tab 的切换、关闭与独立状态保持

## Impact
- Affected specs: 客户端主体导航条、右侧浏览器容器、浏览器页签管理
- Affected code: `src/apps/web/src/components/DesktopTabBar.tsx`、`src/apps/web/src/components/BrowserTabPage.tsx`、`src/apps/web/src/layouts/AppLayout.tsx`、客户端状态上下文、桌面端网页承载能力

## ADDED Requirements
### Requirement: 主体顶部导航与浏览器入口
系统 SHALL 在客户端主体内容区域顶部提供一条独立导航 Bar，用于展示 `chat/work` 模式入口，并提供打开右侧浏览器容器的入口。

#### Scenario: 展示默认主体导航
- **WHEN** 用户打开客户端
- **THEN** 主体内容区域顶部显示现有的 `chat` 与 `work` 入口
- **AND** 最顶部原生标题栏不再承载这些业务 Tab
- **AND** 主体顶部 Bar 右侧提供浏览器容器展开入口

#### Scenario: 打开右侧浏览器容器
- **WHEN** 用户点击主体顶部 Bar 右侧的浏览器展开按钮
- **THEN** 系统从右侧展开独立的浏览器容器
- **AND** 左侧 `chat/work` 主内容区域继续保持可见

### Requirement: 右侧浏览器容器
系统 SHALL 将网页浏览功能承载在右侧扩展容器中，而不是替换左侧 `chat/work` 主内容区。

#### Scenario: 在浏览器容器中新增 Tab
- **WHEN** 用户点击右侧浏览器容器顶部左侧的 `+` 按钮
- **THEN** 系统创建一个新的浏览器 Tab
- **AND** 新 Tab 在创建后立即成为当前激活页签

#### Scenario: 切换浏览器 Tab
- **WHEN** 用户点击右侧浏览器容器中的任一浏览器 Tab
- **THEN** 右侧容器切换到对应网页内容
- **AND** 被切走的浏览器 Tab 状态在本次会话内保持不变

#### Scenario: 关闭网页浏览 Tab
- **WHEN** 用户关闭某个网页浏览 Tab
- **THEN** 系统仅移除该网页 Tab
- **AND** 其他 Tab 保持可用
- **AND** 若关闭的是当前激活 Tab，系统切换到最近的仍可用 Tab
- **AND** 若无剩余浏览器 Tab，右侧浏览器容器可保持空态或允许用户继续新建

### Requirement: 网页浏览 Tab 内容承载
系统 SHALL 为网页浏览 Tab 提供独立的网页内容承载区域，并允许加载用户指定的网址。

#### Scenario: 打开网页
- **WHEN** 用户在网页浏览 Tab 中输入并确认网址
- **THEN** 系统在该 Tab 的内容区域加载对应网页
- **AND** 加载过程对其他 Tab 不产生影响

#### Scenario: 网页加载失败
- **WHEN** 目标网页无法加载
- **THEN** 系统在当前网页浏览 Tab 内展示可恢复的错误提示
- **AND** 用户可以重新输入网址或重试加载

## MODIFIED Requirements
### Requirement: 客户端标题栏与导航分层
系统 SHALL 将窗口标题栏、主体导航 Bar 与右侧浏览器容器分层处理，使最顶部原生标题栏只负责桌面窗口操作，主体顶部 Bar 只负责 `chat/work` 与浏览器入口，而浏览器 Tab 管理由右侧独立容器负责。

#### Scenario: 兼容现有 chat/work
- **WHEN** 用户未打开右侧浏览器容器
- **THEN** 主体顶部 Bar 的默认行为与现有 `chat/work` 使用方式保持一致
- **AND** 不要求用户迁移已有对话或工作流

#### Scenario: 标题栏职责收敛
- **WHEN** 用户处于任意页面
- **THEN** 最顶部标题栏仍保留窗口控制、拖拽区和全局按钮
- **AND** 业务 Tab 不再出现在最顶部标题栏中
