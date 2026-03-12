import { useCallback, useEffect, useState } from 'react'
import { Link, useOutletContext } from 'react-router-dom'
import { Loader2 } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { Badge } from '../../components/Badge'
import { useToast } from '@arkloop/shared'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import {
  getExecutionGovernance,
  type ExecutionGovernancePersona,
  type ExecutionGovernanceResponse,
  type ExecutionGovernanceToolSoftLimit,
} from '../../api/execution-governance'

const inputCls =
  'w-full rounded-md border border-[var(--c-border-console)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none focus:border-[var(--c-border-focus)]'
const sectionCls = 'rounded-lg border border-[var(--c-border-console)] bg-[var(--c-bg-card)] p-5'
const linkCls = 'text-xs text-[var(--c-text-muted)] underline hover:opacity-70'

function formatSoftLimit(limit?: ExecutionGovernanceToolSoftLimit, fallback = '--') {
  if (!limit) return fallback
  const parts: string[] = []
  if (limit.max_output_bytes != null) parts.push(`output=${limit.max_output_bytes}`)
  if (limit.max_continuations != null) parts.push(`continue=${limit.max_continuations}`)
  if (limit.max_yield_time_ms != null) parts.push(`yield=${limit.max_yield_time_ms}`)
  return parts.length > 0 ? parts.join(' · ') : fallback
}

function formatRequestedBudget(persona: ExecutionGovernancePersona, fallback: string) {
  const requested = persona.requested
  const parts: string[] = []
  if (requested.reasoning_iterations != null) parts.push(`reasoning=${requested.reasoning_iterations}`)
  if (requested.tool_continuation_budget != null) parts.push(`continue=${requested.tool_continuation_budget}`)
  if (requested.max_output_tokens != null) parts.push(`max=${requested.max_output_tokens}`)
  if (requested.temperature != null) parts.push(`temp=${requested.temperature}`)
  if (requested.top_p != null) parts.push(`top_p=${requested.top_p}`)
  return parts.length > 0 ? parts.join(' / ') : fallback
}

function formatEffectiveBudget(persona: ExecutionGovernancePersona, fallback: string) {
  const effective = persona.effective
  const parts = [
    `reasoning=${effective.reasoning_iterations}`,
    `continue=${effective.tool_continuation_budget}`,
  ]
  if (effective.max_output_tokens != null) parts.push(`max=${effective.max_output_tokens}`)
  if (effective.temperature != null) parts.push(`temp=${effective.temperature}`)
  if (effective.top_p != null) parts.push(`top_p=${effective.top_p}`)
  if (effective.reasoning_mode) parts.push(`mode=${effective.reasoning_mode}`)
  return parts.length > 0 ? parts.join(' / ') : fallback
}

function formatEffectiveSoftLimits(persona: ExecutionGovernancePersona, fallback: string) {
  const limits = persona.effective.per_tool_soft_limits
  const parts: string[] = []
  if (limits?.write_stdin) parts.push(`write_stdin: ${formatSoftLimit(limits.write_stdin, fallback)}`)
  if (limits?.exec_command) parts.push(`exec_command: ${formatSoftLimit(limits.exec_command, fallback)}`)
  return parts.length > 0 ? parts.join(' / ') : fallback
}

function sourceVariant(source: string): 'success' | 'neutral' {
  return source === 'custom' ? 'success' : 'neutral'
}

