# Plugin Workspace Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a desktop-first plugin workspace framework to the `web` app with sidebar entries, route-backed page plugins, browser takeover modes, and browser-session reuse for browser or hybrid plugins.

**Architecture:** Introduce a small plugin subsystem under `src/apps/web/src/plugins/` that owns plugin metadata, runtime state, host routing, and browser-session mapping. Keep Electron unaware of plugins by layering plugin session state on top of the existing `BrowserTabsProvider`, then teach `AppLayout` to render a route-backed plugin page, preserve the default workspace alongside the right-side browser, or hand the workspace over to the global browser panel depending on plugin presentation.

**Tech Stack:** React 19, React Router 7, TypeScript 5.9, Vitest, existing Arkloop desktop bridge and browser tab context.

---

## File Map

- Create: `src/apps/web/src/plugins/types.ts`
  - Shared plugin types, shell modes, presentation modes, and runtime state contracts.
- Create: `src/apps/web/src/plugins/registry.ts`
  - Built-in plugin definitions and selector helpers.
- Create: `src/apps/web/src/plugins/runtime.tsx`
  - Plugin runtime provider, navigation API, persistence, active plugin lookup, and context-path based activation clearing.
- Create: `src/apps/web/src/plugins/PluginHostPage.tsx`
  - Route entry for page-style plugins.
- Create: `src/apps/web/src/plugins/PluginSidebarSection.tsx`
  - Sidebar section for plugin entries.
- Create: `src/apps/web/src/plugins/browser-session.tsx`
  - Mapping layer between `pluginId` and browser tab ids.
- Create: `src/apps/web/src/plugins/PluginWorkspaceShell.tsx`
  - Workspace renderer for route-backed plugin takeover pages.
- Create: `src/apps/web/src/plugins/builtin/SamplePluginPage.tsx`
  - Minimal built-in route plugin used to verify the framework end-to-end.
- Create: `src/apps/web/src/__tests__/pluginRuntime.test.tsx`
  - Runtime navigation and persistence coverage.
- Create: `src/apps/web/src/__tests__/pluginHostPage.test.tsx`
  - Route-to-renderer selection coverage.
- Create: `src/apps/web/src/__tests__/pluginSidebarSection.test.tsx`
  - Sidebar rendering and click handling coverage.
- Create: `src/apps/web/src/__tests__/pluginBrowserSession.test.tsx`
  - Plugin-to-browser-tab mapping coverage.
- Create: `src/apps/web/src/__tests__/appLayoutPluginWorkspace.test.tsx`
  - Workspace takeover coverage inside `AppLayout`.
- Modify: `src/apps/web/src/App.tsx`
  - Mount the plugin runtime provider and add `/plugins/:pluginId`.
- Modify: `src/apps/web/src/layouts/AppLayout.tsx`
  - Coordinate workspace-host visibility with the global right-side browser panel.
- Modify: `src/apps/web/src/components/Sidebar.tsx`
  - Insert the plugin sidebar section.
- Modify: `src/apps/web/src/storage.ts`
  - Add plugin runtime persistence helpers.

## Current Presentation Semantics

- `route`: render a plugin-owned page in the main workspace area with no browser chrome.
- `embedded-browser`: keep the current workspace route, open the global right-side browser, and fullscreen it so the chat/workspace host is hidden.
- `hybrid`: keep the current workspace route, preserve the chat/workspace host, and open the global right-side browser alongside it.

## Task 1: Plugin Types, Registry, and Runtime Persistence

**Files:**
- Create: `src/apps/web/src/plugins/types.ts`
- Create: `src/apps/web/src/plugins/registry.ts`
- Create: `src/apps/web/src/plugins/runtime.tsx`
- Create: `src/apps/web/src/plugins/builtin/SamplePluginPage.tsx`
- Create: `src/apps/web/src/__tests__/pluginRuntime.test.tsx`
- Modify: `src/apps/web/src/storage.ts`

- [ ] **Step 1: Write the failing runtime test**

