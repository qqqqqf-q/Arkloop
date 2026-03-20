import { useState, useEffect, useCallback } from 'react'
import { Sparkles } from 'lucide-react'
import { useLocale } from '../../contexts/LocaleContext'
import {
  getPlatformSetting,
  updatePlatformSetting,
} from '../../api-admin'
import { SettingsSectionHeader } from './_SettingsSectionHeader'

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
    <div className="flex max-w-xl flex-col gap-8">
      <SettingsSectionHeader title={st.chatSectionTitle} />

      <div
        className="rounded-2xl p-5"
        style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
      >
        <div className="flex items-center gap-2">
          <Sparkles size={18} className="text-[var(--c-text-secondary)]" />
          <h4 className="text-sm font-semibold text-[var(--c-text-heading)]">
            {st.chatCompactCardTitle}
          </h4>
        </div>

        {loadErr ? (
          <p className="mt-3 text-sm text-[var(--c-status-error)]">{loadErr}</p>
        ) : null}

        <label className="mt-4 flex cursor-pointer items-start gap-3">
          <input
            type="checkbox"
            className="mt-0.5 h-4 w-4 rounded border-[var(--c-border-mid)]"
            checked={autoOn}
            onChange={(ev) => setAutoOn(ev.target.checked)}
          />
          <div>
            <div className="text-sm font-medium text-[var(--c-text-heading)]">
              {st.chatCompactEnableLabel}
            </div>
            <p className="mt-0.5 text-xs text-[var(--c-text-muted)]">
              {st.chatCompactEnableDesc}
            </p>
          </div>
        </label>

        <div
          className={`mt-6 space-y-4 transition-opacity ${autoOn ? '' : 'pointer-events-none opacity-40'}`}
        >
          <div>
            <div className="flex items-center justify-between gap-2">
              <span className="text-sm font-medium text-[var(--c-text-heading)]">
                {st.chatCompactThresholdLabel}
              </span>
              <span className="text-sm tabular-nums text-[var(--c-text-secondary)]">
                {thresholdPct}%
              </span>
            </div>
            <div className="mt-2 flex items-center gap-2">
              <span className="w-10 shrink-0 text-[11px] text-[var(--c-text-muted)]">
                {st.chatCompactThresholdEarly}
              </span>
              <input
                type="range"
                min={5}
                max={100}
                step={1}
                value={thresholdPct}
                onChange={(ev) => setThresholdPct(Number(ev.target.value))}
                className="min-w-0 flex-1 accent-[var(--c-accent)]"
              />
              <span className="w-10 shrink-0 text-right text-[11px] text-[var(--c-text-muted)]">
                {st.chatCompactThresholdLate}
              </span>
            </div>
            <p className="mt-1.5 text-xs text-[var(--c-text-muted)]">
              {st.chatCompactThresholdDesc}
            </p>
          </div>

          <div>
            <label className="text-sm font-medium text-[var(--c-text-heading)]">
              {st.chatCompactKeepLabel}
            </label>
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
              className="mt-2 w-24 rounded-lg border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-2 text-sm text-[var(--c-text-primary)]"
            />
            <p className="mt-1.5 text-xs text-[var(--c-text-muted)]">
              {st.chatCompactKeepDesc}
            </p>
          </div>
        </div>

        {saveErr ? (
          <p className="mt-4 text-sm text-[var(--c-status-error)]">{saveErr}</p>
        ) : null}
        {savedHint ? (
          <p className="mt-4 text-sm text-[var(--c-status-success)]">{st.chatCompactSaved}</p>
        ) : null}

        <button
          type="button"
          className="mt-5 rounded-lg bg-[var(--c-accent)] px-4 py-2 text-sm font-medium text-[var(--c-accent-fg)] disabled:opacity-50"
          disabled={saving}
          onClick={() => void handleSave()}
        >
          {saving ? st.chatCompactSaving : st.chatCompactSave}
        </button>
      </div>
    </div>
  )
}
