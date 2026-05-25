import { useCallback, useEffect, useLayoutEffect, useRef } from 'react'
import type { AssistantTurnUi } from '../assistantTurnSegments'

export const SCROLL_BOTTOM_PAD = 160

const PROMPT_PIN_TOP_OFFSET = 160
const AT_BOTTOM_THRESHOLD = 80

type ScrollState = 'following' | 'pinned' | 'free'

const DEBUG = false
function dbg(_tag: string, _extra = '') {
  if (!DEBUG) return
}

interface UseScrollPinOptions {
  messagesLoading?: boolean
  messages?: readonly unknown[]
  liveAssistantTurn?: AssistantTurnUi | null
  liveRunUiVisible?: boolean
  promptPinningDisabled?: boolean
}

export interface ScrollPinResult {
  bottomRef: React.RefObject<HTMLDivElement | null>
  scrollContainerRef: React.RefObject<HTMLDivElement | null>
  lastUserMsgRef: React.RefObject<HTMLDivElement | null>
  lastUserPromptRef: React.RefObject<HTMLDivElement | null>
  inputAreaRef: React.RefObject<HTMLDivElement | null>
  spacerRef: React.RefObject<HTMLDivElement | null>
  isAtBottomRef: React.RefObject<boolean>
  handleScrollContainerScroll: () => void
  scrollToBottom: () => void
  activateAnchor: () => void
  syncBottomState: (el: HTMLDivElement) => void
  stabilizeDocumentPanelScroll: (trigger?: HTMLElement | null) => void
  subscribeIsAtBottom: (listener: () => void) => () => void
  getIsAtBottomSnapshot: () => boolean
}

