import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { CopTimeline } from '../components/CopTimeline'
import { CopTimelineHeaderLabel } from '../components/cop-timeline/CopTimelineHeader'
import { LocaleProvider } from '../contexts/LocaleContext'
import type { SubAgentRef, WebSource } from '../storage'
import type { CodeExecution } from '../components/CodeExecutionCard'

globalThis.scrollTo = (() => {}) as typeof globalThis.scrollTo

const originalRAF = globalThis.requestAnimationFrame
const originalCAF = globalThis.cancelAnimationFrame
const originalMatchMedia = window.matchMedia
const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

let nextFrameId = 1
let pendingFrames = new Map<number, FrameRequestCallback>()

function installAnimationFrameMock() {
  globalThis.requestAnimationFrame = (callback: FrameRequestCallback) => {
    const id = nextFrameId++
    pendingFrames.set(id, callback)
    return id
  }
  globalThis.cancelAnimationFrame = (id: number) => {
    pendingFrames.delete(id)
  }
}

function flushAnimationFrame(at: number) {
  const frames = [...pendingFrames.entries()]
  pendingFrames = new Map()
  for (const [, callback] of frames) callback(at)
}

function reducedMotionMatchMedia(query: string) {
  return {
    matches: query === '(prefers-reduced-motion: reduce)',
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(() => false),
  }
}

function defaultMatchMedia(query: string) {
  return {
    matches: false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(() => false),
  }
}

beforeEach(() => {
  actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
  nextFrameId = 1
  pendingFrames = new Map()
  installAnimationFrameMock()
  window.matchMedia = vi.fn(defaultMatchMedia)
})

afterEach(() => {
  vi.useRealTimers()
  globalThis.requestAnimationFrame = originalRAF
  globalThis.cancelAnimationFrame = originalCAF
  window.matchMedia = originalMatchMedia
  if (originalActEnvironment === undefined) {
    delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
  } else {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
  }
})

function renderTimeline(params: {
  isComplete: boolean
  preserveExpanded?: boolean
  steps: { id: string; kind: 'planning' | 'searching' | 'reviewing' | 'finished'; label: string; status: 'active' | 'done'; queries?: string[]; sources?: WebSource[]; seq?: number }[]
  sources: WebSource[]
  narratives?: Array<{ id: string; text: string; seq: number }>
  codeExecutions?: CodeExecution[]
  subAgents?: SubAgentRef[]
  fileOps?: Array<{ id: string; toolName: string; label: string; status: 'running' | 'success' | 'failed'; seq?: number }>
  webFetches?: Array<{ id: string; url: string; title?: string; status: 'fetching' | 'done' | 'failed'; statusCode?: number; seq?: number }>
  genericTools?: Array<{ id: string; toolName: string; label: string; output?: string; emptyLabel?: string; status: 'running' | 'success' | 'failed'; errorMessage?: string; seq?: number }>
  thinkingRows?: Array<{ id: string; markdown: string; live?: boolean; seq: number; durationSec?: number; startedAtMs?: number }>
  thinkingStartedAt?: number
  trailingAssistantTextPresent?: boolean
  thinkingHint?: string
  live?: boolean
  shimmer?: boolean
}): string {
  const previousMatchMedia = window.matchMedia
  window.matchMedia = vi.fn(reducedMotionMatchMedia)
  try {
    return renderToStaticMarkup(
      <LocaleProvider>
        <CopTimeline
          steps={params.steps}
          sources={params.sources}
          narratives={params.narratives}
          isComplete={params.isComplete}
          preserveExpanded={params.preserveExpanded}
          codeExecutions={params.codeExecutions}
          subAgents={params.subAgents}
          fileOps={params.fileOps}
          webFetches={params.webFetches}
          genericTools={params.genericTools}
          thinkingRows={params.thinkingRows}
          thinkingStartedAt={params.thinkingStartedAt}
          trailingAssistantTextPresent={params.trailingAssistantTextPresent}
          thinkingHint={params.thinkingHint}
          live={params.live}
          shimmer={params.shimmer}
        />
      </LocaleProvider>,
    )
  } finally {
    window.matchMedia = previousMatchMedia
  }
}

