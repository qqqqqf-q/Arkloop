import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { CopTimeline } from '../components/CopTimeline'
import { LocaleProvider } from '../contexts/LocaleContext'
import type { SubAgentRef, WebSource } from '../storage'
import type { CodeExecution } from '../components/CodeExecutionCard'

function renderTimeline(params: {
  isComplete: boolean
  steps: { id: string; kind: 'planning' | 'searching' | 'reviewing' | 'finished'; label: string; status: 'active' | 'done'; queries?: string[]; seq?: number }[]
  sources: WebSource[]
  narratives?: Array<{ id: string; text: string; seq: number }>
  codeExecutions?: CodeExecution[]
  subAgents?: SubAgentRef[]
  fileOps?: Array<{ id: string; toolName: string; label: string; status: 'running' | 'success' | 'failed'; seq?: number }>
  webFetches?: Array<{ id: string; url: string; title?: string; status: 'fetching' | 'done' | 'failed'; statusCode?: number; seq?: number }>
  thinkingRows?: Array<{ id: string; markdown: string; live?: boolean; seq: number; durationSec?: number }>
  thinkingStartedAt?: number
}): string {
  return renderToStaticMarkup(
    <LocaleProvider>
      <CopTimeline
        steps={params.steps}
        sources={params.sources}
        narratives={params.narratives}
        isComplete={params.isComplete}
        codeExecutions={params.codeExecutions}
        subAgents={params.subAgents}
        fileOps={params.fileOps}
        webFetches={params.webFetches}
        thinkingRows={params.thinkingRows}
        thinkingStartedAt={params.thinkingStartedAt}
      />
    </LocaleProvider>,
  )
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
        { id: 'op1', toolName: 'search_tools', label: 'search_tools "exec_command"', status: 'success', seq: 10 },
      ],
    })

    const fileOpIndex = html.indexOf('search_tools &quot;exec_command&quot;')
    const narrativeIndex = html.indexOf('先整理一下现有工具。')
    const stepIndex = html.lastIndexOf('Searching')
    expect(fileOpIndex).toBeGreaterThanOrEqual(0)
    expect(narrativeIndex).toBeGreaterThan(fileOpIndex)
    expect(stepIndex).toBeGreaterThan(narrativeIndex)
    expect(html).not.toContain('Finished')
  })

  it('交错 thinking 流式时显示单行预览触发器', () => {
    const html = renderTimeline({
      isComplete: false,
      steps: [],
      sources: [],
      fileOps: [{ id: 'op1', toolName: 'grep', label: 'x', status: 'success', seq: 2 }],
      thinkingRows: [{ id: 't1', markdown: 'hello world', live: true, seq: 1 }],
    })
    expect(html).toContain('cop-thinking-preview-trigger')
    expect(html).not.toContain('cop-thinking-only-body')
  })

  it('thinking 结束后子行显示 Thought for Xs', () => {
    const html = renderTimeline({
      isComplete: false,
      steps: [],
      sources: [],
      fileOps: [{ id: 'op1', toolName: 'grep', label: 'x', status: 'success', seq: 2 }],
      thinkingRows: [{ id: 't1', markdown: 'done', live: false, seq: 1, durationSec: 8 }],
    })
    expect(html).toContain('cop-thinking-card-trigger')
    expect(html).toMatch(/Thought for \d+s/)
    expect(html).not.toContain('cop-thinking-preview-trigger')
  })

  it('仅 thinking 时也在时间轴圆点列内显示预览 Segment', () => {
    const html = renderTimeline({
      isComplete: false,
      steps: [],
      sources: [],
      thinkingRows: [{ id: 't1', markdown: 'solo', live: true, seq: 1 }],
    })
    expect(html).toContain('cop-thinking-preview-trigger')
    expect(html).toContain('left:-19px')
    expect(html).not.toContain('cop-thinking-header-strip')
  })

  it('仅 thinking 且已结束时点旁直接展开正文，不用内层折叠卡片', () => {
    const html = renderTimeline({
      isComplete: true,
      steps: [],
      sources: [],
      thinkingRows: [{ id: 't1', markdown: '内心独白一段', live: false, seq: 1, durationSec: 2 }],
    })
    expect(html).toContain('cop-thinking-output-md')
    expect(html).toContain('内心独白一段')
    expect(html).not.toContain('cop-thinking-block')
    expect(html).not.toContain('cop-thinking-card-trigger')
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

})
