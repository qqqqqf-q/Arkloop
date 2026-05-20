import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ResourcePreviewPanel } from '../components/resource-preview/ResourcePreviewPanel'
import { LocaleProvider } from '../contexts/LocaleContext'
import type { ArtifactRef } from '../storage'

type GlobalWithActEnvironment = typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}

const originalRAF = globalThis.requestAnimationFrame
const originalCAF = globalThis.cancelAnimationFrame

function flushMicrotasks(): Promise<void> {
  return Promise.resolve()
    .then(() => Promise.resolve())
    .then(() => Promise.resolve())
}

async function waitForAssertion(assertion: () => void): Promise<void> {
  let lastError: unknown
  for (let i = 0; i < 20; i++) {
    try {
      assertion()
      return
    } catch (err) {
      lastError = err
    }
    await act(async () => {
      await flushMicrotasks()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
  }
  throw lastError
}

describe('ResourcePreviewPanel artifact preview', () => {
  const actEnvironmentGlobal = globalThis as GlobalWithActEnvironment
  const originalFetch = globalThis.fetch
  const originalActEnvironment = actEnvironmentGlobal.IS_REACT_ACT_ENVIRONMENT
  let container: HTMLDivElement
  let root: Root

  beforeEach(() => {
    actEnvironmentGlobal.IS_REACT_ACT_ENVIRONMENT = true
    globalThis.requestAnimationFrame = (callback: FrameRequestCallback) => {
      callback(performance.now())
      return 0
    }
    globalThis.cancelAnimationFrame = () => {}
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url
      if (url.endsWith('/doc.md')) {
        return new Response('[Preview](artifact:preview.html)', {
          headers: { 'Content-Type': 'text/markdown' },
        })
      }
      if (url.endsWith('/preview.html')) {
        return new Response('<html><body>preview</body></html>', {
          headers: { 'Content-Type': 'text/html' },
        })
      }
      return new Response('not-found', { status: 404 })
    })
  })

  afterEach(() => {
    act(() => {
      root.unmount()
    })
    container.remove()
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

  it('loads markdown artifacts before rendering linked html artifacts inline', async () => {
    const htmlArtifact: ArtifactRef = {
      key: 'preview.html',
      filename: 'preview.html',
      size: 20,
      mime_type: 'text/html',
    }

    await act(async () => {
      root.render(
        <LocaleProvider>
          <ResourcePreviewPanel
            resource={{
              kind: 'artifact',
              key: 'doc.md',
              filename: 'doc.md',
              mimeType: 'text/markdown',
              size: 10,
            }}
            artifacts={[htmlArtifact]}
            accessToken="token"
            onClose={() => {}}
          />
        </LocaleProvider>,
      )
    })

    await waitForAssertion(() => {
      expect(container.querySelector('iframe[title="preview.html"]')).not.toBeNull()
    })

    expect(globalThis.fetch).toHaveBeenCalledTimes(2)
    const [markdownUrl, markdownInit] = vi.mocked(globalThis.fetch).mock.calls[0]!
    expect(String(markdownUrl)).toContain('/v1/artifacts/doc.md')
    expect((markdownInit as RequestInit | undefined)?.headers).toEqual({ Authorization: 'Bearer token' })
    const [htmlUrl, htmlInit] = vi.mocked(globalThis.fetch).mock.calls[1]!
    expect(String(htmlUrl)).toContain('/v1/artifacts/preview.html')
    expect((htmlInit as RequestInit | undefined)?.headers).toEqual({ Authorization: 'Bearer token' })
  })
})
