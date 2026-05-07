export type PluginShellMode = 'plugin-main' | 'plugin-workspace'

export type PluginPresentation = 'route' | 'embedded-browser' | 'hybrid'

export type PluginBrowserLocation = {
  pathname: string
  search: string
  hash: string
}

export type PluginResolveBrowserUrlContext = {
  pluginId: string
  presentation: PluginPresentation
  location: PluginBrowserLocation
}

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
    resolveBrowserUrl?: (context: PluginResolveBrowserUrlContext) => Promise<string> | string
    browserPlacement?: 'main' | 'sidecar'
  }
}