```tsx
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { PluginRuntimeProvider, usePluginRuntime } from '../plugins/runtime'

vi.mock('../storage', async () => {
  const actual = await vi.importActual<typeof import('../storage')>('../storage')
  return {
    ...actual,
    readPluginRuntimeState: vi.fn(() => ({
      lastPluginId: 'sample-page-plugin',
      presentationByPluginId: { 'sample-page-plugin': 'route' },
    })),
    writePluginRuntimeState: vi.fn(),
  }
})

function Probe() {
  const location = useLocation()
  const { activePluginId, openPlugin } = usePluginRuntime()
  return (
    <div>
      <div data-testid="active">{activePluginId ?? 'none'}</div>
      <div data-testid="path">{location.pathname}</div>
      <button type="button" onClick={() => void openPlugin('sample-page-plugin')}>
        open-page
      </button>
      <button type="button" onClick={() => void openPlugin('sample-browser-plugin')}>
        open-browser
      </button>
    </div>
  )
}

describe('PluginRuntimeProvider', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot>

  beforeEach(() => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(() => {
    act(() => root.unmount())
    container.remove()
    vi.clearAllMocks()
  })

  it('opens a plugin route and tracks the active plugin id', async () => {
    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/']}>
          <PluginRuntimeProvider>
            <Probe />
          </PluginRuntimeProvider>
        </MemoryRouter>,
      )
      await Promise.resolve()
    })

    await act(async () => {
      container.querySelector('button')?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
    })

    expect(container.querySelector('[data-testid="active"]')?.textContent).toBe('sample-page-plugin')
    expect(container.querySelector('[data-testid="path"]')?.textContent).toBe('/plugins/sample-page-plugin')
  })
})
```

- [ ] **Step 2: Run the runtime test to verify it fails**

Run: `cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm test -- --run src/__tests__/pluginRuntime.test.tsx`

Expected: FAIL with module-not-found errors for `../plugins/runtime` or missing storage helpers.

- [ ] **Step 3: Add plugin storage helpers in `storage.ts`**

```ts
const PLUGIN_RUNTIME_STATE_KEY = 'arkloop:web:plugin-runtime'

export type StoredPluginPresentation = 'route' | 'embedded-browser' | 'hybrid'

export type PluginRuntimeStorageState = {
  lastPluginId: string | null
  presentationByPluginId: Record<string, StoredPluginPresentation>
}

export function readPluginRuntimeState(): PluginRuntimeStorageState {
  if (!canUseLocalStorage()) {
    return { lastPluginId: null, presentationByPluginId: {} }
  }
  try {
    const raw = localStorage.getItem(PLUGIN_RUNTIME_STATE_KEY)
    if (!raw) return { lastPluginId: null, presentationByPluginId: {} }
    const parsed = JSON.parse(raw) as Partial<PluginRuntimeStorageState>
    return {
      lastPluginId: typeof parsed.lastPluginId === 'string' ? parsed.lastPluginId : null,
      presentationByPluginId: typeof parsed.presentationByPluginId === 'object' && parsed.presentationByPluginId
        ? Object.fromEntries(
            Object.entries(parsed.presentationByPluginId).filter(([, value]) =>
              value === 'route' || value === 'embedded-browser' || value === 'hybrid',
            ),
          )
        : {},
    }
  } catch {
    return { lastPluginId: null, presentationByPluginId: {} }
  }
}

export function writePluginRuntimeState(state: PluginRuntimeStorageState): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.setItem(PLUGIN_RUNTIME_STATE_KEY, JSON.stringify(state))
  } catch {
    // ignore
  }
}
```

- [ ] **Step 4: Create plugin types and built-in sample plugins**

```ts
// src/apps/web/src/plugins/types.ts
export type PluginShellMode = 'plugin-main' | 'plugin-workspace'
export type PluginPresentation = 'route' | 'embedded-browser' | 'hybrid'

export type PluginDefinition = {
  id: string
  title: string
  desktopOnly?: boolean
  nav: {
    section: 'primary' | 'tools' | 'workspace'
    order: number
    icon?: string
  }
  shell: {
    mode: PluginShellMode
  }
  presentation: {
    default: PluginPresentation
    supported: PluginPresentation[]
  }
  surfaces: {
    mount?: React.ComponentType
    resolveBrowserUrl?: (ctx: PluginResolveBrowserUrlContext) => Promise<string> | string
    browserPlacement?: 'sidecar'
  }
}
```

```tsx
// src/apps/web/src/plugins/builtin/SamplePluginPage.tsx
export function SamplePagePluginPage() {
  return <div data-testid="sample-page-plugin-page">sample page plugin page</div>
}

export function SampleHybridPluginPage() {
  return <div data-testid="sample-hybrid-plugin-page">sample hybrid plugin page</div>
}
```

