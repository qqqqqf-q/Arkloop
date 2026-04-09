import { useState, useCallback, useEffect, useRef, useLayoutEffect } from 'react'
import { useOutletContext } from 'react-router-dom'
import {
  Ticket,
  Search,
  ChevronDown,
  ChevronRight,
  Ban,
  CheckCircle,
  Pencil,
} from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { EmptyState } from '../../components/EmptyState'
import { Badge, type BadgeVariant } from '../../components/Badge'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { Modal } from '../../components/Modal'
import { FormField } from '../../components/FormField'
import { formatDateTime, useToast } from '@arkloop/shared'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import {
  listAdminInviteCodes,
  patchAdminInviteCode,
  listReferrals,
  type AdminInviteCode,
  type Referral,
} from '../../api/invite-codes'

const PAGE_SIZE = 50

const STATUS_BADGE: Record<string, BadgeVariant> = {
  active: 'success',
  inactive: 'neutral',
}

export function InviteCodesPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.inviteCodes

  const [codes, setCodes] = useState<AdminInviteCode[]>([])
  const [loading, setLoading] = useState(false)
  const [hasMore, setHasMore] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)

  const [query, setQuery] = useState('')
  const searchTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  // 展开行
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [referrals, setReferrals] = useState<Referral[]>([])
  const [referralsLoading, setReferralsLoading] = useState(false)

  // 编辑 max_uses
  const [editTarget, setEditTarget] = useState<AdminInviteCode | null>(null)
  const [editMaxUses, setEditMaxUses] = useState('')
  const [editError, setEditError] = useState('')
  const [editing, setEditing] = useState(false)

  // 停用/启用确认
  const [statusTarget, setStatusTarget] = useState<AdminInviteCode | null>(null)
  const [statusChanging, setStatusChanging] = useState(false)

  const fetchCodes = useCallback(
    async (q: string) => {
      setLoading(true)
      setExpandedId(null)
      setReferrals([])
      try {
        const list = await listAdminInviteCodes(
          { limit: PAGE_SIZE, q: q || undefined },
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
    void fetchCodes('')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const handleSearchChange = useCallback(
    (value: string) => {
      setQuery(value)
      if (searchTimerRef.current) clearTimeout(searchTimerRef.current)
      searchTimerRef.current = setTimeout(() => {
        void fetchCodes(value)
      }, 400)
    },
    [fetchCodes],
  )

  const handleLoadMore = useCallback(async () => {
    if (codes.length === 0) return
    const last = codes[codes.length - 1]
    setLoadingMore(true)
    try {
      const more = await listAdminInviteCodes(
        {
          limit: PAGE_SIZE,
          q: query || undefined,
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
  }, [codes, query, accessToken, addToast, tc.toastLoadFailed])

  const handleToggleExpand = useCallback(
    async (code: AdminInviteCode) => {
      if (expandedId === code.id) {
        setExpandedId(null)
        setReferrals([])
        return
      }
      setExpandedId(code.id)
      setReferrals([])
      setReferralsLoading(true)
      try {
        const refs = await listReferrals(code.user_id, { limit: 50 }, accessToken)
        setReferrals(refs)
      } catch {
        addToast(tc.toastReferralsFailed, 'error')
      } finally {
        setReferralsLoading(false)
      }
    },
    [expandedId, accessToken, addToast, tc.toastReferralsFailed],
  )

  // 编辑 max_uses
  const handleOpenEdit = useCallback((ic: AdminInviteCode) => {
    setEditTarget(ic)
    setEditMaxUses(String(ic.max_uses))
    setEditError('')
  }, [])

  const handleCloseEdit = useCallback(() => {
    if (editing) return
    setEditTarget(null)
  }, [editing])

  const handleSaveEdit = useCallback(async () => {
    if (!editTarget) return
    const val = parseInt(editMaxUses, 10)
    if (isNaN(val) || val <= 0) {
      setEditError(tc.editErrPositive)
      return
    }
    setEditing(true)
    setEditError('')
    try {
      const updated = await patchAdminInviteCode(editTarget.id, { max_uses: val }, accessToken)
      addToast(tc.toastUpdated, 'success')
      setEditTarget(null)
      setCodes((prev) =>
        prev.map((c) =>
          c.id === updated.id ? { ...c, max_uses: updated.max_uses } : c,
        ),
      )
    } catch (err) {
      setEditError(isApiError(err) ? err.message : tc.toastUpdateFailed)
    } finally {
      setEditing(false)
    }
  }, [editTarget, editMaxUses, accessToken, addToast, tc])

  // 停用/启用
  const handleStatusChange = useCallback(async () => {
    if (!statusTarget) return
    const newActive = !statusTarget.is_active
    setStatusChanging(true)
    try {
      const updated = await patchAdminInviteCode(statusTarget.id, { is_active: newActive }, accessToken)
      addToast(tc.toastStatusChanged, 'success')
      setStatusTarget(null)
      setCodes((prev) =>
        prev.map((c) =>
          c.id === updated.id ? { ...c, is_active: updated.is_active } : c,
        ),
      )
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastStatusFailed, 'error')
    } finally {
      setStatusChanging(false)
    }
  }, [statusTarget, accessToken, addToast, tc])

  const inputCls =
    'rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none'
  const thCls = 'whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]'
  const tdCls = 'whitespace-nowrap px-4 py-2.5 text-[var(--c-text-secondary)]'

  const isDeactivateAction = statusTarget?.is_active === true

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tc.title} />

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
      </div>

      <div className="flex flex-1 flex-col overflow-auto">
        {loading ? (
          <div className="flex flex-1 items-center justify-center py-16">
            <p className="text-sm text-[var(--c-text-muted)]">Loading...</p>
          </div>
        ) : codes.length === 0 ? (
          <EmptyState icon={<Ticket size={28} />} message={tc.empty} />
        ) : (
          <div className="overflow-auto">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b border-[var(--c-border-console)]">
                  <th className={thCls} />
                  <th className={thCls}>{tc.colId}</th>
                  <th className={thCls}>{tc.colCode}</th>
                  <th className={thCls}>{tc.colUser}</th>
                  <th className={thCls}>{tc.colEmail}</th>
                  <th className={thCls}>{tc.colMaxUses}</th>
                  <th className={thCls}>{tc.colUseCount}</th>
                  <th className={thCls}>{tc.colStatus}</th>
                  <th className={thCls}>{tc.colCreatedAt}</th>
                  <th className={thCls} />
                </tr>
              </thead>
              <tbody>
                {codes.map((ic) => {
                  const isExpanded = expandedId === ic.id
                  return (
                    <InviteCodeRow
                      key={ic.id}
                      code={ic}
                      isExpanded={isExpanded}
                      referrals={isExpanded ? referrals : []}
                      referralsLoading={isExpanded && referralsLoading}
                      tdCls={tdCls}
                      tc={tc}
                      onToggleExpand={() => void handleToggleExpand(ic)}
                      onEdit={() => handleOpenEdit(ic)}
                      onStatusAction={() => setStatusTarget(ic)}
                    />
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

      {/* 停用/启用确认 */}
      <ConfirmDialog
        open={statusTarget !== null}
        onClose={() => {
          if (!statusChanging) setStatusTarget(null)
        }}
        onConfirm={handleStatusChange}
        title={isDeactivateAction ? tc.deactivateTitle : tc.activateTitle}
        message={
          isDeactivateAction
            ? tc.deactivateMessage(statusTarget?.code ?? '')
            : tc.activateMessage(statusTarget?.code ?? '')
        }
        confirmLabel={isDeactivateAction ? tc.deactivateConfirm : tc.activateConfirm}
        loading={statusChanging}
      />

      {/* 编辑 max_uses */}
      <Modal
        open={editTarget !== null}
        onClose={handleCloseEdit}
        title={tc.editTitle}
        width="380px"
      >
        <div className="flex flex-col gap-4">
          <FormField label={tc.editMaxUses}>
            <input
              type="number"
              min={1}
              value={editMaxUses}
              onChange={(e) => {
                setEditMaxUses(e.target.value)
                setEditError('')
              }}
              className={inputCls}
              autoFocus
            />
          </FormField>

          {editError && (
            <p className="text-xs text-[var(--c-status-error-text)]">{editError}</p>
          )}

          <div className="flex justify-end gap-2 border-t border-[var(--c-border)] pt-3">
            <button
              onClick={handleCloseEdit}
              disabled={editing}
              className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {tc.editCancel}
            </button>
            <button
              onClick={handleSaveEdit}
              disabled={editing}
              className="rounded-lg bg-[var(--c-bg-tag)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-text-primary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {editing ? '...' : tc.editSave}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  )
}

type InviteCodeRowProps = {
  code: AdminInviteCode
  isExpanded: boolean
  referrals: Referral[]
  referralsLoading: boolean
  tdCls: string
  tc: ReturnType<typeof import('../../contexts/LocaleContext').useLocale>['t']['pages']['inviteCodes']
  onToggleExpand: () => void
  onEdit: () => void
  onStatusAction: () => void
}

function InviteCodeRow({
  code,
  isExpanded,
  referrals,
  referralsLoading,
  tdCls,
  tc,
  onToggleExpand,
  onEdit,
  onStatusAction,
}: InviteCodeRowProps) {
  const shortId = code.id.split('-')[0]
  const detailRef = useRef<HTMLDivElement>(null)

  const [cachedReferrals, setCachedReferrals] = useState<Referral[]>([])
  useEffect(() => {
    if (referrals.length === 0) return
    const id = requestAnimationFrame(() => setCachedReferrals(referrals))
    return () => cancelAnimationFrame(id)
  }, [referrals])
  const renderReferrals = referrals.length > 0 ? referrals : cachedReferrals

  useEffect(() => {
    if (!isExpanded) {
      const t = setTimeout(() => { setCachedReferrals([]) }, 210)
      return () => clearTimeout(t)
    }
  }, [isExpanded])

  useLayoutEffect(() => {
    const el = detailRef.current
    if (!el) return
    if (isExpanded) {
      el.style.maxHeight = `${el.scrollHeight}px`
    } else {
      void el.offsetHeight
      el.style.maxHeight = '0px'
    }
  }, [isExpanded])

  useLayoutEffect(() => {
    const el = detailRef.current
    if (!el || !isExpanded) return
    el.style.maxHeight = `${el.scrollHeight}px`
  }, [referrals, referralsLoading, isExpanded])

  const statusKey = code.is_active ? 'active' : 'inactive'

  return (
    <>
      <tr
        onClick={onToggleExpand}
        className="cursor-pointer border-b border-[var(--c-border-console)] transition-colors hover:bg-[var(--c-bg-sub)]"
      >
        <td className={tdCls}>
          <span className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)]">
            {isExpanded ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
          </span>
        </td>
        <td className={tdCls}>
          <span className="font-mono text-xs text-[var(--c-text-muted)]" title={code.id}>
            {shortId}
          </span>
        </td>
        <td className={tdCls}>
          <span className="font-mono text-xs font-medium text-[var(--c-text-primary)]">
            {code.code}
          </span>
        </td>
        <td className={tdCls}>
          <span className="text-[var(--c-text-primary)]">{code.user_login}</span>
        </td>
        <td className={tdCls}>
          <span className="text-xs">{code.user_email ?? '--'}</span>
        </td>
        <td className={tdCls}>
          <span className="tabular-nums text-xs">{code.max_uses}</span>
        </td>
        <td className={tdCls}>
          <span className="tabular-nums text-xs">{code.use_count}</span>
        </td>
        <td className={tdCls}>
          <Badge variant={STATUS_BADGE[statusKey] ?? 'neutral'}>
            {code.is_active ? tc.statusActive : tc.statusInactive}
          </Badge>
        </td>
        <td className={tdCls}>
          <span className="tabular-nums text-xs">
            {formatDateTime(code.created_at, { includeZone: false })}
          </span>
        </td>
        <td className={tdCls}>
          <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
            <button
              onClick={onEdit}
              className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
              title={tc.editTitle}
            >
              <Pencil size={13} />
            </button>
            <button
              onClick={onStatusAction}
              className={[
                'flex items-center justify-center rounded p-1 transition-colors hover:bg-[var(--c-bg-sub)]',
                code.is_active
                  ? 'text-[var(--c-text-muted)] hover:text-[var(--c-status-error-text)]'
                  : 'text-[var(--c-text-muted)] hover:text-[var(--c-status-success-text)]',
              ].join(' ')}
              title={code.is_active ? tc.deactivateTitle : tc.activateTitle}
            >
              {code.is_active ? <Ban size={13} /> : <CheckCircle size={13} />}
            </button>
          </div>
        </td>
      </tr>
      <tr>
        <td colSpan={10} className="p-0">
          <div
            ref={detailRef}
            className="overflow-hidden transition-[max-height] duration-200 ease-in-out"
          >
            <div className="bg-[var(--c-bg-sub)] px-6 py-4">
              <span className="text-[10px] font-medium uppercase tracking-wider text-[var(--c-text-muted)]">
                {tc.referralsTitle}
              </span>
              {referralsLoading ? (
                <p className="py-2 text-center text-xs text-[var(--c-text-muted)]">...</p>
              ) : renderReferrals.length === 0 ? (
                <p className="py-2 text-xs text-[var(--c-text-muted)]">{tc.referralsEmpty}</p>
              ) : (
                <table className="mt-2 w-full text-xs">
                  <thead>
                    <tr className="border-b border-[var(--c-border)]">
                      <th className="py-1.5 text-left font-medium text-[var(--c-text-muted)]">
                        {tc.refColInvitee}
                      </th>
                      <th className="py-1.5 text-left font-medium text-[var(--c-text-muted)]">
                        {tc.refColCredited}
                      </th>
                      <th className="py-1.5 text-left font-medium text-[var(--c-text-muted)]">
                        {tc.refColCreatedAt}
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {renderReferrals.map((ref) => (
                      <tr
                        key={ref.id}
                        className="border-b border-[var(--c-border)] last:border-0"
                      >
                        <td className="py-1.5 text-[var(--c-text-primary)]">
                          {ref.invitee_login}
                        </td>
                        <td className="py-1.5 text-[var(--c-text-secondary)]">
                          {ref.credited ? tc.refCreditedYes : tc.refCreditedNo}
                        </td>
                        <td className="py-1.5 tabular-nums text-[var(--c-text-secondary)]">
                          {formatDateTime(ref.created_at, { includeZone: false })}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          </div>
        </td>
      </tr>
    </>
  )
}
