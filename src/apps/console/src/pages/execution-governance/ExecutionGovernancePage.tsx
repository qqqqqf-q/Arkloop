import { useCallback, useEffect, useState } from 'react'
import { Link, useOutletContext } from 'react-router-dom'
import { Loader2 } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { Badge } from '../../components/Badge'
import { useToast } from '../../components/useToast'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import {
  getExecutionGovernance,
  type ExecutionGovernanceAgentConfigSummary,
  type ExecutionGovernancePersona,
  type ExecutionGovernanceResponse,
  type ExecutionGovernanceToolSoftLimit,
} from '../../api/execution-governance'

const inputCls =
  'w-full rounded-md border border-[var(--c-border-console)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none focus:border-[var(--c-border-focus)]'
const sectionCls = 'rounded-lg border border-[var(--c-border-console)] bg-[var(--c-bg-card)] p-5'
const linkCls = 'text-xs text-[var(--c-text-muted)] underline hover:opacity-70'

function sourceVariant(source: string): 'success' | 'warning' | 'neutral' {
  switch (source) {
    case 'org_db':
    case 'org_default':
      return 'success'
    case 'env':
    case 'persona_binding':
      return 'warning'
    default:
      return 'neutral'
  }
}

function fallbackValue(value: string | null | undefined, fallback: string) {
  return value?.trim() ? value : fallback
}

function formatAgentConfig(config?: ExecutionGovernanceAgentConfigSummary, fallback?: string) {
  if (!config) return fallback ?? '--'
  return config.model ? `${config.name} · ${config.model}` : config.name
}

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

