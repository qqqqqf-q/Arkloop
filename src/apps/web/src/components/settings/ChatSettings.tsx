import { useState, useEffect, useCallback } from 'react'
import { CheckCircle, XCircle } from 'lucide-react'
import { useLocale } from '../../contexts/LocaleContext'
import {
  getPlatformSetting,
  updatePlatformSetting,
} from '../../api-admin'
import { bridgeClient } from '../../api-bridge'
import { SettingsPillToggle } from './_SettingsPillToggle'
import { SpinnerIcon } from '@arkloop/shared/components/auth-ui'

const EXEC_MODE_KEY = 'arkloop:desktop:execution_mode'

/** 与 shared/config 注册表默认值一致（无 platform_settings 行时） */
const DEFAULT_KEEP_LAST_MESSAGES = 40
const DEFAULT_FALLBACK_WINDOW = 200_000

const KEY_ENABLED = 'context.compact.enabled'
const KEY_PERSIST = 'context.compact.persist_enabled'
const KEY_PCT = 'context.compact.persist_trigger_context_pct'
const KEY_FALLBACK = 'context.compact.fallback_context_window_tokens'
/** 旧版绝对阈值，仅用于迁移显示 */
const KEY_TRIGGER_LEGACY = 'context.compact.persist_trigger_approx_tokens'
const KEY_KEEP = 'context.compact.persist_keep_last_messages'

const cardShell =
  'overflow-hidden rounded-xl border-[0.5px] border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)]'

const rangeClass =
  'h-2 w-full min-w-0 cursor-pointer appearance-none rounded-full bg-[var(--c-bg-deep)] ' +
  '[&::-webkit-slider-thumb]:h-3.5 [&::-webkit-slider-thumb]:w-3.5 [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:rounded-full ' +
  '[&::-webkit-slider-thumb]:border-0 [&::-webkit-slider-thumb]:bg-[var(--c-accent)] [&::-webkit-slider-thumb]:shadow-sm ' +
  '[&::-moz-range-thumb]:h-3.5 [&::-moz-range-thumb]:w-3.5 [&::-moz-range-thumb]:rounded-full [&::-moz-range-thumb]:border-0 ' +
  '[&::-moz-range-thumb]:bg-[var(--c-accent)] ' +
  '[&::-moz-range-track]:h-2 [&::-moz-range-track]:rounded-full [&::-moz-range-track]:bg-[var(--c-bg-deep)]'