```ts
// src/apps/web/src/plugins/registry.ts
import { SampleHybridPluginPage, SamplePagePluginPage } from './builtin/SamplePluginPage'
import type { PluginDefinition } from './types'

export const builtinPlugins: PluginDefinition[] = [
  {
    id: 'sample-page-plugin',
    title: 'Sample Page Plugin',
    desktopOnly: true,
    nav: { section: 'workspace', order: 100 },
    shell: { mode: 'plugin-main' },
    presentation: {
      default: 'route',
      supported: ['route'],
    },
    surfaces: {
      mount: SamplePagePluginPage,
    },
  },
  {
    id: 'sample-browser-plugin',
    title: 'Sample Browser Plugin',
    desktopOnly: true,
    nav: { section: 'workspace', order: 110 },
    shell: { mode: 'plugin-workspace' },
    presentation: {
      default: 'embedded-browser',
      supported: ['embedded-browser'],
    },
    surfaces: {
      resolveBrowserUrl: () => 'https://example.com/',
      browserPlacement: 'sidecar',
    },
  },
  {
    id: 'sample-hybrid-plugin',
    title: 'Sample Hybrid Plugin',
    desktopOnly: true,
    nav: { section: 'workspace', order: 120 },
    shell: { mode: 'plugin-workspace' },
    presentation: {
      default: 'hybrid',
      supported: ['hybrid'],
    },
    surfaces: {
      mount: SampleHybridPluginPage,
      resolveBrowserUrl: () => 'https://example.com/',
      browserPlacement: 'sidecar',
    },
  },
]

export function listBuiltinPlugins(): PluginDefinition[] {
  return [...builtinPlugins].sort((left, right) => left.nav.order - right.nav.order)
}

export function getBuiltinPluginById(pluginId: string): PluginDefinition | null {
  return builtinPlugins.find((plugin) => plugin.id === pluginId) ?? null
}
```

- [ ] **Step 5: Implement the plugin runtime provider**

```tsx
// src/apps/web/src/plugins/runtime.tsx
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { useLocation, useNavigate } from 'react-router-dom'

import { getBuiltinPluginById } from './registry'
import type { PluginDefinition, PluginPresentation } from './types'
import { readPluginRuntimeState, writePluginRuntimeState } from '../storage'

type PluginRuntimeContextValue = {
  activePluginId: string | null
  activePlugin: PluginDefinition | null
  getPresentationForPlugin: (pluginId: string) => PluginPresentation | null
  openPlugin: (pluginId: string, presentation?: PluginPresentation) => Promise<void>
  setPresentationForPlugin: (pluginId: string, presentation: PluginPresentation) => void
}

const PluginRuntimeContext = createContext<PluginRuntimeContextValue | null>(null)

export function PluginRuntimeProvider({ children }: { children: ReactNode }) {
  const navigate = useNavigate()
  const location = useLocation()
  const initialState = readPluginRuntimeState()
  const [activePluginId, setActivePluginId] = useState<string | null>(initialState.lastPluginId)
  const [activePluginContextPath, setActivePluginContextPath] = useState<string | null>(null)
  const [presentationByPluginId, setPresentationByPluginId] = useState(initialState.presentationByPluginId)
  const lastWorkspacePathRef = useRef('/')

  useEffect(() => {
    if (location.pathname.startsWith('/plugins/')) return
    lastWorkspacePathRef.current = `${location.pathname}${location.search}${location.hash}` || '/'
  }, [location.hash, location.pathname, location.search])

  const setPresentationForPlugin = useCallback((pluginId: string, presentation: PluginPresentation) => {
    setPresentationByPluginId((current) => {
      const next = { ...current, [pluginId]: presentation }
      writePluginRuntimeState({ lastPluginId: activePluginId, presentationByPluginId: next })
      return next
    })
  }, [activePluginId])

  const openPlugin = useCallback(async (pluginId: string, presentation?: PluginPresentation) => {
    const plugin = getBuiltinPluginById(pluginId)
    if (!plugin) return
    const nextPresentation = presentation ?? presentationByPluginId[pluginId] ?? plugin.presentation.default
    setActivePluginId(pluginId)
    setActivePluginContextPath(
      nextPresentation === 'route'
        ? `/plugins/${encodeURIComponent(pluginId)}`
        : (lastWorkspacePathRef.current || '/'),
    )
    const nextPresentationMap = { ...presentationByPluginId, [pluginId]: nextPresentation }
    writePluginRuntimeState({ lastPluginId: pluginId, presentationByPluginId: nextPresentationMap })
    setPresentationByPluginId(nextPresentationMap)
    if (nextPresentation === 'route') {
      navigate(`/plugins/${encodeURIComponent(pluginId)}`)
      return
    }
    navigate(lastWorkspacePathRef.current || '/')
  }, [navigate, presentationByPluginId])

  const value = useMemo<PluginRuntimeContextValue>(() => ({
    activePluginId,
    activePlugin: activePluginId ? getBuiltinPluginById(activePluginId) : null,
    getPresentationForPlugin: (pluginId) => presentationByPluginId[pluginId] ?? getBuiltinPluginById(pluginId)?.presentation.default ?? null,
    openPlugin,
    setPresentationForPlugin,
  }), [activePluginId, openPlugin, presentationByPluginId, setPresentationForPlugin])

  return <PluginRuntimeContext.Provider value={value}>{children}</PluginRuntimeContext.Provider>
}

export function usePluginRuntime(): PluginRuntimeContextValue {
  const value = useContext(PluginRuntimeContext)
  if (!value) throw new Error('usePluginRuntime must be used within PluginRuntimeProvider')
  return value
}
```

