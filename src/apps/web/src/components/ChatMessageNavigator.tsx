import { memo, useCallback, useEffect, useMemo, useRef, useState, type RefObject } from 'react'
import { ChevronDown, ChevronUp } from 'lucide-react'
import type { AgentMessage } from '../agent-ui'
import { messageTextContent } from '../messageContent'
import { ActionIconButton } from './ActionIconButton'
import { useLocale } from '../contexts/LocaleContext'

type ChatMessageNavigatorProps = {
  messages: AgentMessage[]
  scrollContainerRef: RefObject<HTMLDivElement | null>
}

type NavItem = {
  id: string
  role: 'user' | 'assistant'
  preview: string
  index: number
}

const activeScanRatio = 0.42
const scrollTopOffset = 52

function normalizePreview(message: AgentMessage): string {
  const text = messageTextContent(message).replace(/\s+/g, ' ').trim()
  return text || '...'
}

function messageElement(id: string): HTMLElement | null {
  for (const el of document.querySelectorAll('[data-message-anchor-id]')) {
    if (el instanceof HTMLElement && el.dataset.messageAnchorId === id) return el
  }
  for (const el of document.querySelectorAll('[data-message-id]')) {
    if (el instanceof HTMLElement && el.dataset.messageId === id) return el
  }
  return null
}

function activeItemId(items: NavItem[], container: HTMLDivElement): string | null {
  const containerRect = container.getBoundingClientRect()
  const scanY = containerRect.top + containerRect.height * activeScanRatio
  let nextId: string | null = null
  let bestDistance = Number.POSITIVE_INFINITY

  for (const item of items) {
    const el = messageElement(item.id)
    if (!el) continue
    const rect = el.getBoundingClientRect()
    const distance = rect.top <= scanY && rect.bottom >= scanY
      ? 0
      : Math.min(Math.abs(rect.top - scanY), Math.abs(rect.bottom - scanY))
    if (distance < bestDistance) {
      bestDistance = distance
      nextId = item.id
    }
  }

  return nextId
}

function revealHistoricAnchorsThrough(id: string): void {
  let found = false
  for (const el of document.querySelectorAll('[data-message-anchor-id]')) {
    if (!(el instanceof HTMLElement)) continue
    el.style.contentVisibility = 'visible'
    if (el.dataset.messageAnchorId === id) {
      found = true
      break
    }
  }
  if (found) return
  for (const el of document.querySelectorAll('[data-message-anchor-id]')) {
    if (el instanceof HTMLElement) el.style.contentVisibility = 'visible'
  }
}

function topInsideContainer(el: HTMLElement, container: HTMLElement): number {
  const containerRect = container.getBoundingClientRect()
  const targetRect = el.getBoundingClientRect()
  return container.scrollTop + targetRect.top - containerRect.top
}

function scrollTargetTop(el: HTMLElement, container: HTMLElement): number {
  const focusOffset = Math.max(scrollTopOffset, container.clientHeight * activeScanRatio)
  return topInsideContainer(el, container) - focusOffset
}

function afterLayout(callback: () => void): void {
  window.requestAnimationFrame(() => {
    window.requestAnimationFrame(callback)
  })
}

function keepLineVisible(container: HTMLElement, line: HTMLElement): void {
  const containerRect = container.getBoundingClientRect()
  const lineRect = line.getBoundingClientRect()
  let nextTop = container.scrollTop

  if (lineRect.top < containerRect.top) {
    nextTop += lineRect.top - containerRect.top
  } else if (lineRect.bottom > containerRect.bottom) {
    nextTop += lineRect.bottom - containerRect.bottom
  } else {
    return
  }

  const behavior = window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth'
  container.scrollTo({ top: nextTop, behavior })
}

