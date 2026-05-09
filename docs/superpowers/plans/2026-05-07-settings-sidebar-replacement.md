# Desktop Settings Sidebar Replacement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the desktop settings navigation replace the app sidebar slot instead of rendering as a second sidebar to the right of the app sidebar.

**Architecture:** Keep `AppLayout` as the desktop shell owner and move the desktop settings render up one level. When `settingsOpen` is true on desktop, replace the entire `Sidebar + LayoutMain` row with the existing `DesktopSettings` surface, so its own left navigation naturally occupies the former app-sidebar slot without duplicating settings state or adding a second sidebar.

**Tech Stack:** React 19, TypeScript 5.9, React Router 7, Vitest, existing app UI contexts

---

## File Structure

- Modify: `src/apps/web/src/layouts/AppLayout.tsx`
  Owns desktop shell composition. Moves desktop settings rendering out of `LayoutMain` and swaps the full desktop row when `settingsOpen` is true.
- Create: `src/apps/web/src/__tests__/appLayoutDesktopSettingsSidebar.test.tsx`
  Covers the desktop-only regression: opening settings replaces the app sidebar slot and keeps settings content in the main pane.

### Task 1: Add the failing desktop layout regression test

**Files:**
- Create: `src/apps/web/src/__tests__/appLayoutDesktopSettingsSidebar.test.tsx`
- Modify: `src/apps/web/src/layouts/AppLayout.tsx`

- [ ] **Step 1: Write the failing test**

```tsx
import { act, useEffect } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes, Outlet } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AppLayout } from '../layouts/AppLayout'
import { LocaleProvider } from '../contexts/LocaleContext'
import { AuthProvider } from '../contexts/auth'
import { ThreadListProvider } from '../contexts/thread-list'
import { AppUIProvider, useSettingsUI } from '../contexts/app-ui'
import { CreditsProvider } from '../contexts/credits'
import { BrowserTabsProvider } from '../contexts/browser-tabs'
import { getMe, listThreads, getMyCredits, streamThreadRunStateEvents } from '../api'

vi.mock('@arkloop/shared/desktop', async () => {
  const actual = await vi.importActual<typeof import('@arkloop/shared/desktop')>('@arkloop/shared/desktop')
  return {
    ...actual,
    isDesktop: vi.fn(() => true),
    getDesktopApi: () => ({}),
  }
})

vi.mock('../api', async () => {
  const actual = await vi.importActual<typeof import('../api')>('../api')
  return {
    ...actual,
    getMe: vi.fn(),
    listThreads: vi.fn(),
    getMyCredits: vi.fn(),
    streamThreadRunStateEvents: vi.fn(),
  }
})

function OpenSettingsOnMount() {
  const { openSettings } = useSettingsUI()

  useEffect(() => {
    openSettings('settings')
  }, [openSettings])

  return null
}

function OutletShell() {
  return (
    <>
      <OpenSettingsOnMount />
      <Outlet />
    </>
  )
}

describe('AppLayout desktop settings sidebar replacement', () => {
  beforeEach(() => {
    vi.mocked(getMe).mockResolvedValue({
      id: 'user-1',
      email: 'test@example.com',
      name: 'Test User',
      created_at: '2026-01-01T00:00:00.000Z',
      updated_at: '2026-01-01T00:00:00.000Z',
    } as Awaited<ReturnType<typeof getMe>>)
    vi.mocked(listThreads).mockResolvedValue([])
    vi.mocked(getMyCredits).mockResolvedValue({ balance: 0, currency: 'credits' } as Awaited<ReturnType<typeof getMyCredits>>)
    vi.mocked(streamThreadRunStateEvents).mockReturnValue(new Promise(() => {}))
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  it('uses settings navigation in the left slot instead of rendering a second sidebar', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <MemoryRouter initialEntries={['/']}>
            <AuthProvider accessToken="token" onLoggedOut={vi.fn()}>
              <ThreadListProvider>
                <AppUIProvider>
                  <BrowserTabsProvider>
                    <CreditsProvider>
                      <Routes>
                        <Route element={<AppLayout />}>
                          <Route element={<OutletShell />}>
                            <Route index element={<div>chat body</div>} />
                          </Route>
                        </Route>
                      </Routes>
                    </CreditsProvider>
                  </BrowserTabsProvider>
                </AppUIProvider>
              </ThreadListProvider>
            </AuthProvider>
          </MemoryRouter>
        </LocaleProvider>,
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(container.textContent).toContain('chat body')
    expect(container.textContent).toContain('设置')
    expect(container.textContent).toContain('通用')
    expect(container.textContent).not.toContain('新对话')

    act(() => root.unmount())
    container.remove()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test src/__tests__/appLayoutDesktopSettingsSidebar.test.tsx`
Expected: FAIL because the desktop layout still renders the app `Sidebar` on the far left while `LayoutMain` renders `DesktopSettings` in the main pane, so the assertion rejecting the app-sidebar affordance still fails.

- [ ] **Step 3: Keep the failure focused**

```tsx
expect(container.textContent).not.toContain('新对话')
expect(container.textContent).toContain('通用')
```

The first assertion protects against the old app sidebar staying visible; the second proves the replacement left slot is the settings nav rather than an empty column.

- [ ] **Step 4: Commit the red test**

```bash
git add src/apps/web/src/__tests__/appLayoutDesktopSettingsSidebar.test.tsx
git commit -m "test: capture desktop settings sidebar replacement"
```

### Task 2: Move the desktop settings surface to the app-shell row

**Files:**
- Modify: `src/apps/web/src/layouts/AppLayout.tsx`
- Test: `src/apps/web/src/__tests__/appLayoutDesktopSettingsSidebar.test.tsx`

- [ ] **Step 1: Remove the desktop settings branch from `LayoutMain`**

