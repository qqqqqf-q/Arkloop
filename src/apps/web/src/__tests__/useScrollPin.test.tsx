import { act, useEffect, useLayoutEffect } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { useScrollPin, type ScrollPinResult } from '../hooks/useScrollPin'

type HarnessProps = {
  metrics: {
    clientHeight: number
    scrollHeight: number
    turnHeight: number
    turnOffset: number
    bottomOffset: number
  }
  messages: unknown[]
  liveRunUiVisible?: boolean
  onReady: (api: ScrollPinResult) => void
  onScrollIntoView?: (behavior: ScrollBehavior | undefined) => void
  onContainerScrollTo?: (behavior: ScrollBehavior | undefined, top: number) => void
}

function rect(top: number, height: number): DOMRect {
  return {
    x: 0,
    y: top,
    top,
    bottom: top + height,
    left: 0,
    right: 800,
    width: 800,
    height,
    toJSON: () => ({}),
  } as DOMRect
}

function ScrollPinHarness({
  metrics,
  messages,
  liveRunUiVisible = false,
  onReady,
  onScrollIntoView,
  onContainerScrollTo,
}: HarnessProps) {
  const api = useScrollPin({
    messages,
    liveRunUiVisible,
  })

  useEffect(() => {
    onReady(api)
  }, [api, onReady])

  useLayoutEffect(() => {
    const container = api.scrollContainerRef.current
    const turn = api.lastUserMsgRef.current
    const bottom = api.bottomRef.current
    if (!container || !turn || !bottom) return

    Object.defineProperty(container, 'clientHeight', {
      configurable: true,
      get: () => metrics.clientHeight,
    })
    Object.defineProperty(container, 'scrollHeight', {
      configurable: true,
      get: () => metrics.scrollHeight,
    })
    Object.defineProperty(bottom, 'offsetTop', {
      configurable: true,
      get: () => metrics.bottomOffset,
    })

    container.getBoundingClientRect = () => rect(0, metrics.clientHeight)
    turn.getBoundingClientRect = () => rect(metrics.turnOffset - container.scrollTop, metrics.turnHeight)
    container.scrollTo = ((arg1?: number | ScrollToOptions) => {
      if (typeof arg1 === 'number') {
        onContainerScrollTo?.(undefined, arg1)
        container.scrollTop = arg1
        return
      }
      onContainerScrollTo?.(arg1?.behavior, arg1?.top ?? 0)
      container.scrollTop = arg1?.top ?? 0
    }) as typeof container.scrollTo
    bottom.scrollIntoView = ((arg?: boolean | ScrollIntoViewOptions) => {
      const behavior = typeof arg === 'object' && arg != null ? arg.behavior : undefined
      onScrollIntoView?.(behavior)
      container.scrollTop = Math.max(0, metrics.scrollHeight - metrics.clientHeight)
    }) as typeof bottom.scrollIntoView
  }, [api, metrics, onContainerScrollTo, onScrollIntoView])

  return (
    <>
      <div ref={api.scrollContainerRef}>
        <div ref={api.lastUserMsgRef}>user</div>
        <div ref={api.bottomRef}>bottom</div>
      </div>
      <div ref={api.inputAreaRef} />
      <div ref={api.copCodeExecScrollRef} />
      <div ref={api.spacerRef} />
    </>
  )
}

function flushAnimationFrame(): Promise<void> {
  return new Promise((resolve) => {
    requestAnimationFrame(() => resolve())
  })
}

async function flushAnimationFrames(count: number): Promise<void> {
  for (let i = 0; i < count; i += 1) {
    await flushAnimationFrame()
  }
}

type ResizeObserverRecord = {
  callback: ResizeObserverCallback
  elements: Set<Element>
}

const resizeObserverRecords: ResizeObserverRecord[] = []

function triggerResize(target: Element) {
  for (const record of resizeObserverRecords) {
    if (!record.elements.has(target)) continue
    record.callback([{ target } as ResizeObserverEntry], {} as ResizeObserver)
  }
}

