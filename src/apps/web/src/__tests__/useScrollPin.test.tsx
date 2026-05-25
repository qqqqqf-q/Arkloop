import { act, useEffect, useLayoutEffect, useRef } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { useScrollPin, type ScrollPinResult } from '../hooks/useScrollPin'

type HarnessMetrics = {
  clientHeight: number
  scrollHeight: number
  turnHeight: number
  turnOffset: number
  headerOffset?: number
  bottomOffset: number
  inputAreaHeight?: number
}

type HarnessProps = {
  metrics: HarnessMetrics
  messages: unknown[]
  messagesLoading?: boolean
  liveRunUiVisible?: boolean
  promptPinningDisabled?: boolean
  deferSmoothScroll?: boolean
  onReady: (api: ScrollPinResult) => void
  onScrollIntoView?: (behavior: ScrollBehavior | undefined) => void
  onContainerScrollTo?: (behavior: ScrollBehavior | undefined, top: number) => void
}

function rect(top: number, height: number): DOMRect {
  return { x: 0, y: top, top, bottom: top + height, left: 0, right: 800, width: 800, height, toJSON: () => ({}) } as DOMRect
}

function ScrollPinHarness({
  metrics,
  messages,
  messagesLoading = false,
  liveRunUiVisible = false,
  promptPinningDisabled = false,
  deferSmoothScroll = false,
  onReady,
  onScrollIntoView,
  onContainerScrollTo,
}: HarnessProps) {
  const api = useScrollPin({ messages, messagesLoading, liveRunUiVisible, promptPinningDisabled })
  const { bottomRef, handleScrollContainerScroll, inputAreaRef, lastUserMsgRef, lastUserPromptRef, scrollContainerRef, spacerRef } = api
  const metricsKey = JSON.stringify(metrics)
  const headerRef = useRef<HTMLDivElement>(null)

  useEffect(() => { lastUserPromptRef.current = headerRef.current })
  useEffect(() => { onReady(api) }, [api, onReady])

  useLayoutEffect(() => {
    const container = scrollContainerRef.current
    const turn = lastUserMsgRef.current
    const header = headerRef.current
    const bottom = bottomRef.current
    const inputArea = inputAreaRef.current
    const spacer = spacerRef.current
    if (!container || !turn || !header || !bottom || !inputArea || !spacer) return

    const currentScrollHeight = () => metrics.scrollHeight + (Number.parseFloat(spacer.style.height) || 0)

    Object.defineProperty(container, 'clientHeight', { configurable: true, get: () => metrics.clientHeight })
    Object.defineProperty(container, 'scrollHeight', {
      configurable: true,
      get: currentScrollHeight,
    })
    Object.defineProperty(container, 'getBoundingClientRect', {
      configurable: true,
      value: () => rect(0, metrics.clientHeight),
    })

    const applyRect = (el: HTMLElement, getOffset: () => number, getHeight: () => number) => {
      Object.defineProperty(el, 'getBoundingClientRect', {
        configurable: true,
        value: () => rect(getOffset() - container.scrollTop, getHeight()),
      })
    }
    applyRect(turn, () => metrics.turnOffset, () => metrics.turnHeight)
    applyRect(header, () => metrics.headerOffset ?? metrics.turnOffset, () => 64)
    Object.defineProperty(inputArea, 'getBoundingClientRect', {
      configurable: true,
      value: () => rect(0, metrics.inputAreaHeight ?? 160),
    })
    Object.defineProperty(container, 'scrollTo', {
      configurable: true,
      value: ((arg1?: number | ScrollToOptions) => {
        const maxScroll = Math.max(0, currentScrollHeight() - metrics.clientHeight)
        if (typeof arg1 === 'number') {
          onContainerScrollTo?.(undefined, arg1)
          container.scrollTop = Math.min(arg1, maxScroll)
          return
        }
        const top = arg1?.top ?? 0
        onContainerScrollTo?.(arg1?.behavior, top)
        if (deferSmoothScroll && arg1?.behavior === 'smooth') return
        container.scrollTop = Math.min(top, maxScroll)
      }) as typeof container.scrollTo,
    })
    Object.defineProperty(bottom, 'scrollIntoView', {
      configurable: true,
      value: ((arg?: boolean | ScrollIntoViewOptions) => {
        const behavior = typeof arg === 'object' && arg != null ? arg.behavior : undefined
        onScrollIntoView?.(behavior)
        container.scrollTop = Math.max(0, metrics.scrollHeight - metrics.clientHeight)
      }) as typeof bottom.scrollIntoView,
    })
  }, [bottomRef, deferSmoothScroll, lastUserMsgRef, metricsKey, onContainerScrollTo, onScrollIntoView, scrollContainerRef])

  return (
    <>
      <div ref={scrollContainerRef} onScroll={handleScrollContainerScroll}>
        <div ref={lastUserMsgRef}>
          <div ref={(node) => { headerRef.current = node }}>header</div>
        </div>
        <div ref={bottomRef}>bottom</div>
      </div>
      <div ref={inputAreaRef} />
      <div ref={spacerRef} />
    </>
  )
}

