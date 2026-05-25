# Artifact Gallery Panel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an "Artifacts" gallery tab to the right panel that aggregates all artifacts from the current thread, displays them as a thumbnail grid with file-type filtering, and supports download.

**Architecture:** New `ArtifactGalleryPanel` component renders inside RightPanel via a new `'artifacts'` tab kind. It reads all artifacts from `useMessageMeta().metaMap`, deduplicates by key, filters by MIME category, and renders a 3-column thumbnail grid. Images fetch blob URLs via authenticated API calls; non-image files show lucide icons.

**Tech Stack:** React 19, TypeScript, lucide-react icons, CSS (no extra dependencies)

---

### Task 1: Add i18n strings

**Files:**
- Modify: `src/apps/web/src/locales/en.ts:40-50`
- Modify: `src/apps/web/src/locales/zh.ts:40-50`

- [ ] **Step 1: Add English locale strings**

```ts
// In en.ts, inside rightPanel object, add after conversationGraphSystem:
gallery: 'Gallery',
allFiles: 'All',
mediaFiles: 'Media',
textFiles: 'Text',
noArtifacts: 'No files generated yet in this thread',
noMatchingFiles: 'No matching files',
```

- [ ] **Step 2: Add Chinese locale strings**

```ts
// In zh.ts, inside rightPanel object, add after conversationGraphSystem:
gallery: '图库',
allFiles: '全部',
mediaFiles: '媒体',
textFiles: '文本',
noArtifacts: '当前线程还没有生成文件',
noMatchingFiles: '没有匹配的文件',
```

- [ ] **Step 3: Verify types still compile**

Run: `cd src/apps/web && pnpm type-check`
Expected: PASS (no new errors)

- [ ] **Step 4: Commit**

```bash
git add src/apps/web/src/locales/en.ts src/apps/web/src/locales/zh.ts
git commit -m "feat: add gallery i18n strings for artifact panel

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 2: Extend RightPanelTab kind to support 'artifacts'

**Files:**
- Modify: `src/apps/web/src/components/RightPanel.tsx:9-17` (kind union), `:39-43` (TabIcon)

- [ ] **Step 1: Add `'artifacts'` to the kind union**

```tsx
// RightPanel.tsx line 12, change:
kind: 'web' | 'files' | 'source' | 'code' | 'agent' | 'resource' | 'conversation-graph'
// to:
kind: 'web' | 'files' | 'source' | 'code' | 'agent' | 'resource' | 'conversation-graph' | 'artifacts'
```

- [ ] **Step 2: Add Grid3x3 icon import**

```tsx
// RightPanel.tsx line 2, change:
import { FileText, FolderOpen, GitBranch, Globe2, Plus, X } from 'lucide-react'
// to:
import { FileText, FolderOpen, GitBranch, Globe2, Grid3x3, Plus, X } from 'lucide-react'
```

- [ ] **Step 3: Add icon case in TabIcon**

```tsx
// RightPanel.tsx, inside TabIcon function, add before return:
if (kind === 'artifacts') return <Grid3x3 size={rightPanelIconSize} />
```

- [ ] **Step 4: Verify types**

Run: `cd src/apps/web && pnpm type-check`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add src/apps/web/src/components/RightPanel.tsx
git commit -m "feat: add 'artifacts' kind to RightPanelTab

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 3: Create ArtifactGalleryPanel component

**Files:**
- Create: `src/apps/web/src/components/ArtifactGalleryPanel.tsx`

- [ ] **Step 1: Write the component**

```tsx
import { useState, useMemo, useCallback, useEffect, useRef } from 'react'
import { File, FileCode, FileText, FileVideo, FileAudio, Download } from 'lucide-react'
import { apiBaseUrl } from '@arkloop/shared/api'
import { useMessageMeta } from '../contexts/message-meta'
import { useLocale } from '../contexts/LocaleContext'
import type { ArtifactRef } from '../storage'
import './ArtifactGalleryPanel.css'

type FilterType = 'all' | 'media' | 'text'

const PATH_PREFIX = '/v1/artifacts'

function isMediaType(mime: string): boolean {
  return mime.startsWith('image/') || mime.startsWith('video/') || mime.startsWith('audio/')
}

function isTextType(mime: string): boolean {
  return !isMediaType(mime)
}

function extension(filename: string): string {
  const dot = filename.lastIndexOf('.')
  return dot >= 0 ? filename.slice(dot + 1).toUpperCase() : '?'
}

function FileIcon({ mimeType, filename }: { mimeType: string; filename: string }) {
  if (mimeType.startsWith('video/')) return <FileVideo size={28} />
  if (mimeType.startsWith('audio/')) return <FileAudio size={28} />
  if (mimeType === 'text/plain') return <FileText size={28} />
  if (mimeType.startsWith('text/') ||
      mimeType === 'application/json' ||
      mimeType === 'application/xml' ||
      mimeType === 'application/javascript' ||
      mimeType === 'text/html') return <FileCode size={28} />
  return <File size={28} />
}

