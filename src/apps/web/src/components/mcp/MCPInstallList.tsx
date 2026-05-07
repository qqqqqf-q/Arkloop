import { Loader2, MoreHorizontal, Pencil, RefreshCw, Trash2 } from 'lucide-react'
import { DropdownAction } from '../skills/DropdownAction'
import { statusLabel, statusVariant, type MCPCopy } from './types'
import type { MCPInstall } from '../../api'
import { SettingsSwitch } from '../settings/_SettingsSwitch'

type Props = {
  installs: MCPInstall[]
  loading: boolean
  busyID: string | null
  menuID: string | null
  setMenuID: (id: string | null) => void
  onEdit: (install: MCPInstall) => void
  onDelete: (install: MCPInstall) => void
  onToggle: (install: MCPInstall) => void
  onCheck: (install: MCPInstall) => void
  copy: MCPCopy
  menuRef: React.RefObject<HTMLDivElement | null>
}

function statusBadgeStyle(status: string): React.CSSProperties {
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

export function MCPInstallList({
  installs,
  loading,
  busyID,
  menuID,
  setMenuID,
  onEdit,
  onDelete,
  onToggle,
  onCheck,
  copy,
  menuRef,
}: Props) {
  if (loading) {
    return (
      <div className="flex h-40 items-center justify-center">
        <Loader2 size={16} className="animate-spin text-[var(--c-text-tertiary)]" />
      </div>
    )
  }

  if (installs.length === 0) {
    return (
      <div
        className="flex flex-col items-center justify-center gap-1 rounded-xl py-12 text-center"
        style={{ border: '0.5px solid var(--c-border-subtle)' }}
      >
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{copy.empty}</span>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-2">
      {installs.map((install) => {
        const busy = busyID === install.id
        const isOpen = menuID === install.id

        return (
          <div
            key={install.id}
            className="flex items-center gap-3 rounded-xl px-4 py-3 bg-[var(--c-bg-menu)]"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            <div className="flex min-w-0 flex-1 flex-col gap-1">
              <div className="flex flex-wrap items-center gap-2">
                <span className="truncate text-[13px] font-medium text-[var(--c-text-heading)]">
                  {install.display_name}
                </span>
                <span
                  className="shrink-0 rounded px-1.5 py-px text-[10px] font-medium leading-tight"
                  style={statusBadgeStyle(install.discovery_status)}
                >
                  {statusLabel(install.discovery_status, copy.status)}
                </span>
              </div>
              {install.last_error_message && (
                <p className="text-xs" style={{ color: 'var(--c-status-error-text)' }}>
                  {install.last_error_message}
                </p>
              )}
            </div>

            <div className="flex shrink-0 items-center" onClick={(e) => e.stopPropagation()}>
              <SettingsSwitch
                checked={install.workspace_state?.enabled ?? false}
                disabled={busy}
                onChange={() => onToggle(install)}
              />
            </div>

            <div
              className="relative shrink-0"
              ref={isOpen ? menuRef : undefined}
              onClick={(e) => e.stopPropagation()}
            >
              <button
                type="button"
                onClick={() => setMenuID(isOpen ? null : install.id)}
                className="flex h-7 w-7 items-center justify-center rounded-md text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)]"
              >
                {busy ? (
                  <Loader2 size={14} className="animate-spin" />
                ) : (
                  <MoreHorizontal size={14} />
                )}
              </button>
              {isOpen && (
                <div
                  className="dropdown-menu absolute right-0 top-[calc(100%+4px)] z-50"
                  style={{
                    border: '0.5px solid var(--c-border-subtle)',
                    borderRadius: '10px',
                    padding: '4px',
                    background: 'var(--c-bg-menu)',
                    width: '180px',
                    boxShadow: 'var(--c-dropdown-shadow)',
                  }}
                >
                  <DropdownAction
                    icon={<Pencil size={14} />}
                    label={copy.edit}
                    onClick={() => {
                      setMenuID(null)
                      onEdit(install)
                    }}
                  />
                  <DropdownAction
                    icon={<RefreshCw size={14} />}
                    label={copy.recheck}
                    onClick={() => {
                      setMenuID(null)
                      onCheck(install)
                    }}
                  />
                  <DropdownAction
                    icon={<Trash2 size={14} />}
                    label={copy.delete}
                    destructive
                    onClick={() => {
                      setMenuID(null)
                      onDelete(install)
                    }}
                  />
                </div>
              )}
            </div>
          </div>
        )
      })}
    </div>
  )
}
