import { useState, useCallback, useEffect } from 'react'
import { useOutletContext } from 'react-router-dom'
import { UsersRound, Plus, Trash2, ChevronDown, ChevronRight, UserMinus } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { DataTable, type Column } from '../../components/DataTable'
import { Modal } from '../../components/Modal'
import { FormField } from '../../components/FormField'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { formatDateTime, useToast } from '@arkloop/shared'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import {
  listTeams,
  createTeam,
  deleteTeam,
  listTeamMembers,
  addTeamMember,
  removeTeamMember,
  type Team,
  type TeamMember,
} from '../../api/teams'

const ROLE_OPTIONS = ['member', 'owner', 'admin']

export function TeamsPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.teams

  const [teams, setTeams] = useState<Team[]>([])
  const [loading, setLoading] = useState(false)

  // expanded row: team id -> members
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [members, setMembers] = useState<TeamMember[]>([])
  const [membersLoading, setMembersLoading] = useState(false)

  // create team modal
  const [createOpen, setCreateOpen] = useState(false)
  const [createName, setCreateName] = useState('')
  const [createError, setCreateError] = useState('')
  const [creating, setCreating] = useState(false)

  // delete team dialog
  const [deleteTarget, setDeleteTarget] = useState<Team | null>(null)
  const [deleting, setDeleting] = useState(false)

  // add member modal
  const [addMemberOpen, setAddMemberOpen] = useState(false)
  const [addUserId, setAddUserId] = useState('')
  const [addRole, setAddRole] = useState('member')
  const [addMemberError, setAddMemberError] = useState('')
  const [addingMember, setAddingMember] = useState(false)

  // remove member dialog
  const [removeMemberTarget, setRemoveMemberTarget] = useState<TeamMember | null>(null)
  const [removingMember, setRemovingMember] = useState(false)

  const fetchAll = useCallback(async () => {
    setLoading(true)
    try {
      const list = await listTeams(accessToken)
      setTeams(list)
    } catch {
      addToast(tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc.toastLoadFailed])

  useEffect(() => {
    void fetchAll()
  }, [fetchAll])

  const fetchMembers = useCallback(
    async (teamId: string) => {
      setMembersLoading(true)
      try {
        const list = await listTeamMembers(teamId, accessToken)
        setMembers(list)
      } catch {
        addToast(tc.toastMembersLoadFailed, 'error')
      } finally {
        setMembersLoading(false)
      }
    },
    [accessToken, addToast, tc.toastMembersLoadFailed],
  )

  const handleToggleExpand = useCallback(
    (teamId: string) => {
      if (expandedId === teamId) {
        setExpandedId(null)
        setMembers([])
      } else {
        setExpandedId(teamId)
        void fetchMembers(teamId)
      }
    },
    [expandedId, fetchMembers],
  )

  // create team
  const handleOpenCreate = useCallback(() => {
    setCreateName('')
    setCreateError('')
    setCreateOpen(true)
  }, [])

  const handleCloseCreate = useCallback(() => {
    if (creating) return
    setCreateOpen(false)
  }, [creating])

  const handleCreate = useCallback(async () => {
    const name = createName.trim()
    if (!name) {
      setCreateError(tc.errRequired)
      return
    }
    setCreating(true)
    setCreateError('')
    try {
      await createTeam({ name }, accessToken)
      addToast(tc.toastCreated, 'success')
      setCreateOpen(false)
      await fetchAll()
    } catch (err) {
      setCreateError(isApiError(err) ? err.message : tc.toastCreateFailed)
    } finally {
      setCreating(false)
    }
  }, [createName, accessToken, fetchAll, addToast, tc])

  // delete team
  const handleDelete = useCallback(async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await deleteTeam(deleteTarget.id, accessToken)
      addToast(tc.toastDeleted, 'success')
      if (expandedId === deleteTarget.id) {
        setExpandedId(null)
        setMembers([])
      }
      setDeleteTarget(null)
      await fetchAll()
    } catch {
      addToast(tc.toastDeleteFailed, 'error')
    } finally {
      setDeleting(false)
    }
  }, [deleteTarget, accessToken, fetchAll, addToast, expandedId, tc])

  // add member
  const handleOpenAddMember = useCallback(() => {
    setAddUserId('')
    setAddRole('member')
    setAddMemberError('')
    setAddMemberOpen(true)
  }, [])

  const handleCloseAddMember = useCallback(() => {
    if (addingMember) return
    setAddMemberOpen(false)
  }, [addingMember])

  const handleAddMember = useCallback(async () => {
    if (!expandedId) return
    const userId = addUserId.trim()
    const role = addRole.trim()
    if (!userId || !role) {
      setAddMemberError(tc.errRequiredMember)
      return
    }
    setAddingMember(true)
    setAddMemberError('')
    try {
      await addTeamMember(expandedId, { user_id: userId, role }, accessToken)
      addToast(tc.toastMemberAdded, 'success')
      setAddMemberOpen(false)
      void fetchMembers(expandedId)
      // refresh members_count in teams list
      await fetchAll()
    } catch (err) {
      setAddMemberError(isApiError(err) ? err.message : tc.toastMemberAddFailed)
    } finally {
      setAddingMember(false)
    }
  }, [expandedId, addUserId, addRole, accessToken, fetchMembers, fetchAll, addToast, tc])

  // remove member
  const handleRemoveMember = useCallback(async () => {
    if (!removeMemberTarget || !expandedId) return
    setRemovingMember(true)
    try {
      await removeTeamMember(expandedId, removeMemberTarget.user_id, accessToken)
      addToast(tc.toastMemberRemoved, 'success')
      setRemoveMemberTarget(null)
      void fetchMembers(expandedId)
      await fetchAll()
    } catch {
      addToast(tc.toastMemberRemoveFailed, 'error')
    } finally {
      setRemovingMember(false)
    }
  }, [removeMemberTarget, expandedId, accessToken, fetchMembers, fetchAll, addToast, tc])

  const inputCls =
    'rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none'

  const columns: Column<Team>[] = [
    {
      key: 'expand',
      header: '',
      render: (row) => (
        <button
          onClick={(e) => {
            e.stopPropagation()
            handleToggleExpand(row.id)
          }}
          className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
        >
          {expandedId === row.id ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
        </button>
      ),
    },
    {
      key: 'name',
      header: tc.colName,
      render: (row) => (
        <span className="font-medium text-[var(--c-text-primary)]">{row.name}</span>
      ),
    },
    {
      key: 'members_count',
      header: tc.colMembersCount,
      render: (row) => (
        <span className="tabular-nums text-xs text-[var(--c-text-secondary)]">
          {row.members_count}
        </span>
      ),
    },
    {
      key: 'created_at',
      header: tc.colCreatedAt,
      render: (row) => (
        <span className="tabular-nums text-xs">{formatDateTime(row.created_at, { includeZone: false })}</span>
      ),
    },
    {
      key: 'actions',
      header: '',
      render: (row) => (
        <button
          onClick={(e) => {
            e.stopPropagation()
            setDeleteTarget(row)
          }}
          className="flex items-center justify-center rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-status-error-text)]"
          title={tc.deleteTitle}
        >
          <Trash2 size={13} />
        </button>
      ),
    },
  ]

  const headerActions = (
    <button
      onClick={handleOpenCreate}
      className="flex items-center gap-1.5 rounded-lg bg-[var(--c-bg-tag)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
    >
      <Plus size={13} />
      {tc.addTeam}
    </button>
  )

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tc.title} actions={headerActions} />

      <div className="flex flex-1 flex-col overflow-auto">
        <DataTable
          columns={columns}
          data={teams}
          rowKey={(row) => row.id}
          loading={loading}
          emptyMessage={tc.empty}
          emptyIcon={<UsersRound size={28} />}
        />

        {/* expanded members panel */}
        {expandedId !== null && (
          <div className="border-t border-[var(--c-border)] bg-[var(--c-bg-sub)] px-6 py-4">
            <div className="mb-3 flex items-center justify-between">
              <span className="text-xs font-medium uppercase tracking-wider text-[var(--c-text-muted)]">
                {tc.colRole}s
              </span>
              <button
                onClick={handleOpenAddMember}
                className="flex items-center gap-1.5 rounded-lg bg-[var(--c-bg-tag)] px-2.5 py-1 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep2)]"
              >
                <Plus size={11} />
                {tc.addMember}
              </button>
            </div>

            {membersLoading ? (
              <p className="py-3 text-center text-xs text-[var(--c-text-muted)]">...</p>
            ) : members.length === 0 ? (
              <p className="py-3 text-center text-xs text-[var(--c-text-muted)]">
                {tc.emptyMembers}
              </p>
            ) : (
              <table className="w-full text-xs">
                <thead>
                  <tr className="border-b border-[var(--c-border)]">
                    <th className="py-1.5 text-left font-medium text-[var(--c-text-muted)]">
                      {tc.colUserId}
                    </th>
                    <th className="py-1.5 text-left font-medium text-[var(--c-text-muted)]">
                      {tc.colRole}
                    </th>
                    <th className="py-1.5 text-left font-medium text-[var(--c-text-muted)]">
                      {tc.colMemberCreatedAt}
                    </th>
                    <th className="py-1.5" />
                  </tr>
                </thead>
                <tbody>
                  {members.map((m) => (
                    <tr
                      key={m.user_id}
                      className="border-b border-[var(--c-border)] last:border-0"
                    >
                      <td className="py-1.5 font-mono text-[var(--c-text-primary)]">
                        {m.user_id}
                      </td>
                      <td className="py-1.5 text-[var(--c-text-secondary)]">{m.role}</td>
                      <td className="tabular-nums py-1.5 text-[var(--c-text-secondary)]">
                        {formatDateTime(m.created_at, { includeZone: false })}
                      </td>
                      <td className="py-1.5 text-right">
                        <button
                          onClick={() => setRemoveMemberTarget(m)}
                          className="rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-deep2)] hover:text-[var(--c-status-error-text)]"
                          title={tc.removeTitle}
                        >
                          <UserMinus size={12} />
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        )}
      </div>

      {/* Create Team Modal */}
      <Modal open={createOpen} onClose={handleCloseCreate} title={tc.modalTitleCreate} width="400px">
        <div className="flex flex-col gap-4">
          <FormField label={tc.fieldName}>
            <input
              type="text"
              value={createName}
              onChange={(e) => {
                setCreateName(e.target.value)
                setCreateError('')
              }}
              placeholder="Engineering"
              className={inputCls}
              autoFocus
            />
          </FormField>

          {createError && (
            <p className="text-xs text-[var(--c-status-error-text)]">{createError}</p>
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

      {/* Add Member Modal */}
      <Modal
        open={addMemberOpen}
        onClose={handleCloseAddMember}
        title={tc.addMemberTitle}
        width="400px"
      >
        <div className="flex flex-col gap-4">
          <FormField label={tc.fieldUserId}>
            <input
              type="text"
              value={addUserId}
              onChange={(e) => {
                setAddUserId(e.target.value)
                setAddMemberError('')
              }}
              placeholder="uuid"
              className={inputCls}
              autoFocus
            />
          </FormField>

          <FormField label={tc.fieldRole}>
            <select
              value={addRole}
              onChange={(e) => setAddRole(e.target.value)}
              className={inputCls}
            >
              {ROLE_OPTIONS.map((r) => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </select>
          </FormField>

          {addMemberError && (
            <p className="text-xs text-[var(--c-status-error-text)]">{addMemberError}</p>
          )}

          <div className="flex justify-end gap-2 border-t border-[var(--c-border)] pt-3">
            <button
              onClick={handleCloseAddMember}
              disabled={addingMember}
              className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {tc.cancel}
            </button>
            <button
              onClick={handleAddMember}
              disabled={addingMember}
              className="rounded-lg bg-[var(--c-bg-tag)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-text-primary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {addingMember ? '...' : tc.addMember}
            </button>
          </div>
        </div>
      </Modal>

      {/* Remove Member Dialog */}
      <ConfirmDialog
        open={removeMemberTarget !== null}
        onClose={() => {
          if (!removingMember) setRemoveMemberTarget(null)
        }}
        onConfirm={handleRemoveMember}
        title={tc.removeTitle}
        message={tc.removeMessage(removeMemberTarget?.user_id ?? '')}
        confirmLabel={tc.removeConfirm}
        loading={removingMember}
      />

      {/* Delete Team Dialog */}
      <ConfirmDialog
        open={deleteTarget !== null}
        onClose={() => {
          if (!deleting) setDeleteTarget(null)
        }}
        onConfirm={handleDelete}
        title={tc.deleteTitle}
        message={tc.deleteMessage(deleteTarget?.name ?? '')}
        confirmLabel={tc.deleteConfirm}
        loading={deleting}
      />
    </div>
  )
}
