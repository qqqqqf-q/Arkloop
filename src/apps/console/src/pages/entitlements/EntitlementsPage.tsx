import { useCallback, useEffect, useMemo, useState } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Loader2, Save, Trash2 } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '@arkloop/shared'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import {
  deletePlatformSetting,
  getPlatformSetting,
  setPlatformSetting,
} from '../../api/platform-settings'
import {
  deleteEntitlementOverride,
  listEntitlementOverrides,
  upsertEntitlementOverride,
  type EntitlementOverride,
} from '../../api/entitlements'
import { getGatewayConfig, updateGatewayConfig, type GatewayConfig } from '../../api/gateway-config'

type LimitField = {
  key: string
  label: string
  defaultValue: string
  min: number
  allowZeroUnlimited?: boolean
}

const LIMIT_FIELDS: LimitField[] = [
  { key: 'quota.runs_per_month', label: '每月运行次数', defaultValue: '999999', min: 0, allowZeroUnlimited: true },
  { key: 'quota.tokens_per_month', label: '每月 Token 上限', defaultValue: '1000000', min: 0, allowZeroUnlimited: true },
  { key: 'limit.concurrent_runs', label: '并发运行上限', defaultValue: '10', min: 1 },
  { key: 'limit.team_members', label: '团队成员上限', defaultValue: '50', min: 0, allowZeroUnlimited: true },
  { key: 'invite.default_max_uses', label: '邀请码默认可用次数', defaultValue: '1', min: 1 },
]

const DEFAULT_VALUES = LIMIT_FIELDS.reduce<Record<string, string>>((acc, item) => {
  acc[item.key] = item.defaultValue
  return acc
}, {})

const inputClass =
  'w-full rounded-md border border-[var(--c-border-console)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none focus:border-[var(--c-border-focus)]'

function findField(key: string) {
  return LIMIT_FIELDS.find((item) => item.key === key)
}

function parseAndValidate(raw: string, field: LimitField): { value?: string; error?: string } {
  const trimmed = raw.trim()
  if (trimmed === '') {
    return { error: '数值不能为空' }
  }
  if (!/^-?\d+$/.test(trimmed)) {
    return { error: '请输入整数' }
  }
  const num = Number(trimmed)
  if (!Number.isSafeInteger(num)) {
    return { error: '数值超出范围' }
  }
  if (num < field.min) {
    return { error: field.min === 0 ? '数值不能为负' : `数值不能小于 ${field.min}` }
  }
  if (!field.allowZeroUnlimited && num === 0) {
    return { error: `数值不能为 0` }
  }
  return { value: String(num) }
}

function overridesToMap(items: EntitlementOverride[]): Record<string, EntitlementOverride> {
  const result: Record<string, EntitlementOverride> = {}
  for (const item of items) {
    result[item.key] = item
  }
  return result
}

