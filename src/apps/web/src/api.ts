export {
  TRACE_ID_HEADER,
  ApiError,
  isApiError,
  apiFetch,
  setUnauthenticatedHandler,
  setAccessTokenHandler,
  refreshAccessToken,
} from '@arkloop/shared/api'

export type { LoginRequest, LoginResponse } from '@arkloop/shared/api/types'

import {
  apiFetch,
  ApiError,
  TRACE_ID_HEADER,
  buildUrl,
  apiBaseUrl,
  readJsonSafely,
} from '@arkloop/shared/api'
import type { ErrorEnvelope } from '@arkloop/shared/api'
import type { LoginRequest, LoginResponse } from '@arkloop/shared/api/types'
import { parseSSEChunk, type RunEvent } from './sse'

export type RegisterRequest = {
  login: string
  password: string
  email: string
  invite_code?: string
  locale?: string
  cf_turnstile_token?: string
}

export type RegisterResponse = {
  user_id: string
  token_type: string
  access_token: string
  warning?: string
}

export type RegistrationModeResponse = {
  mode: 'invite_only' | 'open'
}

export type ResolveIdentityRequest = {
  identity: string
  cf_turnstile_token?: string
}

export type ResolveIdentityResponse =
  | {
      next_step: 'password'
      flow_token: string
      masked_email?: string
      otp_available: boolean
    }
  | {
      next_step: 'register'
      invite_required: boolean
      prefill?: {
        login?: string
        email?: string
      }
    }

export type MeResponse = {
  id: string
  login: string
  username: string
  email?: string
  email_verified: boolean
  email_verification_required: boolean
}

export type SkillReference = {
  skill_key: string
  version: string
}

export type SkillPackageResponse = {
  skill_key: string
  version: string
  display_name: string
  description?: string
  instruction_path: string
  manifest_key: string
  bundle_key: string
  platforms?: string[]
  is_active: boolean
}

export type InstalledSkill = SkillPackageResponse & {
  profile_ref?: string
  workspace_ref?: string
  source?: 'official' | 'custom' | 'github'
  created_at?: string
  updated_at?: string
}

export type DefaultSkillsResponse = {
  items: InstalledSkill[]
}

export type MarketSkill = {
  skill_key: string
  version?: string
  display_name: string
  description?: string
  source: 'official'
  updated_at?: string
  detail_url?: string
  repository_url?: string
  installed: boolean
  enabled_by_default: boolean
}

export type MarketSkillsResponse = {
  items: MarketSkill[]
}

export type SkillImportCandidate = {
  path: string
  skill_key?: string
  version?: string
  display_name?: string
}

export type GitHubImportResponse = {
  skill: SkillPackageResponse
  candidates?: SkillImportCandidate[]
}

export type Persona = {
  id: string
  org_id: string | null
  scope: 'org' | 'platform'
  source?: 'builtin' | 'custom'
  persona_key: string
  version: string
  display_name: string
  description?: string
  user_selectable: boolean
  selector_name?: string
  selector_order?: number
  prompt_md: string
  tool_allowlist: string[]
  tool_denylist: string[]
  budgets: Record<string, unknown>
  is_active: boolean
  created_at: string
  preferred_credential?: string
  model?: string
  reasoning_mode: string
  prompt_cache_control: string
  executor_type: string
  executor_config: Record<string, unknown>
}

export type SelectablePersona = {
  persona_key: string
  selector_name: string
  selector_order: number
}

export async function login(req: LoginRequest): Promise<LoginResponse> {
  return await apiFetch<LoginResponse>('/v1/auth/login', {
    method: 'POST',
    body: JSON.stringify(req),
  })
}

export async function register(req: RegisterRequest): Promise<RegisterResponse> {
  return await apiFetch<RegisterResponse>('/v1/auth/register', {
    method: 'POST',
    body: JSON.stringify(req),
  })
}

export async function getRegistrationMode(): Promise<RegistrationModeResponse> {
  return await apiFetch<RegistrationModeResponse>('/v1/auth/registration-mode', {
    method: 'GET',
  })
}

