import { useState, useEffect, useCallback, useRef } from 'react'
import { Brain, Trash2, RefreshCw, Search, Plus, Pencil } from 'lucide-react'
import { ConfirmDialog } from '@arkloop/shared'
import { SpinnerIcon } from '@arkloop/shared/components/auth-ui'
import { useLocale } from '../../contexts/LocaleContext'
import { getDesktopApi } from '@arkloop/shared/desktop'
import type { MemoryEntry } from '@arkloop/shared/desktop'
import { secondaryButtonSmCls, secondaryButtonBorderStyle } from '../buttonStyles'
import { SettingsSectionHeader } from './_SettingsSectionHeader'

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleString(undefined, { dateStyle: 'short', timeStyle: 'short' })
  } catch {
    return iso
  }
}

function categoryColor(category: string): string {
  const map: Record<string, string> = {
    profile: 'bg-blue-500/15 text-blue-400',
    preferences: 'bg-purple-500/15 text-purple-400',
    entities: 'bg-amber-500/15 text-amber-400',
    events: 'bg-green-500/15 text-green-400',
    cases: 'bg-red-500/15 text-red-400',
    patterns: 'bg-teal-500/15 text-teal-400',
    general: 'bg-[var(--c-bg-deep)] text-[var(--c-text-muted)]',
  }
  return map[category] ?? 'bg-[var(--c-bg-deep)] text-[var(--c-text-muted)]'
}

