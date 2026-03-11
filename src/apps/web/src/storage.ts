import {
  canUseStorage,
} from '@arkloop/shared/storage'

export {
  readAccessToken as readAccessTokenFromStorage,
  writeAccessToken as writeAccessTokenToStorage,
  clearAccessToken as clearAccessTokenFromStorage,
} from '@arkloop/shared/storage'

const ACTIVE_THREAD_ID_KEY = 'arkloop:web:active_thread_id'
const LOCALE_KEY = 'arkloop:web:locale'
const THEME_KEY = 'arkloop:web:theme'
const SELECTED_PERSONA_KEY = 'arkloop:web:selected_persona_key'
const APP_MODE_KEY = 'arkloop:web:app_mode'

export const DEFAULT_PERSONA_KEY = 'normal'
export const SEARCH_PERSONA_KEY = 'extended-search'

export type Theme = 'system' | 'light' | 'dark'
export type AppMode = 'chat' | 'claw'

function canUseLocalStorage(): boolean {
  return canUseStorage()
}

function lastSeqStorageKey(runId: string): string {
  return `arkloop:sse:last_seq:${runId}`
}

export function readActiveThreadIdFromStorage(): string | null {
  if (!canUseLocalStorage()) return null
  try {
    const raw = localStorage.getItem(ACTIVE_THREAD_ID_KEY)
    if (!raw) return null
    return raw.trim() ? raw : null
  } catch {
    return null
  }
}

export function writeActiveThreadIdToStorage(threadId: string): void {
  if (!canUseLocalStorage()) return
  if (!threadId.trim()) return
  try {
    localStorage.setItem(ACTIVE_THREAD_ID_KEY, threadId)
  } catch {
    // 忽略存储失败
  }
}

export function clearActiveThreadIdInStorage(): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.removeItem(ACTIVE_THREAD_ID_KEY)
  } catch {
    // 忽略存储失败
  }
}

export function readLastSeqFromStorage(runId: string): number {
  if (!runId || !canUseLocalStorage()) return 0
  try {
    const raw = localStorage.getItem(lastSeqStorageKey(runId))
    if (!raw) return 0
    const parsed = Number.parseInt(raw, 10)
    return Number.isFinite(parsed) && parsed >= 0 ? parsed : 0
  } catch {
    return 0
  }
}

export function writeLastSeqToStorage(runId: string, seq: number): void {
  if (!runId || !canUseLocalStorage()) return
  if (!Number.isFinite(seq) || seq < 0) return
  try {
    localStorage.setItem(lastSeqStorageKey(runId), String(seq))
  } catch {
    // 忽略存储失败（无痕模式/禁用存储等）
  }
}

export function clearLastSeqInStorage(runId: string): void {
  if (!runId || !canUseLocalStorage()) return
  try {
    localStorage.removeItem(lastSeqStorageKey(runId))
  } catch {
    // 忽略存储失败
  }
}

export function readLocaleFromStorage(): import('./locales').Locale {
  if (!canUseLocalStorage()) return 'zh'
  try {
    const raw = localStorage.getItem(LOCALE_KEY)
    if (raw === 'zh' || raw === 'en') return raw
    return 'zh'
  } catch {
    return 'zh'
  }
}

export function writeLocaleToStorage(locale: import('./locales').Locale): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.setItem(LOCALE_KEY, locale)
  } catch {
    // 忽略存储失败
  }
}

export function readThemeFromStorage(): Theme {
  if (!canUseLocalStorage()) return 'system'
  try {
    const raw = localStorage.getItem(THEME_KEY)
    if (raw === 'system' || raw === 'light' || raw === 'dark') return raw
    return 'system'
  } catch {
    return 'system'
  }
}

export function writeThemeToStorage(theme: Theme): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.setItem(THEME_KEY, theme)
  } catch {
    // 忽略存储失败
  }
}

export function readAppModeFromStorage(): AppMode {
  if (!canUseLocalStorage()) return 'chat'
  try {
    const raw = localStorage.getItem(APP_MODE_KEY)
    if (raw === 'chat' || raw === 'claw') return raw
    return 'chat'
  } catch {
    return 'chat'
  }
}

export function writeAppModeToStorage(mode: AppMode): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.setItem(APP_MODE_KEY, mode)
  } catch {
    // 忽略存储失败
  }
}

export function readSelectedPersonaKeyFromStorage(): string {
  if (!canUseLocalStorage()) return DEFAULT_PERSONA_KEY
  try {
    const raw = localStorage.getItem(SELECTED_PERSONA_KEY)
    if (raw && raw.trim()) return raw.trim()
    return DEFAULT_PERSONA_KEY
  } catch {
    return DEFAULT_PERSONA_KEY
  }
}