export async function resolveIdentity(req: ResolveIdentityRequest): Promise<ResolveIdentityResponse> {
  return await apiFetch<ResolveIdentityResponse>('/v1/auth/resolve', {
    method: 'POST',
    body: JSON.stringify(req),
  })
}

export async function sendResolvedEmailOTP(flowToken: string, cfTurnstileToken?: string): Promise<void> {
  await apiFetch<void>('/v1/auth/resolve/otp/send', {
    method: 'POST',
    body: JSON.stringify({ flow_token: flowToken, cf_turnstile_token: cfTurnstileToken }),
  })
}

export async function verifyResolvedEmailOTP(flowToken: string, code: string): Promise<LoginResponse> {
  return await apiFetch<LoginResponse>('/v1/auth/resolve/otp/verify', {
    method: 'POST',
    body: JSON.stringify({ flow_token: flowToken, code }),
  })
}

export async function getMe(accessToken: string): Promise<MeResponse> {
  return await apiFetch<MeResponse>('/v1/me', {
    method: 'GET',
    accessToken,
  })
}

export async function listInstalledSkills(accessToken: string): Promise<InstalledSkill[]> {
  const response = await apiFetch<{ items: InstalledSkill[] }>('/v1/profiles/me/skills', {
    method: 'GET',
    accessToken,
  })
  return response.items ?? []
}

export async function listDefaultSkills(accessToken: string): Promise<InstalledSkill[]> {
  const response = await apiFetch<DefaultSkillsResponse>('/v1/profiles/me/default-skills', {
    method: 'GET',
    accessToken,
  })
  return response.items ?? []
}

export async function replaceDefaultSkills(accessToken: string, skills: SkillReference[]): Promise<InstalledSkill[]> {
  const response = await apiFetch<DefaultSkillsResponse>('/v1/profiles/me/default-skills', {
    method: 'PUT',
    accessToken,
    body: JSON.stringify({ skills }),
  })
  return response.items ?? []
}

export async function searchMarketSkills(accessToken: string, query: string, officialOnly = false): Promise<MarketSkill[]> {
  const sp = new URLSearchParams()
  if (query.trim()) sp.set('q', query.trim())
  if (officialOnly) sp.set('official_only', 'true')
  const suffix = sp.toString() ? `?${sp.toString()}` : ''
  const response = await apiFetch<MarketSkillsResponse>(`/v1/market/skills${suffix}`, {
    method: 'GET',
    accessToken,
  })
  return response.items ?? []
}

export async function importSkillsMPSkill(
  accessToken: string,
  payload: { skill_key: string; detail_url: string; repository_url?: string },
): Promise<SkillPackageResponse> {
  return await apiFetch<SkillPackageResponse>('/v1/skill-packages/import/skillsmp', {
    method: 'POST',
    accessToken,
    body: JSON.stringify(payload),
  })
}

export async function importMarketSkill(
  accessToken: string,
  payload: { skill_key: string; detail_url: string; repository_url?: string },
): Promise<SkillPackageResponse> {
  return await importSkillsMPSkill(accessToken, payload)
}

export async function installSkill(accessToken: string, skill: SkillReference): Promise<void> {
  await apiFetch<void>('/v1/profiles/me/skills/install', {
    method: 'POST',
    accessToken,
    body: JSON.stringify(skill),
  })
}

export async function deleteSkill(accessToken: string, skill: SkillReference): Promise<void> {
  await apiFetch<void>(`/v1/profiles/me/skills/${encodeURIComponent(skill.skill_key)}/${encodeURIComponent(skill.version)}`, {
    method: 'DELETE',
    accessToken,
  })
}

export async function importSkillFromGitHub(
  accessToken: string,
  payload: { repository_url: string; ref?: string; candidate_path?: string },
): Promise<GitHubImportResponse> {
  return await apiFetch<GitHubImportResponse>('/v1/skill-packages/import/github', {
    method: 'POST',
    accessToken,
    body: JSON.stringify(payload),
  })
}

