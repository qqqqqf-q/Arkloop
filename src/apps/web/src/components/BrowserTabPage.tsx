import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { AlertCircle, ChevronLeft, ChevronRight, Globe, Loader2, Maximize2, Minimize2, PanelRightClose, Plus, RefreshCcw, X } from 'lucide-react'
import { getDesktopApi, isDesktop } from '@arkloop/shared/desktop'
import { Button } from '@arkloop/shared'
import { useBrowserTabs } from '../contexts/browser-tabs'
import { useLocale } from '../contexts/LocaleContext'

function getRectForElement(element: HTMLElement): { x: number; y: number; width: number; height: number } {
  const rect = element.getBoundingClientRect()
  return {
    x: Math.round(rect.left),
    y: Math.round(rect.top),
    width: Math.round(rect.width),
    height: Math.round(rect.height),
  }
}

function getFallbackFaviconUrl(rawUrl: string): string | null {
  try {
    const parsed = new URL(rawUrl)
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return null
    return new URL('/favicon.ico', parsed).toString()
  } catch {
    return null
  }
}

function getDomain(rawUrl: string): string {
  try {
    const parsed = new URL(rawUrl)
    return parsed.hostname
  } catch {
    return rawUrl
  }
}

type BrowserTabPageProps = {
  browserFullscreen?: boolean
  onToggleBrowserFullscreen?: () => void
}

