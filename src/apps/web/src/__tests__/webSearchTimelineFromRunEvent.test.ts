import { describe, expect, it } from 'vitest'
import {
  applyRunEventToWebSearchSteps,
  isWebSearchToolName,
  webSearchQueriesFromArguments,
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
    expect(steps[0]?.queries).toEqual(['q1'])
    steps = applyRunEventToWebSearchSteps(steps, result)
    expect(steps.some((s) => s.kind === 'reviewing')).toBe(true)
  })
})