function flushAnimationFrame(): Promise<void> {
  return new Promise((resolve) => { requestAnimationFrame(() => resolve()) })
}

async function flushAnimationFrames(count: number): Promise<void> {
  for (let i = 0; i < count; i += 1) await flushAnimationFrame()
}

type ResizeObserverRecord = { callback: ResizeObserverCallback; elements: Set<Element> }
const resizeObserverRecords: ResizeObserverRecord[] = []

function triggerResize(target: Element) {
  for (const record of resizeObserverRecords) {
    if (!record.elements.has(target)) continue
    record.callback([{ target } as ResizeObserverEntry], {} as ResizeObserver)
  }
}

describe('useScrollPin', () => {
  const actEnv = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const origActEnv = actEnv.IS_REACT_ACT_ENVIRONMENT
  const origRO = globalThis.ResizeObserver

  let _root: Root | null = null

  beforeEach(() => {
    actEnv.IS_REACT_ACT_ENVIRONMENT = true
    resizeObserverRecords.length = 0
    globalThis.ResizeObserver = class {
      private readonly rec: ResizeObserverRecord
      constructor(cb: ResizeObserverCallback) {
        this.rec = { callback: cb, elements: new Set() }
        resizeObserverRecords.push(this.rec)
      }
      observe = (el: Element) => { this.rec.elements.add(el) }
      unobserve = (el: Element) => { this.rec.elements.delete(el) }
      disconnect = () => { this.rec.elements.clear() }
    } as typeof ResizeObserver
  })

  afterEach(() => {
    if (_root) { act(() => { _root!.unmount() }); _root = null }
    document.body.innerHTML = ''
    resizeObserverRecords.length = 0
    if (origRO === undefined) Reflect.deleteProperty(globalThis, 'ResizeObserver')
    else globalThis.ResizeObserver = origRO
    if (origActEnv === undefined) delete actEnv.IS_REACT_ACT_ENVIRONMENT
    else actEnv.IS_REACT_ACT_ENVIRONMENT = origActEnv
  })

  async function setup(props: Omit<HarnessProps, 'onReady'> & Partial<Pick<HarnessProps, 'onReady'>>) {
    const apiRef = { current: null as ScrollPinResult | null }
    const el = document.createElement('div')
    document.body.appendChild(el)
    const root = createRoot(el)
    _root = root

    const renderHarness = (overrides?: Partial<HarnessProps>) => {
      root.render(
        <ScrollPinHarness
          {...props}
          {...overrides}
          onReady={(v) => { apiRef.current = v }}
        />,
      )
    }

    await act(async () => {
      renderHarness()
      await flushAnimationFrames(2)
    })

    const api = apiRef.current!
    if (!api?.scrollContainerRef.current) throw new Error('scroll container missing')
    return { api, root, renderHarness, apiRef }
  }

  it('activateAnchor 应 smooth 滚到底部并保持 following', async () => {
    let scrollBehavior: ScrollBehavior | undefined
    const metrics = { clientHeight: 400, scrollHeight: 1400, turnHeight: 120, turnOffset: 560, headerOffset: 600, bottomOffset: 1400 }
    const { api, renderHarness } = await setup({
      metrics,
      messages: [{ id: 'user-1' }],
      onContainerScrollTo: (b) => { scrollBehavior = b },
    })
    const container = api.scrollContainerRef.current!

    act(() => { api.activateAnchor() })
    await act(async () => {
      renderHarness({
        messages: [{ id: 'user-1' }, { id: 'live' }],
        liveRunUiVisible: true,
        onContainerScrollTo: (b) => { scrollBehavior = b },
      })
      await flushAnimationFrames(2)
    })

    expect(scrollBehavior).toBe('smooth')
    expect(container.scrollTop).toBe(1000)
    expect(api.isAtBottomRef.current).toBe(true)
  })

  it('activateAnchor 后流式内容增长应继续跟随到底部', async () => {
    const metrics = { clientHeight: 400, scrollHeight: 1400, turnHeight: 120, turnOffset: 600, headerOffset: 600, bottomOffset: 1400 }
    const { api, renderHarness } = await setup({ metrics, messages: [{ id: 'user-1' }] })
    const container = api.scrollContainerRef.current!

    act(() => { api.activateAnchor() })
    await act(async () => {
      renderHarness({ messages: [{ id: 'user-1' }, { id: 'live' }], liveRunUiVisible: true })
      await flushAnimationFrames(2)
    })
    expect(container.scrollTop).toBe(1000)

    for (let i = 0; i < 5; i++) {
      await act(async () => {
        metrics.scrollHeight += 50
        metrics.turnHeight += 50
        metrics.bottomOffset += 50
        renderHarness({ messages: [{ id: 'user-1' }, { id: `live-${i}` }], liveRunUiVisible: true })
        await flushAnimationFrames(2)
      })
      expect(container.scrollTop).toBe(metrics.scrollHeight - metrics.clientHeight)
    }
    expect(api.isAtBottomRef.current).toBe(true)
  })

  it('新 prompt 下方内容不足时 activateAnchor 仍应滚到自然底部，不补 spacer', async () => {
    const metrics = { clientHeight: 769, scrollHeight: 1009, turnHeight: 86, turnOffset: 702, headerOffset: 702, bottomOffset: 1009 }
    const { api, renderHarness } = await setup({
      metrics,
      messages: [{ id: 'user-1' }],
    })
    const container = api.scrollContainerRef.current!
    const spacer = api.spacerRef.current!

    act(() => { api.activateAnchor() })
    await act(async () => {
      renderHarness({ messages: [{ id: 'user-1' }, { id: 'pending' }] })
      await flushAnimationFrames(2)
    })

    expect(spacer.style.height).toBe('0px')
    expect(container.scrollTop).toBe(240)
  })

  it('scrollToBottom 应回到 following 并 smooth 滚到底部', async () => {
    let lastBehavior: ScrollBehavior | undefined
    const metrics = { clientHeight: 400, scrollHeight: 1400, turnHeight: 120, turnOffset: 600, bottomOffset: 1400 }
    const { api, renderHarness } = await setup({
      metrics,
      messages: [{ id: 'user-1' }],
      liveRunUiVisible: true,
      onContainerScrollTo: (b) => { lastBehavior = b },
    })
    const container = api.scrollContainerRef.current!

    act(() => { api.activateAnchor() })
    await act(async () => { await flushAnimationFrames(3) })

    act(() => { api.scrollToBottom() })
    await act(async () => { await flushAnimationFrames(2) })

    expect(lastBehavior).toBe('smooth')
    expect(container.scrollTop).toBe(1000)
    expect(api.isAtBottomRef.current).toBe(true)

    // following 状态下新内容应 scrollIntoView 到底部
    let scrolledToBottom = false
    await act(async () => {
      renderHarness({
        metrics: { ...metrics, scrollHeight: 1800, bottomOffset: 1800 },
        messages: [{ id: 'user-1' }, { id: 'live-2' }],
        liveRunUiVisible: true,
        onScrollIntoView: () => { scrolledToBottom = true },
      })
      await flushAnimationFrames(2)
    })
    expect(scrolledToBottom).toBe(true)
  })

  it('wheel 向上应从 following 切到 free，停止自动跟随', async () => {
    const metrics = { clientHeight: 400, scrollHeight: 1400, turnHeight: 120, turnOffset: 600, bottomOffset: 1400 }
    const { api, renderHarness } = await setup({ metrics, messages: [{ id: 'user-1' }], liveRunUiVisible: true })
    const container = api.scrollContainerRef.current!

    act(() => { api.scrollToBottom() })
    await act(async () => { await flushAnimationFrames(2) })
    expect(container.scrollTop).toBe(1000)

    act(() => {
      container.dispatchEvent(new WheelEvent('wheel', { deltaY: -100, bubbles: true }))
      container.scrollTop = 800
      api.handleScrollContainerScroll()
    })
    expect(api.isAtBottomRef.current).toBe(false)

    await act(async () => {
      renderHarness({
        metrics: { ...metrics, scrollHeight: 1800, bottomOffset: 1800 },
        messages: [{ id: 'user-1' }, { id: 'live-2' }],
        liveRunUiVisible: true,
      })
      await flushAnimationFrames(2)
    })
    expect(container.scrollTop).toBe(800)
  })

  it('promptPinningDisabled 时 activateAnchor 应滚到底部', async () => {
    let scrollBehavior: ScrollBehavior | undefined
    const metrics = { clientHeight: 400, scrollHeight: 1400, turnHeight: 120, turnOffset: 560, bottomOffset: 1400 }
    const { api } = await setup({
      metrics,
      messages: [{ id: 'user-1' }],
      promptPinningDisabled: true,
      onContainerScrollTo: (b) => { scrollBehavior = b },
    })
    const container = api.scrollContainerRef.current!

    act(() => { api.activateAnchor() })
    await act(async () => { await flushAnimationFrames(2) })

    expect(scrollBehavior).toBe('smooth')
    expect(container.scrollTop).toBe(1000)
    expect(api.isAtBottomRef.current).toBe(true)
  })

  it('输入区变高时 following 状态应更新 CSS 变量', async () => {
    const metrics = { clientHeight: 400, scrollHeight: 1040, turnHeight: 320, turnOffset: 480, bottomOffset: 880, inputAreaHeight: 160 }
    const { api, renderHarness } = await setup({ metrics, messages: [{ id: 'user-1' }] })
    const inputArea = api.inputAreaRef.current!

    await act(async () => {
      metrics.inputAreaHeight = 280
      renderHarness()
      triggerResize(inputArea)
      await flushAnimationFrames(2)
    })

    expect(document.documentElement.style.getPropertyValue('--chat-input-area-height')).toBe('280px')
  })

  it('following 状态下消息内容变高应保持在自然底部', async () => {
    const metrics = { clientHeight: 400, scrollHeight: 1000, turnHeight: 120, turnOffset: 600, bottomOffset: 1000 }
    const { api } = await setup({ metrics, messages: [{ id: 'user-1' }] })
    const container = api.scrollContainerRef.current!
    const turn = api.lastUserMsgRef.current!

    act(() => { api.scrollToBottom() })
    await act(async () => { await flushAnimationFrames(2) })
    expect(container.scrollTop).toBe(600)

    await act(async () => {
      metrics.scrollHeight += 160
      metrics.bottomOffset += 160
      triggerResize(turn)
      await flushAnimationFrames(2)
    })

    expect(container.scrollTop).toBe(760)
    expect(api.isAtBottomRef.current).toBe(true)
  })

  it('subscribeIsAtBottom 应在状态变化时通知监听者', async () => {
    const metrics = { clientHeight: 400, scrollHeight: 1400, turnHeight: 120, turnOffset: 600, bottomOffset: 1400 }
    const { api } = await setup({ metrics, messages: [{ id: 'user-1' }] })
    const container = api.scrollContainerRef.current!

    let notified = false
    const unsub = api.subscribeIsAtBottom(() => { notified = true })

    act(() => {
      container.scrollTop = 200
      api.syncBottomState(container)
    })

    expect(notified).toBe(true)
    expect(api.getIsAtBottomSnapshot()).toBe(false)
    unsub()
  })

  it('历史加载完成后应滚到自然底部，不进入 pinned', async () => {
    const metrics = { clientHeight: 400, scrollHeight: 1400, turnHeight: 300, turnOffset: 600, headerOffset: 600, bottomOffset: 1400 }
    const el = document.createElement('div')
    document.body.appendChild(el)
    const root = createRoot(el)
    _root = root
    let api: ScrollPinResult | null = null

    await act(async () => {
      root.render(
        <ScrollPinHarness
          metrics={metrics}
          messages={[]}
          messagesLoading
          onReady={(v) => { api = v }}
        />,
      )
      await flushAnimationFrames(2)
    })

    await act(async () => {
      root.render(
        <ScrollPinHarness
          metrics={metrics}
          messages={[{ id: 'user-1' }]}
          messagesLoading={false}
          onReady={(v) => { api = v }}
        />,
      )
      await flushAnimationFrames(2)
    })

    const container = api!.scrollContainerRef.current!
    const spacer = api!.spacerRef.current!
    expect(spacer.style.height).toBe('0px')
    expect(container.scrollTop).toBe(1000)
    expect(api!.isAtBottomRef.current).toBe(true)
  })

  it('历史加载完成后自然高度不足以锚定时不补 spacer，滚到自然底部', async () => {
    const metrics = { clientHeight: 769, scrollHeight: 1009, turnHeight: 86, turnOffset: 702, headerOffset: 702, bottomOffset: 1009 }
    const { api, renderHarness } = await setup({
      metrics,
      messages: [],
      messagesLoading: true,
    })

    await act(async () => {
      renderHarness({
        metrics,
        messages: [{ id: 'user-1' }],
        messagesLoading: false,
      })
      await flushAnimationFrames(2)
    })

    const container = api.scrollContainerRef.current!
    const spacer = api.spacerRef.current!
    expect(spacer.style.height).toBe('0px')
    expect(container.scrollTop).toBe(240)
    expect(api.isAtBottomRef.current).toBe(true)
  })
})
