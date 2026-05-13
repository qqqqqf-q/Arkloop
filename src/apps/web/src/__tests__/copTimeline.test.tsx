import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { CopTimeline } from '../components/cop-timeline/CopTimeline'
import { CopTimelineHeaderLabel } from '../components/cop-timeline/CopTimelineHeader'
import type { CopSubSegment, ResolvedPool } from '../copSubSegment'
import { EMPTY_POOL } from '../copSubSegment'
import { LocaleProvider } from '../contexts/LocaleContext'

const incrementalTypewriterMock = vi.hoisted(() => vi.fn((text: string) => text))

vi.mock('../hooks/useTypewriter', () => ({
  useTypewriter: (text: string) => text,
}))

vi.mock('../hooks/useIncrementalTypewriter', () => ({
  useIncrementalTypewriter: incrementalTypewriterMock,
}))

globalThis.scrollTo = (() => {}) as typeof globalThis.scrollTo

const originalMatchMedia = window.matchMedia
const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

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
  window.matchMedia = vi.fn(defaultMatchMedia)
  incrementalTypewriterMock.mockClear()
})

afterEach(() => {
  window.matchMedia = originalMatchMedia
  if (originalActEnvironment === undefined) {
    delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
  } else {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
  }
})

function makePool(overrides?: Partial<ResolvedPool>): ResolvedPool {
  return { ...EMPTY_POOL, ...overrides }
}

function makeSeg(overrides?: Partial<CopSubSegment>): CopSubSegment {
  return {
    id: 'seg1',
    category: 'explore',
    status: 'closed',
    items: [],
    seq: 0,
    title: 'Test segment',
    ...overrides,
  }
}

function renderTimeline(props: Parameters<typeof CopTimeline>[0]): string {
  const prev = window.matchMedia
  window.matchMedia = vi.fn(reducedMotionMatchMedia)
  try {
    return renderToStaticMarkup(
      <LocaleProvider>
        <CopTimeline {...props} />
      </LocaleProvider>,
    )
  } finally {
    window.matchMedia = prev
  }
}

async function renderTimelineDom(props: Parameters<typeof CopTimeline>[0]) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  await act(async () => {
    root.render(
      <LocaleProvider>
        <CopTimeline {...props} />
      </LocaleProvider>,
    )
  })
  return {
    container,
    rerender: async (next: Parameters<typeof CopTimeline>[0]) => {
      await act(async () => {
        root.render(
          <LocaleProvider>
            <CopTimeline {...next} />
          </LocaleProvider>,
        )
      })
    },
    cleanup: () => {
      act(() => { root.unmount() })
      container.remove()
    },
  }
}

async function renderHeaderLabelDom(params: { text: string; phaseKey?: string; incremental?: boolean }) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  await act(async () => {
    root.render(
      <LocaleProvider>
        <CopTimelineHeaderLabel
          text={params.text}
          phaseKey={params.phaseKey ?? 'test'}
          incremental={params.incremental}
        />
      </LocaleProvider>,
    )
  })
  return {
    container,
    cleanup: () => {
      act(() => { root.unmount() })
      container.remove()
    },
  }
}

