# Frontend Design System

## 共享组件库

所有组件来自 `@arkloop/shared`，路径 `src/apps/shared/src/components/`。

**做任何 UI 任务前，必须先查组件库，找到可复用的组件再动手。**

### 可用组件清单

| 组件 | 用途 |
|------|------|
| `Button` | 所有按钮，含 variant 和 size |
| `PillToggle` | 开关切换，带动画 |
| `Modal` | 弹窗容器 |
| `ModalFooter` | 弹窗底部操作区 |
| `ConfirmDialog` | 二次确认弹窗 |
| `FormField` | 表单字段包装 |
| `Badge` | 状态标签 |
| `EmptyState` | 空状态占位 |
| `ErrorCallout` | 错误提示块 |
| `DataTable` | 数据表格 |
| `PageHeader` | 页面标题栏 |
| `PageLoading` | 页面加载态 |
| `SidebarNav` | 侧边导航 |
| `NavButton` | 导航按钮 |
| `Toast` / `useToast` | 消息提示 |
| `SettingsModal` | 设置弹窗框架 |
| `TurnView` | 对话轮次视图 |

### Button variants

```typescript
variant: 'primary' | 'ghost' | 'danger' | 'outline'
size: 'sm' | 'md'
loading: boolean
```

### Console 页面按钮

Console 页面使用 `consoleCls`（`src/apps/console/src/styles.ts`）：

```typescript
consoleCls.btnPrimary      // 主操作
consoleCls.btnSecondary    // 取消/次要操作
consoleCls.btnDestructive  // 删除/危险操作
consoleCls.input           // 文本输入框
consoleCls.textarea        // 多行输入框
```

## CSS 变量规范

**禁止硬编码颜色值**，必须使用 CSS 变量。

### 常用变量

```
背景: --c-bg-page, --c-bg-sidebar, --c-bg-deep, --c-bg-sub, --c-bg-card, --c-bg-input
文字: --c-text-primary, --c-text-secondary, --c-text-tertiary, --c-text-muted
边框: --c-border, --c-border-subtle, --c-border-mid
按钮: --c-btn-bg, --c-btn-text
强调: --c-accent, --c-accent-fg
状态: --c-status-success-text, --c-status-error-text, --c-status-warning-text
错误: --c-error-bg, --c-error-text, --c-error-border
```

## 双语规则

UI 文案变更必须同时更新中文（zh）和英文（en）两个 locale 文件，不允许只改其中一个。

## 主题模式

三种模式：`system` / `light` / `dark`，通过 `data-theme` HTML 属性控制。

使用 `useTheme()` 获取当前主题，不得直接读 localStorage。
