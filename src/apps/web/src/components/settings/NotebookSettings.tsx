import { useState, useEffect, useCallback, useRef } from 'react'
import { BookOpen, Brain, Trash2, RefreshCw, Search, Plus, Pencil } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { ConfirmDialog, Modal, formatDateTime } from '@arkloop/shared'
import { SpinnerIcon } from '@arkloop/shared/components/auth-ui'
import { useLocale } from '../../contexts/LocaleContext'
import type { MemoryEntry } from '@arkloop/shared/desktop'
import { secondaryButtonSmCls, secondaryButtonBorderStyle } from '../buttonStyles'
import { SettingsSectionHeader } from './_SettingsSectionHeader'
import { getDesktopMemoryApi } from '../../desktopMemoryApi'

function formatDate(iso: string): string {
  return formatDateTime(iso, { includeZone: false })
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

// ---------------------------------------------------------------------------
// EntryCard — hover reveals metadata + actions in a unified right column
// ---------------------------------------------------------------------------

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
            <div className="notebook-entry-md prose-sm max-w-none text-sm text-[var(--c-text-primary)]">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>
            </div>
          )}
        </div>

        {/* hover-only: metadata + actions */}
        {!editing && (
          <div className="mt-0.5 flex shrink-0 flex-col items-end gap-1 opacity-0 transition-opacity duration-100 group-hover:opacity-100">
            <div className="flex items-center gap-0.5">
              <span className={`inline-flex items-center rounded-md px-1.5 py-px text-[10px] font-medium leading-tight ${categoryColor(entry.category)}`}>
                {entry.category}
              </span>
              {entry.scope === 'agent' && (
                <span className="inline-flex items-center rounded-md bg-indigo-500/15 px-1.5 py-px text-[10px] font-medium leading-tight text-indigo-400">agent</span>
              )}
            </div>
            <div className="flex items-center gap-0.5">
              <span className="text-[10px] text-[var(--c-text-muted)]">{formatDate(entry.created_at)}</span>
              <button
                onClick={startEdit}
                className="rounded-lg p-1 text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)]"
              >
                <Pencil size={12} />
              </button>
              <button
                onClick={() => onDelete(entry.id)}
                className="rounded-lg p-1 text-[var(--c-text-muted)] transition-colors hover:text-red-400"
              >
                <Trash2 size={12} />
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// NotebookCard — skeuomorphic preview card (mirrors MemoriesCard pattern)
// ---------------------------------------------------------------------------

function NotebookCard({
  entries,
  onClick,
  titles,
}: {
  entries: MemoryEntry[]
  onClick: () => void
  titles: { countLabel: string; viewEdit: string; emptyTitle: string; emptyDesc: string }
}) {
  const [hovered, setHovered] = useState(false)
  const [miniHovered, setMiniHovered] = useState(false)

  const hasContent = entries.length > 0
  const previewText = (() => {
    let text = ''
    for (const e of entries) {
      const clean = e.content.replace(/^\[.*?\]\s*/, '').trim() || e.content
      text += (text ? '\n' : '') + clean
      if (text.length >= 400) break
    }
    return text.slice(0, 400)
  })()

  if (!hasContent) {
    return (
      <div
        className="flex flex-col items-center justify-center rounded-xl py-10"
        style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
      >
        <BookOpen size={24} className="mb-2 text-[var(--c-text-muted)]" />
        <p className="text-sm font-medium text-[var(--c-text-heading)]">{titles.emptyTitle}</p>
        <p className="mt-1 text-xs text-[var(--c-text-muted)]">{titles.emptyDesc}</p>
      </div>
    )
  }

  return (
    <div
      className="group/card cursor-pointer rounded-xl"
      style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
      onClick={onClick}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => { setHovered(false); setMiniHovered(false) }}
    >
      <div className="flex gap-4 p-4">
        {/* mini preview */}
        <div
          className="shrink-0 overflow-hidden rounded-lg transition-shadow duration-200"
          style={{
            width: 120,
            height: 80,
            border: '0.5px solid var(--c-border-subtle)',
            background: 'var(--c-bg-page)',
            boxShadow: hovered
              ? '0 3px 6px -2px rgba(0,0,0,0.08), 1px 0 3px -2px rgba(0,0,0,0.03), -1px 0 3px -2px rgba(0,0,0,0.03)'
              : '0 1px 3px -1px rgba(0,0,0,0.04)',
          }}
          onMouseEnter={() => setMiniHovered(true)}
          onMouseLeave={() => setMiniHovered(false)}
        >
          <div
            className="overflow-hidden transition-all duration-200"
            style={{
              padding: '10px 0 0 12px',
              fontSize: 8,
              lineHeight: '11px',
              letterSpacing: '-0.01em',
              color: hovered ? 'var(--c-text-secondary)' : 'var(--c-text-tertiary)',
              maxHeight: 80,
              transformOrigin: 'top left',
              transform: miniHovered ? 'scale(1.12)' : 'scale(1)',
              WebkitMaskImage: 'linear-gradient(to bottom, black 40%, transparent 90%), linear-gradient(to left, transparent 0px, black 8px)',
              maskImage: 'linear-gradient(to bottom, black 40%, transparent 90%), linear-gradient(to left, transparent 0px, black 8px)',
              WebkitMaskComposite: 'source-in',
              maskComposite: 'intersect',
            }}
          >
            {previewText}
          </div>
        </div>

        {/* text area */}
        <div className="flex min-w-0 flex-1 flex-col justify-center overflow-hidden">
          <p className="text-sm text-[var(--c-text-heading)]" style={{ fontWeight: 450 }}>
            Notebook
          </p>
          <div className="relative h-[18px] overflow-hidden">
            <p
              className="absolute inset-0 text-[11px] text-[var(--c-text-muted)] transition-all duration-150 ease-out"
              style={{
                transform: hovered ? 'translateX(-16px)' : 'translateX(0)',
                opacity: hovered ? 0 : 1,
              }}
            >
              {titles.countLabel}
            </p>
            <p
              className="absolute inset-0 text-[11px] transition-all duration-150 ease-out"
              style={{
                color: 'var(--c-text-muted)',
                transform: hovered ? 'translateX(0)' : 'translateX(16px)',
                opacity: hovered ? 1 : 0,
              }}
            >
              {titles.viewEdit}
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

type Props = { accessToken?: string }

export function NotebookSettings({ accessToken }: Props) {
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
  const [modalOpen, setModalOpen] = useState(false)

  const memoryApi = getDesktopMemoryApi(accessToken)

  const loadEntries = useCallback(async (quiet = false) => {
    if (!memoryApi) { setLoading(false); return }
    if (!quiet) setLoading(true); else setRefreshing(true)
    try {
      const resp = await memoryApi.list()
      setEntries(resp.entries ?? [])
    } catch { /* ignore */ } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }, [memoryApi])

  useEffect(() => {
    void loadEntries()
  }, [loadEntries])

  const handleAdd = useCallback(async () => {
    const content = addContent.trim()
    if (!content || !memoryApi) return
    setAdding(true)
    try {
      await memoryApi.add(content)
      setAddContent('')
      await loadEntries(true)
    } catch { /* ignore */ } finally {
      setAdding(false)
    }
  }, [addContent, memoryApi, loadEntries])

  const handleDelete = useCallback(async (id: string) => {
    if (!memoryApi) return
    try { await memoryApi.delete(id); setEntries((p) => p.filter((e) => e.id !== id)) } catch { /* ignore */ }
    setConfirmDeleteId(null)
  }, [memoryApi])

  const handleEdit = useCallback(async (id: string, newContent: string, category: string) => {
    if (!memoryApi) return
    await memoryApi.delete(id)
    await memoryApi.add(newContent, category)
    await loadEntries(true)
  }, [memoryApi, loadEntries])

  const handleClearAll = useCallback(async () => {
    if (!memoryApi) return
    for (const e of entries) { try { await memoryApi.delete(e.id) } catch { /* ignore */ } }
    setEntries([])
    setConfirmClearAll(false)
  }, [memoryApi, entries])

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

  if (!memoryApi) {
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
      <SettingsSectionHeader title={ds.notebookSettingsTitle} description={ds.notebookSettingsDesc} />

      {/* add card */}
      <div
        className="flex flex-col gap-3 rounded-xl p-4"
        style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
      >
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
          rows={4}
          className="w-full resize-none rounded-lg px-3 py-2.5 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] outline-none"
          style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-input)' }}
        />
        <button
          onClick={() => void handleAdd()}
          disabled={adding || !addContent.trim()}
          className="flex w-full items-center justify-center gap-1.5 rounded-lg py-2.5 text-sm font-medium transition-[filter] hover:[filter:brightness(1.08)] disabled:opacity-40"
          style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
        >
          {adding ? <SpinnerIcon /> : <Plus size={14} />}
          {ds.notebookAddButton}
        </button>
      </div>

      {/* notebook preview card */}
      <NotebookCard
        entries={entries}
        onClick={() => setModalOpen(true)}
        titles={{
          countLabel: ds.notebookEntries(entries.length),
          viewEdit: ds.notebookViewEdit,
          emptyTitle: ds.memoryEmptyTitle,
          emptyDesc: ds.memoryEmptyDesc,
        }}
      />

      {/* Modal: entries list */}
      <Modal open={modalOpen} onClose={() => { setModalOpen(false); setSearchQuery('') }} title={ds.notebookModalTitle} width="560px">
        <div className="flex flex-col gap-4">
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
            <div className="flex items-center gap-1">
              <button
                onClick={() => void loadEntries(true)}
                disabled={refreshing}
                className="shrink-0 rounded-lg p-1.5 text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)] disabled:opacity-40"
              >
                <RefreshCw size={14} className={refreshing ? 'animate-spin' : ''} />
              </button>
              {entries.length > 0 && (
                <button
                  onClick={() => setConfirmClearAll(true)}
                  className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs text-red-400 transition-colors hover:bg-red-500/10"
                >
                  <Trash2 size={12} />{ds.memoryClearAll}
                </button>
              )}
            </div>
          </div>

          {/* search */}
          {entries.length > 0 && (
            <div
              className="flex items-center gap-2 rounded-lg px-3 py-2"
              style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-input)' }}
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
          )}

          {/* list */}
          {filteredEntries.length === 0 ? (
            <div
              className="flex flex-col items-center justify-center rounded-xl py-14"
              style={{ border: '1px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
            >
              <BookOpen size={28} className="mb-3 text-[var(--c-text-muted)]" />
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
        </div>
      </Modal>

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
