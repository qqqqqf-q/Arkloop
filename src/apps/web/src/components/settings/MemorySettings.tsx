import { useState, useEffect, useCallback } from 'react'
import {
  Brain,
  Trash2,
  Loader2,
  Database,
  Server,
  AlertTriangle,
  FileText,
  RefreshCw,
} from 'lucide-react'
import { useLocale } from '../../contexts/LocaleContext'
import { getDesktopApi } from '@arkloop/shared/desktop'
import type { MemoryEntry, MemoryConfig } from '@arkloop/shared/desktop'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleString(undefined, {
      dateStyle: 'short',
      timeStyle: 'short',
    })
  } catch {
    return iso
  }
}

function categoryColor(category: string): string {
  const map: Record<string, string> = {
    profile:     'bg-blue-500/15 text-blue-400',
    preferences: 'bg-purple-500/15 text-purple-400',
    entities:    'bg-amber-500/15 text-amber-400',
    events:      'bg-green-500/15 text-green-400',
    cases:       'bg-red-500/15 text-red-400',
    patterns:    'bg-teal-500/15 text-teal-400',
    general:     'bg-[var(--c-bg-deep)] text-[var(--c-text-muted)]',
  }
  return map[category] ?? 'bg-[var(--c-bg-deep)] text-[var(--c-text-muted)]'
}

// ---------------------------------------------------------------------------
// Confirmation modal
// ---------------------------------------------------------------------------

function ConfirmModal({
  message,
  onConfirm,
  onCancel,
}: {
  message: string
  onConfirm: () => void
  onCancel: () => void
}) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div
        className="mx-4 max-w-sm rounded-xl p-5 shadow-xl"
        style={{ background: 'var(--c-bg-menu)', border: '1px solid var(--c-border-subtle)' }}
      >
        <div className="mb-3 flex items-start gap-3">
          <AlertTriangle size={16} className="mt-0.5 shrink-0 text-amber-400" />
          <p className="text-sm text-[var(--c-text-primary)]">{message}</p>
        </div>
        <div className="flex justify-end gap-2">
          <button
            onClick={onCancel}
            className="rounded-lg px-3 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
          >
            Cancel
          </button>
          <button
            onClick={onConfirm}
            className="rounded-lg bg-red-500/15 px-3 py-1.5 text-sm font-medium text-red-400 transition-colors hover:bg-red-500/25"
          >
            Delete
          </button>
        </div>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Entry card
// ---------------------------------------------------------------------------

