import { useCallback, useEffect, useMemo, useState } from 'react'
import { ChevronDown, ChevronRight, FolderSearch, Loader2, Plus, Trash2 } from 'lucide-react'
import { listMCPDiscoverySources, importMCPInstall, type MCPDiscoverySource, type MCPDiscoveryProposal } from '../../api'
import { SettingsGroup, SETTINGS_CARD_SURFACE_CLASS } from '../settings/_SettingsLayout'
import { SettingsButton, SettingsIconButton } from '../settings/_SettingsButton'
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

const CUSTOM_PATHS_KEY = 'arkloop:web:mcp-external-paths'

function loadCustomPaths(): string[] {
  try {
    const raw = localStorage.getItem(CUSTOM_PATHS_KEY)
    if (!raw) return []
    return JSON.parse(raw) as string[]
  } catch {
    return []
  }
}

function saveCustomPaths(paths: string[]): void {
  localStorage.setItem(CUSTOM_PATHS_KEY, JSON.stringify(paths))
}

function shortenPath(uri: string): string {
  return uri.replace(/^\/Users\/[^/]+\//, '~/').replace(/^\/home\/[^/]+\//, '~/')
}

export function MCPScanSection({ accessToken, copy, onImported }: Props) {
  const [customPaths, setCustomPaths] = useState<string[]>(loadCustomPaths)
  const [sources, setSources] = useState<MCPDiscoverySource[]>([])
  const [scanning, setScanning] = useState(false)
  const [importing, setImporting] = useState<string | null>(null)
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})
  const [newPath, setNewPath] = useState('')
  const [error, setError] = useState('')

  const scan = useCallback(async (extraPaths: string[]) => {
    const allPaths = [...WELL_KNOWN_PATHS, ...extraPaths.filter((p) => !WELL_KNOWN_PATHS.includes(p))]
    setScanning(true)
    setError('')
    try {
      const items = await listMCPDiscoverySources(accessToken, { paths: allPaths })
      setSources(items.filter((s) => (s.proposed_installs?.length ?? 0) > 0 || (s.validation_errors?.length ?? 0) > 0))
    } catch {
      setError(copy.toastScanFailed)
    } finally {
      setScanning(false)
    }
  }, [accessToken, copy.toastScanFailed])

  useEffect(() => { void scan(customPaths) }, [scan, customPaths])

  const totalProposalCount = useMemo(
    () => sources.reduce((sum, s) => sum + s.proposed_installs.length, 0),
    [sources],
  )

  const handleAddPath = () => {
    const trimmed = newPath.trim()
    if (!trimmed) return
    const next = customPaths.includes(trimmed) ? customPaths : [...customPaths, trimmed]
    saveCustomPaths(next)
    setCustomPaths(next)
    setNewPath('')
  }

  const handleRemovePath = (path: string) => {
    const next = customPaths.filter((p) => p !== path)
    saveCustomPaths(next)
    setCustomPaths(next)
  }

  const handleImport = async (source: MCPDiscoverySource, proposal: MCPDiscoveryProposal) => {
    const key = `${source.source_uri}::${proposal.install_key}`
    setImporting(key)
    setError('')
    try {
      const result = await importMCPInstall(accessToken, {
        source_uri: source.source_uri,
        install_key: proposal.install_key,
      })
      onImported(result.id)
      await scan(customPaths)
    } catch {
      setError(copy.toastImportFailed)
    } finally {
      setImporting(null)
    }
  }

  const toggleExpand = (uri: string) => {
    setExpanded((prev) => ({ ...prev, [uri]: !prev[uri] }))
  }

  const isCustom = (sourceUri: string) => {
    const short = shortenPath(sourceUri)
    return customPaths.some((p) => p === sourceUri || p === short)
  }

  return (
    <SettingsGroup title={copy.externalTitle}>
      <div className={SETTINGS_CARD_SURFACE_CLASS}>
        <div className="flex flex-col gap-4 p-4 sm:p-5">
          {!scanning && sources.length > 0 && (
            <p className="text-[13px] leading-snug text-[var(--c-text-tertiary)]">
              {copy.externalScanSummary(sources.length, totalProposalCount)}
            </p>
          )}

          {error && (
            <p className="text-xs" style={{ color: 'var(--c-status-error-text)' }}>{error}</p>
          )}

          {scanning ? (
            <div className="flex h-24 items-center justify-center">
              <Loader2 size={16} className="animate-spin text-[var(--c-text-tertiary)]" />
            </div>
          ) : !error && sources.length === 0 ? (
            <p className="py-1 text-center text-[13px] text-[var(--c-text-muted)]">{copy.externalEmpty}</p>
          ) : sources.length > 0 ? (
            <div className="flex flex-col gap-2">
              {sources.map((source) => {
                const proposals = source.proposed_installs ?? []
                const errors = source.validation_errors ?? []
                const warnings = source.host_warnings ?? []
                const displayPath = shortenPath(source.source_uri)
                const open = expanded[source.source_uri] === true
                const custom = isCustom(source.source_uri)

                return (
                  <div
                    key={source.source_uri}
                    className="overflow-hidden rounded-[10px] border-[0.5px] border-[var(--c-border-subtle)] bg-[var(--c-bg-input)]"
                  >
                    <div
                      role="button"
                      tabIndex={0}
                      className="flex cursor-pointer items-center gap-2 px-3 py-2.5 select-none outline-none transition-colors hover:bg-[color-mix(in_srgb,var(--c-bg-deep)_30%,transparent)] focus-visible:ring-2 focus-visible:ring-[var(--c-accent)]"
                      onClick={() => toggleExpand(source.source_uri)}
                      onKeyDown={(e) => {
                        if (e.key !== 'Enter' && e.key !== ' ') return
                        e.preventDefault()
                        toggleExpand(source.source_uri)
                      }}
                    >
                      {open
                        ? <ChevronDown size={14} className="shrink-0 text-[var(--c-text-tertiary)]" />
                        : <ChevronRight size={14} className="shrink-0 text-[var(--c-text-tertiary)]" />
                      }
                      <FolderSearch size={14} className="shrink-0 text-[var(--c-text-tertiary)]" />
                      <span
                        className="min-w-0 flex-1 truncate font-mono text-[12px] text-[var(--c-text-heading)] sm:text-[13px]"
                        title={source.source_uri}
                      >
                        {displayPath}
                      </span>
                      <span className="shrink-0 rounded-[6px] border-[0.5px] border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] px-2 py-0.5 text-[10px] font-medium tabular-nums text-[var(--c-text-secondary)]">
                        {proposals.length}
                      </span>
                      {custom && (
                        <SettingsIconButton
                          label={copy.externalRemoveDir}
                          danger
                          variant="framed"
                          className="shrink-0"
                          onClick={(e) => {
                            e.stopPropagation()
                            handleRemovePath(displayPath)
                          }}
                        >
                          <Trash2 size={14} />
                        </SettingsIconButton>
                      )}
                    </div>

                    <div
                      className="grid transition-[grid-template-rows] duration-200 ease-out"
                      style={{ gridTemplateRows: open ? '1fr' : '0fr' }}
                    >
                      <div className="overflow-hidden">
                        <div className="border-t border-[var(--c-border-subtle)] px-3 py-2">
                          {errors.length > 0 && (
                            <p className="mb-2 text-xs" style={{ color: 'var(--c-status-error-text)' }}>
                              {errors.join(' | ')}
                            </p>
                          )}
                          {warnings.length > 0 && (
                            <p className="mb-2 text-xs text-[var(--c-text-secondary)]">
                              {warnings.join(' | ')}
                            </p>
                          )}
                          {proposals.length > 0 ? (
                            <ul className="flex flex-col gap-1">
                              {proposals.map((proposal) => {
                                const key = `${source.source_uri}::${proposal.install_key}`
                                const busy = importing === key
                                return (
                                  <li
                                    key={proposal.install_key}
                                    className="flex min-h-[36px] items-center gap-2 rounded-[6.5px] px-2.5 py-1.5"
                                    style={{ background: 'var(--c-bg-menu)' }}
                                  >
                                    <span className="min-w-0 flex-1 truncate text-[13px] text-[var(--c-text-primary)]">
                                      {proposal.display_name}
                                    </span>
                                    <SettingsButton
                                      variant="primary"
                                      size="default"
                                      disabled={!source.installable || busy || importing !== null}
                                      onClick={() => void handleImport(source, proposal)}
                                      className="shrink-0"
                                      icon={busy ? <Loader2 size={12} className="animate-spin" /> : undefined}
                                    >
                                      {copy.import}
                                    </SettingsButton>
                                  </li>
                                )
                              })}
                            </ul>
                          ) : (
                            <p className="pl-1 text-[12px] text-[var(--c-text-muted)]">{copy.sourceEmpty}</p>
                          )}
                        </div>
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          ) : null}

          <div className="flex flex-col gap-2 border-t border-[var(--c-border-subtle)] pt-4 sm:flex-row sm:items-center">
            <SettingsInput
              variant="md"
              className="min-w-0 flex-1"
              value={newPath}
              onChange={(e) => setNewPath(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter') handleAddPath() }}
              placeholder={copy.placeholderFilePath}
              disabled={scanning}
            />
            <SettingsButton
              variant="primary"
              size="modal"
              className="w-full shrink-0 sm:w-auto"
              disabled={scanning || !newPath.trim()}
              icon={<Plus size={14} />}
              onClick={handleAddPath}
            >
              {copy.scan}
            </SettingsButton>
          </div>
        </div>
      </div>
    </SettingsGroup>
  )
}
