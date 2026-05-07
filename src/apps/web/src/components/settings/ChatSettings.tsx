import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import { XCircle } from 'lucide-react'
import { useLocale } from '../../contexts/LocaleContext'
import {
  listPlatformSettings,
  updatePlatformSetting,
} from '../../api-admin'
import { bridgeClient } from '../../api-bridge'
import { useToast } from '@arkloop/shared'
import type { DesktopSettingsHydrationSnapshot } from '../DesktopSettings'
import { SettingsSwitch } from './_SettingsSwitch'

const DEFAULT_FALLBACK_WINDOW = 128_000

const KEY_ENABLED = 'context.compact.enabled'
const KEY_PCT = 'context.compact.persist_trigger_context_pct'
const KEY_TARGET = 'context.compact.target_context_pct'
const KEY_FALLBACK = 'context.compact.fallback_context_window_tokens'

const cardShell =
  'rounded-xl border-[0.5px] border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)]'

const rangeClass =
  'h-2 w-full min-w-0 cursor-pointer appearance-none rounded-full bg-[var(--c-bg-deep)] ' +
  '[&::-webkit-slider-thumb]:h-3.5 [&::-webkit-slider-thumb]:w-3.5 [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:rounded-full ' +
  '[&::-webkit-slider-thumb]:border-0 [&::-webkit-slider-thumb]:bg-[var(--c-accent)] [&::-webkit-slider-thumb]:shadow-sm ' +
  '[&::-moz-range-thumb]:h-3.5 [&::-moz-range-thumb]:w-3.5 [&::-moz-range-thumb]:rounded-full [&::-moz-range-thumb]:border-0 ' +
  '[&::-moz-range-thumb]:bg-[var(--c-accent)] ' +
  '[&::-moz-range-track]:h-2 [&::-moz-range-track]:rounded-full [&::-moz-range-track]:bg-[var(--c-bg-deep)]'

type Props = {
  accessToken: string
  initialSnapshot?: DesktopSettingsHydrationSnapshot
  onExecutionModeChange?: (mode: 'local' | 'vm') => void
  onPlatformSettingsChange?: (updates: Record<string, string>) => void
}

function parseBool(raw: string | undefined): boolean {
  if (raw == null) return false
  const v = raw.trim().toLowerCase()
  return v === 'true' || v === '1' || v === 'yes'
}

function parsePositiveInt(raw: string | undefined, fallback: number): number {
  if (raw == null || raw.trim() === '') return fallback
  const n = Number.parseInt(raw, 10)
  if (!Number.isFinite(n) || n <= 0) return fallback
  return n
}