```tsx
return (
  <>
    {isSearchOpen && (
      <ChatsSearchModal
        threads={filteredThreads}
        mode={appMode}
        accessToken={accessToken}
        onClose={onSearchClose}
      />
    )}

    <div
      ref={containerRef}
      className="relative flex min-w-0 flex-1 overflow-hidden rounded-l-xl"
      style={{ border: '0.5px solid var(--c-border-subtle)' }}
    >
      <div className="flex min-w-0 flex-1 overflow-hidden">
        {!browserFullscreen && (
          <div
            className="flex min-w-0 flex-col overflow-hidden"
            style={{ flex: browserPanelOpen ? `0 0 ${chatRatio}%` : '1 1 100%' }}
          >
            <DesktopTabBar
              appMode={appMode}
              availableModes={availableModes}
              browserPanelOpen={browserPanelOpen}
              onSetAppMode={onSetAppMode}
              onToggleBrowserPanel={onToggleBrowserPanel}
              currentThread={currentThread}
            />
            <MainViewport
              accessToken={accessToken}
              notificationsOpen={notificationsOpen}
              closeNotifications={closeNotifications}
              markNotificationRead={markNotificationRead}
            />
          </div>
        )}
        {desktop && browserPanelOpen && (
          <aside
            className="flex min-h-0 min-w-0 shrink-0 bg-[var(--c-bg-page)] relative"
            style={{
              flex: browserFullscreen ? '1 1 100%' : `0 0 ${100 - chatRatio}%`,
            }}
          >
            {!browserFullscreen && (
              <div
                className="absolute left-0 top-0 bottom-0 cursor-col-resize"
                style={{ width: '12px', marginLeft: '-6px', zIndex: 10 }}
                onMouseDown={handleMouseDown}
              >
                <div
                  className="absolute right-0 top-0 bottom-0 w-[3px] bg-transparent hover:bg-[var(--c-border-subtle)] transition-colors"
                  style={{ right: '3px' }}
                />
              </div>
            )}
            <div
              className="flex-1 min-w-0"
              style={{
                borderLeft: browserFullscreen ? 'none' : '0.5px solid var(--c-border-subtle)',
              }}
            >
              <BrowserTabPage
                browserFullscreen={browserFullscreen}
                onToggleBrowserFullscreen={onToggleBrowserFullscreen}
              />
            </div>
          </aside>
        )}
      </div>
    </div>
  </>
)
```

`LayoutMain` should stop knowing about `settingsOpen`. Its job returns to "chat shell content only".

- [ ] **Step 2: Lift the desktop settings condition to `AppLayout`**

```tsx
const {
  settingsOpen,
  desktopSettingsSection,
  desktopAdvancedSection,
  desktopSettingsRequestId,
  closeSettings,
} = useSettingsUI()
```

Use the same settings state the app already owns; do not add a second settings-open flag.

- [ ] **Step 3: Replace the full desktop row when settings are open**

```tsx
<div className="flex min-h-0 flex-1">
  {desktop && settingsOpen ? (
    <DesktopSettings
      me={me}
      accessToken={accessToken}
      initialSection={desktopSettingsSection}
      initialAdvancedKey={desktopAdvancedSection}
      sectionRequestId={desktopSettingsRequestId}
      onClose={closeSettings}
      onLogout={logout}
      onMeUpdated={updateMe}
      onTrySkill={queueSkillPrompt}
    />
  ) : (
    <>
      {!sidebarHiddenByWidth && (
        <Sidebar
          threads={filteredThreads}
          onNewThread={handleNewThread}
          onThreadDeleted={handleThreadDeleted}
          isPrivateMode={isPrivateMode}
          pendingIncognitoMode={pendingIncognitoMode}
          privateThreadIds={privateThreadIds}
          onTogglePrivateMode={togglePrivateMode}
        />
      )}
      <LayoutMain
        desktop={desktop}
        isSearchOpen={searchOverlayOpen}
        filteredThreads={filteredThreads}
        appMode={activeAppMode}
        availableModes={availableAppModes}
        pathname={location.pathname}
        onSearchClose={handleSearchClose}
        onMeUpdated={updateMe}
        onTrySkill={queueSkillPrompt}
        onSetAppMode={setAppMode}
        browserPanelOpen={browserPanelOpen}
        onToggleBrowserPanel={toggleBrowserPanel}
        browserFullscreen={browserFullscreen}
        onToggleBrowserFullscreen={handleToggleBrowserFullscreen}
        currentThread={currentThread}
      />
    </>
  )}
</div>
```

The key behavior is "one desktop row at a time": either chat shell with app sidebar, or the existing desktop settings surface. Never both.

- [ ] **Step 4: Run the red test and verify it turns green**

Run: `pnpm test src/__tests__/appLayoutDesktopSettingsSidebar.test.tsx`
Expected: PASS with the app sidebar affordance removed and the settings navigation visible in the left slot.

- [ ] **Step 5: Run nearby regression coverage**

Run: `pnpm test src/__tests__/appLayoutLoading.test.tsx src/__tests__/appUI.test.tsx`
Expected: PASS, confirming the desktop shell still loads correctly and the existing UI context behavior remains intact.

- [ ] **Step 6: Check diagnostics for edited files**

Run diagnostics for:
- `src/apps/web/src/layouts/AppLayout.tsx`
- `src/apps/web/src/__tests__/appLayoutDesktopSettingsSidebar.test.tsx`

Expected: no new TypeScript or lint diagnostics.

- [ ] **Step 7: Commit the feature**

```bash
git add src/apps/web/src/layouts/AppLayout.tsx src/apps/web/src/__tests__/appLayoutDesktopSettingsSidebar.test.tsx
git commit -m "feat: replace app sidebar with settings nav on desktop"
```