export async function importSkillFromUpload(
  accessToken: string,
  payload: { file: File; install_after_import?: boolean },
): Promise<SkillPackageResponse> {
  const body = new FormData()
  body.append('file', payload.file)
  if (payload.install_after_import) body.append('install_after_import', 'true')
  return await apiFetch<SkillPackageResponse>('/v1/skill-packages/import/upload', {
    method: 'POST',
    accessToken,
    body,
    headers: {},
  })
}

export async function listSelectablePersonas(accessToken: string): Promise<SelectablePersona[]> {
  const personas = await apiFetch<Persona[]>('/v1/me/selectable-personas', {
    method: 'GET',
    accessToken,
  })

  return personas
    .filter((persona) => persona.user_selectable)
    .map((persona) => ({
      persona_key: persona.persona_key,
      selector_name: (persona.selector_name ?? persona.display_name).trim() || persona.persona_key,
      selector_order: persona.selector_order ?? 99,
    }))
    .sort((left, right) => {
      if (left.selector_order !== right.selector_order) {
        return left.selector_order - right.selector_order
      }
      const byName = left.selector_name.localeCompare(right.selector_name)
      if (byName !== 0) return byName
      return left.persona_key.localeCompare(right.persona_key)
    })
}

export async function updateMe(accessToken: string, username: string): Promise<{ username: string }> {
  return await apiFetch<{ username: string }>('/v1/me', {
    method: 'PATCH',
    accessToken,
    body: JSON.stringify({ username }),
  })
}

export async function sendEmailVerification(accessToken: string): Promise<void> {
  await apiFetch<void>('/v1/auth/email/verify/send', {
    method: 'POST',
    accessToken,
  })
}

export async function confirmEmailVerification(token: string): Promise<{ ok: boolean }> {
  return await apiFetch<{ ok: boolean }>('/v1/auth/email/verify/confirm', {
    method: 'POST',
    body: JSON.stringify({ token }),
  })
}

export async function sendEmailOTP(email: string, cfTurnstileToken?: string): Promise<void> {
  await apiFetch<void>('/v1/auth/email/otp/send', {
    method: 'POST',
    body: JSON.stringify({ email, cf_turnstile_token: cfTurnstileToken }),
  })
}

export async function verifyEmailOTP(email: string, code: string): Promise<LoginResponse> {
  return await apiFetch<LoginResponse>('/v1/auth/email/otp/verify', {
    method: 'POST',
    body: JSON.stringify({ email, code }),
  })
}


export type LogoutResponse = {
  ok: boolean
}

export type CaptchaConfigResponse = {
  enabled: boolean
  site_key: string
}

export async function getCaptchaConfig(): Promise<CaptchaConfigResponse> {
  return await apiFetch<CaptchaConfigResponse>('/v1/auth/captcha-config')
}

export async function logout(accessToken: string): Promise<LogoutResponse> {
  return await apiFetch<LogoutResponse>('/v1/auth/logout', {
    method: 'POST',
    accessToken,
  })
}

// Threads API

export type CreateThreadRequest = {
  title?: string
  is_private?: boolean
  project_id?: string
}

export type ThreadResponse = {
  id: string
  org_id: string
  created_by_user_id: string
  title: string | null
  project_id: string
  created_at: string
  active_run_id: string | null
  is_private: boolean
  parent_thread_id?: string | null
}

export async function getThread(
  accessToken: string,
  threadId: string,
): Promise<ThreadResponse> {
  return await apiFetch<ThreadResponse>(`/v1/threads/${threadId}`, {
    method: 'GET',
    accessToken,
  })
}

export async function createThread(
  accessToken: string,
  req?: CreateThreadRequest,
): Promise<ThreadResponse> {
  return await apiFetch<ThreadResponse>('/v1/threads', {
    method: 'POST',
    accessToken,
    body: JSON.stringify(req ?? {}),
  })
}

export type ListThreadsRequest = {
  limit?: number
  before_created_at?: string
  before_id?: string
}

