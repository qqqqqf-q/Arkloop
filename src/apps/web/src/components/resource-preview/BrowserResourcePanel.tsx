import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ArrowLeft, ArrowRight, Copy, ExternalLink, List, MoreHorizontal, RefreshCw, Search, Star, Trash2, X } from 'lucide-react'
import { Button } from '@arkloop/shared'
import { getDesktopApi } from '@arkloop/shared/desktop'
import { DropdownAction } from '../DropdownAction'
import { rightPanelIconButtonCls, rightPanelIconSize } from '../rightPanelControls'
import { openExternal } from '../../openExternal'
import { useLocale } from '../../contexts/LocaleContext'
import type { BrowserResourceRef } from './types'
import { browserFaviconUrl, browserTitleFromUrl, displayBrowserUrl, normalizeBrowserUrl } from './browserIdentity'
import { BrowserSiteIcon } from './BrowserSiteIcon'
import './BrowserResourcePanel.css'

type HistoryEntry = {
  url: string
  title: string
  at: number
  faviconUrl?: string
}

type Props = {
  resource: BrowserResourceRef
  onClose?: () => void
  onResourceChange?: (resource: BrowserResourceRef) => void
}

const storageKey = 'arkloop:web:browser-renderer'
const maxHistoryEntries = 80
let savedStateFallback = ''

function resourceFromUrl(url: string, title?: string): BrowserResourceRef {
  return {
    kind: 'browser',
    url,
    title: title?.trim() || browserTitleFromUrl(url),
    faviconUrl: browserFaviconUrl(url),
  }
}

function historyEntryFromResource(resource: BrowserResourceRef, at = Date.now()): HistoryEntry {
  return {
    url: resource.url,
    title: resource.title?.trim() || browserTitleFromUrl(resource.url),
    faviconUrl: resource.faviconUrl ?? browserFaviconUrl(resource.url),
    at,
  }
}

function sanitizeHistory(items: unknown): HistoryEntry[] {
  if (!Array.isArray(items)) return []
  const byUrl = new Map<string, HistoryEntry>()
  for (const item of items) {
    if (!item || typeof item !== 'object') continue
    const raw = item as Partial<HistoryEntry>
    if (typeof raw.url !== 'string') continue
    const url = normalizeBrowserUrl(raw.url)
    if (!url) continue
    byUrl.delete(url)
    byUrl.set(url, {
      url,
      title: typeof raw.title === 'string' && raw.title.trim() ? raw.title.trim() : browserTitleFromUrl(url),
      faviconUrl: typeof raw.faviconUrl === 'string' ? raw.faviconUrl : browserFaviconUrl(url),
      at: typeof raw.at === 'number' ? raw.at : Date.now(),
    })
  }
  return Array.from(byUrl.values()).slice(-maxHistoryEntries)
}

function upsertHistoryEntry(items: HistoryEntry[], entry: HistoryEntry): HistoryEntry[] {
  return [...items.filter((item) => item.url !== entry.url), entry].slice(-maxHistoryEntries)
}

function readSavedState(): { bookmarks: HistoryEntry[]; history: HistoryEntry[]; showBookmarkBar: boolean } {
  try {
    const stored = localStorage.getItem?.(storageKey)
    const raw = stored === undefined || stored === null ? savedStateFallback : stored
    if (!raw) return { bookmarks: [], history: [], showBookmarkBar: false }
    const parsed = JSON.parse(raw) as { bookmarks?: HistoryEntry[]; history?: HistoryEntry[]; showBookmarkBar?: boolean }
    return {
      bookmarks: sanitizeHistory(parsed.bookmarks).slice(-12),
      history: sanitizeHistory(parsed.history),
      showBookmarkBar: parsed.showBookmarkBar === true,
    }
  } catch {
    return { bookmarks: [], history: [], showBookmarkBar: false }
  }
}

function writeSavedState(bookmarks: HistoryEntry[], history: HistoryEntry[], showBookmarkBar: boolean): void {
  const payload = JSON.stringify({
    bookmarks: bookmarks.slice(-12),
    history: sanitizeHistory(history),
    showBookmarkBar,
  })
  savedStateFallback = payload
  try {
    localStorage.setItem?.(storageKey, payload)
  } catch {
    // localStorage can be unavailable in hardened browser contexts.
  }
}

function withCacheBust(url: string): string {
  try {
    const parsed = new URL(url)
    parsed.searchParams.set('__arkloop_reload', String(Date.now()))
    return parsed.toString()
  } catch {
    return url
  }
}

