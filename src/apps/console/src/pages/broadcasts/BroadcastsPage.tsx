import { useState, useCallback, useEffect } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Megaphone, Plus } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { EmptyState } from '../../components/EmptyState'
import { Badge, type BadgeVariant } from '../../components/Badge'
import { Modal } from '../../components/Modal'
import { FormField } from '../../components/FormField'
import { useToast } from '../../components/useToast'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import {
  listBroadcasts,
  createBroadcast,
  type Broadcast,
} from '../../api/broadcasts'

const PAGE_SIZE = 50

const STATUS_BADGE: Record<string, BadgeVariant> = {
  pending: 'warning',
  processing: 'neutral',
  completed: 'success',
  failed: 'error',
}

export function BroadcastsPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.broadcasts

  const [items, setItems] = useState<Broadcast[]>([])
  const [loading, setLoading] = useState(false)
  const [hasMore, setHasMore] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)

  const [createOpen, setCreateOpen] = useState(false)
  const [form, setForm] = useState({ type: 'announcement', titleZh: '', titleEn: '', bodyZh: '', bodyEn: '', target: 'all' })
  const [formError, setFormError] = useState('')
  const [creating, setCreating] = useState(false)

  const fetchList = useCallback(async () => {
    setLoading(true)
    try {
      const list = await listBroadcasts({ limit: PAGE_SIZE }, accessToken)
      setItems(list)
      setHasMore(list.length >= PAGE_SIZE)
    } catch {
      addToast(tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc])

  useEffect(() => {
    void fetchList()
  }, [fetchList])

  const handleLoadMore = useCallback(async () => {
    if (items.length === 0) return
    setLoadingMore(true)
    try {
      const last = items[items.length - 1]
      const more = await listBroadcasts(
        { limit: PAGE_SIZE, before_created_at: last.created_at, before_id: last.id },
        accessToken,
      )
      setItems((prev) => [...prev, ...more])
      setHasMore(more.length >= PAGE_SIZE)
    } catch {
      addToast(tc.toastLoadFailed, 'error')
    } finally {
      setLoadingMore(false)
    }
  }, [items, accessToken, addToast, tc])

  const handleOpenCreate = useCallback(() => {
    setForm({ type: 'announcement', titleZh: '', titleEn: '', bodyZh: '', bodyEn: '', target: 'all' })
    setFormError('')
    setCreateOpen(true)
  }, [])

  const handleCloseCreate = useCallback(() => {
    if (!creating) setCreateOpen(false)
  }, [creating])

  const handleCreate = useCallback(async () => {
    const titleFallback = form.titleZh.trim() || form.titleEn.trim()
    if (!titleFallback) {
      setFormError(tc.errTitleRequired)
      return
    }
    setCreating(true)

    // 构建 i18n payload — 只包含非空语言版本
    const i18nTitle: Record<string, string> = {}
    const i18nBody: Record<string, string> = {}
    if (form.titleZh.trim()) i18nTitle['zh'] = form.titleZh.trim()
    if (form.titleEn.trim()) i18nTitle['en'] = form.titleEn.trim()
    if (form.bodyZh.trim()) i18nBody['zh'] = form.bodyZh.trim()
    if (form.bodyEn.trim()) i18nBody['en'] = form.bodyEn.trim()

    const bodyFallback = form.bodyZh.trim() || form.bodyEn.trim()

    try {
      await createBroadcast(
        {
          type: form.type,
          title: titleFallback,
          body: bodyFallback,
          target: form.target,
          payload: { i18n: { title: i18nTitle, body: i18nBody } },
        },
        accessToken,
      )
      setCreateOpen(false)
      addToast(tc.toastCreated, 'success')
      void fetchList()
    } catch (err) {
      if (isApiError(err)) {
        addToast(err.message, 'error')
      } else {
        addToast(tc.toastCreateFailed, 'error')
      }
    } finally {
      setCreating(false)
    }
  }, [form, accessToken, addToast, tc, fetchList])

  const inputCls =
    'w-full rounded-md border border-[var(--c-border)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none transition-colors focus:border-[var(--c-border-focus)]'
  const thCls = 'whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]'
  const tdCls = 'whitespace-nowrap px-4 py-2.5 text-[var(--c-text-secondary)]'

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader
        title={tc.title}
        actions={
          <button
            onClick={handleOpenCreate}
            className="flex items-center gap-1.5 rounded-lg bg-[var(--c-bg-tag)] px-3 py-1.5 text-sm font-medium text-[var(--c-text-primary)] transition-colors hover:bg-[var(--c-bg-sub)]"
          >
            <Plus size={14} />
            {tc.addBroadcast}
          </button>
        }
      />

      <div className="flex flex-1 flex-col overflow-auto">
        {loading ? (
          <div className="flex flex-1 items-center justify-center py-16">
            <p className="text-sm text-[var(--c-text-muted)]">Loading...</p>
          </div>
        ) : items.length === 0 ? (
          <EmptyState icon={<Megaphone size={28} />} message={tc.empty} />
        ) : (
          <div className="overflow-auto">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b border-[var(--c-border-console)]">
                  <th className={thCls}>{tc.colType}</th>
                  <th className={thCls}>{tc.colTitle}</th>
                  <th className={thCls}>{tc.colTarget}</th>
                  <th className={thCls}>{tc.colSentCount}</th>
                  <th className={thCls}>{tc.colStatus}</th>
                  <th className={thCls}>{tc.colCreatedAt}</th>
                </tr>
              </thead>
              <tbody>
                {items.map((b) => (
                  <tr
                    key={b.id}
                    className="border-b border-[var(--c-border-console)] transition-colors hover:bg-[var(--c-bg-sub)]"
                  >
                    <td className={tdCls}>
                      <span className="text-xs">{b.type}</span>
                    </td>
                    <td className={tdCls}>
                      <span className="text-xs font-medium text-[var(--c-text-primary)]">
                        {b.title}
                      </span>
                    </td>
                    <td className={tdCls}>
                      <span className="text-xs">
                        {b.target_type === 'all' ? tc.targetAll : b.target_id ?? '--'}
                      </span>
                    </td>
                    <td className={tdCls}>
                      <span className="tabular-nums text-xs">{b.sent_count}</span>
                    </td>
                    <td className={tdCls}>
                      <Badge variant={STATUS_BADGE[b.status] ?? 'neutral'}>{b.status}</Badge>
                    </td>
                    <td className={tdCls}>
                      <span className="tabular-nums text-xs">
                        {new Date(b.created_at).toLocaleString()}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {hasMore && !loading && (
          <div className="flex justify-center border-t border-[var(--c-border-console)] py-3">
            <button
              onClick={handleLoadMore}
              disabled={loadingMore}
              className="rounded-lg bg-[var(--c-bg-tag)] px-4 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {loadingMore ? '...' : tc.loadMore}
            </button>
          </div>
        )}
      </div>

      <Modal open={createOpen} onClose={handleCloseCreate} title={tc.modalTitle} width="460px">
        <div className="flex flex-col gap-4">
          <FormField label={tc.fieldType}>
            <select
              value={form.type}
              onChange={(e) => setForm((f) => ({ ...f, type: e.target.value }))}
              className={inputCls}
            >
              <option value="announcement">{tc.typeAnnouncement}</option>
              <option value="maintenance">{tc.typeMaintenance}</option>
              <option value="update">{tc.typeUpdate}</option>
            </select>
          </FormField>
          <FormField label={tc.fieldTitleZh}>
            <input
              type="text"
              value={form.titleZh}
              onChange={(e) => {
                setForm((f) => ({ ...f, titleZh: e.target.value }))
                setFormError('')
              }}
              className={inputCls}
              autoFocus
            />
          </FormField>
          <FormField label={tc.fieldTitleEn}>
            <input
              type="text"
              value={form.titleEn}
              onChange={(e) => {
                setForm((f) => ({ ...f, titleEn: e.target.value }))
                setFormError('')
              }}
              className={inputCls}
            />
          </FormField>
          <FormField label={tc.fieldBodyZh}>
            <textarea
              value={form.bodyZh}
              onChange={(e) => setForm((f) => ({ ...f, bodyZh: e.target.value }))}
              className={`${inputCls} min-h-[72px] resize-y`}
            />
          </FormField>
          <FormField label={tc.fieldBodyEn}>
            <textarea
              value={form.bodyEn}
              onChange={(e) => setForm((f) => ({ ...f, bodyEn: e.target.value }))}
              className={`${inputCls} min-h-[72px] resize-y`}
            />
          </FormField>
          <FormField label={tc.fieldTarget}>
            <input
              type="text"
              value={form.target}
              onChange={(e) => setForm((f) => ({ ...f, target: e.target.value }))}
              className={inputCls}
              placeholder={tc.fieldTargetPlaceholder}
            />
          </FormField>

          {formError && (
            <p className="text-xs text-[var(--c-status-error-text)]">{formError}</p>
          )}

          <div className="flex justify-end gap-2 border-t border-[var(--c-border)] pt-3">
            <button
              onClick={handleCloseCreate}
              disabled={creating}
              className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {tc.cancel}
            </button>
            <button
              onClick={handleCreate}
              disabled={creating}
              className="rounded-lg bg-[var(--c-bg-tag)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-text-primary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {creating ? '...' : tc.create}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  )
}
