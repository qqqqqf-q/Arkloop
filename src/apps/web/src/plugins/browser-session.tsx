import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'

import { useBrowserTabs } from '../contexts/browser-tabs'
import {
  readPluginBrowserSessionMap,
  writePluginBrowserSessionMap,
} from '../storage'

type PluginBrowserSessionContextValue = {
  ensureBrowserSession: (pluginId: string) => Promise<string | null>
  getBrowserTabIdForPlugin: (pluginId: string) => string | null
}

const PluginBrowserSessionContext =
  createContext<PluginBrowserSessionContextValue | null>(null)

export function PluginBrowserSessionProvider({
  children,
}: {
  children: ReactNode
}) {
  const { createBrowserTab, activateBrowserTab, openBrowserPanel } =
    useBrowserTabs()
  const [sessions, setSessions] = useState(readPluginBrowserSessionMap)
  const sessionsRef = useRef(sessions)
  const pendingSessionsRef = useRef<Record<string, Promise<string | null>>>({})

  const ensureBrowserSession = useCallback(
    async (pluginId: string) => {
      const existingTabId = sessionsRef.current[pluginId]
      if (existingTabId) {
        openBrowserPanel()
        activateBrowserTab(existingTabId)
        return existingTabId
      }

      const pending = pendingSessionsRef.current[pluginId]
      if (pending) {
        const pendingTabId = await pending
        if (pendingTabId) {
          openBrowserPanel()
          activateBrowserTab(pendingTabId)
        }
        return pendingTabId
      }

      const creation = (async () => {
        const newTabId = await createBrowserTab()
        if (!newTabId) return null

        const next = { ...sessionsRef.current, [pluginId]: newTabId }
        sessionsRef.current = next
        setSessions(next)
        writePluginBrowserSessionMap(next)
        return newTabId
      })()

      pendingSessionsRef.current[pluginId] = creation

      try {
        const newTabId = await creation
        if (newTabId) {
          openBrowserPanel()
          activateBrowserTab(newTabId)
        }
        return newTabId
      } finally {
        delete pendingSessionsRef.current[pluginId]
      }
    },
    [activateBrowserTab, createBrowserTab, openBrowserPanel],
  )

  const value = useMemo<PluginBrowserSessionContextValue>(
    () => ({
      ensureBrowserSession,
      getBrowserTabIdForPlugin: (pluginId) => sessions[pluginId] ?? null,
    }),
    [ensureBrowserSession, sessions],
  )

  return (
    <PluginBrowserSessionContext.Provider value={value}>
      {children}
    </PluginBrowserSessionContext.Provider>
  )
}

export function usePluginBrowserSession(): PluginBrowserSessionContextValue {
  const value = useContext(PluginBrowserSessionContext)
  if (!value) {
    throw new Error(
      'usePluginBrowserSession must be used within PluginBrowserSessionProvider',
    )
  }
  return value
}