describe('useScrollPin', () => {
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT
  const originalResizeObserver = globalThis.ResizeObserver

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    resizeObserverRecords.length = 0
    globalThis.ResizeObserver = class ResizeObserverMock {
      private readonly record: ResizeObserverRecord

      constructor(callback: ResizeObserverCallback) {
        this.record = { callback, elements: new Set<Element>() }
        resizeObserverRecords.push(this.record)
      }

      observe = (element: Element) => {
        this.record.elements.add(element)
      }

      unobserve = (element: Element) => {
        this.record.elements.delete(element)
      }

      disconnect = () => {
        this.record.elements.clear()
      }
    } as typeof ResizeObserver
  })

  afterEach(() => {
    document.body.innerHTML = ''
    resizeObserverRecords.length = 0
    if (originalResizeObserver === undefined) {
      Reflect.deleteProperty(globalThis, 'ResizeObserver')
    } else {
      globalThis.ResizeObserver = originalResizeObserver
    }
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  function requireApi(api: ScrollPinResult | null): ScrollPinResult {
    if (!api?.scrollContainerRef.current) {
      throw new Error('scroll container missing')
    }
    return api
  }

  function requireContainer(api: ScrollPinResult): HTMLDivElement {
    const container = api.scrollContainerRef.current
    if (!container) {
      throw new Error('scroll container missing')
    }
    return container
  }

  it('发送后应固定在用户消息顶部而不是跟随到底部', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    let api: ScrollPinResult | null = null
    let anchorScrollBehavior: ScrollBehavior | undefined
    const metrics = {
      clientHeight: 400,
      scrollHeight: 1400,
      turnHeight: 120,
      turnOffset: 600,
      bottomOffset: 1400,
    }

    await act(async () => {
      root.render(
        <ScrollPinHarness
          metrics={metrics}
          messages={[{ id: 'user-1' }]}
          onReady={(value) => { api = value }}
          onContainerScrollTo={(behavior) => { anchorScrollBehavior = behavior }}
        />,
      )
    })

    const readyApi = requireApi(api)
    const scrollContainer = requireContainer(readyApi)

    act(() => {
      readyApi.activateAnchor()
    })
    await act(async () => {
      await flushAnimationFrames(3)
    })

    expect(anchorScrollBehavior).toBe('smooth')
    expect(scrollContainer.scrollTop).toBe(552)
    expect(readyApi.isAtBottomRef.current).toBe(false)

    await act(async () => {
      metrics.scrollHeight = 1900
      metrics.turnHeight = 860
      metrics.bottomOffset = 1900
      root.render(
        <ScrollPinHarness
          metrics={metrics}
          messages={[{ id: 'user-1' }, { id: 'assistant-live' }]}
          liveRunUiVisible
          onReady={(value) => { api = value }}
        />,
      )
      await flushAnimationFrames(2)
    })

    expect(scrollContainer.scrollTop).toBe(552)
    expect(readyApi.isAtBottomRef.current).toBe(false)

    act(() => {
      root.unmount()
    })
  })

  it('点击向下箭头后应重新进入持续跟随', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    let api: ScrollPinResult | null = null
    let lastScrollBehavior: ScrollBehavior | undefined
    const metrics = {
      clientHeight: 400,
      scrollHeight: 1400,
      turnHeight: 120,
      turnOffset: 600,
      bottomOffset: 1400,
    }

    await act(async () => {
      root.render(
        <ScrollPinHarness
          metrics={metrics}
          messages={[{ id: 'user-1' }]}
          liveRunUiVisible
          onReady={(value) => { api = value }}
          onScrollIntoView={(behavior) => { lastScrollBehavior = behavior }}
        />,
      )
    })

    const readyApi = requireApi(api)
    const scrollContainer = requireContainer(readyApi)

    act(() => {
      readyApi.activateAnchor()
    })
    await act(async () => {
      await flushAnimationFrames(15)
    })

    act(() => {
      readyApi.scrollToBottom()
    })
    await act(async () => {
      await flushAnimationFrames(2)
    })

    expect(lastScrollBehavior).toBe('instant')
    expect(scrollContainer.scrollTop).toBe(1000)
    expect(readyApi.isAtBottomRef.current).toBe(true)

    await act(async () => {
      metrics.scrollHeight = 2200
      metrics.turnHeight = 980
      metrics.bottomOffset = 2200
      root.render(
        <ScrollPinHarness
          metrics={metrics}
          messages={[{ id: 'user-1' }, { id: 'assistant-live-1' }, { id: 'assistant-live-2' }]}
          liveRunUiVisible
          onReady={(value) => { api = value }}
        />,
      )
      await flushAnimationFrames(2)
    })

    expect(scrollContainer.scrollTop).toBe(1800)
    expect(readyApi.isAtBottomRef.current).toBe(true)

    await act(async () => {
      metrics.scrollHeight = 2480
      metrics.turnHeight = 1260
      metrics.bottomOffset = 2480
      const observedTurn = readyApi.lastUserMsgRef.current
      if (!observedTurn) {
        throw new Error('last turn missing')
      }
      triggerResize(observedTurn)
      await flushAnimationFrames(2)
    })

    expect(scrollContainer.scrollTop).toBe(2080)
    expect(readyApi.isAtBottomRef.current).toBe(true)

    act(() => {
      root.unmount()
    })
  })
})
