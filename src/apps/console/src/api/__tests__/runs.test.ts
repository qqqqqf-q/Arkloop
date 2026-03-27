import { describe, expect, it, vi } from 'vitest'

import { apiFetch } from '../client'
import { cancelRun } from '../runs'

vi.mock('../client', () => ({
  apiFetch: vi.fn(),
}))

const mockedApiFetch = vi.mocked(apiFetch)

describe('console cancel run API', () => {
  beforeEach(() => {
    mockedApiFetch.mockReset()
  })

  it('includes the provided last_seen_seq', async () => {
    mockedApiFetch.mockResolvedValue({ ok: true })
    await cancelRun('run-1', 'token', 42)
    expect(mockedApiFetch).toHaveBeenCalledWith(
      '/v1/runs/run-1:cancel',
      expect.objectContaining({
        method: 'POST',
        accessToken: 'token',
        body: JSON.stringify({ last_seen_seq: 42 }),
      }),
    )
  })

  it('clamps negative seq to zero', async () => {
    mockedApiFetch.mockResolvedValue({ ok: true })
    await cancelRun('run-2', 'token', -5)
    expect(mockedApiFetch).toHaveBeenCalledWith(
      '/v1/runs/run-2:cancel',
      expect.objectContaining({
        method: 'POST',
        accessToken: 'token',
        body: JSON.stringify({ last_seen_seq: 0 }),
      }),
    )
  })
})