- [ ] **Step 6: Run the runtime test to verify it passes**

Run: `cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm test -- --run src/__tests__/pluginRuntime.test.tsx`

Expected: PASS with `1 passed`.

- [ ] **Step 7: Commit**

```bash
cd /Users/huhui/Projects/Arkloop
git add src/apps/web/src/storage.ts src/apps/web/src/plugins/types.ts src/apps/web/src/plugins/registry.ts src/apps/web/src/plugins/runtime.tsx src/apps/web/src/plugins/builtin/SamplePluginPage.tsx src/apps/web/src/__tests__/pluginRuntime.test.tsx
git commit -m "feat(web): add plugin runtime foundation"
```

## Task 2: Plugin Route Host and App Wiring

**Files:**
- Create: `src/apps/web/src/plugins/PluginHostPage.tsx`
- Create: `src/apps/web/src/__tests__/pluginHostPage.test.tsx`
- Modify: `src/apps/web/src/App.tsx`

- [ ] **Step 1: Write the failing host-page test**

```tsx
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { PluginRuntimeProvider } from '../plugins/runtime'
import { PluginHostPage } from '../plugins/PluginHostPage'

describe('PluginHostPage', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot>

  beforeEach(() => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(() => {
    act(() => root.unmount())
    container.remove()
  })

  it('renders the registered plugin page for a valid route plugin', async () => {
    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/plugins/sample-page-plugin']}>
          <PluginRuntimeProvider>
            <Routes>
              <Route path="/plugins/:pluginId" element={<PluginHostPage />} />
            </Routes>
          </PluginRuntimeProvider>
        </MemoryRouter>,
      )
      await Promise.resolve()
    })

    expect(container.querySelector('[data-testid="sample-page-plugin-page"]')).not.toBeNull()
  })
})
```

- [ ] **Step 2: Run the host-page test to verify it fails**

Run: `cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm test -- --run src/__tests__/pluginHostPage.test.tsx`

Expected: FAIL with module-not-found for `PluginHostPage`.

- [ ] **Step 3: Implement `PluginHostPage`**

```tsx
import { Navigate, useParams } from 'react-router-dom'

import { getBuiltinPluginById } from './registry'
import { usePluginRuntime } from './runtime'

export function PluginHostPage() {
  const { pluginId = '' } = useParams()
  const plugin = getBuiltinPluginById(pluginId)
  const { getPresentationForPlugin } = usePluginRuntime()

  if (!plugin) {
    return <Navigate to="/" replace />
  }

  const presentation = getPresentationForPlugin(plugin.id) ?? plugin.presentation.default

  if (presentation === 'route' && plugin.surfaces.mount) {
    const Component = plugin.surfaces.mount
    return <Component />
  }

  return <Navigate to="/" replace />
}
```

- [ ] **Step 4: Wire the provider and route in `App.tsx`**

```tsx
import { PluginRuntimeProvider } from './plugins/runtime'
import { PluginHostPage } from './plugins/PluginHostPage'
```

```tsx
<AuthProvider accessToken={accessToken} onLoggedOut={handleLoggedOut}>
  <ThreadListProvider>
    <AppUIProvider>
      <BrowserTabsProvider>
        <PluginRuntimeProvider>
          <CreditsProvider>
            <AppLayout />
          </CreditsProvider>
        </PluginRuntimeProvider>
      </BrowserTabsProvider>
    </AppUIProvider>
  </ThreadListProvider>
</AuthProvider>
```

```tsx
<Route path="plugins/:pluginId" element={<PluginHostPage />} />
```

- [ ] **Step 5: Run the host-page test to verify it passes**

Run: `cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm test -- --run src/__tests__/pluginHostPage.test.tsx`

Expected: PASS with `1 passed`.

- [ ] **Step 6: Commit**

```bash
cd /Users/huhui/Projects/Arkloop
git add src/apps/web/src/plugins/PluginHostPage.tsx src/apps/web/src/App.tsx src/apps/web/src/__tests__/pluginHostPage.test.tsx
git commit -m "feat(web): add plugin route host"
```

## Task 3: Sidebar Plugin Section and Navigation Entry

**Files:**
- Create: `src/apps/web/src/plugins/PluginSidebarSection.tsx`
- Create: `src/apps/web/src/__tests__/pluginSidebarSection.test.tsx`
- Modify: `src/apps/web/src/components/Sidebar.tsx`