function EntryCard({
  entry,
  onDelete,
}: {
  entry: MemoryEntry
  onDelete: (id: string) => void
}) {
  const [hovered, setHovered] = useState(false)

  // Parse the label from content "[scope/category/key] text" or display raw
  const content = entry.content.replace(/^\[.*?\]\s*/, '').trim() || entry.content

  return (
    <div
      className="group relative rounded-xl transition-[border-color] duration-150"
      style={{
        border: '1px solid var(--c-border-subtle)',
        background: hovered ? 'var(--c-bg-deep)' : 'var(--c-bg-menu)',
      }}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      <div className="flex items-start gap-3 px-4 py-3">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <span
              className={`inline-flex items-center rounded-full px-1.5 py-0.5 text-[10px] font-medium ${categoryColor(entry.category)}`}
            >
              {entry.category}
            </span>
            {entry.scope === 'agent' && (
              <span className="inline-flex items-center rounded-full bg-indigo-500/15 px-1.5 py-0.5 text-[10px] font-medium text-indigo-400">
                agent
              </span>
            )}
            {entry.key && (
              <span className="text-[10px] text-[var(--c-text-muted)]">{entry.key}</span>
            )}
          </div>
          <p className="text-sm text-[var(--c-text-primary)]">{content}</p>
          <p className="text-[10px] text-[var(--c-text-muted)]">{formatDate(entry.created_at)}</p>
        </div>

        <button
          onClick={() => onDelete(entry.id)}
          className="mt-0.5 shrink-0 rounded-lg p-1.5 text-[var(--c-text-muted)] opacity-0 transition-[opacity,color] duration-100 group-hover:opacity-100 hover:text-red-400"
          title="Delete memory"
        >
          <Trash2 size={13} />
        </button>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// OpenViking snapshot view
// ---------------------------------------------------------------------------

function SnapshotView({ snapshot }: { snapshot: string }) {
  if (!snapshot) {
    return (
      <div
        className="flex flex-col items-center justify-center rounded-xl py-14"
        style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
      >
        <Brain size={28} className="mb-3 text-[var(--c-text-muted)]" />
        <p className="text-sm text-[var(--c-text-muted)]">No memory snapshot available yet.</p>
      </div>
    )
  }
  return (
    <pre
      className="overflow-auto rounded-xl p-4 text-xs leading-relaxed text-[var(--c-text-secondary)] whitespace-pre-wrap"
      style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)', maxHeight: 360 }}
    >
      {snapshot}
    </pre>
  )
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export function MemorySettings() {
  const { t } = useLocale()
  const ds = t.desktopSettings

  const [memConfig, setMemConfig] = useState<MemoryConfig | null>(null)
  const [entries, setEntries] = useState<MemoryEntry[]>([])
  const [snapshot, setSnapshot] = useState<string>('')
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null)
  const [confirmClearAll, setConfirmClearAll] = useState(false)

  const api = getDesktopApi()

  const loadData = useCallback(async (quiet = false) => {
    if (!api?.memory) { setLoading(false); return }
    if (!quiet) setLoading(true)
    else setRefreshing(true)
    try {
      const [cfg, listResp] = await Promise.all([
        api.memory.getConfig(),
        api.memory.list(),
      ])
      setMemConfig(cfg)
      setEntries(listResp.entries ?? [])
      if (cfg.provider === 'openviking') {
        const snap = await api.memory.getSnapshot()
        setSnapshot(snap.memory_block ?? '')
      }
    } catch {
      // ignore
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }, [api])

  useEffect(() => {
    void loadData()
  }, [loadData])

  const handleDelete = useCallback(async (id: string) => {
    if (!api?.memory) return
    try {
      await api.memory.delete(id)
      setEntries((prev) => prev.filter((e) => e.id !== id))
    } catch {
      // ignore
    }
    setConfirmDeleteId(null)
  }, [api])

  const handleClearAll = useCallback(async () => {
    if (!api?.memory) return
    const ids = entries.map((e) => e.id)
    for (const id of ids) {
      try { await api.memory.delete(id) } catch { /* ignore */ }
    }
    setEntries([])
    setConfirmClearAll(false)
  }, [api, entries])

  const isLocal = memConfig?.provider !== 'openviking'

  if (loading) {
    return (
      <div className="flex flex-col gap-4">
        <PageHeader ds={ds} />
        <div className="flex items-center justify-center py-20">
          <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
        </div>
      </div>
    )
  }

  if (!api?.memory) {
    return (
      <div className="flex flex-col gap-4">
        <PageHeader ds={ds} />
        <div
          className="flex flex-col items-center justify-center rounded-xl bg-[var(--c-bg-menu)] py-16"
          style={{ border: '0.5px solid var(--c-border-subtle)' }}
        >
          <p className="text-sm text-[var(--c-text-muted)]">Not available outside Desktop mode.</p>
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader ds={ds} />

      {/* Provider status bar */}
      <div
        className="flex items-center gap-3 rounded-xl px-4 py-3"
        style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
      >
        {isLocal
          ? <Database size={15} className="shrink-0 text-[var(--c-accent)]" />
          : <Server size={15} className="shrink-0 text-purple-400" />
        }
        <div className="flex-1">
          <p className="text-sm font-medium text-[var(--c-text-heading)]">
            {isLocal ? ds.memoryLocalProvider : ds.memoryOpenvikingProvider}
          </p>
          <p className="text-xs text-[var(--c-text-muted)]">
            {isLocal ? ds.memoryLocalProviderDesc : ds.memoryOpenvikingProviderDesc}
          </p>
        </div>
        <button
          onClick={() => void loadData(true)}
          disabled={refreshing}
          className="shrink-0 rounded-lg p-1.5 text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)] disabled:opacity-40"
          title="Refresh"
        >
          <RefreshCw size={14} className={refreshing ? 'animate-spin' : ''} />
        </button>
      </div>

      {/* ── Local mode: entry list ── */}
      {isLocal && (
        <div className="flex flex-col gap-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Brain size={15} className="text-[var(--c-text-secondary)]" />
              <h4 className="text-sm font-semibold text-[var(--c-text-heading)]">{ds.memoryEntriesTitle}</h4>
              {entries.length > 0 && (
                <span
                  className="inline-flex items-center rounded-full px-1.5 py-0.5 text-[10px] font-medium"
                  style={{ background: 'var(--c-bg-deep)', color: 'var(--c-text-muted)' }}
                >
                  {entries.length}
                </span>
              )}
            </div>
            {entries.length > 0 && (
              <button
                onClick={() => setConfirmClearAll(true)}
                className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs text-red-400 transition-colors hover:bg-red-500/10"
              >
                <Trash2 size={12} />
                {ds.memoryClearAll}
              </button>
            )}
          </div>

          {entries.length === 0 ? (
            <div
              className="flex flex-col items-center justify-center rounded-xl py-14"
              style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
            >
              <Brain size={28} className="mb-3 text-[var(--c-text-muted)]" />
              <p className="text-sm font-medium text-[var(--c-text-heading)]">{ds.memoryEmptyTitle}</p>
              <p className="mt-1 text-xs text-[var(--c-text-muted)]">{ds.memoryEmptyDesc}</p>
            </div>
          ) : (
            <div className="flex flex-col gap-2">
              {entries.map((entry) => (
                <EntryCard
                  key={entry.id}
                  entry={entry}
                  onDelete={(id) => setConfirmDeleteId(id)}
                />
              ))}
            </div>
          )}
        </div>
      )}

      {/* ── OpenViking mode: snapshot view ── */}
      {!isLocal && (
        <div className="flex flex-col gap-3">
          <div className="flex items-center gap-2">
            <FileText size={15} className="text-[var(--c-text-secondary)]" />
            <h4 className="text-sm font-semibold text-[var(--c-text-heading)]">{ds.memorySnapshotTitle}</h4>
          </div>
          <div
            className="flex items-start gap-2 rounded-xl px-4 py-3"
            style={{ border: '1px solid var(--c-border-subtle)', background: 'color-mix(in srgb, var(--c-accent) 8%, transparent)' }}
          >
            <AlertTriangle size={13} className="mt-0.5 shrink-0 text-amber-400" />
            <p className="text-xs text-[var(--c-text-secondary)]">{ds.memoryOpenvikingNote}</p>
          </div>
          <SnapshotView snapshot={snapshot} />
        </div>
      )}

      {/* Confirm delete modal */}
      {confirmDeleteId !== null && (
        <ConfirmModal
          message={ds.memoryDeleteConfirm}
          onConfirm={() => void handleDelete(confirmDeleteId)}
          onCancel={() => setConfirmDeleteId(null)}
        />
      )}

      {/* Confirm clear all modal */}
      {confirmClearAll && (
        <ConfirmModal
          message={ds.memoryClearAllConfirm}
          onConfirm={() => void handleClearAll()}
          onCancel={() => setConfirmClearAll(false)}
        />
      )}
    </div>
  )
}

type PageHeaderDs = {
  memorySettingsTitle: string
  memorySettingsDesc: string
}

function PageHeader({ ds }: { ds: PageHeaderDs }) {
  return (
    <div>
      <h3 className="text-base font-semibold text-[var(--c-text-heading)]">{ds.memorySettingsTitle}</h3>
      <p className="mt-1 text-sm text-[var(--c-text-secondary)]">{ds.memorySettingsDesc}</p>
    </div>
  )
}
