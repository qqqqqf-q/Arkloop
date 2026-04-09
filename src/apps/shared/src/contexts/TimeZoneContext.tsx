import { createContext, useContext, useMemo, type ReactNode } from 'react'
import {
  detectDeviceTimeZone,
  getActiveTimeZone,
  resolveTimeZone,
  setActiveTimeZone,
} from '../timezone'

type TimeZoneContextValue = {
  timeZone: string
  userTimeZone: string | null
  accountTimeZone: string | null
  detectedTimeZone: string
}

const TimeZoneContext = createContext<TimeZoneContextValue | null>(null)

export function TimeZoneProvider({
  children,
  userTimeZone,
  accountTimeZone,
}: {
  children: ReactNode
  userTimeZone?: string | null
  accountTimeZone?: string | null
}) {
  const detectedTimeZone = detectDeviceTimeZone()
  const timeZone = resolveTimeZone({
    userTimeZone,
    accountTimeZone,
    fallbackTimeZone: detectedTimeZone,
  })

  setActiveTimeZone(timeZone)

  const value = useMemo<TimeZoneContextValue>(() => ({
    timeZone,
    userTimeZone: userTimeZone ?? null,
    accountTimeZone: accountTimeZone ?? null,
    detectedTimeZone,
  }), [accountTimeZone, detectedTimeZone, timeZone, userTimeZone])

  return (
    <TimeZoneContext.Provider value={value}>
      {children}
    </TimeZoneContext.Provider>
  )
}

export function useTimeZone(): TimeZoneContextValue {
  const ctx = useContext(TimeZoneContext)
  if (ctx) return ctx
  return {
    timeZone: getActiveTimeZone(),
    userTimeZone: null,
    accountTimeZone: null,
    detectedTimeZone: detectDeviceTimeZone(),
  }
}