- [ ] **Step 1: Write the failing sidebar-section test**

```tsx
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { PluginRuntimeProvider } from '../plugins/runtime'
import { PluginSidebarSection } from '../plugins/PluginSidebarSection'

describe('PluginSidebarSection', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot>

  beforeEach(() => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(() => {
    act(() => root.unmount())
    container.remove()
  })

  it('renders built-in plugins and opens the clicked plugin', async () => {
    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/']}>
          <PluginRuntimeProvider>
            <PluginSidebarSection />
          </PluginRuntimeProvider>
        </MemoryRouter>,
      )
      await Promise.resolve()
    })

    expect(container.textContent).toContain('Sample Page Plugin')
    expect(container.textContent).toContain('Sample Browser Plugin')
    expect(container.textContent).toContain('Sample Hybrid Plugin')
  })
})
```

- [ ] **Step 2: Run the sidebar-section test to verify it fails**

Run: `cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm test -- --run src/__tests__/pluginSidebarSection.test.tsx`

Expected: FAIL with module-not-found for `PluginSidebarSection`.

- [ ] **Step 3: Implement `PluginSidebarSection`**

```tsx
import { listBuiltinPlugins } from './registry'
import { usePluginRuntime } from './runtime'

export function PluginSidebarSection() {
  const { activePluginId, openPlugin } = usePluginRuntime()
  const plugins = listBuiltinPlugins()

  return (
    <section aria-label="Plugins" className="mt-3 px-2">
      <div className="px-2 pb-1 text-[11px] uppercase tracking-[0.08em] text-[var(--c-text-tertiary)]">
        Plugins
      </div>
      <div className="flex flex-col gap-1">
        {plugins.map((plugin) => {
          const active = activePluginId === plugin.id
          return (
            <button
              key={plugin.id}
              type="button"
              data-testid={`plugin-entry-${plugin.id}`}
              onClick={() => void openPlugin(plugin.id)}
              className="flex h-9 items-center rounded-[8px] px-3 text-left text-sm"
              style={{
                background: active ? 'var(--c-bg-deep)' : 'transparent',
                color: 'var(--c-text-primary)',
              }}
            >
              {plugin.title}
            </button>
          )
        })}
      </div>
    </section>
  )
}
```

- [ ] **Step 4: Insert the section into `Sidebar.tsx`**

```tsx
import { PluginSidebarSection } from '../plugins/PluginSidebarSection'
```

```tsx
<div className="min-h-0 flex-1 overflow-y-auto">
  <PluginSidebarSection />
  {/* existing thread groups continue below */}
</div>
```

- [ ] **Step 5: Add a layout-level regression test**

Extend `src/apps/web/src/__tests__/appLayoutDesktopSettingsSidebar.test.tsx` with a second case that mounts `AppLayout` inside `PluginRuntimeProvider` and asserts the plugin entry is visible in desktop mode:

```tsx
expect(container.textContent).toContain('Sample Page Plugin')
```

- [ ] **Step 6: Run the sidebar and layout tests**

Run: `cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm test -- --run src/__tests__/pluginSidebarSection.test.tsx src/__tests__/appLayoutDesktopSettingsSidebar.test.tsx`

Expected: PASS with both test files green.

- [ ] **Step 7: Commit**

```bash
cd /Users/huhui/Projects/Arkloop
git add src/apps/web/src/plugins/PluginSidebarSection.tsx src/apps/web/src/components/Sidebar.tsx src/apps/web/src/__tests__/pluginSidebarSection.test.tsx src/apps/web/src/__tests__/appLayoutDesktopSettingsSidebar.test.tsx
git commit -m "feat(web): add plugin sidebar entries"
```

## Task 4: Plugin Browser Session on Top of Browser Tabs

**Files:**
- Create: `src/apps/web/src/plugins/browser-session.tsx`
- Create: `src/apps/web/src/__tests__/pluginBrowserSession.test.tsx`
- Modify: `src/apps/web/src/plugins/runtime.tsx`
- Modify: `src/apps/web/src/storage.ts`

- [ ] **Step 1: Write the failing browser-session test**

