import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act } from 'react'
import { createRoot } from 'react-dom/client'

import { ArtifactHtmlPreview } from '../components/ArtifactHtmlPreview'
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
}

describe('ArtifactHtmlPreview', () => {
  const urlWithObjectURL = URL as URLWithObjectURL
  const actEnvironmentGlobal = globalThis as GlobalWithActEnvironment
  const originalCreateObjectURL = urlWithObjectURL.createObjectURL
  const originalRevokeObjectURL = urlWithObjectURL.revokeObjectURL
  const originalFetch = globalThis.fetch
  const originalActEnvironment = actEnvironmentGlobal.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironmentGlobal.IS_REACT_ACT_ENVIRONMENT = true
    urlWithObjectURL.createObjectURL = vi.fn(() => 'blob:mock')
    urlWithObjectURL.revokeObjectURL = vi.fn()
    globalThis.fetch = vi.fn(async () => new Response('<html></html>', {
      headers: { 'Content-Type': 'text/html' },
    }))
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

  it('只接受当前 iframe 自身发来的 resize 消息', async () => {
    const artifact: ArtifactRef = {
      key: 'artifact-key',
      filename: 'index.html',
      size: 10,
      mime_type: 'text/html',
    }

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<ArtifactHtmlPreview artifact={artifact} accessToken="token" />)
    })
    await act(async () => {
      await flushMicrotasks()
    })

    const iframe = container.querySelector('iframe') as HTMLIFrameElement | null
    expect(iframe).not.toBeNull()
    if (!iframe) return

    const iframeWindow = {} as WindowProxy
    Object.defineProperty(iframe, 'contentWindow', { value: iframeWindow, configurable: true })

    const badSource = new MessageEvent('message', {
      data: { type: 'arkloop:artifact:action', action: 'resize', height: 123 },
    })
    Object.defineProperty(badSource, 'source', { value: window, configurable: true })
    window.dispatchEvent(badSource)
    expect(iframe.style.height).not.toBe('123px')

    const goodSource = new MessageEvent('message', {
      data: { type: 'arkloop:artifact:action', action: 'resize', height: 456 },
    })
    Object.defineProperty(goodSource, 'source', { value: iframeWindow, configurable: true })
    window.dispatchEvent(goodSource)
    expect(iframe.style.height).toBe('456px')

    act(() => {
      root.unmount()
    })
    container.remove()
  })
})
