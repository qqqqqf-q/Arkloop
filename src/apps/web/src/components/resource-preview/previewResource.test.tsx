import { renderToStaticMarkup } from 'react-dom/server'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, type ComponentProps } from 'react'
import { createRoot } from 'react-dom/client'
import { LocaleProvider } from '../../contexts/LocaleContext'
import { PreviewResourceView } from './PreviewResourceView'
import { ResourcePreviewPanel } from './ResourcePreviewPanel'
import { getPreviewRendererKind } from './rendererKind'
import { buildWorkspaceResourceUrl, loadPreviewResource } from './loader'
import { guessMimeType, normalizeMimeType } from './mime'
import type { PreviewResource, ResourceRef } from './types'

vi.mock('@arkloop/shared/desktop', () => ({
  getDesktopApi: () => (globalThis as typeof globalThis & { __desktopApi?: unknown }).__desktopApi ?? null,
}))

vi.mock('@arkloop/shared/api', () => ({
  apiBaseUrl: () => 'http://api.test',
}))

beforeEach(() => {
  localStorage.setItem?.('arkloop:web:browser-renderer', '')
  delete (globalThis as typeof globalThis & { __desktopApi?: unknown }).__desktopApi
})

function preview(source: PreviewResource['source'], mimeType: string, filename: string): PreviewResource {
  return {
    source,
    ref: source === 'artifact'
      ? { kind: source, source, key: `${source}-key`, filename }
      : source === 'local-file'
        ? { kind: source, source, rootPath: '/root', path: filename }
        : source === 'workspace-file'
          ? { kind: source, source, path: filename, runId: 'run-1' }
          : { kind: source, source, url: `https://example.com/${filename}` },
    mimeType,
    filename,
    text: 'hello',
  }
}

function LocalizedResourcePreviewPanel(props: ComponentProps<typeof ResourcePreviewPanel>) {
  return (
    <LocaleProvider>
      <ResourcePreviewPanel {...props} />
    </LocaleProvider>
  )
}

function setInputValue(input: HTMLInputElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')?.set
  setter?.call(input, value)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}

