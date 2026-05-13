import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { LocaleProvider } from '../../contexts/LocaleContext'
import { LocalFilesPanel } from './LocalFilesPanel'

function flushMicrotasks(): Promise<void> {
  return Promise.resolve()
    .then(() => Promise.resolve())
    .then(() => Promise.resolve())
}

function installDesktopFs() {
  Object.defineProperty(globalThis, 'arkloop', {
    configurable: true,
    writable: true,
    value: {
      isDesktop: true,
      fs: {
        listDir: vi.fn().mockResolvedValue({
          entries: [
            { name: 'README.md', path: 'README.md', type: 'file' },
            { name: 'package.json', path: 'package.json', type: 'file' },
          ],
        }),
        readFile: vi.fn().mockResolvedValue({
          data: btoa('hello'),
          mime_type: 'text/plain',
        }),
      },
    },
  })
}

describe('LocalFilesPanel', () => {
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    installDesktopFs()
  })

  afterEach(() => {
    delete (globalThis as Record<string, unknown>).arkloop
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('Browse Files 可以收起文件树，预览区保持存在', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<LocalFilesPanel rootPath="/repo" accessToken="" />)
    })

    const browseButton = container.querySelector<HTMLButtonElement>('[title="Browse Files"]')
    expect(container.querySelector('.local-files-panel__preview')).not.toBeNull()
    expect(container.querySelector('.local-files-panel__browser--closed')).toBeNull()

    await act(async () => {
      browseButton?.click()
    })

    expect(container.querySelector('.local-files-panel__browser--closed')).not.toBeNull()

    await act(async () => {
      root.unmount()
    })
    container.remove()
  })

  it('Search 会显示输入框', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<LocalFilesPanel rootPath="/repo" accessToken="" />)
      await flushMicrotasks()
    })

    await act(async () => {
      container.querySelector<HTMLButtonElement>('[title="Search"]')?.click()
    })

    const input = container.querySelector<HTMLInputElement>('.local-files-panel__search-input')
    expect(input).not.toBeNull()

    expect(input?.getAttribute('placeholder')).toBe('Search')

    await act(async () => {
      root.unmount()
    })
    container.remove()
  })

  it('从 Search 点回 Browse Files 是切换模式，不关闭文件区域', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<LocalFilesPanel rootPath="/repo" accessToken="" />)
      await flushMicrotasks()
    })

    await act(async () => {
      container.querySelector<HTMLButtonElement>('[title="Search"]')?.click()
    })

    expect(container.querySelector('.local-files-panel__search-input')).not.toBeNull()

    await act(async () => {
      container.querySelector<HTMLButtonElement>('[title="Browse Files"]')?.click()
    })

    expect(container.querySelector('.local-files-panel__browser--closed')).toBeNull()
    expect(container.querySelector('.local-files-panel__search-input')).toBeNull()
    expect(container.querySelector('[aria-label="Local files"]')).not.toBeNull()

    await act(async () => {
      root.unmount()
    })
    container.remove()
  })

  it('文件预览可以独立关闭', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<LocaleProvider><LocalFilesPanel rootPath="/repo" accessToken="" /></LocaleProvider>)
      await flushMicrotasks()
    })

    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-path="README.md"]')?.click()
      await flushMicrotasks()
      await flushMicrotasks()
    })

    expect(container.textContent).toContain('README.md')

    const closeButton = container.querySelector<HTMLButtonElement>('[aria-label="关闭"]')
    expect(closeButton).not.toBeNull()

    await act(async () => {
      closeButton?.click()
      await flushMicrotasks()
    })

    expect(container.textContent).toContain('No file selected')

    await act(async () => {
      root.unmount()
    })
    container.remove()
  })

  it('单击文件更新受控 preview，双击文件触发 pin', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const onPreviewResourceChange = vi.fn()
    const onPinResource = vi.fn()

    await act(async () => {
      root.render(
        <LocalFilesPanel
          rootPath="/repo"
          accessToken=""
          previewResource={null}
          onPreviewResourceChange={onPreviewResourceChange}
          onPinResource={onPinResource}
        />,
      )
      await flushMicrotasks()
    })

    const readmeButton = container.querySelector<HTMLButtonElement>('[data-path="README.md"]')
    expect(readmeButton).not.toBeNull()

    await act(async () => {
      readmeButton?.click()
    })

    expect(onPreviewResourceChange).toHaveBeenCalledWith(expect.objectContaining({
      kind: 'local-file',
      rootPath: '/repo',
      path: 'README.md',
      name: 'README.md',
    }))

    await act(async () => {
      readmeButton?.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }))
    })

    expect(onPinResource).toHaveBeenCalledWith(expect.objectContaining({
      kind: 'local-file',
      rootPath: '/repo',
      path: 'README.md',
      name: 'README.md',
    }))

    await act(async () => {
      root.unmount()
    })
    container.remove()
  })
})
