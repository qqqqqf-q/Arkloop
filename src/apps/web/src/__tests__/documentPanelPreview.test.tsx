import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act } from 'react'
import { createRoot } from 'react-dom/client'

import { ResourcePreviewPanel } from '../components/resource-preview/ResourcePreviewPanel'
import { LocaleProvider } from '../contexts/LocaleContext'
import type { ArtifactRef } from '../storage'

type URLWithObjectURL = typeof URL & {
  createObjectURL?: (object: Blob) => string
  revokeObjectURL?: (url: string) => void
}

type GlobalWithActEnvironment = typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}

const originalRAF = globalThis.requestAnimationFrame
const originalCAF = globalThis.cancelAnimationFrame

function flushMicrotasks(): Promise<void> {
  return Promise.resolve()
    .then(() => Promise.resolve())
    .then(() => Promise.resolve())
    .then(() => Promise.resolve())
}

async function flushPreviewWork(): Promise<void> {
  for (let i = 0; i < 8; i++) {
    await flushMicrotasks()
    await new Promise((resolve) => setTimeout(resolve, 0))
  }
}

describe('ResourcePreviewPanel artifact preview', () => {
  const urlWithObjectURL = URL as URLWithObjectURL
  const actEnvironmentGlobal = globalThis as GlobalWithActEnvironment
  const originalCreateObjectURL = urlWithObjectURL.createObjectURL
  const originalRevokeObjectURL = urlWithObjectURL.revokeObjectURL
  const originalFetch = globalThis.fetch
  const originalActEnvironment = actEnvironmentGlobal.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironmentGlobal.IS_REACT_ACT_ENVIRONMENT = true
    globalThis.requestAnimationFrame = (cb: FrameRequestCallback) => {
      cb(performance.now())
      return 0
    }
    globalThis.cancelAnimationFrame = () => {}
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
    globalThis.requestAnimationFrame = originalRAF
    globalThis.cancelAnimationFrame = originalCAF
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
          <ResourcePreviewPanel
            resource={{
              kind: 'artifact',
              key: markdownArtifact.key,
              filename: markdownArtifact.filename,
              mimeType: markdownArtifact.mime_type,
              size: markdownArtifact.size,
            }}
            artifacts={[htmlArtifact]}
            accessToken="token"
            onClose={() => {}}
          />
        </LocaleProvider>,
      )
    })

    await act(async () => {
      await flushPreviewWork()
    })

    expect(globalThis.fetch).toHaveBeenCalledTimes(2)
    expect(container.querySelector('iframe')).not.toBeNull()

    act(() => {
      root.unmount()
    })
    container.remove()
  })
})
