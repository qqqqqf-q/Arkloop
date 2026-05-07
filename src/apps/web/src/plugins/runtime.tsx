import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { useNavigate } from 'react-router-dom'

import { readPluginRuntimeState, writePluginRuntimeState } from '../storage'
import { getBuiltinPluginById } from './registry'
import type { PluginDefinition, PluginPresentation } from './types'

type PluginRuntimeContextValue = {
  activePluginId: string | null
  activePlugin: PluginDefinition | null
  getPresentationForPlugin: (pluginId: string) => PluginPresentation | null
  openPlugin: (pluginId: string, presentation?: PluginPresentation) => Promise<void>
  setPresentationForPlugin: (pluginId: string, presentation: PluginPresentation) => void
}

const PluginRuntimeContext = createContext<PluginRuntimeContextValue | null>(null)

export function PluginRuntimeProvider({ children }: { children: ReactNode }) {
  const navigate = useNavigate()
  const [activePluginId, setActivePluginId] = useState<string | null>(
    () => readPluginRuntimeState().lastPluginId,
  )
  const [presentationByPluginId, setPresentationByPluginId] = useState<
    Record<string, PluginPresentation>
  >(() => readPluginRuntimeState().presentationByPluginId)

  const setPresentationForPlugin = useCallback(
    (pluginId: string, presentation: PluginPresentation) => {
      setPresentationByPluginId((current) => {
        const next = { ...current, [pluginId]: presentation }
        writePluginRuntimeState({
          lastPluginId: activePluginId,
          presentationByPluginId: next,
        })
        return next
      })
    },
    [activePluginId],
  )

  const openPlugin = useCallback(
    async (pluginId: string, presentation?: PluginPresentation) => {
      const plugin = getBuiltinPluginById(pluginId)
      if (!plugin) return
      const nextPresentation =
        presentation ?? presentationByPluginId[pluginId] ?? plugin.presentation.default
      const nextPresentationMap = {
        ...presentationByPluginId,
        [pluginId]: nextPresentation,
      }
      setActivePluginId(pluginId)
      setPresentationByPluginId(nextPresentationMap)
      writePluginRuntimeState({
        lastPluginId: pluginId,
        presentationByPluginId: nextPresentationMap,
      })
      navigate(`/plugins/${encodeURIComponent(pluginId)}`)
    },
    [navigate, presentationByPluginId],
  )

  const value = useMemo<PluginRuntimeContextValue>(
    () => ({
      activePluginId,
      activePlugin: activePluginId ? getBuiltinPluginById(activePluginId) : null,
      getPresentationForPlugin: (pluginId) =>
        presentationByPluginId[pluginId] ??
        getBuiltinPluginById(pluginId)?.presentation.default ??
        null,
      openPlugin,
      setPresentationForPlugin,
    }),
    [activePluginId, openPlugin, presentationByPluginId, setPresentationForPlugin],
  )

  return <PluginRuntimeContext.Provider value={value}>{children}</PluginRuntimeContext.Provider>
}

export function usePluginRuntime(): PluginRuntimeContextValue {
  const value = useContext(PluginRuntimeContext)
  if (!value) {
    throw new Error('usePluginRuntime must be used within PluginRuntimeProvider')
  }
  return value
}
