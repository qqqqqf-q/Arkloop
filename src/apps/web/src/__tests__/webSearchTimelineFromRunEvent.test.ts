import { describe, expect, it } from 'vitest'
import { ACP_DELEGATE_LAYER } from '@arkloop/shared'
import {
  applyRunEventToWebSearchSteps,
  COMPLETED_SEARCHING_LABEL,
  DEFAULT_SEARCHING_LABEL,
  isWebSearchToolName,
  webSearchQueriesFromArguments,
  webSearchSourcesFromResult,
} from '../webSearchTimelineFromRunEvent'
import type { RunEvent } from '../sse'

describe('isWebSearchToolName', () => {
  it('接受常见供应商/模型命名变体', () => {
    expect(isWebSearchToolName('web_search')).toBe(true)
    expect(isWebSearchToolName('WebSearch')).toBe(true)
    expect(isWebSearchToolName('web-search')).toBe(true)
    expect(isWebSearchToolName('web_search.tavily')).toBe(true)
    expect(isWebSearchToolName('other')).toBe(false)
  })
})

describe('webSearchQueriesFromArguments', () => {
  it('同时支持 query 与 queries', () => {
    expect(webSearchQueriesFromArguments({ query: 'a' })).toEqual(['a'])
    expect(webSearchQueriesFromArguments({ queries: ['b', 'c'] })).toEqual(['b', 'c'])
    expect(webSearchQueriesFromArguments({ query: 'a', queries: ['b'] })).toEqual(['a', 'b'])
  })
})

describe('webSearchSourcesFromResult', () => {
  it('提取 results 中的 sources', () => {
    expect(
      webSearchSourcesFromResult({
        results: [
          { title: 'A', url: 'https://a.test', snippet: 'aa' },
          { title: 'B', url: '' },
        ],
      }),
    ).toEqual([{ title: 'A', url: 'https://a.test', snippet: 'aa' }])
  })
})

describe('applyRunEventToWebSearchSteps', () => {
  it('tool.call 与 tool.result 推进 searching 阶段', () => {
    const call: RunEvent = {
      type: 'tool.call',
      seq: 1,
      ts: '',
      event_id: 'e1',
      run_id: 'r',
      data: {
        tool_name: 'WebSearch',
        tool_call_id: 'c1',
        arguments: { queries: ['q1'] },
      },
    }
    const result: RunEvent = {
      type: 'tool.result',
      seq: 2,
      ts: '',
      event_id: 'e2',
      run_id: 'r',
      data: {
        tool_name: 'web_search',
        tool_call_id: 'c1',
        result: { results: [{ title: 't', url: 'https://x.test' }] },
      },
    }
    let steps = applyRunEventToWebSearchSteps([], call)
    expect(steps).toHaveLength(1)
    expect(steps[0]?.kind).toBe('searching')
    expect(steps[0]?.label).toBe(DEFAULT_SEARCHING_LABEL)
    expect(steps[0]?.queries).toEqual(['q1'])
    steps = applyRunEventToWebSearchSteps(steps, result)
    expect(steps).toHaveLength(1)
    expect(steps[0]?.label).toBe(COMPLETED_SEARCHING_LABEL)
    expect(steps[0]?.sources).toEqual([{ title: 't', url: 'https://x.test', snippet: undefined }])
    expect(steps[0]?.seq).toBe(1)
    expect(steps[0]?.resultSeq).toBe(2)
  })

  it('多次 search 时只给对应 call 绑定自己的 sources', () => {
    let steps = applyRunEventToWebSearchSteps([], {
      type: 'tool.call',
      seq: 10,
      ts: '',
      event_id: 'e1',
      run_id: 'r',
      data: { tool_name: 'web_search', tool_call_id: 's1', arguments: { query: 'first' } },
    })
    steps = applyRunEventToWebSearchSteps(steps, {
      type: 'tool.call',
      seq: 11,
      ts: '',
      event_id: 'e2',
      run_id: 'r',
      data: { tool_name: 'web_search', tool_call_id: 's2', arguments: { query: 'second' } },
    })
    steps = applyRunEventToWebSearchSteps(steps, {
      type: 'tool.result',
      seq: 20,
      ts: '',
      event_id: 'e3',
      run_id: 'r',
      data: {
        tool_name: 'web_search',
        tool_call_id: 's1',
        result: { results: [{ title: 'one', url: 'https://one.test' }] },
      },
    })

    expect(steps.find((step) => step.id === 's1')?.sources).toEqual([{ title: 'one', url: 'https://one.test', snippet: undefined }])
    expect(steps.find((step) => step.id === 's2')?.sources).toBeUndefined()
  })

  it('忽略 delegate_layer 的搜索工具与内层 run 生命周期', () => {
    const d = { delegate_layer: ACP_DELEGATE_LAYER }
    const delegateCall: RunEvent = {
      type: 'tool.call',
      seq: 1,
      ts: '',
      event_id: 'e1',
      run_id: 'r',
      data: {
        ...d,
        tool_name: 'web_search',
        tool_call_id: 'inner',
        arguments: { query: 'q' },
      },
    }
    expect(applyRunEventToWebSearchSteps([], delegateCall)).toEqual([])

    const active = applyRunEventToWebSearchSteps([], {
      type: 'tool.call',
      seq: 2,
      ts: '',
      event_id: 'e2',
      run_id: 'r',
      data: {
        tool_name: 'web_search',
        tool_call_id: 'host',
        arguments: { query: 'h' },
      },
    })
    expect(active).toHaveLength(1)
    expect(active[0]?.status).toBe('active')

    const afterInnerComplete = applyRunEventToWebSearchSteps(active, {
      type: 'run.completed',
      seq: 3,
      ts: '',
      event_id: 'e3',
      run_id: 'r',
      data: { ...d },
    })
    expect(afterInnerComplete).toEqual(active)

    const afterHostComplete = applyRunEventToWebSearchSteps(
      afterInnerComplete,
      { type: 'run.completed', seq: 4, ts: '', event_id: 'e4', run_id: 'r', data: {} },
    )
    expect(afterHostComplete.every((s) => s.status === 'done')).toBe(true)
  })

  it('run.interrupted 也会把主会话搜索步骤收口为 done', () => {
    const active = applyRunEventToWebSearchSteps([], {
      type: 'tool.call',
      seq: 1,
      ts: '',
      event_id: 'e1',
      run_id: 'r',
      data: {
        tool_name: 'web_search',
        tool_call_id: 'host',
        arguments: { query: 'resume me' },
      },
    })
    const interrupted = applyRunEventToWebSearchSteps(active, {
      type: 'run.interrupted',
      seq: 2,
      ts: '',
      event_id: 'e2',
      run_id: 'r',
      data: {},
    })
    expect(interrupted).toHaveLength(1)
    expect(interrupted[0]?.status).toBe('done')
  })
})
