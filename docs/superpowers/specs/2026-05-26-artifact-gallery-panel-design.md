# Artifact Gallery Panel

在右侧面板中新增 "图库" 标签页，以缩略图网格方式浏览和管理当前 thread 中生成的所有 artifact，支持按文件类型筛选和下载。

## 需求

- 聚合当前 thread 中所有消息的 artifact，以缩略图网格展示
- 图片 artifact 显示实际缩略图，其他文件类型显示对应图标
- 按文件类型筛选：全部 / 媒体文件 / 文本文件
- 支持下载 artifact
- 入口：右侧面板 "+" 菜单，可打开/关闭
- 不做删除功能

## 架构

### 改动范围

| 文件 | 改动 |
|------|------|
| `RightPanel.tsx` | `RightPanelTab['kind']` 联合类型新增 `'artifacts'`；图标映射加 Grid3x3 图标 |
| `rightPanelControls.ts` | 新增 `ARTIFACTS_TAB_KIND` 常量 |
| `ChatView.tsx` | `addOptions` 加入口；`buildStoredPanelTab` 加 `'artifacts'` 分支 |
| `ArtifactGalleryPanel.tsx` (新) | 主组件 ~150 行 |
| `ArtifactGalleryPanel.css` (新) | 样式 ~40 行 |

### 组件树

```
ChatView
  RightPanel
    tabs: [
      ...永久标签页 (conversation-graph, web, files),
      ...动态标签页 (source, code, agent, resource),
      ArtifactGalleryPanel (新增, kind: 'artifacts')
    ]
```

### ArtifactGalleryPanel 内部结构

```
ArtifactGalleryPanel
  FilterBar        — 分段按钮 (全部 | 媒体 | 文本) + 计数
  ThumbnailGrid    — 3 列网格，可滚动
    ArtifactCard[] — 每张卡片: 缩略图/图标 + 文件名 + 下载按钮
  EmptyState       — 无 artifact 时显示占位
```

## 数据流

### 数据来源

从 `MessageMetaContext` 获取当前 thread 所有消息，通过 `MessageMeta.artifacts?: ArtifactRef[]` 聚合。

```ts
const artifacts = useMemo(() => {
  const all = messages.flatMap(m => m.artifacts ?? []);
  // 按 key 去重，保留第一次出现 (消息时间顺序)
  const unique = dedupBy(all, a => a.key);
  if (filter === 'all') return unique;
  if (filter === 'media') return unique.filter(a => isMediaType(a.mime_type));
  return unique.filter(a => isTextType(a.mime_type));
}, [messages, filter]);
```

### 图片缩略图 URL

复用现有 `useArtifactUrl(key)` hook 获取签名 S3 URL。仅对 `image/*` MIME 类型请求 URL。

### 下载

1. `useArtifactUrl(key)` → 签名 URL
2. `fetch(url)` → blob
3. 创建临时 `<a>` 标签触发下载
4. `URL.revokeObjectURL()` 清理

## MIME 分类

### 媒体文件 (filter === 'media')

- `image/*`
- `video/*`
- `audio/*`

### 文本文件 (filter === 'text')

- `text/*`
- `application/json`, `application/xml`, `application/javascript`
- `application/pdf`
- 未知 MIME 类型默认归为文本

## 文件类型图标

| MIME 类型 | 图标 |
|-----------|------|
| `image/*` | 实际缩略图 (签名 URL) |
| `video/*` | FileVideo (lucide) |
| `audio/*` | FileAudio (lucide) |
| `text/plain` | FileText (lucide) |
| `text/x-*`, `application/json`, `application/javascript` | FileCode (lucide) |
| `text/html` | FileCode (lucide) |
| 其他 (默认) | File (lucide) |

## 交互

| 操作 | 行为 |
|------|------|
| 点击缩略图 | 在右侧面板打开该 artifact 的预览标签页 (复用现有 resource 标签页) |
| 点击下载按钮 | 获取签名 URL → 触发浏览器下载 |
| 切换筛选 | 即时过滤网格 |
| 空状态 | 显示 "当前线程还没有生成文件" 占位提示 |
| 筛选后为空 | 显示 "没有匹配的[媒体/文本]文件" 提示 |

## 边界情况

- **空状态** (线程无 artifact): 居中占位提示
- **筛选后为空**: 对应分类的空结果提示
- **重复 artifact**: 按 `key` 去重（同一 artifact 可能出现在多条消息中）
- **图片加载失败**: fallback 到文件图标
- **URL 获取失败**: 显示文件图标，下载按钮可重试
- **大量 artifact**: `useMemo` 缓存 + `img loading="lazy"` + IntersectionObserver 懒加载
- **未知 MIME 类型**: 归于文本文件，使用默认 File 图标

## 入口实现

在 `ChatView.tsx` 的 `addOptions` 数组中新增：

```ts
{ label: '图库', kind: 'artifacts' }
```

用户点击后调用 `upsertRightPanelTab` 创建 artifacts 标签页。标签页可正常关闭。
