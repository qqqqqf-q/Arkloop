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

  return (
    <div data-testid="plugin-host-placeholder">
      plugin host placeholder for {plugin.id} ({presentation})
    </div>
  )
}
