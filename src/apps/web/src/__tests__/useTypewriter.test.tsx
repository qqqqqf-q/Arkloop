import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useTypewriter } from '../hooks/useTypewriter'

const performanceNow = vi.fn(() => 0)

function HookProbe({
  text,
  complete = false,
  onValue,
}: {
  text: string
  complete?: boolean
  onValue: (value: string) => void
}) {
  const value = useTypewriter(text, complete)
  onValue(value)
  return null
}

describe('useTypewriter', () => {
  const originalPerformance = globalThis.performance
  const originalRAF = globalThis.requestAnimationFrame
  const originalCAF = globalThis.cancelAnimationFrame
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  let now = 0
  let nextFrameId = 1
  let pendingFrames = new Map<number, FrameRequestCallback>()

  function flushFrame(at: number) {
    now = at
    const frames = [...pendingFrames.entries()]
    pendingFrames = new Map()
    for (const [, callback] of frames) callback(at)
  }

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
    now = 0
    nextFrameId = 1
    pendingFrames = new Map()
    performanceNow.mockImplementation(() => now)
    Object.defineProperty(globalThis, 'performance', {
      configurable: true,
      value: { now: performanceNow },
    })
    globalThis.requestAnimationFrame = (callback: FrameRequestCallback) => {
      const id = nextFrameId++
      pendingFrames.set(id, callback)
      return id
    }
    globalThis.cancelAnimationFrame = (id: number) => {
      pendingFrames.delete(id)
    }
  })

  afterEach(() => {
    vi.clearAllMocks()
    globalThis.requestAnimationFrame = originalRAF
    globalThis.cancelAnimationFrame = originalCAF
    Object.defineProperty(globalThis, 'performance', {
      configurable: true,
      value: originalPerformance,
    })
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('流式阶段应按降频节奏推进而不是每帧提交', async () => {
    const values: string[] = []
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<HookProbe text="abcdefghijklmnopqrstuvwxyz" onValue={(value) => values.push(value)} />)
    })

    await act(async () => {
      flushFrame(10)
      flushFrame(26)
      flushFrame(42)
    })
    expect(values.at(-1)).toBe('')

    await act(async () => {
      flushFrame(54)
    })
    const firstTick = values.at(-1) ?? ''
    expect(firstTick.length).toBeGreaterThan(0)
    expect(firstTick.length).toBeLessThan(26)

    await act(async () => {
      flushFrame(70)
      flushFrame(86)
    })
    expect(values.at(-1)).toBe(firstTick)

    await act(async () => {
      flushFrame(98)
    })
    expect((values.at(-1) ?? '').length).toBeGreaterThan(firstTick.length)

    act(() => root.unmount())
    container.remove()
  })

  it('完成态应直接同步到完整内容', async () => {
    const values: string[] = []
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<HookProbe text="hello world" complete onValue={(value) => values.push(value)} />)
    })

    await act(async () => {
      flushFrame(5)
    })

    expect(values.at(-1)).toBe('hello world')

    act(() => root.unmount())
    container.remove()
  })
})
