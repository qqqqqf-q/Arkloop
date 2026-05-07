import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
} from 'react'
import { createPortal } from 'react-dom'
import { ChevronDown } from 'lucide-react'
import type { MeResponse } from '../../api'
import { updateMe } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { useToast } from '@arkloop/shared'
import {
  detectDeviceTimeZone,
  formatTimeZoneOffset,
  listSupportedTimeZones,
  normalizeTimeZone,
} from '@arkloop/shared'
import { settingsInputCls } from './_SettingsInput'
import { getAdaptiveMenuLeft, settingsSelectBorderColor } from './_SettingsSelectUtils'

type Props = {
  me: MeResponse | null
  accessToken: string
  onMeUpdated?: (me: MeResponse) => void
  showLabel?: boolean
  className?: string
  triggerClassName?: string
  triggerButtonClassName?: string
}

/** 与 SettingsModelDropdown 菜单内选项一致 */
const ROW_CLS =
  'flex w-full items-center justify-between gap-2 px-3 py-2 text-sm font-[450] transition-colors bg-[var(--c-bg-menu)] hover:bg-[var(--c-bg-deep)]'

type MenuRow = {
  key: string
  label: string
  offset: string
  persistValue: string
  active: boolean
}

const EMPTY_ZONES: string[] = []
const EMPTY_ZONE_OFFSETS = new Map<string, string>()
const PRELOAD_CHUNK_BUDGET_MS = 8
const PRELOAD_FALLBACK_DELAY_MS = 16
const MENU_MAX_HEIGHT_PX = 220
const MENU_ROW_ESTIMATE_PX = 36
const MENU_OVERSCAN_ROWS = 6
const MENU_WIDTH_MIN_PX = 260
const MENU_WIDTH_MAX_PX = 460

type IdleDeadlineLike = {
  didTimeout: boolean
  timeRemaining: () => number
}

let cachedZones: string[] | null = null
let cachedOffsetKey: string | null = null
let cachedOffsets = EMPTY_ZONE_OFFSETS
let offsetPreloadPromise: Promise<void> | null = null
let offsetPreloadToken = 0

function getSupportedZonesCached(): string[] {
  if (cachedZones != null) return cachedZones
  cachedZones = listSupportedTimeZones()
  return cachedZones
}

function buildOffsetCacheKey(value: Date): string {
  return value.toISOString().slice(0, 13)
}

function getCachedOffsets(value: Date): Map<string, string> {
  return cachedOffsetKey === buildOffsetCacheKey(value) ? cachedOffsets : EMPTY_ZONE_OFFSETS
}

function scheduleIdleWork(callback: (deadline?: IdleDeadlineLike) => void): void {
  if (typeof globalThis.requestIdleCallback === 'function') {
    globalThis.requestIdleCallback((deadline) => callback(deadline))
    return
  }
  globalThis.setTimeout(() => callback(), PRELOAD_FALLBACK_DELAY_MS)
}

function preloadOffsets(value: Date): Promise<void> {
  const key = buildOffsetCacheKey(value)
  const zones = getSupportedZonesCached()
  if (cachedOffsetKey === key && cachedOffsets.size === zones.length) {
    return Promise.resolve()
  }
  if (cachedOffsetKey === key && offsetPreloadPromise) {
    return offsetPreloadPromise
  }

  const token = ++offsetPreloadToken
  cachedOffsetKey = key
  cachedOffsets = new Map<string, string>()

  offsetPreloadPromise = new Promise<void>((resolve) => {
    let index = 0

    const step = (deadline?: IdleDeadlineLike) => {
      if (token !== offsetPreloadToken) {
        resolve()
        return
      }

      const startedAt = typeof performance !== 'undefined' ? performance.now() : 0
      while (index < zones.length) {
        const zone = zones[index]
        if (zone != null) cachedOffsets.set(zone, formatTimeZoneOffset(value, zone))
        index += 1

        if (deadline) {
          if (!deadline.didTimeout && deadline.timeRemaining() < 2) break
        } else if (typeof performance !== 'undefined') {
          if (performance.now() - startedAt >= PRELOAD_CHUNK_BUDGET_MS) break
        } else if (index % 24 === 0) {
          break
        }
      }

      if (index >= zones.length) {
        if (token === offsetPreloadToken) offsetPreloadPromise = null
        resolve()
        return
      }

      scheduleIdleWork(step)
    }

    scheduleIdleWork(step)
  })

  return offsetPreloadPromise
}

