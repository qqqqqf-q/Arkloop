import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { SearchTimeline } from '../components/SearchTimeline'
import { LocaleProvider } from '../contexts/LocaleContext'
import type { SubAgentRef, WebSource } from '../storage'
import type { CodeExecution } from '../components/ThinkingBlock'

function renderTimeline(params: {
  isComplete: boolean
  steps: { id: string; kind: 'planning' | 'searching' | 'reviewing' | 'finished'; label: string; status: 'active' | 'done'; queries?: string[]; seq?: number }[]
  sources: WebSource[]
  narratives?: Array<{ id: string; text: string; seq: number }>
  codeExecutions?: CodeExecution[]
  subAgents?: SubAgentRef[]
  fileOps?: Array<{ id: string; toolName: string; label: string; status: 'running' | 'success' | 'failed'; seq?: number }>
}): string {
  return renderToStaticMarkup(
    <LocaleProvider>
      <SearchTimeline
        steps={params.steps}
        sources={params.sources}
        narratives={params.narratives}
        isComplete={params.isComplete}
        codeExecutions={params.codeExecutions}
        subAgents={params.subAgents}
        fileOps={params.fileOps}
      />
    </LocaleProvider>,
  )
}

describe('SearchTimeline', () => {
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
})
