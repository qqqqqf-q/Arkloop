import { Navigate, useParams } from 'react-router-dom'

import { getBuiltinPluginById } from './registry'
import { usePluginRuntime } from './runtime'
import { PluginWorkspaceShell } from './PluginWorkspaceShell'

export function PluginHostPage() {
  const { pluginId = '' } = useParams()
  const plugin = getBuiltinPluginById(pluginId)
  const { getPresentationForPlugin } = usePluginRuntime()

  if (!plugin) {
    return <Navigate to="/" replace />
  }

  const presentation = getPresentationForPlugin(plugin.id) ?? plugin.presentation.default

  if (plugin.shell.mode === 'plugin-workspace') {
    return <PluginWorkspaceShell plugin={plugin} presentation={presentation} />
  }

  if (presentation === 'route' && plugin.surfaces.mount) {
    const Component = plugin.surfaces.mount
    return <Component />
  }

  if (presentation === 'embedded-browser' || presentation === 'hybrid') {
    return <PluginWorkspaceShell plugin={plugin} presentation={presentation} />
  }

  return (
    <div data-testid="plugin-host-placeholder">
      plugin host placeholder for {plugin.id} ({presentation})
    </div>
  )
}