export async function listThreads(
  accessToken: string,
  req?: ListThreadsRequest,
): Promise<ThreadResponse[]> {
  const sp = new URLSearchParams()
  if (req?.limit) sp.set('limit', String(req.limit))
  if (req?.before_created_at) sp.set('before_created_at', req.before_created_at)
  if (req?.before_id) sp.set('before_id', req.before_id)
  const suffix = sp.toString() ? `?${sp.toString()}` : ''
  return await apiFetch<ThreadResponse[]>(`/v1/threads${suffix}`, {
    method: 'GET',
    accessToken,
  })
}

export async function searchThreads(
  accessToken: string,
  q: string,
  limit = 50,
): Promise<ThreadResponse[]> {
  const sp = new URLSearchParams({ q, limit: String(limit) })
  return await apiFetch<ThreadResponse[]>(`/v1/threads/search?${sp.toString()}`, {
    method: 'GET',
    accessToken,
  })
}

export async function listStarredThreadIds(accessToken: string): Promise<string[]> {
  return await apiFetch<string[]>('/v1/threads/starred', {
    method: 'GET',
    accessToken,
  })
}

export async function starThread(accessToken: string, threadId: string): Promise<void> {
  await apiFetch<void>(`/v1/threads/${threadId}:star`, {
    method: 'POST',
    accessToken,
  })
}

export async function unstarThread(accessToken: string, threadId: string): Promise<void> {
  await apiFetch<void>(`/v1/threads/${threadId}:star`, {
    method: 'DELETE',
    accessToken,
  })
}

export async function updateThreadTitle(
  accessToken: string,
  threadId: string,
  title: string,
): Promise<ThreadResponse> {
  return await apiFetch<ThreadResponse>(`/v1/threads/${threadId}`, {
    method: 'PATCH',
    accessToken,
    body: JSON.stringify({ title }),
  })
}

export async function deleteThread(accessToken: string, threadId: string): Promise<void> {
  await apiFetch<void>(`/v1/threads/${threadId}`, {
    method: 'DELETE',
    accessToken,
  })
}

export type ForkThreadResponse = ThreadResponse & {
  id_mapping?: Array<{ old_id: string; new_id: string }>
}

export async function forkThread(
  accessToken: string,
  threadId: string,
  messageId: string,
  isPrivate?: boolean,
): Promise<ForkThreadResponse> {
  const body: Record<string, unknown> = { message_id: messageId }
  if (isPrivate !== undefined) body.is_private = isPrivate
  return await apiFetch<ForkThreadResponse>(`/v1/threads/${threadId}:fork`, {
    method: 'POST',
    accessToken,
    body: JSON.stringify(body),
  })
}

// Messages API

export type MessageAttachmentRef = {
  key: string
  filename: string
  mime_type: string
  size: number
}

export type MessageContentPart =
  | { type: 'text'; text: string }
  | { type: 'image'; attachment: MessageAttachmentRef }
  | { type: 'file'; attachment: MessageAttachmentRef; extracted_text: string }

export type MessageContent = {
  parts: MessageContentPart[]
}

export type CreateMessageRequest = {
  content?: string
  content_json?: MessageContent
}

export type MessageResponse = {
  id: string
  org_id: string
  thread_id: string
  created_by_user_id: string
  role: string
  content: string
  content_json?: MessageContent
  created_at: string
  run_id?: string
}

export type UploadedThreadAttachment = {
  key: string
  filename: string
  mime_type: string
  size: number
  kind: 'image' | 'file'
  extracted_text?: string
}

export async function uploadThreadAttachment(
  accessToken: string,
  threadId: string,
  file: File,
): Promise<UploadedThreadAttachment> {
  const body = new FormData()
  body.append('file', file)
  return await apiFetch<UploadedThreadAttachment>(`/v1/threads/${threadId}/attachments`, {
    method: 'POST',
    accessToken,
    body,
    headers: {},
  })
}

