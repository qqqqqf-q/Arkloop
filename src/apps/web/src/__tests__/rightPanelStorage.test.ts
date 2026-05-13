import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { readThreadRightPanelState, writeThreadRightPanelState } from '../storage'

vi.mock('@arkloop/shared/storage', () => ({
  canUseStorage: () => true,
  readAccessToken: () => null,
  writeAccessToken: () => {},
  clearAccessToken: () => {},
}))

describe('thread right panel storage', () => {
  let items: Map<string, string>

  beforeEach(() => {
    items = new Map()
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        getItem: (key: string) => items.get(key) ?? null,
        setItem: (key: string, value: string) => { items.set(key, value) },
        removeItem: (key: string) => { items.delete(key) },
        clear: () => { items.clear() },
        key: (index: number) => Array.from(items.keys())[index] ?? null,
        get length() { return items.size },
      },
    })
  })

  afterEach(() => {
    items.clear()
  })

  it('按 thread 隔离 browser tab 状态', () => {
    writeThreadRightPanelState('thread-a', {
      visible: true,
      activeTabId: 'web:1',
      tabOrder: ['web', 'web:1'],
      web: {
        kind: 'browser',
        url: 'example.com',
      },
      browserTabs: [
        {
          id: 'web:1',
          resource: {
            kind: 'browser',
            url: 'https://arkloop.dev/docs',
            title: 'Docs',
          },
        },
      ],
      resourceTabs: [],
      filesPreview: null,
    })
    writeThreadRightPanelState('thread-b', {
      visible: false,
      activeTabId: null,
      tabOrder: [],
      web: null,
      browserTabs: [],
      resourceTabs: [],
      filesPreview: null,
    })

    expect(readThreadRightPanelState('thread-a')?.activeTabId).toBe('web:1')
    expect(readThreadRightPanelState('thread-a')?.web?.url).toBe('http://example.com/')
    expect(readThreadRightPanelState('thread-a')?.browserTabs[0]?.resource?.title).toBe('Docs')
    expect(readThreadRightPanelState('thread-b')?.browserTabs).toEqual([])
  })

  it('只恢复当前 work folder 下的 files preview', () => {
    writeThreadRightPanelState('thread-a', {
      visible: true,
      activeTabId: 'files',
      tabOrder: ['files'],
      web: null,
      browserTabs: [],
      resourceTabs: [],
      filesPreview: {
        kind: 'local-file',
        rootPath: '/Users/dev/project-a',
        path: 'report.html',
        filename: 'report.html',
      },
    }, { workFolder: '/Users/dev/project-a' })

    expect(readThreadRightPanelState('thread-a', { workFolder: '/Users/dev/project-a' })?.filesPreview?.path).toBe('report.html')
    expect(readThreadRightPanelState('thread-a', { workFolder: '/Users/dev/project-b' })?.filesPreview).toBeNull()
  })

  it('恢复 pinned local file tabs 和当前选择', () => {
    writeThreadRightPanelState('thread-a', {
      visible: true,
      activeTabId: 'local-file:2',
      tabOrder: ['web', 'local-file:1', 'local-file:2'],
      web: null,
      browserTabs: [],
      resourceTabs: [
        {
          id: 'local-file:1',
          title: 'bench.py',
          resource: {
            kind: 'local-file',
            rootPath: '/Users/dev/project-a',
            path: 'bench.py',
            filename: 'bench.py',
          },
        },
        {
          id: 'local-file:2',
          title: 'report.html',
          resource: {
            kind: 'local-file',
            rootPath: '/Users/dev/project-a',
            path: 'report.html',
            filename: 'report.html',
          },
        },
      ],
      filesPreview: null,
    }, { workFolder: '/Users/dev/project-a' })

    const restored = readThreadRightPanelState('thread-a', { workFolder: '/Users/dev/project-a' })
    expect(restored?.activeTabId).toBe('local-file:2')
    expect(restored?.tabOrder).toEqual(['web', 'local-file:1', 'local-file:2'])
    expect(restored?.resourceTabs.map((tab) => tab.id)).toEqual(['local-file:1', 'local-file:2'])
    expect(restored?.resourceTabs[1]?.resource.kind).toBe('local-file')
    const secondResource = restored?.resourceTabs[1]?.resource
    expect(secondResource?.kind === 'local-file' ? secondResource.path : null).toBe('report.html')
  })
})