async function renderTimelineDom(params: Parameters<typeof renderTimeline>[0]) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  await act(async () => {
    root.render(
      <LocaleProvider>
        <CopTimeline
          steps={params.steps}
          sources={params.sources}
          narratives={params.narratives}
          isComplete={params.isComplete}
          preserveExpanded={params.preserveExpanded}
          codeExecutions={params.codeExecutions}
          subAgents={params.subAgents}
          fileOps={params.fileOps}
          webFetches={params.webFetches}
          genericTools={params.genericTools}
          thinkingRows={params.thinkingRows}
          thinkingStartedAt={params.thinkingStartedAt}
          trailingAssistantTextPresent={params.trailingAssistantTextPresent}
          thinkingHint={params.thinkingHint}
          live={params.live}
          shimmer={params.shimmer}
        />
      </LocaleProvider>,
    )
  })
  return {
    container,
    cleanup: () => {
      act(() => {
        root.unmount()
      })
      container.remove()
    },
  }
}

async function flushTypingFrames(times: number[]) {
  await act(async () => {
    for (const time of times) {
      flushAnimationFrame(time)
    }
  })
}

async function renderHeaderLabelDom(params: { text: string; incremental?: boolean }) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  await act(async () => {
    root.render(
      <LocaleProvider>
        <CopTimelineHeaderLabel
          text={params.text}
          phaseKey="test"
          incremental={params.incremental}
        />
      </LocaleProvider>,
    )
  })
  return {
    container,
    cleanup: () => {
      act(() => {
        root.unmount()
      })
      container.remove()
    },
  }
}