type Props = {
  accessToken: string
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

export function ChatSettings({ accessToken }: Props) {
  const { t } = useLocale()
  const st = t.desktopSettings

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [loadErr, setLoadErr] = useState('')
  const [saveErr, setSaveErr] = useState('')
  const [savedHint, setSavedHint] = useState(false)

  const [autoOn, setAutoOn] = useState(false)
  const [thresholdPct, setThresholdPct] = useState(80)
  const [keepLast, setKeepLast] = useState(4)

  const [executionMode, setExecutionMode] = useState<'local' | 'vm'>('local')
  const [execModeLoading, setExecModeLoading] = useState(true)
  const [execModeError, setExecModeError] = useState('')
  const [execSaveResult, setExecSaveResult] = useState<'ok' | 'error' | null>(null)

  const load = useCallback(async () => {
    setLoadErr('')
    setLoading(true)
    try {
      const [en, pe, pctRow, fbRow, trLeg, ke] = await Promise.all([
        getPlatformSetting(accessToken, KEY_ENABLED),
        getPlatformSetting(accessToken, KEY_PERSIST),
        getPlatformSetting(accessToken, KEY_PCT),
        getPlatformSetting(accessToken, KEY_FALLBACK),
        getPlatformSetting(accessToken, KEY_TRIGGER_LEGACY),
        getPlatformSetting(accessToken, KEY_KEEP),
      ])
      const enabled = parseBool(en?.value)
      const persist = parseBool(pe?.value)
      setAutoOn(enabled && persist)

      let pct = parsePositiveInt(pctRow?.value, 0)
      if (pct > 100) pct = 100
      if (pct <= 0) {
        const fb = parsePositiveInt(fbRow?.value, DEFAULT_FALLBACK_WINDOW)
        const triggerTok = parsePositiveInt(trLeg?.value, 0)
        if (triggerTok > 0 && fb > 0) {
          pct = Math.min(100, Math.max(5, Math.round((triggerTok / fb) * 100)))
        } else {
          pct = 80
        }
      }
      setThresholdPct(Math.min(100, Math.max(5, pct)))

      const keep = parsePositiveInt(ke?.value, DEFAULT_KEEP_LAST_MESSAGES)
      setKeepLast(Math.min(50, Math.max(2, keep)))
    } catch (e) {
      setLoadErr(e instanceof Error ? e.message : t.requestFailed)
    } finally {
      setLoading(false)
    }
  }, [accessToken, t.requestFailed])

  useEffect(() => {
    void load()
  }, [load])

  const loadExecutionMode = useCallback(async () => {
    setExecModeLoading(true)
    setExecModeError('')
    // Read from localStorage first (persists across restarts), then sync with bridge
    const stored = localStorage.getItem(EXEC_MODE_KEY) as 'local' | 'vm' | null
    if (stored === 'local' || stored === 'vm') {
      setExecutionMode(stored)
    }
    try {
      const mode = await bridgeClient.getExecutionMode()
      setExecutionMode(mode)
      localStorage.setItem(EXEC_MODE_KEY, mode)
    } catch (e) {
      if (!stored) {
        setExecModeError(e instanceof Error ? e.message : 'Failed to load execution mode')
      }
    } finally {
      setExecModeLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadExecutionMode()
  }, [loadExecutionMode])

  const handleExecutionModeToggle = useCallback(async (vm: boolean) => {
    const newMode = vm ? 'vm' : 'local'
    setExecModeError('')
    setExecSaveResult(null)
    setExecutionMode(newMode)
    localStorage.setItem(EXEC_MODE_KEY, newMode)
    try {
      await bridgeClient.setExecutionMode(newMode)
      setExecSaveResult('ok')
      window.setTimeout(() => setExecSaveResult(null), 3000)
    } catch (e) {
      setExecModeError(e instanceof Error ? e.message : 'Failed to set execution mode')
      setExecSaveResult('error')
    }
  }, [])

  const handleSave = useCallback(async () => {
    setSaveErr('')
    setSavedHint(false)
    const keepClamped = Math.min(50, Math.max(2, Math.floor(keepLast)))
    if (keepClamped !== keepLast) setKeepLast(keepClamped)

    const pctClamped = Math.min(100, Math.max(5, Math.round(thresholdPct)))
    if (pctClamped !== thresholdPct) setThresholdPct(pctClamped)

    setSaving(true)
    try {
      const enStr = autoOn ? 'true' : 'false'
      const keepStr = String(keepClamped)
      await updatePlatformSetting(accessToken, KEY_ENABLED, enStr)
      await updatePlatformSetting(accessToken, KEY_PERSIST, enStr)
      await updatePlatformSetting(accessToken, KEY_PCT, String(pctClamped))
      await updatePlatformSetting(accessToken, KEY_KEEP, keepStr)
      setSavedHint(true)
      window.setTimeout(() => setSavedHint(false), 2000)
    } catch (e) {
      setSaveErr(e instanceof Error ? e.message : t.requestFailed)
    } finally {
      setSaving(false)
    }
  }, [accessToken, autoOn, keepLast, thresholdPct, t.requestFailed])

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
            <SettingsPillToggle checked={autoOn} onChange={setAutoOn} />
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
            min={2}
            max={50}
            step={1}
            value={keepLast}
            onChange={(ev) => {
              const n = Number.parseInt(ev.target.value, 10)
              if (Number.isFinite(n)) setKeepLast(n)
            }}
            className="h-9 w-14 shrink-0 rounded-md border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-1 text-center text-sm tabular-nums text-[var(--c-text-primary)] outline-none focus:border-[var(--c-border)]"
          />
        </div>
      </div>

      {/* Execution Mode */}
      <div className={cardShell}>
        <div className="flex items-center justify-between gap-4 px-4 py-4">
          <div className="min-w-0 flex-1 pr-2">
            <p className="text-sm font-medium text-[var(--c-text-heading)]">{st.chatCompactExecutionModeLabel}</p>
            <p className="mt-0.5 text-xs text-[var(--c-text-muted)]">
              {executionMode === 'vm' ? st.chatCompactExecutionModeSandbox : st.chatCompactExecutionModeTerminal}
            </p>
          </div>
          <div className="shrink-0">
            {execModeLoading ? (
              <div className="h-6 w-12 animate-pulse rounded-full bg-[var(--c-bg-deep)]" />
            ) : (
              <SettingsPillToggle
                checked={executionMode === 'vm'}
                onChange={handleExecutionModeToggle}
              />
            )}
          </div>
        </div>
        {(execModeError || execSaveResult) ? (
          <div className="border-t border-[var(--c-border-subtle)] flex items-center gap-2 px-4 py-2 text-xs">
            {execSaveResult === 'ok' && (
              <span className="flex items-center gap-1.5 text-green-400"><CheckCircle size={13} />{st.chatCompactSaved}</span>
            )}
            {execSaveResult === 'error' && (
              <span className="flex items-center gap-1.5 text-red-400"><XCircle size={13} />{st.chatCompactSaveError}</span>
            )}
            {execModeError && !execSaveResult && (
              <span className="text-[var(--c-status-error)]">{execModeError}</span>
            )}
          </div>
        ) : null}
      </div>

      {saveErr ? (
        <p className="text-sm text-[var(--c-status-error)]">{saveErr}</p>
      ) : null}
      {savedHint ? (
        <span className="flex items-center gap-1.5 text-sm text-green-400"><CheckCircle size={13} />{st.chatCompactSaved}</span>
      ) : null}

      <button
        type="button"
        className="flex w-fit items-center gap-2 rounded-lg bg-[var(--c-btn-bg)] px-4 py-2 text-sm font-medium text-[var(--c-btn-text)] transition-opacity hover:opacity-90 disabled:opacity-50"
        disabled={saving}
        onClick={() => void handleSave()}
      >
        {saving && <SpinnerIcon />}
        {saving ? st.chatCompactSaving : st.chatCompactSave}
      </button>
    </div>
  )
}
