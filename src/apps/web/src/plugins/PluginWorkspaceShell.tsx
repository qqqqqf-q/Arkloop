import { useEffect } from 'react'

import { useBrowserTabs } from '../contexts/browser-tabs'
import type { PluginDefinition, PluginPresentation } from './types'
import { usePluginBrowserSession } from './browser-session'
import { usePluginRuntime } from './runtime'

const PRESENTATION_LABELS: Record<PluginPresentation, string> = {
  route: 'Page',
  'embedded-browser': 'Browser',
  hybrid: 'Hybrid',
}

export function PluginWorkspaceShell({
  plugin,
  presentation,
}: {
  plugin: PluginDefinition
  presentation: PluginPresentation
}) {
  const { ensureBrowserSession } = usePluginBrowserSession()
  const { closeBrowserPanel, navigateBrowserTab } = useBrowserTabs()
  const { setPresentationForPlugin } = usePluginRuntime()

  useEffect(() => {
    if (presentation === 'route') {
      closeBrowserPanel()
      return
    }
    let cancelled = false

    const openPluginSurface = async () => {
      const browserTabId = await ensureBrowserSession(plugin.id)
      if (!browserTabId || cancelled) return
      const browserUrl = await plugin.surfaces.resolveBrowserUrl?.()
      if (!browserUrl || cancelled) return
      await navigateBrowserTab(browserTabId, browserUrl)
    }

    void openPluginSurface()

    return () => {
      cancelled = true
    }
  }, [closeBrowserPanel, ensureBrowserSession, navigateBrowserTab, plugin.id, plugin.surfaces, presentation])

  const Component = plugin.surfaces.mount
  const supportsPresentationSwitch =
    plugin.presentation.supported.length > 1

  return (
    <div
      className="flex h-full min-h-0 w-full min-w-0 flex-col"
      data-testid="plugin-workspace-shell"
    >
      <div
        className="flex h-12 shrink-0 items-center justify-between gap-3 px-4 text-sm font-medium text-(--c-text-primary)"
        style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}
      >
        <div className="min-w-0 truncate">{plugin.title}</div>
        <div className="flex items-center gap-2">
          <span
            className="rounded-full px-2 py-1 text-xs text-(--c-text-secondary)"
            data-testid="plugin-presentation-value"
            style={{ backgroundColor: 'var(--c-bg-sub)' }}
          >
            {presentation}
          </span>
          {supportsPresentationSwitch ? (
            <div
              className="flex items-center gap-1 rounded-full p-1"
              style={{ backgroundColor: 'var(--c-bg-sub)' }}
            >
              {plugin.presentation.supported.map((mode) => (
                <button
                  key={mode}
                  type="button"
                  className="rounded-full px-2 py-1 text-xs transition-colors"
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
      <div className="min-h-0 flex-1 overflow-auto">
        {Component ? <Component /> : null}
      </div>
    </div>
  )
}