export async function createMessage(
  accessToken: string,
  threadId: string,
  req: CreateMessageRequest,
): Promise<MessageResponse> {
  return await apiFetch<MessageResponse>(`/v1/threads/${threadId}/messages`, {
    method: 'POST',
    accessToken,
    body: JSON.stringify(req),
  })
}

export async function listMessages(
  accessToken: string,
  threadId: string,
  limit = 200,
): Promise<MessageResponse[]> {
  return await apiFetch<MessageResponse[]>(
    `/v1/threads/${threadId}/messages?limit=${limit}`,
    {
      method: 'GET',
      accessToken,
    },
  )
}

export async function editMessage(
  accessToken: string,
  threadId: string,
  messageId: string,
  content: string,
): Promise<CreateRunResponse> {
  return await apiFetch<CreateRunResponse>(`/v1/threads/${threadId}/messages/${messageId}`, {
    method: 'PATCH',
    accessToken,
    body: JSON.stringify({ content }),
  })
}

// Runs API

export type CreateRunResponse = {
  run_id: string
  trace_id: string
}

export async function createRun(
  accessToken: string,
  threadId: string,
  personaId?: string,
): Promise<CreateRunResponse> {
  return await apiFetch<CreateRunResponse>(`/v1/threads/${threadId}/runs`, {
    method: 'POST',
    accessToken,
    body: personaId ? JSON.stringify({ persona_id: personaId }) : undefined,
  })
}

export type ThreadRunResponse = {
  run_id: string
  status: 'running' | 'completed' | 'failed' | 'cancelled'
  created_at: string
}

export async function listThreadRuns(
  accessToken: string,
  threadId: string,
  limit = 50,
): Promise<ThreadRunResponse[]> {
  return await apiFetch<ThreadRunResponse[]>(
    `/v1/threads/${threadId}/runs?limit=${limit}`,
    {
      method: 'GET',
      accessToken,
    },
  )
}

export async function listRunEvents(
  accessToken: string,
  runId: string,
  options?: { afterSeq?: number; follow?: boolean },
): Promise<RunEvent[]> {
  const sp = new URLSearchParams()
  sp.set('follow', options?.follow === true ? 'true' : 'false')
  sp.set('after_seq', String(options?.afterSeq ?? 0))

  const response = await fetch(buildUrl(`/v1/runs/${runId}/events?${sp.toString()}`), {
    method: 'GET',
    headers: {
      Accept: 'text/event-stream',
      Authorization: `Bearer ${accessToken}`,
    },
  })

  if (!response.ok) {
    const headerTraceId = response.headers.get(TRACE_ID_HEADER) ?? undefined
    const payload = await readJsonSafely(response)
    if (payload && typeof payload === 'object') {
      const env = payload as ErrorEnvelope
      const traceId = typeof env.trace_id === 'string' ? env.trace_id : headerTraceId
      const code = typeof env.code === 'string' ? env.code : undefined
      const message =
        typeof env.message === 'string'
          ? env.message
          : `请求失败（HTTP ${response.status}）`
      throw new ApiError({
        status: response.status,
        message,
        code,
        traceId,
        details: env.details,
      })
    }
    throw new ApiError({
      status: response.status,
      message: `请求失败（HTTP ${response.status}）`,
      traceId: headerTraceId,
    })
  }

  const text = await response.text()
  if (text.trim() === '') return []

  const { events } = parseSSEChunk(text.endsWith('\n') ? text : `${text}\n`)
  const runEvents: RunEvent[] = []
  for (const event of events) {
    if (!event.data) continue
    try {
      runEvents.push(JSON.parse(event.data) as RunEvent)
    } catch {
      // ignore malformed item
    }
  }
  return runEvents
}

export type CancelRunResponse = {
  ok: boolean
}

export async function cancelRun(
  accessToken: string,
  runId: string,
): Promise<CancelRunResponse> {
  return await apiFetch<CancelRunResponse>(`/v1/runs/${runId}:cancel`, {
    method: 'POST',
    accessToken,
  })
}

