import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { AssistantTurnUi } from '../assistantTurnSegments'

// bottom padding on the content wrapper — clears the input area overlay
// spacer calculation subtracts this so total fill = viewportH - turnH
export const SCROLL_BOTTOM_PAD = 160

// top offset when pinning user prompt — clears the top gradient overlay (h-10 = 40px)
const SCROLL_TOP_OFFSET = 48
const ANCHOR_SCROLL_MAX_MONITOR_FRAMES = 24

type ViewportAnchor = {
  element: HTMLElement | null
  top: number
  turnOffset: number | null
  path: number[] | null
}

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
  lastUserPromptRef: React.RefObject<HTMLDivElement | null>
  inputAreaRef: React.RefObject<HTMLDivElement | null>
  copCodeExecScrollRef: React.RefObject<HTMLDivElement | null>
  spacerRef: React.RefObject<HTMLDivElement | null>
  forceInstantBottomScrollRef: React.MutableRefObject<boolean>
  wasLoadingRef: React.MutableRefObject<boolean>
  documentPanelScrollFrameRef: React.MutableRefObject<number | null>
  isAtBottomRef: React.MutableRefObject<boolean>
  programmaticScrollDepthRef: React.MutableRefObject<number>
  handleScrollContainerScroll: () => void
  captureViewportAnchor: () => void
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
  const lastUserPromptRef = useRef<HTMLDivElement>(null)
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
  const anchorActivationPendingRef = useRef(false)
  const viewportAnchorRef = useRef<ViewportAnchor | null>(null)

  const syncBottomState = useCallback((el: HTMLDivElement) => {
    const physicallyAtBottom = el.scrollHeight - el.scrollTop - el.clientHeight <= 80
    const anchoredViewLocked =
      isAnchoredRef.current &&
      !userScrolledUpRef.current &&
      !followLiveOutputRef.current
    const atBottom = physicallyAtBottom || anchoredViewLocked
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

  const contentRoot = useCallback((): HTMLElement | null => {
    const container = scrollContainerRef.current
    if (!container) return null
    const first = container.firstElementChild
    return first instanceof HTMLElement ? first : null
  }, [])

  const pathFromRoot = useCallback((root: HTMLElement, node: HTMLElement): number[] | null => {
    if (node === root || !root.contains(node)) return null
    const path: number[] = []
    let current: HTMLElement | null = node
    while (current && current !== root) {
      const parent: HTMLElement | null = current.parentElement
      if (!parent) return null
      const index = Array.prototype.indexOf.call(parent.children, current)
      if (index < 0) return null
      path.unshift(index)
      current = parent
    }
    return current === root ? path : null
  }, [])

  const resolvePathFromRoot = useCallback((root: HTMLElement, path: number[] | null): HTMLElement | null => {
    if (!path || path.length === 0) return null
    let current: HTMLElement | null = root
    for (const index of path) {
      const next: Element | null = current.children.item(index)
      if (!(next instanceof HTMLElement)) return null
      current = next
    }
    return current
  }, [])

  const shouldPreserveViewport = useCallback(() => {
    if (anchorActivationPendingRef.current) return false
    if (followLiveOutputRef.current || isAtBottomRef.current) return false
    if (!isAnchoredRef.current) return true
    return userScrolledUpRef.current
  }, [])

  const findViewportAnchor = useCallback((): ViewportAnchor | null => {
    const container = scrollContainerRef.current
    const root = contentRoot()
    if (!container || !root) return null

    const containerRect = container.getBoundingClientRect()
    const markerTop = Math.min(
      Math.max(container.clientHeight - 1, 0),
      Math.max(16, SCROLL_TOP_OFFSET + 8),
    )
    const topEdge = containerRect.top + 1
    const bottomEdge = containerRect.bottom - 1
    const isVisibleCandidate = (node: HTMLElement | null): node is HTMLElement => {
      if (!node) return false
      if (node === container || node === root) return false
      if (!root.contains(node)) return false
      if (node === bottomRef.current || node === spacerRef.current) return false
      const rect = node.getBoundingClientRect()
      if (rect.width <= 0 && rect.height <= 0) return false
      if (rect.bottom <= topEdge) return false
      if (rect.top >= bottomEdge) return false
      return true
    }

    const candidateDepth = (node: HTMLElement) => {
      let depth = 0
      let parent = node.parentElement
      while (parent && parent !== root) {
        depth += 1
        parent = parent.parentElement
      }
      return depth
    }

    const chooseBetterCandidate = (
      current: { element: HTMLElement; top: number; depth: number } | null,
      next: { element: HTMLElement; top: number; depth: number },
    ) => {
      if (current == null) return next
      const currentStartsInside = current.top >= 0
      const nextStartsInside = next.top >= 0
      if (currentStartsInside !== nextStartsInside) {
        return nextStartsInside ? next : current
      }
      if (currentStartsInside && nextStartsInside) {
        if (next.top < current.top - 0.5) return next
        if (Math.abs(next.top - current.top) <= 0.5 && next.depth > current.depth) return next
        return current
      }
      if (next.top > current.top + 0.5) return next
      if (Math.abs(next.top - current.top) <= 0.5 && next.depth > current.depth) return next
      return current
    }

    const samplePoints = [
      topEdge + container.clientHeight * 0.45,
      topEdge + container.clientHeight * 0.3,
      topEdge + Math.min(32, Math.max(12, container.clientHeight * 0.12)),
    ]
    const sampleX = containerRect.left + Math.max(16, Math.min(containerRect.width - 16, containerRect.width * 0.5))

    let best: { element: HTMLElement; top: number; depth: number } | null = null

    if (typeof document.elementFromPoint === 'function') {
      for (const sampleY of samplePoints) {
        const hit = document.elementFromPoint(sampleX, sampleY)
        if (!(hit instanceof HTMLElement) || !container.contains(hit)) continue

        let candidate: HTMLElement | null = hit
        while (candidate && candidate !== container && candidate !== root) {
          if (isVisibleCandidate(candidate)) {
            best = chooseBetterCandidate(best, {
              element: candidate,
              top: candidate.getBoundingClientRect().top - containerRect.top,
              depth: candidateDepth(candidate),
            })
          }
          candidate = candidate.parentElement as HTMLElement | null
        }
      }
    }

    const walker = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT)
    let current = walker.nextNode()
    while (current) {
      if (current instanceof HTMLElement && isVisibleCandidate(current)) {
        best = chooseBetterCandidate(best, {
          element: current,
          top: current.getBoundingClientRect().top - containerRect.top,
          depth: candidateDepth(current),
        })
      }
      current = walker.nextNode()
    }

    if (best) {
      const turn = lastUserMsgRef.current
      let turnOffset: number | null = null
      if (turn) {
        const markerScrollTop = container.scrollTop + markerTop
        const turnTop = offsetInContainer(turn)
        const turnBottom = turnTop + turn.getBoundingClientRect().height
        if (markerScrollTop >= turnTop && markerScrollTop <= turnBottom) {
          turnOffset = markerScrollTop - turnTop
        }
      }
      return {
        element: best.element,
        top: best.top,
        turnOffset,
        path: pathFromRoot(root, best.element),
      }
    }
    return null
  }, [contentRoot, offsetInContainer, pathFromRoot])

  const captureViewportAnchor = useCallback(() => {
    viewportAnchorRef.current = shouldPreserveViewport() ? findViewportAnchor() : null
  }, [findViewportAnchor, shouldPreserveViewport])

  const isAnchorAnimating = useCallback(() => {
    return anchorScrollMonitorFrameRef.current !== null
  }, [])

  const preserveViewportAnchor = useCallback(() => {
    if (!shouldPreserveViewport()) {
      viewportAnchorRef.current = null
      return
    }

    const container = scrollContainerRef.current
    if (!container) return

    const anchor = viewportAnchorRef.current ?? findViewportAnchor()
    if (!anchor) return

    const root = contentRoot()
    const currentAnchor = (() => {
      if (anchor.element && anchor.element.isConnected && container.contains(anchor.element)) {
        return anchor
      }
      if (root) {
        const resolved = resolvePathFromRoot(root, anchor.path)
        if (resolved && resolved.isConnected && container.contains(resolved)) {
          return {
            element: resolved,
            top: anchor.top,
            turnOffset: anchor.turnOffset,
            path: anchor.path,
          }
        }
      }
      return findViewportAnchor()
    })()

    if (currentAnchor?.element && container.contains(currentAnchor.element)) {
      const nextTop = currentAnchor.element.getBoundingClientRect().top - container.getBoundingClientRect().top
      const delta = nextTop - anchor.top
      if (Math.abs(delta) <= 0.5) {
        viewportAnchorRef.current = {
          element: currentAnchor.element,
          top: nextTop,
          turnOffset: currentAnchor.turnOffset ?? anchor.turnOffset,
          path: currentAnchor.path ?? anchor.path,
        }
        return
      }

      programmaticScrollDepthRef.current++
      container.scrollTop += delta
      viewportAnchorRef.current = {
        element: currentAnchor.element,
        top: anchor.top,
        turnOffset: currentAnchor.turnOffset ?? anchor.turnOffset,
        path: currentAnchor.path ?? anchor.path,
      }
      requestAnimationFrame(() => {
        programmaticScrollDepthRef.current--
        syncBottomState(container)
        if (shouldPreserveViewport()) {
          captureViewportAnchor()
        }
      })
      return
    }

    const turn = lastUserMsgRef.current
    if (!turn || anchor.turnOffset == null) {
      viewportAnchorRef.current = findViewportAnchor()
      return
    }

    const markerTop = Math.min(
      Math.max(container.clientHeight - 1, 0),
      Math.max(16, SCROLL_TOP_OFFSET + 8),
    )
    programmaticScrollDepthRef.current++
    container.scrollTop = Math.max(0, offsetInContainer(turn) + anchor.turnOffset - markerTop)
    viewportAnchorRef.current = {
      element: null,
      top: markerTop,
      turnOffset: anchor.turnOffset,
      path: anchor.path,
    }
    requestAnimationFrame(() => {
      programmaticScrollDepthRef.current--
      syncBottomState(container)
      if (shouldPreserveViewport()) {
        captureViewportAnchor()
      }
    })
  }, [captureViewportAnchor, contentRoot, findViewportAnchor, offsetInContainer, resolvePathFromRoot, shouldPreserveViewport, syncBottomState])

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
    const prompt = lastUserPromptRef.current
    if (!container || !turn) return null

    const viewportH = container.clientHeight
    const turnH = turn.getBoundingClientRect().height
    const turnTop = offsetInContainer(turn)
    const promptTop = offsetInContainer(prompt ?? turn)

    if (followLiveOutputRef.current && liveStreamActiveRef.current && turnH > viewportH) {
      return turnTop + turnH - viewportH
    }
    return Math.max(0, promptTop - SCROLL_TOP_OFFSET)
  }, [offsetInContainer])

  const scrollToAnchor = useCallback(() => {
    const container = scrollContainerRef.current
    const targetScroll = anchorScrollTop()
    if (!container || targetScroll == null) return
    if (Math.abs(container.scrollTop - targetScroll) <= 0.5) {
      syncBottomState(container)
      return
    }

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
    isAtBottomRef.current = true
    setIsAtBottom(true)
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
    anchorActivationPendingRef.current = false
    viewportAnchorRef.current = null
    if (spacerRef.current) spacerRef.current.style.height = '0px'
  }, [])

  // activate anchor on the current lastUserMsg turn
  const activateAnchor = useCallback(() => {
    anchorActivationPendingRef.current = true
    isAnchoredRef.current = true
    userScrolledUpRef.current = false
    spacerRatchetRef.current = 0
    followLiveOutputRef.current = false
    viewportAnchorRef.current = null
    isAtBottomRef.current = true
    setIsAtBottom(true)
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        const turn = lastUserMsgRef.current
        if (!turn) {
          // fallback: simple scroll to bottom
          anchorActivationPendingRef.current = false
          isAnchoredRef.current = false
          bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
          isAtBottomRef.current = true
          setIsAtBottom(true)
          return
        }

        anchorActivationPendingRef.current = false
        isAnchoredRef.current = true
        userScrolledUpRef.current = false
        spacerRatchetRef.current = 0
        followLiveOutputRef.current = false
        viewportAnchorRef.current = null
        isAtBottomRef.current = true
        setIsAtBottom(true)

        recalcSpacer()
        animateAnchorIntoPlace()
      })
    })
  }, [animateAnchorIntoPlace, recalcSpacer])

  // scroll-to-bottom button: collapse spacer and scroll to actual bottom
  const scrollToBottom = useCallback(() => {
    collapseSpacer()
    followLiveOutputRef.current = true
    viewportAnchorRef.current = null
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
    if (anchorActivationPendingRef.current) return
    if (!isAtBottomRef.current) {
      followLiveOutputRef.current = false
    }
    if (shouldPreserveViewport()) {
      captureViewportAnchor()
    } else {
      viewportAnchorRef.current = null
    }
    if (!isAnchoredRef.current) return

    const st = el.scrollTop
    const turn = lastUserMsgRef.current
    const anchorOffset = turn ? offsetInContainer(turn) : 0

    if (userScrolledUpRef.current) {
      // check if user scrolled back to anchor zone — clear the scrolled-up flag
      if (st >= anchorOffset - 10) {
        userScrolledUpRef.current = false
        syncBottomState(el)
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
      captureViewportAnchor()
    } else {
      // detect: user scrolled above the anchor turn
      if (st < anchorOffset - 10) {
        userScrolledUpRef.current = true
        lastUserScrollTopRef.current = st
        captureViewportAnchor()
        syncBottomState(el)
      }
    }
  }, [syncBottomState, captureViewportAnchor, collapseSpacer, offsetInContainer, shouldPreserveViewport])

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

  useLayoutEffect(() => {
    if (messagesLoading) return
    if (shouldPreserveViewport()) {
      preserveViewportAnchor()
    }
  }, [messages, liveAssistantTurn, liveRunUiVisible, messagesLoading, preserveViewportAnchor, shouldPreserveViewport])

  useLayoutEffect(() => {
    const container = scrollContainerRef.current
    if (!container) return
    const previous = container.style.overflowAnchor
    container.style.overflowAnchor = 'none'
    return () => {
      container.style.overflowAnchor = previous
    }
  }, [])

  // auto-scroll during streaming when anchored or at bottom
  useEffect(() => {
    const container = scrollContainerRef.current
    if (anchorActivationPendingRef.current || isAnchorAnimating()) return
    if (isAnchoredRef.current) {
      recalcSpacer()
      if (userScrolledUpRef.current) {
        preserveViewportAnchor()
      } else {
        scrollToAnchor()
      }
      if (container) syncBottomState(container)
      return
    }

    if (!isAtBottomRef.current) {
      if (shouldPreserveViewport()) {
        preserveViewportAnchor()
      }
      return
    }
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
    if (shouldPreserveViewport()) {
      captureViewportAnchor()
    }
  }, [messages, liveAssistantTurn, liveRunUiVisible, preserveViewportAnchor, recalcSpacer, scrollToAnchor, scrollViewportToBottom, shouldPreserveViewport, captureViewportAnchor])

  // ResizeObserver on anchor turn: recalc spacer when turn content changes size
  useEffect(() => {
    const turn = lastUserMsgRef.current
    if (!turn || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(() => {
      if (anchorActivationPendingRef.current || isAnchorAnimating()) return
      if (isAnchoredRef.current) {
        recalcSpacer()
        if (userScrolledUpRef.current) {
          preserveViewportAnchor()
        } else {
          scrollToAnchor()
        }
        const container = scrollContainerRef.current
        if (container) syncBottomState(container)
        return
      }

      if (followLiveOutputRef.current || isAtBottomRef.current) {
        scrollViewportToBottom('instant')
        return
      }
      if (shouldPreserveViewport()) {
        preserveViewportAnchor()
      }
    })
    ro.observe(turn)
    return () => ro.disconnect()
  }, [messages, liveAssistantTurn, preserveViewportAnchor, recalcSpacer, scrollToAnchor, scrollViewportToBottom, shouldPreserveViewport, syncBottomState])

  useEffect(() => {
    const root = contentRoot()
    if (!root || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(() => {
      if (anchorActivationPendingRef.current || isAnchorAnimating()) return
      if (isAnchoredRef.current) {
        recalcSpacer()
        if (userScrolledUpRef.current) {
          preserveViewportAnchor()
        } else {
          scrollToAnchor()
        }
        const container = scrollContainerRef.current
        if (container) syncBottomState(container)
        return
      }

      if (followLiveOutputRef.current || isAtBottomRef.current) {
        scrollViewportToBottom('instant')
        return
      }
      if (shouldPreserveViewport()) {
        preserveViewportAnchor()
      }
    })
    ro.observe(root)
    return () => ro.disconnect()
  }, [contentRoot, preserveViewportAnchor, recalcSpacer, scrollToAnchor, scrollViewportToBottom, shouldPreserveViewport, syncBottomState])

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
      if (anchorActivationPendingRef.current || isAnchorAnimating()) return
      if (isAnchoredRef.current) {
        recalcSpacer()
        if (userScrolledUpRef.current) {
          preserveViewportAnchor()
        } else {
          scrollToAnchor()
        }
        syncBottomStateFromContainer()
        return
      }

      if (followLiveOutputRef.current || isAtBottomRef.current) {
        scrollViewportToBottom('instant')
        return
      }
      if (shouldPreserveViewport()) {
        preserveViewportAnchor()
      }
    }
    window.addEventListener('resize', handler)
    return () => window.removeEventListener('resize', handler)
  }, [isAnchorAnimating, preserveViewportAnchor, recalcSpacer, scrollToAnchor, scrollViewportToBottom, shouldPreserveViewport, syncBottomStateFromContainer])

  // cleanup animation frames on unmount
  useEffect(() => {
    return () => {
      if (anchorScrollMonitorFrameRef.current !== null) {
        cancelAnimationFrame(anchorScrollMonitorFrameRef.current)
      }
      if (documentPanelScrollFrameRef.current !== null) {
        cancelAnimationFrame(documentPanelScrollFrameRef.current)
      }
      anchorActivationPendingRef.current = false
    }
  }, [])

  return {
    isAtBottom,
    bottomRef,
    scrollContainerRef,
    lastUserMsgRef,
    lastUserPromptRef,
    inputAreaRef,
    copCodeExecScrollRef,
    spacerRef,
    forceInstantBottomScrollRef,
    wasLoadingRef,
    documentPanelScrollFrameRef,
    isAtBottomRef,
    programmaticScrollDepthRef,
    handleScrollContainerScroll,
    captureViewportAnchor,
    scrollToBottom,
    activateAnchor,
    syncBottomState,
    stabilizeDocumentPanelScroll,
  }
}
