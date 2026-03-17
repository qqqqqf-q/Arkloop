import { useCallback, useEffect, useRef, useState } from 'react'
import { refreshAccessToken, writeAccessToken } from '@arkloop/shared'
import { isLocalMode } from '@arkloop/shared/desktop'
import { createSSEClient, type RunEvent, type SSEClient, type SSEClientState } from '../sse'
import { clearLastSeqInStorage, readLastSeqFromStorage, writeLastSeqToStorage } from '../storage'

export type UseSSEOptions = {
  runId: string
  accessToken: string
  baseUrl?: string
}

export type UseSSEResult = {
  events: RunEvent[]
  state: SSEClientState
  lastSeq: number
  error: Error | null
  connect: () => void
  disconnect: () => void
  reconnect: () => void
  clearEvents: () => void
  reset: () => void
}

/**
 * SSE 订阅 Hook
 * 管理 run 事件流的订阅与状态
 */
export function useSSE(options: UseSSEOptions): UseSSEResult {
  const { runId, accessToken, baseUrl = '' } = options

  const [events, setEvents] = useState<RunEvent[]>([])
  const [state, setState] = useState<SSEClientState>('idle')
  const [lastSeq, setLastSeq] = useState(0)
  const [error, setError] = useState<Error | null>(null)

  const clientRef = useRef<SSEClient | null>(null)
  const seenSeqsRef = useRef<Set<number>>(new Set())
  const cursorRef = useRef(0)
  const connectedRunIdRef = useRef('')

  // 构建 SSE URL
  const normalizedBaseUrl = baseUrl.replace(/\/$/, '')
  const sseUrl = `${normalizedBaseUrl}/v1/runs/${runId}/events`

  const handleEvent = useCallback((event: RunEvent) => {
    // 去重：避免重连时重复展示
    if (seenSeqsRef.current.has(event.seq)) return
    seenSeqsRef.current.add(event.seq)

    setEvents(prev => [...prev, event])

    if (typeof event.seq === 'number' && event.seq >= 0) {
      cursorRef.current = event.seq
      writeLastSeqToStorage(runId, event.seq)
      setLastSeq(event.seq)
    }
  }, [runId])

  const handleStateChange = useCallback((newState: SSEClientState) => {
    setState(newState)
  }, [])

  const handleError = useCallback((err: Error) => {
    setError(err)
  }, [])

  const handleTokenRefresh = useCallback(async (): Promise<string> => {
    if (isLocalMode()) return accessToken
    const resp = await refreshAccessToken()
    writeAccessToken(resp.access_token)
    return resp.access_token
  }, [accessToken])

  const connect = useCallback(() => {
    if (!runId || !accessToken) return

    if (clientRef.current) {
      clientRef.current.close()
    }

    setError(null)

    if (connectedRunIdRef.current !== runId) {
      connectedRunIdRef.current = runId
      cursorRef.current = 0
      setLastSeq(0)
      setEvents([])
      seenSeqsRef.current.clear()
    }

    const stored = readLastSeqFromStorage(runId)
    const nextCursor = Math.max(cursorRef.current, stored)
    cursorRef.current = nextCursor
    setLastSeq(nextCursor)

    const client = createSSEClient({
      url: sseUrl,
      accessToken,
      afterSeq: cursorRef.current,
      follow: true,
      onEvent: handleEvent,
      onStateChange: handleStateChange,
      onError: handleError,
      onTokenRefresh: handleTokenRefresh,
    })

    clientRef.current = client
    void client.connect()
  }, [sseUrl, accessToken, runId, handleEvent, handleStateChange, handleError, handleTokenRefresh])

  const disconnect = useCallback(() => {
    clientRef.current?.close()
    clientRef.current = null
  }, [])

  const reconnect = useCallback(() => {
    setError(null)
    void clientRef.current?.reconnect()
  }, [])

  const clearEvents = useCallback(() => {
    setEvents([])
  }, [])

  const reset = useCallback(() => {
    clearLastSeqInStorage(runId)
    cursorRef.current = 0
    setLastSeq(0)
    setEvents([])
    seenSeqsRef.current.clear()
    setError(null)
    setState('idle')
  }, [runId])

  // 组件卸载时断开连接
  useEffect(() => {
    return () => {
      disconnect()
    }
  }, [disconnect])

  return {
    events,
    state,
    lastSeq,
    error,
    connect,
    disconnect,
    reconnect,
    clearEvents,
    reset,
  }
}