export function writeSelectedPersonaKeyToStorage(personaKey: string): void {
  if (!canUseLocalStorage()) return
  const trimmed = personaKey.trim()
  if (!trimmed) return
  try {
    localStorage.setItem(SELECTED_PERSONA_KEY, trimmed)
  } catch {
    // 忽略存储失败
  }
}

export type WebSource = {
  title: string
  url: string
  snippet?: string
}

function messageSourcesKey(messageId: string): string {
  return `arkloop:web:msg_sources:${messageId}`
}

export function readMessageSources(messageId: string): WebSource[] | null {
  if (!canUseLocalStorage() || !messageId) return null
  try {
    const raw = localStorage.getItem(messageSourcesKey(messageId))
    if (!raw) return null
    return JSON.parse(raw) as WebSource[]
  } catch {
    return null
  }
}

export function writeMessageSources(messageId: string, sources: WebSource[]): void {
  if (!canUseLocalStorage() || !messageId || sources.length === 0) return
  try {
    localStorage.setItem(messageSourcesKey(messageId), JSON.stringify(sources))
  } catch { /* ignore */ }
}

export type ArtifactRef = {
  key: string
  filename: string
  size: number
  mime_type: string
}

function messageArtifactsKey(messageId: string): string {
  return `arkloop:web:msg_artifacts:${messageId}`
}

export function readMessageArtifacts(messageId: string): ArtifactRef[] | null {
  if (!canUseLocalStorage() || !messageId) return null
  try {
    const raw = localStorage.getItem(messageArtifactsKey(messageId))
    if (!raw) return null
    return JSON.parse(raw) as ArtifactRef[]
  } catch {
    return null
  }
}

export function writeMessageArtifacts(messageId: string, artifacts: ArtifactRef[]): void {
  if (!canUseLocalStorage() || !messageId || artifacts.length === 0) return
  try {
    localStorage.setItem(messageArtifactsKey(messageId), JSON.stringify(artifacts))
  } catch { /* ignore */ }
}

export type BrowserActionRef = {
  id: string
  command: string
  output?: string
  screenshotArtifact?: ArtifactRef
  url?: string
  exitCode?: number
}

function messageBrowserActionsKey(messageId: string): string {
  return `arkloop:web:msg_browser_actions:${messageId}`
}

export function readMessageBrowserActions(messageId: string): BrowserActionRef[] | null {
  if (!canUseLocalStorage() || !messageId) return null
  try {
    const raw = localStorage.getItem(messageBrowserActionsKey(messageId))
    if (!raw) return null
    return JSON.parse(raw) as BrowserActionRef[]
  } catch {
    return null
  }
}

export function writeMessageBrowserActions(messageId: string, actions: BrowserActionRef[]): void {
  if (!canUseLocalStorage() || !messageId || actions.length === 0) return
  try {
    localStorage.setItem(messageBrowserActionsKey(messageId), JSON.stringify(actions))
  } catch { /* ignore */ }
}

export type CodeExecutionRef = {
  id: string
  language: 'python' | 'shell'
  code?: string
  output?: string
  exitCode?: number
  sessionId?: string
}

function messageCodeExecutionsKey(messageId: string): string {
  return `arkloop:web:msg_code_exec:${messageId}`
}

export function readMessageCodeExecutions(messageId: string): CodeExecutionRef[] | null {
  if (!canUseLocalStorage() || !messageId) return null
  try {
    const raw = localStorage.getItem(messageCodeExecutionsKey(messageId))
    if (!raw) return null
    return JSON.parse(raw) as CodeExecutionRef[]
  } catch {
    return null
  }
}

export function writeMessageCodeExecutions(messageId: string, executions: CodeExecutionRef[]): void {
  if (!canUseLocalStorage() || !messageId) return
  try {
    localStorage.setItem(messageCodeExecutionsKey(messageId), JSON.stringify(executions))
  } catch { /* ignore */ }
}

export type ThinkingSegmentRef = {
  segmentId: string
  kind: string
  mode: string
  label: string
  content: string
}

export type MessageThinkingRef = {
  thinkingText: string
  segments: ThinkingSegmentRef[]
}

function messageThinkingKey(messageId: string): string {
  return `arkloop:web:msg_thinking:${messageId}`
}

