import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useIncrementalTypewriter } from '../hooks/useIncrementalTypewriter'

const performanceNow = vi.fn(() => 0)

function HookProbe({
  text,
  enabled = true,
  onValue,
}: {
  text: string
  enabled?: boolean
  onValue: (value: string) => void
}) {
  const value = useIncrementalTypewriter(text, enabled)
  onValue(value)
  return null
}

describe('useIncrementalTypewriter', () => {
  const originalPerformance = globalThis.performance
  const originalRAF = globalThis.requestAnimationFrame
  const originalCAF = globalThis.cancelAnimationFrame
  const originalMatchMedia = window.matchMedia
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
    window.matchMedia = vi.fn((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(() => false),
    }))
  })

  afterEach(() => {
    vi.clearAllMocks()
    globalThis.requestAnimationFrame = originalRAF
    globalThis.cancelAnimationFrame = originalCAF
    Object.defineProperty(globalThis, 'performance', {
      configurable: true,
      value: originalPerformance,
    })
    window.matchMedia = originalMatchMedia
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('初始挂载时应从空串逐字打到完整状态句', async () => {
    const values: string[] = []
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<HookProbe text="Finding the right words..." onValue={(value) => values.push(value)} />)
    })

    expect(values.at(-1)).toBe('')

    await act(async () => {
      flushFrame(54)
    })
    expect((values.at(-1) ?? '').length).toBeGreaterThan(0)
    expect(values.at(-1)).not.toBe('Finding the right words...')

    await act(async () => {
      flushFrame(120)
      flushFrame(220)
      flushFrame(340)
      flushFrame(520)
      flushFrame(760)
      flushFrame(1040)
    })
    expect(values.at(-1)).toBe('Finding the right words...')

    act(() => root.unmount())
    container.remove()
  })

  it('尾部扩展时应保留前缀，只补新增尾巴', async () => {
    const values: string[] = []
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<HookProbe text="Finding the right words..." onValue={(value) => values.push(value)} />)
    })
    await act(async () => {
      flushFrame(80)
      flushFrame(180)
      flushFrame(320)
      flushFrame(520)
      flushFrame(760)
      flushFrame(1040)
    })
    values.length = 0

    await act(async () => {
      root.render(<HookProbe text="Finding the right words for 1s" onValue={(value) => values.push(value)} />)
    })

    expect(values.at(-1)).toBe('Finding the right words...')

    await act(async () => {
      flushFrame(1100)
    })

    const mid = values.at(-1) ?? ''
    expect(mid.startsWith('Finding the right words')).toBe(true)
    expect(mid).not.toBe('Finding the right words for 1s')

    await act(async () => {
      flushFrame(1180)
      flushFrame(1300)
      flushFrame(1460)
    })
    expect(values.at(-1)).toBe('Finding the right words for 1s')

    act(() => root.unmount())
    container.remove()
  })

  it('局部字符替换时只更新变化位，不重打未变化后缀', async () => {
    const values: string[] = []
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<HookProbe text="Finding the right words for 1s" onValue={(value) => values.push(value)} />)
    })
    await act(async () => {
      flushFrame(80)
      flushFrame(180)
      flushFrame(320)
      flushFrame(520)
      flushFrame(760)
      flushFrame(1040)
    })
    values.length = 0

    await act(async () => {
      root.render(<HookProbe text="Finding the right words for 2s" onValue={(value) => values.push(value)} />)
    })

    expect(values.at(-1)).toBe('Finding the right words for 1s')

    await act(async () => {
      flushFrame(1100)
    })

    expect(values).not.toContain('Finding the right words for s')
    expect(values.at(-1)).toBe('Finding the right words for 1s')

    await act(async () => {
      flushFrame(1240)
    })

    expect(values.at(-1)).toBe('Finding the right words for 2s')

    act(() => root.unmount())
    container.remove()
  })

  it('前半段改写时应保留共同尾巴，不把尾巴重打一次', async () => {
    const values: string[] = []
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<HookProbe text="Finding the right words for 2s" onValue={(value) => values.push(value)} />)
    })
    await act(async () => {
      flushFrame(80)
      flushFrame(180)
      flushFrame(320)
      flushFrame(520)
      flushFrame(760)
      flushFrame(1040)
    })
    values.length = 0

    await act(async () => {
      root.render(<HookProbe text="Thought for 2s" onValue={(value) => values.push(value)} />)
    })

    expect(values.at(-1)).toBe('Finding the right words for 2s')

    await act(async () => {
      flushFrame(1100)
    })

    const partial = values.at(-1) ?? ''
    expect(partial.endsWith(' for 2s')).toBe(true)
    expect(partial).not.toBe('Thought for 2s')

    await act(async () => {
      flushFrame(1180)
      flushFrame(1300)
      flushFrame(1460)
    })
    expect(values.at(-1)).toBe('Thought for 2s')

    act(() => root.unmount())
    container.remove()
  })

  it('相同文本重复 render 时不应重启动画', async () => {
    const values: string[] = []
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<HookProbe text="Thought for 2s" onValue={(value) => values.push(value)} />)
    })
    await act(async () => {
      flushFrame(80)
      flushFrame(180)
      flushFrame(320)
      flushFrame(520)
    })
    values.length = 0

    await act(async () => {
      root.render(<HookProbe text="Thought for 2s" onValue={(value) => values.push(value)} />)
    })

    expect(values.at(-1)).toBe('Thought for 2s')
    expect(values).not.toContain('')

    act(() => root.unmount())
    container.remove()
  })

  it('reduced motion 下应直接显示完整目标句', async () => {
    window.matchMedia = vi.fn((query: string) => ({
      matches: query === '(prefers-reduced-motion: reduce)',
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(() => false),
    }))

    const values: string[] = []
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<HookProbe text="Finding the right words..." onValue={(value) => values.push(value)} />)
    })

    expect(values.at(-1)).toBe('Finding the right words...')

    act(() => root.unmount())
    container.remove()
  })
})
