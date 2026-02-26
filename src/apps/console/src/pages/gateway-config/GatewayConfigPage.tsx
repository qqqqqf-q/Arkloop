import { useState, useEffect, useCallback } from 'react'
import { useOutletContext } from 'react-router-dom'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { FormField } from '../../components/FormField'
import { useToast } from '../../components/useToast'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { getGatewayConfig, updateGatewayConfig, type GatewayConfig } from '../../api/gateway-config'

export function GatewayConfigPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.gatewayConfig

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  const [ipMode, setIPMode] = useState<string>('direct')
  const [trustedCIDRs, setTrustedCIDRs] = useState('')
  const [riskThreshold, setRiskThreshold] = useState(0)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const cfg = await getGatewayConfig(accessToken)
      applyConfig(cfg)
    } catch {
      addToast(tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc.toastLoadFailed])

  function applyConfig(cfg: GatewayConfig) {
    setIPMode(cfg.ip_mode)
    setTrustedCIDRs((cfg.trusted_cidrs ?? []).join('\n'))
    setRiskThreshold(cfg.risk_reject_threshold ?? 0)
  }

  useEffect(() => {
    void load()
  }, [load])

  const handleSave = useCallback(async () => {
    setSaving(true)
    try {
      const cidrs = trustedCIDRs
        .split('\n')
        .map((s) => s.trim())
        .filter(Boolean)

      const updated = await updateGatewayConfig(
        { ip_mode: ipMode, trusted_cidrs: cidrs, risk_reject_threshold: riskThreshold },
        accessToken,
      )
      applyConfig(updated)
      addToast(tc.toastSaved, 'success')
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastSaveFailed, 'error')
    } finally {
      setSaving(false)
    }
  }, [ipMode, trustedCIDRs, riskThreshold, accessToken, addToast, tc])

  const inputCls =
    'w-full rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] focus:outline-none'

  const headerActions = (
    <button
      onClick={handleSave}
      disabled={saving || loading}
      className="flex items-center gap-1.5 rounded-lg bg-[var(--c-bg-tag)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
    >
      {saving ? '...' : tc.save}
    </button>
  )

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tc.title} actions={headerActions} />

      <div className="flex flex-1 flex-col gap-6 overflow-auto p-6">
        {loading ? (
          <div className="flex items-center justify-center py-12">
            <span className="text-sm text-[var(--c-text-muted)]">...</span>
          </div>
        ) : (
          <>
            <FormField label={tc.fieldIPMode}>
              <select
                value={ipMode}
                onChange={(e) => setIPMode(e.target.value)}
                className={inputCls}
              >
                <option value="direct">{tc.ipModeDirect}</option>
                <option value="cloudflare">{tc.ipModeCloudflare}</option>
                <option value="trusted_proxy">{tc.ipModeTrustedProxy}</option>
              </select>
            </FormField>

            <FormField label={tc.fieldTrustedCIDRs}>
              <textarea
                value={trustedCIDRs}
                onChange={(e) => setTrustedCIDRs(e.target.value)}
                rows={6}
                placeholder="103.21.244.0/22&#10;103.22.200.0/22"
                className={`${inputCls} resize-y font-mono text-xs`}
              />
              <p className="text-xs text-[var(--c-text-muted)]">{tc.fieldTrustedCIDRsHint}</p>
            </FormField>

            <FormField label={tc.fieldRiskThreshold}>
              <input
                type="number"
                min={0}
                max={100}
                value={riskThreshold}
                onChange={(e) => setRiskThreshold(Number(e.target.value))}
                className={inputCls}
              />
              <p className="text-xs text-[var(--c-text-muted)]">{tc.fieldRiskThresholdHint}</p>
            </FormField>
          </>
        )}
      </div>
    </div>
  )
}