export function BrowserResourcePanel({ resource, onClose, onResourceChange }: Props) {
  const { t } = useLocale()
  const text = t.browserPanel
  const initialUrl = useMemo(() => normalizeBrowserUrl(resource.url), [resource.url])
  const saved = useMemo(() => readSavedState(), [])
  const [currentUrl, setCurrentUrl] = useState(initialUrl ?? '')
  const [address, setAddress] = useState(initialUrl ? displayBrowserUrl(initialUrl) : '')
  const [frameSrc, setFrameSrc] = useState<string | null>(initialUrl)
  const [frameKey, setFrameKey] = useState(0)
  const [loading, setLoading] = useState(!!initialUrl)
  const [history, setHistory] = useState<HistoryEntry[]>(initialUrl
    ? upsertHistoryEntry(saved.history, historyEntryFromResource(resourceFromUrl(initialUrl, resource.title)))
    : saved.history)
  const [historyIndex, setHistoryIndex] = useState(() => initialUrl ? Math.max(0, upsertHistoryEntry(saved.history, historyEntryFromResource(resourceFromUrl(initialUrl, resource.title))).findIndex((item) => item.url === initialUrl)) : Math.max(0, saved.history.length - 1))
  const [historyOpen, setHistoryOpen] = useState(false)
  const [historySearch, setHistorySearch] = useState('')
  const [menuOpen, setMenuOpen] = useState(false)
  const [bookmarks, setBookmarks] = useState(saved.bookmarks)
  const [showBookmarkBar, setShowBookmarkBar] = useState(saved.showBookmarkBar)
  const menuRef = useRef<HTMLDivElement | null>(null)
  const frameRef = useRef<HTMLIFrameElement | null>(null)
  const lastInitialUrlRef = useRef(initialUrl)

  useEffect(() => {
    if (lastInitialUrlRef.current === initialUrl) return
    lastInitialUrlRef.current = initialUrl
    if (initialUrl && initialUrl === currentUrl) return
    setCurrentUrl(initialUrl ?? '')
    setAddress(initialUrl ? displayBrowserUrl(initialUrl) : '')
    setFrameSrc(initialUrl)
    setFrameKey((key) => key + 1)
    setLoading(!!initialUrl)
    if (initialUrl) {
      const entry = historyEntryFromResource(resourceFromUrl(initialUrl, resource.title))
      setHistory((items) => {
        const next = upsertHistoryEntry(items, entry)
        setHistoryIndex(next.findIndex((item) => item.url === initialUrl))
        return next
      })
    }
  }, [currentUrl, initialUrl, resource.title])

  useEffect(() => {
    writeSavedState(bookmarks, history, showBookmarkBar)
  }, [bookmarks, history, showBookmarkBar])

  useEffect(() => {
    if (!menuOpen) return
    const handlePointerDown = (event: PointerEvent) => {
      if (menuRef.current?.contains(event.target as Node)) return
      setMenuOpen(false)
    }
    window.addEventListener('pointerdown', handlePointerDown)
    return () => window.removeEventListener('pointerdown', handlePointerDown)
  }, [menuOpen])

  const navigateTo = useCallback((nextValue: string, replace = false) => {
    const nextUrl = normalizeBrowserUrl(nextValue)
    if (!nextUrl) return
    if (nextUrl === currentUrl) return
    const nextResource = resourceFromUrl(nextUrl)
    const entry = historyEntryFromResource(nextResource)
    const nextHistory = replace
      ? history.map((item, index) => index === historyIndex ? entry : item)
      : upsertHistoryEntry(history, entry)

    setCurrentUrl(nextUrl)
    setAddress(displayBrowserUrl(nextUrl))
    setFrameSrc(nextUrl)
    setFrameKey((key) => key + 1)
    setLoading(true)
    setHistory(nextHistory)
    setHistoryIndex(Math.max(0, nextHistory.findIndex((item) => item.url === nextUrl)))
    onResourceChange?.(nextResource)
  }, [currentUrl, history, historyIndex, onResourceChange])

  const navigateToHistoryIndex = useCallback((nextIndex: number) => {
    const entry = history[nextIndex]
    if (!entry) return
    if (entry.url === currentUrl) return
    setCurrentUrl(entry.url)
    setAddress(displayBrowserUrl(entry.url))
    setFrameSrc(entry.url)
    setFrameKey((key) => key + 1)
    setLoading(true)
    setHistoryIndex(nextIndex)
    onResourceChange?.({ kind: 'browser', url: entry.url, title: entry.title, faviconUrl: entry.faviconUrl })
  }, [history, currentUrl, onResourceChange])

  const selectHistoryEntry = useCallback((url: string) => {
    const index = history.findIndex((item) => item.url === url)
    if (index < 0) {
      navigateTo(url)
      return
    }
    navigateToHistoryIndex(index)
  }, [history, navigateTo, navigateToHistoryIndex])

  const goBack = useCallback(() => {
    if (historyIndex === 0) return
    navigateToHistoryIndex(historyIndex - 1)
  }, [historyIndex, navigateToHistoryIndex])

  const goForward = useCallback(() => {
    if (historyIndex >= history.length - 1) return
    navigateToHistoryIndex(historyIndex + 1)
  }, [historyIndex, history.length, navigateToHistoryIndex])

  const reload = useCallback((hard = false) => {
    if (!currentUrl) return
    setFrameSrc(hard ? withCacheBust(currentUrl) : currentUrl)
    setFrameKey((key) => key + 1)
    setLoading(true)
  }, [currentUrl])

  const copyCurrentUrl = useCallback(() => {
    if (!currentUrl) return
    void navigator.clipboard?.writeText(currentUrl)
    setMenuOpen(false)
  }, [currentUrl])

  const toggleBookmark = useCallback(() => {
    if (!currentUrl) return
    setBookmarks((items) => {
      if (items.some((item) => item.url === currentUrl)) return items.filter((item) => item.url !== currentUrl)
      return upsertHistoryEntry(items, historyEntryFromResource(resourceFromUrl(currentUrl))).slice(-12)
    })
  }, [currentUrl])

  const clearHistory = useCallback(() => {
    setHistory([])
    setHistoryIndex(0)
  }, [])

  const handleFrameLoad = useCallback(() => {
    setLoading(false)
    if (!currentUrl) return
    let frameTitle = ''
    try {
      frameTitle = frameRef.current?.contentDocument?.title?.trim() ?? ''
    } catch {
      frameTitle = ''
    }
    if (!frameTitle) return
    setHistory((items) => items.map((item) => item.url === currentUrl ? { ...item, title: frameTitle } : item))
    onResourceChange?.({ ...resourceFromUrl(currentUrl, frameTitle), faviconUrl: resource.faviconUrl ?? browserFaviconUrl(currentUrl) })
  }, [currentUrl, onResourceChange, resource.faviconUrl])

  useEffect(() => {
    if (!currentUrl) return
    let cancelled = false
    getDesktopApi()?.app.fetchPageMetadata?.(currentUrl)
      .then((metadata) => {
        const title = metadata?.title?.trim()
        if (cancelled || !title) return
        setHistory((items) => items.map((item) => item.url === currentUrl ? { ...item, title } : item))
        onResourceChange?.({ ...resourceFromUrl(currentUrl, title), faviconUrl: resource.faviconUrl ?? browserFaviconUrl(currentUrl) })
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [currentUrl, onResourceChange, resource.faviconUrl])

  const filteredHistory = useMemo(() => {
    const query = historySearch.trim().toLowerCase()
    const reversed = [...history].reverse()
    if (!query) return reversed
    return reversed.filter((item) => item.url.toLowerCase().includes(query) || item.title.toLowerCase().includes(query))
  }, [history, historySearch])

  const hasPage = !!currentUrl
  const currentFaviconUrl = resource.url === currentUrl ? resource.faviconUrl : undefined
  const bookmarked = hasPage && bookmarks.some((item) => item.url === currentUrl)

  return (
    <section
      className="browser-panel"
      aria-label={text.preview}
      onClick={(event) => event.stopPropagation()}
      onMouseDown={(event) => event.stopPropagation()}
    >
      <div className="browser-panel__toolbar">
        <button
          type="button"
          title={text.history}
          aria-pressed={historyOpen}
          onClick={() => setHistoryOpen((open) => !open)}
          className={`${rightPanelIconButtonCls} browser-panel__tool${historyOpen ? ' browser-panel__tool--active' : ''}`}
        >
          <List size={rightPanelIconSize} />
        </button>
        <button type="button" title={text.back} disabled={historyIndex === 0} onClick={goBack} className={`${rightPanelIconButtonCls} browser-panel__tool`}>
          <ArrowLeft size={rightPanelIconSize} />
        </button>
        <button type="button" title={text.forward} disabled={historyIndex >= history.length - 1} onClick={goForward} className={`${rightPanelIconButtonCls} browser-panel__tool`}>
          <ArrowRight size={rightPanelIconSize} />
        </button>
        <button type="button" title={text.reload} disabled={!hasPage} onClick={() => reload(false)} className={`${rightPanelIconButtonCls} browser-panel__tool`}>
          <RefreshCw size={rightPanelIconSize} className={loading ? 'browser-panel__spin' : undefined} />
        </button>
        <button
          type="button"
          title={bookmarked ? text.removeBookmark : text.bookmark}
          disabled={!hasPage}
          aria-pressed={bookmarked}
          onClick={toggleBookmark}
          className={`${rightPanelIconButtonCls} browser-panel__tool${bookmarked ? ' browser-panel__tool--active' : ''}`}
        >
          <Star size={rightPanelIconSize} fill={bookmarked ? 'currentColor' : 'none'} />
        </button>
        <form
          className="browser-panel__address"
          onSubmit={(event) => {
            event.preventDefault()
            event.stopPropagation()
            navigateTo(address)
          }}
        >
          <BrowserSiteIcon url={currentUrl} faviconUrl={currentFaviconUrl} size={rightPanelIconSize} />
          <input
            value={address}
            onChange={(event) => setAddress(event.target.value)}
            placeholder={text.addressPlaceholder}
            spellCheck={false}
            className="browser-panel__address-input"
          />
        </form>
        <button type="button" title={text.openExternal} disabled={!hasPage} onClick={() => openExternal(currentUrl)} className={`${rightPanelIconButtonCls} browser-panel__tool`}>
          <ExternalLink size={rightPanelIconSize} />
        </button>
        <div ref={menuRef} className="browser-panel__menu-wrap">
          <button
            type="button"
            title={text.more}
            aria-expanded={menuOpen}
            onClick={() => setMenuOpen((open) => !open)}
            className={`${rightPanelIconButtonCls} browser-panel__tool${menuOpen ? ' browser-panel__tool--active' : ''}`}
          >
            <MoreHorizontal size={rightPanelIconSize} />
          </button>
          <div className="browser-panel__menu" data-open={menuOpen}>
            <DropdownAction icon={<RefreshCw size={14} />} label={text.hardReload} disabled={!hasPage} onClick={() => { reload(true); setMenuOpen(false) }} />
            <DropdownAction icon={<Copy size={14} />} label={text.copyCurrentUrl} disabled={!hasPage} onClick={copyCurrentUrl} />
            <DropdownAction
              icon={<Star size={14} />}
              label={showBookmarkBar ? text.hideBookmarkBar : text.showBookmarkBar}
              onClick={() => { setShowBookmarkBar((show) => !show); setMenuOpen(false) }}
            />
            <DropdownAction icon={<Trash2 size={14} />} label={text.clearBrowsingHistory} disabled={history.length === 0} onClick={() => { clearHistory(); setMenuOpen(false) }} />
          </div>
        </div>
        {onClose ? (
          <button type="button" title={text.close} onClick={onClose} className={`${rightPanelIconButtonCls} browser-panel__tool`}>
            <X size={rightPanelIconSize} />
          </button>
        ) : null}
      </div>

      {showBookmarkBar && bookmarks.length > 0 ? (
        <div className="browser-panel__bookmarks">
          {bookmarks.map((item) => (
            <Button
              key={item.url}
              variant="ghost"
              size="sm"
              className="browser-panel__bookmark-button"
              onClick={(event) => {
                event.preventDefault()
                event.stopPropagation()
                navigateTo(item.url)
              }}
              title={item.url}
            >
              <BrowserSiteIcon url={item.url} faviconUrl={item.faviconUrl} size={13} />
              <span>{item.title}</span>
            </Button>
          ))}
        </div>
      ) : null}

      <div className="browser-panel__content">
        <aside className={`browser-panel__history${historyOpen ? '' : ' browser-panel__history--closed'}`}>
          <div className="browser-panel__search">
            <Search size={14} aria-hidden="true" />
            <input value={historySearch} onChange={(event) => setHistorySearch(event.target.value)} placeholder={text.search} />
          </div>
          <div className="browser-panel__history-list">
            {filteredHistory.length === 0 ? (
              <div className="browser-panel__empty">{text.noHistory}</div>
            ) : filteredHistory.map((item) => (
              <Button
                key={`${item.at}:${item.url}`}
                variant="ghost"
                size="sm"
                className="browser-panel__history-item"
                onClick={(event) => {
                  event.preventDefault()
                  event.stopPropagation()
                  selectHistoryEntry(item.url)
                }}
                title={item.url}
              >
                <BrowserSiteIcon url={item.url} faviconUrl={item.faviconUrl} size={14} />
                <span>{item.title}</span>
              </Button>
            ))}
          </div>
          {history.length > 1 ? (
            <Button variant="ghost" size="sm" className="browser-panel__clear" onClick={clearHistory}>
              <Trash2 size={13} />
              <span>{text.clear}</span>
            </Button>
          ) : null}
        </aside>
        <div className="browser-panel__viewport">
          {loading ? <div className="browser-panel__progress" /> : null}
          {frameSrc ? (
            <iframe
              ref={frameRef}
              key={frameKey}
              src={frameSrc}
              title={browserTitleFromUrl(currentUrl)}
              className="browser-panel__frame"
              sandbox="allow-same-origin allow-scripts allow-forms allow-popups allow-downloads"
              onLoad={handleFrameLoad}
            />
          ) : null}
        </div>
      </div>
    </section>
  )
}
