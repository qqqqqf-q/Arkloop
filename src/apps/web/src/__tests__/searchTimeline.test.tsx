import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { SearchTimeline } from '../components/SearchTimeline'
import { LocaleProvider } from '../contexts/LocaleContext'
import type { SubAgentRef, WebSource } from '../storage'
import type { CodeExecution } from '../components/ThinkingBlock'

function renderTimeline(params: {
  isComplete: boolean
  steps: { id: string; kind: 'planning' | 'searching' | 'reviewing' | 'finished'; label: string; status: 'active' | 'done'; queries?: string[] }[]
  sources: WebSource[]
  codeExecutions?: CodeExecution[]
  subAgents?: SubAgentRef[]
}): string {
  return renderToStaticMarkup(
    <LocaleProvider>
      <SearchTimeline
        steps={params.steps}
        sources={params.sources}
        isComplete={params.isComplete}
        codeExecutions={params.codeExecutions}
        subAgents={params.subAgents}
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
})