export function ExecutionGovernancePage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.executionGovernance

  const [loading, setLoading] = useState(true)
  const [draftProjectId, setDraftProjectId] = useState('')
  const [activeProjectId, setActiveProjectId] = useState('')
  const [data, setData] = useState<ExecutionGovernanceResponse | null>(null)

  const load = useCallback(async (projectId?: string) => {
    setLoading(true)
    try {
      const resp = await getExecutionGovernance(accessToken, projectId)
      setData(resp)
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc.toastLoadFailed])

  useEffect(() => {
    void load()
  }, [load])

  const applyProjectScope = useCallback(() => {
    const nextProjectId = draftProjectId.trim()
    setActiveProjectId(nextProjectId)
    void load(nextProjectId || undefined)
  }, [draftProjectId, load])

  const resetProjectScope = useCallback(() => {
    setDraftProjectId('')
    setActiveProjectId('')
    void load(undefined)
  }, [load])

  const emptyValue = tc.defaultEmpty
  const limits = data?.limits ?? []
  const personas = data?.personas ?? []
  const hasProjectScope = activeProjectId.trim() !== ''

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tc.title} />

      <div className="flex-1 overflow-y-auto p-6">
        {loading ? (
          <div className="flex items-center justify-center py-16">
            <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
          </div>
        ) : (
          <div className="space-y-6">
            <section className={sectionCls}>
              <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
                <div>
                  <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.filterTitle}</h3>
                  <p className="mt-1 text-xs text-[var(--c-text-muted)]">
                    {hasProjectScope ? tc.filterActive(activeProjectId) : tc.filterPlatformOnly}
                  </p>
                </div>
                <div className="flex w-full flex-col gap-3 sm:flex-row lg:w-auto">
                  <input
                    className={inputCls}
                    placeholder={tc.fieldAccountIdPlaceholder}
                    value={draftProjectId}
                    onChange={(e) => setDraftProjectId(e.target.value)}
                  />
                  <div className="flex gap-2">
                    <button
                      onClick={applyProjectScope}
                      className="rounded-md bg-[var(--c-bg-tag)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
                    >
                      {tc.apply}
                    </button>
                    <button
                      onClick={resetProjectScope}
                      className="rounded-md border border-[var(--c-border-console)] px-3 py-1.5 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
                    >
                      {tc.reset}
                    </button>
                  </div>
                </div>
              </div>
            </section>

            <section className={sectionCls}>
              <div className="mb-4 flex items-center justify-between gap-3">
                <div>
                  <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.limitsTitle}</h3>
                </div>
                <div className="flex items-center gap-3">
                  <Link to="/entitlements" className={linkCls}>{tc.gotoEntitlements}</Link>
                  <Link to="/personas" className={linkCls}>{tc.gotoPersonas}</Link>
                </div>
              </div>
              <div className="overflow-x-auto">
                <table className="min-w-full text-left text-sm">
                  <thead className="text-xs text-[var(--c-text-muted)]">
                    <tr>
                      <th className="pb-2 pr-4 font-medium">{tc.colLimit}</th>
                      <th className="pb-2 pr-4 font-medium">{tc.colEffective}</th>
                      <th className="pb-2 pr-4 font-medium">{tc.colSource}</th>
                      <th className="pb-2 font-medium">{tc.colLayers}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {limits.map((item) => (
                      <tr key={item.key} className="border-t border-[var(--c-border-console)] align-top">
                        <td className="py-3 pr-4">
                          <div className="font-medium text-[var(--c-text-primary)]">{item.key}</div>
                          <div className="mt-1 text-xs text-[var(--c-text-muted)]">{item.description}</div>
                        </td>
                        <td className="py-3 pr-4 text-xs text-[var(--c-text-secondary)]">{item.effective.value || emptyValue}</td>
                        <td className="py-3 pr-4 text-xs text-[var(--c-text-secondary)]">{item.effective.source || emptyValue}</td>
                        <td className="py-3 text-xs text-[var(--c-text-secondary)]">
                          <div>{tc.layerEnv}: {item.layers.env ?? emptyValue}</div>
                          <div>{tc.layerAccount}: {item.layers.project_db ?? emptyValue}</div>
                          <div>{tc.layerPlatform}: {item.layers.platform_db ?? emptyValue}</div>
                          <div>{tc.layerDefault}: {item.layers.default || emptyValue}</div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </section>

            <section className={sectionCls}>
              <div className="mb-4 flex items-center justify-between gap-3">
                <div>
                  <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.titleSummarizerTitle}</h3>
                </div>
                <Link to="/title-summarizer" className={linkCls}>{tc.gotoTitleSummarizer}</Link>
              </div>
              <div className="text-sm text-[var(--c-text-primary)]">
                {data?.title_summarizer_model?.trim() || emptyValue}
              </div>
            </section>

            <section className={sectionCls}>
              <div className="mb-4 flex items-center justify-between gap-3">
                <div>
                  <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.personasTitle}</h3>
                  <p className="mt-1 text-xs text-[var(--c-text-muted)]">{tc.personasHint}</p>
                </div>
                <Link to="/personas" className={linkCls}>{tc.gotoPersonas}</Link>
              </div>
              {!hasProjectScope ? (
                <p className="text-xs text-[var(--c-text-muted)]">{tc.personasPlatformOnly}</p>
              ) : (
                <div className="overflow-x-auto">
                  <table className="min-w-full text-left text-sm">
                    <thead className="text-xs text-[var(--c-text-muted)]">
                      <tr>
                        <th className="pb-2 pr-4 font-medium">{tc.colPersona}</th>
                        <th className="pb-2 pr-4 font-medium">{tc.colRequested}</th>
                        <th className="pb-2 pr-4 font-medium">{tc.colEffectiveBudget}</th>
                        <th className="pb-2 pr-4 font-medium">{tc.colModel}</th>
                        <th className="pb-2 pr-4 font-medium">{tc.colReasoningMode}</th>
                        <th className="pb-2 pr-4 font-medium">{tc.colPromptCacheControl}</th>
                        <th className="pb-2 font-medium">{tc.colSoftLimits}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {personas.length === 0 ? (
                        <tr>
                          <td colSpan={7} className="py-4 text-xs text-[var(--c-text-muted)]">{tc.personaEmpty}</td>
                        </tr>
                      ) : personas.map((item) => (
                        <tr key={item.id} className="border-t border-[var(--c-border-console)] align-top">
                          <td className="py-3 pr-4">
                            <div className="flex items-center gap-2">
                              <span className="font-medium text-[var(--c-text-primary)]">{item.display_name}</span>
                              <Badge variant={sourceVariant(item.source)}>{item.source}</Badge>
                            </div>
                            <div className="mt-1 font-mono text-xs text-[var(--c-text-muted)]">{item.persona_key}</div>
                            {item.preferred_credential && (
                              <div className="mt-1 text-xs text-[var(--c-text-muted)]">
                                {t.pages.agents.fieldPreferredCredential}: {item.preferred_credential}
                              </div>
                            )}
                          </td>
                          <td className="py-3 pr-4 text-xs text-[var(--c-text-secondary)]">{formatRequestedBudget(item, emptyValue)}</td>
                          <td className="py-3 pr-4 text-xs text-[var(--c-text-secondary)]">{formatEffectiveBudget(item, emptyValue)}</td>
                          <td className="py-3 pr-4 font-mono text-xs text-[var(--c-text-secondary)]">{item.model ?? emptyValue}</td>
                          <td className="py-3 pr-4 text-xs text-[var(--c-text-secondary)]">{item.reasoning_mode || item.effective.reasoning_mode || emptyValue}</td>
                          <td className="py-3 pr-4 text-xs text-[var(--c-text-secondary)]">{item.prompt_cache_control || emptyValue}</td>
                          <td className="py-3 text-xs text-[var(--c-text-secondary)]">{formatEffectiveSoftLimits(item, emptyValue)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </section>

            <section className={sectionCls}>
              <div className="mb-3 flex items-center justify-between gap-3">
                <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.rulesTitle}</h3>
                <div className="flex items-center gap-3">
                  <Link to="/entitlements" className={linkCls}>{tc.gotoEntitlements}</Link>
                  <Link to="/personas" className={linkCls}>{tc.gotoPersonas}</Link>
                </div>
              </div>
              <ul className="space-y-2 text-xs text-[var(--c-text-secondary)]">
                <li>{tc.ruleSources}</li>
                <li>{tc.ruleClamp}</li>
              </ul>
            </section>
          </div>
        )}
      </div>
    </div>
  )
}