export type ProvideInputResponse = {
  ok: boolean
}

export async function provideInput(
  accessToken: string,
  runId: string,
  content: string,
): Promise<ProvideInputResponse> {
  return await apiFetch<ProvideInputResponse>(`/v1/runs/${runId}/input`, {
    method: 'POST',
    body: JSON.stringify({ content }),
    accessToken,
  })
}

export type RetryThreadResponse = {
  run_id: string
  trace_id: string
}

export async function retryThread(
  accessToken: string,
  threadId: string,
): Promise<RetryThreadResponse> {
  return await apiFetch<RetryThreadResponse>(`/v1/threads/${threadId}:retry`, {
    method: 'POST',
    accessToken,
  })
}

// Credits API

export type CreditTransaction = {
  id: string
  org_id: string
  amount: number
  type: string
  reference_type?: string
  reference_id?: string
  note?: string
  thread_title?: string
  created_at: string
}

export type MeCreditsResponse = {
  balance: number
  transactions: CreditTransaction[]
}

export async function getMyCredits(
  accessToken: string,
  from?: string,
  to?: string,
): Promise<MeCreditsResponse> {
  const params = new URLSearchParams()
  if (from) params.set('from', from)
  if (to) params.set('to', to)
  const qs = params.size > 0 ? `?${params.toString()}` : ''
  return await apiFetch<MeCreditsResponse>(`/v1/me/credits${qs}`, {
    method: 'GET',
    accessToken,
  })
}

export type MeUsageSummary = {
  org_id: string
  year: number
  month: number
  total_input_tokens: number
  total_output_tokens: number
  total_cost_usd: number
  record_count: number
}

export async function getMyUsage(
  accessToken: string,
  year: number,
  month: number,
): Promise<MeUsageSummary> {
  return await apiFetch<MeUsageSummary>(`/v1/me/usage?year=${year}&month=${month}`, {
    method: 'GET',
    accessToken,
  })
}

export type RedeemCodeResponse = {
  code: string
  type: string
  value: string
}

export async function redeemCode(
  accessToken: string,
  code: string,
): Promise<RedeemCodeResponse> {
  return await apiFetch<RedeemCodeResponse>('/v1/me/redeem', {
    method: 'POST',
    accessToken,
    body: JSON.stringify({ code }),
  })
}

// Invite Code API

export type InviteCodeResponse = {
  id: string
  user_id: string
  code: string
  max_uses: number
  use_count: number
  is_active: boolean
  created_at: string
}

export async function getMyInviteCode(
  accessToken: string,
): Promise<InviteCodeResponse> {
  return await apiFetch<InviteCodeResponse>('/v1/me/invite-code', {
    method: 'GET',
    accessToken,
  })
}

export async function resetMyInviteCode(
  accessToken: string,
): Promise<InviteCodeResponse> {
  return await apiFetch<InviteCodeResponse>('/v1/me/invite-code/reset', {
    method: 'POST',
    accessToken,
  })
}

// Notifications API

export type NotificationItem = {
  id: string
  user_id: string
  org_id: string
  type: string
  title: string
  body: string
  payload: Record<string, unknown>
  read_at?: string
  created_at: string
}

export async function listNotifications(
  accessToken: string,
  opts?: { unreadOnly?: boolean; type?: string },
): Promise<{ data: NotificationItem[] }> {
  const params = new URLSearchParams()
  if (opts?.unreadOnly) params.set('unread_only', 'true')
  if (opts?.type) params.set('type', opts.type)
  const query = params.toString()
  return await apiFetch<{ data: NotificationItem[] }>(`/v1/notifications${query ? `?${query}` : ''}`, {
    method: 'GET',
    accessToken,
  })
}

export async function markAllNotificationsRead(
  accessToken: string,
): Promise<{ ok: boolean; count: number }> {
  return await apiFetch<{ ok: boolean; count: number }>('/v1/notifications', {
    method: 'PATCH',
    accessToken,
  })
}

