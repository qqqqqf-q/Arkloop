import { useState, useEffect, useCallback } from 'react'
import { Ticket, RefreshCw } from 'lucide-react'
import {
  type InviteCodeResponse,
  getMyInviteCode,
  resetMyInviteCode,
  isApiError,
} from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { CopyIconButton } from '../CopyIconButton'

export function InviteCodeContent({ accessToken }: { accessToken: string }) {
  const { t } = useLocale()
  const [inviteCode, setInviteCode] = useState<InviteCodeResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [resetting, setResetting] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    void (async () => {
      try {
        const code = await getMyInviteCode(accessToken)
        setInviteCode(code)
      } catch {
        setError(t.requestFailed)
      } finally {
        setLoading(false)
      }
    })()
  }, [accessToken, t.requestFailed])

  const handleCopy = useCallback(async () => {
    if (!inviteCode) return
    await navigator.clipboard.writeText(inviteCode.code)
  }, [inviteCode])

  const handleReset = useCallback(async () => {
    setResetting(true)
    setError('')
    try {
      const code = await resetMyInviteCode(accessToken)
      setInviteCode(code)
    } catch (err) {
      if (isApiError(err) && err.code === 'invite_codes.reset_cooldown') {
        setError(t.inviteCodeResetCooldown)
      } else {
        setError(t.requestFailed)
      }
    } finally {
      setResetting(false)
    }
  }, [accessToken, t])

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <Ticket size={15} className="text-[var(--c-text-heading)]" />
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.inviteCode}</span>
      </div>
      <p className="text-xs text-[var(--c-text-tertiary)]">{t.inviteCodeDesc}</p>

      {loading ? (
        <div className="flex h-9 w-[240px] items-center px-3 text-sm text-[var(--c-text-tertiary)]">
          ...
        </div>
      ) : inviteCode ? (
          <div className="flex flex-col gap-2">
          <div
            className="flex w-[360px] items-center gap-2 rounded-lg px-3 py-2"
            style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
          >
            <span className="flex-1 font-mono text-sm font-medium tracking-wider text-[var(--c-text-heading)]">
              {inviteCode.code}
            </span>
            <span className="text-xs text-[var(--c-text-tertiary)]">
              {t.inviteCodeUses(inviteCode.use_count, inviteCode.max_uses)}
            </span>
            <CopyIconButton
              onCopy={handleCopy}
              size={13}
              tooltip={t.inviteCodeCopy}
              className="flex h-6 w-6 items-center justify-center rounded text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)]"
            />
            <button
              onClick={handleReset}
              disabled={resetting}
              className="flex h-6 w-6 items-center justify-center rounded text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)] disabled:opacity-50"
              title={t.inviteCodeReset}
            >
              <RefreshCw size={13} className={resetting ? 'animate-spin' : ''} />
            </button>
          </div>
          {error && (
            <p className="text-xs text-[var(--c-status-error-text)]">{error}</p>
          )}
        </div>
      ) : error ? (
        <p className="text-xs text-[var(--c-status-error-text)]">{error}</p>
      ) : null}
    </div>
  )
}