function estimateMenuWidth(baseWidth: number, labels: string[], viewportWidth: number): number {
  const longestLabel = Math.max(...labels.map((label) => label.length), 0)
  const estimatedContentWidth = longestLabel * 7 + 116
  const availableWidth = Math.max(MENU_WIDTH_MIN_PX, viewportWidth - 32)
  return Math.min(
    Math.max(baseWidth, estimatedContentWidth, MENU_WIDTH_MIN_PX),
    MENU_WIDTH_MAX_PX,
    availableWidth,
  )
}

export function TimeZoneSettings({
  me,
  accessToken,
  onMeUpdated,
  showLabel = true,
  className,
  triggerClassName,
  triggerButtonClassName,
}: Props) {
  const { t } = useLocale()
  const { addToast } = useToast()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [saving, setSaving] = useState(false)
  const [menuStyle, setMenuStyle] = useState<CSSProperties>({})
  const [labelNow, setLabelNow] = useState(() => new Date())
  const [offsetCacheVersion, setOffsetCacheVersion] = useState(0)
  const [listScrollTop, setListScrollTop] = useState(0)
  const [rowHeightPx, setRowHeightPx] = useState(MENU_ROW_ESTIMATE_PX)
  const btnRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const zones = useMemo(() => (open ? getSupportedZonesCached() : EMPTY_ZONES), [open])
  const zoneOffsets = useMemo(
    () => {
      void offsetCacheVersion
      return open ? getCachedOffsets(labelNow) : EMPTY_ZONE_OFFSETS
    },
    [open, labelNow, offsetCacheVersion],
  )

  const userZone = useMemo(() => {
    const raw = me?.timezone?.trim() ?? ''
    if (!raw) return null
    return normalizeTimeZone(raw) ?? raw
  }, [me?.timezone])
  const accountZone = useMemo(() => {
    const raw = me?.account_timezone?.trim() ?? ''
    if (!raw) return null
    return normalizeTimeZone(raw) ?? raw
  }, [me?.account_timezone])

  const deviceTz = useMemo(() => detectDeviceTimeZone(), [])
  const storedMatchesDevice = userZone != null && userZone === deviceTz
  const triggerLabel = storedMatchesDevice
    ? `${t.timezoneUseDevice} · ${formatTimeZoneOffset(labelNow, deviceTz)}`
    : userZone != null
      ? `${userZone} · ${formatTimeZoneOffset(labelNow, userZone)}`
      : accountZone != null
        ? `${t.timezoneUseAccountDefault} · ${formatTimeZoneOffset(labelNow, accountZone)}`
        : `${t.timezoneUseDevice} · ${formatTimeZoneOffset(labelNow, deviceTz)}`

  const persist = async (choice: string) => {
    if (saving) return
    const payload = choice === '' ? '' : (normalizeTimeZone(choice) ?? choice)
    if (choice !== '' && normalizeTimeZone(choice) == null) return
    setSaving(true)
    try {
      const result = await updateMe(accessToken, { timezone: payload })
      if (me && onMeUpdated) {
        const nextTz = result.timezone !== undefined ? result.timezone : null
        onMeUpdated({ ...me, timezone: nextTz })
      }
    } catch {
      addToast(t.requestFailed, 'error')
    } finally {
      setSaving(false)
    }
  }

  useEffect(() => {
    let cancelled = false
    scheduleIdleWork(() => {
      if (cancelled) return
      void preloadOffsets(labelNow).then(() => {
        if (!cancelled) setOffsetCacheVersion((value) => value + 1)
      })
    })
    return () => {
      cancelled = true
    }
  }, [labelNow])

  useEffect(() => {
    if (!open) return
    setLabelNow(new Date())
    let cancelled = false
    const outer = requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        if (!cancelled) searchRef.current?.focus()
      })
    })
    return () => {
      cancelled = true
      cancelAnimationFrame(outer)
    }
  }, [open])

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (
        menuRef.current?.contains(e.target as Node)
        || btnRef.current?.contains(e.target as Node)
      ) {
        return
      }
      setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  const prepareTimeZoneMenu = (value = new Date()) => {
    void preloadOffsets(value).then(() => {
      setOffsetCacheVersion((current) => current + 1)
    })
  }

  const handleToggle = () => {
    if (saving) return
    if (open) {
      setOpen(false)
      return
    }
    const nextNow = new Date()
    setSearch('')
    setLabelNow(nextNow)
    setListScrollTop(0)
    if (btnRef.current) {
      const rect = btnRef.current.getBoundingClientRect()
      const viewportWidth = typeof window === 'undefined' ? rect.width + 32 : window.innerWidth
      const menuLabels = [
        t.timezoneUseDevice,
        ...(accountZone ? [t.timezoneUseAccountDefault] : []),
        ...getSupportedZonesCached(),
      ]
      const width = estimateMenuWidth(rect.width, menuLabels, viewportWidth)
      setMenuStyle({
        position: 'fixed',
        top: rect.bottom + 4,
        left: getAdaptiveMenuLeft(rect, width, viewportWidth),
        width,
        minWidth: rect.width,
        zIndex: 9999,
      })
    }
    prepareTimeZoneMenu(nextNow)
    setOpen(true)
  }

  const q = search.trim().toLowerCase()
  const deviceMatches = !q || t.timezoneUseDevice.toLowerCase().includes(q)
  const accountMatches = accountZone != null && (!q || t.timezoneUseAccountDefault.toLowerCase().includes(q))
  const filteredZones = useMemo(
    () => (open ? zones.filter((z) => !q || z.toLowerCase().includes(q)) : EMPTY_ZONES),
    [open, zones, q],
  )

  const deviceRowActive =
    (userZone == null && accountZone == null)
    || storedMatchesDevice

  const headerNow = labelNow

  const menuRows = useMemo((): MenuRow[] => {
    const out: MenuRow[] = []
    if (deviceMatches) {
      out.push({
        key: '__device__',
        label: t.timezoneUseDevice,
        offset: formatTimeZoneOffset(headerNow, deviceTz),
        persistValue: deviceTz,
        active: deviceRowActive,
      })
    }
    if (accountMatches) {
      out.push({
        key: '__account__',
        label: t.timezoneUseAccountDefault,
        offset: formatTimeZoneOffset(headerNow, accountZone!),
        persistValue: '',
        active: userZone == null,
      })
    }
    for (const z of filteredZones) {
      out.push({
        key: z,
        label: z,
        offset: zoneOffsets.get(z) ?? '',
        persistValue: z,
        active: userZone != null && z === userZone,
      })
    }
    return out
  }, [
    deviceMatches,
    accountMatches,
    filteredZones,
    zoneOffsets,
    t.timezoneUseDevice,
    t.timezoneUseAccountDefault,
    headerNow,
    deviceTz,
    accountZone,
    userZone,
    deviceRowActive,
  ])

  useEffect(() => {
    const el = listRef.current
    if (el) el.scrollTop = 0
  }, [q, open])

  const measureRowHeight = useCallback((node: HTMLButtonElement | null) => {
    if (!node) return
    const nextHeight = Math.round(node.getBoundingClientRect().height)
    if (nextHeight <= 0) return
    setRowHeightPx((current) => (current === nextHeight ? current : nextHeight))
  }, [])

  const visibleWindow = useMemo(() => {
    if (!open) {
      return { items: [] as MenuRow[], topPadding: 0, bottomPadding: 0 }
    }
    const visibleCount = Math.ceil(MENU_MAX_HEIGHT_PX / rowHeightPx) + MENU_OVERSCAN_ROWS * 2
    const start = Math.max(0, Math.floor(listScrollTop / rowHeightPx) - MENU_OVERSCAN_ROWS)
    const end = Math.min(menuRows.length, start + visibleCount)
    return {
      items: menuRows.slice(start, end),
      topPadding: start * rowHeightPx,
      bottomPadding: Math.max(0, (menuRows.length - end) * rowHeightPx),
    }
  }, [listScrollTop, menuRows, open, rowHeightPx])

  const menu = open ? (
    <div
      ref={menuRef}
      className="dropdown-menu"
      style={{
        ...menuStyle,
        border: `0.65px solid ${settingsSelectBorderColor}`,
        borderRadius: '10px',
        padding: '4px',
        background: 'var(--c-bg-menu)',
        boxShadow: 'var(--c-dropdown-shadow)',
        boxSizing: 'border-box',
        display: 'flex',
        flexDirection: 'column',
        gap: '4px',
        overflow: 'hidden',
        maxHeight: 'min(320px, calc(100vh - 120px))',
      }}
    >
      <div className="shrink-0 px-1 py-0.5">
        <input
          ref={searchRef}
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className={settingsInputCls('sm')}
        />
      </div>
      <div
        ref={listRef}
        className="min-h-0 overflow-y-auto"
        style={{ maxHeight: `${MENU_MAX_HEIGHT_PX}px` }}
        onScroll={(event) => setListScrollTop(event.currentTarget.scrollTop)}
      >
        {visibleWindow.topPadding > 0 && (
          <div aria-hidden="true" style={{ height: `${visibleWindow.topPadding}px` }} />
        )}
        {visibleWindow.items.map((row, index) => (
          <button
            key={row.key}
            ref={index === 0 ? measureRowHeight : undefined}
            type="button"
            className={ROW_CLS}
            style={{
              borderRadius: '6.5px',
              fontWeight: 450,
              color: row.active ? 'var(--c-text-heading)' : 'var(--c-text-secondary)',
              background: row.active ? 'var(--c-bg-deep)' : 'var(--c-bg-menu)',
            }}
            onClick={() => {
              setOpen(false)
              void persist(row.persistValue)
            }}
          >
            <span className="min-w-0 truncate text-left">{row.label}</span>
            <span className="shrink-0 tabular-nums text-xs text-[var(--c-text-muted)]">{row.offset}</span>
          </button>
        ))}
        {visibleWindow.bottomPadding > 0 && (
          <div aria-hidden="true" style={{ height: `${visibleWindow.bottomPadding}px` }} />
        )}
      </div>
    </div>
  ) : null

  return (
    <div className={`flex flex-col gap-2${className ? ` ${className}` : ''}`}>
      {showLabel && (
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.timezone}</span>
      )}
      <div className={`relative inline-flex max-w-full${triggerClassName ? ` ${triggerClassName}` : ''}`}>
        <button
          ref={btnRef}
          type="button"
          disabled={saving}
          onClick={handleToggle}
          onFocus={() => prepareTimeZoneMenu()}
          onMouseEnter={() => prepareTimeZoneMenu()}
          className={[
            'inline-flex h-9 max-w-[360px] items-center justify-between rounded-[6.5px] border-[0.65px] bg-[var(--c-bg-input)] px-3 text-sm font-[450] text-[var(--c-text-primary)] [background-clip:padding-box] transition-colors duration-[180ms] hover:bg-[var(--c-bg-deep)] disabled:cursor-not-allowed disabled:opacity-50',
            triggerButtonClassName,
          ].filter(Boolean).join(' ')}
          style={{
            borderColor: settingsSelectBorderColor,
          }}
        >
          <span className="min-w-0 truncate text-left">{triggerLabel}</span>
          <ChevronDown size={13} className="ml-2 shrink-0 text-[var(--c-text-muted)] opacity-80" />
        </button>
        {menu && createPortal(menu, document.body)}
      </div>
    </div>
  )
}
