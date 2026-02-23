import { useState, useCallback, useEffect, useRef } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Gift, Search, Ban, Plus } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { EmptyState } from '../../components/EmptyState'
import { Badge, type BadgeVariant } from '../../components/Badge'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { Modal } from '../../components/Modal'
import { FormField } from '../../components/FormField'
import { useToast } from '../../components/useToast'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import {
  listRedemptionCodes,
  batchCreateRedemptionCodes,
  deactivateRedemptionCode,
  type RedemptionCode,
} from '../../api/redemption-codes'

const PAGE_SIZE = 50

const STATUS_BADGE: Record<string, BadgeVariant> = {
  active: 'success',
  inactive: 'neutral',
}

const TYPE_BADGE: Record<string, BadgeVariant> = {
  credit: 'info',
  feature: 'warning',
}

export function RedemptionCodesPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.redemptionCodes

  const [codes, setCodes] = useState<RedemptionCode[]>([])
  const [loading, setLoading] = useState(false)
  const [hasMore, setHasMore] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)

  const [query, setQuery] = useState('')
  const [typeFilter, setTypeFilter] = useState('')
  const searchTimerRef = useRef<ReturnType<typeof setTimeout>>()

  // 停用确认
  const [deactivateTarget, setDeactivateTarget] = useState<RedemptionCode | null>(null)
  const [deactivating, setDeactivating] = useState(false)

  // 批量生成
  const [batchOpen, setBatchOpen] = useState(false)
  const [batchForm, setBatchForm] = useState({
    count: '10',
    type: 'credit',
    value: '',
    max_uses: '1',
    expires_at: '',
    batch_id: '',
  })
  const [batchError, setBatchError] = useState('')
  const [batchCreating, setBatchCreating] = useState(false)

  const fetchCodes = useCallback(
    async (q: string, codeType: string) => {
      setLoading(true)
      try {
        const list = await listRedemptionCodes(
          { limit: PAGE_SIZE, q: q || undefined, type: codeType || undefined },
          accessToken,
        )
        setCodes(list)
        setHasMore(list.length >= PAGE_SIZE)
      } catch {
        addToast(tc.toastLoadFailed, 'error')
      } finally {
        setLoading(false)
      }
    },
    [accessToken, addToast, tc.toastLoadFailed],
  )

  useEffect(() => {
    void fetchCodes(query, typeFilter)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [typeFilter])

  useEffect(() => {
    void fetchCodes('', '')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const handleSearchChange = useCallback(
    (value: string) => {
      setQuery(value)
      if (searchTimerRef.current) clearTimeout(searchTimerRef.current)
      searchTimerRef.current = setTimeout(() => {
        void fetchCodes(value, typeFilter)
      }, 400)
    },
    [fetchCodes, typeFilter],
  )

  const handleLoadMore = useCallback(async () => {
    if (codes.length === 0) return
    const last = codes[codes.length - 1]
    setLoadingMore(true)
    try {
      const more = await listRedemptionCodes(
        {
          limit: PAGE_SIZE,
          q: query || undefined,
          type: typeFilter || undefined,
          before_created_at: last.created_at,
          before_id: last.id,
        },
        accessToken,
      )
      setCodes((prev) => [...prev, ...more])
      setHasMore(more.length >= PAGE_SIZE)
    } catch {
      addToast(tc.toastLoadFailed, 'error')
    } finally {
      setLoadingMore(false)
    }
  }, [codes, query, typeFilter, accessToken, addToast, tc.toastLoadFailed])

  // 停用
  const handleDeactivate = useCallback(async () => {
    if (!deactivateTarget) return
    setDeactivating(true)
    try {
      await deactivateRedemptionCode(deactivateTarget.id, accessToken)
      addToast(tc.toastDeactivated, 'success')
      setDeactivateTarget(null)
      setCodes((prev) =>
        prev.map((c) =>
          c.id === deactivateTarget.id ? { ...c, is_active: false } : c,
        ),
      )
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastDeactivateFailed, 'error')
    } finally {
      setDeactivating(false)
    }
  }, [deactivateTarget, accessToken, addToast, tc])

  // 批量生成
  const handleOpenBatch = useCallback(() => {
    setBatchForm({ count: '10', type: 'credit', value: '', max_uses: '1', expires_at: '', batch_id: '' })
    setBatchError('')
    setBatchOpen(true)
  }, [])

  const handleCloseBatch = useCallback(() => {
    if (batchCreating) return
    setBatchOpen(false)
  }, [batchCreating])

  const handleBatchCreate = useCallback(async () => {
    const count = parseInt(batchForm.count, 10)
    const maxUses = parseInt(batchForm.max_uses, 10)
    const value = batchForm.value.trim()

    if (!count || !value) {
      setBatchError(tc.errRequired)
      return
    }
    if (count < 1 || count > 500) {
      setBatchError(tc.errCountRange)
      return
    }

    setBatchCreating(true)
    setBatchError('')
    try {
      const body: Parameters<typeof batchCreateRedemptionCodes>[0] = {
        count,
        type: batchForm.type,
        value,
        max_uses: isNaN(maxUses) || maxUses <= 0 ? 1 : maxUses,
      }
      if (batchForm.expires_at) body.expires_at = new Date(batchForm.expires_at).toISOString()
      if (batchForm.batch_id.trim()) body.batch_id = batchForm.batch_id.trim()

      const created = await batchCreateRedemptionCodes(body, accessToken)
      addToast(tc.toastCreated(created.length), 'success')
      setBatchOpen(false)
      void fetchCodes(query, typeFilter)
    } catch (err) {
      setBatchError(isApiError(err) ? err.message : tc.toastCreateFailed)
    } finally {
      setBatchCreating(false)
    }
  }, [batchForm, accessToken, addToast, fetchCodes, query, typeFilter, tc])

  const inputCls =
    'rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none'
  const thCls = 'whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]'
  const tdCls = 'whitespace-nowrap px-4 py-2.5 text-[var(--c-text-secondary)]'

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader
        title={tc.title}
        actions={
          <button
            onClick={handleOpenBatch}
            className="flex items-center gap-1.5 rounded-lg bg-[var(--c-bg-tag)] px-3 py-1.5 text-sm font-medium text-[var(--c-text-primary)] transition-colors hover:bg-[var(--c-bg-sub)]"
          >
            <Plus size={14} />
            {tc.addBatch}
          </button>
        }
      />

      <div className="flex items-center gap-3 border-b border-[var(--c-border-console)] px-6 py-3">
        <div className="relative flex-1">
          <Search
            size={14}
            className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--c-text-muted)]"
          />
          <input
            type="text"
            value={query}
            onChange={(e) => handleSearchChange(e.target.value)}
            placeholder={tc.searchPlaceholder}
            className={`${inputCls} w-full pl-8`}
          />
        </div>
        <select
          value={typeFilter}
          onChange={(e) => setTypeFilter(e.target.value)}
          className={`${inputCls} min-w-[120px]`}
        >
          <option value="">{tc.filterAllTypes}</option>
          <option value="credit">{tc.typeCredit}</option>
          <option value="feature">{tc.typeFeature}</option>
        </select>
      </div>

      <div className="flex flex-1 flex-col overflow-auto">
        {loading ? (
          <div className="flex flex-1 items-center justify-center py-16">
            <p className="text-sm text-[var(--c-text-muted)]">Loading...</p>
          </div>
        ) : codes.length === 0 ? (
          <EmptyState icon={<Gift size={28} />} message={tc.empty} />
        ) : (
          <div className="overflow-auto">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b border-[var(--c-border-console)]">
                  <th className={thCls}>{tc.colId}</th>
                  <th className={thCls}>{tc.colCode}</th>
                  <th className={thCls}>{tc.colType}</th>
                  <th className={thCls}>{tc.colValue}</th>
                  <th className={thCls}>{tc.colMaxUses}</th>
                  <th className={thCls}>{tc.colUseCount}</th>
                  <th className={thCls}>{tc.colExpiresAt}</th>
                  <th className={thCls}>{tc.colStatus}</th>
                  <th className={thCls}>{tc.colBatchId}</th>
                  <th className={thCls}>{tc.colCreatedAt}</th>
                  <th className={thCls} />
                </tr>
              </thead>
              <tbody>
                {codes.map((rc) => {
                  const shortId = rc.id.split('-')[0]
                  const statusKey = rc.is_active ? 'active' : 'inactive'
                  return (
                    <tr
                      key={rc.id}
                      className="border-b border-[var(--c-border-console)] transition-colors hover:bg-[var(--c-bg-sub)]"
                    >
                      <td className={tdCls}>
                        <span className="font-mono text-xs text-[var(--c-text-muted)]" title={rc.id}>
                          {shortId}
                        </span>
                      </td>
                      <td className={tdCls}>
                        <span className="font-mono text-xs font-medium text-[var(--c-text-primary)]">
                          {rc.code}
                        </span>
                      </td>
                      <td className={tdCls}>
                        <Badge variant={TYPE_BADGE[rc.type] ?? 'neutral'}>
                          {rc.type === 'credit' ? tc.typeCredit : tc.typeFeature}
                        </Badge>
                      </td>
                      <td className={tdCls}>
                        <span className="tabular-nums text-xs">{rc.value}</span>
                      </td>
                      <td className={tdCls}>
                        <span className="tabular-nums text-xs">{rc.max_uses}</span>
                      </td>
                      <td className={tdCls}>
                        <span className="tabular-nums text-xs">{rc.use_count}</span>
                      </td>
                      <td className={tdCls}>
                        <span className="tabular-nums text-xs">
                          {rc.expires_at ? new Date(rc.expires_at).toLocaleString() : '--'}
                        </span>
                      </td>
                      <td className={tdCls}>
                        <Badge variant={STATUS_BADGE[statusKey] ?? 'neutral'}>
                          {rc.is_active ? tc.statusActive : tc.statusInactive}
                        </Badge>
                      </td>
                      <td className={tdCls}>
                        <span className="font-mono text-xs text-[var(--c-text-muted)]">
                          {rc.batch_id ?? '--'}
                        </span>
                      </td>
                      <td className={tdCls}>
                        <span className="tabular-nums text-xs">
                          {new Date(rc.created_at).toLocaleString()}
                        </span>
                      </td>
                      <td className={tdCls}>
                        {rc.is_active && (
                          <button
                            onClick={() => setDeactivateTarget(rc)}
                            className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-status-error-text)]"
                            title={tc.deactivateTitle}
                          >
                            <Ban size={13} />
                          </button>
                        )}
                      </td>
                    </tr>
                  )
                })}
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

      {/* 停用确认 */}
      <ConfirmDialog
        open={deactivateTarget !== null}
        onClose={() => {
          if (!deactivating) setDeactivateTarget(null)
        }}
        onConfirm={handleDeactivate}
        title={tc.deactivateTitle}
        message={tc.deactivateMessage(deactivateTarget?.code ?? '')}
        confirmLabel={tc.deactivateConfirm}
        loading={deactivating}
      />

      {/* 批量生成 */}
      <Modal
        open={batchOpen}
        onClose={handleCloseBatch}
        title={tc.batchTitle}
        width="460px"
      >
        <div className="flex flex-col gap-4">
          <div className="grid grid-cols-2 gap-3">
            <FormField label={tc.fieldCount}>
              <input
                type="number"
                min={1}
                max={500}
                value={batchForm.count}
                onChange={(e) => {
                  setBatchForm((f) => ({ ...f, count: e.target.value }))
                  setBatchError('')
                }}
                className={inputCls}
                autoFocus
              />
            </FormField>
            <FormField label={tc.fieldType}>
              <select
                value={batchForm.type}
                onChange={(e) => setBatchForm((f) => ({ ...f, type: e.target.value }))}
                className={inputCls}
              >
                <option value="credit">{tc.typeCredit}</option>
                <option value="feature">{tc.typeFeature}</option>
              </select>
            </FormField>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <FormField label={tc.fieldValue}>
              <input
                type="text"
                value={batchForm.value}
                onChange={(e) => {
                  setBatchForm((f) => ({ ...f, value: e.target.value }))
                  setBatchError('')
                }}
                className={inputCls}
                placeholder={batchForm.type === 'credit' ? '1000' : 'feature_key'}
              />
            </FormField>
            <FormField label={tc.fieldMaxUses}>
              <input
                type="number"
                min={1}
                value={batchForm.max_uses}
                onChange={(e) => setBatchForm((f) => ({ ...f, max_uses: e.target.value }))}
                className={inputCls}
              />
            </FormField>
          </div>
          <FormField label={tc.fieldExpiresAt}>
            <input
              type="datetime-local"
              value={batchForm.expires_at}
              onChange={(e) => setBatchForm((f) => ({ ...f, expires_at: e.target.value }))}
              className={inputCls}
            />
          </FormField>
          <FormField label={tc.fieldBatchId}>
            <input
              type="text"
              value={batchForm.batch_id}
              onChange={(e) => setBatchForm((f) => ({ ...f, batch_id: e.target.value }))}
              className={inputCls}
              placeholder="campaign-2026-q1"
            />
          </FormField>

          {batchError && (
            <p className="text-xs text-[var(--c-status-error-text)]">{batchError}</p>
          )}

          <div className="flex justify-end gap-2 border-t border-[var(--c-border)] pt-3">
            <button
              onClick={handleCloseBatch}
              disabled={batchCreating}
              className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {tc.cancel}
            </button>
            <button
              onClick={handleBatchCreate}
              disabled={batchCreating}
              className="rounded-lg bg-[var(--c-bg-tag)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-text-primary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {batchCreating ? '...' : tc.create}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  )
}
