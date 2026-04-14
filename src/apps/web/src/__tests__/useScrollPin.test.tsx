import { act, useEffect, useLayoutEffect, useRef } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { useScrollPin, type ScrollPinResult } from '../hooks/useScrollPin'

type HarnessMetrics = {
  clientHeight: number
  scrollHeight: number
  turnHeight: number
  turnOffset: number
  bottomOffset: number
  leadingHeight?: number
  leadingOffset?: number
  headerHeight?: number
  headerOffset?: number
  collapsibleHeight?: number
  collapsibleOffset?: number
  anchorHeight?: number
  anchorOffset?: number
  tailHeight?: number
  tailOffset?: number
}

type HarnessProps = {
  metrics: HarnessMetrics
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
  const {
    bottomRef,
    copCodeExecScrollRef,
    handleScrollContainerScroll,
    inputAreaRef,
    lastUserMsgRef,
    lastUserPromptRef,
    scrollContainerRef,
    spacerRef,
  } = api
  const metricsKey = JSON.stringify(metrics)
  const contentRootRef = useRef<HTMLDivElement>(null)
  const leadingRef = useRef<HTMLDivElement>(null)
  const headerRef = useRef<HTMLDivElement>(null)
  const collapsibleRef = useRef<HTMLDivElement>(null)
  const anchorRef = useRef<HTMLDivElement>(null)
  const tailRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    lastUserPromptRef.current = headerRef.current
  })

  useEffect(() => {
    onReady(api)
  }, [api, onReady])

  useLayoutEffect(() => {
    const container = scrollContainerRef.current
    const contentRoot = contentRootRef.current
    const leading = leadingRef.current
    const turn = lastUserMsgRef.current
    const header = headerRef.current
    const collapsible = collapsibleRef.current
    const anchor = anchorRef.current
    const tail = tailRef.current
    const bottom = bottomRef.current
    if (!container || !contentRoot || !leading || !turn || !header || !collapsible || !anchor || !tail || !bottom) return

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

    const applyRect = (element: HTMLElement, getOffset: () => number, getHeight: () => number) => {
      Object.defineProperty(element, 'getBoundingClientRect', {
        configurable: true,
        value: () => rect(getOffset() - container.scrollTop, getHeight()),
      })
    }

    Object.defineProperty(container, 'getBoundingClientRect', {
      configurable: true,
      value: () => rect(0, metrics.clientHeight),
    })
    Object.defineProperty(contentRoot, 'getBoundingClientRect', {
      configurable: true,
      value: () => rect(0, metrics.scrollHeight),
    })
    applyRect(leading, () => metrics.leadingOffset ?? 120, () => metrics.leadingHeight ?? 260)
    applyRect(turn, () => metrics.turnOffset, () => metrics.turnHeight)
    applyRect(header, () => metrics.headerOffset ?? metrics.turnOffset, () => metrics.headerHeight ?? 64)
    applyRect(collapsible, () => metrics.collapsibleOffset ?? (metrics.turnOffset + 84), () => metrics.collapsibleHeight ?? 220)
    applyRect(anchor, () => metrics.anchorOffset ?? (metrics.turnOffset + 340), () => metrics.anchorHeight ?? 120)
    applyRect(tail, () => metrics.tailOffset ?? (metrics.turnOffset + metrics.turnHeight - 80), () => metrics.tailHeight ?? 80)
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

    const hitOrder = [anchor, collapsible, header, turn, leading, contentRoot, container]
    document.elementFromPoint = ((x: number, y: number) => {
      for (const element of hitOrder) {
        const elementRect = element.getBoundingClientRect()
        if (x < elementRect.left || x > elementRect.right || y < elementRect.top || y > elementRect.bottom) {
          continue
        }
        return element
      }
      return null
    }) as typeof document.elementFromPoint
  }, [bottomRef, lastUserMsgRef, metricsKey, onContainerScrollTo, onScrollIntoView, scrollContainerRef])

  return (
    <>
      <div ref={scrollContainerRef} onScroll={handleScrollContainerScroll}>
        <div ref={contentRootRef}>
          <div ref={leadingRef} className="group/turn" data-testid="leading-turn">leading</div>
          <div ref={lastUserMsgRef} className="group/turn" data-testid="last-turn">
            <div ref={(node) => {
              headerRef.current = node
            }} data-testid="turn-header">header</div>
            <div ref={collapsibleRef} data-testid="turn-collapsible">collapsible</div>
            <div ref={anchorRef} data-testid="turn-anchor">anchor</div>
            <div ref={tailRef} data-testid="turn-tail">tail</div>
          </div>
          <div ref={bottomRef}>bottom</div>
        </div>
      </div>
      <div ref={inputAreaRef} />
      <div ref={copCodeExecScrollRef} />
      <div ref={spacerRef} />
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
  const originalElementFromPoint = document.elementFromPoint

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
    document.elementFromPoint = originalElementFromPoint
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

  function requireContentRoot(api: ScrollPinResult): HTMLDivElement {
    const root = api.scrollContainerRef.current?.firstElementChild
    if (!(root instanceof HTMLDivElement)) {
      throw new Error('content root missing')
    }
    return root
  }

  function requireLastTurn(api: ScrollPinResult): HTMLDivElement {
    const turn = api.lastUserMsgRef.current
    if (!(turn instanceof HTMLDivElement)) {
      throw new Error('last turn missing')
    }
    return turn
  }

  function requireLastUserPrompt(api: ScrollPinResult): HTMLDivElement {
    const prompt = api.lastUserPromptRef.current
    if (!(prompt instanceof HTMLDivElement)) {
      throw new Error('last user prompt missing')
    }
    return prompt
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
      turnOffset: 560,
      headerOffset: 600,
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
    const contentRoot = requireContentRoot(readyApi)
    const prompt = requireLastUserPrompt(readyApi)

    act(() => {
      readyApi.activateAnchor()
    })
    await act(async () => {
      await flushAnimationFrames(3)
    })

    expect(anchorScrollBehavior).toBe('smooth')
    expect(scrollContainer.scrollTop).toBe(552)
    expect(readyApi.isAtBottomRef.current).toBe(true)
    expect(prompt.getBoundingClientRect().top).toBe(48)

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
      triggerResize(contentRoot)
      await flushAnimationFrames(2)
    })

    expect(scrollContainer.scrollTop).toBe(552)
    expect(readyApi.isAtBottomRef.current).toBe(true)
    expect(prompt.getBoundingClientRect().top).toBe(48)

    act(() => {
      root.unmount()
    })
  })

  it('发送后进入锚定时，不应被旧的视角保持重新往下带走', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    let api: ScrollPinResult | null = null
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
          messages={[]}
          onReady={(value) => { api = value }}
        />,
      )
    })

    const readyApi = requireApi(api)
    const scrollContainer = requireContainer(readyApi)

    act(() => {
      scrollContainer.scrollTop = 320
      readyApi.captureViewportAnchor()
      readyApi.activateAnchor()
    })

    await act(async () => {
      root.render(
        <ScrollPinHarness
          metrics={metrics}
          messages={[{ id: 'user-1' }]}
          onReady={(value) => { api = value }}
        />,
      )
      await flushAnimationFrames(3)
    })

    expect(scrollContainer.scrollTop).toBe(552)
    expect(readyApi.isAtBottomRef.current).toBe(true)

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
    expect(readyApi.isAtBottomRef.current).toBe(true)

    act(() => {
      root.unmount()
    })
  })

  it('发送后等待锚定生效前，不应先跟到底部再回弹', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    let api: ScrollPinResult | null = null
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
          messages={[{ id: 'history-1' }]}
          onReady={(value) => { api = value }}
        />,
      )
    })

    const readyApi = requireApi(api)
    const scrollContainer = requireContainer(readyApi)

    act(() => {
      scrollContainer.scrollTop = 1000
      readyApi.syncBottomState(scrollContainer)
      readyApi.activateAnchor()
      metrics.scrollHeight = 1900
      metrics.turnHeight = 860
      metrics.bottomOffset = 1900
      root.render(
        <ScrollPinHarness
          metrics={metrics}
          messages={[{ id: 'history-1' }, { id: 'user-1' }, { id: 'assistant-live' }]}
          liveRunUiVisible
          onReady={(value) => { api = value }}
        />,
      )
    })

    expect(scrollContainer.scrollTop).toBe(1000)

    await act(async () => {
      await flushAnimationFrames(3)
    })

    expect(scrollContainer.scrollTop).toBe(552)
    expect(readyApi.isAtBottomRef.current).toBe(true)

    act(() => {
      root.unmount()
    })
  })

  it('发送后顶部锚定期间，持续输出不应把视角带到底部', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    let api: ScrollPinResult | null = null
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
        />,
      )
    })

    const readyApi = requireApi(api)
    const scrollContainer = requireContainer(readyApi)
    const contentRoot = requireContentRoot(readyApi)
    const observedTurn = requireLastTurn(readyApi)

    act(() => {
      readyApi.activateAnchor()
    })
    await act(async () => {
      await flushAnimationFrames(3)
    })

    expect(scrollContainer.scrollTop).toBe(552)

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
      triggerResize(observedTurn)
      triggerResize(contentRoot)
      await flushAnimationFrames(2)
    })

    expect(scrollContainer.scrollTop).toBe(552)
    expect(readyApi.isAtBottomRef.current).toBe(true)

    act(() => {
      root.unmount()
    })
  })

  it('发送后顶部锚定期间，即使滚动被往下带也应立刻回到用户消息', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    let api: ScrollPinResult | null = null
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
        />,
      )
    })

    const readyApi = requireApi(api)
    const scrollContainer = requireContainer(readyApi)
    const contentRoot = requireContentRoot(readyApi)
    const observedTurn = requireLastTurn(readyApi)

    act(() => {
      readyApi.activateAnchor()
    })
    await act(async () => {
      await flushAnimationFrames(3)
    })

    expect(scrollContainer.scrollTop).toBe(552)

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

    act(() => {
      scrollContainer.scrollTop = 980
      triggerResize(observedTurn)
      triggerResize(contentRoot)
    })

    await act(async () => {
      await flushAnimationFrames(2)
    })

    expect(scrollContainer.scrollTop).toBe(552)
    expect(readyApi.isAtBottomRef.current).toBe(true)

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

    const contentRoot = requireContentRoot(readyApi)
    const observedTurn = readyApi.lastUserMsgRef.current
    if (!observedTurn) {
      throw new Error('last turn missing')
    }
    await act(async () => {
      metrics.scrollHeight = 2660
      metrics.turnHeight = 1380
      metrics.bottomOffset = 2660
      root.render(
        <ScrollPinHarness
          metrics={metrics}
          messages={[{ id: 'user-1' }, { id: 'assistant-completed' }]}
          onReady={(value) => { api = value }}
        />,
      )
      triggerResize(observedTurn)
      triggerResize(contentRoot)
      await flushAnimationFrames(2)
    })

    expect(scrollContainer.scrollTop).toBe(2260)
    expect(readyApi.isAtBottomRef.current).toBe(true)

    await act(async () => {
      metrics.scrollHeight = 2520
      metrics.turnHeight = 1240
      metrics.bottomOffset = 2520
      root.render(
        <ScrollPinHarness
          metrics={metrics}
          messages={[{ id: 'user-1' }, { id: 'assistant-completed' }]}
          onReady={(value) => { api = value }}
        />,
      )
      triggerResize(contentRoot)
      await flushAnimationFrames(2)
    })

    expect(scrollContainer.scrollTop).toBe(2120)
    expect(readyApi.isAtBottomRef.current).toBe(true)

    act(() => {
      root.unmount()
    })
  })

  it('底部跟随时，轻微向上滚动也应立刻退出自动跟随', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    let api: ScrollPinResult | null = null
    const metrics = {
      clientHeight: 400,
      scrollHeight: 1400,
      turnHeight: 980,
      turnOffset: 600,
      bottomOffset: 1400,
    }

    await act(async () => {
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

    const readyApi = requireApi(api)
    const scrollContainer = requireContainer(readyApi)

    act(() => {
      readyApi.scrollToBottom()
    })
    await act(async () => {
      await flushAnimationFrames(2)
    })

    expect(scrollContainer.scrollTop).toBe(1000)
    expect(readyApi.isAtBottomRef.current).toBe(true)

    act(() => {
      scrollContainer.scrollTop = 968
      readyApi.handleScrollContainerScroll()
    })

    expect(readyApi.isAtBottomRef.current).toBe(false)

    await act(async () => {
      metrics.scrollHeight = 1560
      metrics.turnHeight = 1140
      metrics.bottomOffset = 1560
      root.render(
        <ScrollPinHarness
          metrics={metrics}
          messages={[{ id: 'user-1' }, { id: 'assistant-live' }, { id: 'assistant-live-2' }]}
          liveRunUiVisible
          onReady={(value) => { api = value }}
        />,
      )
      await flushAnimationFrames(2)
    })

    expect(scrollContainer.scrollTop).toBe(968)
    expect(readyApi.isAtBottomRef.current).toBe(false)

    act(() => {
      root.unmount()
    })
  })

  it('发送后顶部锚定下，流结束后补上操作区也不应把视角带到底部', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    let api: ScrollPinResult | null = null
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

    expect(scrollContainer.scrollTop).toBe(552)

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
      await flushAnimationFrames(1)
    })

    await act(async () => {
      triggerResize(requireLastTurn(requireApi(api)))
      triggerResize(requireContentRoot(requireApi(api)))
      await flushAnimationFrames(2)
    })

    expect(scrollContainer.scrollTop).toBe(552)

    await act(async () => {
      metrics.scrollHeight = 1988
      metrics.turnHeight = 948
      metrics.bottomOffset = 1988
      root.render(
        <ScrollPinHarness
          metrics={metrics}
          messages={[{ id: 'user-1' }, { id: 'assistant-completed' }]}
          onReady={(value) => { api = value }}
        />,
      )
      await flushAnimationFrames(1)
    })

    await act(async () => {
      triggerResize(requireLastTurn(requireApi(api)))
      triggerResize(requireContentRoot(requireApi(api)))
      await flushAnimationFrames(2)
    })

    expect(scrollContainer.scrollTop).toBe(552)
    expect(requireLastUserPrompt(requireApi(api)).getBoundingClientRect().top).toBe(48)

    act(() => {
      root.unmount()
    })
  })

  it('在最后一段内部阅读时，上方收起不应改变当前阅读位置', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    let api: ScrollPinResult | null = null
    let metrics: HarnessMetrics = {
      clientHeight: 400,
      scrollHeight: 2200,
      turnHeight: 980,
      turnOffset: 600,
      bottomOffset: 2200,
      leadingHeight: 260,
      leadingOffset: 120,
      headerHeight: 64,
      headerOffset: 600,
      collapsibleHeight: 260,
      collapsibleOffset: 700,
      anchorHeight: 120,
      anchorOffset: 1040,
      tailHeight: 80,
      tailOffset: 1500,
    }

    await act(async () => {
      root.render(
        <ScrollPinHarness
          metrics={metrics}
          messages={[{ id: 'user-1' }, { id: 'assistant-live' }]}
          onReady={(value) => { api = value }}
        />,
      )
    })

    const readyApi = requireApi(api)
    const scrollContainer = requireContainer(readyApi)
    act(() => {
      scrollContainer.scrollTop = 880
      readyApi.syncBottomState(scrollContainer)
      readyApi.captureViewportAnchor()
    })
    expect(scrollContainer.scrollTop).toBe(880)

    await act(async () => {
      metrics = {
        ...metrics,
        scrollHeight: 2060,
        turnHeight: 840,
        bottomOffset: 2060,
        collapsibleHeight: 120,
        anchorOffset: 900,
        tailOffset: 1360,
      }
      root.render(
        <ScrollPinHarness
          metrics={metrics}
          messages={[{ id: 'user-1' }, { id: 'assistant-completed' }]}
          onReady={(value) => { api = value }}
        />,
      )
      await flushAnimationFrames(1)
    })

    await act(async () => {
      triggerResize(requireLastTurn(requireApi(api)))
      await flushAnimationFrames(2)
    })

    expect(scrollContainer.scrollTop).toBe(740)

    act(() => {
      root.unmount()
    })
  })
})
