import { useEffect } from 'react'

import type { PluginDefinition, PluginPresentation } from './types'
import { usePluginBrowserSession } from './browser-session'

export function PluginWorkspaceShell({
  plugin,
  presentation,
}: {
  plugin: PluginDefinition
  presentation: PluginPresentation
}) {
  const { ensureBrowserSession } = usePluginBrowserSession()

  useEffect(() => {
    if (presentation === 'route') return
    void ensureBrowserSession(plugin.id)
  }, [ensureBrowserSession, plugin.id, presentation])

  const Component = plugin.surfaces.mount

  return (
    <div
      className="flex h-full min-h-0 w-full min-w-0 flex-col"
      data-testid="plugin-workspace-shell"
    >
      <div
        className="flex h-12 shrink-0 items-center px-4 text-sm font-medium text-(--c-text-primary)"
        style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}
      >
        {plugin.title}
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        {Component ? <Component /> : null}
      </div>
    </div>
  )
}
