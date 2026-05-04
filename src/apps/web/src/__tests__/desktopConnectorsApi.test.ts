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
              { provider_name: 'web_fetch.firecrawl', is_active: true, base_url: 'https://firecrawl.local' },
            ],
          },
          {
            group_name: 'web_search',
            providers: [
              { provider_name: 'web_search.basic', is_active: true },
              { provider_name: 'web_search.tavily', is_active: false },
            ],
          },
        ],
      }))

    const api = getDesktopConnectorsApi('local-jwt')
    expect(api).toBeTruthy()

    const config = await api!.get()
    expect(config).toMatchObject({
      fetch: { provider: 'firecrawl', firecrawlBaseUrl: 'https://firecrawl.local' },
      search: { provider: 'basic' },
    })

    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('http://127.0.0.1:19080/v1/tool-providers?scope=platform')
    expect((init?.headers as Headers).get('Authorization')).toBe('Bearer local-jwt')
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
