import { type CSSProperties, type KeyboardEvent as ReactKeyboardEvent } from 'react'
import { Loader2 } from 'lucide-react'
import { statusLabel, statusVariant, type MCPCopy } from './types'
import type { MCPInstall } from '../../api'
import { SettingsSwitch } from '../settings/_SettingsSwitch'

type Props = {
  installs: MCPInstall[]
  loading: boolean
  busyID: string | null
  onEdit: (install: MCPInstall) => void
  onToggle: (install: MCPInstall) => void
  copy: MCPCopy
}

function statusBadgeStyle(status: string): CSSProperties {
  const variant = statusVariant(status)
  switch (variant) {
    case 'success':
      return { background: 'var(--c-status-ok-bg)', color: 'var(--c-status-ok-text)' }
    case 'warning':
      return { background: 'var(--c-status-danger-bg)', color: 'var(--c-status-warning-text)' }
    case 'error':
      return { background: 'var(--c-status-danger-bg)', color: 'var(--c-status-danger-text)' }
    default:
      return { background: 'var(--c-bg-deep)', color: 'var(--c-text-tertiary)' }
  }
}

function rowSubtitle(install: MCPInstall): { text: string; tone: 'error' | 'muted' } {
  if (install.last_error_message?.trim()) {
    return { text: install.last_error_message.trim(), tone: 'error' }
  }
  const launch = install.launch_spec ?? {}
  if (install.transport === 'stdio') {
    const cmd = typeof launch.command === 'string' ? launch.command.trim() : ''
    return { text: cmd || install.transport, tone: 'muted' }
  }
  const url = typeof launch.url === 'string' ? launch.url.trim() : ''
  return { text: url || install.source_uri?.trim() || install.transport, tone: 'muted' }
}

export function MCPInstallList({
  installs,
  loading,
  busyID,
  onEdit,
  onToggle,
  copy,
}: Props) {
  if (loading) {
    return (
      <div className="grid min-h-[220px] place-items-center rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] text-[var(--c-text-muted)]">
        <Loader2 size={18} className="animate-spin" />
      </div>
    )
  }

  if (installs.length === 0) {
    return (
      <div className="rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] px-5 py-6 text-center text-sm text-[var(--c-text-tertiary)]">
        {copy.empty}
      </div>
    )
  }

  return (
    <div className="grid gap-2">
      {installs.map((install) => {
        const busy = busyID === install.id
        const sub = rowSubtitle(install)

        const handleMainKeyDown = (e: ReactKeyboardEvent<HTMLDivElement>) => {
          if (busy) return
          if (e.key !== 'Enter' && e.key !== ' ') return
          e.preventDefault()
          onEdit(install)
        }

        return (
          <div
            key={install.id}
            className="overflow-hidden rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] transition-colors duration-[140ms] hover:bg-[var(--c-bg-deep)]"
          >
            <div className="grid w-full grid-cols-[minmax(0,1fr)_auto] items-center">
              <div
                role="button"
                tabIndex={busy ? -1 : 0}
                className={[
                  'flex cursor-pointer items-center px-4 py-3 outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-[var(--c-accent)]',
                  busy ? 'pointer-events-none opacity-50' : '',
                ].join(' ')}
                onClick={() => { if (!busy) onEdit(install) }}
                onKeyDown={handleMainKeyDown}
              >
                <div className="min-w-0 flex-1">
                  <div className="flex min-w-0 flex-wrap items-center gap-2">
                    <h3 className="truncate text-[14px] font-semibold leading-5 text-[var(--c-text-primary)]">
                      {install.display_name}
                    </h3>
                    <span
                      className="inline-flex h-5 shrink-0 items-center rounded px-1.5 text-[10px] font-medium leading-none"
                      style={busy ? statusBadgeStyle('needs_check') : statusBadgeStyle(install.discovery_status)}
                    >
                      {busy && <Loader2 size={10} className="mr-0.5 animate-spin" />}
                      {busy ? copy.loading : statusLabel(install.discovery_status, copy.status)}
                    </span>
                  </div>
                  <p
                    className={[
                      'mt-1 truncate text-[12.5px] leading-5',
                      sub.tone === 'error' ? '' : 'text-[var(--c-text-tertiary)]',
                    ].filter(Boolean).join(' ')}
                    style={sub.tone === 'error' ? { color: 'var(--c-status-error-text)' } : undefined}
                    title={sub.text}
                  >
                    {sub.text}
                  </p>
                </div>
              </div>

              <div
                className="flex items-center px-4"
                onClick={(e) => e.stopPropagation()}
                onKeyDown={(e) => e.stopPropagation()}
              >
                <SettingsSwitch
                  checked={install.workspace_state?.enabled ?? false}
                  disabled={busy}
                  onChange={() => onToggle(install)}
                />
              </div>
            </div>
          </div>
        )
      })}
    </div>
  )
}
