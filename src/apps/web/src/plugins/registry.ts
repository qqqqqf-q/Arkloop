import { SamplePluginPage } from './builtin/SamplePluginPage'
import type { PluginDefinition } from './types'

export const builtinPlugins: PluginDefinition[] = [
  {
    id: 'sample-plugin',
    title: 'Sample Plugin',
    desktopOnly: true,
    nav: { section: 'workspace', order: 100 },
    shell: { mode: 'plugin-workspace' },
    presentation: {
      default: 'route',
      supported: ['route', 'embedded-browser', 'hybrid'],
    },
    surfaces: {
      mount: SamplePluginPage,
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