function ArtifactThumbnail({ artifact, accessToken, onClick }: {
  artifact: ArtifactRef
  accessToken: string
  onClick: () => void
}) {
  const [blobUrl, setBlobUrl] = useState<string | null>(null)
  const [imgError, setImgError] = useState(false)

  useEffect(() => {
    if (!isMediaType(artifact.mime_type) || !artifact.mime_type.startsWith('image/')) return
    let cancelled = false
    const url = `${apiBaseUrl()}${PATH_PREFIX}/${artifact.key}`
    fetch(url, { headers: { Authorization: `Bearer ${accessToken}` } })
      .then((res) => {
        if (!res.ok) throw new Error(`${res.status}`)
        return res.blob()
      })
      .then((blob) => {
        if (cancelled) return
        setBlobUrl(URL.createObjectURL(blob))
      })
      .catch(() => {
        if (cancelled) return
        setImgError(true)
      })
    return () => { cancelled = true }
  }, [artifact.key, artifact.mime_type, accessToken])

  useEffect(() => {
    return () => {
      if (blobUrl) URL.revokeObjectURL(blobUrl)
    }
  }, [blobUrl])

  const showImage = artifact.mime_type.startsWith('image/') && blobUrl && !imgError

  return (
    <div className="artifact-card" onClick={onClick} role="button" tabIndex={0}
      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') onClick() }}>
      <div className="artifact-card__thumb">
        {showImage ? (
          <img src={blobUrl} alt={artifact.filename} loading="lazy"
            className="artifact-card__img"
            onError={() => setImgError(true)} />
        ) : (
          <div className="artifact-card__icon">
            <FileIcon mimeType={artifact.mime_type} filename={artifact.filename} />
            <span className="artifact-card__ext">{extension(artifact.filename)}</span>
            {artifact.size > 0 && (
              <span className="artifact-card__size">{formatSize(artifact.size)}</span>
            )}
          </div>
        )}
      </div>
      <div className="artifact-card__footer">
        <span className="artifact-card__name" title={artifact.filename}>{artifact.filename}</span>
        <DownloadButton artifact={artifact} accessToken={accessToken} />
      </div>
    </div>
  )
}

