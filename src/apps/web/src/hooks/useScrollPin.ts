import { useCallback, useEffect, useLayoutEffect, useRef } from 'react'
import type { AssistantTurnUi } from '../assistantTurnSegments'

// fallback reserved space before the input area is measured
export const SCROLL_BOTTOM_PAD = 160

// top offset when pinning user prompt — clears the top gradient overlay (h-10 = 40px)
const SCROLL_TOP_OFFSET = 48
const ANCHOR_SCROLL_MAX_MONITOR_FRAMES = 180
const ANCHOR_SCROLL_SETTLE_FRAMES = 10
const ANCHOR_SCROLL_TARGET_EPSILON = 0.5
const LAYOUT_SCROLL_WIDTH_EPSILON = 0.5
const resizeObserverBlockSizeCache = new WeakMap<Element, number>()

type ViewportAnchor = {
  element: HTMLElement | null
  top: number
  turnOffset: number | null
  path: number[] | null
}

function hasResizeObserverBlockChange(entry: ResizeObserverEntry): boolean {
  const borderBoxSize = Array.isArray(entry.borderBoxSize) ? entry.borderBoxSize[0] : entry.borderBoxSize
  const blockSize = borderBoxSize?.blockSize
    ?? entry.contentRect?.height
    ?? (entry.target instanceof HTMLElement ? entry.target.getBoundingClientRect().height : 0)
  const previous = resizeObserverBlockSizeCache.get(entry.target)
  resizeObserverBlockSizeCache.set(entry.target, blockSize)
  return previous == null || Math.abs(blockSize - previous) > 0.5
}

interface UseScrollPinOptions {
  messagesLoading?: boolean
  messages?: readonly unknown[]
  liveAssistantTurn?: AssistantTurnUi | null
  liveRunUiVisible?: boolean
  topLevelCodeExecutionsLength?: number
  promptPinningDisabled?: boolean
}

export interface ScrollPinResult {
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
  subscribeIsAtBottom: (listener: () => void) => () => void
  getIsAtBottomSnapshot: () => boolean
}

