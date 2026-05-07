import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { PluginHostPage } from '../plugins/PluginHostPage'
import { PluginRuntimeProvider } from '../plugins/runtime'

describe('PluginHostPage', () => {
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

  it('renders the registered plugin page for a valid route plugin', async () => {
    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/plugins/sample-plugin']}>
          <PluginRuntimeProvider>
            <Routes>
              <Route path="/plugins/:pluginId" element={<PluginHostPage />} />
            </Routes>
          </PluginRuntimeProvider>
        </MemoryRouter>,
      )
      await Promise.resolve()
    })

    expect(container.querySelector('[data-testid="sample-plugin-page"]')).not.toBeNull()
  })
})