export function useScrollPin(options: UseScrollPinOptions = {}): ScrollPinResult {
  const {
    messagesLoading = false,
    messages = [],
    liveAssistantTurn = null,
    liveRunUiVisible = false,
    promptPinningDisabled = false,
  } = options

  const bottomRef = useRef<HTMLDivElement>(null)
  const scrollContainerRef = useRef<HTMLDivElement>(null)
  const lastUserMsgRef = useRef<HTMLDivElement>(null)
  const lastUserPromptRef = useRef<HTMLDivElement>(null)
  const inputAreaRef = useRef<HTMLDivElement>(null)
  const spacerRef = useRef<HTMLDivElement>(null)

  const isAtBottomRef = useRef(true)
  const listenersRef = useRef(new Set<() => void>())
  const stateRef = useRef<ScrollState>('following')
  const pinnedNeedsInitialScrollRef = useRef(false)
  const wasLoadingRef = useRef(false)
  const stabilizeFrameRef = useRef<number | null>(null)

  const inputAreaHeight = useCallback(() => {
    const el = inputAreaRef.current
    if (!el) return SCROLL_BOTTOM_PAD
    const h = el.getBoundingClientRect().height
    return Number.isFinite(h) && h > 0 ? h : SCROLL_BOTTOM_PAD
  }, [])

  const setAtBottomState = useCallback((v: boolean) => {
    if (isAtBottomRef.current === v) return
    isAtBottomRef.current = v
    for (const fn of listenersRef.current) fn()
  }, [])

  const subscribeIsAtBottom = useCallback((listener: () => void) => {
    listenersRef.current.add(listener)
    return () => { listenersRef.current.delete(listener) }
  }, [])

  const getIsAtBottomSnapshot = useCallback(() => isAtBottomRef.current, [])

  const isPhysicallyAtBottom = useCallback((el: HTMLDivElement) => {
    return el.scrollHeight - el.scrollTop - el.clientHeight <= AT_BOTTOM_THRESHOLD
  }, [])

  const collapseSpacer = useCallback(() => {
    if (spacerRef.current) spacerRef.current.style.height = '0px'
  }, [])

  const syncBottomState = useCallback((el: HTMLDivElement) => {
    const atPhysicalBottom = isPhysicallyAtBottom(el)
    if (stateRef.current === 'pinned') {
      setAtBottomState(true)
    } else {
      setAtBottomState(atPhysicalBottom)
    }
  }, [isPhysicallyAtBottom, setAtBottomState])

  const recalcSpacer = useCallback(() => {
    const spacer = spacerRef.current
    const container = scrollContainerRef.current
    const prompt = lastUserPromptRef.current ?? lastUserMsgRef.current
    if (!spacer || !container) return
    if (stateRef.current !== 'pinned' || !prompt) {
      spacer.style.height = '0px'
      return
    }
    const spacerH = Number.parseFloat(spacer.style.height) || 0
    const naturalScrollHeight = container.scrollHeight - spacerH
    const promptTop = prompt.getBoundingClientRect().top - container.getBoundingClientRect().top + container.scrollTop
    const desiredScrollTop = Math.max(0, promptTop - PROMPT_PIN_TOP_OFFSET)
    const needed = Math.max(0, desiredScrollTop + container.clientHeight - naturalScrollHeight)
    spacer.style.height = needed + 'px'
  }, [])

  const scrollToAnchorInstant = useCallback(() => {
    const container = scrollContainerRef.current
    const prompt = lastUserPromptRef.current ?? lastUserMsgRef.current
    if (!container || !prompt) return
    const promptTop = prompt.getBoundingClientRect().top - container.getBoundingClientRect().top + container.scrollTop
    const target = Math.max(0, promptTop - PROMPT_PIN_TOP_OFFSET)
    const maxScrollTop = Math.max(0, container.scrollHeight - container.clientHeight)
    container.scrollTop = Math.min(target, maxScrollTop)
  }, [])

  // --- scroll handler: only updates isAtBottom for button UI ---
  // 不做任何状态转换。free→following 只通过用户显式操作（点按钮、发消息）。
  const handleScrollContainerScroll = useCallback(() => {
    const el = scrollContainerRef.current
    if (!el) return
    syncBottomState(el)
  }, [syncBottomState])

  // --- user intent detection: wheel/touch, NOT scroll event ---
  useEffect(() => {
    const container = scrollContainerRef.current
    if (!container) return

    const onWheel = (e: WheelEvent) => {
      if (e.deltaY >= 0) return
      if (stateRef.current === 'free') return
      dbg(`wheel→free dy=${e.deltaY}`)
      stateRef.current = 'free'
      const el = scrollContainerRef.current
      if (el) syncBottomState(el)
    }

    const onTouchStart = () => {
      if (stateRef.current === 'free') return
      dbg(`touch→free state=${stateRef.current}`)
      stateRef.current = 'free'
    }

    container.addEventListener('wheel', onWheel, { passive: true })
    container.addEventListener('touchstart', onTouchStart, { passive: true })
    return () => {
      container.removeEventListener('wheel', onWheel)
      container.removeEventListener('touchstart', onTouchStart)
    }
  }, [collapseSpacer, syncBottomState])

  const activateAnchor = useCallback(() => {
    dbg('activateAnchor', `pinDisabled=${promptPinningDisabled} prompt=${!!lastUserPromptRef.current} msg=${!!lastUserMsgRef.current}`)
    stateRef.current = 'following'
    pinnedNeedsInitialScrollRef.current = false
    collapseSpacer()
    setAtBottomState(true)
    scrollContainerRef.current?.scrollTo({ top: scrollContainerRef.current.scrollHeight, behavior: 'smooth' })
  }, [setAtBottomState, collapseSpacer])

  const scrollToBottom = useCallback(() => {
    dbg('scrollToBottom')
    stateRef.current = 'following'
    collapseSpacer()
    setAtBottomState(true)
    scrollContainerRef.current?.scrollTo({ top: scrollContainerRef.current.scrollHeight, behavior: 'smooth' })
  }, [setAtBottomState, collapseSpacer])

  const stabilizeDocumentPanelScroll = useCallback((trigger?: HTMLElement | null) => {
    const container = scrollContainerRef.current
    if (!container) return

    if (stabilizeFrameRef.current !== null) {
      cancelAnimationFrame(stabilizeFrameRef.current)
      stabilizeFrameRef.current = null
    }

    const anchor = trigger && container.contains(trigger) ? trigger : null
    const anchorTop = anchor
      ? anchor.getBoundingClientRect().top - container.getBoundingClientRect().top
      : null
    const distFromBottom = container.scrollHeight - container.scrollTop - container.clientHeight
    const startedAt = performance.now()

    const step = () => {
      const c = scrollContainerRef.current
      if (!c) return

      if (anchor && anchorTop !== null && anchor.isConnected && c.contains(anchor)) {
        const nextTop = anchor.getBoundingClientRect().top - c.getBoundingClientRect().top
        c.scrollTop += nextTop - anchorTop
      } else {
        c.scrollTop = Math.max(0, c.scrollHeight - c.clientHeight - distFromBottom)
      }

      syncBottomState(c)

      if (performance.now() - startedAt < 360) {
        stabilizeFrameRef.current = requestAnimationFrame(step)
        return
      }
      stabilizeFrameRef.current = null
    }

    stabilizeFrameRef.current = requestAnimationFrame(step)
  }, [syncBottomState])

  // scroll-margin-top for scrollIntoView positioning
  useLayoutEffect(() => {
    const prompt = lastUserPromptRef.current
    if (prompt) prompt.style.scrollMarginTop = PROMPT_PIN_TOP_OFFSET + 'px'
    const turn = lastUserMsgRef.current
    if (turn) turn.style.scrollMarginTop = PROMPT_PIN_TOP_OFFSET + 'px'
  })

  // streaming auto-follow
  useEffect(() => {
    const isLive = liveAssistantTurn != null || liveRunUiVisible

    dbg('streamEffect', `state=${stateRef.current} needsInit=${pinnedNeedsInitialScrollRef.current} prompt=${!!(lastUserPromptRef.current ?? lastUserMsgRef.current)} msgs=${messages.length} live=${liveAssistantTurn != null} runUi=${liveRunUiVisible}`)

    if (!isLive && stateRef.current !== 'pinned') {
      collapseSpacer()
    }

    if (stateRef.current === 'following') {
      bottomRef.current?.scrollIntoView({ behavior: 'instant' })
    } else if (stateRef.current === 'pinned') {
      const prompt = lastUserPromptRef.current ?? lastUserMsgRef.current
      if (pinnedNeedsInitialScrollRef.current && prompt) {
        pinnedNeedsInitialScrollRef.current = false
        recalcSpacer()
        dbg('streamEffect:initialScroll')
        const container = scrollContainerRef.current
        if (container) {
          const promptTop = prompt.getBoundingClientRect().top - container.getBoundingClientRect().top + container.scrollTop
          container.scrollTo({ top: Math.max(0, promptTop - PROMPT_PIN_TOP_OFFSET), behavior: 'smooth' })
        }
      }
    }
  }, [messages, liveAssistantTurn, liveRunUiVisible, recalcSpacer, collapseSpacer])

  // history load complete
  useEffect(() => {
    if (messagesLoading) {
      dbg('historyLoad:loading')
      wasLoadingRef.current = true
      stateRef.current = 'following'
      collapseSpacer()
      return
    }
    if (!wasLoadingRef.current) return
    wasLoadingRef.current = false
    dbg('historyLoad:complete', `turn=${!!lastUserMsgRef.current} container=${!!scrollContainerRef.current}`)

    const container = scrollContainerRef.current
    if (!container) return

    stateRef.current = 'following'
    collapseSpacer()
    container.scrollTop = container.scrollHeight - container.clientHeight
    syncBottomState(container)
  }, [messagesLoading, syncBottomState, collapseSpacer])

  // promptPinningDisabled changed
  useEffect(() => {
    if (!promptPinningDisabled) return
    stateRef.current = 'following'
    collapseSpacer()
  }, [promptPinningDisabled, collapseSpacer])

  // input area resize observer
  useEffect(() => {
    const el = inputAreaRef.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const sync = () => {
      document.documentElement.style.setProperty('--chat-input-area-height', `${Math.ceil(inputAreaHeight())}px`)
    }
    sync()
    const ro = new ResizeObserver(() => {
      sync()
      if (stateRef.current === 'following') {
        bottomRef.current?.scrollIntoView({ behavior: 'instant' })
      } else if (stateRef.current === 'pinned') {
        recalcSpacer()
      }
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [inputAreaHeight, recalcSpacer])

  // Message hydration can change rendered height after history loading finishes.
  useEffect(() => {
    const container = scrollContainerRef.current
    if (!container || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(() => {
      if (stateRef.current === 'following') {
        bottomRef.current?.scrollIntoView({ behavior: 'instant' })
      } else if (stateRef.current === 'pinned') {
        recalcSpacer()
      }
    })
    for (const child of Array.from(container.children)) {
      ro.observe(child)
    }
    return () => ro.disconnect()
  }, [messages, messagesLoading, recalcSpacer])

  // window resize
  useEffect(() => {
    const handler = () => {
      if (stateRef.current === 'following') {
        bottomRef.current?.scrollIntoView({ behavior: 'instant' })
      } else if (stateRef.current === 'pinned') {
        recalcSpacer()
        scrollToAnchorInstant()
        const c = scrollContainerRef.current
        if (c) syncBottomState(c)
      }
    }
    window.addEventListener('resize', handler)
    return () => window.removeEventListener('resize', handler)
  }, [recalcSpacer, scrollToAnchorInstant, syncBottomState])

  // cleanup
  useEffect(() => {
    return () => {
      if (stabilizeFrameRef.current !== null) {
        cancelAnimationFrame(stabilizeFrameRef.current)
      }
    }
  }, [])

  return {
    bottomRef,
    scrollContainerRef,
    lastUserMsgRef,
    lastUserPromptRef,
    inputAreaRef,
    spacerRef,
    isAtBottomRef,
    handleScrollContainerScroll,
    scrollToBottom,
    activateAnchor,
    syncBottomState,
    stabilizeDocumentPanelScroll,
    subscribeIsAtBottom,
    getIsAtBottomSnapshot,
  }
}
