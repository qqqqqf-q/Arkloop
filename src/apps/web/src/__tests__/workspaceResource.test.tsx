import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { WorkspaceResource } from '../components/WorkspaceResource'
import { LocaleProvider } from '../contexts/LocaleContext'

type URLWithObjectURL = typeof URL & {
  createObjectURL?: (object: Blob) => string
  revokeObjectURL?: (url: string) => void
}

function flushMicrotasks(): Promise<void> {
  return Promise.resolve()
    .then(() => Promise.resolve())
    .then(() => Promise.resolve())
}

describe('WorkspaceResource', () => {
  const originalFetch = globalThis.fetch
  const urlWithObjectURL = URL as URLWithObjectURL
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalCreateObjectURL = urlWithObjectURL.createObjectURL
  const originalRevokeObjectURL = urlWithObjectURL.revokeObjectURL
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    urlWithObjectURL.createObjectURL = vi.fn(() => 'blob:workspace-preview')
    urlWithObjectURL.revokeObjectURL = vi.fn()
  })

  afterEach(() => {
    vi.restoreAllMocks()
    globalThis.fetch = originalFetch
    if (originalCreateObjectURL) {
      urlWithObjectURL.createObjectURL = originalCreateObjectURL
    } else {
      Reflect.deleteProperty(urlWithObjectURL, 'createObjectURL')
    }
    if (originalRevokeObjectURL) {
      urlWithObjectURL.revokeObjectURL = originalRevokeObjectURL
    } else {
      Reflect.deleteProperty(urlWithObjectURL, 'revokeObjectURL')
    }
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('保留 runId 文件读取链路', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response('hello', { status: 200, headers: { 'content-type': 'text/plain' } }),
    )
    globalThis.fetch = fetchMock as typeof fetch

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <WorkspaceResource
            accessToken="token"
            runId="run-1"
            file={{ path: '/notes/example.txt', filename: 'example.txt', mime_type: 'text/plain' }}
          />
        </LocaleProvider>,
      )
    })
    await act(async () => {
      await flushMicrotasks()
    })

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0]?.[0]).toContain('/v1/workspace-files?run_id=run-1&path=%2Fnotes%2Fexample.txt')
    await act(async () => {
      root.unmount()
    })
    container.remove()
  })

  it('支持 projectId 文件读取链路', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response('hello', { status: 200, headers: { 'content-type': 'text/plain' } }),
    )
    globalThis.fetch = fetchMock as typeof fetch

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <WorkspaceResource
            accessToken="token"
            projectId="project-1"
            file={{ path: '/notes/example.txt', filename: 'example.txt', mime_type: 'text/plain' }}
          />
        </LocaleProvider>,
      )
    })
    await act(async () => {
      await flushMicrotasks()
    })

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0]?.[0]).toContain('/v1/projects/project-1/workspace/file?path=%2Fnotes%2Fexample.txt')
    await act(async () => {
      root.unmount()
    })
    container.remove()
  })

  it('将 html 与 svg 预览切到统一 runtime', async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(new Response('<html><body>hello</body></html>', {
        status: 200,
        headers: { 'content-type': 'text/html' },
      }))
      .mockResolvedValueOnce(new Response('<svg viewBox="0 0 10 10"><text x="1" y="8">ok</text></svg>', {
        status: 200,
        headers: { 'content-type': 'image/svg+xml' },
      }))
    globalThis.fetch = fetchMock as typeof fetch

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <div>
            <WorkspaceResource
              accessToken="token"
              runId="run-1"
              file={{ path: '/dash/index.html', filename: 'index.html', mime_type: 'text/html' }}
            />
            <WorkspaceResource
              accessToken="token"
              runId="run-1"
              file={{ path: '/dash/diagram.svg', filename: 'diagram.svg', mime_type: 'image/svg+xml' }}
            />
          </div>
        </LocaleProvider>,
      )
    })
    await act(async () => {
      await flushMicrotasks()
    })

    expect(container.querySelectorAll('[data-workspace-kind="html"]')).toHaveLength(2)
    expect(container.querySelectorAll('iframe')).toHaveLength(2)

    await act(async () => {
      root.unmount()
    })
    container.remove()
  })
})
