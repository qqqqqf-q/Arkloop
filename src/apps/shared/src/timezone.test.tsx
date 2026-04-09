import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { TimeZoneProvider } from './contexts/TimeZoneContext'
import { formatDateTime, parseDateTimeLocalToUTC, setActiveTimeZone } from './timezone'

function Stamp({ value }: { value: string }) {
  return <span>{formatDateTime(value, { includeSeconds: true, includeZone: true })}</span>
}

describe('timezone rendering', () => {
  let container: HTMLDivElement
  let root: ReturnType<typeof createRoot>

  beforeEach(() => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
    setActiveTimeZone('UTC')
  })

  afterEach(() => {
    act(() => root.unmount())
    container.remove()
    setActiveTimeZone(null)
  })

  it('首屏渲染时直接使用用户时区', async () => {
    await act(async () => {
      root.render(
        <TimeZoneProvider userTimeZone="Asia/Shanghai">
          <Stamp value="2026-04-09T14:11:19Z" />
        </TimeZoneProvider>,
      )
    })

    expect(container.textContent).toBe('2026-04-09 22:11:19 [UTC+8]')
  })

  it('用户未设置时回退到账户时区', async () => {
    await act(async () => {
      root.render(
        <TimeZoneProvider accountTimeZone="America/Los_Angeles">
          <Stamp value="2026-04-09T14:11:19Z" />
        </TimeZoneProvider>,
      )
    })

    expect(container.textContent).toBe('2026-04-09 07:11:19 [UTC-7]')
  })

  it('支持保留毫秒精度', () => {
    expect(
      formatDateTime('2026-04-09T14:11:19.123Z', {
        timeZone: 'Asia/Shanghai',
        includeSeconds: true,
        includeMilliseconds: true,
        includeZone: true,
      }),
    ).toBe('2026-04-09 22:11:19.123 [UTC+8]')
  })

  it('按当前时区把 datetime-local 转成 UTC', () => {
    expect(parseDateTimeLocalToUTC('2026-04-09T22:11', 'Asia/Shanghai')).toBe('2026-04-09T14:11:00.000Z')
  })

  it('DST 时区也能正确换算 datetime-local', () => {
    expect(parseDateTimeLocalToUTC('2026-07-04T09:30', 'America/Los_Angeles')).toBe('2026-07-04T16:30:00.000Z')
  })

  it('非法的 datetime-local 会返回 undefined', () => {
    expect(parseDateTimeLocalToUTC('2026-02-30T10:00', 'UTC')).toBeUndefined()
    expect(parseDateTimeLocalToUTC('2026-04-09T25:00', 'UTC')).toBeUndefined()
  })

  it('DST gap 时间不会被默默偏移', () => {
    expect(parseDateTimeLocalToUTC('2026-03-08T02:30', 'America/Los_Angeles')).toBeUndefined()
  })

  it('DST fold 时间保持未知而不偏移', () => {
    expect(parseDateTimeLocalToUTC('2026-11-01T01:30', 'America/Los_Angeles')).toBeUndefined()
  })
})
