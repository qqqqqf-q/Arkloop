import { useCallback, useEffect, useRef, useState } from 'react'
import type { AssistantTurnUi } from '../assistantTurnSegments'

// bottom padding on the content wrapper — clears the input area overlay
// spacer calculation subtracts this so total fill = viewportH - turnH
export const SCROLL_BOTTOM_PAD = 160

// top offset when pinning user prompt — clears the top gradient overlay (h-10 = 40px)
const SCROLL_TOP_OFFSET = 48
const ANCHOR_SCROLL_MAX_MONITOR_FRAMES = 24

interface UseScrollPinOptions {
  messagesLoading?: boolean
  messages?: readonly unknown[]
  liveAssistantTurn?: AssistantTurnUi | null
  liveRunUiVisible?: boolean
  topLevelCodeExecutionsLength?: number
}

export interface ScrollPinResult {
  isAtBottom: boolean
  bottomRef: React.RefObject<HTMLDivElement | null>
  scrollContainerRef: React.RefObject<HTMLDivElement | null>
  lastUserMsgRef: React.RefObject<HTMLDivElement | null>
  inputAreaRef: React.RefObject<HTMLDivElement | null>
  copCodeExecScrollRef: React.RefObject<HTMLDivElement | null>
  spacerRef: React.RefObject<HTMLDivElement | null>
  forceInstantBottomScrollRef: React.MutableRefObject<boolean>
  wasLoadingRef: React.MutableRefObject<boolean>
  documentPanelScrollFrameRef: React.MutableRefObject<number | null>
  isAtBottomRef: React.MutableRefObject<boolean>
  programmaticScrollDepthRef: React.MutableRefObject<number>
  handleScrollContainerScroll: () => void
  scrollToBottom: () => void
  activateAnchor: () => void
  syncBottomState: (el: HTMLDivElement) => void
  stabilizeDocumentPanelScroll: (trigger?: HTMLElement | null) => void
}

