import type { ReactElement } from 'react'
import { useEffect, useState } from 'react'
import { useLocation } from 'react-router-dom'

import { BrowserTabPage } from '../components/BrowserTabPage'
import { useBrowserTabs } from '../contexts/browser-tabs'
import type { PluginDefinition, PluginPresentation } from './types'
import { usePluginBrowserSession } from './browser-session'
import { usePluginRuntime } from './runtime'

const PRESENTATION_LABELS: Record<PluginPresentation, string> = {
  route: 'Page',
  'embedded-browser': 'Browser',
  hybrid: 'Hybrid',
}

function normalizePluginBrowserTarget(rawUrl: string): string | null {
  const trimmed = rawUrl.trim()
  if (!trimmed) return null
  const candidate = /^[a-z][a-z\d+\-.]*:\/\//i.test(trimmed) ? trimmed : `https://${trimmed}`
  try {
    const parsed = new URL(candidate)
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
      return null
    }
    return parsed.toString()
  } catch {
    return null
  }
}

export function PluginWorkspaceShell({
  plugin,
  presentation,
}: {
  plugin: PluginDefinition
  presentation: PluginPresentation
}) {
  const location = useLocation()
  const { ensureBrowserSession, getBrowserTabIdForPlugin } = usePluginBrowserSession()
  const { closeBrowserPanel, navigateBrowserTab } = useBrowserTabs()
  const { setPresentationForPlugin } = usePluginRuntime()
  const [browserTarget, setBrowserTarget] = useState<string | null>(null)

  useEffect(() => {
    if (presentation === 'route') {
      closeBrowserPanel()
      return
    }
    let cancelled = false

    const openPluginSurface = async () => {
      const browserTabId = await ensureBrowserSession(plugin.id, { openPanel: false })
      if (!browserTabId || cancelled) return
      const rawBrowserUrl = await plugin.surfaces.resolveBrowserUrl?.({
        pluginId: plugin.id,
        presentation,
        location: {
          pathname: location.pathname,
          search: location.search,
          hash: location.hash,
        },
      })
      if (cancelled) return
      const resolvedBrowserUrl = rawBrowserUrl
        ? normalizePluginBrowserTarget(rawBrowserUrl)
        : null
      setBrowserTarget(resolvedBrowserUrl)
      if (!resolvedBrowserUrl) return
      await navigateBrowserTab(browserTabId, resolvedBrowserUrl)
    }

    void openPluginSurface()

    return () => {
      cancelled = true
    }
  }, [
    closeBrowserPanel,
    ensureBrowserSession,
    location.hash,
    location.pathname,
    location.search,
    navigateBrowserTab,
    plugin.id,
    plugin.surfaces,
    presentation,
  ])

  const Component = plugin.surfaces.mount
  const pluginBrowserTabId = getBrowserTabIdForPlugin(plugin.id)
  const supportsPresentationSwitch =
    plugin.presentation.supported.length > 1
  const renderBrowserSurface = () => <BrowserTabPage embedded forcedTabId={pluginBrowserTabId} />

  let content: ReactElement | null = null

  if (presentation === 'embedded-browser') {
    content = (
      <div className="min-h-0 flex-1 overflow-hidden" data-testid="plugin-browser-layout">
        {renderBrowserSurface()}
      </div>
    )
  } else if (presentation === 'hybrid') {
    content = (
      <div
        className="grid min-h-0 flex-1 gap-4 overflow-hidden p-4 lg:grid-cols-[minmax(0,1fr)_minmax(360px,520px)]"
        data-testid="plugin-hybrid-layout"
      >
        <div className="min-h-0 overflow-auto rounded-2xl border border-(--c-border-subtle) bg-(--c-bg-page)">
          {Component ? <Component /> : null}
        </div>
        <div
          className="min-h-0 overflow-hidden rounded-2xl border border-(--c-border-subtle) bg-(--c-bg-page)"
          data-testid="plugin-browser-pane"
        >
          {renderBrowserSurface()}
        </div>
      </div>
    )
  } else {
    content = (
      <div className="min-h-0 flex-1 overflow-auto">
        {Component ? <Component /> : null}
      </div>
    )
  }

  return (
    <div
      className="flex h-full min-h-0 w-full min-w-0 flex-col"
      data-testid="plugin-workspace-shell"
    >
      <div
        className="flex h-10 shrink-0 items-center justify-between gap-3 px-3 text-xs font-medium text-(--c-text-primary)"
        data-testid="plugin-workspace-header"
        style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}
      >
        <div className="flex min-w-0 items-center gap-2">
          <div className="min-w-0 truncate">{plugin.title}</div>
          {browserTarget ? (
            <span
              className="max-w-56 truncate text-(--c-text-tertiary)"
              data-testid="plugin-browser-target"
              title={browserTarget}
            >
              {browserTarget}
            </span>
          ) : null}
        </div>
        <div className="flex items-center gap-2">
          {supportsPresentationSwitch ? (
            <div
              className="flex items-center gap-1 rounded-full p-0.5"
              style={{ backgroundColor: 'var(--c-bg-sub)' }}
            >
              {plugin.presentation.supported.map((mode) => (
                <button
                  key={mode}
                  type="button"
                  aria-pressed={mode === presentation}
                  className="rounded-full px-2 py-1 text-[11px] transition-colors"
                  data-testid={`plugin-presentation-button-${mode}`}
                  onClick={() => setPresentationForPlugin(plugin.id, mode)}
                  style={
                    mode === presentation
                      ? {
                          backgroundColor: 'var(--c-bg-page)',
                          color: 'var(--c-text-primary)',
                        }
                      : {
                          color: 'var(--c-text-secondary)',
                        }
                  }
                >
                  {PRESENTATION_LABELS[mode]}
                </button>
              ))}
            </div>
          ) : null}
        </div>
      </div>
      {content}
    </div>
  )
}
