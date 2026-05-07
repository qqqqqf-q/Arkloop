import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const mockedSilentRefresh = vi.hoisted(() => vi.fn())
const mockedApiBaseUrl = vi.hoisted(() => vi.fn())
const mockedCreateAgentClient = vi.hoisted(() => vi.fn())

vi.mock('@arkloop/shared', () => ({
  silentRefresh: mockedSilentRefresh,
}))

vi.mock('@arkloop/shared/api', () => ({
  apiBaseUrl: mockedApiBaseUrl,
}))

vi.mock('../contexts/auth', () => ({
  useAuth: () => ({ accessToken: 'expired-token' }),
}))

vi.mock('../agent-ui/arkloop-adapter', () => ({
  createArkloopAgentClient: mockedCreateAgentClient,
}))

import { useAgentClient } from '../agent-ui/use-agent-client'

describe('useAgentClient', () => {
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    mockedSilentRefresh.mockReset()
    mockedApiBaseUrl.mockReset()
    mockedCreateAgentClient.mockReset()
    mockedApiBaseUrl.mockReturnValue('http://api.test')
    mockedCreateAgentClient.mockReturnValue({ marker: 'agent-client' })
  })

  afterEach(() => {
    vi.clearAllMocks()
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('流式连接刷新 token 时使用统一 silent refresh', async () => {
    mockedSilentRefresh.mockResolvedValue('fresh-token')

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    function Probe() {
      useAgentClient()
      return null
    }

    await act(async () => {
      root.render(<Probe />)
    })

    const options = mockedCreateAgentClient.mock.calls[0]?.[0]
    await expect(options.refreshAccessToken()).resolves.toBe('fresh-token')
    expect(mockedSilentRefresh).toHaveBeenCalledTimes(1)

    act(() => {
      root.unmount()
    })
    container.remove()
  })
})