export const ChatMessageNavigator = memo(function ChatMessageNavigator({
  messages,
  scrollContainerRef,
}: ChatMessageNavigatorProps) {
  const { t } = useLocale()
  const rootRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const programmaticTargetIdRef = useRef<string | null>(null)
  const programmaticScrollTimerRef = useRef<number | null>(null)
  const [activeId, setActiveId] = useState<string | null>(null)
  const [hoveredId, setHoveredId] = useState<string | null>(null)
  const [previewTop, setPreviewTop] = useState(0)

  const items = useMemo<NavItem[]>(() => messages
    .filter((message): message is AgentMessage & { role: 'user' | 'assistant' } =>
      message.role === 'user' || message.role === 'assistant')
    .map((message) => ({
      id: message.id,
      role: message.role,
      preview: normalizePreview(message),
      index: messages.findIndex((item) => item.id === message.id),
    })), [messages])

  const hoveredItem = useMemo(
    () => items.find((item) => item.id === hoveredId) ?? null,
    [hoveredId, items],
  )
  const assistantTurnCount = useMemo(
    () => items.filter((item) => item.role === 'assistant').length,
    [items],
  )
  const updateActiveMessage = useCallback(() => {
    const container = scrollContainerRef.current
    if (!container || items.length === 0) {
      setActiveId(null)
      return
    }

    if (programmaticTargetIdRef.current) {
      setActiveId(programmaticTargetIdRef.current)
      return
    }

    setActiveId(activeItemId(items, container))
  }, [items, scrollContainerRef])

  useEffect(() => {
    const container = scrollContainerRef.current
    if (!container) return

    let frame = 0
    const schedule = () => {
      if (frame) return
      frame = window.requestAnimationFrame(() => {
        frame = 0
        updateActiveMessage()
      })
    }

    schedule()
    container.addEventListener('scroll', schedule, { passive: true })
    window.addEventListener('resize', schedule)
    return () => {
      if (frame) window.cancelAnimationFrame(frame)
      container.removeEventListener('scroll', schedule)
      window.removeEventListener('resize', schedule)
    }
  }, [scrollContainerRef, updateActiveMessage])

  useEffect(() => () => {
    if (programmaticScrollTimerRef.current) window.clearTimeout(programmaticScrollTimerRef.current)
  }, [])

  useEffect(() => {
    const list = listRef.current
    if (!list || !activeId) return

    const line = list.querySelector(`[data-message-nav-id="${CSS.escape(activeId)}"]`)
    if (!(line instanceof HTMLElement)) return
    keepLineVisible(list, line)
  }, [activeId])

  const scrollToItem = useCallback((id: string) => {
    const container = scrollContainerRef.current
    if (!container) return
    if (programmaticScrollTimerRef.current) {
      window.clearTimeout(programmaticScrollTimerRef.current)
      programmaticScrollTimerRef.current = null
    }
    programmaticTargetIdRef.current = id
    setActiveId(id)
    container.scrollTo({ top: container.scrollTop, behavior: 'auto' })

    const previousOverflowAnchor = container.style.overflowAnchor
    container.style.overflowAnchor = 'none'
    revealHistoricAnchorsThrough(id)

    afterLayout(() => {
      const el = messageElement(id)
      if (!el) {
        container.style.overflowAnchor = previousOverflowAnchor
        return
      }
      const targetTop = scrollTargetTop(el, container)
      container.scrollTo({
        top: Math.max(0, targetTop),
        behavior: 'smooth',
      })
      programmaticScrollTimerRef.current = window.setTimeout(() => {
        container.style.overflowAnchor = previousOverflowAnchor
        programmaticTargetIdRef.current = null
        programmaticScrollTimerRef.current = null
        setActiveId(activeItemId(items, container) ?? id)
      }, 700)
    })
  }, [items, scrollContainerRef])

  const jumpBy = useCallback((direction: -1 | 1) => {
    const activeIndex = activeId ? items.findIndex((item) => item.id === activeId) : -1
    if (activeIndex < 0) return

    const target = items[activeIndex + direction]
    if (!target) return
    scrollToItem(target.id)
  }, [activeId, items, scrollToItem])

  const handleItemHover = useCallback((id: string, button: HTMLButtonElement) => {
    const root = rootRef.current
    if (!root) return
    const rootRect = root.getBoundingClientRect()
    const buttonRect = button.getBoundingClientRect()
    setPreviewTop(buttonRect.top + buttonRect.height / 2 - rootRect.top)
    setHoveredId(id)
  }, [])

  if (assistantTurnCount <= 1) return null

  const activeIndex = activeId ? items.findIndex((item) => item.id === activeId) : -1
  const canJumpPrev = activeIndex > 0
  const canJumpNext = activeIndex >= 0 && activeIndex < items.length - 1
  const prevTooltip = canJumpPrev ? t.previousResponse : t.alreadyAtFirstResponse
  const nextTooltip = canJumpNext ? t.nextResponse : t.alreadyAtLastResponse

  return (
    <div
      ref={rootRef}
      className="chat-message-navigator"
      onMouseLeave={() => setHoveredId(null)}
    >
      <ActionIconButton
        className="chat-message-navigator-arrow chat-message-navigator-arrow-up"
        style={{
          width: 24,
          height: 24,
          padding: 0,
          alignItems: 'center',
          justifyContent: 'center',
          borderRadius: '6.5px',
          color: 'var(--c-text-tertiary)',
          cursor: canJumpPrev ? 'pointer' : 'not-allowed',
        }}
        hoverBackground="var(--c-bg-deep)"
        tooltip={prevTooltip}
        onClick={() => { if (canJumpPrev) jumpBy(-1) }}
        onMouseEnter={() => setHoveredId(null)}
        data-disabled={!canJumpPrev ? 'true' : undefined}
        aria-label={prevTooltip}
      >
        <ChevronUp size={13} strokeWidth={2.1} />
      </ActionIconButton>

      <div ref={listRef} className="chat-message-navigator-list">
        {items.map((item) => {
          const selected = item.id === activeId
          const hovered = item.id === hoveredId
          return (
            <button
              key={item.id}
              type="button"
              className="chat-message-navigator-line"
              data-message-nav-id={item.id}
              data-role={item.role}
              data-active={selected ? 'true' : undefined}
              data-hovered={hovered ? 'true' : undefined}
              onClick={() => scrollToItem(item.id)}
              onMouseEnter={(event) => handleItemHover(item.id, event.currentTarget)}
              aria-label={item.preview}
            />
          )
        })}
      </div>

      <ActionIconButton
        className="chat-message-navigator-arrow chat-message-navigator-arrow-down"
        style={{
          width: 24,
          height: 24,
          padding: 0,
          alignItems: 'center',
          justifyContent: 'center',
          borderRadius: '6.5px',
          color: 'var(--c-text-tertiary)',
          cursor: canJumpNext ? 'pointer' : 'not-allowed',
        }}
        hoverBackground="var(--c-bg-deep)"
        tooltip={nextTooltip}
        onClick={() => { if (canJumpNext) jumpBy(1) }}
        onMouseEnter={() => setHoveredId(null)}
        data-disabled={!canJumpNext ? 'true' : undefined}
        aria-label={nextTooltip}
      >
        <ChevronDown size={13} strokeWidth={2.1} />
      </ActionIconButton>

      {hoveredItem && (
        <div
          className="chat-message-navigator-preview"
          data-role={hoveredItem.role}
          style={{ top: previewTop }}
        >
          {hoveredItem.role === 'assistant' && (
            <div className="chat-message-navigator-preview-label">Assistant</div>
          )}
          <div className="chat-message-navigator-preview-text">{hoveredItem.preview}</div>
        </div>
      )}
    </div>
  )
})