export function readMessageThinking(messageId: string): MessageThinkingRef | null {
  if (!canUseLocalStorage() || !messageId) return null
  try {
    const raw = localStorage.getItem(messageThinkingKey(messageId))
    if (!raw) return null
    const parsed = JSON.parse(raw) as MessageThinkingRef
    if (!parsed || typeof parsed !== 'object') return null
    const segments = Array.isArray(parsed.segments)
      ? parsed.segments.filter(
        (s): s is ThinkingSegmentRef =>
          !!s &&
          typeof s.segmentId === 'string' &&
          typeof s.kind === 'string' &&
          typeof s.mode === 'string' &&
          typeof s.label === 'string' &&
          typeof s.content === 'string',
      )
      : []
    const thinkingText = typeof parsed.thinkingText === 'string' ? parsed.thinkingText : ''
    return { thinkingText, segments }
  } catch {
    return null
  }
}

export function writeMessageThinking(messageId: string, thinking: MessageThinkingRef): void {
  if (!canUseLocalStorage() || !messageId) return
  if (thinking.thinkingText.trim() === '' && thinking.segments.length === 0) return
  try {
    localStorage.setItem(messageThinkingKey(messageId), JSON.stringify(thinking))
  } catch { /* ignore */ }
}

export type MessageSearchStepRef = {
  id: string
  kind: 'planning' | 'searching' | 'reviewing' | 'finished'
  label: string
  status: 'active' | 'done'
  queries?: string[]
}

function messageSearchStepsKey(messageId: string): string {
  return `arkloop:web:msg_search_steps:${messageId}`
}

export function readMessageSearchSteps(messageId: string): MessageSearchStepRef[] | null {
  if (!canUseLocalStorage() || !messageId) return null
  try {
    const raw = localStorage.getItem(messageSearchStepsKey(messageId))
    if (!raw) return null
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed)) return null
    const steps = parsed
      .filter((item): item is Record<string, unknown> => item != null && typeof item === 'object')
      .map((item): MessageSearchStepRef | null => {
        const id = typeof item.id === 'string' ? item.id : ''
        const kind = item.kind
        const label = typeof item.label === 'string' ? item.label : ''
        const status = item.status
        const queries = Array.isArray(item.queries)
          ? item.queries.filter((q): q is string => typeof q === 'string')
          : undefined
        if (!id) return null
        if (kind !== 'planning' && kind !== 'searching' && kind !== 'reviewing' && kind !== 'finished') return null
        if (status !== 'active' && status !== 'done') return null
        return { id, kind, label, status, queries }
      })
      .filter((step): step is MessageSearchStepRef => step != null)
    return steps.length > 0 ? steps : null
  } catch {
    return null
  }
}

export function writeMessageSearchSteps(messageId: string, steps: MessageSearchStepRef[]): void {
  if (!canUseLocalStorage() || !messageId || steps.length === 0) return
  try {
    localStorage.setItem(messageSearchStepsKey(messageId), JSON.stringify(steps))
  } catch { /* ignore */ }
}

const SEARCH_THREAD_IDS_KEY = 'arkloop:web:search_thread_ids'

export function addSearchThreadId(threadId: string): void {
  if (!canUseLocalStorage()) return
  try {
    const raw = localStorage.getItem(SEARCH_THREAD_IDS_KEY)
    const ids: string[] = raw ? (JSON.parse(raw) as string[]) : []
    if (ids.includes(threadId)) return
    ids.push(threadId)
    if (ids.length > 500) ids.splice(0, ids.length - 500)
    localStorage.setItem(SEARCH_THREAD_IDS_KEY, JSON.stringify(ids))
  } catch { /* ignore */ }
}

export function isSearchThreadId(threadId: string): boolean {
  if (!canUseLocalStorage()) return false
  try {
    const raw = localStorage.getItem(SEARCH_THREAD_IDS_KEY)
    if (!raw) return false
    return (JSON.parse(raw) as string[]).includes(threadId)
  } catch { return false }
}

// 将 fork 前旧消息 ID 对应的 localStorage 缓存迁移到新消息 ID
export function migrateMessageMetadata(mapping: Array<{ old_id: string; new_id: string }>): void {
  if (!canUseLocalStorage() || mapping.length === 0) return
  for (const { old_id, new_id } of mapping) {
    const sources = readMessageSources(old_id)
    if (sources) writeMessageSources(new_id, sources)
    const artifacts = readMessageArtifacts(old_id)
    if (artifacts) writeMessageArtifacts(new_id, artifacts)
    const codeExec = readMessageCodeExecutions(old_id)
    if (codeExec) writeMessageCodeExecutions(new_id, codeExec)
    const thinking = readMessageThinking(old_id)
    if (thinking) writeMessageThinking(new_id, thinking)
    const searchSteps = readMessageSearchSteps(old_id)
    if (searchSteps) writeMessageSearchSteps(new_id, searchSteps)
  }
}