```tsx
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { BrowserTabsProvider } from '../contexts/browser-tabs'
import { PluginBrowserSessionProvider, usePluginBrowserSession } from '../plugins/browser-session'

const desktopMock = vi.hoisted(() => {
  const browserTabsApi = {
    list: vi.fn().mockResolvedValue({ tabs: [] }),
    create: vi.fn().mockResolvedValue({
      id: 'browser-plugin',
      title: 'Plugin Tab',
      url: 'https://example.com/',
      faviconUrl: null,
      loading: false,
      error: null,
      canGoBack: false,
      canGoForward: false,
    }),
    close: vi.fn(),
    navigate: vi.fn(),
    reload: vi.fn(),
    goBack: vi.fn(),
    goForward: vi.fn(),
    show: vi.fn(),
    hide: vi.fn(),
    syncBounds: vi.fn(),
    onStateChanged: vi.fn(() => () => {}),
  }
  return {
    isDesktop: vi.fn(() => true),
    getDesktopApi: vi.fn(() => ({ browserTabs: browserTabsApi })),
    browserTabsApi,
  }
})

vi.mock('@arkloop/shared/desktop', () => ({
  isDesktop: desktopMock.isDesktop,
  getDesktopApi: desktopMock.getDesktopApi,
}))

function Probe() {
  const { ensureBrowserSession } = usePluginBrowserSession()
  return (
    <button type="button" onClick={() => void ensureBrowserSession('sample-browser-plugin')}>
      ensure
    </button>
  )
}

describe('PluginBrowserSessionProvider', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot>

  beforeEach(() => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(() => {
    act(() => root.unmount())
    container.remove()
    vi.clearAllMocks()
  })

  it('creates one browser tab per plugin and reuses it on repeated activation', async () => {
    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/']}>
          <BrowserTabsProvider>
            <PluginBrowserSessionProvider>
              <Probe />
            </PluginBrowserSessionProvider>
          </BrowserTabsProvider>
        </MemoryRouter>,
      )
      await Promise.resolve()
    })

    await act(async () => {
      const button = container.querySelector('button')
      button?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
      button?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
    })

    expect(desktopMock.browserTabsApi.create).toHaveBeenCalledTimes(1)
  })
})
```

- [ ] **Step 2: Run the browser-session test to verify it fails**

Run: `cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm test -- --run src/__tests__/pluginBrowserSession.test.tsx`

Expected: FAIL with module-not-found for `../plugins/browser-session`.

- [ ] **Step 3: Add session storage helpers**

```ts
const PLUGIN_BROWSER_SESSION_KEY = 'arkloop:web:plugin-browser-sessions'

export type PluginBrowserSessionMap = Record<string, string>

export function readPluginBrowserSessionMap(): PluginBrowserSessionMap {
  if (!canUseLocalStorage()) return {}
  try {
    const raw = localStorage.getItem(PLUGIN_BROWSER_SESSION_KEY)
    if (!raw) return {}
    const parsed = JSON.parse(raw) as Record<string, unknown>
    return Object.fromEntries(
      Object.entries(parsed).filter(([, value]) => typeof value === 'string' && value.trim() !== ''),
    )
  } catch {
    return {}
  }
}

export function writePluginBrowserSessionMap(map: PluginBrowserSessionMap): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.setItem(PLUGIN_BROWSER_SESSION_KEY, JSON.stringify(map))
  } catch {
    // ignore
  }
}
```

- [ ] **Step 4: Implement the provider on top of `useBrowserTabs`**

```tsx
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'

import { useBrowserTabs } from '../contexts/browser-tabs'
import { readPluginBrowserSessionMap, writePluginBrowserSessionMap } from '../storage'

type PluginBrowserSessionContextValue = {
  ensureBrowserSession: (
    pluginId: string,
    options?: { openPanel?: boolean }
  ) => Promise<string | null>
  getBrowserTabIdForPlugin: (pluginId: string) => string | null
}

const PluginBrowserSessionContext = createContext<PluginBrowserSessionContextValue | null>(null)

export function PluginBrowserSessionProvider({ children }: { children: ReactNode }) {
  const { tabs, createBrowserTab, activateBrowserTab, openBrowserPanel } = useBrowserTabs()
  const [sessions, setSessions] = useState(readPluginBrowserSessionMap)

  const ensureBrowserSession = useCallback(async (
    pluginId: string,
    options?: { openPanel?: boolean },
  ) => {
    const existingTabId = sessions[pluginId]
    if (existingTabId && tabs.some((tab) => tab.id === existingTabId)) {
      if (options?.openPanel ?? true) openBrowserPanel()
      activateBrowserTab(existingTabId, { openPanel: options?.openPanel ?? true })
      return existingTabId
    }
    const newTabId = await createBrowserTab({ openPanel: options?.openPanel ?? true })
    if (!newTabId) return null
    const next = { ...sessions, [pluginId]: newTabId }
    setSessions(next)
    writePluginBrowserSessionMap(next)
    if (options?.openPanel ?? true) openBrowserPanel()
    activateBrowserTab(newTabId, { openPanel: options?.openPanel ?? true })
    return newTabId
  }, [activateBrowserTab, createBrowserTab, openBrowserPanel, sessions, tabs])

  const value = useMemo<PluginBrowserSessionContextValue>(() => ({
    ensureBrowserSession,
    getBrowserTabIdForPlugin: (pluginId) => sessions[pluginId] ?? null,
  }), [ensureBrowserSession, sessions])

  return (
    <PluginBrowserSessionContext.Provider value={value}>
      {children}
    </PluginBrowserSessionContext.Provider>
  )
}

export function usePluginBrowserSession(): PluginBrowserSessionContextValue {
  const value = useContext(PluginBrowserSessionContext)
  if (!value) throw new Error('usePluginBrowserSession must be used within PluginBrowserSessionProvider')
  return value
}
```