describe('resource preview renderer registry', () => {
  it('同 mime 不同 source 走同一 renderer', () => {
    const resources = [
      preview('artifact', 'text/markdown', 'note.md'),
      preview('local-file', 'text/markdown', 'note.md'),
      preview('workspace-file', 'text/markdown', 'note.md'),
    ]

    expect(resources.map(getPreviewRendererKind)).toEqual(['markdown', 'markdown', 'markdown'])
  })

  it('按 mime 分派 renderer，不读取来源字段', () => {
    expect(getPreviewRendererKind(preview('artifact', 'application/json', 'data.json'))).toBe('json')
    expect(getPreviewRendererKind(preview('workspace-file', 'image/png', 'chart.png'))).toBe('image')
    expect(getPreviewRendererKind(preview('local-file', 'text/html', 'index.html'))).toBe('iframe')
    expect(getPreviewRendererKind(preview('artifact', 'application/octet-stream', 'model.bin'))).toBe('binary')
  })

  it('按文件名修正 octet-stream 文本文件', () => {
    expect(guessMimeType('go.work')).toBe('text/plain')
    expect(normalizeMimeType('application/octet-stream', 'go.work')).toBe('text/plain')
    expect(normalizeMimeType('application/octet-stream', '.env.example')).toBe('text/plain')
    expect(normalizeMimeType('application/octet-stream', '.gitignore')).toBe('text/plain')
  })

  it('组件暴露可测的 renderer 标记', () => {
    const html = renderToStaticMarkup(
      <PreviewResourceView resource={preview('workspace-file', 'application/json', 'data.json')} />,
    )

    expect(html).toContain('data-preview-renderer="json"')
    expect(html).toContain('hello')
  })

  it('Markdown preview 会把 Cursor 风格 plan front matter 渲染成计划文档', () => {
    const resource = preview('local-file', 'text/markdown', 'plan.md')
    resource.text = `---
name: Channel Phase 1 Implementation
overview: 实现 Channel Integration Phase 1A。
todos:
  - id: 1a-migration
    content: "创建 00130_channels.sql migration"
    status: completed
  - id: frontend-component
    content: "新建 ChannelsSettingsContent.tsx"
    status: pending
isProject: false
---`

    const html = renderToStaticMarkup(<PreviewResourceView resource={resource} />)

    expect(html).toContain('Channel Phase 1 Implementation')
    expect(html).toContain('实现 Channel Integration Phase 1A')
    expect(html).toContain('创建 00130_channels.sql migration')
    expect(html).toContain('新建 ChannelsSettingsContent.tsx')
    expect(html).not.toContain('isProject')
    expect(html).not.toContain('1a-migration')
  })

  it('browser resource 使用独立 iframe renderer', async () => {
    const originalOpen = window.open
    const openMock = vi.fn()
    window.open = openMock as typeof window.open
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    try {
      await act(async () => {
        root.render(
          <LocalizedResourcePreviewPanel
            accessToken="token"
            resource={{ kind: 'browser', url: 'http://localhost:5173/app', title: 'localhost' }}
          />,
        )
      })

      const iframe = container.querySelector('iframe.browser-panel__frame') as HTMLIFrameElement | null
      const input = container.querySelector('input.browser-panel__address-input') as HTMLInputElement | null
      expect(iframe).not.toBeNull()
      expect(iframe?.getAttribute('src')).toBe('http://localhost:5173/app')
      expect(input?.placeholder).toBe('搜索或输入 URL')
      expect(openMock).not.toHaveBeenCalled()
    } finally {
      act(() => root.unmount())
      container.remove()
      window.open = originalOpen
    }
  })

  it('browser 空状态不渲染 about:blank 或历史记录', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    try {
      await act(async () => {
        root.render(
          <LocalizedResourcePreviewPanel
            accessToken="token"
            resource={{ kind: 'browser', url: '', title: 'Web' }}
          />,
        )
      })

      const input = container.querySelector('input.browser-panel__address-input') as HTMLInputElement | null
      expect(input?.value).toBe('')
      expect(container.querySelector('iframe.browser-panel__frame')).toBeNull()
      expect(container.textContent).not.toContain('about:blank')
    } finally {
      act(() => root.unmount())
      container.remove()
    }
  })

  it('browser history 点击已有记录不会重复写入', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    try {
      await act(async () => {
        root.render(
          <LocalizedResourcePreviewPanel
            accessToken="token"
            resource={{ kind: 'browser', url: 'http://localhost:5173/app', title: 'localhost' }}
          />,
        )
      })

      const form = container.querySelector('form.browser-panel__address') as HTMLFormElement | null
      const input = container.querySelector('input.browser-panel__address-input') as HTMLInputElement | null
      expect(form).not.toBeNull()
      expect(input).not.toBeNull()

      await act(async () => {
        setInputValue(input!, 'https://example.com')
        form!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
      })

      const historyButton = (url: string) => (
        Array.from(container.querySelectorAll<HTMLButtonElement>('.browser-panel__history-list button'))
          .find((button) => button.title === url)
      )

      await act(async () => {
        historyButton('http://localhost:5173/app')?.click()
      })
      await act(async () => {
        historyButton('https://example.com/')?.click()
      })
      await act(async () => {
        historyButton('http://localhost:5173/app')?.click()
      })

      const historyButtons = Array.from(container.querySelectorAll<HTMLButtonElement>('.browser-panel__history-list button'))
      expect(historyButtons.filter((button) => button.title === 'http://localhost:5173/app')).toHaveLength(1)
      expect(historyButtons.filter((button) => button.title === 'https://example.com/')).toHaveLength(1)
      expect(container.querySelector('iframe.browser-panel__frame')?.getAttribute('src')).toBe('http://localhost:5173/app')
    } finally {
      act(() => root.unmount())
      container.remove()
    }
  })

  it('browser 导航会回报当前页面 identity 并渲染 favicon', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const onResourceChange = vi.fn()

    try {
      await act(async () => {
        root.render(
          <LocalizedResourcePreviewPanel
            accessToken="token"
            resource={{ kind: 'browser', url: '', title: 'Web' }}
            onResourceChange={onResourceChange}
          />,
        )
      })

      const form = container.querySelector('form.browser-panel__address') as HTMLFormElement | null
      const input = container.querySelector('input.browser-panel__address-input') as HTMLInputElement | null

      await act(async () => {
        setInputValue(input!, 'example.com')
        form!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
      })

      expect(onResourceChange).toHaveBeenCalledWith(expect.objectContaining({
        kind: 'browser',
        url: 'http://example.com/',
        title: 'example.com',
      }))
      expect(onResourceChange.mock.calls[0]?.[0]?.faviconUrl).toContain('google.com/s2/favicons')
      expect(container.querySelector('form.browser-panel__address img')).not.toBeNull()
      expect(container.querySelector('.browser-panel__history-list img')).not.toBeNull()
    } finally {
      act(() => root.unmount())
      container.remove()
    }
  })

  it('browser 使用 desktop metadata 更新标题', async () => {
    const host = globalThis as typeof globalThis & { __desktopApi?: unknown }
    host.__desktopApi = {
      app: {
        fetchPageMetadata: vi.fn().mockResolvedValue({ title: 'Example Domain' }),
      },
    }
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const onResourceChange = vi.fn()

    try {
      await act(async () => {
        root.render(
          <LocalizedResourcePreviewPanel
            accessToken="token"
            resource={{ kind: 'browser', url: 'https://example.com', title: 'example.com' }}
            onResourceChange={onResourceChange}
          />,
        )
      })
      await act(async () => {
        await Promise.resolve()
      })

      expect(onResourceChange).toHaveBeenCalledWith(expect.objectContaining({
        title: 'Example Domain',
        url: 'https://example.com/',
      }))
      expect(container.querySelector('.browser-panel__history-list')?.textContent).toContain('Example Domain')
    } finally {
      act(() => root.unmount())
      container.remove()
    }
  })

  it('browser history 持久化到前端状态', async () => {
    const first = document.createElement('div')
    document.body.appendChild(first)
    const firstRoot = createRoot(first)

    await act(async () => {
      firstRoot.render(<LocalizedResourcePreviewPanel accessToken="token" resource={{ kind: 'browser', url: '', title: 'Web' }} />)
    })
    const firstForm = first.querySelector('form.browser-panel__address') as HTMLFormElement
    const firstInput = first.querySelector('input.browser-panel__address-input') as HTMLInputElement
    await act(async () => {
      setInputValue(firstInput, 'example.com')
      firstForm.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    })
    await act(async () => {
      firstRoot.unmount()
    })
    first.remove()

    const second = document.createElement('div')
    document.body.appendChild(second)
    const secondRoot = createRoot(second)
    try {
      await act(async () => {
        secondRoot.render(<LocalizedResourcePreviewPanel accessToken="token" resource={{ kind: 'browser', url: '', title: 'Web' }} />)
      })

      expect(second.querySelector('.browser-panel__history-list')?.textContent).toContain('example.com')
    } finally {
      await act(async () => {
        secondRoot.unmount()
      })
      second.remove()
    }
  })

  it('browser 地址栏显示中文域名', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    try {
      await act(async () => {
        root.render(
          <LocalizedResourcePreviewPanel
            accessToken="token"
            resource={{ kind: 'browser', url: 'http://xn--yeto2zmxe103c.fun/', title: '清风小栈.fun' }}
          />,
        )
      })

      const input = container.querySelector('input.browser-panel__address-input') as HTMLInputElement | null
      expect(input?.value).toBe('http://清风小栈.fun/')
    } finally {
      act(() => root.unmount())
      container.remove()
    }
  })
})