describe('CopTimeline', () => {
  describe('shouldRender gate', () => {
    it('empty segments, no thinkingOnly, no thinkingHint -> renders null', () => {
      const html = renderTimeline({
        segments: [],
        pool: EMPTY_POOL,
        isComplete: false,
      })
      expect(html).toBe('')
    })

    it('thinkingHint + live with no segments -> renders pending header', async () => {
      const { container, cleanup } = await renderTimelineDom({
        segments: [],
        pool: EMPTY_POOL,
        isComplete: false,
        live: true,
        thinkingHint: 'Planning next moves',
      })
      try {
        const span = container.querySelector('[data-phase="thinking-pending"]')
        expect(span).not.toBeNull()
        expect(span?.textContent).toContain('Planning next moves')
      } finally {
        cleanup()
      }
    })

    it('only work typography adds the work root class', () => {
      const props = {
        segments: [],
        pool: EMPTY_POOL,
        isComplete: false,
        live: true,
        thinkingHint: 'Planning next moves',
      } satisfies Parameters<typeof CopTimeline>[0]

      const defaultHtml = renderTimeline(props)
      const workHtml = renderTimeline({ ...props, typography: 'work' })

      expect(defaultHtml).toContain('class="cop-timeline-root"')
      expect(defaultHtml).not.toContain('cop-timeline-root--work')
      expect(workHtml).toContain('class="cop-timeline-root cop-timeline-root--work"')
    })

    it('重复搜索 query 不触发 React key 警告', async () => {
      const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
      const segment = makeSeg({
        category: 'search',
        items: [{
          kind: 'call',
          seq: 1,
          call: { toolCallId: 'search-1', toolName: 'web_search', arguments: {} },
        }],
        title: 'Search',
      })
      const pool = makePool({
        steps: new Map([['search-1', {
          id: 'search-1',
          kind: 'searching',
          label: 'Searching',
          status: 'done',
          queries: ['Arkloop AI assistant open source', 'Arkloop AI assistant open source'],
          seq: 1,
        }]]),
      })
      const { container, cleanup } = await renderTimelineDom({
        segments: [segment],
        pool,
        isComplete: true,
      })

      try {
        expect(container.textContent).toContain('Arkloop AI assistant open source')
        expect(consoleError.mock.calls.some((call) => String(call[0]).includes('Encountered two children with the same key'))).toBe(false)
      } finally {
        cleanup()
        consoleError.mockRestore()
      }
    })

    it('code execution pool without timeline segments renders nothing', () => {
      const pool = makePool({
        codeExecutions: new Map([['ce1', { id: 'ce1', code: 'echo hi' } as unknown as import('../storage').CodeExecutionRef]]),
      })
      const html = renderTimeline({
        segments: [],
        pool,
        isComplete: true,
      })
      expect(html).toBe('')
    })
  })

  describe('thinking-only mode', () => {
    it('complete thinkingOnly collapses body by default', () => {
      const html = renderTimeline({
        segments: [],
        pool: EMPTY_POOL,
        thinkingOnly: { markdown: 'Let me think', durationSec: 3, live: false },
        isComplete: true,
      })
      expect(html).toContain('data-phase="thought"')
      // framer-motion SSR renders collapsed state as height:0
      expect(html).toContain('height:0')
    })

    it('complete thinkingOnly with durationSec > 0 shows thoughtDuration label', () => {
      const html = renderTimeline({
        segments: [],
        pool: EMPTY_POOL,
        thinkingOnly: { markdown: 'some thinking', durationSec: 5, live: false },
        isComplete: true,
      })
      expect(html).toContain('已思考 5s')
    })

    it('complete thinkingOnly with durationSec=0 shows "Thought"', () => {
      const html = renderTimeline({
        segments: [],
        pool: EMPTY_POOL,
        thinkingOnly: { markdown: 'think', durationSec: 0, live: false },
        isComplete: true,
      })
      expect(html).toContain('已思考')
    })

    it('live thinkingOnly shows thinking-live phase key', async () => {
      const { container, cleanup } = await renderTimelineDom({
        segments: [],
        pool: EMPTY_POOL,
        thinkingOnly: { markdown: 'live thought', durationSec: 0, live: true },
        isComplete: false,
        live: true,
      })
      try {
        const span = container.querySelector('[data-phase="thinking-live"]')
        expect(span).not.toBeNull()
      } finally {
        cleanup()
      }
    })

    it('live thinkingOnly with hint shows hint in header', async () => {
      const { container, cleanup } = await renderTimelineDom({
        segments: [],
        pool: EMPTY_POOL,
        thinkingOnly: { markdown: 'live thought', durationSec: 0, live: true, startedAtMs: Date.now() },
        isComplete: false,
        live: true,
        thinkingHint: 'Working through this',
      })
      try {
        const span = container.querySelector('[data-phase="thinking-live"]')
        expect(span?.textContent).toContain('Working through this')
      } finally {
        cleanup()
      }
    })
  })

  describe('segment rendering', () => {
    it('single exec segment with closed status renders inside CopTimeline', () => {
      const seg = makeSeg({
        id: 'seg1',
        category: 'exec',
        status: 'closed',
        title: '1 steps completed',
        items: [{ kind: 'call', call: { toolCallId: 'ce1', toolName: 'exec_command', arguments: {} }, seq: 0 }],
      })
      const html = renderTimeline({
        segments: [seg],
        pool: EMPTY_POOL,
        isComplete: true,
      })
      expect(html).not.toBe('')
      expect(html).toContain('完成 1 步')
    })

    it('exec segment renders outside header collapse, not as nested segment', () => {
      const seg = makeSeg({
        id: 'seg1',
        category: 'exec',
        status: 'closed',
        title: '1 steps completed',
        items: [{ kind: 'call', call: { toolCallId: 'ce1', toolName: 'exec_command', arguments: {} }, seq: 0 }],
      })
      const html = renderTimeline({
        segments: [seg],
        pool: EMPTY_POOL,
        isComplete: true,
      })
      // exec-only: no header button should render
      expect(html).not.toContain('1 steps completed')
    })

    it('exec-only live segment ignores thinking hint header', () => {
      const seg = makeSeg({
        id: 'seg1',
        category: 'exec',
        status: 'open',
        title: 'Running...',
        items: [{ kind: 'call', call: { toolCallId: 'ce1', toolName: 'exec_command', arguments: {} }, seq: 0 }],
      })
      const html = renderTimeline({
        segments: [seg],
        pool: EMPTY_POOL,
        isComplete: false,
        live: true,
        thinkingHint: 'Gathering my thoughts',
      })
      expect(html).not.toContain('Gathering my thoughts')
      expect(html).not.toContain('Running...')
    })

    it('exec segment is not collapsed when mixed with non-exec segments', () => {
      const seg1 = makeSeg({ id: 'seg1', category: 'explore', status: 'closed', title: 'Read 2 files', seq: 0 })
      const seg2 = makeSeg({
        id: 'seg2',
        category: 'exec',
        status: 'closed',
        title: '1 steps completed',
        items: [{ kind: 'call', call: { toolCallId: 'ce1', toolName: 'exec_command', arguments: {} }, seq: 1 }],
      })
      const html = renderTimeline({
        segments: [seg1, seg2],
        pool: EMPTY_POOL,
        isComplete: true,
      })
      // header should only reflect non-exec segments
      expect(html).not.toContain('1 steps completed')
      expect(html).toContain('Read 2 files')
    })

    it('multiple segments rendered in order', () => {
      const seg1 = makeSeg({ id: 'seg1', title: 'First segment', seq: 0, status: 'closed', category: 'generic' })
      const seg2 = makeSeg({ id: 'seg2', title: 'Second segment', seq: 1, status: 'closed', category: 'edit' })
      const html = renderTimeline({
        segments: [seg1, seg2],
        pool: EMPTY_POOL,
        isComplete: true,
      })
      const idx1 = html.indexOf('First segment')
      const idx2 = html.indexOf('Second segment')
      expect(idx1).toBeGreaterThan(-1)
      expect(idx2).toBeGreaterThan(-1)
      expect(idx1).toBeLessThan(idx2)
    })

    it('does not guess-translate legacy composite segment titles', () => {
      const seg = makeSeg({
        id: 'seg1',
        category: 'explore',
        status: 'closed',
        title: 'Read 9 files, 10 searches, Listed 3 files',
        items: [],
      })
      const html = renderTimeline({
        segments: [seg],
        pool: EMPTY_POOL,
        isComplete: true,
      })
      expect(html).toContain('完成 1 步')
      expect(html).not.toContain('Read 9 files, 10 searches, Listed 3 files')
      expect(html).not.toContain('正在搜索 for')
    })

    it('uses display name for enter_plan_mode titles', () => {
      const seg = makeSeg({
        id: 'seg1',
        category: 'generic',
        status: 'closed',
        title: 'enter_plan_mode',
        items: [{ kind: 'call', call: { toolCallId: 'plan_1', toolName: 'enter_plan_mode', arguments: {} }, seq: 0 }],
      })
      const html = renderTimeline({
        segments: [seg],
        pool: EMPTY_POOL,
        isComplete: true,
      })
      expect(html).toContain('进入计划模式')
      expect(html).not.toContain('enter_plan_mode')
    })

    it('segment with status=open shows its title', () => {
      const seg = makeSeg({
        id: 'seg1',
        category: 'explore',
        status: 'open',
        title: 'Exploring code...',
        items: [],
      })
      const html = renderTimeline({
        segments: [seg],
        pool: EMPTY_POOL,
        isComplete: false,
        live: true,
      })
      expect(html).toContain('正在查看代码...')
    })

    it('segment with status=closed shows its completed title', () => {
      const seg1 = makeSeg({
        id: 'seg1',
        category: 'edit',
        status: 'closed',
        title: 'Edited foo.ts',
        items: [{ kind: 'call', call: { toolCallId: 'c1', toolName: 'edit_file', arguments: {} }, seq: 0 }],
      })
      const seg2 = makeSeg({
        id: 'seg2',
        category: 'explore',
        status: 'closed',
        title: 'Read 1 file',
        items: [{ kind: 'call', call: { toolCallId: 'c2', toolName: 'read_file', arguments: {} }, seq: 1 }],
      })
      const html = renderTimeline({
        segments: [seg1, seg2],
        pool: EMPTY_POOL,
        isComplete: true,
      })
      expect(html).toContain('Edited foo.ts')
    })

    it('write_file completed title stays Wrote instead of Edited', () => {
      const seg = makeSeg({
        id: 'seg1',
        category: 'edit',
        status: 'closed',
        title: 'Wrote hello.py',
        items: [{ kind: 'call', call: { toolCallId: 'c1', toolName: 'write_file', arguments: { file_path: '/tmp/hello.py' } }, seq: 0 }],
      })
      const html = renderTimeline({
        segments: [seg],
        pool: EMPTY_POOL,
        isComplete: true,
      })
      expect(html).toContain('写入 hello.py')
      expect(html).not.toContain('Edited hello.py')
    })
  })

  describe('header label and phaseKey', () => {
    it('isComplete with segments -> data-phase="complete"', () => {
      const seg = makeSeg({ id: 's1', status: 'closed', title: '1 steps completed' })
      const html = renderTimeline({
        segments: [seg],
        pool: EMPTY_POOL,
        isComplete: true,
      })
      expect(html).toContain('data-phase="complete"')
    })

    it('headerOverride overrides auto-generated label', () => {
      const seg = makeSeg({ id: 's1', status: 'closed', title: '1 steps completed' })
      const html = renderTimeline({
        segments: [seg],
        pool: EMPTY_POOL,
        isComplete: true,
        headerOverride: 'Custom Header Text',
      })
      expect(html).toContain('Custom Header Text')
    })

    it('live with open segments -> data-phase="live"', () => {
      const seg = makeSeg({ id: 's1', status: 'open', title: 'Working...' })
      const html = renderTimeline({
        segments: [seg],
        pool: EMPTY_POOL,
        isComplete: false,
        live: true,
      })
      expect(html).toContain('data-phase="live"')
    })

    it('live with open last segment -> header shows that segment title', () => {
      const seg1 = makeSeg({ id: 's1', status: 'closed', title: 'Read 1 file', seq: 0 })
      const seg2 = makeSeg({ id: 's2', status: 'open', title: 'Running...', seq: 1, category: 'edit' })
      const html = renderTimeline({
        segments: [seg1, seg2],
        pool: EMPTY_POOL,
        isComplete: false,
        live: true,
      })
      expect(html).toContain('编辑中...')
    })

    it('thinkingOnly + live -> data-phase="thinking-live"', () => {
      const html = renderTimeline({
        segments: [],
        pool: EMPTY_POOL,
        thinkingOnly: { markdown: 'Hmm', durationSec: 0, live: true },
        isComplete: false,
        live: true,
      })
      expect(html).toContain('data-phase="thinking-live"')
    })

    it('thinkingOnly + complete -> data-phase="thought"', () => {
      const html = renderTimeline({
        segments: [],
        pool: EMPTY_POOL,
        thinkingOnly: { markdown: 'Hmm', durationSec: 2, live: false },
        isComplete: true,
      })
      expect(html).toContain('data-phase="thought"')
    })

    it('thinkingHint pending (no segments, no thinkingOnly, live) -> data-phase="thinking-pending"', () => {
      const html = renderTimeline({
        segments: [],
        pool: EMPTY_POOL,
        isComplete: false,
        live: true,
        thinkingHint: 'Planning next steps',
      })
      expect(html).toContain('data-phase="thinking-pending"')
      expect(html).toContain('Planning next steps...')
    })

    it('complete with N segments -> header shows localized step count', () => {
      const seg1 = makeSeg({ id: 's1', status: 'closed', title: 'Step 1', seq: 0 })
      const seg2 = makeSeg({ id: 's2', status: 'closed', title: 'Step 2', seq: 1, category: 'edit' })
      const html = renderTimeline({
        segments: [seg1, seg2],
        pool: EMPTY_POOL,
        isComplete: true,
      })
      expect(html).toContain('完成 2 步')
      expect(html).not.toContain('2 steps completed')
    })

    it('complete with 1 empty segment uses localized fallback title', () => {
      const seg = makeSeg({ id: 's1', status: 'closed', title: 'Step 1', seq: 0 })
      const html = renderTimeline({
        segments: [seg],
        pool: EMPTY_POOL,
        isComplete: true,
      })
      expect(html).toContain('完成 1 步')
      expect(html).not.toContain('Step 1')
      expect(html).not.toContain('1 step completed')
    })

    it('single segment body uses the scrollable card shell', () => {
      const seg = makeSeg({ id: 's1', status: 'open', title: 'Reading files', items: [
        { kind: 'call', call: { toolCallId: 'c1', toolName: 'read_file', arguments: {} }, seq: 0 },
        { kind: 'call', call: { toolCallId: 'c2', toolName: 'read_file', arguments: {} }, seq: 1 },
      ]})
      const html = renderTimeline({
        segments: [seg],
        pool: EMPTY_POOL,
        isComplete: false,
        live: true,
      })
      expect(html).toContain('class="cop-timeline-items-card"')
      expect(html).toContain('class="cop-timeline-items-card__scroll"')
      expect(html).toContain('padding-top:1px')
    })
  })

  describe('collapse and expand behavior', () => {
    it('complete timeline starts collapsed', async () => {
      const seg = makeSeg({ id: 's1', status: 'closed', title: '1 step completed', items: [
        { kind: 'call', call: { toolCallId: 'c1', toolName: 'read_file', arguments: {} }, seq: 0 },
      ]})
      const { container, cleanup } = await renderTimelineDom({
        segments: [seg],
        pool: EMPTY_POOL,
        isComplete: true,
      })
      try {
        const root = container.querySelector('.cop-timeline-root')
        expect(root).not.toBeNull()
        const motionDiv = container.querySelector('.cop-timeline-root > div:last-child') as HTMLElement | null
        expect(motionDiv?.style.overflow).toBe('hidden')
      } finally {
        cleanup()
      }
    })

    it('user click on header expands a collapsed timeline', async () => {
      const seg = makeSeg({ id: 's1', status: 'closed', title: '1 step completed', items: [
        { kind: 'call', call: { toolCallId: 'c1', toolName: 'read_file', arguments: {} }, seq: 0 },
      ]})
      const { container, cleanup } = await renderTimelineDom({
        segments: [seg],
        pool: EMPTY_POOL,
        isComplete: true,
      })
      try {
        const button = container.querySelector('button')!
        // collapsed: overflow hidden
        const motionBefore = container.querySelector('.cop-timeline-root > div:last-child') as HTMLElement | null
        expect(motionBefore?.style.overflow).toBe('hidden')

        await act(async () => { button.click() })

        // expanded: overflow visible
        const motionAfter = container.querySelector('.cop-timeline-root > div:last-child') as HTMLElement | null
        expect(motionAfter?.style.overflow).toBe('visible')
      } finally {
        cleanup()
      }
    })

    it('transition from live to complete auto-collapses', async () => {
      const seg = makeSeg({ id: 's1', status: 'open', title: 'Running...', items: [
        { kind: 'call', call: { toolCallId: 'c1', toolName: 'read_file', arguments: {} }, seq: 0 },
      ]})
      const { container, rerender, cleanup } = await renderTimelineDom({
        segments: [seg],
        pool: EMPTY_POOL,
        isComplete: false,
        live: true,
      })
      try {
        // live = expanded: overflow visible
        const motionBefore = container.querySelector('.cop-timeline-root > div:last-child') as HTMLElement | null
        expect(motionBefore?.style.overflow).toBe('visible')

        const closedSeg = { ...seg, status: 'closed' as const, title: '1 step completed' }
        await rerender({
          segments: [closedSeg],
          pool: EMPTY_POOL,
          isComplete: true,
          live: false,
        })

        // complete = collapsed: overflow hidden
        const motionAfter = container.querySelector('.cop-timeline-root > div:last-child') as HTMLElement | null
        expect(motionAfter?.style.overflow).toBe('hidden')
      } finally {
        cleanup()
      }
    })

    it('scrollable card starts pinned to bottom and updates edge shadows', async () => {
      const scrollHeightSpy = vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockImplementation(function getScrollHeight(this: HTMLElement) {
        return (this as HTMLElement).classList.contains('cop-timeline-items-card__scroll') ? 320 : 160
      })
      const clientHeightSpy = vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockImplementation(function getClientHeight(this: HTMLElement) {
        return (this as HTMLElement).classList.contains('cop-timeline-items-card__scroll') ? 120 : 160
      })
      const seg = makeSeg({ id: 's1', status: 'closed', title: 'Read files', items: [
        { kind: 'call', call: { toolCallId: 'c1', toolName: 'read_file', arguments: {} }, seq: 0 },
        { kind: 'call', call: { toolCallId: 'c2', toolName: 'read_file', arguments: {} }, seq: 1 },
      ]})
      try {
        const { container, cleanup } = await renderTimelineDom({
          segments: [seg],
          pool: EMPTY_POOL,
          isComplete: true,
          live: false,
        })
        try {
          const card = container.querySelector('.cop-timeline-items-card') as HTMLElement | null
          const scroll = container.querySelector('.cop-timeline-items-card__scroll') as HTMLElement | null
          expect(card).not.toBeNull()
          expect(scroll).not.toBeNull()
          expect(scroll?.scrollTop).toBe(320)
          expect(card?.dataset.topShadow).toBe('true')
          expect(card?.dataset.bottomShadow).toBe('false')

          await act(async () => {
            scroll!.scrollTop = 0
            scroll!.dispatchEvent(new Event('scroll', { bubbles: true }))
          })
          expect(card?.dataset.topShadow).toBe('false')
          expect(card?.dataset.bottomShadow).toBe('true')
        } finally {
          cleanup()
        }
      } finally {
        scrollHeightSpy.mockRestore()
        clientHeightSpy.mockRestore()
      }
    })

    it('live scrollable card stops following bottom after user scrolls up', async () => {
      const scrollHeightSpy = vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockImplementation(function getScrollHeight(this: HTMLElement) {
        return (this as HTMLElement).classList.contains('cop-timeline-items-card__scroll') ? 320 : 160
      })
      const clientHeightSpy = vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockImplementation(function getClientHeight(this: HTMLElement) {
        return (this as HTMLElement).classList.contains('cop-timeline-items-card__scroll') ? 120 : 160
      })
      const seg = makeSeg({ id: 's1', status: 'open', title: 'Reading files', items: [
        { kind: 'call', call: { toolCallId: 'c1', toolName: 'read_file', arguments: {} }, seq: 0 },
        { kind: 'call', call: { toolCallId: 'c2', toolName: 'read_file', arguments: {} }, seq: 1 },
      ]})
      try {
        const { container, rerender, cleanup } = await renderTimelineDom({
          segments: [seg],
          pool: EMPTY_POOL,
          isComplete: false,
          live: true,
        })
        try {
          const scroll = container.querySelector('.cop-timeline-items-card__scroll') as HTMLElement | null
          expect(scroll).not.toBeNull()
          expect(scroll?.scrollTop).toBe(320)

          await act(async () => {
            scroll!.scrollTop = 0
            scroll!.dispatchEvent(new Event('scroll', { bubbles: true }))
          })

          await rerender({
            segments: [{
              ...seg,
              items: [
                ...seg.items,
                { kind: 'call', call: { toolCallId: 'c3', toolName: 'read_file', arguments: {} }, seq: 2 },
              ],
            }],
            pool: EMPTY_POOL,
            isComplete: false,
            live: true,
          })

          expect(scroll?.scrollTop).toBe(0)
        } finally {
          cleanup()
        }
      } finally {
        scrollHeightSpy.mockRestore()
        clientHeightSpy.mockRestore()
      }
    })
  })

  describe('mixed thinking + segments', () => {
    it('thinkingOnly + segments complete -> step count shown in header', () => {
      const seg = makeSeg({ id: 's1', status: 'closed', title: '1 step completed' })
      const html = renderTimeline({
        segments: [seg],
        pool: EMPTY_POOL,
        thinkingOnly: { markdown: 'Thought about it', durationSec: 4, live: false },
        isComplete: true,
      })
      expect(html).toContain('完成 1 步')
      expect(html).not.toContain('1 step completed')
      expect(html).toContain('data-phase="thought"')
    })

    it('thinkingOnly + live segments -> thinking-live phase', () => {
      const seg = makeSeg({ id: 's1', status: 'open', title: 'Running...' })
      const html = renderTimeline({
        segments: [seg],
        pool: EMPTY_POOL,
        thinkingOnly: { markdown: 'thinking...', durationSec: 0, live: true },
        isComplete: false,
        live: true,
      })
      expect(html).toContain('data-phase="thinking-live"')
    })

    it('thinkingOnly + multiple segments complete -> N steps label', () => {
      const seg1 = makeSeg({ id: 's1', status: 'closed', seq: 0 })
      const seg2 = makeSeg({ id: 's2', status: 'closed', seq: 1, category: 'edit' })
      const seg3 = makeSeg({ id: 's3', status: 'closed', seq: 2, category: 'fetch' })
      const html = renderTimeline({
        segments: [seg1, seg2, seg3],
        pool: EMPTY_POOL,
        thinkingOnly: { markdown: 'thought', durationSec: 2, live: false },
        isComplete: true,
      })
      expect(html).toContain('完成 3 步')
      expect(html).not.toContain('3 steps completed')
    })
  })

  describe('edge cases', () => {
    it('todo_write segment renders nothing inside CopTimeline', () => {
      const seg = makeSeg({
        id: 's1',
        category: 'generic',
        status: 'closed',
        title: 'todo_write',
        items: [{ kind: 'call', call: { toolCallId: 'g1', toolName: 'todo_write', arguments: {} }, seq: 0 }],
      })
      const html = renderTimeline({
        segments: [seg],
        pool: EMPTY_POOL,
        isComplete: true,
      })
      expect(html).toBe('')
    })

    it('headerOverride takes priority over thinkingOnly label', () => {
      const html = renderTimeline({
        segments: [],
        pool: EMPTY_POOL,
        thinkingOnly: { markdown: 'some thought', durationSec: 5, live: false },
        isComplete: true,
        headerOverride: 'Override wins',
      })
      expect(html).toContain('Override wins')
      expect(html).not.toContain('Thought for 5s')
    })
  })
})

