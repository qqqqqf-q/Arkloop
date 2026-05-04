import { apiFetch } from './api'
import { getDesktopApi, isDesktop, isLocalMode } from '@arkloop/shared/desktop'
import type { ArkloopDesktopApi, MemoryConfig, MemoryProvider, MemoryRuntimeStatus } from '@arkloop/shared/desktop'

type DesktopMemoryApi = NonNullable<ArkloopDesktopApi['memory']>

let cachedLocalToken = ''
let cachedLocalMemoryApi: DesktopMemoryApi | null = null

function normalizeProvider(provider: string | undefined): MemoryProvider {
  if (provider === 'openviking' || provider === 'nowledge') return provider
  return 'notebook'
}

function query(agentId?: string): string {
  const trimmed = agentId?.trim()
  return trimmed ? `?agent_id=${encodeURIComponent(trimmed)}` : ''
}

function localConfigFromStatus(status: MemoryRuntimeStatus): MemoryConfig {
  const provider = normalizeProvider(status.provider)
  return {
    enabled: true,
    provider,
    memoryCommitEachTurn: true,
    ...(provider === 'nowledge' ? { nowledge: { baseUrl: 'http://127.0.0.1:14242' } } : {}),
  }
}

function localMemoryApi(accessToken: string): DesktopMemoryApi {
  const auth = { accessToken }
  return {
    getConfig: async () => localConfigFromStatus(await apiFetch<MemoryRuntimeStatus>('/v1/desktop/memory/status', auth)),
    setConfig: async () => {
      throw new Error('headless memory configuration requires restart')
    },
    list: async (agentId?: string) => apiFetch(`/v1/desktop/memory/entries${query(agentId)}`, auth),
    delete: async (id: string) => apiFetch(`/v1/desktop/memory/entries/${encodeURIComponent(id)}`, {
      ...auth,
      method: 'DELETE',
    }),
    getStatus: async (agentId?: string) => apiFetch(`/v1/desktop/memory/status${query(agentId)}`, auth),
    getSnapshot: async (agentId?: string) => apiFetch(`/v1/desktop/memory/snapshot${query(agentId)}`, auth),
    rebuildSnapshot: async (agentId?: string) => apiFetch(`/v1/desktop/memory/snapshot/rebuild${query(agentId)}`, {
      ...auth,
      method: 'POST',
    }),
    getContent: async (uri: string, layer?: 'overview' | 'read') => {
      const params = new URLSearchParams({ uri })
      if (layer) params.set('layer', layer)
      return apiFetch(`/v1/desktop/memory/content?${params.toString()}`, auth)
    },
    add: async (content: string, category?: string) => apiFetch('/v1/desktop/memory/entries', {
      ...auth,
      method: 'POST',
      body: JSON.stringify({ content, category: category || undefined }),
    }),
    getImpression: async (agentId?: string) => apiFetch(`/v1/desktop/memory/impression${query(agentId)}`, auth),
    rebuildImpression: async (agentId?: string) => apiFetch(`/v1/desktop/memory/impression/rebuild${query(agentId)}`, {
      ...auth,
      method: 'POST',
      signal: AbortSignal.timeout(120_000),
    }),
  }
}

export function getDesktopMemoryApi(accessToken?: string): DesktopMemoryApi | undefined {
  const memory = getDesktopApi()?.memory
  if (memory) return memory
  if (!accessToken || !isDesktop() || !isLocalMode()) return undefined
  if (cachedLocalMemoryApi && cachedLocalToken === accessToken) return cachedLocalMemoryApi
  cachedLocalToken = accessToken
  cachedLocalMemoryApi = localMemoryApi(accessToken)
  return cachedLocalMemoryApi
}