describe('resource preview loader', () => {
  const originalFetch = globalThis.fetch
  const originalCreateObjectURL = URL.createObjectURL
  const originalDesktopApi = (globalThis as typeof globalThis & { __desktopApi?: unknown }).__desktopApi

  afterEach(() => {
    globalThis.fetch = originalFetch
    URL.createObjectURL = originalCreateObjectURL
    const host = globalThis as typeof globalThis & { __desktopApi?: unknown }
    if (originalDesktopApi === undefined) {
      delete host.__desktopApi
    } else {
      host.__desktopApi = originalDesktopApi
    }
  })

  it('local-file 通过 desktop fs 读取并归一化为文本资源', async () => {
    const host = globalThis as typeof globalThis & { __desktopApi?: unknown }
    host.__desktopApi = {
      fs: {
        readFile: vi.fn().mockResolvedValue({
          data: btoa('hello local'),
          mime_type: 'text/plain',
        }),
      },
    }

    const resource = await loadPreviewResource({
      source: 'local-file',
      kind: 'local-file',
      rootPath: '/root',
      path: 'notes/a.txt',
    })

    expect(resource).toMatchObject({
      source: 'local-file',
      filename: 'a.txt',
      mimeType: 'text/plain',
      text: 'hello local',
    })
  })

  it('artifact 使用 /v1/artifacts/:key 并输出 blobUrl', async () => {
    URL.createObjectURL = vi.fn().mockReturnValue('blob:artifact')
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(new Blob(['png'], { type: 'image/png' }), {
        status: 200,
        headers: { 'content-type': 'image/png', 'content-length': '3' },
      }),
    )
    globalThis.fetch = fetchMock as typeof fetch

    const resource = await loadPreviewResource({
      source: 'artifact',
      kind: 'artifact',
      key: 'artifact-key',
      filename: 'image.png',
      mimeType: 'image/png',
    }, { accessToken: 'token' })

    expect(fetchMock.mock.calls[0]?.[0]).toBe('http://api.test/v1/artifacts/artifact-key')
    expect(fetchMock.mock.calls[0]?.[1]?.headers).toEqual({ Authorization: 'Bearer token' })
    expect(resource).toMatchObject({
      source: 'artifact',
      filename: 'image.png',
      mimeType: 'image/png',
      size: 3,
      blobUrl: 'blob:artifact',
    })
  })

  it('workspace-file 复用 run/project endpoint 构造', () => {
    const runRef: ResourceRef = { kind: 'workspace-file', source: 'workspace-file', runId: 'run-1', path: 'notes/a.txt' }
    const projectRef: ResourceRef = { kind: 'workspace-file', source: 'workspace-file', projectId: 'project-1', path: '/notes/a.txt' }

    expect(buildWorkspaceResourceUrl(runRef)).toBe('http://api.test/v1/workspace-files?run_id=run-1&path=%2Fnotes%2Fa.txt')
    expect(buildWorkspaceResourceUrl(projectRef)).toBe('http://api.test/v1/projects/project-1/workspace/file?path=%2Fnotes%2Fa.txt')
  })
})