export function EntitlementsPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()

  const [loadingGlobal, setLoadingGlobal] = useState(true)
  const [savingGlobal, setSavingGlobal] = useState(false)
  const [globalValues, setGlobalValues] = useState<Record<string, string>>(DEFAULT_VALUES)
  const [savedGlobalValues, setSavedGlobalValues] = useState<Record<string, string>>(DEFAULT_VALUES)

  const [gatewayConfig, setGatewayConfig] = useState<GatewayConfig | null>(null)
  const [gatewayCapacity, setGatewayCapacity] = useState('')
  const [gatewayPerMinute, setGatewayPerMinute] = useState('')
  const [savingGateway, setSavingGateway] = useState(false)

  const [projectId, setProjectId] = useState('')
  const [loadingOverrides, setLoadingOverrides] = useState(false)
  const [savingOverrideKey, setSavingOverrideKey] = useState<string | null>(null)
  const [deletingOverrideKey, setDeletingOverrideKey] = useState<string | null>(null)
  const [overrideByKey, setOverrideByKey] = useState<Record<string, EntitlementOverride>>({})
  const [overrideDrafts, setOverrideDrafts] = useState<Record<string, string>>({})

  const loadGlobal = useCallback(async () => {
    setLoadingGlobal(true)
    try {
      const entries = await Promise.all(
        LIMIT_FIELDS.map(async (field) => {
          try {
            const setting = await getPlatformSetting(field.key, accessToken)
            return [field.key, setting.value] as const
          } catch (err) {
            if (isApiError(err) && err.status === 404) {
              return [field.key, field.defaultValue] as const
            }
            throw err
          }
        }),
      )
      const nextValues = Object.fromEntries(entries)
      setGlobalValues(nextValues)
      setSavedGlobalValues(nextValues)
    } catch {
      addToast('加载全局限制失败', 'error')
    } finally {
      setLoadingGlobal(false)
    }
  }, [accessToken, addToast])

  const loadGateway = useCallback(async () => {
    try {
      const cfg = await getGatewayConfig(accessToken)
      setGatewayConfig(cfg)
      setGatewayCapacity(String(cfg.rate_limit_capacity))
      setGatewayPerMinute(String(cfg.rate_limit_per_minute))
    } catch {
      addToast('加载网关限流失败', 'error')
    }
  }, [accessToken, addToast])

  useEffect(() => {
    void loadGlobal()
    void loadGateway()
  }, [loadGlobal, loadGateway])

  const globalChanged = useMemo(() => {
    return LIMIT_FIELDS.some((field) => globalValues[field.key] !== savedGlobalValues[field.key])
  }, [globalValues, savedGlobalValues])

  const gatewayChanged = useMemo(() => {
    if (!gatewayConfig) return false
    return (
      gatewayCapacity.trim() !== String(gatewayConfig.rate_limit_capacity) ||
      gatewayPerMinute.trim() !== String(gatewayConfig.rate_limit_per_minute)
    )
  }, [gatewayCapacity, gatewayPerMinute, gatewayConfig])

  const handleSaveGlobal = useCallback(async () => {
    const normalized: Record<string, string> = {}
    for (const field of LIMIT_FIELDS) {
      const checked = parseAndValidate(globalValues[field.key] ?? '', field)
      if (!checked.value) {
        addToast(`${field.label}: ${checked.error ?? '格式错误'}`, 'error')
        return
      }
      normalized[field.key] = checked.value
    }

    setSavingGlobal(true)
    try {
      const tasks: Promise<unknown>[] = []
      for (const field of LIMIT_FIELDS) {
        if (normalized[field.key] !== savedGlobalValues[field.key]) {
          tasks.push(setPlatformSetting(field.key, normalized[field.key], accessToken))
        }
      }
      await Promise.all(tasks)
      setGlobalValues(normalized)
      setSavedGlobalValues(normalized)
      addToast('全局限制已保存', 'success')
    } catch (err) {
      addToast(isApiError(err) ? err.message : '保存全局限制失败', 'error')
    } finally {
      setSavingGlobal(false)
    }
  }, [accessToken, addToast, globalValues, savedGlobalValues])

  const handleResetGlobal = useCallback(async () => {
    setSavingGlobal(true)
    try {
      await Promise.all(LIMIT_FIELDS.map((field) => deletePlatformSetting(field.key, accessToken)))
      const reset = { ...DEFAULT_VALUES }
      setGlobalValues(reset)
      setSavedGlobalValues(reset)
      addToast('已恢复默认限制', 'success')
    } catch (err) {
      addToast(isApiError(err) ? err.message : '恢复默认失败', 'error')
    } finally {
      setSavingGlobal(false)
    }
  }, [accessToken, addToast])

  const handleSaveGateway = useCallback(async () => {
    if (!gatewayConfig) return
    const c = Number(gatewayCapacity.trim())
    const r = Number(gatewayPerMinute.trim())
    if (!Number.isFinite(c) || c <= 0) {
      addToast('网关突发容量必须大于 0', 'error')
      return
    }
    if (!Number.isFinite(r) || r <= 0) {
      addToast('网关每分钟速率必须大于 0', 'error')
      return
    }

    setSavingGateway(true)
    try {
      const updated = await updateGatewayConfig(
        {
          ip_mode: gatewayConfig.ip_mode,
          trusted_cidrs: gatewayConfig.trusted_cidrs,
          risk_reject_threshold: gatewayConfig.risk_reject_threshold,
          rate_limit_capacity: c,
          rate_limit_per_minute: r,
        },
        accessToken,
      )
      setGatewayConfig(updated)
      setGatewayCapacity(String(updated.rate_limit_capacity))
      setGatewayPerMinute(String(updated.rate_limit_per_minute))
      addToast('网关限流已保存', 'success')
    } catch (err) {
      addToast(isApiError(err) ? err.message : '保存网关限流失败', 'error')
    } finally {
      setSavingGateway(false)
    }
  }, [accessToken, addToast, gatewayCapacity, gatewayConfig, gatewayPerMinute])

  const handleLoadOverrides = useCallback(async () => {
    const id = projectId.trim()
    if (!id) {
      addToast('请先输入项目 ID', 'error')
      return
    }
    setLoadingOverrides(true)
    try {
      const items = await listEntitlementOverrides(id, accessToken)
      const byKey = overridesToMap(items)
      setOverrideByKey(byKey)
      const drafts: Record<string, string> = {}
      for (const field of LIMIT_FIELDS) {
        drafts[field.key] = byKey[field.key]?.value ?? ''
      }
      setOverrideDrafts(drafts)
      addToast('已加载项目覆盖', 'success')
    } catch (err) {
      addToast(isApiError(err) ? err.message : '加载项目覆盖失败', 'error')
    } finally {
      setLoadingOverrides(false)
    }
  }, [accessToken, addToast, projectId])

  const handleSaveOverride = useCallback(async (key: string) => {
    const field = findField(key)
    if (!field) return
    const id = projectId.trim()
    if (!id) {
      addToast('请先输入项目 ID', 'error')
      return
    }
    const checked = parseAndValidate(overrideDrafts[key] ?? '', field)
    if (!checked.value) {
      addToast(`${field.label}: ${checked.error ?? '格式错误'}`, 'error')
      return
    }

    setSavingOverrideKey(key)
    try {
      const saved = await upsertEntitlementOverride(
        {
          account_id: id,
          key,
          value: checked.value,
          value_type: 'int',
          reason: 'console override',
        },
        accessToken,
      )
      setOverrideByKey((prev) => ({ ...prev, [key]: saved }))
      setOverrideDrafts((prev) => ({ ...prev, [key]: checked.value! }))
      addToast(`${field.label} 覆盖已保存`, 'success')
    } catch (err) {
      addToast(isApiError(err) ? err.message : '保存项目覆盖失败', 'error')
    } finally {
      setSavingOverrideKey(null)
    }
  }, [accessToken, addToast, projectId, overrideDrafts])

  const handleDeleteOverride = useCallback(async (key: string) => {
    const field = findField(key)
    if (!field) return
    const id = projectId.trim()
    if (!id) {
      addToast('请先输入项目 ID', 'error')
      return
    }
    const existing = overrideByKey[key]
    if (!existing) {
      setOverrideDrafts((prev) => ({ ...prev, [key]: '' }))
      return
    }

    setDeletingOverrideKey(key)
    try {
      await deleteEntitlementOverride(existing.id, id, accessToken)
      setOverrideByKey((prev) => {
        const next = { ...prev }
        delete next[key]
        return next
      })
      setOverrideDrafts((prev) => ({ ...prev, [key]: '' }))
      addToast(`${field.label} 覆盖已删除`, 'success')
    } catch (err) {
      addToast(isApiError(err) ? err.message : '删除项目覆盖失败', 'error')
    } finally {
      setDeletingOverrideKey(null)
    }
  }, [accessToken, addToast, projectId, overrideByKey])

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={t.nav.entitlements} />

      <div className="flex-1 space-y-6 overflow-y-auto p-6">
        <section className="rounded-lg border border-[var(--c-border-console)] bg-[var(--c-bg-card)] p-5">
          <div className="mb-4 flex items-center justify-between">
            <div>
              <h2 className="text-sm font-medium text-[var(--c-text-primary)]">全局默认限制</h2>
              <p className="mt-1 text-xs text-[var(--c-text-muted)]">0 仅对支持不限额的项目生效。</p>
            </div>
            <div className="flex items-center gap-2">
              <button
                onClick={handleResetGlobal}
                disabled={savingGlobal || loadingGlobal}
                className="inline-flex items-center gap-1 rounded-md border border-[var(--c-border-console)] px-3 py-1.5 text-xs text-[var(--c-text-secondary)] disabled:opacity-50"
              >
                重置默认
              </button>
              <button
                onClick={handleSaveGlobal}
                disabled={savingGlobal || loadingGlobal || !globalChanged}
                className="inline-flex items-center gap-1 rounded-md border border-[var(--c-border-console)] px-3 py-1.5 text-xs text-[var(--c-text-secondary)] disabled:opacity-50"
              >
                {savingGlobal ? <Loader2 size={12} className="animate-spin" /> : <Save size={12} />}
                保存
              </button>
            </div>
          </div>

          {loadingGlobal ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 size={16} className="animate-spin text-[var(--c-text-muted)]" />
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
              {LIMIT_FIELDS.map((field) => (
                <label key={field.key} className="space-y-1">
                  <span className="block text-xs font-medium text-[var(--c-text-secondary)]">{field.label}</span>
                  <input
                    type="number"
                    min={field.min}
                    value={globalValues[field.key] ?? ''}
                    onChange={(e) => setGlobalValues((prev) => ({ ...prev, [field.key]: e.target.value }))}
                    className={inputClass}
                  />
                  <span className="block text-[11px] text-[var(--c-text-muted)]">{field.key}</span>
                </label>
              ))}
            </div>
          )}
        </section>

        <section className="rounded-lg border border-[var(--c-border-console)] bg-[var(--c-bg-card)] p-5">
          <div className="mb-4 flex items-center justify-between">
            <div>
              <h2 className="text-sm font-medium text-[var(--c-text-primary)]">网关限流</h2>
              <p className="mt-1 text-xs text-[var(--c-text-muted)]">直接作用于网关 Token Bucket。</p>
            </div>
            <button
              onClick={handleSaveGateway}
              disabled={savingGateway || !gatewayChanged}
              className="inline-flex items-center gap-1 rounded-md border border-[var(--c-border-console)] px-3 py-1.5 text-xs text-[var(--c-text-secondary)] disabled:opacity-50"
            >
              {savingGateway ? <Loader2 size={12} className="animate-spin" /> : <Save size={12} />}
              保存
            </button>
          </div>

          <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <label className="space-y-1">
              <span className="block text-xs font-medium text-[var(--c-text-secondary)]">突发容量</span>
              <input
                type="number"
                min={1}
                value={gatewayCapacity}
                onChange={(e) => setGatewayCapacity(e.target.value)}
                className={inputClass}
              />
              <span className="block text-[11px] text-[var(--c-text-muted)]">gateway.ratelimit_capacity</span>
            </label>
            <label className="space-y-1">
              <span className="block text-xs font-medium text-[var(--c-text-secondary)]">每分钟速率</span>
              <input
                type="number"
                min={1}
                value={gatewayPerMinute}
                onChange={(e) => setGatewayPerMinute(e.target.value)}
                className={inputClass}
              />
              <span className="block text-[11px] text-[var(--c-text-muted)]">gateway.ratelimit_rate_per_minute</span>
            </label>
          </div>
        </section>

        <section className="rounded-lg border border-[var(--c-border-console)] bg-[var(--c-bg-card)] p-5">
          <div className="mb-4">
            <h2 className="text-sm font-medium text-[var(--c-text-primary)]">项目覆盖</h2>
            <p className="mt-1 text-xs text-[var(--c-text-muted)]">按项目覆盖全局默认限制。</p>
          </div>

          <div className="mb-4 flex flex-col gap-2 lg:flex-row">
            <input
              type="text"
              value={projectId}
              onChange={(e) => setProjectId(e.target.value)}
              placeholder="account_id"
              className={inputClass}
            />
            <button
              onClick={handleLoadOverrides}
              disabled={loadingOverrides}
              className="inline-flex items-center justify-center gap-1 rounded-md border border-[var(--c-border-console)] px-3 py-1.5 text-xs text-[var(--c-text-secondary)] disabled:opacity-50"
            >
              {loadingOverrides ? <Loader2 size={12} className="animate-spin" /> : null}
              加载
            </button>
          </div>

          <div className="space-y-3">
            {LIMIT_FIELDS.map((field) => {
              const hasOverride = Boolean(overrideByKey[field.key])
              return (
                <div key={field.key} className="rounded-md border border-[var(--c-border-console)] p-3">
                  <div className="mb-2 flex items-center justify-between">
                    <div>
                      <p className="text-xs font-medium text-[var(--c-text-primary)]">{field.label}</p>
                      <p className="text-[11px] text-[var(--c-text-muted)]">{field.key}</p>
                    </div>
                    <span className="text-[11px] text-[var(--c-text-muted)]">{hasOverride ? '已覆盖' : '未覆盖'}</span>
                  </div>
                  <div className="flex flex-col gap-2 lg:flex-row">
                    <input
                      type="number"
                      min={field.min}
                      value={overrideDrafts[field.key] ?? ''}
                      onChange={(e) => setOverrideDrafts((prev) => ({ ...prev, [field.key]: e.target.value }))}
                      className={inputClass}
                    />
                    <button
                      onClick={() => { void handleSaveOverride(field.key) }}
                      disabled={savingOverrideKey === field.key}
                      className="inline-flex items-center justify-center gap-1 rounded-md border border-[var(--c-border-console)] px-3 py-1.5 text-xs text-[var(--c-text-secondary)] disabled:opacity-50"
                    >
                      {savingOverrideKey === field.key ? <Loader2 size={12} className="animate-spin" /> : <Save size={12} />}
                      保存覆盖
                    </button>
                    <button
                      onClick={() => { void handleDeleteOverride(field.key) }}
                      disabled={deletingOverrideKey === field.key}
                      className="inline-flex items-center justify-center gap-1 rounded-md border border-[var(--c-border-console)] px-3 py-1.5 text-xs text-[var(--c-text-secondary)] disabled:opacity-50"
                    >
                      {deletingOverrideKey === field.key ? <Loader2 size={12} className="animate-spin" /> : <Trash2 size={12} />}
                      删除覆盖
                    </button>
                  </div>
                </div>
              )
            })}
          </div>
        </section>
      </div>
    </div>
  )
}