function DownloadButton({ artifact, accessToken }: { artifact: ArtifactRef; accessToken: string }) {
  const handleDownload = useCallback(async (e: React.MouseEvent) => {
    e.stopPropagation()
    try {
      const url = `${apiBaseUrl()}${PATH_PREFIX}/${artifact.key}`
      const res = await fetch(url, { headers: { Authorization: `Bearer ${accessToken}` } })
      if (!res.ok) throw new Error(`${res.status}`)
      const blob = await res.blob()
      const blobUrl = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = blobUrl
      a.download = artifact.filename
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(blobUrl)
    } catch { /* ignore */ }
  }, [artifact.key, artifact.filename, accessToken])

  return (
    <button className="artifact-card__download" onClick={handleDownload}
      title="Download" aria-label={`Download ${artifact.filename}`}>
      <Download size={12} />
    </button>
  )
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

export function ArtifactGalleryPanel({ accessToken, onOpenArtifact }: {
  accessToken: string
  onOpenArtifact: (artifact: ArtifactRef) => void
}) {
  const { metaMap } = useMessageMeta()
  const { t } = useLocale()
  const [filter, setFilter] = useState<FilterType>('all')

  const artifacts = useMemo(() => {
    const seen = new Set<string>()
    const result: ArtifactRef[] = []
    for (const meta of metaMap.values()) {
      if (!meta.artifacts) continue
      for (const a of meta.artifacts) {
        if (seen.has(a.key)) continue
        seen.add(a.key)
        result.push(a)
      }
    }
    if (filter === 'all') return result
    if (filter === 'media') return result.filter(a => isMediaType(a.mime_type))
    return result.filter(a => isTextType(a.mime_type))
  }, [metaMap, filter])

  return (
    <div className="artifact-gallery">
      <div className="artifact-gallery__filter">
        <div className="artifact-gallery__filter-group">
          {(['all', 'media', 'text'] as const).map((type) => (
            <button key={type}
              className={`artifact-gallery__filter-btn${filter === type ? ' artifact-gallery__filter-btn--active' : ''}`}
              onClick={() => setFilter(type)}>
              {type === 'all' ? t.rightPanel.allFiles : type === 'media' ? t.rightPanel.mediaFiles : t.rightPanel.textFiles}
            </button>
          ))}
        </div>
        <span className="artifact-gallery__count">{artifacts.length} {t.rightPanel.allFiles}</span>
      </div>
      {artifacts.length === 0 ? (
        <div className="artifact-gallery__empty">
          {filter === 'all' ? t.rightPanel.noArtifacts : t.rightPanel.noMatchingFiles}
        </div>
      ) : (
        <div className="artifact-gallery__grid">
          {artifacts.map((a) => (
            <ArtifactThumbnail key={a.key} artifact={a} accessToken={accessToken}
              onClick={() => onOpenArtifact(a)} />
          ))}
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 2: Verify types**

Run: `cd src/apps/web && pnpm type-check`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add src/apps/web/src/components/ArtifactGalleryPanel.tsx
git commit -m "feat: add ArtifactGalleryPanel component

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 4: Create ArtifactGalleryPanel CSS

**Files:**
- Create: `src/apps/web/src/components/ArtifactGalleryPanel.css`

- [ ] **Step 1: Write the stylesheet**

```css
.artifact-gallery {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--c-bg-page);
}

.artifact-gallery__filter {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px;
  flex-shrink: 0;
}

.artifact-gallery__filter-group {
  display: flex;
  gap: 0;
  background: var(--c-bg-sub);
  border-radius: 7px;
  padding: 2px;
}

.artifact-gallery__filter-btn {
  padding: 4px 12px;
  border: none;
  border-radius: 5px;
  font-size: 11px;
  font-family: inherit;
  color: var(--c-text-secondary);
  background: transparent;
  cursor: pointer;
  transition: background 120ms, color 120ms, box-shadow 120ms;
}

.artifact-gallery__filter-btn--active {
  background: var(--c-bg-page);
  color: var(--c-text-primary);
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.08);
}

.artifact-gallery__count {
  font-size: 10px;
  color: var(--c-text-tertiary);
  flex-shrink: 0;
}

.artifact-gallery__empty {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--c-text-muted);
  font-size: 13px;
  padding: 24px;
  text-align: center;
}

.artifact-gallery__grid {
  flex: 1;
  overflow-y: auto;
  padding: 0 10px 10px;
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
  align-content: start;
}

.artifact-card {
  border: 0.5px solid var(--c-border-subtle);
  border-radius: 8px;
  overflow: hidden;
  cursor: pointer;
  transition: box-shadow 120ms;
  background: var(--c-bg-page);
}

.artifact-card:hover {
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.08);
}

.artifact-card__thumb {
  aspect-ratio: 1;
  overflow: hidden;
  background: var(--c-bg-sub);
}

.artifact-card__img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.artifact-card__icon {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2px;
  color: var(--c-text-tertiary);
}

.artifact-card__ext {
  font-size: 9px;
  font-weight: 600;
  color: var(--c-text-muted);
  text-transform: uppercase;
}

.artifact-card__size {
  font-size: 8px;
  color: var(--c-text-muted);
}

.artifact-card__footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 4px;
  padding: 5px 8px;
}

.artifact-card__name {
  font-size: 10px;
  color: var(--c-text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}

.artifact-card__download {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  border: none;
  border-radius: 4px;
  background: transparent;
  color: var(--c-text-tertiary);
  cursor: pointer;
  flex-shrink: 0;
  transition: background 120ms, color 120ms;
}

.artifact-card__download:hover {
  background: var(--c-bg-deep);
  color: var(--c-text-primary);
}
```

- [ ] **Step 2: Verify build**

Run: `cd src/apps/web && pnpm build`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add src/apps/web/src/components/ArtifactGalleryPanel.css
git commit -m "feat: add ArtifactGalleryPanel styles

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 5: Integrate into ChatView

**Files:**
- Modify: `src/apps/web/src/components/ChatView.tsx`

- [ ] **Step 1: Add import for ArtifactGalleryPanel**

At the top of ChatView.tsx alongside the other panel component imports, add:

```tsx
import { ArtifactGalleryPanel } from './ArtifactGalleryPanel'
```

- [ ] **Step 2: Add 'artifacts' addOption to rightPanelAddOptions**

Find the `rightPanelAddOptions` useMemo (around line 3035). Add after the `web` option:

```tsx
const rightPanelAddOptions = useMemo(() => [
  {
    id: 'web',
    label: t.rightPanel.browser,
    icon: <Globe2 size={14} />,
    onSelect: () => {
      const id = `web:${browserTabSeqRef.current + 1}`
      browserTabSeqRef.current += 1
      setExtraBrowserTabs((current) => [...current, { id, resource: null }])
      setRightPanelVisible(true)
      setActiveRightPanelTabId(id)
    },
  },
  {
    id: 'artifacts',
    label: t.rightPanel.gallery,
    icon: <Grid3x3 size={14} />,
    onSelect: () => {
      upsertRightPanelTab({ id: 'artifacts', kind: 'artifacts', title: t.rightPanel.gallery })
      setRightPanelVisible(true)
    },
  },
], [t.rightPanel.browser, t.rightPanel.gallery, upsertRightPanelTab])
```

Add `Grid3x3` import from lucide at the top:

```tsx
import { ..., Globe2, Grid3x3, ... } from 'lucide-react'
```

- [ ] **Step 3: Extend RightPanelStoredTab type**

Find `RightPanelStoredTab` type (around line 292). Add a new variant:

```tsx
type RightPanelStoredTab =
  | { id: string; kind: 'source'; title: string; messageId: string }
  | { id: string; kind: 'code'; title: string; execution: CodeExecution }
  | { id: string; kind: 'agent'; title: string; agent: SubAgentRef }
  | { id: string; kind: 'resource'; title: string; resource: ResourceRef; artifacts?: ArtifactRef[]; runId?: string }
  | { id: string; kind: 'artifacts'; title: string }
```

- [ ] **Step 4: Handle 'artifacts' in buildStoredPanelTab**

In `buildStoredPanelTab` (around line 2807), add before the final `return`:

```tsx
if (tab.kind === 'artifacts') {
  return {
    id: tab.id,
    kind: 'artifacts',
    title: tab.title,
    content: (
      <div style={{ width: '100%', height: '100%', contain: 'layout style' }}>
        <ArtifactGalleryPanel
          accessToken={accessToken}
          onOpenArtifact={(artifact) => {
            const tabId = `resource:artifact:${artifact.key}`
            upsertRightPanelTab({
              id: tabId,
              kind: 'resource',
              title: artifact.title || artifact.filename,
              resource: {
                kind: 'artifact',
                key: artifact.key,
                filename: artifact.filename,
                mimeType: artifact.mime_type,
                size: artifact.size,
                title: artifact.title,
              },
            })
            setActiveRightPanelTabId(tabId)
          }}
        />
      </div>
    ),
  }
}
```

Update the useCallback dependency array to include `upsertRightPanelTab`:

```tsx
}, [accessToken, closeRightPanelTab, handleBuildPlan, onOpenSettings, resolvedMessageSources, workPanelFolder, upsertRightPanelTab, t.rightPanel.gallery])
```

Actually check the existing deps and add the missing ones. `setActiveRightPanelTabId` doesn't need to be in deps (it's a useState setter).

- [ ] **Step 5: Handle 'artifacts' tab close in closeRightPanelTab**

In `closeRightPanelTab` (around line 2684), add a case for artifacts:

```tsx
if (tab.kind === 'artifacts') {
  // just close it like a normal tab
  setRightPanelTabs((current) => current.filter((item) => item.id !== id))
  setActiveRightPanelTabId((activeId) => activeId === id ? 'web' : activeId)
  return
}
```

Actually, wait — the current closeRightPanelTab already has a generic handler at the bottom that handles most stored tabs. Let me check if artifacts needs special handling...

Looking at the current closeRightPanelTab code (lines 2684-2719): the generic handler at lines 2700-2718 handles source/code/agent/resource tab closures, but there's no generic catch-all for unknown kinds. Let me add a specific case for artifacts, and also handle it in the generic path.

Actually, looking more carefully at lines 2700-2718, the code already handles all stored tabs generically — it checks for `source`, `code`, `agent`, and `resource` specific cleanup, then removes the tab. For `artifacts`, we don't need any special cleanup (no panel to close), so it would just fall through to the removal part. But wait, the code structure is:

```
setRightPanelTabs((current) => {
  const index = current.findIndex(...)
  if (index < 0) return current
  const target = current[index]
  // special cleanup for source/code/agent/resource...
  const next = current.filter(...)
  setActiveRightPanelTabId(...)
  return next
})
```

So if the tab kind is `artifacts`, none of the special cleanup conditions match, it just removes the tab and sets the activeTabId. That's actually the correct behavior! No extra code needed.

So step 5 is unnecessary. Let me remove it.

- [ ] **Step 5: Verify types and build**

Run: `cd src/apps/web && pnpm type-check`
Expected: PASS

Run: `cd src/apps/web && pnpm build`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add src/apps/web/src/components/ChatView.tsx
git commit -m "feat: integrate ArtifactGalleryPanel into right panel

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Verification

- [ ] Run `cd src/apps/web && pnpm type-check` — should pass with no new errors
- [ ] Run `cd src/apps/web && pnpm build` — should produce a valid build
- [ ] Manual test: Open a thread with artifacts, click "+" in right panel → "Gallery" → verify grid renders
- [ ] Manual test: Filter by Media / Text — verify filtering works
- [ ] Manual test: Click thumbnail → opens artifact preview tab
- [ ] Manual test: Click download button → file downloads
- [ ] Manual test: Thread with no artifacts → empty state message shown
- [ ] Manual test: Close gallery tab → tab closes normally
