import { memo, useCallback, useEffect, useMemo, useRef, useState, type DragEvent, type ReactNode, type WheelEvent } from 'react'
import { FileText, FolderOpen, Globe2, Plus, X } from 'lucide-react'
import { iconButtonSmCls } from './buttonStyles'
import { DropdownAction } from './DropdownAction'
import { rightPanelIconButtonCls, rightPanelIconButtonSize, rightPanelIconSize } from './rightPanelControls'
import { useLocale } from '../contexts/LocaleContext'
import './RightPanel.css'

export type RightPanelTab = {
  id: string
  title: string
  kind: 'web' | 'files' | 'source' | 'code' | 'agent' | 'resource'
  content: ReactNode
  closable?: boolean
  icon?: ReactNode
  hideTitle?: boolean
}

export type RightPanelAddOption = {
  id: string
  label: string
  icon: ReactNode
  disabled?: boolean
  onSelect: () => void
}

type Props = {
  tabs: RightPanelTab[]
  activeTabId: string | null
  onSelectTab: (id: string) => void
  onCloseTab?: (id: string) => void
  tabOrder?: string[]
  onTabOrderChange?: (ids: string[]) => void
  addOptions?: RightPanelAddOption[]
  addLabel?: string
  emptyLabel?: string
}

function TabIcon({ kind }: { kind: RightPanelTab['kind'] }) {
  if (kind === 'web') return <Globe2 size={rightPanelIconSize} />
  if (kind === 'files') return <FolderOpen size={rightPanelIconSize} />
  return <FileText size={rightPanelIconSize} />
}

type DropIndicator = {
  targetId: string
  side: 'before' | 'after'
}

function sameStringArray(left: string[], right: string[]): boolean {
  return left.length === right.length && left.every((item, index) => item === right[index])
}

