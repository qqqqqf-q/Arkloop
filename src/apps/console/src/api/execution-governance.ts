import { apiFetch } from './client'

export type ExecutionGovernanceLimit = {
  key: string
  type: string
  scope: string
  description: string
  env_keys: string[]
  effective: {
    value: string
    source: string
  }
  layers: {
    env: string | null
    org_db: string | null
    platform_db: string | null
    default: string
  }
}

export type ExecutionGovernanceAgentConfigSummary = {
  id: string
  name: string
  scope: string
  is_default: boolean
  project_id?: string
  persona_id?: string
  model?: string
  max_output_tokens?: number
  reasoning_mode?: string
  prompt_cache_control?: string
  tool_policy?: string
}

export type ExecutionGovernanceResolvedAgentConfig = {
  source: string
  config?: ExecutionGovernanceAgentConfigSummary
}

export type ExecutionGovernanceToolSoftLimit = {
  max_continuations?: number
  max_yield_time_ms?: number
  max_output_bytes?: number
}

export type ExecutionGovernanceRequestedBudgets = {
  reasoning_iterations?: number
  tool_continuation_budget?: number
  max_output_tokens?: number
  tool_timeout_ms?: number
  tool_budget?: Record<string, unknown>
  per_tool_soft_limits?: Record<string, ExecutionGovernanceToolSoftLimit>
  temperature?: number
  top_p?: number
}

export type ExecutionGovernancePersona = {
  id: string
  source: string
  persona_key: string
  version: string
  display_name: string
  preferred_credential?: string
  agent_config_name?: string
  requested: ExecutionGovernanceRequestedBudgets
  effective: {
    resolved_agent_config: ExecutionGovernanceResolvedAgentConfig
    reasoning_iterations: number
    tool_continuation_budget: number
    max_output_tokens?: number
    temperature?: number
    top_p?: number
    reasoning_mode?: string
    per_tool_soft_limits?: Record<string, ExecutionGovernanceToolSoftLimit>
  }
}

export type ExecutionGovernanceResponse = {
  limits: ExecutionGovernanceLimit[]
  agent_config_defaults: {
    org_default?: ExecutionGovernanceAgentConfigSummary
    platform_default?: ExecutionGovernanceAgentConfigSummary
  }
  agent_configs: ExecutionGovernanceAgentConfigSummary[]
  personas: ExecutionGovernancePersona[]
}

export async function getExecutionGovernance(
  accessToken: string,
  orgId?: string,
): Promise<ExecutionGovernanceResponse> {
  const qs = new URLSearchParams()
  if (orgId?.trim()) {
    qs.set('org_id', orgId.trim())
  }
  const suffix = qs.toString() ? `?${qs.toString()}` : ''
  return apiFetch<ExecutionGovernanceResponse>(`/v1/admin/execution-governance${suffix}`, { accessToken })
}
