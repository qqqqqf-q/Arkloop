import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../api', async () => {
  const actual = await vi.importActual<typeof import('../api')>('../api')
  return {
    ...actual,
    updateMe: vi.fn(),
  }
})

vi.mock('@arkloop/shared', async () => {
  const actual = await vi.importActual<typeof import('@arkloop/shared')>('@arkloop/shared')
  return {
    ...actual,
    detectDeviceTimeZone: vi.fn(() => 'Asia/Singapore'),
    listSupportedTimeZones: vi.fn(() => ['Asia/Singapore', 'America/Los_Angeles', 'Asia/Shanghai']),
    useToast: () => ({ addToast: vi.fn() }),
  }
})

describe('TimeZoneSettings', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot>
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    vi.useRealTimers()
    vi.resetModules()
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
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

  async function loadSubject() {
    const api = await import('../api')
    const shared = await import('@arkloop/shared')
    const { LocaleProvider } = await import('../contexts/LocaleContext')
    const { TimeZoneSettings } = await import('../components/settings/TimeZoneSettings')
    vi.mocked(api.updateMe).mockReset()
    vi.mocked(api.updateMe).mockResolvedValue({ username: 'alice', timezone: null })
    return { api, shared, LocaleProvider, TimeZoneSettings }
  }

  it('可恢复到账户默认时区', async () => {
    const { api, shared, LocaleProvider, TimeZoneSettings } = await loadSubject()

    await act(async () => {
      root.render(
        <LocaleProvider>
          <TimeZoneSettings
            accessToken="token"
            me={{
              id: 'user-1',
              username: 'alice',
              email_verified: true,
              email_verification_required: false,
              work_enabled: true,
              timezone: 'Asia/Shanghai',
              account_timezone: 'America/Los_Angeles',
            }}
          />
        </LocaleProvider>,
      )
    })

    expect(shared.listSupportedTimeZones).not.toHaveBeenCalled()

    const trigger = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('Asia/Shanghai'),
    )
    expect(trigger).toBeTruthy()

    await act(async () => {
      trigger!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    expect(shared.listSupportedTimeZones).toHaveBeenCalledTimes(1)

    const accountDefault = Array.from(document.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('账户默认时区'),
    )
    expect(accountDefault).toBeTruthy()

    await act(async () => {
      accountDefault!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    expect(api.updateMe).toHaveBeenCalledWith('token', { timezone: '' })
  })

  it('空闲时会提前预热时区数据', async () => {
    vi.useFakeTimers()
    const { shared, LocaleProvider, TimeZoneSettings } = await loadSubject()

    await act(async () => {
      root.render(
        <LocaleProvider>
          <TimeZoneSettings
            accessToken="token"
            me={{
              id: 'user-1',
              username: 'alice',
              email_verified: true,
              email_verification_required: false,
              work_enabled: true,
              timezone: 'Asia/Shanghai',
              account_timezone: 'America/Los_Angeles',
            }}
          />
        </LocaleProvider>,
      )
    })

    expect(shared.listSupportedTimeZones).not.toHaveBeenCalled()

    await act(async () => {
      vi.advanceTimersByTime(20)
    })

    expect(shared.listSupportedTimeZones).toHaveBeenCalledTimes(1)
  })

  it('展开时只渲染可见范围内的时区项', async () => {
    const { shared, LocaleProvider, TimeZoneSettings } = await loadSubject()
    vi.mocked(shared.listSupportedTimeZones).mockReturnValue(
      Array.from({ length: 200 }, (_, index) => `Region/City_${index}`),
    )

    await act(async () => {
      root.render(
        <LocaleProvider>
          <TimeZoneSettings
            accessToken="token"
            me={{
              id: 'user-1',
              username: 'alice',
              email_verified: true,
              email_verification_required: false,
              work_enabled: true,
              timezone: 'Region/City_120',
              account_timezone: 'America/Los_Angeles',
            }}
          />
        </LocaleProvider>,
      )
    })

    const trigger = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('Region/City_120'),
    )
    expect(trigger).toBeTruthy()

    await act(async () => {
      trigger!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    const menuButtons = document.querySelectorAll('.dropdown-menu button')
    expect(menuButtons.length).toBeLessThan(40)
    const firstZoneButton = Array.from(menuButtons).find((button) =>
      button.textContent?.includes('Region/City_'),
    ) as HTMLButtonElement | undefined
    expect(firstZoneButton?.style.minHeight).toBe('')
  })
})