export function useScrollPin(options: UseScrollPinOptions = {}): ScrollPinResult {
  const {
    messagesLoading = false,
    messages = [],
    liveAssistantTurn = null,
    liveRunUiVisible = false,
    topLevelCodeExecutionsLength = 0,
    promptPinningDisabled = false,
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
  const localExpansionActiveUntilRef = useRef(0)
  const isAtBottomRef = useRef(true)
  const isPhysicallyAtBottomRef = useRef(true)
  const isAtBottomListenersRef = useRef(new Set<() => void>())

  // anchor state (imperative, not React state — avoid re-renders on every scroll)
  const isAnchoredRef = useRef(false)
  const userScrolledUpRef = useRef(false)
  const spacerRatchetRef = useRef(0)
  const programmaticScrollDepthRef = useRef(0)
  const lastUserScrollTopRef = useRef(0)
  const lastObservedScrollTopRef = useRef(0)
  const lastContainerInlineSizeRef = useRef(0)
  // tracks whether streaming is active — only follow scroll during streaming
  const liveStreamActiveRef = useRef(false)
  const followLiveOutputRef = useRef(false)
  const bottomScrollFrameRef = useRef<number | null>(null)
  const bottomSmoothScrollMonitorFrameRef = useRef<number | null>(null)
  const bottomSmoothScrollPendingRef = useRef(false)
  const anchorScrollMonitorFrameRef = useRef<number | null>(null)
  const anchorScrollSettleFrameRef = useRef<number | null>(null)
  const anchorScrollSettleFramesRef = useRef(0)
  const anchorActivationPendingRef = useRef(false)
  const viewportAnchorRef = useRef<ViewportAnchor | null>(null)

  const inputAreaHeight = useCallback(() => {
    const inputArea = inputAreaRef.current
    if (!inputArea) return SCROLL_BOTTOM_PAD
    const height = inputArea.getBoundingClientRect().height
    return Number.isFinite(height) && height > 0 ? height : SCROLL_BOTTOM_PAD
  }, [])

  const maxScrollTop = useCallback((container: HTMLDivElement) => {
    return Math.max(0, container.scrollHeight - container.clientHeight)
  }, [])

  const rememberScrollTop = useCallback((container: HTMLDivElement | null) => {
    if (!container) return
    lastObservedScrollTopRef.current = container.scrollTop
  }, [])

  const subscribeIsAtBottom = useCallback((listener: () => void) => {
    isAtBottomListenersRef.current.add(listener)
    return () => {
      isAtBottomListenersRef.current.delete(listener)
    }
  }, [])

  const getIsAtBottomSnapshot = useCallback(() => isAtBottomRef.current, [])

  const setAtBottomState = useCallback((atBottom: boolean) => {
    if (isAtBottomRef.current === atBottom) return
    isAtBottomRef.current = atBottom
    for (const listener of isAtBottomListenersRef.current) {
      listener()
    }
  }, [])

  const syncBottomState = useCallback((el: HTMLDivElement) => {
    const physicallyAtBottom = el.scrollHeight - el.scrollTop - el.clientHeight <= 80
    isPhysicallyAtBottomRef.current = physicallyAtBottom
    const anchoredViewLocked =
      isAnchoredRef.current &&
      !userScrolledUpRef.current &&
      !followLiveOutputRef.current
    const atBottom = physicallyAtBottom || anchoredViewLocked
    setAtBottomState(atBottom)
  }, [setAtBottomState])

  const shouldStickToBottom = useCallback(() => {
    return followLiveOutputRef.current || (isAtBottomRef.current && isPhysicallyAtBottomRef.current)
  }, [])

  const syncBottomStateFromContainer = useCallback(() => {
    const container = scrollContainerRef.current
    if (!container) return
    syncBottomState(container)
  }, [syncBottomState])

  const isLayoutWidthScroll = useCallback((container: HTMLDivElement) => {
    const width = container.clientWidth
    const previousWidth = lastContainerInlineSizeRef.current
    lastContainerInlineSizeRef.current = width
    return previousWidth > 0 && Math.abs(width - previousWidth) > LAYOUT_SCROLL_WIDTH_EPSILON
  }, [])

  const isLocalExpansionActive = useCallback(() => performance.now() < localExpansionActiveUntilRef.current, [])

  const clearBottomScrollFrame = useCallback(() => {
    if (bottomScrollFrameRef.current === null) return
    cancelAnimationFrame(bottomScrollFrameRef.current)
    bottomScrollFrameRef.current = null
    bottomSmoothScrollPendingRef.current = false
  }, [])

  const clearBottomSmoothScrollMonitor = useCallback(() => {
    bottomSmoothScrollPendingRef.current = false
    if (bottomSmoothScrollMonitorFrameRef.current === null) return
    cancelAnimationFrame(bottomSmoothScrollMonitorFrameRef.current)
    bottomSmoothScrollMonitorFrameRef.current = null
    programmaticScrollDepthRef.current = Math.max(0, programmaticScrollDepthRef.current - 1)
  }, [])

  const isBottomSmoothScrolling = useCallback(() => {
    return bottomSmoothScrollPendingRef.current || bottomSmoothScrollMonitorFrameRef.current !== null
  }, [])

  const clearAnchorScrollMonitor = useCallback(() => {
    if (anchorScrollMonitorFrameRef.current === null) return
    cancelAnimationFrame(anchorScrollMonitorFrameRef.current)
    anchorScrollMonitorFrameRef.current = null
    programmaticScrollDepthRef.current = Math.max(0, programmaticScrollDepthRef.current - 1)
  }, [])

  const isAnchorAnimating = useCallback(() => anchorScrollMonitorFrameRef.current !== null, [])

  const clearAnchorScrollSettleGuard = useCallback(() => {
    if (anchorScrollSettleFrameRef.current !== null) {
      cancelAnimationFrame(anchorScrollSettleFrameRef.current)
      anchorScrollSettleFrameRef.current = null
    }
    anchorScrollSettleFramesRef.current = 0
  }, [])

  const startAnchorScrollSettleGuard = useCallback(() => {
    clearAnchorScrollSettleGuard()
    anchorScrollSettleFramesRef.current = ANCHOR_SCROLL_SETTLE_FRAMES
    const tick = () => {
      anchorScrollSettleFramesRef.current = Math.max(0, anchorScrollSettleFramesRef.current - 1)
      if (anchorScrollSettleFramesRef.current <= 0) {
        anchorScrollSettleFrameRef.current = null
        return
      }
      anchorScrollSettleFrameRef.current = requestAnimationFrame(tick)
    }
    anchorScrollSettleFrameRef.current = requestAnimationFrame(tick)
  }, [clearAnchorScrollSettleGuard])

  const scrollViewportToBottom = useCallback((behavior: ScrollBehavior) => {
    const container = scrollContainerRef.current
    const bottom = bottomRef.current
    if (!container) return
    if (isAnchoredRef.current && !followLiveOutputRef.current) return

    clearAnchorScrollMonitor()
    if (behavior === 'instant') {
      clearBottomScrollFrame()
      clearBottomSmoothScrollMonitor()
    }

    const targetScroll = maxScrollTop(container)
    programmaticScrollDepthRef.current++

    if (behavior === 'instant') {
      container.scrollTop = targetScroll
      bottom?.scrollIntoView({ behavior: 'instant' })
    } else {
      container.scrollTo({ top: targetScroll, behavior })
    }
    rememberScrollTop(container)

    requestAnimationFrame(() => {
      programmaticScrollDepthRef.current--
      rememberScrollTop(container)
      syncBottomState(container)
    })
  }, [clearAnchorScrollMonitor, clearBottomScrollFrame, clearBottomSmoothScrollMonitor, maxScrollTop, rememberScrollTop, syncBottomState])

  const animateBottomIntoPlace = useCallback(() => {
    const container = scrollContainerRef.current
    if (!container) {
      bottomSmoothScrollPendingRef.current = false
      return
    }

    clearBottomSmoothScrollMonitor()

    programmaticScrollDepthRef.current++
    setAtBottomState(true)
    let targetScroll = maxScrollTop(container)
    container.scrollTo({ top: targetScroll, behavior: 'smooth' })
    rememberScrollTop(container)

    let frame = 0
    let stableFrames = 0
    let lastScrollTop = container.scrollTop
    let observedMovement = false
    const tick = () => {
      const currentContainer = scrollContainerRef.current
      if (!currentContainer) {
        bottomSmoothScrollMonitorFrameRef.current = null
        programmaticScrollDepthRef.current = Math.max(0, programmaticScrollDepthRef.current - 1)
        return
      }

      frame += 1
      const currentScrollTop = currentContainer.scrollTop
      const latestTargetScroll = maxScrollTop(currentContainer)
      if (Math.abs(latestTargetScroll - targetScroll) > ANCHOR_SCROLL_TARGET_EPSILON) {
        targetScroll = latestTargetScroll
        stableFrames = 0
        observedMovement = false
        currentContainer.scrollTo({ top: targetScroll, behavior: 'smooth' })
      }

      const nearTarget = Math.abs(currentScrollTop - targetScroll) <= ANCHOR_SCROLL_TARGET_EPSILON
      const stationary = Math.abs(currentScrollTop - lastScrollTop) <= 0.5
      if (!stationary) observedMovement = true
      stableFrames = nearTarget || stationary ? stableFrames + 1 : 0
      lastScrollTop = currentScrollTop

      if (observedMovement && stableFrames >= 2 && !nearTarget && frame < ANCHOR_SCROLL_MAX_MONITOR_FRAMES) {
        stableFrames = 0
        observedMovement = false
        currentContainer.scrollTo({ top: targetScroll, behavior: 'smooth' })
      }

      if (nearTarget || frame >= ANCHOR_SCROLL_MAX_MONITOR_FRAMES) {
        const finalTargetScroll = maxScrollTop(currentContainer)
        if (Math.abs(currentContainer.scrollTop - finalTargetScroll) > ANCHOR_SCROLL_TARGET_EPSILON) {
          currentContainer.scrollTop = finalTargetScroll
        }
        bottomSmoothScrollMonitorFrameRef.current = null
        programmaticScrollDepthRef.current = Math.max(0, programmaticScrollDepthRef.current - 1)
        rememberScrollTop(currentContainer)
        syncBottomState(currentContainer)
        return
      }

      bottomSmoothScrollMonitorFrameRef.current = requestAnimationFrame(tick)
    }

    bottomSmoothScrollMonitorFrameRef.current = requestAnimationFrame(tick)
  }, [clearBottomSmoothScrollMonitor, maxScrollTop, rememberScrollTop, setAtBottomState, syncBottomState])

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

      viewportAnchorRef.current = {
        element: currentAnchor.element,
        top: anchor.top,
        turnOffset: currentAnchor.turnOffset ?? anchor.turnOffset,
        path: currentAnchor.path ?? anchor.path,
      }
      programmaticScrollDepthRef.current++
      container.scrollTop += delta
      rememberScrollTop(container)
      syncBottomState(container)
      if (shouldPreserveViewport()) {
        captureViewportAnchor()
      }
      requestAnimationFrame(() => {
        programmaticScrollDepthRef.current--
        const freshContainer = scrollContainerRef.current
        if (!freshContainer) return
        rememberScrollTop(freshContainer)
        syncBottomState(freshContainer)
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
    const targetScrollTop = Math.max(0, offsetInContainer(turn) + anchor.turnOffset - markerTop)
    viewportAnchorRef.current = {
      element: null,
      top: markerTop,
      turnOffset: anchor.turnOffset,
      path: anchor.path,
    }
    programmaticScrollDepthRef.current++
    container.scrollTop = targetScrollTop
    rememberScrollTop(container)
    syncBottomState(container)
    if (shouldPreserveViewport()) {
      captureViewportAnchor()
    }
    requestAnimationFrame(() => {
      programmaticScrollDepthRef.current--
      const freshContainer = scrollContainerRef.current
      if (!freshContainer) return
      rememberScrollTop(freshContainer)
      syncBottomState(freshContainer)
    })
  }, [captureViewportAnchor, contentRoot, findViewportAnchor, offsetInContainer, rememberScrollTop, resolvePathFromRoot, shouldPreserveViewport, syncBottomState])

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
    let needed = Math.max(0, viewportH - turnH - inputAreaHeight() - SCROLL_TOP_OFFSET)

    if (userScrolledUpRef.current) {
      // ratchet: only allow decrease
      needed = Math.min(needed, spacerRatchetRef.current)
    } else {
      spacerRatchetRef.current = needed
    }

    spacer.style.height = needed + 'px'
    spacerRatchetRef.current = needed
  }, [inputAreaHeight])

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

    clearAnchorScrollMonitor()

    programmaticScrollDepthRef.current++
    container.scrollTop = targetScroll
    rememberScrollTop(container)
    requestAnimationFrame(() => {
      programmaticScrollDepthRef.current--
      rememberScrollTop(container)
      syncBottomState(container)
    })
  }, [anchorScrollTop, clearAnchorScrollMonitor, rememberScrollTop, syncBottomState])

  const animateAnchorIntoPlace = useCallback(() => {
    const container = scrollContainerRef.current
    const initialTargetScroll = anchorScrollTop()
    if (!container || initialTargetScroll == null) return

    clearAnchorScrollMonitor()

    if (Math.abs(container.scrollTop - initialTargetScroll) <= ANCHOR_SCROLL_TARGET_EPSILON) {
      scrollToAnchor()
      return
    }

    programmaticScrollDepthRef.current++
    setAtBottomState(true)
    let targetScroll = initialTargetScroll
    container.scrollTo({ top: targetScroll, behavior: 'smooth' })
    rememberScrollTop(container)

    let frame = 0
    let stableFrames = 0
    let lastScrollTop = container.scrollTop
    let observedMovement = false
    const tick = () => {
      const currentContainer = scrollContainerRef.current
      if (!currentContainer) {
        anchorScrollMonitorFrameRef.current = null
        programmaticScrollDepthRef.current = Math.max(0, programmaticScrollDepthRef.current - 1)
        return
      }

      frame += 1
      const currentScrollTop = currentContainer.scrollTop
      const latestTargetScroll = anchorScrollTop()
      if (
        latestTargetScroll != null &&
        Math.abs(latestTargetScroll - targetScroll) > ANCHOR_SCROLL_TARGET_EPSILON
      ) {
        targetScroll = latestTargetScroll
        stableFrames = 0
        observedMovement = false
        currentContainer.scrollTo({ top: targetScroll, behavior: 'smooth' })
      }
      const nearTarget = Math.abs(currentScrollTop - targetScroll) <= ANCHOR_SCROLL_TARGET_EPSILON
      const stationary = Math.abs(currentScrollTop - lastScrollTop) <= 0.5
      if (!stationary) observedMovement = true
      stableFrames = nearTarget || stationary ? stableFrames + 1 : 0
      lastScrollTop = currentScrollTop

      if (observedMovement && stableFrames >= 2 && !nearTarget && frame < ANCHOR_SCROLL_MAX_MONITOR_FRAMES) {
        stableFrames = 0
        observedMovement = false
        currentContainer.scrollTo({ top: targetScroll, behavior: 'smooth' })
      }

      if (nearTarget || frame >= ANCHOR_SCROLL_MAX_MONITOR_FRAMES) {
        const finalTargetScroll = anchorScrollTop() ?? targetScroll
        if (Math.abs(currentContainer.scrollTop - finalTargetScroll) > ANCHOR_SCROLL_TARGET_EPSILON) {
          currentContainer.scrollTop = finalTargetScroll
        }
        anchorScrollMonitorFrameRef.current = null
        programmaticScrollDepthRef.current = Math.max(0, programmaticScrollDepthRef.current - 1)
        startAnchorScrollSettleGuard()
        rememberScrollTop(currentContainer)
        syncBottomState(currentContainer)
        return
      }

      anchorScrollMonitorFrameRef.current = requestAnimationFrame(tick)
    }

    anchorScrollMonitorFrameRef.current = requestAnimationFrame(tick)
  }, [anchorScrollTop, clearAnchorScrollMonitor, rememberScrollTop, scrollToAnchor, setAtBottomState, startAnchorScrollSettleGuard, syncBottomState])

  const collapseSpacer = useCallback(() => {
    clearBottomScrollFrame()
    clearBottomSmoothScrollMonitor()
    clearAnchorScrollMonitor()
    clearAnchorScrollSettleGuard()
    isAnchoredRef.current = false
    userScrolledUpRef.current = false
    spacerRatchetRef.current = 0
    anchorActivationPendingRef.current = false
    viewportAnchorRef.current = null
    if (spacerRef.current) spacerRef.current.style.height = '0px'
  }, [clearAnchorScrollMonitor, clearAnchorScrollSettleGuard, clearBottomScrollFrame, clearBottomSmoothScrollMonitor])

  // scroll-to-bottom button: collapse spacer and scroll to actual bottom
  const scrollToBottom = useCallback(() => {
    const shouldAnimate = !liveStreamActiveRef.current
    collapseSpacer()
    followLiveOutputRef.current = true
    viewportAnchorRef.current = null
    clearBottomScrollFrame()
    bottomSmoothScrollPendingRef.current = shouldAnimate
    bottomScrollFrameRef.current = requestAnimationFrame(() => {
      bottomScrollFrameRef.current = null
      if (shouldAnimate) {
        animateBottomIntoPlace()
      } else {
        bottomSmoothScrollPendingRef.current = false
        scrollViewportToBottom('instant')
      }
      setAtBottomState(true)
    })
  }, [animateBottomIntoPlace, clearBottomScrollFrame, collapseSpacer, scrollViewportToBottom, setAtBottomState])

  // activate anchor on the current lastUserMsg turn
  const activateAnchor = useCallback(() => {
    if (promptPinningDisabled) {
      scrollToBottom()
      return
    }

    clearBottomScrollFrame()
    clearBottomSmoothScrollMonitor()
    anchorActivationPendingRef.current = true
    isAnchoredRef.current = true
    userScrolledUpRef.current = false
    spacerRatchetRef.current = 0
    followLiveOutputRef.current = false
    viewportAnchorRef.current = null
    setAtBottomState(true)
    const finishActivation = (remainingFrames: number) => {
      if (!anchorActivationPendingRef.current) return
      const turn = lastUserMsgRef.current
      if (!turn) {
        if (remainingFrames > 0) {
          requestAnimationFrame(() => finishActivation(remainingFrames - 1))
          return
        }
        anchorActivationPendingRef.current = false
        isAnchoredRef.current = false
        userScrolledUpRef.current = false
        spacerRatchetRef.current = 0
        followLiveOutputRef.current = false
        viewportAnchorRef.current = null
        syncBottomStateFromContainer()
        return
      }

      anchorActivationPendingRef.current = false
      isAnchoredRef.current = true
      userScrolledUpRef.current = false
      spacerRatchetRef.current = 0
      followLiveOutputRef.current = false
      viewportAnchorRef.current = null
      setAtBottomState(true)

      recalcSpacer()
      animateAnchorIntoPlace()
    }

    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        finishActivation(6)
      })
    })
  }, [animateAnchorIntoPlace, clearBottomScrollFrame, clearBottomSmoothScrollMonitor, promptPinningDisabled, recalcSpacer, scrollToBottom, setAtBottomState, syncBottomStateFromContainer])

  const stickToBottomAfterLayoutScroll = useCallback((container: HTMLDivElement) => {
    programmaticScrollDepthRef.current++
    container.scrollTop = maxScrollTop(container)
    rememberScrollTop(container)
    setAtBottomState(true)
    requestAnimationFrame(() => {
      programmaticScrollDepthRef.current = Math.max(0, programmaticScrollDepthRef.current - 1)
      const freshContainer = scrollContainerRef.current
      if (!freshContainer) return
      rememberScrollTop(freshContainer)
      syncBottomState(freshContainer)
    })
  }, [maxScrollTop, rememberScrollTop, setAtBottomState, syncBottomState])

  // scroll handler: ratchet logic
  const handleScrollContainerScroll = useCallback(() => {
    const el = scrollContainerRef.current
    if (!el) return
    const previousScrollTop = lastObservedScrollTopRef.current
    const currentScrollTop = el.scrollTop
    const layoutWidthScroll = isLayoutWidthScroll(el)

    // ignore programmatic scrolls
    if (programmaticScrollDepthRef.current > 0) {
      rememberScrollTop(el)
      return
    }

    if (layoutWidthScroll) {
      if (isAnchoredRef.current) {
        recalcSpacer()
        if (userScrolledUpRef.current) {
          preserveViewportAnchor()
        } else {
          scrollToAnchor()
        }
        syncBottomState(el)
        return
      }
      if (shouldStickToBottom()) {
        stickToBottomAfterLayoutScroll(el)
        return
      }
      syncBottomState(el)
      rememberScrollTop(el)
      if (shouldPreserveViewport()) {
        captureViewportAnchor()
      }
      return
    }

    syncBottomState(el)
    rememberScrollTop(el)

    if (anchorActivationPendingRef.current) return
    const userScrolledUp = currentScrollTop < previousScrollTop - 0.5
    if (followLiveOutputRef.current && userScrolledUp) {
      followLiveOutputRef.current = false
      setAtBottomState(false)
    }
    if (!isAtBottomRef.current) {
      followLiveOutputRef.current = false
    }
    if (shouldPreserveViewport()) {
      captureViewportAnchor()
    } else {
      viewportAnchorRef.current = null
    }
    if (!isAnchoredRef.current) return

    const st = currentScrollTop
    const anchorTarget = anchorScrollTop() ?? 0

    if (userScrolledUpRef.current) {
      // check if user scrolled back to anchor zone — clear the scrolled-up flag
      if (st >= anchorTarget - 10) {
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
      if (currentScrollTop > previousScrollTop + 0.5) {
        if (anchorScrollSettleFramesRef.current > 0) {
          scrollToAnchor()
          syncBottomState(el)
          return
        }
        followLiveOutputRef.current = false
        setAtBottomState(false)
        collapseSpacer()
        captureViewportAnchor()
        return
      }

      // detect: user scrolled above the pinned prompt position
      if (st < anchorTarget - 10) {
        userScrolledUpRef.current = true
        lastUserScrollTopRef.current = st
        captureViewportAnchor()
        syncBottomState(el)
      }
    }
  }, [syncBottomState, rememberScrollTop, isLayoutWidthScroll, recalcSpacer, preserveViewportAnchor, scrollToAnchor, shouldStickToBottom, stickToBottomAfterLayoutScroll, shouldPreserveViewport, captureViewportAnchor, setAtBottomState, collapseSpacer, anchorScrollTop])

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
    localExpansionActiveUntilRef.current = startedAt + 420
    followLiveOutputRef.current = false

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
  // keep liveStreamActive in sync for effects that read it
  useEffect(() => {
    liveStreamActiveRef.current = liveAssistantTurn != null || liveRunUiVisible
  }, [liveAssistantTurn, liveRunUiVisible])

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

    if (!promptPinningDisabled && turnH > viewportH * 0.5) {
      // long turn: pin user prompt to top with spacer
      isAnchoredRef.current = true
      userScrolledUpRef.current = false
      spacerRatchetRef.current = 0
      followLiveOutputRef.current = false
      recalcSpacer()

      programmaticScrollDepthRef.current++
      container.scrollTop = Math.max(0, offsetInContainer(turn) - SCROLL_TOP_OFFSET)
      rememberScrollTop(container)
      requestAnimationFrame(() => {
        programmaticScrollDepthRef.current--
        rememberScrollTop(container)
      })
      syncBottomState(container)
    } else {
      // short turn: scroll to natural bottom, no spacer
      container.scrollTop = container.scrollHeight - viewportH
      rememberScrollTop(container)
      syncBottomState(container)
    }
  }, [messagesLoading, promptPinningDisabled, recalcSpacer, collapseSpacer, offsetInContainer, rememberScrollTop, syncBottomState])

  useEffect(() => {
    if (!promptPinningDisabled) return
    collapseSpacer()
  }, [collapseSpacer, promptPinningDisabled])

  useLayoutEffect(() => {
    if (messagesLoading) return
    if (shouldPreserveViewport()) {
      preserveViewportAnchor()
    }
  }, [messages, liveAssistantTurn, liveRunUiVisible, messagesLoading, preserveViewportAnchor, shouldPreserveViewport])

  useLayoutEffect(() => {
    const container = scrollContainerRef.current
    if (!container) return
    lastContainerInlineSizeRef.current = container.clientWidth
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

    if (!shouldStickToBottom()) {
      if (shouldPreserveViewport()) {
        preserveViewportAnchor()
      }
      return
    }
    const forceInstant = forceInstantBottomScrollRef.current
    if (isBottomSmoothScrolling() && !forceInstant) {
      setAtBottomState(true)
      return
    }
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
  }, [messages, liveAssistantTurn, liveRunUiVisible, preserveViewportAnchor, recalcSpacer, scrollToAnchor, scrollViewportToBottom, shouldPreserveViewport, captureViewportAnchor, shouldStickToBottom, isAnchorAnimating, isBottomSmoothScrolling, setAtBottomState])

  // ResizeObserver on anchor turn: recalc spacer when turn content changes size
  useEffect(() => {
    const turn = lastUserMsgRef.current
    if (!turn || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver((entries) => {
      if (!entries.some(hasResizeObserverBlockChange)) return
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

      if (isLocalExpansionActive()) return
      if (shouldStickToBottom()) {
        if (isBottomSmoothScrolling()) return
        scrollViewportToBottom('instant')
        return
      }
      if (shouldPreserveViewport()) {
        preserveViewportAnchor()
      }
    })
    ro.observe(turn)
    return () => ro.disconnect()
  }, [messages, liveAssistantTurn, preserveViewportAnchor, recalcSpacer, scrollToAnchor, scrollViewportToBottom, shouldPreserveViewport, syncBottomState, isLocalExpansionActive, shouldStickToBottom, isAnchorAnimating, isBottomSmoothScrolling])

  useEffect(() => {
    const root = contentRoot()
    if (!root || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver((entries) => {
      if (!entries.some(hasResizeObserverBlockChange)) return
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

      if (isLocalExpansionActive()) return
      if (shouldStickToBottom()) {
        if (isBottomSmoothScrolling()) return
        scrollViewportToBottom('instant')
        return
      }
      if (shouldPreserveViewport()) {
        preserveViewportAnchor()
      }
    })
    ro.observe(root)
    return () => ro.disconnect()
  }, [contentRoot, preserveViewportAnchor, recalcSpacer, scrollToAnchor, scrollViewportToBottom, shouldPreserveViewport, syncBottomState, isLocalExpansionActive, shouldStickToBottom, isAnchorAnimating, isBottomSmoothScrolling])

  useEffect(() => {
    const el = copCodeExecScrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [topLevelCodeExecutionsLength, liveAssistantTurn])

  // input area resize observer
  useEffect(() => {
    const el = inputAreaRef.current
    if (!el) return
    const syncInputAreaHeight = () => {
      document.documentElement.style.setProperty('--chat-input-area-height', `${Math.ceil(inputAreaHeight())}px`)
    }
    if (typeof ResizeObserver === 'undefined') {
      syncInputAreaHeight()
      return
    }
    syncInputAreaHeight()
    const ro = new ResizeObserver(() => {
      syncInputAreaHeight()
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

      if (shouldStickToBottom()) {
        if (isBottomSmoothScrolling()) return
        scrollViewportToBottom('instant')
        return
      }
      if (shouldPreserveViewport()) {
        preserveViewportAnchor()
      }
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [inputAreaHeight, preserveViewportAnchor, recalcSpacer, scrollToAnchor, scrollViewportToBottom, shouldPreserveViewport, syncBottomStateFromContainer, shouldStickToBottom, isAnchorAnimating, isBottomSmoothScrolling])

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

      if (shouldStickToBottom()) {
        if (isBottomSmoothScrolling()) return
        scrollViewportToBottom('instant')
        return
      }
      if (shouldPreserveViewport()) {
        preserveViewportAnchor()
      }
    }
    window.addEventListener('resize', handler)
    return () => window.removeEventListener('resize', handler)
  }, [preserveViewportAnchor, recalcSpacer, scrollToAnchor, scrollViewportToBottom, shouldPreserveViewport, syncBottomStateFromContainer, shouldStickToBottom, isAnchorAnimating, isBottomSmoothScrolling])

  // cleanup animation frames on unmount
  useEffect(() => {
    return () => {
      clearAnchorScrollMonitor()
      clearAnchorScrollSettleGuard()
      clearBottomScrollFrame()
      clearBottomSmoothScrollMonitor()
      if (documentPanelScrollFrameRef.current !== null) {
        cancelAnimationFrame(documentPanelScrollFrameRef.current)
      }
      anchorActivationPendingRef.current = false
    }
  }, [clearAnchorScrollMonitor, clearAnchorScrollSettleGuard, clearBottomScrollFrame, clearBottomSmoothScrollMonitor])

  return {
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
    subscribeIsAtBottom,
    getIsAtBottomSnapshot,
  }
}