export const RightPanel = memo(function RightPanel({
  tabs,
  activeTabId,
  onSelectTab,
  onCloseTab,
  tabOrder,
  onTabOrderChange,
  addOptions = [],
  addLabel,
  emptyLabel,
}: Props) {
  const { t } = useLocale()
  const activeTab = tabs.find((tab) => tab.id === activeTabId) ?? tabs[0] ?? null
  const [addMenuOpen, setAddMenuOpen] = useState(false)
  const [orderedTabIds, setOrderedTabIds] = useState<string[]>(() => tabs.map((tab) => tab.id))
  const [draggingTabId, setDraggingTabId] = useState<string | null>(null)
  const [dropIndicator, setDropIndicator] = useState<DropIndicator | null>(null)
  const [scrollFade, setScrollFade] = useState({ left: false, right: false })
  const addMenuRef = useRef<HTMLDivElement | null>(null)
  const trackRef = useRef<HTMLDivElement | null>(null)
  const draggingTabIdRef = useRef<string | null>(null)
  const suppressClickRef = useRef(false)
  const effectiveAddLabel = addLabel ?? t.rightPanel.newTab
  const effectiveEmptyLabel = emptyLabel ?? t.rightPanel.empty

  const updateScrollFade = useCallback(() => {
    const track = trackRef.current
    if (!track) return
    const maxScrollLeft = track.scrollWidth - track.clientWidth
    setScrollFade({
      left: track.scrollLeft > 1,
      right: track.scrollLeft < maxScrollLeft - 1,
    })
  }, [])

  const normalizeOrder = useCallback((current: string[]) => {
    const ids = tabs.map((tab) => tab.id)
    const kept = current.filter((id) => ids.includes(id))
    const added = ids.filter((id) => !kept.includes(id))
    return [...kept, ...added]
  }, [tabs])

  const currentTabOrder = tabOrder ?? orderedTabIds
  const normalizedCurrentTabOrder = useMemo(() => normalizeOrder(currentTabOrder), [currentTabOrder, normalizeOrder])

  useEffect(() => {
    if (!tabOrder) return
    if (!sameStringArray(normalizedCurrentTabOrder, tabOrder)) onTabOrderChange?.(normalizedCurrentTabOrder)
  }, [normalizedCurrentTabOrder, onTabOrderChange, tabOrder])

  const orderedTabs = useMemo(() => {
    const byId = new Map(tabs.map((tab) => [tab.id, tab]))
    return normalizedCurrentTabOrder.map((id) => byId.get(id)).filter((tab): tab is RightPanelTab => !!tab)
  }, [normalizedCurrentTabOrder, tabs])

  useEffect(() => {
    updateScrollFade()
    const track = trackRef.current
    if (!track) return
    if (typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(updateScrollFade)
    observer.observe(track)
    return () => observer.disconnect()
  }, [orderedTabs, updateScrollFade])

  const moveTab = (dragId: string, targetId: string, side: DropIndicator['side']) => {
    if (dragId === targetId) return
    const current = normalizedCurrentTabOrder
    if (!current.includes(dragId) || !current.includes(targetId)) return
    const next = current.filter((id) => id !== dragId)
    const targetIndex = next.indexOf(targetId)
    if (targetIndex < 0) return
    const insertIndex = side === 'after' ? targetIndex + 1 : targetIndex
    next.splice(insertIndex, 0, dragId)
    if (tabOrder) {
      onTabOrderChange?.(next)
    } else {
      setOrderedTabIds(next)
    }
  }

  const handleTrackWheel = (event: WheelEvent<HTMLDivElement>) => {
    const track = event.currentTarget
    if (track.scrollWidth <= track.clientWidth) return
    const delta = Math.abs(event.deltaX) > Math.abs(event.deltaY) ? event.deltaX : event.deltaY
    if (delta === 0) return
    event.preventDefault()
    track.scrollLeft += delta
    updateScrollFade()
  }

  const handleTabDragStart = (event: DragEvent<HTMLButtonElement>, id: string) => {
    draggingTabIdRef.current = id
    setDraggingTabId(id)
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('text/plain', id)
  }

  const handleTabDragOver = (event: DragEvent<HTMLButtonElement>, id: string) => {
    const dragId = draggingTabIdRef.current
    if (!dragId) return
    if (dragId === id) {
      setDropIndicator(null)
      return
    }
    event.preventDefault()
    event.dataTransfer.dropEffect = 'move'
    suppressClickRef.current = true
    const rect = event.currentTarget.getBoundingClientRect()
    setDropIndicator({
      targetId: id,
      side: event.clientX < rect.left + rect.width / 2 ? 'before' : 'after',
    })
  }

  const handleTabDrop = (event: DragEvent<HTMLButtonElement>, id: string) => {
    const dragId = draggingTabIdRef.current
    if (!dragId || dragId === id) return
    event.preventDefault()
    const rect = event.currentTarget.getBoundingClientRect()
    moveTab(dragId, id, event.clientX < rect.left + rect.width / 2 ? 'before' : 'after')
    setDropIndicator(null)
  }

  const handleTabDragEnd = () => {
    draggingTabIdRef.current = null
    setDraggingTabId(null)
    setDropIndicator(null)
    window.setTimeout(() => {
      suppressClickRef.current = false
    }, 0)
  }

  useEffect(() => {
    if (!addMenuOpen) return
    const handlePointerDown = (event: PointerEvent) => {
      if (addMenuRef.current?.contains(event.target as Node)) return
      setAddMenuOpen(false)
    }
    window.addEventListener('pointerdown', handlePointerDown)
    return () => window.removeEventListener('pointerdown', handlePointerDown)
  }, [addMenuOpen])

  return (
    <div style={{ height: '100%', width: '100%', minWidth: 0, display: 'flex', flexDirection: 'column', background: 'var(--c-bg-page)' }}>
      <div className="right-panel-tabbar">
        <div
          ref={trackRef}
          className={[
            'right-panel-tabbar__track',
            scrollFade.left ? 'right-panel-tabbar__track--fade-left' : '',
            scrollFade.right ? 'right-panel-tabbar__track--fade-right' : '',
          ].filter(Boolean).join(' ')}
          onWheel={handleTrackWheel}
          onScroll={updateScrollFade}
        >
          {orderedTabs.map((tab) => {
            const active = tab.id === activeTab?.id
            const closable = tab.closable !== false && !!onCloseTab
            const iconOnlyClosable = closable && tab.hideTitle
            const icon = tab.icon ?? <TabIcon kind={tab.kind} />
            return (
              <button
                key={tab.id}
                type="button"
                onClick={() => {
                  if (suppressClickRef.current) {
                    suppressClickRef.current = false
                    return
                  }
                  if (iconOnlyClosable) {
                    onCloseTab(tab.id)
                    return
                  }
                  onSelectTab(tab.id)
                }}
                className={[
                  'right-panel-tab group/right-panel-tab hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]',
                  closable ? 'right-panel-tab--closable' : '',
                  iconOnlyClosable ? 'right-panel-tab--icon-only-closable' : '',
                  draggingTabId === tab.id ? 'right-panel-tab--dragging' : '',
                ].filter(Boolean).join(' ')}
                draggable
                onDragStart={(event) => handleTabDragStart(event, tab.id)}
                onDragOver={(event) => handleTabDragOver(event, tab.id)}
                onDrop={(event) => handleTabDrop(event, tab.id)}
                onDragEnd={handleTabDragEnd}
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: tab.hideTitle ? 'center' : undefined,
                  gap: tab.hideTitle ? 0 : 5,
                  minWidth: 0,
                  maxWidth: 220,
                  width: tab.hideTitle ? rightPanelIconButtonSize : undefined,
                  height: 28,
                  padding: tab.hideTitle ? 0 : '0 8px',
                  borderRadius: 6.5,
                  border: 0,
                  background: active ? 'var(--c-bg-deep)' : undefined,
                  color: active ? 'var(--c-text-primary)' : 'var(--c-text-secondary)',
                  fontSize: 13,
                  font: 'inherit',
                  cursor: 'pointer',
                  transition: 'background-color 160ms ease, color 160ms ease',
                }}
              >
                {dropIndicator?.targetId === tab.id ? (
                  <span className={`right-panel-tab__drop-indicator right-panel-tab__drop-indicator--${dropIndicator.side}`} />
                ) : null}
                {iconOnlyClosable ? (
                  <span className="right-panel-tab__icon-swap">
                    <span className="right-panel-tab__icon">{icon}</span>
                    <span className={`${iconButtonSmCls} right-panel-tab__close right-panel-tab__close--swap opacity-0 group-hover/right-panel-tab:opacity-100`}>
                      <X size={12} />
                    </span>
                  </span>
                ) : icon}
                {!tab.hideTitle ? <span className="right-panel-tab__title">{tab.title}</span> : null}
                {closable && !iconOnlyClosable ? (
                  <span
                    role="button"
                    tabIndex={-1}
                    aria-label={`Close ${tab.title}`}
                    onClick={(event) => {
                      event.stopPropagation()
                      onCloseTab(tab.id)
                    }}
                    className={`${iconButtonSmCls} right-panel-tab__close pointer-events-none absolute right-1 opacity-0 group-hover/right-panel-tab:pointer-events-auto group-hover/right-panel-tab:opacity-100`}
                    style={{
                      width: 18,
                      height: 18,
                      borderRadius: 5,
                      background: 'var(--c-bg-deep)',
                      color: 'inherit',
                    }}
                  >
                    <X size={12} />
                  </span>
                ) : null}
              </button>
            )
          })}
        </div>
        {addOptions.length > 0 ? (
          <div ref={addMenuRef} className="right-panel-tabbar__add">
            <button
              type="button"
              title={effectiveAddLabel}
              aria-label={effectiveAddLabel}
              aria-expanded={addMenuOpen}
              onClick={() => setAddMenuOpen((open) => !open)}
              className={`${rightPanelIconButtonCls} right-panel-tabbar__add-button`}
            >
              <Plus size={rightPanelIconSize} />
            </button>
            <div className="right-panel-tabbar__add-menu" data-open={addMenuOpen}>
              {addOptions.map((option) => (
                <DropdownAction
                  key={option.id}
                  icon={option.icon}
                  label={option.label}
                  disabled={option.disabled}
                  onClick={() => {
                    option.onSelect()
                    setAddMenuOpen(false)
                  }}
                />
              ))}
            </div>
          </div>
        ) : null}
      </div>
      <div key={activeTab?.id ?? 'empty'} style={{ flex: 1, minHeight: 0, minWidth: 0 }}>
        {activeTab?.content ?? (
          <div style={{ height: '100%', display: 'grid', placeItems: 'center', color: 'var(--c-text-muted)', fontSize: 13 }}>
            {effectiveEmptyLabel}
          </div>
        )}
      </div>
    </div>
  )
})