describe('CopTimeline', () => {
  it('isComplete=true 时应默认收起内容', () => {
    const html = renderTimeline({
      isComplete: true,
      steps: [
        { id: 's1', kind: 'planning', label: 'Plan step', status: 'done' },
        { id: 's2', kind: 'searching', label: 'Search step', status: 'done', queries: ['hello'] },
      ],
      sources: [{ title: 'Example', url: 'https://example.com' }],
    })

    expect(html).toContain('Reviewed 1 sources')
    expect(html).not.toContain('Plan step')
    expect(html).not.toContain('Search step')
  })

  it('handoff 保留态下即使 isComplete=true 也不应自动收起内容', () => {
    const html = renderTimeline({
      isComplete: true,
      preserveExpanded: true,
      steps: [
        { id: 's1', kind: 'planning', label: 'Plan step', status: 'done' },
      ],
      sources: [],
    })

    expect(html).toContain('Plan step')
    expect(html).not.toContain('1 steps completed')
  })

  it('已完成且仅包含 thinking 时应显示完成后的 thought 文案', () => {
    const html = renderTimeline({
      isComplete: true,
      preserveExpanded: true,
      steps: [],
      sources: [],
      thinkingRows: [{ id: 't1', markdown: 'hello', seq: 1, durationSec: 2, startedAtMs: new Date('2026-03-10T00:00:00Z').getTime() }],
    })

    expect(html).toContain('Thought for 2s')
    expect(html).not.toContain('Thinking')
  })

  it('已完成且仅包含 thinking 时应在正文下方追加一条 done 行', () => {
    const html = renderTimeline({
      isComplete: true,
      preserveExpanded: true,
      steps: [],
      sources: [],
      thinkingRows: [{ id: 't1', markdown: 'hello', seq: 1, durationSec: 2, startedAtMs: new Date('2026-03-10T00:00:00Z').getTime() }],
    })

    expect(html).toContain('hello')
    expect(html).toContain('Done')
    expect(html).toContain('left:-19px')
  })

  it('preserveExpanded 未开启时，完成态应保持折叠而不是自动展开内容', () => {
    const html = renderTimeline({
      isComplete: true,
      steps: [
        { id: 's1', kind: 'planning', label: 'Plan step', status: 'done' },
      ],
      sources: [],
    })

    expect(html).toContain('1 step completed')
    expect(html).not.toContain('Plan step')
  })

  it('isComplete=false 时应默认展开内容', () => {
    const html = renderTimeline({
      isComplete: false,
      steps: [
        { id: 's1', kind: 'planning', label: 'Plan step', status: 'done' },
        { id: 's2', kind: 'searching', label: 'Search step', status: 'active', queries: ['hello'] },
      ],
      sources: [{ title: 'Example', url: 'https://example.com' }],
    })

    expect(html).toContain('Plan step')
  })

  it('单条代码执行也应显示时间轴圆点', () => {
    const html = renderTimeline({
      isComplete: false,
      steps: [],
      sources: [],
      codeExecutions: [{ id: 'ce-1', language: 'python', code: 'print(1)', status: 'running' }],
    })

    expect(html).toContain('Python')
    expect(html).toContain('left:-19px')
    expect(html).toContain('width:8px;height:8px;border-radius:50%')
  })

  it('单条 sub-agent 也应显示时间轴圆点', () => {
    const html = renderTimeline({
      isComplete: false,
      steps: [],
      sources: [],
      subAgents: [{ id: 'sa-1', nickname: 'WikiFetcher', personaId: 'normal', status: 'active' }],
    })

    expect(html).toContain('WikiFetcher')
    expect(html).toContain('left:-19px')
    expect(html).toContain('width:8px;height:8px;border-radius:50%')
  })

  it('统一时间线应按 seq 交错排序不同类型条目', () => {
    const html = renderTimeline({
      isComplete: false,
      steps: [
        { id: 's1', kind: 'searching', label: 'Searching', status: 'done', seq: 30 },
      ],
      sources: [],
      narratives: [
        { id: 'n1', text: '先整理一下现有工具。', seq: 20 },
      ],
      fileOps: [
        { id: 'op1', toolName: 'load_tools', label: 'load_tools "exec_command"', status: 'success', seq: 10 },
      ],
    })

    const fileOpIndex = html.indexOf('load_tools &quot;exec_command&quot;')
    const narrativeIndex = html.indexOf('先整理一下现有工具。')
    const stepIndex = html.lastIndexOf('Search completed')
    expect(fileOpIndex).toBeGreaterThanOrEqual(0)
    expect(narrativeIndex).toBeGreaterThan(fileOpIndex)
    expect(stepIndex).toBeGreaterThan(narrativeIndex)
    expect(html).not.toContain('Finished')
  })

  it('搜索 fallback 步骤在完成后应显示完成态文案', () => {
    const html = renderTimeline({
      isComplete: true,
      preserveExpanded: true,
      steps: [
        { id: 's1', kind: 'searching', label: 'Searching', status: 'done', seq: 30 },
      ],
      sources: [],
    })

    expect(html).toContain('Search completed')
    expect(html).not.toContain('>Searching<')
  })

  it('历史标题应直接显示完整文本，不走打字机', async () => {
    const { container, cleanup } = await renderHeaderLabelDom({
      text: 'Reviewed 6 sources',
      incremental: false,
    })

    expect(container.textContent).toBe('Reviewed 6 sources')
    cleanup()
  })

  it('实时标题应使用打字机效果', async () => {
    const { container, cleanup } = await renderHeaderLabelDom({
      text: 'Reviewed 6 sources',
      incremental: true,
    })

    expect(container.textContent).not.toBe('Reviewed 6 sources')
    await flushTypingFrames([40, 80, 120, 160, 200, 240, 280, 320, 360, 400, 440, 480, 520, 560])
    expect(container.textContent).toBe('Reviewed 6 sources')
    cleanup()
  })

  it('live 搜索步骤与 query 应整项进入，不逐字打字', async () => {
    const { container, cleanup } = await renderTimelineDom({
      isComplete: false,
      preserveExpanded: true,
      live: true,
      steps: [
        {
          id: 's1',
          kind: 'searching',
          label: 'Searching',
          status: 'active',
          queries: ['AI chat product competitive landscape 2025 2026 open source vs closed source'],
          seq: 1,
        },
      ],
      sources: [],
    })

    expect(container.textContent ?? '').toContain('AI chat product competitive landscape 2025 2026 open source vs closed source')
    await flushTypingFrames([40, 80, 120, 160, 200, 240, 280, 320])
    expect(container.textContent ?? '').toContain('Searching')
    cleanup()
  })

  it('交错 thinking 流式时默认仅显示标题，不直接暴露 think 正文', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-03-10T00:00:03Z'))
    const html = renderTimeline({
      isComplete: false,
      steps: [],
      sources: [],
      fileOps: [{ id: 'op1', toolName: 'grep', label: 'x', status: 'success', seq: 2 }],
      thinkingRows: [{ id: 't1', markdown: 'hello world', live: true, seq: 1 }],
      thinkingStartedAt: new Date('2026-03-10T00:00:00Z').getTime(),
      thinkingHint: 'Planning next moves',
    })
    expect(html).toContain('Planning next moves for 3s')
    expect(html).not.toContain('hello world')
  })

  it('mixed segment 在默认折叠时不直接显示 thought 摘要行', () => {
    const html = renderTimeline({
      isComplete: false,
      steps: [],
      sources: [],
      fileOps: [{ id: 'op1', toolName: 'grep', label: 'x', status: 'success', seq: 2 }],
      thinkingRows: [{ id: 't1', markdown: 'done', live: false, seq: 1, durationSec: 8, startedAtMs: new Date('2026-03-10T00:00:00Z').getTime() }],
    })
    expect(html).toContain('Thought for 8s')
    expect(html).not.toContain('done')
  })

  it('pending thinking shell 收到 think 前应先显示提示句，不立即追加 Thought', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-03-10T00:00:02Z'))
    const html = renderTimeline({
      isComplete: false,
      steps: [],
      sources: [],
      thinkingRows: [],
      thinkingStartedAt: new Date('2026-03-10T00:00:00Z').getTime(),
      thinkingHint: 'Planning next moves',
      live: true,
      shimmer: true,
    })
    expect(html).toContain('Planning next moves...')
    expect(html).not.toContain('Thought for')
  })

  it('thinking 计时只从首个真实 think 开始，不把 TTFT 算进去', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-03-10T00:00:07Z'))
    const html = renderTimeline({
      isComplete: false,
      steps: [],
      sources: [],
      thinkingRows: [{ id: 't1', markdown: 'solo', live: true, seq: 1, startedAtMs: new Date('2026-03-10T00:00:05Z').getTime() }],
      thinkingStartedAt: new Date('2026-03-10T00:00:05Z').getTime(),
      thinkingHint: 'Planning next moves',
    })
    expect(html).toContain('Planning next moves for 2s')
    expect(html).not.toContain('Planning next moves for 7s')
  })

  it('仅 thinking 时默认折叠，只显示标题', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-03-10T00:00:02Z'))
    const html = renderTimeline({
      isComplete: false,
      steps: [],
      sources: [],
      thinkingRows: [{ id: 't1', markdown: 'solo', live: true, seq: 1, startedAtMs: new Date('2026-03-10T00:00:00Z').getTime() }],
      thinkingStartedAt: new Date('2026-03-10T00:00:00Z').getTime(),
      thinkingHint: 'Planning next moves',
    })
    expect(html).toContain('Planning next moves for 2s')
    expect(html).not.toContain('solo')
  })

  it('live thinking 计时递增时不应每秒重打整句', async () => {
    vi.useFakeTimers()
    installAnimationFrameMock()
    vi.setSystemTime(new Date('2026-03-10T00:00:02Z'))
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <CopTimeline
            isComplete={false}
            steps={[]}
            sources={[]}
            thinkingRows={[{ id: 't1', markdown: 'solo', live: true, seq: 1 }]}
            thinkingStartedAt={new Date('2026-03-10T00:00:00Z').getTime()}
            thinkingHint="Planning next moves"
          />
        </LocaleProvider>,
      )
    })

    expect(container.textContent).toBe('')

    await flushTypingFrames([60, 180, 320, 520, 760, 1040])

    expect(container.textContent).toContain('Planning next moves for 2s')

    await act(async () => {
      vi.advanceTimersByTime(1000)
    })

    await flushTypingFrames([1140])
    expect(container.textContent).toContain('Planning next moves for 2s')

    await flushTypingFrames([1280])

    expect(container.textContent).toContain('Planning next moves for 3s')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('仅 thinking 且已结束时默认折叠，不直接显示正文', () => {
    const html = renderTimeline({
      isComplete: true,
      steps: [],
      sources: [],
      thinkingRows: [{ id: 't1', markdown: '内心独白一段', live: false, seq: 1, durationSec: 2 }],
    })
    expect(html).toContain('Thought for 2s')
    expect(html).not.toContain('内心独白一段')
  })

  it('仅 thinking 的 segment 展开后直接显示 think 正文', async () => {
    const { container, cleanup } = await renderTimelineDom({
      isComplete: true,
      trailingAssistantTextPresent: true,
      steps: [],
      sources: [],
      thinkingRows: [{ id: 't1', markdown: 'solo think body', live: false, seq: 1, durationSec: 2 }],
    })

    await act(async () => {
      container.querySelector('button')?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    expect(container.textContent).toContain('solo think body')
    expect(container.querySelector('[data-testid="cop-thought-summary-row"]')).toBeNull()
    cleanup()
  })

  it('仅 thinking live 展开时应使用 timeline-plain 排版而不是 think card 排版', async () => {
    const { container, cleanup } = await renderTimelineDom({
      isComplete: false,
      live: true,
      trailingAssistantTextPresent: true,
      steps: [],
      sources: [],
      thinkingRows: [{ id: 't1', markdown: 'solo think body', live: true, seq: 1, startedAtMs: new Date('2026-03-10T00:00:00Z').getTime() }],
      thinkingHint: 'Planning next moves',
    })

    await act(async () => {
      container.querySelector('button')?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    expect(container.querySelector('.cop-thinking-output-md--timeline-plain')).not.toBeNull()
    expect(container.querySelector('.cop-thinking-card-outer')).toBeNull()
    cleanup()
  })

  it('标题从 thinking 切到 thought 时保留共同尾巴并增量打到 Thought for 2s', async () => {
    vi.useFakeTimers()
    installAnimationFrameMock()
    vi.setSystemTime(new Date('2026-03-10T00:00:02Z'))
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <CopTimeline
            isComplete={false}
            steps={[]}
            sources={[]}
            thinkingRows={[{ id: 't1', markdown: 'solo', live: true, seq: 1 }]}
            thinkingStartedAt={new Date('2026-03-10T00:00:00Z').getTime()}
            thinkingHint="Planning next moves"
          />
        </LocaleProvider>,
      )
    })

    await flushTypingFrames([60, 180, 320, 520, 760, 1040])

    expect(container.textContent).toContain('Planning next moves for 2s')

    await act(async () => {
      root.render(
        <LocaleProvider>
          <CopTimeline
            isComplete={true}
            steps={[]}
            sources={[]}
            thinkingRows={[{ id: 't1', markdown: 'solo', live: false, seq: 1, durationSec: 2 }]}
            thinkingStartedAt={new Date('2026-03-10T00:00:00Z').getTime()}
          />
        </LocaleProvider>,
      )
    })

    expect(container.textContent).toContain('Planning next moves for 2s')

    expect(container.textContent).toContain('Planning next moves for 2s')

    await flushTypingFrames([1120])
    expect(container.textContent).toContain(' for 2s')
    expect(container.textContent).not.toContain('Thought for 2s')

    await flushTypingFrames([1240, 1360])
    expect(container.textContent).toContain('Thought for 2s')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('标题从提示句切到带计时时只补尾巴，不硬切到 Planning next moves for Ns', async () => {
    vi.useFakeTimers()
    installAnimationFrameMock()
    vi.setSystemTime(new Date('2026-03-10T00:00:02Z'))
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <CopTimeline
            isComplete={false}
            live
            steps={[]}
            sources={[]}
            thinkingRows={[]}
            thinkingHint="Planning next moves"
          />
        </LocaleProvider>,
      )
    })

    expect(container.textContent).toBe('')

    await flushTypingFrames([60, 180, 320, 520, 760])

    expect(container.textContent).toContain('Planning next moves...')

    await act(async () => {
      root.render(
        <LocaleProvider>
          <CopTimeline
            isComplete={false}
            steps={[]}
            sources={[]}
            thinkingRows={[{ id: 't1', markdown: 'solo', live: true, seq: 1 }]}
            thinkingStartedAt={new Date('2026-03-10T00:00:00Z').getTime()}
            thinkingHint="Planning next moves"
          />
        </LocaleProvider>,
      )
    })

    const header = container.querySelector('button')
    expect(header?.textContent ?? '').toContain('Planning next moves')
    expect(header?.textContent ?? '').not.toContain('Planning next moves for 2s')

    await flushTypingFrames([860, 980, 1120])
    expect(header?.textContent ?? '').toMatch(/Planning next moves for 2s/)

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('自定义动态标题切换时应继续打字，而不是整句硬切', async () => {
    vi.useFakeTimers()
    installAnimationFrameMock()
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <CopTimeline
            isComplete={false}
            live
            steps={[]}
            sources={[]}
            headerOverride="Impressions"
          />
        </LocaleProvider>,
      )
    })

    await flushTypingFrames([60, 180, 320, 520])
    expect(container.textContent).toContain('Impressions')

    await act(async () => {
      root.render(
        <LocaleProvider>
          <CopTimeline
            isComplete={false}
            live
            steps={[]}
            sources={[]}
            headerOverride="Translating thoughts to words for 4s"
          />
        </LocaleProvider>,
      )
    })

    expect(container.textContent).toContain('Impressions')
    expect(container.textContent).not.toContain('Translating thoughts to words for 4s')

    await flushTypingFrames([620])
    expect(container.textContent).not.toContain('Translating thoughts to words for 4s')

    await flushTypingFrames([760, 940, 1180, 1460, 1700])
    expect(container.textContent).toContain('Translating thoughts to words for 4s')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('自定义标题补秒数时只改数字，不在第一帧硬切到新秒数', async () => {
    vi.useFakeTimers()
    installAnimationFrameMock()
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <CopTimeline
            isComplete={false}
            live
            steps={[]}
            sources={[]}
            headerOverride="Translating thoughts to words for 4s"
          />
        </LocaleProvider>,
      )
    })

    await flushTypingFrames([60, 180, 320, 520, 760, 1040, 1320])
    expect(container.textContent).toContain('Translating thoughts to words for 4s')

    await act(async () => {
      root.render(
        <LocaleProvider>
          <CopTimeline
            isComplete={false}
            live
            steps={[]}
            sources={[]}
            headerOverride="Translating thoughts to words for 5s"
          />
        </LocaleProvider>,
      )
    })

    expect(container.textContent).toContain('Translating thoughts to words for 4s')

    await flushTypingFrames([1380])
    expect(container.textContent).toContain('Translating thoughts to words for 4s')
    expect(container.textContent).not.toContain('Translating thoughts to words for 5s')

    await flushTypingFrames([1520])
    expect(container.textContent).toContain('Translating thoughts to words for 5s')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('pending 提示句应从空串逐字打到完整文案', async () => {
    vi.useFakeTimers()
    installAnimationFrameMock()
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <CopTimeline
            isComplete={false}
            live
            shimmer
            steps={[]}
            sources={[]}
            thinkingRows={[]}
            thinkingHint="Planning next moves"
          />
        </LocaleProvider>,
      )
    })

    expect(container.textContent).toBe('')

    await flushTypingFrames([60])
    expect((container.textContent ?? '').length).toBeGreaterThan(0)
    expect(container.textContent).not.toContain('Planning next moves...')

    await flushTypingFrames([180, 320, 520, 760])
    expect(container.textContent).toContain('Planning next moves...')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('thinking live 结束时不应硬切，第一帧仍保留共同尾巴', async () => {
    vi.useFakeTimers()
    installAnimationFrameMock()
    vi.setSystemTime(new Date('2026-03-10T00:00:02Z'))
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <CopTimeline
            isComplete={false}
            steps={[]}
            sources={[]}
            thinkingRows={[{ id: 't1', markdown: 'solo', live: true, seq: 1 }]}
            thinkingStartedAt={new Date('2026-03-10T00:00:00Z').getTime()}
            thinkingHint="Planning next moves"
          />
        </LocaleProvider>,
      )
    })
    await flushTypingFrames([60, 180, 320, 520, 760, 1040])
    expect(container.textContent).toContain('Planning next moves for 2s')

    await act(async () => {
      root.render(
        <LocaleProvider>
          <CopTimeline
            isComplete
            steps={[]}
            sources={[]}
            thinkingRows={[{ id: 't1', markdown: 'solo', live: false, seq: 1, durationSec: 2 }]}
          />
        </LocaleProvider>,
      )
    })

    expect(container.textContent).toContain('Planning next moves for 2s')

    await flushTypingFrames([1240])
    expect(container.textContent).toContain(' for 2s')
    expect(container.textContent).not.toContain('Planning next moves for 2s')
    expect(container.textContent).not.toContain('Thought for 2s')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('mixed summary 行从 Thinking 切到 Thought 时保留共同尾巴，不先清空整句', async () => {
    vi.useFakeTimers()
    installAnimationFrameMock()
    vi.setSystemTime(new Date('2026-03-10T00:00:02Z'))
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <CopTimeline
            isComplete={false}
            preserveExpanded
            steps={[]}
            sources={[]}
            fileOps={[{ id: 'op1', toolName: 'grep', label: 'x', status: 'success', seq: 2 }]}
            thinkingRows={[{ id: 't1', markdown: 'done', live: true, seq: 1, startedAtMs: new Date('2026-03-10T00:00:00Z').getTime() }]}
            thinkingStartedAt={new Date('2026-03-10T00:00:00Z').getTime()}
          />
        </LocaleProvider>,
      )
    })

    await flushTypingFrames([60, 180, 320, 520, 760])
    const summaryButton = container.querySelector('[data-testid="cop-thought-summary-row"]')
    expect(summaryButton?.textContent ?? '').toContain('Thinking for 2s')

    await act(async () => {
      root.render(
        <LocaleProvider>
          <CopTimeline
            isComplete
            preserveExpanded
            steps={[]}
            sources={[]}
            fileOps={[{ id: 'op1', toolName: 'grep', label: 'x', status: 'success', seq: 2 }]}
            thinkingRows={[{ id: 't1', markdown: 'done', live: false, seq: 1, durationSec: 2 }]}
          />
        </LocaleProvider>,
      )
    })

    await flushTypingFrames([880])
    expect(summaryButton?.textContent ?? '').toContain(' for 2s')
    expect(summaryButton?.textContent ?? '').not.toContain('Thought for 2s')

    await flushTypingFrames([1020, 1160])
    expect(summaryButton?.textContent ?? '').toContain('Thought for 2s')

    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('mixed segment 展开后不显示 think 正文，只显示 thought 摘要和工具链', async () => {
    const { container, cleanup } = await renderTimelineDom({
      isComplete: true,
      steps: [],
      sources: [],
      fileOps: [{ id: 'op1', toolName: 'load_tools', label: 'load_tools "abc"', status: 'success', seq: 2 }],
      thinkingRows: [{ id: 't1', markdown: 'hidden think body', live: false, seq: 1, durationSec: 8, startedAtMs: new Date('2026-03-10T00:00:00Z').getTime() }],
    })

    await act(async () => {
      container.querySelector('button')?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    await flushTypingFrames([40, 80, 120, 160, 200, 240, 280, 320, 360, 400, 440, 480])
    expect(container.textContent).toContain('Thought for 8s')
    expect(container.textContent).toContain('load_tools "abc"')
    expect(container.textContent).not.toContain('hidden think body')
    expect(container.querySelector('[data-testid="cop-thought-summary-row"]')).not.toBeNull()
    cleanup()
  })

  it('live mixed segment 一旦出现工具调用应默认展开，但继续隐藏 think 正文', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-03-10T00:00:02Z'))
    const { container, cleanup } = await renderTimelineDom({
      isComplete: false,
      live: true,
      steps: [],
      sources: [],
      fileOps: [{ id: 'op1', toolName: 'load_tools', label: 'load_tools "abc"', status: 'running', seq: 2 }],
      thinkingRows: [{ id: 't1', markdown: 'hidden think body', live: true, seq: 1, startedAtMs: new Date('2026-03-10T00:00:00Z').getTime() }],
      thinkingHint: 'Planning next moves',
    })

    await act(async () => {
      vi.advanceTimersByTime(800)
    })

    await flushTypingFrames([60, 180, 320, 520, 760, 1040])
    expect(container.textContent).toContain('load_tools "abc"')
    expect(container.textContent).toContain('Thinking for 2s')
    expect(container.textContent).not.toContain('hidden think body')
    cleanup()
  })

  it('mixed segment 的 thought 摘要可展开查看对应 think 内容', async () => {
    const { container, cleanup } = await renderTimelineDom({
      isComplete: true,
      preserveExpanded: true,
      steps: [],
      sources: [],
      fileOps: [{ id: 'op1', toolName: 'load_tools', label: 'load_tools "abc"', status: 'success', seq: 2 }],
      thinkingRows: [{ id: 't1', markdown: 'recover previous think', live: false, seq: 1, durationSec: 1, startedAtMs: new Date('2026-03-10T00:00:00Z').getTime() }],
    })

    const thoughtRow = container.querySelector('[data-testid="cop-thought-summary-row"]')
    if (!(thoughtRow instanceof HTMLButtonElement)) {
      throw new Error('thought summary button not found')
    }

    await act(async () => {
      thoughtRow.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    expect(container.textContent).toContain('recover previous think')
    cleanup()
  })

  it('segment 关闭时若用户未手动切换，应立即自动收起（title 保留，body 收起）', async () => {
    vi.useFakeTimers()
    installAnimationFrameMock()
    vi.setSystemTime(new Date('2026-03-10T00:00:02Z'))
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <CopTimeline
            isComplete={false}
            live
            steps={[]}
            sources={[]}
            fileOps={[{ id: 'op1', toolName: 'load_tools', label: 'load_tools "abc"', status: 'running', seq: 2 }]}
            thinkingRows={[{ id: 't1', markdown: 'hidden think body', live: true, seq: 1, startedAtMs: new Date('2026-03-10T00:00:00Z').getTime() }]}
            thinkingHint="Planning next moves"
          />
        </LocaleProvider>,
      )
    })

    await act(async () => {
      vi.advanceTimersByTime(800)
    })

    await act(async () => {
      root.render(
        <LocaleProvider>
          <CopTimeline
            isComplete={false}
            steps={[]}
            sources={[]}
            fileOps={[{ id: 'op1', toolName: 'load_tools', label: 'load_tools "abc"', status: 'success', seq: 2 }]}
            thinkingRows={[{ id: 't1', markdown: 'hidden think body', live: false, seq: 1, durationSec: 2, startedAtMs: new Date('2026-03-10T00:00:00Z').getTime() }]}
          />
        </LocaleProvider>,
      )
    })

    await flushTypingFrames([1120, 1260, 1400])
    expect(container.textContent).toContain('Thought for 2s')
    act(() => {
      root.unmount()
    })
    container.remove()
  })

  it('web_fetch 遇到 file 地址时不应渲染坏掉的 favicon', () => {
    const html = renderTimeline({
      isComplete: false,
      steps: [],
      sources: [],
      webFetches: [
        { id: 'wf-1', url: 'file:///Users/demo/runtime/skills/demo/SKILL.md', status: 'failed' },
      ],
    })

    expect(html).toContain('file:///Users/demo/runtime/skills/demo/SKILL.md')
    expect(html).toContain('>file<')
    expect(html).not.toContain('google.com/s2/favicons')
  })

  it('reviewing 行优先渲染步骤自带的 scoped sources', () => {
    const html = renderTimeline({
      isComplete: false,
      steps: [
        {
          id: 's1',
          kind: 'reviewing',
          label: 'Reviewing sources',
          status: 'done',
          seq: 2,
          sources: [{ title: 'Scoped', url: 'https://scoped.test' }],
        },
      ],
      sources: [{ title: 'Global', url: 'https://global.test' }],
    })

    expect(html).toContain('scoped.test')
    expect(html).not.toContain('global.test')
  })

  it('generic tool 也按统一时间线渲染', () => {
    const html = renderTimeline({
      isComplete: false,
      steps: [],
      sources: [],
      genericTools: [{ id: 'g1', toolName: 'fetch_url', label: 'fetch_url', status: 'running', seq: 1 }],
    })

    expect(html).toContain('fetch_url')
  })

})
