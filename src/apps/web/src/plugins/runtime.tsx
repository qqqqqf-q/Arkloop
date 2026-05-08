import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { useLocation, useNavigate } from 'react-router-dom'

import { readPluginRuntimeState, writePluginRuntimeState } from '../storage'
import { getBuiltinPluginById } from './registry'
import type { PluginDefinition, PluginPresentation } from './types'

type PluginRuntimeContextValue = {
  activePluginId: string | null
  activePlugin: PluginDefinition | null
  activePluginPresentation: PluginPresentation | null
  getPresentationForPlugin: (pluginId: string) => PluginPresentation | null
  openPlugin: (pluginId: string, presentation?: PluginPresentation) => Promise<void>
  setPresentationForPlugin: (pluginId: string, presentation: PluginPresentation) => void
  deactivateActivePlugin: () => void
}

const PluginRuntimeContext = createContext<PluginRuntimeContextValue | null>(null)

export function PluginRuntimeProvider({ children }: { children: ReactNode }) {
  const navigate = useNavigate()
  const location = useLocation()
  const [activePluginId, setActivePluginId] = useState<string | null>(null)
  const [activePluginContextPath, setActivePluginContextPath] = useState<string | null>(null)
  const [presentationByPluginId, setPresentationByPluginId] = useState<
    Record<string, PluginPresentation>
  >(() => readPluginRuntimeState().presentationByPluginId)
  const lastWorkspacePathRef = useRef('/')

  useEffect(() => {
    if (location.pathname.startsWith('/plugins/')) return
    const nextPath = `${location.pathname}${location.search}${location.hash}` || '/'
    lastWorkspacePathRef.current = nextPath
  }, [location.hash, location.pathname, location.search])

  useEffect(() => {
    if (!location.pathname.startsWith('/plugins/')) return
    const pluginId = decodeURIComponent(location.pathname.slice('/plugins/'.length))
    const plugin = getBuiltinPluginById(pluginId)
    if (!plugin) {
      setActivePluginId(null)
      setActivePluginContextPath(null)
      return
    }
    setActivePluginId(plugin.id)
    setActivePluginContextPath(`/plugins/${encodeURIComponent(plugin.id)}`)
  }, [location.pathname])

  useEffect(() => {
    if (!activePluginId) return
    const currentPath = `${location.pathname}${location.search}${location.hash}` || '/'
    const activePresentation =
      presentationByPluginId[activePluginId] ??
      getBuiltinPluginById(activePluginId)?.presentation.default ??
      null
    if (!activePluginContextPath) return
    if (currentPath === activePluginContextPath) return
    if (activePresentation === 'route') {
      setActivePluginId(null)
      setActivePluginContextPath(null)
      return
    }
    setActivePluginId(null)
    setActivePluginContextPath(null)
  }, [
    location.hash,
    location.pathname,
    location.search,
  ])

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

  const deactivateActivePlugin = useCallback(() => {
    setActivePluginId(null)
    setActivePluginContextPath(null)
  }, [])

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
      setActivePluginContextPath(
        nextPresentation === 'route'
          ? `/plugins/${encodeURIComponent(pluginId)}`
          : (lastWorkspacePathRef.current || '/'),
      )
      setPresentationByPluginId(nextPresentationMap)
      writePluginRuntimeState({
        lastPluginId: pluginId,
        presentationByPluginId: nextPresentationMap,
      })
      if (nextPresentation === 'route') {
        navigate(`/plugins/${encodeURIComponent(pluginId)}`)
        return
      }
      navigate(lastWorkspacePathRef.current || '/')
    },
    [navigate, presentationByPluginId],
  )

  const value = useMemo<PluginRuntimeContextValue>(
    () => ({
      activePluginId,
      activePlugin: activePluginId ? getBuiltinPluginById(activePluginId) : null,
      activePluginPresentation: activePluginId
        ? (presentationByPluginId[activePluginId] ??
          getBuiltinPluginById(activePluginId)?.presentation.default ??
          null)
        : null,
      getPresentationForPlugin: (pluginId) =>
        presentationByPluginId[pluginId] ??
        getBuiltinPluginById(pluginId)?.presentation.default ??
        null,
      openPlugin,
      setPresentationForPlugin,
      deactivateActivePlugin,
    }),
    [
      activePluginId,
      deactivateActivePlugin,
      openPlugin,
      presentationByPluginId,
      setPresentationForPlugin,
    ],
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
