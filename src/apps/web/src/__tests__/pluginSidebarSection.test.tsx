import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { PluginRuntimeProvider } from '../plugins/runtime'
import { PluginSidebarSection } from '../plugins/PluginSidebarSection'

const desktopMock = vi.hoisted(() => ({
  isDesktop: vi.fn(() => true),
}))

vi.mock('@arkloop/shared/desktop', () => ({
  isDesktop: desktopMock.isDesktop,
}))

function Probe() {
  const location = useLocation()
  return <div data-testid="path">{location.pathname}</div>
}

describe('PluginSidebarSection', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot>
  const actEnvironment = globalThis as typeof globalThis & {
    IS_REACT_ACT_ENVIRONMENT?: boolean
  }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(() => {
    act(() => root.unmount())
    container.remove()
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('renders built-in plugins and opens the clicked plugin', async () => {
    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/']}>
          <PluginRuntimeProvider>
            <PluginSidebarSection />
            <Probe />
          </PluginRuntimeProvider>
        </MemoryRouter>,
      )
      await Promise.resolve()
    })

    expect(container.textContent).toContain('Sample Plugin')

    await act(async () => {
      container
        .querySelector('[data-testid="plugin-entry-sample-plugin"]')
        ?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
    })

    expect(container.querySelector('[data-testid="path"]')?.textContent).toBe('/plugins/sample-plugin')
  })
})
