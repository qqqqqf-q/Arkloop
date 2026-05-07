import { useCallback, useEffect, useMemo, useState } from 'react'
import { ChevronDown, ChevronRight, FolderSearch, Loader2, Plus } from 'lucide-react'
import { listMCPDiscoverySources, importMCPInstall, type MCPDiscoverySource, type MCPDiscoveryProposal } from '../../api'
import { SettingsButton } from '../settings/_SettingsButton'
import { SettingsInput } from '../settings/_SettingsInput'
import type { MCPCopy } from './types'

type Props = {
  accessToken: string
  copy: MCPCopy
  onImported: (installId: string) => void
}

const WELL_KNOWN_PATHS = [
  '~/.claude/mcp_servers.json',
  '~/.cursor/mcp.json',
  '~/.copilot/mcp-config.json',
  '~/.kiro/settings/mcp.json',
  '~/.gemini/settings.json',
  '~/.factory/mcp.json',
  '~/.windsurf/mcp.json',
  '~/.cline/mcp_settings.json',
  '~/Library/Application Support/Code/User/mcp.json',
]

function shortenPath(uri: string): string {
  return uri.replace(/^\/Users\/[^/]+\//, '~/').replace(/^\/home\/[^/]+\//, '~/')
}

function sourceLabel(uri: string): string {
  const short = shortenPath(uri)
  if (short.includes('Library/Application Support/Code')) return 'VS Code'
  const parts = short.split('/')
  if (parts.length >= 2) {
    const folder = parts[parts.length - 2]
    const name = folder.startsWith('.') ? folder.slice(1) : folder
    return name.charAt(0).toUpperCase() + name.slice(1)
  }
  return short
}

export function MCPScanSection({ accessToken, copy, onImported }: Props) {
  const [scanItems, setScanItems] = useState<MCPDiscoverySource[]>([])
  const [scanLoading, setScanLoading] = useState(false)
  const [importing, setImporting] = useState(false)
  const [error, setError] = useState('')
  const [open, setOpen] = useState(false)
  const [manualPath, setManualPath] = useState('')
  const [manualLoading, setManualLoading] = useState(false)

  const autoScan = useCallback(async () => {
    setScanLoading(true)
    setError('')
    try {
      const items = await listMCPDiscoverySources(accessToken, { paths: WELL_KNOWN_PATHS })
      setScanItems(items)
    } catch {
      setError(copy.toastScanFailed)
    } finally {
      setScanLoading(false)
    }
  }, [accessToken, copy.toastScanFailed])

  useEffect(() => { void autoScan() }, [autoScan])

  const totalProposalCount = useMemo(
    () => scanItems.reduce((sum, s) => sum + (s.proposed_installs?.length ?? 0), 0),
    [scanItems],
  )

  const handleManualScan = async () => {
    const trimmed = manualPath.trim()
    if (!trimmed) return
    setManualLoading(true)
    setError('')
    try {
      const items = await listMCPDiscoverySources(accessToken, { paths: [trimmed] })
      setScanItems((prev) => {
        const existing = new Set(prev.map((s) => s.source_uri))
        return [...prev, ...items.filter((s) => !existing.has(s.source_uri))]
      })
      setManualPath('')
    } catch {
      setError(copy.toastScanFailed)
    } finally {
      setManualLoading(false)
    }
  }

  const handleImport = async (source: MCPDiscoverySource, proposal: MCPDiscoveryProposal) => {
    setImporting(true)
    setError('')
    try {
      const result = await importMCPInstall(accessToken, {
        source_uri: source.source_uri,
        install_key: proposal.install_key,
      })
      onImported(result.id)
      await autoScan()
    } catch {
      setError(copy.toastImportFailed)
    } finally {
      setImporting(false)
    }
  }

  return (
    <div className="rounded-xl overflow-hidden" style={{ border: '0.5px solid var(--c-border-subtle)' }}>
      <button
        type="button"
        className="flex w-full items-center gap-2 p-3 select-none transition-colors bg-[var(--c-bg-menu)] hover:bg-[var(--c-bg-deep)]"
        onClick={() => setOpen((v) => !v)}
      >
        {open
          ? <ChevronDown size={14} className="shrink-0 text-[var(--c-text-tertiary)]" />
          : <ChevronRight size={14} className="shrink-0 text-[var(--c-text-tertiary)]" />
        }
        <FolderSearch size={14} className="shrink-0 text-[var(--c-text-tertiary)]" />
        <span className="text-sm font-medium text-[var(--c-text-heading)]">
          {copy.externalTitle}
        </span>
        {!scanLoading && totalProposalCount > 0 && (
          <span
            className="rounded px-1.5 py-px text-[10px] font-medium leading-tight text-[var(--c-text-secondary)]"
            style={{ background: 'var(--c-bg-deep)' }}
          >
            {totalProposalCount}
          </span>
        )}
      </button>

      <div
        className="grid transition-[grid-template-rows] duration-200 ease-out"
        style={{ gridTemplateRows: open ? '1fr' : '0fr' }}
      >
        <div
          className="overflow-hidden"
          style={{ borderTop: open ? '0.5px solid var(--c-border-subtle)' : 'none' }}
        >
          <div className="flex flex-col gap-2 p-3">
            {error && (
              <p className="text-xs" style={{ color: 'var(--c-status-error-text)' }}>{error}</p>
            )}

            {scanLoading && (
              <div className="flex h-20 items-center justify-center">
                <Loader2 size={14} className="animate-spin text-[var(--c-text-tertiary)]" />
              </div>
            )}

            {!scanLoading && !error && scanItems.length === 0 && (
              <p className="py-4 text-center text-sm text-[var(--c-text-muted)]">
                {copy.sourceEmpty}
              </p>
            )}

            {!scanLoading && scanItems.map((source) => {
              const proposals = source.proposed_installs ?? []
              const errors = source.validation_errors ?? []
              const warnings = source.host_warnings ?? []
              const label = sourceLabel(source.source_uri)
              const path = shortenPath(source.source_uri)

              if (proposals.length === 0 && errors.length > 0) {
                return (
                  <div
                    key={source.source_uri}
                    className="rounded-xl px-4 py-3"
                    style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
                  >
                    <div className="flex items-center gap-2">
                      <span className="text-[13px] font-medium text-[var(--c-text-heading)]">{label}</span>
                      <span className="text-xs text-[var(--c-text-muted)]">{path}</span>
                    </div>
                    <p className="mt-1.5 text-xs" style={{ color: 'var(--c-status-error-text)' }}>
                      {errors.join(' | ')}
                    </p>
                  </div>
                )
              }
              if (proposals.length === 0) return null
              return (
                <div
                  key={source.source_uri}
                  className="rounded-xl overflow-hidden"
                  style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
                >
                  <div className="flex items-center gap-2 px-4 py-2.5">
                    <span className="text-[13px] font-medium text-[var(--c-text-heading)]">{label}</span>
                    <span className="text-xs text-[var(--c-text-muted)]">{path}</span>
                  </div>

                  {errors.length > 0 && (
                    <p className="px-4 pb-2 text-xs" style={{ color: 'var(--c-status-error-text)' }}>
                      {errors.join(' | ')}
                    </p>
                  )}
                  {warnings.length > 0 && (
                    <p className="px-4 pb-2 text-xs text-[var(--c-text-secondary)]">
                      {warnings.join(' | ')}
                    </p>
                  )}

                  <div className="flex flex-col gap-0.5 px-2 pb-2">
                    {proposals.map((proposal) => (
                      <div
                        key={proposal.install_key}
                        className="flex items-center justify-between gap-3 rounded-lg px-3 py-2.5"
                        style={{ background: 'var(--c-bg-page)' }}
                      >
                        <span className="min-w-0 flex-1 truncate text-[13px] font-medium text-[var(--c-text-heading)]">
                          {proposal.display_name}
                        </span>
                        <SettingsButton
                          variant="primary"
                          type="button"
                          disabled={!source.installable || importing}
                          onClick={() => void handleImport(source, proposal)}
                          className="shrink-0 text-xs"
                        >
                          {copy.import}
                        </SettingsButton>
                      </div>
                    ))}
                  </div>
                </div>
              )
            })}

            {/* manual path input */}
            <div className="flex items-end gap-2">
              <div className="min-w-0 flex-1">
                <SettingsInput
                  value={manualPath}
                  onChange={(e) => setManualPath(e.target.value)}
                  onKeyDown={(e) => { if (e.key === 'Enter') void handleManualScan() }}
                  placeholder={copy.placeholderFilePath}
                />
              </div>
              <SettingsButton
                variant="primary"
                type="button"
                disabled={manualLoading || !manualPath.trim()}
                onClick={() => void handleManualScan()}
                className="shrink-0 text-xs"
                icon={manualLoading ? <Loader2 size={12} className="animate-spin" /> : <Plus size={12} />}
              >
                {copy.scan}
              </SettingsButton>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
