import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { getDesktopConnectorsApi } from '../desktopConnectorsApi'

type DesktopGlobals = typeof globalThis & {
  __ARKLOOP_DESKTOP__?: {
    getMode?: () => 'local' | 'saas' | 'self-hosted'
    getApiBaseUrl?: () => string
  }
  arkloop?: unknown
}

const globals = globalThis as DesktopGlobals

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('desktop connectors API', () => {
  beforeEach(() => {
    globals.__ARKLOOP_DESKTOP__ = {
      getMode: () => 'local',
      getApiBaseUrl: () => 'http://127.0.0.1:19080',
    }
  })

  afterEach(() => {
    vi.restoreAllMocks()
    delete globals.__ARKLOOP_DESKTOP__
    delete globals.arkloop
  })

  it('uses platform tool providers for headless local mode', async () => {
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(jsonResponse({
        groups: [
          {
            group_name: 'web_fetch',
            providers: [
              { provider_name: 'web_fetch.basic', is_active: false },
              {
                provider_name: 'web_fetch.firecrawl',
                is_active: true,
                key_prefix: 'fc-123456789',
                base_url: 'https://firecrawl.local',
              },
            ],
          },
          {
            group_name: 'web_search',
            providers: [
              { provider_name: 'web_search.basic', is_active: false },
              { provider_name: 'web_search.tavily', is_active: true, key_prefix: 'tvly-1234567' },
              { provider_name: 'web_search.exa', is_active: false },
            ],
          },
        ],
      }))

    const api = getDesktopConnectorsApi('local-jwt')
    expect(api).toBeTruthy()

    const config = await api!.get()
    expect(config).toMatchObject({
      fetch: {
        provider: 'firecrawl',
        firecrawlApiKey: 'fc-123456789',
        firecrawlApiKeyStored: true,
        firecrawlBaseUrl: 'https://firecrawl.local',
      },
      search: {
        provider: 'tavily',
        tavilyApiKey: 'tvly-1234567',
        tavilyApiKeyStored: true,
      },
      xSearch: {
        provider: 'none',
      },
    })

    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('http://127.0.0.1:19080/v1/tool-providers?scope=platform')
    expect((init?.headers as Headers).get('Authorization')).toBe('Bearer local-jwt')
  })

  it('maps exa search provider', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(jsonResponse({
        groups: [
          {
            group_name: 'web_search',
            providers: [
              {
                provider_name: 'web_search.exa',
                is_active: true,
              },
            ],
          },
        ],
      }))

    const api = getDesktopConnectorsApi('local-jwt')
    expect(api).toBeTruthy()

    const config = await api!.get()
    expect(config.search).toMatchObject({
      provider: 'exa',
    })
  })

  it('maps xAI auth modes', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(jsonResponse({
        groups: [
          {
            group_name: 'x_search',
            providers: [
              {
                provider_name: 'x_search.xai',
                is_active: true,
                oauth_connected: true,
                config_json: { auth_mode: 'oauth' },
              },
            ],
          },
        ],
      }))

    const api = getDesktopConnectorsApi('local-jwt')
    expect(api).toBeTruthy()

    const config = await api!.get()
    expect(config.xSearch).toMatchObject({
      provider: 'xai_oauth',
      xaiOAuthConnected: true,
    })
  })

  it('keeps stored key previews out of credential writes', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockImplementation(() => Promise.resolve(jsonResponse({ groups: [] })))

    const api = getDesktopConnectorsApi('local-jwt')
    expect(api).toBeTruthy()

    await api!.set({
      fetch: {
        provider: 'firecrawl',
        firecrawlApiKey: 'fc-123456789',
        firecrawlApiKeyStored: true,
        firecrawlBaseUrl: 'https://firecrawl.local',
      },
      search: {
        provider: 'exa',
      },
      xSearch: {
        provider: 'none',
      },
    })

    const credentialBodies = fetchMock.mock.calls
      .filter(([url]) => String(url).endsWith('/credential?scope=platform'))
      .map(([, init]) => JSON.parse(String(init?.body ?? '{}')) as Record<string, string>)

    expect(credentialBodies).toEqual([
      { base_url: 'https://firecrawl.local' },
    ])
  })

  it('activates exa without credential writes', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockImplementation(() => Promise.resolve(jsonResponse({ groups: [] })))

    const api = getDesktopConnectorsApi('local-jwt')
    expect(api).toBeTruthy()

    await api!.set({
      fetch: { provider: 'basic' },
      search: { provider: 'exa' },
      xSearch: { provider: 'none' },
    })

    const credentialCalls = fetchMock.mock.calls
      .filter(([url]) => String(url).endsWith('/credential?scope=platform'))
    const activateCalls = fetchMock.mock.calls
      .filter(([url]) => String(url).endsWith('/v1/tool-providers/web_search/web_search.exa/activate?scope=platform'))

    expect(credentialCalls).toHaveLength(0)
    expect(activateCalls).toHaveLength(1)
  })

  it('writes xAI API key auth mode separately from OAuth', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockImplementation(() => Promise.resolve(jsonResponse({ groups: [] })))

    const api = getDesktopConnectorsApi('local-jwt')
    expect(api).toBeTruthy()

    await api!.set({
      fetch: { provider: 'basic' },
      search: { provider: 'exa' },
      xSearch: {
        provider: 'xai_api_key',
        xaiApiKey: 'xai-key',
      },
    })

    const configCalls = fetchMock.mock.calls
      .filter(([url]) => String(url).endsWith('/v1/tool-providers/x_search/x_search.xai/config?scope=platform'))
      .map(([, init]) => JSON.parse(String(init?.body ?? '{}')) as Record<string, string>)
    const credentialCalls = fetchMock.mock.calls
      .filter(([url]) => String(url).endsWith('/v1/tool-providers/x_search/x_search.xai/credential?scope=platform'))
      .map(([, init]) => JSON.parse(String(init?.body ?? '{}')) as Record<string, string>)

    expect(configCalls).toEqual([{ auth_mode: 'api_key' }])
    expect(credentialCalls).toEqual([{ api_key: 'xai-key' }])
  })

  it('prefers Electron preload connectors API when present', () => {
    const electronConnectors = {
      get: vi.fn(),
      set: vi.fn(),
    }
    globals.arkloop = {
      isDesktop: true,
      connectors: electronConnectors,
    }

    expect(getDesktopConnectorsApi('local-jwt')).toBe(electronConnectors)
  })
})
