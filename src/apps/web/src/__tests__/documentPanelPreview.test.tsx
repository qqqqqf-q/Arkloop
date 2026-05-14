import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act } from 'react'
import { createRoot } from 'react-dom/client'

import { ResourcePreviewPanel } from '../components/resource-preview/ResourcePreviewPanel'
import { LocaleProvider } from '../contexts/LocaleContext'
import type { ArtifactRef } from '../storage'

vi.mock('../components/ArtifactIframe', async () => {
  const { createElement } = await import('react')
  return {
    ArtifactIframe: ({ frameTitle }: { frameTitle?: string }) => createElement('iframe', {
      'data-preview-renderer': 'artifact-html-preview',
      title: frameTitle ?? 'artifact',
    }),
  }
})

type GlobalWithActEnvironment = typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}

function flushMicrotasks(): Promise<void> {
  return Promise.resolve()
    .then(() => Promise.resolve())
    .then(() => Promise.resolve())
    .then(() => Promise.resolve())
}

function fetchInputUrl(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url
}

async function waitForPreviewWork(predicate: () => boolean): Promise<void> {
  for (let i = 0; i < 40; i++) {
    if (predicate()) return
    await act(async () => {
      await flushMicrotasks()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
  }
  if (predicate()) return
  throw new Error('preview did not settle')
}

describe('ResourcePreviewPanel artifact preview', () => {
  const actEnvironmentGlobal = globalThis as GlobalWithActEnvironment
  const originalFetch = globalThis.fetch
  const originalActEnvironment = actEnvironmentGlobal.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironmentGlobal.IS_REACT_ACT_ENVIRONMENT = true
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
      const url = fetchInputUrl(input)
      if (url.endsWith('/doc.md')) {
        return new Response('[预览](artifact:preview.html)', {
          headers: { 'Content-Type': 'text/markdown' },
        })
      }
      return new Response('not-found', { status: 404 })
    })
  })

  afterEach(() => {
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

    await waitForPreviewWork(() => (
      container.querySelector('iframe[data-preview-renderer="artifact-html-preview"]') !== null
    ))

    const fetchUrls = vi.mocked(globalThis.fetch).mock.calls.map(([input]) => fetchInputUrl(input))
    expect(fetchUrls).toHaveLength(1)
    expect(fetchUrls[0]).toMatch(/\/doc\.md$/)
    expect(container.querySelector('iframe[data-preview-renderer="artifact-html-preview"]')?.getAttribute('title')).toBe('preview.html')

    act(() => {
      root.unmount()
    })
    container.remove()
  })
})
