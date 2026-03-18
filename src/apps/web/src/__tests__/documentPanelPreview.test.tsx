import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act } from 'react'
import { createRoot } from 'react-dom/client'

import { DocumentPanel } from '../components/DocumentPanel'
import { LocaleProvider } from '../contexts/LocaleContext'
import type { ArtifactRef } from '../storage'

type URLWithObjectURL = typeof URL & {
  createObjectURL?: (object: Blob) => string
  revokeObjectURL?: (url: string) => void
}

type GlobalWithActEnvironment = typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}

function flushMicrotasks(): Promise<void> {
  return Promise.resolve()
    .then(() => Promise.resolve())
    .then(() => Promise.resolve())
    .then(() => Promise.resolve())
}

describe('DocumentPanel artifact preview', () => {
  const urlWithObjectURL = URL as URLWithObjectURL
  const actEnvironmentGlobal = globalThis as GlobalWithActEnvironment
  const originalCreateObjectURL = urlWithObjectURL.createObjectURL
  const originalRevokeObjectURL = urlWithObjectURL.revokeObjectURL
  const originalFetch = globalThis.fetch
  const originalActEnvironment = actEnvironmentGlobal.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironmentGlobal.IS_REACT_ACT_ENVIRONMENT = true
    urlWithObjectURL.createObjectURL = vi.fn(() => 'blob:artifact-preview')
    urlWithObjectURL.revokeObjectURL = vi.fn()
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url
      if (url.endsWith('/doc.md')) {
        return new Response('[预览](artifact:preview.html)', {
          headers: { 'Content-Type': 'text/markdown' },
        })
      }
      if (url.endsWith('/preview.html')) {
        return new Response('<html><body>ok</body></html>', {
          headers: { 'Content-Type': 'text/html' },
        })
      }
      return new Response('not-found', { status: 404 })
    })
  })

  afterEach(() => {
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
    globalThis.fetch = originalFetch
    if (originalActEnvironment === undefined) {
      delete actEnvironmentGlobal.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironmentGlobal.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
    vi.restoreAllMocks()
  })

  it('Markdown 文档中的 html artifact 应继续内联渲染', async () => {
    const markdownArtifact: ArtifactRef = {
      key: 'doc.md',
      filename: 'doc.md',
      size: 10,
      mime_type: 'text/markdown',
    }
    const htmlArtifact: ArtifactRef = {
      key: 'preview.html',
      filename: 'preview.html',
      size: 20,
      mime_type: 'text/html',
    }

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <DocumentPanel
            artifact={markdownArtifact}
            artifacts={[htmlArtifact]}
            accessToken="token"
            onClose={() => {}}
          />
        </LocaleProvider>,
      )
    })

    await act(async () => {
      await flushMicrotasks()
    })

    await act(async () => {
      await flushMicrotasks()
    })

    expect(globalThis.fetch).toHaveBeenCalledTimes(2)
    expect(container.querySelector('iframe')).not.toBeNull()

    act(() => {
      root.unmount()
    })
    container.remove()
  })
})
