import { RefreshCw } from 'lucide-react'
import { PageHeader } from '../../components/PageHeader'
import { useLocale } from '../../contexts/LocaleContext'
import { useProject } from '../../contexts/ProjectContext'

function formatProjectMeta(isDefault: boolean, visibility: string, t: ReturnType<typeof useLocale>['t']) {
  const parts = [visibility]
  if (isDefault) {
    parts.unshift(t.pages.projects.defaultBadge)
  }
  return parts.join(' · ')
}

export function ProjectsPage() {
  const { t } = useLocale()
  const { currentProjectId, loading, projects, reload, selectProject } = useProject()
  const tc = t.pages.projects

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader
        title={tc.title}
        actions={(
          <button
            type="button"
            onClick={() => { void reload() }}
            className="inline-flex items-center gap-2 rounded-lg border border-[var(--c-border)] px-3 py-1.5 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
          >
            <RefreshCw size={14} />
            {tc.refresh}
          </button>
        )}
      />

      <div className="flex flex-1 flex-col gap-4 overflow-auto p-6">
        <section className="rounded-2xl border border-[var(--c-border)] bg-[var(--c-bg-card)] p-5">
          <div className="text-xs uppercase tracking-[0.18em] text-[var(--c-text-muted)]">
            {tc.currentLabel}
          </div>
          <div className="mt-2 text-lg font-medium text-[var(--c-text-primary)]">
            {projects.find((item) => item.id === currentProjectId)?.name ?? tc.empty}
          </div>
          <div className="mt-1 text-sm text-[var(--c-text-muted)]">
            {tc.currentHint}
          </div>
        </section>

        <section className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {loading && projects.length === 0 ? (
            <div className="rounded-2xl border border-[var(--c-border)] bg-[var(--c-bg-card)] p-5 text-sm text-[var(--c-text-muted)]">
              {tc.loading}
            </div>
          ) : projects.length === 0 ? (
            <div className="rounded-2xl border border-dashed border-[var(--c-border)] bg-[var(--c-bg-card)] p-5 text-sm text-[var(--c-text-muted)]">
              {tc.empty}
            </div>
          ) : (
            projects.map((project) => {
              const active = project.id === currentProjectId
              return (
                <button
                  key={project.id}
                  type="button"
                  onClick={() => selectProject(project.id)}
                  className={[
                    'rounded-2xl border px-5 py-4 text-left transition-colors',
                    active
                      ? 'border-[var(--c-border-focus)] bg-[var(--c-bg-card)]'
                      : 'border-[var(--c-border)] bg-[var(--c-bg-card)] hover:border-[var(--c-border-focus)]',
                  ].join(' ')}
                >
                  <div className="flex items-start justify-between gap-3">
                    <div>
                      <div className="text-sm font-medium text-[var(--c-text-primary)]">{project.name}</div>
                      <div className="mt-1 text-xs text-[var(--c-text-muted)]">
                        {formatProjectMeta(project.is_default, project.visibility, t)}
                      </div>
                    </div>
                    {active && (
                      <span className="rounded-full bg-[var(--c-bg-tag)] px-2 py-0.5 text-[11px] text-[var(--c-text-secondary)]">
                        {tc.activeBadge}
                      </span>
                    )}
                  </div>
                  <div className="mt-3 text-xs text-[var(--c-text-muted)]">
                    {project.description?.trim() || tc.noDescription}
                  </div>
                </button>
              )
            })
          )}
        </section>
      </div>
    </div>
  )
}
