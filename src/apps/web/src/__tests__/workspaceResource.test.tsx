import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi, type MockInstance } from 'vitest'

import { WorkspaceResource } from '../components/WorkspaceResource'
import { LocaleProvider } from '../contexts/LocaleContext'

function flushMicrotasks(): Promise<void> {
  return Promise.resolve()
    .then(() => Promise.resolve())
    .then(() => Promise.resolve())
}

describe('WorkspaceResource', () => {
  const originalFetch = globalThis.fetch
  const originalRAF = globalThis.requestAnimationFrame
  const originalCAF = globalThis.cancelAnimationFrame
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  let createObjectURLSpy: MockInstance<(obj: Blob | MediaSource) => string>
  let revokeObjectURLSpy: MockInstance<(url: string) => void>

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    createObjectURLSpy = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:workspace-preview')
    revokeObjectURLSpy = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    globalThis.requestAnimationFrame = (cb: FrameRequestCallback) => {
      cb(performance.now())
      return 0
    }
    globalThis.cancelAnimationFrame = () => {}
  })

  afterEach(() => {
    createObjectURLSpy?.mockRestore()
    revokeObjectURLSpy?.mockRestore()
    globalThis.fetch = originalFetch
    globalThis.requestAnimationFrame = originalRAF
    globalThis.cancelAnimationFrame = originalCAF
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
    const runPreview = async (
      file: { path: string; filename: string; mime_type: string },
      body: string,
      contentType: string,
    ) => {
      const fetchMock = vi.fn().mockResolvedValue(
        new Response(body, { status: 200, headers: { 'content-type': contentType } }),
      )
      globalThis.fetch = fetchMock as typeof fetch

      const container = document.createElement('div')
      document.body.appendChild(container)
      const root = createRoot(container)

      await act(async () => {
        root.render(
          <LocaleProvider>
            <WorkspaceResource accessToken="token" runId="run-1" file={file} />
          </LocaleProvider>,
        )
      })
      await act(async () => {
        await flushMicrotasks()
      })

      expect(container.querySelectorAll('[data-workspace-kind="html"]')).toHaveLength(1)
      expect(container.querySelectorAll('iframe')).toHaveLength(1)
      expect(fetchMock).toHaveBeenCalledTimes(1)

      await act(async () => {
        root.unmount()
      })
      container.remove()
    }

    await runPreview(
      { path: '/dash/index.html', filename: 'index.html', mime_type: 'text/html' },
      '<html><body>hello</body></html>',
      'text/html',
    )
    await runPreview(
      { path: '/dash/diagram.svg', filename: 'diagram.svg', mime_type: 'image/svg+xml' },
      '<svg viewBox="0 0 10 10"><text x="1" y="8">ok</text></svg>',
      'image/svg+xml',
    )
  })
})
