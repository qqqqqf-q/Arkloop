import { useCallback, useEffect, useRef, useState } from 'react'
import type { AssistantTurnUi } from '../assistantTurnSegments'

// bottom padding on the content wrapper — clears the input area overlay
// spacer calculation subtracts this so total fill = viewportH - turnH
export const SCROLL_BOTTOM_PAD = 160

// top offset when pinning user prompt — clears the top gradient overlay (h-10 = 40px)
const SCROLL_TOP_OFFSET = 48

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

  const syncBottomState = useCallback((el: HTMLDivElement) => {
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight <= 80
    isAtBottomRef.current = atBottom
    setIsAtBottom(atBottom)
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
  const scrollToAnchor = useCallback(() => {
    const container = scrollContainerRef.current
    const turn = lastUserMsgRef.current
    if (!container || !turn) return

    const viewportH = container.clientHeight
    const turnH = turn.getBoundingClientRect().height
    const turnTop = offsetInContainer(turn)

    programmaticScrollDepthRef.current++
    if (liveStreamActiveRef.current && turnH > viewportH) {
      // streaming: follow bottom of tall turn to show latest output
      container.scrollTop = turnTop + turnH - viewportH
    } else {
      // history / short turn: pin user prompt below gradient
      container.scrollTop = Math.max(0, turnTop - SCROLL_TOP_OFFSET)
    }
    requestAnimationFrame(() => {
      programmaticScrollDepthRef.current--
    })
  }, [offsetInContainer])

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

        recalcSpacer()
        scrollToAnchor()

        isAtBottomRef.current = true
        setIsAtBottom(true)
      })
    })
  }, [recalcSpacer, scrollToAnchor])

  // scroll-to-bottom button: collapse spacer and scroll to actual bottom
  const scrollToBottom = useCallback(() => {
    collapseSpacer()
    requestAnimationFrame(() => {
      bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
      isAtBottomRef.current = true
      setIsAtBottom(true)
    })
  }, [collapseSpacer])

  // scroll handler: ratchet logic
  const handleScrollContainerScroll = useCallback(() => {
    const el = scrollContainerRef.current
    if (!el) return
    syncBottomState(el)

    // ignore programmatic scrolls
    if (programmaticScrollDepthRef.current > 0) return
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
      recalcSpacer()

      programmaticScrollDepthRef.current++
      container.scrollTop = Math.max(0, offsetInContainer(turn) - SCROLL_TOP_OFFSET)
      requestAnimationFrame(() => {
        programmaticScrollDepthRef.current--
      })
    } else {
      // short turn: scroll to natural bottom, no spacer
      container.scrollTop = container.scrollHeight - viewportH
    }
  }, [messagesLoading, recalcSpacer, collapseSpacer])

  // auto-scroll during streaming when anchored or at bottom
  useEffect(() => {
    if (isAnchoredRef.current) {
      recalcSpacer()
      // only follow scroll position during active streaming
      if (liveStreamActiveRef.current) {
        scrollToAnchor()
      }
      return
    }

    if (!isAtBottomRef.current) return
    const forceInstant = forceInstantBottomScrollRef.current
    const liveHandoffPaint =
      liveAssistantTurn != null && liveAssistantTurn.segments.length > 0
    const behavior: ScrollBehavior = forceInstant || liveRunUiVisible || liveHandoffPaint ? 'instant' : 'smooth'
    const container = scrollContainerRef.current
    const bottom = bottomRef.current
    if (container && bottom) {
      const bottomTop = bottom.offsetTop
      const viewBottom = container.scrollTop + container.clientHeight
      if (bottomTop > viewBottom) {
        const targetScroll = bottomTop - container.clientHeight
        if (behavior === 'instant') {
          container.scrollTop = targetScroll
          bottom.scrollIntoView({ behavior: 'instant' })
        } else {
          container.scrollTo({ top: targetScroll, behavior })
        }
      }
    } else {
      bottomRef.current?.scrollIntoView({ behavior })
    }
    if (forceInstant) forceInstantBottomScrollRef.current = false
  }, [messages, liveAssistantTurn, liveRunUiVisible, recalcSpacer, scrollToAnchor])

  // ResizeObserver on anchor turn: recalc spacer when turn content changes size
  useEffect(() => {
    const turn = lastUserMsgRef.current
    if (!turn || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(() => {
      if (isAnchoredRef.current) {
        recalcSpacer()
        // reposition unless user has manually scrolled away
        if (!userScrolledUpRef.current) {
          scrollToAnchor()
        }
      }
    })
    ro.observe(turn)
    return () => ro.disconnect()
  }, [messages, liveAssistantTurn, recalcSpacer, scrollToAnchor])

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
        if (liveStreamActiveRef.current) {
          scrollToAnchor()
        }
      }
    }
    window.addEventListener('resize', handler)
    return () => window.removeEventListener('resize', handler)
  }, [recalcSpacer, scrollToAnchor])

  // cleanup animation frames on unmount
  useEffect(() => {
    return () => {
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