function EntryCard({
  entry,
  onDelete,
  onEdit,
}: {
  entry: MemoryEntry
  onDelete: (id: string) => void
  onEdit: (id: string, newContent: string, category: string) => Promise<void>
}) {
  const content = entry.content.replace(/^\[.*?\]\s*/, '').trim() || entry.content
  const [editing, setEditing] = useState(false)
  const [editValue, setEditValue] = useState(content)
  const [saving, setSaving] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  const startEdit = () => {
    setEditValue(content)
    setEditing(true)
    setTimeout(() => textareaRef.current?.focus(), 0)
  }

  const cancelEdit = () => {
    setEditing(false)
    setEditValue(content)
  }

  const saveEdit = async () => {
    const trimmed = editValue.trim()
    if (!trimmed || trimmed === content) { cancelEdit(); return }
    setSaving(true)
    try {
      await onEdit(entry.id, trimmed, entry.category)
      setEditing(false)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div
      className="group relative rounded-xl"
      style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
    >
      <div className="flex items-start gap-3 px-4 py-3">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium ${categoryColor(entry.category)}`}>
              {entry.category}
            </span>
            {entry.scope === 'agent' && (
              <span className="inline-flex items-center rounded-md bg-indigo-500/15 px-2 py-0.5 text-xs font-medium text-indigo-400">agent</span>
            )}
            {entry.key && <span className="text-[10px] text-[var(--c-text-muted)]">{entry.key}</span>}
          </div>
          {editing ? (
            <div className="flex flex-col gap-2">
              <textarea
                ref={textareaRef}
                value={editValue}
                onChange={(e) => setEditValue(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); void saveEdit() }
                  if (e.key === 'Escape') { cancelEdit() }
                }}
                rows={3}
                className="w-full resize-none rounded-lg px-2 py-1.5 text-sm text-[var(--c-text-primary)] outline-none"
                style={{ border: '1px solid var(--c-border)', background: 'var(--c-bg-input)' }}
              />
              <div className="flex items-center gap-2">
                <button
                  onClick={() => void saveEdit()}
                  disabled={saving || !editValue.trim()}
                  className="inline-flex items-center justify-center gap-1.5 rounded-lg bg-[var(--c-btn-bg)] px-4 py-1.5 text-sm font-medium text-[var(--c-btn-text)] transition-[filter] hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  {saving && <SpinnerIcon />}
                  Save
                </button>
                <button
                  onClick={cancelEdit}
                  className={secondaryButtonSmCls}
                  style={secondaryButtonBorderStyle}
                >
                  Cancel
                </button>
              </div>
            </div>
          ) : (
            <p className="text-sm text-[var(--c-text-primary)]">{content}</p>
          )}
          <p className="text-[10px] text-[var(--c-text-muted)]">{formatDate(entry.created_at)}</p>
        </div>
        {!editing && (
          <div className="mt-0.5 flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity duration-100 group-hover:opacity-100">
            <button
              onClick={startEdit}
              className="rounded-lg p-1.5 text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)]"
            >
              <Pencil size={13} />
            </button>
            <button
              onClick={() => onDelete(entry.id)}
              className="rounded-lg p-1.5 text-[var(--c-text-muted)] transition-colors hover:text-red-400"
            >
              <Trash2 size={13} />
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

export function NotebookSettings() {
  const { t } = useLocale()
  const ds = t.desktopSettings

  const [entries, setEntries] = useState<MemoryEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [addContent, setAddContent] = useState('')
  const [adding, setAdding] = useState(false)
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null)
  const [confirmClearAll, setConfirmClearAll] = useState(false)

  const api = getDesktopApi()

  const loadEntries = useCallback(async (quiet = false) => {
    if (!api?.memory) { setLoading(false); return }
    if (!quiet) setLoading(true); else setRefreshing(true)
    try {
      const resp = await api.memory.list()
      setEntries(resp.entries ?? [])
    } catch { /* ignore */ } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }, [api])

  useEffect(() => {
    void loadEntries()
  }, [loadEntries])

  const handleAdd = useCallback(async () => {
    const content = addContent.trim()
    if (!content || !api?.memory) return
    setAdding(true)
    try {
      await api.memory.add(content)
      setAddContent('')
      await loadEntries(true)
    } catch { /* ignore */ } finally {
      setAdding(false)
    }
  }, [addContent, api, loadEntries])

  const handleDelete = useCallback(async (id: string) => {
    if (!api?.memory) return
    try { await api.memory.delete(id); setEntries((p) => p.filter((e) => e.id !== id)) } catch { /* ignore */ }
    setConfirmDeleteId(null)
  }, [api])

  const handleEdit = useCallback(async (id: string, newContent: string, category: string) => {
    if (!api?.memory) return
    await api.memory.delete(id)
    await api.memory.add(newContent, category)
    await loadEntries(true)
  }, [api, loadEntries])

  const handleClearAll = useCallback(async () => {
    if (!api?.memory) return
    for (const e of entries) { try { await api.memory.delete(e.id) } catch { /* ignore */ } }
    setEntries([])
    setConfirmClearAll(false)
  }, [api, entries])

  const filteredEntries = searchQuery.trim()
    ? entries.filter((e) => {
        const q = searchQuery.toLowerCase()
        return (
          e.content.toLowerCase().includes(q) ||
          e.category.toLowerCase().includes(q) ||
          e.key.toLowerCase().includes(q)
        )
      })
    : entries

  if (loading) {
    return (
      <div className="flex flex-col gap-4">
        <SettingsSectionHeader title={ds.notebookSettingsTitle} description={ds.notebookSettingsDesc} />
        <div className="flex items-center justify-center py-20"><SpinnerIcon /></div>
      </div>
    )
  }

  if (!api?.memory) {
    return (
      <div className="flex flex-col gap-4">
        <SettingsSectionHeader title={ds.notebookSettingsTitle} description={ds.notebookSettingsDesc} />
        <div
          className="rounded-xl py-16 text-center text-sm text-[var(--c-text-muted)]"
          style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
        >
          Not available outside Desktop mode.
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-6">
      {/* header */}
      <div className="flex items-start justify-between gap-2">
        <SettingsSectionHeader title={ds.notebookSettingsTitle} description={ds.notebookSettingsDesc} />
        <button
          onClick={() => void loadEntries(true)}
          disabled={refreshing}
          className="shrink-0 rounded-lg p-1.5 text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)] disabled:opacity-40"
        >
          <RefreshCw size={14} className={refreshing ? 'animate-spin' : ''} />
        </button>
      </div>

      {/* search */}
      <div
        className="flex items-center gap-2 rounded-xl px-3 py-2"
        style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-input)' }}
      >
        <Search size={14} className="shrink-0 text-[var(--c-text-muted)]" />
        <input
          type="text"
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          placeholder={ds.notebookSearchPlaceholder}
          className="min-w-0 flex-1 bg-transparent text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] outline-none"
        />
      </div>

      {/* add */}
      <div className="flex flex-col gap-2">
        <textarea
          value={addContent}
          onChange={(e) => setAddContent(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault()
              void handleAdd()
            }
          }}
          placeholder={ds.notebookAddPlaceholder}
          rows={3}
          className="w-full resize-none rounded-xl px-3 py-2 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] outline-none"
          style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-input)' }}
        />
        <div className="flex justify-end">
          <button
            onClick={() => void handleAdd()}
            disabled={adding || !addContent.trim()}
            className="flex items-center gap-1.5 rounded-lg bg-[var(--c-btn-bg)] px-4 py-2 text-sm font-medium text-[var(--c-btn-text)] transition-opacity hover:opacity-90 disabled:opacity-40"
          >
            {adding ? <SpinnerIcon /> : <Plus size={14} />}
            {ds.notebookAddButton}
          </button>
        </div>
      </div>

      <div className="border-t border-[var(--c-border-subtle)]" />

      {/* entries header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Brain size={15} className="text-[var(--c-text-secondary)]" />
          <h4 className="text-sm font-semibold text-[var(--c-text-heading)]">{ds.memoryEntriesTitle}</h4>
          {entries.length > 0 && (
            <span
              className="inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium"
              style={{ background: 'var(--c-bg-deep)', color: 'var(--c-text-muted)' }}
            >
              {filteredEntries.length}{searchQuery.trim() && entries.length !== filteredEntries.length ? `/${entries.length}` : ''}
            </span>
          )}
        </div>
        {entries.length > 0 && (
          <button
            onClick={() => setConfirmClearAll(true)}
            className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs text-red-400 transition-colors hover:bg-red-500/10"
          >
            <Trash2 size={12} />{ds.memoryClearAll}
          </button>
        )}
      </div>

      {/* list */}
      {filteredEntries.length === 0 ? (
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
          {filteredEntries.map((e) => (
            <EntryCard key={e.id} entry={e} onDelete={(id) => setConfirmDeleteId(id)} onEdit={handleEdit} />
          ))}
        </div>
      )}

      <ConfirmDialog
        open={confirmDeleteId !== null}
        onClose={() => setConfirmDeleteId(null)}
        onConfirm={() => void handleDelete(confirmDeleteId!)}
        message={ds.memoryDeleteConfirm}
        confirmLabel="Delete"
      />
      <ConfirmDialog
        open={confirmClearAll}
        onClose={() => setConfirmClearAll(false)}
        onConfirm={() => void handleClearAll()}
        message={ds.memoryClearAllConfirm}
        confirmLabel="Delete"
      />
    </div>
  )
}
