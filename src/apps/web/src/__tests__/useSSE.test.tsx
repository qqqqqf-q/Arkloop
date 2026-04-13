import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useSSE, type UseSSEResult } from '../hooks/useSSE'

const mockedCreateSSEClient = vi.hoisted(() => vi.fn())
const mockedReadLastSeqFromStorage = vi.hoisted(() => vi.fn())
const mockedWriteLastSeqToStorage = vi.hoisted(() => vi.fn())
const mockedClearLastSeqInStorage = vi.hoisted(() => vi.fn())

vi.mock('../sse', () => ({
  createSSEClient: mockedCreateSSEClient,
}))

vi.mock('../storage', () => ({
  readLastSeqFromStorage: mockedReadLastSeqFromStorage,
  writeLastSeqToStorage: mockedWriteLastSeqToStorage,
  clearLastSeqInStorage: mockedClearLastSeqInStorage,
}))

vi.mock('../streamDebug', () => ({
  emitStreamDebug: vi.fn(),
}))

vi.mock('@arkloop/shared', () => ({
  silentRefresh: vi.fn(async () => 'refreshed-token'),
}))

vi.mock('@arkloop/shared/desktop', () => ({
  isLocalMode: vi.fn(() => true),
}))

function HookProbe({
  runId,
  accessToken,
  onSnapshot,
}: {
  runId: string
  accessToken: string
  onSnapshot: (value: UseSSEResult) => void
}) {
  const value = useSSE({ runId, accessToken, baseUrl: 'http://api.test' })
  onSnapshot(value)
  return null
}

describe('useSSE', () => {
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    vi.clearAllMocks()
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
  })

  afterEach(() => {
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('切换 runId 后应关闭旧 client，并使用新 run 的 last seq 建连', async () => {
    const firstClient = {
      connect: vi.fn(async () => {}),
      close: vi.fn(),
      reconnect: vi.fn(async () => {}),
    }
    const secondClient = {
      connect: vi.fn(async () => {}),
      close: vi.fn(),
      reconnect: vi.fn(async () => {}),
    }
    mockedCreateSSEClient
      .mockReturnValueOnce(firstClient)
      .mockReturnValueOnce(secondClient)
    mockedReadLastSeqFromStorage.mockImplementation((runId: string) => (
      runId === 'run-1' ? 7 : 3
    ))

    let latest: UseSSEResult | null = null
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<HookProbe runId="run-1" accessToken="token" onSnapshot={(value) => { latest = value }} />)
    })

    await act(async () => {
      latest?.connect()
      await Promise.resolve()
    })

    expect(mockedCreateSSEClient).toHaveBeenNthCalledWith(1, expect.objectContaining({
      url: 'http://api.test/v1/runs/run-1/events',
      afterSeq: 7,
      accessToken: 'token',
    }))
    expect(firstClient.connect).toHaveBeenCalledTimes(1)

    await act(async () => {
      root.render(<HookProbe runId="run-2" accessToken="token" onSnapshot={(value) => { latest = value }} />)
    })

    await act(async () => {
      latest?.connect()
      await Promise.resolve()
    })

    expect(firstClient.close).toHaveBeenCalledTimes(1)
    expect(mockedCreateSSEClient).toHaveBeenNthCalledWith(2, expect.objectContaining({
      url: 'http://api.test/v1/runs/run-2/events',
      afterSeq: 3,
      accessToken: 'token',
    }))
    expect(secondClient.connect).toHaveBeenCalledTimes(1)

    act(() => root.unmount())
    container.remove()
  })

  it('run 切换后 reconnect 只应作用于当前 client', async () => {
    const firstClient = {
      connect: vi.fn(async () => {}),
      close: vi.fn(),
      reconnect: vi.fn(async () => {}),
    }
    const secondClient = {
      connect: vi.fn(async () => {}),
      close: vi.fn(),
      reconnect: vi.fn(async () => {}),
    }
    mockedCreateSSEClient
      .mockReturnValueOnce(firstClient)
      .mockReturnValueOnce(secondClient)
    mockedReadLastSeqFromStorage.mockReturnValue(0)

    let latest: UseSSEResult | null = null
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<HookProbe runId="run-1" accessToken="token" onSnapshot={(value) => { latest = value }} />)
    })
    await act(async () => {
      latest?.connect()
      await Promise.resolve()
    })

    await act(async () => {
      root.render(<HookProbe runId="run-2" accessToken="token" onSnapshot={(value) => { latest = value }} />)
    })
    await act(async () => {
      latest?.connect()
      await Promise.resolve()
    })

    await act(async () => {
      latest?.reconnect()
      await Promise.resolve()
    })

    expect(firstClient.reconnect).not.toHaveBeenCalled()
    expect(secondClient.reconnect).toHaveBeenCalledTimes(1)

    act(() => root.unmount())
    container.remove()
  })
})
