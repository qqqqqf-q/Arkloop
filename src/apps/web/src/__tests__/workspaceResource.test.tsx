import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { WorkspaceResource } from '../components/WorkspaceResource'
import { LocaleProvider } from '../contexts/LocaleContext'

function flushMicrotasks(): Promise<void> {
  return Promise.resolve()
    .then(() => Promise.resolve())
    .then(() => Promise.resolve())
}

describe('WorkspaceResource', () => {
  const originalFetch = globalThis.fetch
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
  })

  afterEach(() => {
    vi.restoreAllMocks()
    globalThis.fetch = originalFetch
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
})