- [ ] **Step 5: Mount `PluginBrowserSessionProvider` around `CreditsProvider`**

```tsx
<BrowserTabsProvider>
  <PluginRuntimeProvider>
    <PluginBrowserSessionProvider>
      <CreditsProvider>
        <AppLayout />
      </CreditsProvider>
    </PluginBrowserSessionProvider>
  </PluginRuntimeProvider>
</BrowserTabsProvider>
```

- [ ] **Step 6: Run the browser-session and browser-tabs tests**

Run: `cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm test -- --run src/__tests__/pluginBrowserSession.test.tsx src/__tests__/browserTabs.test.tsx`

Expected: PASS with both files green.

- [ ] **Step 7: Commit**

```bash
cd /Users/huhui/Projects/Arkloop
git add src/apps/web/src/plugins/browser-session.tsx src/apps/web/src/storage.ts src/apps/web/src/plugins/runtime.tsx src/apps/web/src/App.tsx src/apps/web/src/__tests__/pluginBrowserSession.test.tsx
git commit -m "feat(web): add plugin browser sessions"
```

## Task 5: Workspace Takeover in `AppLayout`

**Files:**
- Create: `src/apps/web/src/plugins/PluginWorkspaceShell.tsx`
- Create: `src/apps/web/src/__tests__/appLayoutPluginWorkspace.test.tsx`
- Modify: `src/apps/web/src/layouts/AppLayout.tsx`
- Modify: `src/apps/web/src/plugins/PluginHostPage.tsx`
- Modify: `src/apps/web/src/plugins/runtime.tsx`

- [ ] **Step 1: Write the failing `AppLayout` takeover test**

```tsx
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ToastProvider } from '@arkloop/shared'

import { AppLayout } from '../layouts/AppLayout'
import { LocaleProvider } from '../contexts/LocaleContext'
import { AuthProvider } from '../contexts/auth'
import { ThreadListProvider } from '../contexts/thread-list'
import { AppUIProvider } from '../contexts/app-ui'
import { BrowserTabsProvider } from '../contexts/browser-tabs'
import { PluginRuntimeProvider } from '../plugins/runtime'
import { PluginBrowserSessionProvider } from '../plugins/browser-session'
import { CreditsProvider } from '../contexts/credits'
import { PluginHostPage } from '../plugins/PluginHostPage'

describe('AppLayout plugin workspace takeover', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot>

  beforeEach(() => {
    container = document.createElement('div')
    document.body.appendChild(container)
  root = createRoot(container)
  })

  afterEach(() => {
    act(() => root.unmount())
    container.remove()
    vi.clearAllMocks()
  })

  it('renders plugin workspace content while keeping the global sidebar visible', async () => {
    await act(async () => {
      root.render(
        <LocaleProvider>
          <ToastProvider>
            <MemoryRouter initialEntries={['/plugins/sample-page-plugin']}>
              <AuthProvider accessToken="token" onLoggedOut={vi.fn()}>
                <ThreadListProvider>
                  <AppUIProvider>
                    <BrowserTabsProvider>
                      <PluginRuntimeProvider>
                        <PluginBrowserSessionProvider>
                          <CreditsProvider>
                            <Routes>
                              <Route element={<AppLayout />}>
                                <Route path="/plugins/:pluginId" element={<PluginHostPage />} />
                              </Route>
                            </Routes>
                          </CreditsProvider>
                        </PluginBrowserSessionProvider>
                      </PluginRuntimeProvider>
                    </BrowserTabsProvider>
                  </AppUIProvider>
                </ThreadListProvider>
              </AuthProvider>
            </MemoryRouter>
          </ToastProvider>
        </LocaleProvider>,
      )
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(container.textContent).toContain('Sample Page Plugin')
    expect(container.textContent).toContain('sample page plugin page')
  })
})
```

- [ ] **Step 2: Run the layout takeover test to verify it fails**

Run: `cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm test -- --run src/__tests__/appLayoutPluginWorkspace.test.tsx`

Expected: FAIL because `AppLayout` still treats plugin pages as ordinary route content with no workspace shell behavior.

- [ ] **Step 3: Implement the route-backed plugin workspace shell**

