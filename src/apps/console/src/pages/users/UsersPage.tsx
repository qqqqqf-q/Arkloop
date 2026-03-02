import { useState, useCallback, useEffect, useLayoutEffect, useRef } from 'react'
import { useOutletContext } from 'react-router-dom'
import {
  Users as UsersIcon,
  Search,
  ChevronDown,
  ChevronRight,
  ShieldBan,
  ShieldCheck,
  Pencil,
  Trash2,
} from 'lucide-react'
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
  listAdminUsers,
  getAdminUser,
  updateAdminUserStatus,
  updateAdminUser,
  adjustAdminCredits,
  deleteAdminUser,
  type AdminUser,
  type AdminUserDetail,
} from '../../api/admin-users'

const PAGE_SIZE = 50

const STATUS_BADGE: Record<string, BadgeVariant> = {
  active: 'success',
  suspended: 'error',
}

function DetailField({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-[10px] font-medium uppercase tracking-wider text-[var(--c-text-muted)]">
        {label}
      </span>
      <span className="font-mono text-xs text-[var(--c-text-primary)]">{value}</span>
    </div>
  )
}

export function UsersPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.users

  const [users, setUsers] = useState<AdminUser[]>([])
  const [loading, setLoading] = useState(false)
  const [hasMore, setHasMore] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)

  const [query, setQuery] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const searchTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [detail, setDetail] = useState<AdminUserDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)

  const [statusTarget, setStatusTarget] = useState<AdminUser | null>(null)
  const [statusChanging, setStatusChanging] = useState(false)

  // delete
  const [deleteTarget, setDeleteTarget] = useState<AdminUser | null>(null)
  const [deleting, setDeleting] = useState(false)

  // edit modal
  const [editTarget, setEditTarget] = useState<AdminUserDetail | null>(null)
  const [editForm, setEditForm] = useState({
    username: '',
    email: '',
    locale: '',
    timezone: '',
    email_verified: false,
  })
  const [editError, setEditError] = useState('')
  const [editing, setEditing] = useState(false)

  // credit adjust modal
  const [creditTarget, setCreditTarget] = useState<{ orgID: string; username: string } | null>(null)
  const [creditForm, setCreditForm] = useState({ amount: '', note: '' })
  const [creditError, setCreditError] = useState('')
  const [creditAdjusting, setCreditAdjusting] = useState(false)

  const fetchUsers = useCallback(
    async (q: string, status: string) => {
      setLoading(true)
      setExpandedId(null)
      setDetail(null)
      try {
        const list = await listAdminUsers(
          { limit: PAGE_SIZE, q: q || undefined, status: status || undefined },
          accessToken,
        )
        setUsers(list)
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
    void fetchUsers(query, statusFilter)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [statusFilter])

  useEffect(() => {
    void fetchUsers('', '')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const handleSearchChange = useCallback(
    (value: string) => {
      setQuery(value)
      if (searchTimerRef.current) clearTimeout(searchTimerRef.current)
      searchTimerRef.current = setTimeout(() => {
        void fetchUsers(value, statusFilter)
      }, 400)
    },
    [fetchUsers, statusFilter],
  )

  const handleLoadMore = useCallback(async () => {
    if (users.length === 0) return
    const last = users[users.length - 1]
    setLoadingMore(true)
    try {
      const more = await listAdminUsers(
        {
          limit: PAGE_SIZE,
          q: query || undefined,
          status: statusFilter || undefined,
          before_created_at: last.created_at,
          before_id: last.id,
        },
        accessToken,
      )
      setUsers((prev) => [...prev, ...more])
      setHasMore(more.length >= PAGE_SIZE)
    } catch {
      addToast(tc.toastLoadFailed, 'error')
    } finally {
      setLoadingMore(false)
    }
  }, [users, query, statusFilter, accessToken, addToast, tc.toastLoadFailed])

  const handleToggleExpand = useCallback(
    async (userId: string) => {
      if (expandedId === userId) {
        setExpandedId(null)
        setDetail(null)
        return
      }
      setExpandedId(userId)
      setDetail(null)
      setDetailLoading(true)
      try {
        const d = await getAdminUser(userId, accessToken)
        setDetail(d)
      } catch {
        addToast(tc.toastDetailFailed, 'error')
      } finally {
        setDetailLoading(false)
      }
    },
    [expandedId, accessToken, addToast, tc.toastDetailFailed],
  )

  const handleStatusChange = useCallback(async () => {
    if (!statusTarget) return
    const newStatus = statusTarget.status === 'active' ? 'suspended' : 'active'
    setStatusChanging(true)
    try {
      await updateAdminUserStatus(statusTarget.id, newStatus, accessToken)
      addToast(newStatus === 'suspended' ? tc.toastSuspended : tc.toastActivated, 'success')
      setStatusTarget(null)
      setUsers((prev) =>
        prev.map((u) => (u.id === statusTarget.id ? { ...u, status: newStatus } : u)),
      )
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastStatusFailed, 'error')
    } finally {
      setStatusChanging(false)
    }
  }, [statusTarget, accessToken, addToast, tc])

  const handleDelete = useCallback(async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await deleteAdminUser(deleteTarget.id, accessToken)
      addToast(tc.toastDeleted, 'success')
      setDeleteTarget(null)
      setUsers((prev) => prev.filter((u) => u.id !== deleteTarget.id))
      if (expandedId === deleteTarget.id) {
        setExpandedId(null)
        setDetail(null)
      }
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastDeleteFailed, 'error')
    } finally {
      setDeleting(false)
    }
  }, [deleteTarget, accessToken, addToast, expandedId, tc])

  const handleOpenEdit = useCallback((d: AdminUserDetail) => {
    setEditTarget(d)
    setEditForm({
      username: d.username,
      email: d.email ?? '',
      locale: d.locale ?? '',
      timezone: d.timezone ?? '',
      email_verified: !!d.email_verified_at,
    })
    setEditError('')
  }, [])

  const handleOpenCredit = useCallback((d: AdminUserDetail) => {
    const orgID = d.orgs[0]?.org_id
    if (!orgID) return
    setCreditTarget({ orgID, username: d.username })
    setCreditForm({ amount: '', note: '' })
    setCreditError('')
  }, [])

  const handleCloseCredit = useCallback(() => {
    if (creditAdjusting) return
    setCreditTarget(null)
  }, [creditAdjusting])

  const handleSaveCredit = useCallback(async () => {
    if (!creditTarget) return
    const amount = parseInt(creditForm.amount, 10)
    if (!creditForm.amount || isNaN(amount) || amount === 0) {
      setCreditError(tc.creditAdjustErrAmount)
      return
    }
    const note = creditForm.note.trim()
    if (!note) {
      setCreditError(tc.creditAdjustErrNote)
      return
    }
    setCreditAdjusting(true)
    setCreditError('')
    try {
      await adjustAdminCredits({ org_id: creditTarget.orgID, amount, note }, accessToken)
      addToast(tc.toastCreditAdjusted, 'success')
      setCreditTarget(null)
    } catch (err) {
      setCreditError(isApiError(err) ? err.message : tc.toastCreditAdjustFailed)
    } finally {
      setCreditAdjusting(false)
    }
  }, [creditTarget, creditForm, accessToken, addToast, tc])

  const handleCloseEdit = useCallback(() => {
    if (editing) return
    setEditTarget(null)
  }, [editing])

  const handleSaveEdit = useCallback(async () => {
    if (!editTarget) return
    const username = editForm.username.trim()
    if (!username) {
      setEditError(tc.editErrNameRequired)
      return
    }
    setEditing(true)
    setEditError('')
    try {
      const updated = await updateAdminUser(
        editTarget.id,
        {
          username: username,
          email: editForm.email.trim() || null,
          locale: editForm.locale.trim() || null,
          timezone: editForm.timezone.trim() || null,
          email_verified: editForm.email_verified,
        },
        accessToken,
      )
      addToast(tc.toastEditSaved, 'success')
      setEditTarget(null)
      // 刷新列表中的用户数据
      setUsers((prev) =>
        prev.map((u) =>
          u.id === updated.id
            ? { ...u, username: updated.username, email: updated.email }
            : u,
        ),
      )
      // 刷新展开的详情
      if (expandedId === updated.id) {
        setDetail((prev) => (prev ? { ...prev, ...updated } : prev))
      }
    } catch (err) {
      setEditError(isApiError(err) ? err.message : tc.toastEditFailed)
    } finally {
      setEditing(false)
    }
  }, [editTarget, editForm, accessToken, addToast, expandedId, tc])

  const inputCls =
    'rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none'

  const thCls = 'whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]'
  const tdCls = 'whitespace-nowrap px-4 py-2.5 text-[var(--c-text-secondary)]'

  const isSuspendAction = statusTarget?.status === 'active'

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
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
          className={`${inputCls} min-w-[120px]`}
        >
          <option value="">{tc.filterAll}</option>
          <option value="active">{tc.filterActive}</option>
          <option value="suspended">{tc.filterSuspended}</option>
        </select>
      </div>

      <div className="flex flex-1 flex-col overflow-auto">
        {loading ? (
          <div className="flex flex-1 items-center justify-center py-16">
            <p className="text-sm text-[var(--c-text-muted)]">Loading...</p>
          </div>
        ) : users.length === 0 ? (
          <EmptyState icon={<UsersIcon size={28} />} message={tc.empty} />
        ) : (
          <div className="overflow-auto">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b border-[var(--c-border-console)]">
                  <th className={thCls} />
                  <th className={thCls}>{tc.colId}</th>
                  <th className={thCls}>{tc.colLogin}</th>
                  <th className={thCls}>{tc.colEmail}</th>
                  <th className={thCls}>{tc.colStatus}</th>
                  <th className={thCls}>{tc.colLastLogin}</th>
                  <th className={thCls}>{tc.colCreatedAt}</th>
                  <th className={thCls} />
                </tr>
              </thead>
              <tbody>
                {users.map((user) => {
                  const isExpanded = expandedId === user.id
                  return (
                    <UserRow
                      key={user.id}
                      user={user}
                      isExpanded={isExpanded}
                      detail={isExpanded ? detail : null}
                      detailLoading={isExpanded && detailLoading}
                      tdCls={tdCls}
                      tc={tc}
                      onToggleExpand={() => void handleToggleExpand(user.id)}
                      onStatusAction={() => setStatusTarget(user)}
                      onDelete={() => setDeleteTarget(user)}
                      onEdit={handleOpenEdit}
                      onCredit={handleOpenCredit}
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

      <ConfirmDialog
        open={statusTarget !== null}
        onClose={() => {
          if (!statusChanging) setStatusTarget(null)
        }}
        onConfirm={handleStatusChange}
        title={isSuspendAction ? tc.suspendTitle : tc.activateTitle}
        message={
          isSuspendAction
            ? tc.suspendMessage(statusTarget?.username ?? '')
            : tc.activateMessage(statusTarget?.username ?? '')
        }
        confirmLabel={isSuspendAction ? tc.suspendConfirm : tc.activateConfirm}
        loading={statusChanging}
      />

      <ConfirmDialog
        open={deleteTarget !== null}
        onClose={() => { if (!deleting) setDeleteTarget(null) }}
        onConfirm={handleDelete}
        title={tc.deleteTitle}
        message={tc.deleteMessage(deleteTarget?.username ?? '')}
        confirmLabel={tc.deleteConfirm}
        loading={deleting}
      />

      {/* Edit User Modal */}
      <Modal
        open={editTarget !== null}
        onClose={handleCloseEdit}
        title={tc.editTitle}
        width="460px"
      >
        <div className="flex flex-col gap-4">
          <FormField label={tc.editUsername}>
            <input
              type="text"
              value={editForm.username}
              onChange={(e) => {
                setEditForm((f) => ({ ...f, username: e.target.value }))
                setEditError('')
              }}
              className={inputCls}
              autoFocus
            />
          </FormField>
          <FormField label={tc.editEmail}>
            <input
              type="email"
              value={editForm.email}
              onChange={(e) => setEditForm((f) => ({ ...f, email: e.target.value }))}
              className={inputCls}
              placeholder="user@example.com"
            />
          </FormField>
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={editForm.email_verified}
              onChange={(e) => setEditForm((f) => ({ ...f, email_verified: e.target.checked }))}
              className="h-3.5 w-3.5 rounded border-[var(--c-border)] accent-[var(--c-status-success-text)]"
            />
            <span className="text-xs text-[var(--c-text-secondary)]">
              {tc.editEmailVerified}
            </span>
          </label>
          <div className="grid grid-cols-2 gap-3">
            <FormField label={tc.editLocale}>
              <input
                type="text"
                value={editForm.locale}
                onChange={(e) => setEditForm((f) => ({ ...f, locale: e.target.value }))}
                className={inputCls}
                placeholder="zh-CN"
              />
            </FormField>
            <FormField label={tc.editTimezone}>
              <input
                type="text"
                value={editForm.timezone}
                onChange={(e) => setEditForm((f) => ({ ...f, timezone: e.target.value }))}
                className={inputCls}
                placeholder="Asia/Shanghai"
              />
            </FormField>
          </div>

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

      {/* Credit Adjust Modal */}
      <Modal
        open={creditTarget !== null}
        onClose={handleCloseCredit}
        title={tc.creditAdjustTitle}
        width="360px"
      >
        <div className="flex flex-col gap-4">
          <FormField label={tc.creditAdjustAmount}>
            <input
              type="number"
              value={creditForm.amount}
              onChange={(e) => {
                setCreditForm((f) => ({ ...f, amount: e.target.value }))
                setCreditError('')
              }}
              className={inputCls}
              placeholder="+100 或 -50"
              autoFocus
            />
          </FormField>
          <FormField label={tc.creditAdjustNote}>
            <input
              type="text"
              value={creditForm.note}
              onChange={(e) => {
                setCreditForm((f) => ({ ...f, note: e.target.value }))
                setCreditError('')
              }}
              className={inputCls}
              placeholder={tc.creditAdjustNotePlaceholder}
            />
          </FormField>
          {creditError && (
            <p className="text-xs text-[var(--c-status-error-text)]">{creditError}</p>
          )}
          <div className="flex justify-end gap-2 border-t border-[var(--c-border)] pt-3">
            <button
              onClick={handleCloseCredit}
              disabled={creditAdjusting}
              className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {tc.creditAdjustCancel}
            </button>
            <button
              onClick={() => void handleSaveCredit()}
              disabled={creditAdjusting}
              className="rounded-lg bg-[var(--c-bg-tag)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-text-primary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {creditAdjusting ? '...' : tc.creditAdjustConfirm}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  )
}

type UserRowProps = {
  user: AdminUser
  isExpanded: boolean
  detail: AdminUserDetail | null
  detailLoading: boolean
  tdCls: string
  tc: ReturnType<typeof import('../../contexts/LocaleContext').useLocale>['t']['pages']['users']
  onToggleExpand: () => void
  onStatusAction: () => void
  onDelete: () => void
  onEdit: (detail: AdminUserDetail) => void
  onCredit: (detail: AdminUserDetail) => void
}

function UserRow({
  user,
  isExpanded,
  detail,
  detailLoading,
  tdCls,
  tc,
  onToggleExpand,
  onStatusAction,
  onDelete,
  onEdit,
  onCredit,
}: UserRowProps) {
  const shortId = user.id.split('-')[0]
  const detailRef = useRef<HTMLDivElement>(null)

  // 收缩动画期间保留上一次的 detail 内容，防止内容消失导致高度瞬降
  const lastDetailRef = useRef<AdminUserDetail | null>(null)
  if (detail !== null) lastDetailRef.current = detail
  const renderDetail = detail ?? lastDetailRef.current

  // 动画结束后清除缓存（210ms > 过渡时长 200ms）
  useEffect(() => {
    if (!isExpanded) {
      const t = setTimeout(() => { lastDetailRef.current = null }, 210)
      return () => clearTimeout(t)
    }
  }, [isExpanded])

  useLayoutEffect(() => {
    const el = detailRef.current
    if (!el) return
    if (isExpanded) {
      el.style.maxHeight = `${el.scrollHeight}px`
    } else {
      // el.style.maxHeight 保留了展开时设置的像素值（不被 React style prop 覆盖）
      // void offsetHeight 让浏览器将当前值提交为过渡起点，再设 0 触发动画
      void el.offsetHeight
      el.style.maxHeight = '0px'
    }
  }, [isExpanded])

  // 内容（detail/loading）变化时更新展开高度
  useLayoutEffect(() => {
    const el = detailRef.current
    if (!el || !isExpanded) return
    el.style.maxHeight = `${el.scrollHeight}px`
  }, [detail, detailLoading])

  return (
    <>
      <tr
        onClick={onToggleExpand}
        className="cursor-pointer border-b border-[var(--c-border-console)] transition-colors hover:bg-[var(--c-bg-sub)]"
      >
        <td className={tdCls}>
          <span className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-transform duration-200">
            {isExpanded ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
          </span>
        </td>
        <td className={tdCls}>
          <span className="font-mono text-xs text-[var(--c-text-muted)]" title={user.id}>
            {shortId}
          </span>
        </td>
        <td className={tdCls}>
          <span className="font-mono text-xs text-[var(--c-text-secondary)]">{user.login ?? '--'}</span>
        </td>
        <td className={tdCls}>
          <span className="text-xs">{user.email ?? '--'}</span>
        </td>
        <td className={tdCls}>
          <Badge variant={STATUS_BADGE[user.status] ?? 'neutral'}>
            {user.status === 'active' ? tc.statusActive : tc.statusSuspended}
          </Badge>
        </td>
        <td className={tdCls}>
          <span className="tabular-nums text-xs">
            {user.last_login_at ? new Date(user.last_login_at).toLocaleString() : '--'}
          </span>
        </td>
        <td className={tdCls}>
          <span className="tabular-nums text-xs">
            {new Date(user.created_at).toLocaleString()}
          </span>
        </td>
        <td className={tdCls}>
          <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
            <button
              onClick={onStatusAction}
              className={[
                'flex items-center justify-center rounded p-1 transition-colors hover:bg-[var(--c-bg-sub)]',
                user.status === 'active'
                  ? 'text-[var(--c-text-muted)] hover:text-[var(--c-status-error-text)]'
                  : 'text-[var(--c-text-muted)] hover:text-[var(--c-status-success-text)]',
              ].join(' ')}
              title={user.status === 'active' ? tc.suspendTitle : tc.activateTitle}
            >
              {user.status === 'active' ? <ShieldBan size={14} /> : <ShieldCheck size={14} />}
            </button>
            <button
              onClick={onDelete}
              className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-red-500"
              title={tc.deleteTitle}
            >
              <Trash2 size={14} />
            </button>
          </div>
        </td>
      </tr>
      <tr>
        <td colSpan={9} className="p-0">
          <div
            ref={detailRef}
            className="overflow-hidden transition-[max-height] duration-200 ease-in-out"
          >
            <div className="bg-[var(--c-bg-sub)] px-6 py-4">
              {detailLoading ? (
                <p className="py-2 text-center text-xs text-[var(--c-text-muted)]">...</p>
              ) : renderDetail ? (
                <div className="flex flex-col gap-4">
                  <div className="flex items-start justify-between">
                    <div className="grid flex-1 grid-cols-2 gap-x-8 gap-y-3 sm:grid-cols-4">
                      <DetailField label={tc.detailId} value={renderDetail.id} />
                      <DetailField label={tc.detailLogin} value={renderDetail.login ?? '--'} />
                      <DetailField label={tc.detailEmail} value={renderDetail.email ?? '--'} />
                      <DetailField
                        label={tc.detailEmailVerified}
                        value={
                          renderDetail.email_verified_at
                            ? new Date(renderDetail.email_verified_at).toLocaleString()
                            : tc.detailEmailNotVerified
                        }
                      />
                      <DetailField label={tc.detailLocale} value={renderDetail.locale ?? '--'} />
                      <DetailField label={tc.detailTimezone} value={renderDetail.timezone ?? '--'} />
                    </div>
                    <div className="ml-4 flex shrink-0 gap-2">
                      {renderDetail.orgs.length > 0 && (
                        <button
                          onClick={() => onCredit(renderDetail)}
                          className="flex items-center gap-1.5 rounded-lg bg-[var(--c-bg-tag)] px-2.5 py-1 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep2)]"
                        >
                          {tc.creditAdjustButton}
                        </button>
                      )}
                      <button
                        onClick={() => onEdit(renderDetail)}
                        className="flex items-center gap-1.5 rounded-lg bg-[var(--c-bg-tag)] px-2.5 py-1 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep2)]"
                      >
                        <Pencil size={11} />
                        {tc.editButton}
                      </button>
                    </div>
                  </div>

                  <div>
                    <span className="text-[10px] font-medium uppercase tracking-wider text-[var(--c-text-muted)]">
                      {tc.detailOrgs}
                    </span>
                    {renderDetail.orgs.length > 0 ? (
                      <table className="mt-2 w-full text-xs">
                        <thead>
                          <tr className="border-b border-[var(--c-border)]">
                            <th className="py-1.5 text-left font-medium text-[var(--c-text-muted)]">
                              {tc.detailOrgId}
                            </th>
                            <th className="py-1.5 text-left font-medium text-[var(--c-text-muted)]">
                              {tc.detailOrgRole}
                            </th>
                          </tr>
                        </thead>
                        <tbody>
                          {renderDetail.orgs.map((o) => (
                            <tr
                              key={o.org_id}
                              className="border-b border-[var(--c-border)] last:border-0"
                            >
                              <td className="py-1.5 font-mono text-[var(--c-text-primary)]">
                                {o.org_id}
                              </td>
                              <td className="py-1.5 text-[var(--c-text-secondary)]">{o.role}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    ) : (
                      <p className="py-2 text-xs text-[var(--c-text-muted)]">
                        {tc.detailNoOrgs}
                      </p>
                    )}
                  </div>
                </div>
              ) : null}
            </div>
          </div>
        </td>
      </tr>
    </>
  )
}
