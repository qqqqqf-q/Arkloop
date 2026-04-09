import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const { mockDetectDeviceTimeZone } = vi.hoisted(() => ({
  mockDetectDeviceTimeZone: vi.fn(() => 'Asia/Singapore'),
}))

vi.mock('../api', async () => {
  const actual = await vi.importActual<typeof import('../api')>('../api')
  return {
    ...actual,
    getMe: vi.fn(),
    updateMe: vi.fn(),
    logout: vi.fn(),
  }
})

vi.mock('@arkloop/shared', async () => {
  const actual = await vi.importActual<typeof import('@arkloop/shared')>('@arkloop/shared')
  return {
    ...actual,
    detectDeviceTimeZone: mockDetectDeviceTimeZone,
  }
})

import { getMe, updateMe } from '../api'
import { AuthProvider, useAuth } from '../contexts/auth'

function AuthSnapshot() {
  const { me, meLoaded } = useAuth()
  return <div>{meLoaded ? (me?.timezone ?? 'empty') : 'loading'}</div>
}

describe('AuthProvider timezone bootstrap', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot>
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
    vi.mocked(getMe).mockReset()
    vi.mocked(updateMe).mockReset()
    mockDetectDeviceTimeZone.mockReset()
    mockDetectDeviceTimeZone.mockReturnValue('Asia/Singapore')
  })

  afterEach(() => {
    act(() => root.unmount())
    container.remove()
    vi.clearAllMocks()
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('用户未设置时区时会在首次进入自动写入设备时区', async () => {
    vi.mocked(getMe).mockResolvedValue({
      id: 'user-1',
      username: 'alice',
      email_verified: true,
      email_verification_required: false,
      work_enabled: true,
      timezone: null,
      account_timezone: null,
    })
    vi.mocked(updateMe).mockResolvedValue({
      username: 'alice',
      timezone: 'Asia/Singapore',
    })

    await act(async () => {
      root.render(
        <AuthProvider accessToken="token" onLoggedOut={vi.fn()}>
          <AuthSnapshot />
        </AuthProvider>,
      )
      await Promise.resolve()
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(updateMe).toHaveBeenCalledWith('token', { timezone: 'Asia/Singapore' })
    expect(container.textContent).toContain('Asia/Singapore')
  })

  it('已有账户默认时区时不会再次写入', async () => {
    vi.mocked(getMe).mockResolvedValue({
      id: 'user-1',
      username: 'alice',
      email_verified: true,
      email_verification_required: false,
      work_enabled: true,
      timezone: null,
      account_timezone: 'America/Los_Angeles',
    })
    await act(async () => {
      root.render(
        <AuthProvider accessToken="token" onLoggedOut={vi.fn()}>
          <AuthSnapshot />
        </AuthProvider>,
      )
      await Promise.resolve()
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(updateMe).not.toHaveBeenCalled()
  })
})