export function BrowserTabPage({ browserFullscreen = false, onToggleBrowserFullscreen }: BrowserTabPageProps) {
  const { t } = useLocale()
  const {
    tabs,
    panelOpen,
    activeBrowserTab,
    activeBrowserTabId,
    getDraftUrl,
    setDraftUrl,
    createBrowserTab,
    activateBrowserTab,
    closeBrowserPanel,
    closeBrowserTab,
    navigateBrowserTab,
    reloadBrowserTab,
    goBackBrowserTab,
    goForwardBrowserTab,
  } = useBrowserTabs()
  const hostRef = useRef<HTMLDivElement>(null)
  const [_submitting, setSubmitting] = useState(false)
  const [failedFavicons, setFailedFavicons] = useState<Record<string, string>>({})
  const [inputFocused, setInputFocused] = useState(false)
  const draftUrl = activeBrowserTabId ? getDraftUrl(activeBrowserTabId) : ''
  const desktop = isDesktop()
  const browserApi = getDesktopApi()?.browserTabs

  useEffect(() => {
    setFailedFavicons((current) => {
      const next: Record<string, string> = {}
      for (const tab of tabs) {
        const displayFaviconUrl = tab.faviconUrl ?? getFallbackFaviconUrl(tab.url)
        if (current[tab.id] && current[tab.id] === displayFaviconUrl) {
          next[tab.id] = current[tab.id]
        }
      }
      return next
    })
  }, [tabs])

  const showEmbeddedBrowser = desktop && panelOpen && Boolean(activeBrowserTabId && activeBrowserTab && browserApi)

  const syncBounds = useMemo(() => {
    if (!showEmbeddedBrowser || !activeBrowserTabId) return null
    return () => {
      const element = hostRef.current
      if (!element) return
      void browserApi?.syncBounds(activeBrowserTabId, getRectForElement(element)).catch(() => {})
    }
  }, [activeBrowserTabId, browserApi, showEmbeddedBrowser])

  useLayoutEffect(() => {
    if (!showEmbeddedBrowser || !activeBrowserTabId) return
    const element = hostRef.current
    if (!element) return
    void browserApi?.show(activeBrowserTabId, getRectForElement(element)).catch(() => {})
    return () => {
      void browserApi?.hide().catch(() => {})
    }
  }, [activeBrowserTabId, browserApi, showEmbeddedBrowser])

  useEffect(() => {
    if (!syncBounds) return
    syncBounds()
    const element = hostRef.current
    if (!element) return
    const observer = new ResizeObserver(() => syncBounds())
    observer.observe(element)
    window.addEventListener('resize', syncBounds)
    window.addEventListener('scroll', syncBounds, true)
    return () => {
      observer.disconnect()
      window.removeEventListener('resize', syncBounds)
      window.removeEventListener('scroll', syncBounds, true)
    }
  }, [syncBounds])

  const submitNavigation = async () => {
    if (!activeBrowserTabId) return
    setSubmitting(true)
    try {
      await navigateBrowserTab(activeBrowserTabId, draftUrl)
    } finally {
      setSubmitting(false)
    }
  }

  if (!desktop || !browserApi) {
    return (
      <div className="flex h-full min-h-0 items-center justify-center bg-[var(--c-bg-page)] p-6">
        <div
          className="max-w-md rounded-2xl bg-[var(--c-bg-sub)] p-6 text-center"
          style={{ border: '0.5px solid var(--c-border-subtle)' }}
        >
          <p className="text-sm text-[var(--c-text-secondary)]">{t.browserTabOnlyDesktop}</p>
        </div>
      </div>
    )
  }

  if (!panelOpen) {
    return null
  }

  if (!activeBrowserTab && tabs.length > 0) {
    return (
      <div className="flex h-full min-h-0 items-center justify-center bg-[var(--c-bg-page)] p-6">
        <div
          className="max-w-md rounded-2xl bg-[var(--c-bg-sub)] p-6 text-center"
          style={{ border: '0.5px solid var(--c-border-subtle)' }}
        >
          <p className="text-sm text-[var(--c-text-secondary)]">{t.browserTabMissing}</p>
        </div>
      </div>
    )
  }

  return (
    <div className="flex h-full min-h-0 w-full min-w-0 flex-col bg-[var(--c-bg-page)]">
      <div
        className="flex h-12 shrink-0 items-center gap-2 px-3"
        style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}
      >
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <div
            className="flex h-9 w-fit max-w-full min-w-0 items-center gap-1 overflow-x-auto px-1 py-0.5"
          >
            {tabs.map((tab) => {
              const active = activeBrowserTabId === tab.id
              const displayFaviconUrl = tab.faviconUrl ?? getFallbackFaviconUrl(tab.url)
              return (
                <div
                  key={tab.id}
                  className="group flex h-8 min-w-0 shrink-0 items-center pr-1 rounded-[10px]"
                  style={{
                    maxWidth: 220,
                    background: active ? 'var(--c-mode-switch-pill)' : 'transparent',
                    border: active ? '0.5px solid var(--c-mode-switch-border)' : '0.5px solid transparent',
                  }}
                >
                  <button
                    type="button"
                    onClick={(event) => {
                      event.stopPropagation()
                      void closeBrowserTab(tab.id)
                    }}
                    className="flex h-full w-8 shrink-0 items-center justify-center rounded-l-[10px] text-[var(--c-text-tertiary)] transition-colors group-hover:text-[var(--c-text-primary)]"
                    title="Close browser tab"
                    aria-label="Close browser tab"
                  >
                    {tab.loading ? (
                      <Loader2 size={12} className="shrink-0 animate-spin" />
                    ) : (
                      <>
                        {displayFaviconUrl && failedFavicons[tab.id] !== displayFaviconUrl ? (
                          <img
                            src={displayFaviconUrl}
                            alt=""
                            className="h-3.5 w-3.5 shrink-0 rounded-[3px] object-contain group-hover:hidden"
                            onError={() => {
                              setFailedFavicons((current) => ({ ...current, [tab.id]: displayFaviconUrl }))
                            }}
                          />
                        ) : (
                          <Globe size={12} className="shrink-0 group-hover:hidden" />
                        )}
                        <X size={12} className="hidden shrink-0 group-hover:block" />
                      </>
                    )}
                  </button>
                  <button
                    type="button"
                    onClick={() => activateBrowserTab(tab.id)}
                    className="flex h-full min-w-0 flex-1 items-center rounded-r-[10px] pl-0 pr-2.5 text-left text-[12.5px] leading-[18px]"
                    title={tab.url || tab.title}
                    style={{
                      color: active ? 'var(--c-mode-switch-active-text)' : 'var(--c-mode-switch-inactive-text)',
                    }}
                  >
                    <span className="truncate">{tab.title || t.modeBrowser}</span>
                  </button>
                </div>
              )
            })}
          </div>
          <button
            type="button"
            className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-primary)]"
            title={t.newBrowserTab}
            onClick={() => { void createBrowserTab() }}
          >
            <Plus size={16} />
          </button>
        </div>
        {onToggleBrowserFullscreen && (
          <button
            type="button"
            className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-primary)]"
            title={browserFullscreen ? '退出全屏' : '全屏'}
            onClick={onToggleBrowserFullscreen}
          >
            {browserFullscreen ? <Minimize2 size={16} /> : <Maximize2 size={16} />}
          </button>
        )}
        <button
          type="button"
          className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-primary)]"
          title={t.browserPanelCollapse}
          onClick={closeBrowserPanel}
        >
          <PanelRightClose size={16} />
        </button>
      </div>

      {activeBrowserTab && activeBrowserTabId && (
        <div
          className="flex shrink-0 items-center gap-2 px-4 py-1"
          style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}
        >
        <button
          type="button"
          className="flex h-9 w-9 items-center justify-center rounded-lg text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:cursor-not-allowed disabled:opacity-45"
          disabled={!activeBrowserTab.canGoBack}
          onClick={() => void goBackBrowserTab(activeBrowserTabId)}
          aria-label="Back"
        >
          <ChevronLeft size={16} />
        </button>
        <button
          type="button"
          className="flex h-9 w-9 items-center justify-center rounded-lg text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:cursor-not-allowed disabled:opacity-45"
          disabled={!activeBrowserTab.canGoForward}
          onClick={() => void goForwardBrowserTab(activeBrowserTabId)}
          aria-label="Forward"
        >
          <ChevronRight size={16} />
        </button>
        <button
          type="button"
          className="flex h-9 w-9 items-center justify-center rounded-lg text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:cursor-not-allowed disabled:opacity-45"
          disabled={activeBrowserTab.loading}
          onClick={() => void reloadBrowserTab(activeBrowserTabId)}
          aria-label="Reload"
        >
          {activeBrowserTab.loading ? <Loader2 size={16} className="animate-spin" /> : <RefreshCcw size={16} />}
        </button>
        <form
          className="flex min-w-0 flex-1 items-center gap-2"
          onSubmit={(event) => {
            event.preventDefault()
            void submitNavigation()
          }}
        >
          <input
            value={inputFocused ? draftUrl : (draftUrl ? getDomain(draftUrl) : '')}
            onChange={(event) => setDraftUrl(activeBrowserTabId, event.target.value)}
            onFocus={() => setInputFocused(true)}
            onBlur={() => setInputFocused(false)}
            placeholder="输入 URL"
            className="h-9 min-w-0 flex-1 rounded-lg bg-transparent px-4 text-sm text-[var(--c-text-primary)] outline-none hover:bg-[var(--c-bg-input)] focus:bg-transparent focus:border-[var(--c-border-subtle)] border border-transparent transition-colors"
            style={{ textAlign: inputFocused ? 'left' : 'center' }}
          />
        </form>
        </div>
      )}

      {activeBrowserTab?.error && (
        <div
          className="mx-4 mt-3 flex shrink-0 items-start gap-2 rounded-xl px-3 py-2 text-sm text-[var(--c-status-error)]"
          style={{ background: 'color-mix(in srgb, var(--c-status-error) 12%, transparent)' }}
        >
          <AlertCircle size={16} className="mt-0.5 shrink-0" />
          <div className="min-w-0 flex-1">
            <p className="font-medium">{t.browserTabLoadFailed}</p>
            <p className="mt-1 break-words text-[var(--c-text-secondary)]">{activeBrowserTab.error}</p>
            <div className="mt-2">
              <Button variant="outline" size="sm" onClick={() => void submitNavigation()}>
                {t.browserTabRetry}
              </Button>
            </div>
          </div>
        </div>
      )}

      <div
        ref={hostRef}
        className="relative min-h-0 w-full min-w-0 flex-1"
      >
        {!activeBrowserTab?.url && (
          <div className="absolute inset-0 flex items-center justify-center p-6">
            <div
              className="max-w-md rounded-2xl bg-[var(--c-bg-sub)] p-6 text-center"
              style={{ border: '0.5px solid var(--c-border-subtle)' }}
            >
              <p className="text-sm text-[var(--c-text-secondary)]">
                {tabs.length === 0 ? t.browserPanelEmptyState : t.browserTabEmptyState}
              </p>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
