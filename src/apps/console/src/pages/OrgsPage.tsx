import { useState, useCallback, useEffect } from 'react'
import { useOutletContext } from 'react-router-dom'
import { RefreshCw, Building2 } from 'lucide-react'
import type { ConsoleOutletContext } from '../layouts/ConsoleLayout'
import { PageHeader } from '../components/PageHeader'
import { EmptyState } from '../components/EmptyState'
import { useToast } from '@arkloop/shared'
import { listMyOrgs, type Org } from '../api/orgs'

type TypeFilter = 'all' | 'personal' | 'workspace'

export function OrgsPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()

  const [orgs, setOrgs] = useState<Org[]>([])
  const [loading, setLoading] = useState(false)
  const [filter, setFilter] = useState<TypeFilter>('all')

  const fetchOrgs = useCallback(async () => {
    setLoading(true)
    try {
      const data = await listMyOrgs(accessToken)
      setOrgs(data)
    } catch {
      addToast('Failed to load organizations', 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast])

  useEffect(() => {
    void fetchOrgs()
  }, [fetchOrgs])

  const filtered = filter === 'all' ? orgs : orgs.filter((o) => o.type === filter)

  const tabs: { value: TypeFilter; label: string }[] = [
    { value: 'all', label: 'All' },
    { value: 'personal', label: 'Personal' },
    { value: 'workspace', label: 'Workspace' },
  ]

  const actions = (
    <button
      onClick={fetchOrgs}
      disabled={loading}
      className="flex items-center gap-1.5 rounded-lg border border-[var(--c-border)] px-2.5 py-1.5 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
    >
      <RefreshCw size={13} className={loading ? 'animate-spin' : ''} />
      Refresh
    </button>
  )

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title="Organizations" actions={actions} />

      <div className="flex gap-1 border-b border-[var(--c-border-console)] px-6">
        {tabs.map((tab) => (
          <button
            key={tab.value}
            onClick={() => setFilter(tab.value)}
            className={[
              'px-3 py-2 text-xs transition-colors',
              filter === tab.value
                ? 'border-b-2 border-[var(--c-accent)] font-medium text-[var(--c-text-primary)]'
                : 'text-[var(--c-text-muted)] hover:text-[var(--c-text-secondary)]',
            ].join(' ')}
          >
            {tab.label}
          </button>
        ))}
      </div>

      <div className="flex flex-1 flex-col overflow-auto">
        {loading ? (
          <div className="flex flex-1 items-center justify-center py-16">
            <p className="text-sm text-[var(--c-text-muted)]">Loading...</p>
          </div>
        ) : filtered.length === 0 ? (
          <EmptyState icon={<Building2 size={28} />} message="No organizations found" />
        ) : (
          <table className="w-full text-left text-sm">
            <thead>
              <tr className="border-b border-[var(--c-border-console)]">
                <th className="whitespace-nowrap px-6 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">Name</th>
                <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">Slug</th>
                <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">Type</th>
                <th className="whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]">Created At</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((org) => (
                <tr
                  key={org.id}
                  className="border-b border-[var(--c-border-console)] transition-colors hover:bg-[var(--c-bg-sub)]"
                >
                  <td className="whitespace-nowrap px-6 py-2.5 text-[var(--c-text-primary)]">
                    <span className="text-sm font-medium">{org.name}</span>
                  </td>
                  <td className="whitespace-nowrap px-4 py-2.5">
                    <span className="font-mono text-xs text-[var(--c-text-muted)]">{org.slug}</span>
                  </td>
                  <td className="whitespace-nowrap px-4 py-2.5">
                    <span
                      className={[
                        'rounded px-1.5 py-0.5 text-xs font-medium',
                        org.type === 'personal'
                          ? 'bg-[var(--c-bg-tag)] text-[var(--c-text-muted)]'
                          : 'bg-[var(--c-accent-soft,var(--c-bg-tag))] text-[var(--c-accent)]',
                      ].join(' ')}
                    >
                      {org.type}
                    </span>
                  </td>
                  <td className="whitespace-nowrap px-4 py-2.5 text-[var(--c-text-muted)]">
                    <span className="text-xs tabular-nums">
                      {new Date(org.created_at).toLocaleString()}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