export function useScrollPin(options: UseScrollPinOptions = {}): ScrollPinResult {
  const {
    messagesLoading = false,
    messages = [],
    liveAssistantTurn = null,
    liveRunUiVisible = false,
    topLevelCodeExecutionsLength = 0,
  } = options
  const bottomRef = useRef<HTMLDivElement>(null)
  const scrollContainerRef = useRef<HTMLDivElement>(null)
  const lastUserMsgRef = useRef<HTMLDivElement>(null)
  const inputAreaRef = useRef<HTMLDivElement>(null)
  const copCodeExecScrollRef = useRef<HTMLDivElement>(null)
  const spacerRef = useRef<HTMLDivElement>(null)
  const forceInstantBottomScrollRef = useRef(false)
  const wasLoadingRef = useRef(false)
  const documentPanelScrollFrameRef = useRef<number | null>(null)
  const isAtBottomRef = useRef(true)
  const [isAtBottom, setIsAtBottom] = useState(true)

  // anchor state (imperative, not React state — avoid re-renders on every scroll)
  const isAnchoredRef = useRef(false)
  const userScrolledUpRef = useRef(false)
  const spacerRatchetRef = useRef(0)
  const programmaticScrollDepthRef = useRef(0)
  const lastUserScrollTopRef = useRef(0)
  // tracks whether streaming is active — only follow scroll during streaming
  const liveStreamActiveRef = useRef(false)
  const followLiveOutputRef = useRef(false)
  const anchorScrollMonitorFrameRef = useRef<number | null>(null)

  const syncBottomState = useCallback((el: HTMLDivElement) => {
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight <= 80
    isAtBottomRef.current = atBottom
    setIsAtBottom(atBottom)
  }, [])

  const syncBottomStateFromContainer = useCallback(() => {
    const container = scrollContainerRef.current
    if (!container) return
    syncBottomState(container)
  }, [syncBottomState])

  const scrollViewportToBottom = useCallback((behavior: ScrollBehavior) => {
    const container = scrollContainerRef.current
    const bottom = bottomRef.current
    if (!container || !bottom) return

    if (anchorScrollMonitorFrameRef.current !== null) {
      cancelAnimationFrame(anchorScrollMonitorFrameRef.current)
      anchorScrollMonitorFrameRef.current = null
      programmaticScrollDepthRef.current = Math.max(0, programmaticScrollDepthRef.current - 1)
    }

    const targetScroll = Math.max(0, bottom.offsetTop - container.clientHeight)
    programmaticScrollDepthRef.current++

    if (behavior === 'instant') {
      container.scrollTop = targetScroll
      bottom.scrollIntoView({ behavior: 'instant' })
    } else {
      container.scrollTo({ top: targetScroll, behavior })
    }

    requestAnimationFrame(() => {
      programmaticScrollDepthRef.current--
      syncBottomState(container)
    })
  }, [syncBottomState])

  const prefersReducedMotion = useCallback(() => {
    return typeof window !== 'undefined'
      && typeof window.matchMedia === 'function'
      && window.matchMedia('(prefers-reduced-motion: reduce)').matches
  }, [])

  // compute element's scrollTop-relative offset (robust against positioned parents)
  const offsetInContainer = useCallback((el: HTMLElement): number => {
    const container = scrollContainerRef.current
    if (!container) return 0
    return el.getBoundingClientRect().top - container.getBoundingClientRect().top + container.scrollTop
  }, [])

  // spacer height = max(0, viewport - turn height), clamped by ratchet when scrolled up
  const recalcSpacer = useCallback(() => {
    const spacer = spacerRef.current
    const container = scrollContainerRef.current
    const turn = lastUserMsgRef.current
    if (!spacer || !container) return

    if (!isAnchoredRef.current || !turn) {
      spacer.style.height = '0px'
      spacerRatchetRef.current = 0
      return
    }

    const viewportH = container.clientHeight
    const turnH = turn.getBoundingClientRect().height
    let needed = Math.max(0, viewportH - turnH - SCROLL_BOTTOM_PAD - SCROLL_TOP_OFFSET)

    if (userScrolledUpRef.current) {
      // ratchet: only allow decrease
      needed = Math.min(needed, spacerRatchetRef.current)
    } else {
      spacerRatchetRef.current = needed
    }

    spacer.style.height = needed + 'px'
    spacerRatchetRef.current = needed
  }, [])

  // scroll so that the anchor turn top aligns below the top gradient overlay
  // during streaming, follow the bottom of tall turns to show latest output
  const anchorScrollTop = useCallback((): number | null => {
    const container = scrollContainerRef.current
    const turn = lastUserMsgRef.current
    if (!container || !turn) return null

    const viewportH = container.clientHeight
    const turnH = turn.getBoundingClientRect().height
    const turnTop = offsetInContainer(turn)

    if (followLiveOutputRef.current && liveStreamActiveRef.current && turnH > viewportH) {
      return turnTop + turnH - viewportH
    }
    return Math.max(0, turnTop - SCROLL_TOP_OFFSET)
  }, [offsetInContainer])

  const scrollToAnchor = useCallback(() => {
    const container = scrollContainerRef.current
    const targetScroll = anchorScrollTop()
    if (!container || targetScroll == null) return

    if (anchorScrollMonitorFrameRef.current !== null) {
      cancelAnimationFrame(anchorScrollMonitorFrameRef.current)
      anchorScrollMonitorFrameRef.current = null
      programmaticScrollDepthRef.current = Math.max(0, programmaticScrollDepthRef.current - 1)
    }

    programmaticScrollDepthRef.current++
    container.scrollTop = targetScroll
    requestAnimationFrame(() => {
      programmaticScrollDepthRef.current--
      syncBottomState(container)
    })
  }, [anchorScrollTop, syncBottomState])

  const animateAnchorIntoPlace = useCallback(() => {
    const container = scrollContainerRef.current
    const targetScroll = anchorScrollTop()
    if (!container || targetScroll == null) return

    if (anchorScrollMonitorFrameRef.current !== null) {
      cancelAnimationFrame(anchorScrollMonitorFrameRef.current)
      anchorScrollMonitorFrameRef.current = null
      programmaticScrollDepthRef.current = Math.max(0, programmaticScrollDepthRef.current - 1)
    }

    if (Math.abs(targetScroll - container.scrollTop) < 1 || prefersReducedMotion()) {
      scrollToAnchor()
      return
    }

    programmaticScrollDepthRef.current++
    isAtBottomRef.current = false
    setIsAtBottom(false)
    container.scrollTo({ top: targetScroll, behavior: 'smooth' })

    let frame = 0
    let stableFrames = 0
    let lastScrollTop = container.scrollTop
    const tick = () => {
      const currentContainer = scrollContainerRef.current
      if (!currentContainer) {
        anchorScrollMonitorFrameRef.current = null
        programmaticScrollDepthRef.current = Math.max(0, programmaticScrollDepthRef.current - 1)
        return
      }

      frame += 1
      const currentScrollTop = currentContainer.scrollTop
      const nearTarget = Math.abs(currentScrollTop - targetScroll) <= 2
      const stationary = Math.abs(currentScrollTop - lastScrollTop) <= 0.5
      stableFrames = nearTarget || stationary ? stableFrames + 1 : 0
      lastScrollTop = currentScrollTop

      if (stableFrames >= 2 || frame >= ANCHOR_SCROLL_MAX_MONITOR_FRAMES) {
        anchorScrollMonitorFrameRef.current = null
        programmaticScrollDepthRef.current = Math.max(0, programmaticScrollDepthRef.current - 1)
        syncBottomState(currentContainer)
        return
      }

      anchorScrollMonitorFrameRef.current = requestAnimationFrame(tick)
    }

    anchorScrollMonitorFrameRef.current = requestAnimationFrame(tick)
  }, [anchorScrollTop, prefersReducedMotion, scrollToAnchor, syncBottomState])

  const collapseSpacer = useCallback(() => {
    isAnchoredRef.current = false
    userScrolledUpRef.current = false
    spacerRatchetRef.current = 0
    if (spacerRef.current) spacerRef.current.style.height = '0px'
  }, [])

  // activate anchor on the current lastUserMsg turn
  const activateAnchor = useCallback(() => {
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        const turn = lastUserMsgRef.current
        if (!turn) {
          // fallback: simple scroll to bottom
          bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
          isAtBottomRef.current = true
          setIsAtBottom(true)
          return
        }

        isAnchoredRef.current = true
        userScrolledUpRef.current = false
        spacerRatchetRef.current = 0
        followLiveOutputRef.current = false

        recalcSpacer()
        animateAnchorIntoPlace()
      })
    })
  }, [animateAnchorIntoPlace, recalcSpacer])

  // scroll-to-bottom button: collapse spacer and scroll to actual bottom
  const scrollToBottom = useCallback(() => {
    collapseSpacer()
    followLiveOutputRef.current = true
    requestAnimationFrame(() => {
      const behavior: ScrollBehavior = liveStreamActiveRef.current ? 'instant' : 'smooth'
      scrollViewportToBottom(behavior)
      isAtBottomRef.current = true
      setIsAtBottom(true)
    })
  }, [collapseSpacer, scrollViewportToBottom])

  // scroll handler: ratchet logic
  const handleScrollContainerScroll = useCallback(() => {
    const el = scrollContainerRef.current
    if (!el) return
    syncBottomState(el)

    // ignore programmatic scrolls
    if (programmaticScrollDepthRef.current > 0) return
    if (!isAtBottomRef.current) {
      followLiveOutputRef.current = false
    }
    if (!isAnchoredRef.current) return

    const st = el.scrollTop
    const turn = lastUserMsgRef.current
    const anchorOffset = turn ? offsetInContainer(turn) : 0

    if (userScrolledUpRef.current) {
      // check if user scrolled back to anchor zone — clear the scrolled-up flag
      if (st >= anchorOffset - 10) {
        userScrolledUpRef.current = false
        return
      }

      const currentSpacer = parseFloat(spacerRef.current?.style.height ?? '0')

      if (st < lastUserScrollTopRef.current) {
        // scrolling UP: consume spacer by the delta
        const delta = lastUserScrollTopRef.current - st
        const newH = Math.max(0, currentSpacer - delta)
        if (spacerRef.current) spacerRef.current.style.height = newH + 'px'
        spacerRatchetRef.current = newH

        if (newH <= 0) {
          collapseSpacer()
        }
      }
      // scrolling DOWN: spacer does NOT grow back (ratchet)
      lastUserScrollTopRef.current = st
    } else {
      // detect: user scrolled above the anchor turn
      if (st < anchorOffset - 10) {
        userScrolledUpRef.current = true
        lastUserScrollTopRef.current = st
      }
    }
  }, [syncBottomState, collapseSpacer, offsetInContainer])

  const stabilizeDocumentPanelScroll = useCallback((trigger?: HTMLElement | null) => {
    const container = scrollContainerRef.current
    if (!container) return

    if (documentPanelScrollFrameRef.current !== null) {
      cancelAnimationFrame(documentPanelScrollFrameRef.current)
      documentPanelScrollFrameRef.current = null
    }

    const anchor = trigger && container.contains(trigger) ? trigger : null
    const anchorTop = anchor
      ? anchor.getBoundingClientRect().top - container.getBoundingClientRect().top
      : null
    const distanceFromBottom = container.scrollHeight - container.scrollTop - container.clientHeight
    const startedAt = performance.now()

    const step = () => {
      const currentContainer = scrollContainerRef.current
      if (!currentContainer) return

      if (anchor && anchorTop !== null && anchor.isConnected && currentContainer.contains(anchor)) {
        const nextTop = anchor.getBoundingClientRect().top - currentContainer.getBoundingClientRect().top
        currentContainer.scrollTop += nextTop - anchorTop
      } else {
        currentContainer.scrollTop = Math.max(0, currentContainer.scrollHeight - currentContainer.clientHeight - distanceFromBottom)
      }

      syncBottomState(currentContainer)

      if (performance.now() - startedAt < 360) {
        documentPanelScrollFrameRef.current = requestAnimationFrame(step)
        return
      }

      documentPanelScrollFrameRef.current = null
    }

    documentPanelScrollFrameRef.current = requestAnimationFrame(step)
  }, [syncBottomState])

  // history load: activate anchor and scroll to last user message
  // keep liveStreamActive in sync (before effects that read it)
  liveStreamActiveRef.current = liveAssistantTurn != null || liveRunUiVisible

  useEffect(() => {
    if (messagesLoading) {
      wasLoadingRef.current = true
      // reset anchor state from previous thread
      followLiveOutputRef.current = false
      collapseSpacer()
      return
    }
    if (!wasLoadingRef.current) return
    wasLoadingRef.current = false

    const container = scrollContainerRef.current
    const turn = lastUserMsgRef.current
    if (!container || !turn) return

    const viewportH = container.clientHeight
    const turnH = turn.getBoundingClientRect().height

    if (turnH > viewportH * 0.5) {
      // long turn: pin user prompt to top with spacer
      isAnchoredRef.current = true
      userScrolledUpRef.current = false
      spacerRatchetRef.current = 0
      followLiveOutputRef.current = false
      recalcSpacer()

      programmaticScrollDepthRef.current++
      container.scrollTop = Math.max(0, offsetInContainer(turn) - SCROLL_TOP_OFFSET)
      requestAnimationFrame(() => {
        programmaticScrollDepthRef.current--
      })
      syncBottomState(container)
    } else {
      // short turn: scroll to natural bottom, no spacer
      container.scrollTop = container.scrollHeight - viewportH
      syncBottomState(container)
    }
  }, [messagesLoading, recalcSpacer, collapseSpacer, offsetInContainer, syncBottomState])

  // auto-scroll during streaming when anchored or at bottom
  useEffect(() => {
    const container = scrollContainerRef.current
    if (isAnchoredRef.current) {
      recalcSpacer()
      if (container) syncBottomState(container)
      // only follow scroll position during active streaming
      if (followLiveOutputRef.current && liveStreamActiveRef.current) {
        scrollToAnchor()
      }
      return
    }

    if (!isAtBottomRef.current) return
    const forceInstant = forceInstantBottomScrollRef.current
    const liveHandoffPaint =
      liveAssistantTurn != null && liveAssistantTurn.segments.length > 0
    const behavior: ScrollBehavior = forceInstant || liveRunUiVisible || liveHandoffPaint ? 'instant' : 'smooth'
    const bottom = bottomRef.current
    if (container && bottom) {
      const bottomTop = bottom.offsetTop
      const viewBottom = container.scrollTop + container.clientHeight
      if (bottomTop > viewBottom) {
        scrollViewportToBottom(behavior)
      }
    } else {
      bottomRef.current?.scrollIntoView({ behavior })
    }
    if (forceInstant) forceInstantBottomScrollRef.current = false
  }, [messages, liveAssistantTurn, liveRunUiVisible, recalcSpacer, scrollToAnchor, scrollViewportToBottom])

  // ResizeObserver on anchor turn: recalc spacer when turn content changes size
  useEffect(() => {
    const turn = lastUserMsgRef.current
    if (!turn || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(() => {
      if (isAnchoredRef.current) {
        recalcSpacer()
        if (!userScrolledUpRef.current && (!liveStreamActiveRef.current || followLiveOutputRef.current)) {
          scrollToAnchor()
        }
        const container = scrollContainerRef.current
        if (container) syncBottomState(container)
        return
      }

      if (liveStreamActiveRef.current && (followLiveOutputRef.current || isAtBottomRef.current)) {
        scrollViewportToBottom('instant')
      }
    })
    ro.observe(turn)
    return () => ro.disconnect()
  }, [messages, liveAssistantTurn, recalcSpacer, scrollToAnchor, scrollViewportToBottom, syncBottomState])

  useEffect(() => {
    const el = copCodeExecScrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [topLevelCodeExecutionsLength, liveAssistantTurn])

  // input area resize observer
  useEffect(() => {
    const el = inputAreaRef.current
    if (!el) return
    if (typeof ResizeObserver === 'undefined') {
      document.documentElement.style.setProperty('--chat-input-area-height', `${el.getBoundingClientRect().height}px`)
      return
    }
    const ro = new ResizeObserver(([entry]) => {
      document.documentElement.style.setProperty('--chat-input-area-height', `${entry.contentRect.height}px`)
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  // window resize: recalc spacer
  useEffect(() => {
    const handler = () => {
      if (isAnchoredRef.current) {
        recalcSpacer()
        if (!liveStreamActiveRef.current || followLiveOutputRef.current) {
          scrollToAnchor()
        }
        syncBottomStateFromContainer()
        return
      }

      if (liveStreamActiveRef.current && (followLiveOutputRef.current || isAtBottomRef.current)) {
        scrollViewportToBottom('instant')
      }
    }
    window.addEventListener('resize', handler)
    return () => window.removeEventListener('resize', handler)
  }, [recalcSpacer, scrollToAnchor, scrollViewportToBottom, syncBottomStateFromContainer])

  // cleanup animation frames on unmount
  useEffect(() => {
    return () => {
      if (anchorScrollMonitorFrameRef.current !== null) {
        cancelAnimationFrame(anchorScrollMonitorFrameRef.current)
      }
      if (documentPanelScrollFrameRef.current !== null) {
        cancelAnimationFrame(documentPanelScrollFrameRef.current)
      }
    }
  }, [])

  return {
    isAtBottom,
    bottomRef,
    scrollContainerRef,
    lastUserMsgRef,
    inputAreaRef,
    copCodeExecScrollRef,
    spacerRef,
    forceInstantBottomScrollRef,
    wasLoadingRef,
    documentPanelScrollFrameRef,
    isAtBottomRef,
    programmaticScrollDepthRef,
    handleScrollContainerScroll,
    scrollToBottom,
    activateAnchor,
    syncBottomState,
    stabilizeDocumentPanelScroll,
  }
}
