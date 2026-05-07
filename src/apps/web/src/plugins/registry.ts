import {
  SampleHybridPluginPage,
  SamplePagePluginPage,
} from './builtin/SamplePluginPage'
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
      resolveBrowserUrl: ({ location }) => {
        const target = new URLSearchParams(location.search).get('target')?.trim()
        return target || 'https://example.com/'
      },
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
      resolveBrowserUrl: ({ location }) => {
        const target = new URLSearchParams(location.search).get('target')?.trim()
        return target || 'https://example.com/'
      },
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
