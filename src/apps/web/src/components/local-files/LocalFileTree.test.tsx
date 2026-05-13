import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { LocalFileResourceRef } from '../resource-preview/types'
import { LocalFileTree } from './LocalFileTree'

function flushMicrotasks(): Promise<void> {
  return Promise.resolve()
    .then(() => Promise.resolve())
    .then(() => Promise.resolve())
}

function installDesktopFs(listDir: ReturnType<typeof vi.fn>) {
  Object.defineProperty(globalThis, 'arkloop', {
    configurable: true,
    writable: true,
    value: {
      isDesktop: true,
      fs: { listDir },
    },
  })
}

function renderTree(rootPath: string, onOpenFile: (ref: LocalFileResourceRef) => void = vi.fn()) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  act(() => {
    root.render(<LocalFileTree rootPath={rootPath} onOpenFile={onOpenFile} />)
  })

  return { container, root, onOpenFile }
}

function renderSearchTree(rootPath: string, searchQuery: string) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  act(() => {
    root.render(<LocalFileTree rootPath={rootPath} searchQuery={searchQuery} onOpenFile={vi.fn()} />)
  })

  return { container, root }
}

async function cleanup(root: Root, container: HTMLElement) {
  await act(async () => {
    root.unmount()
  })
  container.remove()
}

describe('LocalFileTree', () => {
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
  })

  afterEach(() => {
    delete (globalThis as Record<string, unknown>).arkloop
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('只有 rootPath 存在时才读取本地目录', async () => {
    const listDir = vi.fn().mockResolvedValue({ entries: [] })
    installDesktopFs(listDir)

    const empty = renderTree('')
    await act(async () => {
      await flushMicrotasks()
    })

    expect(listDir).not.toHaveBeenCalled()
    expect(empty.container.textContent).toContain('No folder selected')

    await cleanup(empty.root, empty.container)

    const ready = renderTree('/Users/dev/project')
    await act(async () => {
      await flushMicrotasks()
    })

    expect(listDir).toHaveBeenCalledTimes(1)
    expect(listDir).toHaveBeenCalledWith('/Users/dev/project', undefined)

    await cleanup(ready.root, ready.container)
  })

  it('展开目录时按 entry.path 读取子目录', async () => {
    const listDir = vi.fn()
      .mockResolvedValueOnce({
        entries: [
          { name: 'src', path: 'src', type: 'dir' },
          { name: 'README.md', path: 'README.md', type: 'file' },
        ],
      })
      .mockResolvedValueOnce({
        entries: [
          { name: 'index.ts', path: 'src/index.ts', type: 'file' },
        ],
      })
    installDesktopFs(listDir)

    const { container, root } = renderTree('/repo')
    await act(async () => {
      await flushMicrotasks()
    })

    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-path="src"]')?.click()
      await flushMicrotasks()
    })

    expect(listDir).toHaveBeenCalledWith('/repo', 'src')
    expect(container.textContent).toContain('index.ts')

    await cleanup(root, container)
  })

  it('点击文件回调 local-file ResourceRef', async () => {
    const listDir = vi.fn().mockResolvedValue({
      entries: [
        { name: 'note.txt', path: 'notes/note.txt', type: 'file' },
      ],
    })
    const onOpenFile = vi.fn<(ref: LocalFileResourceRef) => void>()
    installDesktopFs(listDir)

    const { container, root } = renderTree('/repo', onOpenFile)
    await act(async () => {
      await flushMicrotasks()
    })

    act(() => {
      container.querySelector<HTMLButtonElement>('[data-path="notes/note.txt"]')?.click()
    })

    expect(onOpenFile).toHaveBeenCalledWith({
      kind: 'local-file',
      rootPath: '/repo',
      path: 'notes/note.txt',
      name: 'note.txt',
    })

    await cleanup(root, container)
  })

  it('不显示 rootPath，但在文件行保留完整路径标题', async () => {
    const listDir = vi.fn().mockResolvedValue({
      entries: [
        { name: 'app.tsx', path: 'src/app.tsx', type: 'file' },
      ],
    })
    installDesktopFs(listDir)

    const { container, root } = renderTree('/Users/dev/project')
    await act(async () => {
      await flushMicrotasks()
    })

    expect(container.textContent).not.toContain('/Users/dev/project')
    expect(container.querySelector('[data-path="src/app.tsx"]')?.getAttribute('title')).toBe('/Users/dev/project/src/app.tsx')

    await cleanup(root, container)
  })

  it('按文件名和目录名渲染图标与基础装饰', async () => {
    const listDir = vi.fn().mockResolvedValue({
      entries: [
        { name: 'src', path: 'src', type: 'dir' },
        { name: 'node_modules', path: 'node_modules', type: 'dir' },
        { name: 'package.json', path: 'package.json', type: 'file' },
      ],
    })
    installDesktopFs(listDir)

    const { container, root } = renderTree('/repo')
    await act(async () => {
      await flushMicrotasks()
    })

    expect(container.querySelector('[data-path="src"]')?.className).toContain('local-file-tree__row--accent')
    expect(container.querySelector('[data-path="node_modules"]')?.className).toContain('local-file-tree__row--muted')
    expect(container.querySelector('[data-path="src"] .local-file-tree__icon-image')).toBeNull()

    await act(async () => {
      await flushMicrotasks()
    })

    expect(container.querySelectorAll('.local-file-tree__icon-image').length).toBeGreaterThan(0)

    await cleanup(root, container)
  })

  it('按 searchQuery 过滤已加载文件', async () => {
    const listDir = vi.fn().mockResolvedValue({
      entries: [
        { name: 'README.md', path: 'README.md', type: 'file' },
        { name: 'package.json', path: 'package.json', type: 'file' },
      ],
    })
    installDesktopFs(listDir)

    const { container, root } = renderSearchTree('/repo', 'read')
    await act(async () => {
      await flushMicrotasks()
    })

    expect(container.textContent).toContain('README.md')
    expect(container.textContent).not.toContain('package.json')

    await cleanup(root, container)
  })
})