export function ChatSettings({
  accessToken,
  initialSnapshot,
  onExecutionModeChange,
  onPlatformSettingsChange,
}: Props) {
  const { t } = useLocale()
  const st = t.desktopSettings
  const { addToast } = useToast()

  const [loading, setLoading] = useState(true)
  const [loadErr, setLoadErr] = useState('')

  const [autoOn, setAutoOn] = useState(false)
  const [thresholdPct, setThresholdPct] = useState(80)
  const [targetPct, setTargetPct] = useState(75)
  const [compactCardHovered, setCompactCardHovered] = useState(false)
  const [execCardHovered, setExecCardHovered] = useState(false)

  const [executionMode, setExecutionMode] = useState<'local' | 'vm'>('local')
  const [execModeLoading, setExecModeLoading] = useState(true)
  const [execModeError, setExecModeError] = useState('')

  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const initializedRef = useRef(false)
  const persistedRef = useRef({ autoOn: false, thresholdPct: 80, targetPct: 75 })

  const applyPlatformSettingsSnapshot = useCallback((values: Record<string, string>, fallbackError = '') => {
    const enabled = parseBool(values[KEY_ENABLED])
    const nextAutoOn = enabled

    let pct = parsePositiveInt(values[KEY_PCT], 0)
    if (pct > 100) pct = 100
    if (pct <= 0) {
      const fb = parsePositiveInt(values[KEY_FALLBACK], DEFAULT_FALLBACK_WINDOW)
      pct = fb > 0 ? 80 : 80
    }
    const nextThresholdPct = Math.min(100, Math.max(5, pct))

    const target = parsePositiveInt(values[KEY_TARGET], 75)
    const nextTargetPct = Math.min(95, Math.max(5, target))

    persistedRef.current = {
      autoOn: nextAutoOn,
      thresholdPct: nextThresholdPct,
      targetPct: nextTargetPct,
    }
    setAutoOn(nextAutoOn)
    setThresholdPct(nextThresholdPct)
    setTargetPct(nextTargetPct)
    setLoadErr(fallbackError)
    setLoading(false)
    initializedRef.current = true
  }, [])

  const load = useCallback(async () => {
    if (initialSnapshot?.platformSettings) {
      applyPlatformSettingsSnapshot(initialSnapshot.platformSettings, initialSnapshot.platformSettingsError)
      return
    }
    setLoadErr('')
    setLoading(true)
    try {
      const rows = await listPlatformSettings(accessToken)
      applyPlatformSettingsSnapshot(Object.fromEntries(rows.map((row) => [row.key, row.value])))
    } catch (e) {
      setLoadErr(e instanceof Error ? e.message : t.requestFailed)
    } finally {
      setLoading(false)
      initializedRef.current = true
    }
  }, [accessToken, applyPlatformSettingsSnapshot, initialSnapshot?.platformSettings, initialSnapshot?.platformSettingsError, t.requestFailed])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    if (!initialSnapshot?.platformSettings) return
    applyPlatformSettingsSnapshot(initialSnapshot.platformSettings, initialSnapshot.platformSettingsError)
  }, [applyPlatformSettingsSnapshot, initialSnapshot?.platformSettings, initialSnapshot?.platformSettingsError])

  const normalizedState = useMemo(() => ({
    autoOn,
    thresholdPct: Math.min(100, Math.max(5, Math.round(thresholdPct))),
    targetPct: Math.min(95, Math.max(5, Math.round(targetPct))),
  }), [autoOn, thresholdPct, targetPct])

  const handleSave = useCallback(async () => {
    const targetClamped = normalizedState.targetPct
    if (targetClamped !== targetPct) setTargetPct(targetClamped)

    const pctClamped = normalizedState.thresholdPct
    if (pctClamped !== thresholdPct) setThresholdPct(pctClamped)

    try {
      const enStr = normalizedState.autoOn ? 'true' : 'false'
      await updatePlatformSetting(accessToken, KEY_ENABLED, enStr)
      await updatePlatformSetting(accessToken, KEY_PCT, String(pctClamped))
      await updatePlatformSetting(accessToken, KEY_TARGET, String(targetClamped))
      onPlatformSettingsChange?.({
        [KEY_ENABLED]: enStr,
        [KEY_PCT]: String(pctClamped),
        [KEY_TARGET]: String(targetClamped),
      })
      persistedRef.current = normalizedState
      addToast(st.chatCompactSaved, 'success')
    } catch (e) {
      addToast(e instanceof Error ? e.message : t.requestFailed, 'error')
    }
  }, [accessToken, addToast, normalizedState, onPlatformSettingsChange, st.chatCompactSaved, t.requestFailed, targetPct, thresholdPct])

  useEffect(() => {
    if (!initializedRef.current) return
    if (
      persistedRef.current.autoOn === normalizedState.autoOn &&
      persistedRef.current.thresholdPct === normalizedState.thresholdPct &&
      persistedRef.current.targetPct === normalizedState.targetPct
    ) {
      return
    }
    if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current)
    debounceTimerRef.current = setTimeout(() => {
      void handleSave()
    }, 500)
    return () => {
      if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current)
    }
  }, [handleSave, normalizedState])

  const loadExecutionMode = useCallback(async () => {
    if (initialSnapshot?.executionMode) {
      setExecutionMode(initialSnapshot.executionMode)
      setExecModeError(initialSnapshot.executionModeError)
      setExecModeLoading(false)
      return
    }
    setExecModeLoading(true)
    setExecModeError('')
    try {
      const mode = await bridgeClient.getExecutionMode()
      setExecutionMode(mode)
    } catch (e) {
      setExecModeError(e instanceof Error ? e.message : 'Failed to load execution mode')
    } finally {
      setExecModeLoading(false)
    }
  }, [initialSnapshot?.executionMode, initialSnapshot?.executionModeError])

  useEffect(() => {
    void loadExecutionMode()
  }, [loadExecutionMode])

  useEffect(() => {
    if (!initialSnapshot?.executionMode) return
    setExecutionMode(initialSnapshot.executionMode)
    setExecModeError(initialSnapshot.executionModeError)
    setExecModeLoading(false)
  }, [initialSnapshot?.executionMode, initialSnapshot?.executionModeError])

  const handleExecutionModeToggle = useCallback(async (vm: boolean) => {
    const newMode = vm ? 'vm' : 'local'
    setExecModeError('')
    setExecutionMode(newMode)
    try {
      await bridgeClient.setExecutionMode(newMode)
      onExecutionModeChange?.(newMode)
      addToast(st.chatCompactSaved, 'success')
    } catch (e) {
      setExecModeError(e instanceof Error ? e.message : 'Failed to set execution mode')
    }
  }, [addToast, onExecutionModeChange, st.chatCompactSaved])

  if (loading) {
    return (
      <div className="text-sm text-[var(--c-text-muted)]">
        {st.chatCompactLoading}
      </div>
    )
  }

  return (
    <div className="flex max-w-xl flex-col gap-4">
      <p className="text-sm font-medium text-[var(--c-text-heading)]">{st.chatCompactCardTitle}</p>

      {loadErr ? (
        <p className="text-sm text-[var(--c-status-error)]">{loadErr}</p>
      ) : null}

      <div className={cardShell}>
        <div
          role="button"
          tabIndex={0}
          className="flex cursor-pointer items-center justify-between gap-4 px-4 py-4 outline-none transition-colors hover:bg-[var(--c-bg-deep)]/25 focus-visible:ring-2 focus-visible:ring-[var(--c-accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--c-bg-page)]"
          onMouseEnter={() => setCompactCardHovered(true)}
          onMouseLeave={() => setCompactCardHovered(false)}
          onClick={() => setAutoOn((v) => !v)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              setAutoOn((v) => !v)
            }
          }}
        >
          <div className="min-w-0 flex-1 pr-2">
            <p className="text-sm font-medium text-[var(--c-text-heading)]">{st.chatCompactEnableLabel}</p>
            <p className="mt-0.5 text-xs text-[var(--c-text-muted)]">{st.chatCompactEnableDesc}</p>
          </div>
          <div className="shrink-0" onClick={(e) => e.stopPropagation()}>
            <SettingsSwitch checked={autoOn} onChange={setAutoOn} forceHover={compactCardHovered} />
          </div>
        </div>

        <div
          className={`flex flex-col gap-3 border-t border-[var(--c-border-subtle)] px-4 py-4 transition-opacity ${autoOn ? '' : 'pointer-events-none opacity-40'}`}
        >
          <div className="flex items-center justify-between gap-3">
            <span className="text-sm font-medium text-[var(--c-text-heading)]">
              {st.chatCompactThresholdLabel}
            </span>
            <span className="shrink-0 rounded-md bg-[var(--c-bg-deep)] px-2.5 py-0.5 text-xs font-medium tabular-nums text-[var(--c-text-secondary)]">
              {thresholdPct}%
            </span>
          </div>
          <div className="flex items-center gap-3">
            <span className="w-9 shrink-0 text-center text-[10px] font-medium uppercase tracking-wide text-[var(--c-text-muted)]">
              {st.chatCompactThresholdEarly}
            </span>
            <input
              type="range"
              min={5}
              max={100}
              step={1}
              value={thresholdPct}
              onChange={(ev) => setThresholdPct(Number(ev.target.value))}
              className={rangeClass}
            />
            <span className="w-9 shrink-0 text-center text-[10px] font-medium uppercase tracking-wide text-[var(--c-text-muted)]">
              {st.chatCompactThresholdLate}
            </span>
          </div>
        </div>

        <div
          className={`flex items-center justify-between gap-4 border-t border-[var(--c-border-subtle)] px-4 py-4 transition-opacity ${autoOn ? '' : 'pointer-events-none opacity-40'}`}
        >
          <div className="min-w-0 flex-1 pr-2">
            <p className="text-sm font-medium text-[var(--c-text-heading)]">{st.chatCompactKeepLabel}</p>
            <p className="mt-0.5 text-xs text-[var(--c-text-muted)]">{st.chatCompactKeepDesc}</p>
          </div>
          <input
            type="number"
            min={5}
            max={95}
            step={1}
            value={targetPct}
            onChange={(ev) => {
              const n = Number.parseInt(ev.target.value, 10)
              if (Number.isFinite(n)) setTargetPct(n)
            }}
            className="h-9 w-14 shrink-0 rounded-lg border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-1 text-center text-sm tabular-nums text-[var(--c-text-primary)] outline-none transition-colors duration-150 focus:border-[var(--c-border)]"
          />
        </div>
      </div>

      {/* Execution Mode */}
      <div className={cardShell}>
        <div
          role="button"
          tabIndex={0}
          className="flex cursor-pointer items-center justify-between gap-4 px-4 py-4 outline-none transition-colors hover:bg-[var(--c-bg-deep)]/25 focus-visible:ring-2 focus-visible:ring-[var(--c-accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--c-bg-page)]"
          onMouseEnter={() => setExecCardHovered(true)}
          onMouseLeave={() => setExecCardHovered(false)}
          onClick={() => { if (!execModeLoading) void handleExecutionModeToggle(executionMode !== 'vm') }}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              if (!execModeLoading) void handleExecutionModeToggle(executionMode !== 'vm')
            }
          }}
        >
          <div className="min-w-0 flex-1 pr-2">
            <p className="text-sm font-medium text-[var(--c-text-heading)]">{st.chatCompactExecutionModeLabel}</p>
            <p className="mt-0.5 text-xs text-[var(--c-text-muted)]">
              {executionMode === 'vm' ? st.chatCompactExecutionModeSandbox : st.chatCompactExecutionModeTerminal}
            </p>
          </div>
          <div className="shrink-0" onClick={(e) => e.stopPropagation()}>
            {execModeLoading ? (
              <div className="h-6 w-12 animate-pulse rounded-full bg-[var(--c-bg-deep)]" />
            ) : (
              <SettingsSwitch
                checked={executionMode === 'vm'}
                onChange={handleExecutionModeToggle}
                forceHover={execCardHovered}
              />
            )}
          </div>
        </div>
        {execModeError ? (
          <div className="border-t border-[var(--c-border-subtle)] flex items-center gap-2 px-4 py-2 text-xs">
            <span className="flex items-center gap-1.5 text-[var(--c-status-error)]"><XCircle size={13} />{execModeError}</span>
          </div>
        ) : null}
      </div>
    </div>
  )
}
