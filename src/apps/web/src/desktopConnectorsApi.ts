import { getDesktopApi, isDesktop, isLocalMode } from '@arkloop/shared/desktop'
import type { ArkloopDesktopApi, ConnectorsConfig } from '@arkloop/shared/desktop'
import {
  activateToolProvider,
  deactivateToolProvider,
  listToolProviders,
  updateToolProviderCredential,
  type ToolProviderGroup,
} from './api-admin'

type DesktopConnectorsApi = NonNullable<ArkloopDesktopApi['connectors']>

let cachedLocalToken = ''
let cachedLocalConnectorsApi: DesktopConnectorsApi | null = null

function secretPreview(keyPrefix?: string): string | undefined {
  const prefix = keyPrefix?.trim()
  if (!prefix) return undefined
  const paddingLength = Math.max(0, 12 - prefix.length)
  return `${prefix}${'*'.repeat(paddingLength)}`
}

function findProviderGroup(groups: ToolProviderGroup[], groupName: string): ToolProviderGroup | undefined {
  return groups.find((group) => group.group_name === groupName)
}

function providerNameToFetch(providerName: string): ConnectorsConfig['fetch']['provider'] {
  switch (providerName) {
  case 'web_fetch.basic':
    return 'basic'
  case 'web_fetch.firecrawl':
    return 'firecrawl'
  case 'web_fetch.jina':
    return 'jina'
  default:
    return 'none'
  }
}

function providerNameToSearch(providerName: string): ConnectorsConfig['search']['provider'] {
  switch (providerName) {
  case 'web_search.basic':
    return 'basic'
  case 'web_search.searxng':
    return 'searxng'
  case 'web_search.exa':
    return 'exa'
  case 'web_search.tavily':
    return 'tavily'
  default:
    return 'none'
  }
}

function connectorsFromProviderGroups(groups: ToolProviderGroup[]): ConnectorsConfig {
  const fetchGroup = findProviderGroup(groups, 'web_fetch')
  const searchGroup = findProviderGroup(groups, 'web_search')
  const activeFetch = fetchGroup?.providers.find((provider) => provider.is_active)
  const activeSearch = searchGroup?.providers.find((provider) => provider.is_active)

  return {
    fetch: {
      provider: activeFetch ? providerNameToFetch(activeFetch.provider_name) : 'none',
      jinaApiKey: activeFetch?.provider_name === 'web_fetch.jina'
        ? secretPreview(activeFetch.key_prefix)
        : undefined,
      jinaApiKeyStored: activeFetch?.provider_name === 'web_fetch.jina' && Boolean(activeFetch.key_prefix),
      firecrawlApiKey: activeFetch?.provider_name === 'web_fetch.firecrawl'
        ? secretPreview(activeFetch.key_prefix)
        : undefined,
      firecrawlApiKeyStored: activeFetch?.provider_name === 'web_fetch.firecrawl' && Boolean(activeFetch.key_prefix),
      firecrawlBaseUrl: activeFetch?.provider_name === 'web_fetch.firecrawl' ? activeFetch.base_url : undefined,
    },
    search: {
      provider: activeSearch ? providerNameToSearch(activeSearch.provider_name) : 'none',
      tavilyApiKey: activeSearch?.provider_name === 'web_search.tavily'
        ? secretPreview(activeSearch.key_prefix)
        : undefined,
      tavilyApiKeyStored: activeSearch?.provider_name === 'web_search.tavily' && Boolean(activeSearch.key_prefix),
      searxngBaseUrl: activeSearch?.provider_name === 'web_search.searxng' ? activeSearch.base_url : undefined,
    },
  }
}

async function deactivateToolProviderGroup(accessToken: string, groupName: string): Promise<void> {
  const group = findProviderGroup(await listToolProviders(accessToken), groupName)
  if (!group) return
  await Promise.all(
    group.providers
      .filter((provider) => provider.is_active)
      .map((provider) => deactivateToolProvider(accessToken, groupName, provider.provider_name)),
  )
}

async function applySearchConnector(accessToken: string, search: ConnectorsConfig['search']): Promise<void> {
  await deactivateToolProviderGroup(accessToken, 'web_search')
  if (search.provider === 'basic') {
    await activateToolProvider(accessToken, 'web_search', 'web_search.basic')
    return
  }
  if (search.provider === 'tavily') {
    await activateToolProvider(accessToken, 'web_search', 'web_search.tavily')
    if (!search.tavilyApiKeyStored) {
      await updateToolProviderCredential(accessToken, 'web_search', 'web_search.tavily', {
        api_key: search.tavilyApiKey ?? '',
      })
    }
    return
  }
  if (search.provider === 'exa') {
    await activateToolProvider(accessToken, 'web_search', 'web_search.exa')
    return
  }
  if (search.provider === 'searxng') {
    await activateToolProvider(accessToken, 'web_search', 'web_search.searxng')
    await updateToolProviderCredential(accessToken, 'web_search', 'web_search.searxng', {
      base_url: search.searxngBaseUrl ?? '',
    })
  }
}

async function applyFetchConnector(accessToken: string, fetch: ConnectorsConfig['fetch']): Promise<void> {
  await deactivateToolProviderGroup(accessToken, 'web_fetch')
  if (fetch.provider === 'basic') {
    await activateToolProvider(accessToken, 'web_fetch', 'web_fetch.basic')
    return
  }
  if (fetch.provider === 'jina') {
    await activateToolProvider(accessToken, 'web_fetch', 'web_fetch.jina')
    if (!fetch.jinaApiKeyStored) {
      await updateToolProviderCredential(accessToken, 'web_fetch', 'web_fetch.jina', {
        api_key: fetch.jinaApiKey ?? '',
      })
    }
    return
  }
  if (fetch.provider === 'firecrawl') {
    await activateToolProvider(accessToken, 'web_fetch', 'web_fetch.firecrawl')
    const credential: Record<string, string> = {
      base_url: fetch.firecrawlBaseUrl ?? '',
    }
    if (!fetch.firecrawlApiKeyStored) {
      credential.api_key = fetch.firecrawlApiKey ?? ''
    }
    await updateToolProviderCredential(accessToken, 'web_fetch', 'web_fetch.firecrawl', credential)
  }
}

function localConnectorsApi(accessToken: string): DesktopConnectorsApi {
  return {
    get: async () => connectorsFromProviderGroups(await listToolProviders(accessToken)),
    set: async (config: ConnectorsConfig) => {
      await applySearchConnector(accessToken, config.search)
      await applyFetchConnector(accessToken, config.fetch)
      return { ok: true }
    },
  }
}

export function getDesktopConnectorsApi(accessToken?: string): DesktopConnectorsApi | undefined {
  const connectors = getDesktopApi()?.connectors
  if (connectors) return connectors
  if (!accessToken || !isDesktop() || !isLocalMode()) return undefined
  if (cachedLocalConnectorsApi && cachedLocalToken === accessToken) return cachedLocalConnectorsApi
  cachedLocalToken = accessToken
  cachedLocalConnectorsApi = localConnectorsApi(accessToken)
  return cachedLocalConnectorsApi
}
