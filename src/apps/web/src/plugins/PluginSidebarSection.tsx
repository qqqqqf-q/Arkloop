import { isDesktop } from '@arkloop/shared/desktop'

import { listBuiltinPlugins } from './registry'
import { usePluginRuntime } from './runtime'

export function PluginSidebarSection() {
  const { activePluginId, openPlugin } = usePluginRuntime()
  const desktop = isDesktop()
  const plugins = listBuiltinPlugins().filter(
    (plugin) => !(plugin.desktopOnly && !desktop),
  )

  if (plugins.length === 0) return null

  return (
    <section aria-label="Plugins" className="mb-3">
      <div className="mb-[12px] mt-1 flex shrink-0 items-center gap-2 px-2">
        <h3
          className="text-(--c-text-tertiary) text-[11px] tracking-[0.3px]"
          style={{ fontWeight: 'var(--c-sidebar-section-weight)' }}
        >
          Plugins
        </h3>
      </div>
      <div className="flex flex-col gap-[2px]">
        {plugins.map((plugin) => {
          const active = activePluginId === plugin.id
          return (
            <button
              key={plugin.id}
              type="button"
              data-testid={`plugin-entry-${plugin.id}`}
              onClick={() => void openPlugin(plugin.id)}
              className="text-(--c-text-primary) flex h-[34px] w-full items-center rounded-[6px] px-3 text-left text-[13.5px] leading-[20px]"
              style={{
                background: active ? 'var(--c-bg-deep)' : 'transparent',
                fontWeight: 'var(--c-sidebar-thread-weight)',
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