```tsx
// src/apps/web/src/plugins/PluginWorkspaceShell.tsx
export function PluginWorkspaceShell({ plugin }: { plugin: PluginDefinition }) {
  if (!plugin.surfaces.mount) return null

  const Component = plugin.surfaces.mount

  return (
    <div className="flex h-full min-h-0 w-full min-w-0 flex-col" data-testid="plugin-workspace-shell">
      <div
        className="flex h-12 shrink-0 items-center px-4 text-sm font-medium text-[var(--c-text-primary)]"
        style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}
      >
        {plugin.title}
      </div>
      <div className="min-h-0 flex-1">
        <Component />
      </div>
    </div>
  )
}
```

- [ ] **Step 4: Teach `PluginHostPage` and `AppLayout` about page plugins vs browser-driven plugins**

```tsx
// in PluginHostPage.tsx
import { PluginWorkspaceShell } from './PluginWorkspaceShell'

if (presentation === 'route' && plugin.shell.mode === 'plugin-workspace') {
  return <PluginWorkspaceShell plugin={plugin} />
}
```

```tsx
// in AppLayout.tsx, keep Sidebar and outer frame unchanged, but coordinate browser takeover
<main
  className="relative flex min-w-0 flex-1 flex-col overflow-y-auto"
  style={{ scrollbarGutter: 'stable' }}
  data-testid="workspace-host"
>
  <Outlet />
  {notificationsOpen && <NotificationsPanel ... />}
</main>
```

Keep the existing sidebar and `Workspace Host` intact for `route` plugins. For `embedded-browser` and `hybrid`, coordinate the existing global right-side browser panel from `AppLayout`: `hybrid` keeps `workspace-host` visible, while `embedded-browser` forces the browser fullscreen so `workspace-host` is hidden.

- [ ] **Step 5: Add browser takeover behavior to `AppLayout`**

Drive the browser panel from plugin presentation state instead of returning a placeholder page:

```tsx
const pluginUsesBrowserPanel =
  activePluginPresentation === 'embedded-browser' ||
  activePluginPresentation === 'hybrid'

const pluginForcesBrowserFullscreen =
  activePluginPresentation === 'embedded-browser'
```

Then:

- `hybrid`: keep `workspace-host` rendered and open the right-side browser panel
- `embedded-browser`: open the right-side browser panel and fullscreen it so `workspace-host` is hidden
- `route`: close takeover state and let normal route content render

- [ ] **Step 6: Run focused tests and typecheck**

Run: `cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm test -- --run src/__tests__/pluginHostPage.test.tsx src/__tests__/appLayoutPluginWorkspace.test.tsx src/__tests__/pluginSidebarSection.test.tsx`

Expected: PASS with all three files green.

Run: `cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm type-check`

Expected: PASS with no TypeScript errors.

- [ ] **Step 7: Commit**

```bash
cd /Users/huhui/Projects/Arkloop
git add src/apps/web/src/plugins/PluginWorkspaceShell.tsx src/apps/web/src/plugins/PluginHostPage.tsx src/apps/web/src/layouts/AppLayout.tsx src/apps/web/src/__tests__/appLayoutPluginWorkspace.test.tsx
git commit -m "feat(web): add plugin workspace shell"
```

## Final Verification

- [ ] Run: `cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm test -- --run src/__tests__/pluginRuntime.test.tsx src/__tests__/pluginHostPage.test.tsx src/__tests__/pluginSidebarSection.test.tsx src/__tests__/pluginBrowserSession.test.tsx src/__tests__/appLayoutPluginWorkspace.test.tsx src/__tests__/browserTabs.test.tsx src/__tests__/appLayoutDesktopSettingsSidebar.test.tsx`
Expected: PASS with all plugin-related test files green.

- [ ] Run: `cd /Users/huhui/Projects/Arkloop/src/apps/web && pnpm type-check`
Expected: PASS with no type errors.

- [ ] Run: `cd /Users/huhui/Projects/Arkloop && git status --short`
Expected: clean working tree after the final commit.

## Self-Review

- Spec coverage:
  - Sidebar entry: covered in Task 3.
  - Unified plugin route and host: covered in Task 2.
  - Workspace takeover: covered in Task 5.
  - Browser-session reuse: covered in Task 4.
  - Desktop-first built-in plugin strategy: covered in Task 1 and Task 4.
- Placeholder scan:
  - No `TBD`, `TODO`, or “handle later” text remains.
  - All code-changing steps include concrete snippets.
  - All verification steps include exact commands and expected outcomes.
- Type consistency:
  - `PluginDefinition`, `PluginPresentation`, and `PluginShellMode` are introduced once in `types.ts` and reused consistently in runtime, host, and tests.