describe('CopTimelineHeaderLabel', () => {
  it('live thinking header 从 pending 状态句继续增量动画', () => {
    renderTimeline({
      segments: [],
      pool: EMPTY_POOL,
      thinkingOnly: { markdown: 'Thinking', durationSec: 0, live: true },
      isComplete: false,
      live: true,
      thinkingHint: 'Planning next moves',
    })

    expect(incrementalTypewriterMock).toHaveBeenCalledWith(
      'Planning next moves 0s',
      true,
      'Planning next moves...',
    )
  })

  it('headerOverride 不使用 pending 状态句作为动画种子', () => {
    renderTimeline({
      segments: [],
      pool: EMPTY_POOL,
      thinkingOnly: { markdown: 'Thinking', durationSec: 0, live: true },
      isComplete: false,
      live: true,
      thinkingHint: 'Planning next moves',
      headerOverride: 'Thinking',
    })

    expect(incrementalTypewriterMock).toHaveBeenCalledWith(
      'Thinking',
      true,
      undefined,
    )
  })

  it('renders text with data-phase attribute', async () => {
    const { container, cleanup } = await renderHeaderLabelDom({
      text: 'Thinking for 3s',
      phaseKey: 'thinking-live',
    })
    try {
      const span = container.querySelector('[data-phase="thinking-live"]')
      expect(span).not.toBeNull()
      expect(span?.textContent).toBe('Thinking for 3s')
    } finally {
      cleanup()
    }
  })

  it('renders with shimmer class when shimmer=true', () => {
    const html = renderToStaticMarkup(
      <LocaleProvider>
        <CopTimelineHeaderLabel text="Working..." phaseKey="live" shimmer />
      </LocaleProvider>,
    )
    expect(html).toContain('thinking-shimmer')
  })

  it('renders without shimmer class when shimmer=false', () => {
    const html = renderToStaticMarkup(
      <LocaleProvider>
        <CopTimelineHeaderLabel text="Done" phaseKey="complete" shimmer={false} />
      </LocaleProvider>,
    )
    expect(html).not.toContain('thinking-shimmer')
  })
})