export async function markNotificationRead(
  accessToken: string,
  id: string,
): Promise<{ ok: boolean }> {
  return await apiFetch<{ ok: boolean }>(`/v1/notifications/${id}`, {
    method: 'PATCH',
    accessToken,
  })
}

export async function transcribeAudio(
  accessToken: string,
  audioBlob: Blob,
  filename: string,
  language?: string,
): Promise<{ text: string }> {
  const form = new FormData()
  form.append('file', audioBlob, filename)
  if (language) form.append('language', language)

  const base = apiBaseUrl()
  const url = base ? `${base}/v1/asr/transcribe` : `/v1/asr/transcribe`

  const headers = new Headers()
  headers.set('Accept', 'application/json')
  headers.set('Authorization', `Bearer ${accessToken}`)

  const response = await fetch(url, { method: 'POST', body: form, headers })
  if (!response.ok) {
    const headerTraceId = response.headers.get(TRACE_ID_HEADER) ?? undefined
    const payload = await readJsonSafely(response)
    const env = payload && typeof payload === 'object' ? (payload as ErrorEnvelope) : null
    throw new ApiError({
      status: response.status,
      message: typeof env?.message === 'string' ? env.message : `转写失败（HTTP ${response.status}）`,
      traceId: headerTraceId,
    })
  }
  return response.json() as Promise<{ text: string }>
}

// Share API

export type ShareResponse = {
  id: string
  token: string
  url: string
  access_type: 'public' | 'password'
  password?: string
  live_update: boolean
  snapshot_turn_count: number
  created_at: string
}

export async function createThreadShare(
  accessToken: string,
  threadId: string,
  accessType: 'public' | 'password',
  password?: string,
  liveUpdate?: boolean,
): Promise<ShareResponse> {
  return await apiFetch<ShareResponse>(`/v1/threads/${threadId}:share`, {
    method: 'POST',
    accessToken,
    body: JSON.stringify({ access_type: accessType, password, live_update: liveUpdate }),
  })
}

export async function listThreadShares(
  accessToken: string,
  threadId: string,
): Promise<ShareResponse[]> {
  return await apiFetch<ShareResponse[]>(`/v1/threads/${threadId}:share`, {
    method: 'GET',
    accessToken,
  })
}

export async function deleteThreadShare(
  accessToken: string,
  threadId: string,
  shareId: string,
): Promise<void> {
  await apiFetch<void>(`/v1/threads/${threadId}:share?id=${shareId}`, {
    method: 'DELETE',
    accessToken,
  })
}

export async function createThreadReport(
  accessToken: string,
  threadId: string,
  categories: string[],
  feedback?: string,
): Promise<void> {
  await apiFetch<void>(`/v1/threads/${threadId}:report`, {
    method: 'POST',
    accessToken,
    body: JSON.stringify({ categories, feedback: feedback || undefined }),
  })
}

export async function createSuggestionFeedback(
  accessToken: string,
  feedback: string,
): Promise<void> {
  await apiFetch<void>('/v1/me/feedback', {
    method: 'POST',
    accessToken,
    body: JSON.stringify({ feedback }),
  })
}

export type SharedThreadResponse = {
  requires_password: boolean
  thread?: {
    title: string | null
    created_at: string
  }
  messages?: Array<{
    id: string
    role: string
    content: string
    content_json?: MessageContent
    created_at: string
  }>
}

export async function getSharedThread(
  token: string,
  sessionToken?: string,
): Promise<SharedThreadResponse> {
  const params = new URLSearchParams()
  if (sessionToken) params.set('session_token', sessionToken)
  const qs = params.toString()
  return await apiFetch<SharedThreadResponse>(`/v1/s/${token}${qs ? `?${qs}` : ''}`)
}

export type VerifyShareResponse = {
  session_token: string
}

export async function verifySharePassword(
  token: string,
  password: string,
): Promise<VerifyShareResponse> {
  return await apiFetch<VerifyShareResponse>(`/v1/s/${token}/verify`, {
    method: 'POST',
    body: JSON.stringify({ password }),
  })
}
