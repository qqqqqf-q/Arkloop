import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { getDesktopMemoryApi } from '../desktopMemoryApi'

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

describe('desktop memory API', () => {
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

  it('uses HTTP memory routes for headless local mode', async () => {
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(jsonResponse({
        provider: 'notebook',
        configured: true,
        healthy: true,
        checked_at: '2026-05-05T00:00:00Z',
      }))

    const api = getDesktopMemoryApi('local-jwt')
    expect(api).toBeTruthy()

    const config = await api!.getConfig()
    expect(config).toMatchObject({
      enabled: true,
      provider: 'notebook',
      memoryCommitEachTurn: true,
    })

    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('http://127.0.0.1:19080/v1/desktop/memory/status')
    expect((init?.headers as Headers).get('Authorization')).toBe('Bearer local-jwt')
  })

  it('maps headless runtime provider into local config', async () => {
    vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(jsonResponse({
        provider: 'nowledge',
        configured: true,
        healthy: true,
        checked_at: '2026-05-05T00:00:00Z',
      }))

    const api = getDesktopMemoryApi('nowledge-jwt')
    const config = await api!.getConfig()
    expect(config.provider).toBe('nowledge')
  })

  it('keeps the local adapter stable for hook dependencies', () => {
    expect(getDesktopMemoryApi('local-jwt')).toBe(getDesktopMemoryApi('local-jwt'))
    expect(getDesktopMemoryApi('next-jwt')).not.toBe(getDesktopMemoryApi('local-jwt'))
  })

  it('prefers Electron preload memory API when present', () => {
    const electronMemory = {
      getConfig: vi.fn(),
    }
    globals.arkloop = {
      isDesktop: true,
      memory: electronMemory,
    }

    expect(getDesktopMemoryApi('local-jwt')).toBe(electronMemory)
  })
})