export function ExecutionGovernancePage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.executionGovernance

  const [loading, setLoading] = useState(true)
  const [draftOrgId, setDraftOrgId] = useState('')
  const [activeOrgId, setActiveOrgId] = useState('')
  const [data, setData] = useState<ExecutionGovernanceResponse | null>(null)

  const load = useCallback(async (orgId?: string) => {
    setLoading(true)
    try {
      const resp = await getExecutionGovernance(accessToken, orgId)
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

  const hasOrgScope = activeOrgId.trim() !== ''
  const emptyValue = tc.defaultEmpty
  const limits = data?.limits ?? []
  const agentConfigs = data?.agent_configs ?? []
  const personas = data?.personas ?? []

  const applyOrgScope = useCallback(() => {
    const nextOrgId = draftOrgId.trim()
    setActiveOrgId(nextOrgId)
    void load(nextOrgId || undefined)
  }, [draftOrgId, load])

  const resetOrgScope = useCallback(() => {
    setDraftOrgId('')
    setActiveOrgId('')
    void load(undefined)
  }, [load])

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
                    {hasOrgScope ? tc.filterActive(activeOrgId) : tc.filterPlatformOnly}
                  </p>
                </div>
                <div className="flex w-full flex-col gap-2 lg:w-[520px]">
                  <label className="text-xs font-medium text-[var(--c-text-secondary)]">{tc.fieldOrgId}</label>
                  <div className="flex gap-2">
                    <input
                      type="text"
                      value={draftOrgId}
                      onChange={(e) => setDraftOrgId(e.target.value)}
                      className={inputCls}
                      placeholder={tc.fieldOrgIdPlaceholder}
                    />
                    <button
                      onClick={applyOrgScope}
                      className="rounded-md bg-[var(--c-bg-tag)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-sub)]"
                    >
                      {tc.apply}
                    </button>
                    <button
                      onClick={resetOrgScope}
                      className="rounded-md border border-[var(--c-border-console)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-sub)]"
                    >
                      {tc.reset}
                    </button>
                  </div>
                </div>
              </div>
            </section>

            <section className={sectionCls}>
              <div className="mb-4 flex items-center justify-between gap-3">
                <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.limitsTitle}</h3>
                <Link to="/entitlements" className={linkCls}>{tc.gotoEntitlements}</Link>
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
                          <div className="font-mono text-xs text-[var(--c-text-primary)]">{item.key}</div>
                          <div className="mt-1 text-xs text-[var(--c-text-muted)]">{item.description}</div>
                          <div className="mt-1 text-[11px] text-[var(--c-text-muted)]">
                            {item.env_keys.join(', ') || emptyValue}
                          </div>
                        </td>
                        <td className="py-3 pr-4 text-[var(--c-text-primary)]">{item.effective.value}</td>
                        <td className="py-3 pr-4">
                          <Badge variant={sourceVariant(item.effective.source)}>{item.effective.source}</Badge>
                        </td>
                        <td className="py-3 text-xs text-[var(--c-text-secondary)]">
                          <div>{tc.layerEnv}: {fallbackValue(item.layers.env, emptyValue)}</div>
                          <div>{tc.layerOrg}: {fallbackValue(item.layers.org_db, emptyValue)}</div>
                          <div>{tc.layerPlatform}: {fallbackValue(item.layers.platform_db, emptyValue)}</div>
                          <div>{tc.layerDefault}: {item.layers.default}</div>
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
                  <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.agentConfigsTitle}</h3>
                  <p className="mt-1 text-xs text-[var(--c-text-muted)]">{tc.agentConfigsHint}</p>
                </div>
                <Link to="/agent-configs" className={linkCls}>{tc.gotoAgentConfigs}</Link>
              </div>
              {!hasOrgScope ? (
                <p className="text-xs text-[var(--c-text-muted)]">{tc.personasPlatformOnly}</p>
              ) : (
                <>
                  <div className="grid gap-4 md:grid-cols-2">
                    <div className="rounded-md border border-[var(--c-border-console)] bg-[var(--c-bg-sub)] p-4">
                      <div className="mb-1 text-xs text-[var(--c-text-muted)]">{tc.orgDefaultTitle}</div>
                      <div className="text-sm text-[var(--c-text-primary)]">{formatAgentConfig(data?.agent_config_defaults.org_default, tc.defaultEmpty)}</div>
                    </div>
                    <div className="rounded-md border border-[var(--c-border-console)] bg-[var(--c-bg-sub)] p-4">
                      <div className="mb-1 text-xs text-[var(--c-text-muted)]">{tc.platformDefaultTitle}</div>
                      <div className="text-sm text-[var(--c-text-primary)]">{formatAgentConfig(data?.agent_config_defaults.platform_default, tc.defaultEmpty)}</div>
                    </div>
                  </div>
                  <div className="mt-4 overflow-x-auto">
                    <table className="min-w-full text-left text-sm">
                      <thead className="text-xs text-[var(--c-text-muted)]">
                        <tr>
                          <th className="pb-2 pr-4 font-medium">{tc.colAgentConfig}</th>
                          <th className="pb-2 pr-4 font-medium">{tc.colScope}</th>
                          <th className="pb-2 pr-4 font-medium">{tc.colProject}</th>
                          <th className="pb-2 pr-4 font-medium">{tc.colModel}</th>
                          <th className="pb-2 pr-4 font-medium">{t.pages.agentConfigs.colMaxOutputTokens}</th>
                          <th className="pb-2 font-medium">{tc.colReasoningMode}</th>
                        </tr>
                      </thead>
                      <tbody>
                        {agentConfigs.length === 0 ? (
                          <tr>
                            <td colSpan={6} className="py-4 text-xs text-[var(--c-text-muted)]">{tc.agentConfigEmpty}</td>
                          </tr>
                        ) : agentConfigs.map((item) => (
                          <tr key={item.id} className="border-t border-[var(--c-border-console)]">
                            <td className="py-3 pr-4">
                              <div className="flex items-center gap-2">
                                <span className="text-[var(--c-text-primary)]">{item.name}</span>
                                {item.is_default && <Badge>{tc.defaultBadge}</Badge>}
                              </div>
                            </td>
                            <td className="py-3 pr-4 text-xs text-[var(--c-text-secondary)]">{item.scope}</td>
                            <td className="py-3 pr-4 text-xs text-[var(--c-text-secondary)]">{item.project_id ?? emptyValue}</td>
                            <td className="py-3 pr-4 text-xs text-[var(--c-text-secondary)]">{item.model ?? emptyValue}</td>
                            <td className="py-3 pr-4 text-xs text-[var(--c-text-secondary)]">{item.max_output_tokens ?? emptyValue}</td>
                            <td className="py-3 text-xs text-[var(--c-text-secondary)]">{item.reasoning_mode || emptyValue}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </>
              )}
            </section>

            <section className={sectionCls}>
              <div className="mb-4 flex items-center justify-between gap-3">
                <div>
                  <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.personasTitle}</h3>
                  <p className="mt-1 text-xs text-[var(--c-text-muted)]">{tc.personasHint}</p>
                </div>
                <Link to="/personas" className={linkCls}>{tc.gotoPersonas}</Link>
              </div>
              {!hasOrgScope ? (
                <p className="text-xs text-[var(--c-text-muted)]">{tc.personasPlatformOnly}</p>
              ) : (
                <div className="overflow-x-auto">
                  <table className="min-w-full text-left text-sm">
                    <thead className="text-xs text-[var(--c-text-muted)]">
                      <tr>
                        <th className="pb-2 pr-4 font-medium">{tc.colPersona}</th>
                        <th className="pb-2 pr-4 font-medium">{tc.colRequested}</th>
                        <th className="pb-2 pr-4 font-medium">{tc.colEffectiveBudget}</th>
                        <th className="pb-2 pr-4 font-medium">{tc.colResolvedConfig}</th>
                        <th className="pb-2 font-medium">{tc.colSoftLimits}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {personas.length === 0 ? (
                        <tr>
                          <td colSpan={5} className="py-4 text-xs text-[var(--c-text-muted)]">{tc.personaEmpty}</td>
                        </tr>
                      ) : personas.map((item) => (
                        <tr key={item.id} className="border-t border-[var(--c-border-console)] align-top">
                          <td className="py-3 pr-4">
                            <div className="flex items-center gap-2">
                              <span className="font-medium text-[var(--c-text-primary)]">{item.display_name}</span>
                              <Badge variant={item.source === 'custom' ? 'success' : 'neutral'}>{item.source}</Badge>
                            </div>
                            <div className="mt-1 font-mono text-xs text-[var(--c-text-muted)]">{item.persona_key}</div>
                            {item.preferred_credential && (
                              <div className="mt-1 text-xs text-[var(--c-text-muted)]">
                                {t.pages.personas.fieldPreferredCredential}: {item.preferred_credential}
                              </div>
                            )}
                          </td>
                          <td className="py-3 pr-4 text-xs text-[var(--c-text-secondary)]">{formatRequestedBudget(item, emptyValue)}</td>
                          <td className="py-3 pr-4 text-xs text-[var(--c-text-secondary)]">{formatEffectiveBudget(item, emptyValue)}</td>
                          <td className="py-3 pr-4 text-xs text-[var(--c-text-secondary)]">
                            <div>{tc.boundAgentConfig}: {item.agent_config_name ?? emptyValue}</div>
                            <div className="mt-1 flex items-center gap-2">
                              <span>{formatAgentConfig(item.effective.resolved_agent_config.config, emptyValue)}</span>
                              <Badge variant={sourceVariant(item.effective.resolved_agent_config.source)}>
                                {item.effective.resolved_agent_config.source}
                              </Badge>
                            </div>
                            {item.effective.reasoning_mode && (
                              <div className="mt-1">
                                {tc.labelReasoningMode}: {item.effective.reasoning_mode}
                              </div>
                            )}
                          </td>
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
                  <Link to="/agent-configs" className={linkCls}>{tc.gotoAgentConfigs}</Link>
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
