import { describe, expect, it } from 'vitest'
import {
  applyAgentEventToWebSearchSteps,
  COMPLETED_SEARCHING_LABEL,
  DEFAULT_SEARCHING_LABEL,
  isWebSearchToolName,
  webSearchQueriesFromArguments,
  webSearchSourcesFromResult,
} from '../webSearchTimelineFromAgentEvent'
import {
  normalizeAgentEventData,
  normalizeAgentEventToolName,
  normalizeAgentEventType,
  type AgentUIEvent,
} from '../agent-ui'

function agentEvent(input: AgentUIEvent): AgentUIEvent {
  const type = normalizeAgentEventType(input.type)
  const data = normalizeAgentEventData({
    type,
    rawType: input.type,
    eventId: input.id,
    data: input.data,
    toolName: input.toolName,
    errorCode: input.errorCode,
  })
  return {
    ...input,
    type,
    data,
    toolName: normalizeAgentEventToolName({ type, data, fallback: input.toolName }),
  }
}

describe('isWebSearchToolName', () => {
  it('接受常见供应商/模型命名变体', () => {
    expect(isWebSearchToolName('web_search')).toBe(true)
    expect(isWebSearchToolName('WebSearch')).toBe(true)
    expect(isWebSearchToolName('web-search')).toBe(true)
    expect(isWebSearchToolName('web_search.tavily')).toBe(true)
    expect(isWebSearchToolName('x_search')).toBe(true)
    expect(isWebSearchToolName('x_search.xai')).toBe(true)
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

  it('提取 x_search citations 中的 X 帖子来源', () => {
    expect(
      webSearchSourcesFromResult({
        answer: 'recent posts',
        citations: [
          'https://x.com/qqqqqf_/status/2056736604845404380',
          { url: 'https://x.com/xai/status/2056000000000000000', title: 'xAI update' },
          'https://x.com/qqqqqf_/status/2056736604845404380',
        ],
      }),
    ).toEqual([
      { title: '@qqqqqf_', url: 'https://x.com/qqqqqf_/status/2056736604845404380', snippet: undefined },
      { title: 'xAI update', url: 'https://x.com/xai/status/2056000000000000000', snippet: undefined },
    ])
  })
})

describe('applyAgentEventToWebSearchSteps', () => {
  it('tool.call 与 tool.result 推进 searching 阶段', () => {
    const call: AgentUIEvent = agentEvent({
      type: 'tool-call',
      order: 1,
      timestamp: '',
      id: 'e1',
      streamId: 'r',
      data: {
        tool_name: 'WebSearch',
        tool_call_id: 'c1',
        arguments: { queries: ['q1'] },
      },
    })
    const result: AgentUIEvent = agentEvent({
      type: 'tool-result',
      order: 2,
      timestamp: '',
      id: 'e2',
      streamId: 'r',
      data: {
        tool_name: 'web_search',
        tool_call_id: 'c1',
        result: { results: [{ title: 't', url: 'https://x.test' }] },
      },
    })
    let steps = applyAgentEventToWebSearchSteps([], call)
    expect(steps).toHaveLength(1)
    expect(steps[0]?.kind).toBe('searching')
    expect(steps[0]?.label).toBe(DEFAULT_SEARCHING_LABEL)
    expect(steps[0]?.queries).toEqual(['q1'])
    steps = applyAgentEventToWebSearchSteps(steps, result)
    expect(steps).toHaveLength(1)
    expect(steps[0]?.label).toBe(COMPLETED_SEARCHING_LABEL)
    expect(steps[0]?.text).toEqual({ kind: 'search_completed' })
    expect(steps[0]?.sourceKind).toBe('web')
    expect(steps[0]?.sources).toEqual([{ title: 't', url: 'https://x.test', snippet: undefined }])
    expect(steps[0]?.seq).toBe(1)
    expect(steps[0]?.resultSeq).toBe(2)
  })

  it('x_search tool.result 复用搜索时间线并绑定 citations', () => {
    const call: AgentUIEvent = agentEvent({
      type: 'tool-call',
      order: 1,
      timestamp: '',
      id: 'e1',
      streamId: 'r',
      data: {
        tool_name: 'x_search',
        tool_call_id: 'x1',
        arguments: { query: 'from:@qqqqqf_' },
      },
    })
    const result: AgentUIEvent = agentEvent({
      type: 'tool-result',
      order: 2,
      timestamp: '',
      id: 'e2',
      streamId: 'r',
      data: {
        tool_name: 'x_search',
        tool_call_id: 'x1',
        result: {
          answer: 'Recent posts',
          citations: ['https://x.com/qqqqqf_/status/2056736604845404380'],
        },
      },
    })
    let steps = applyAgentEventToWebSearchSteps([], call)
    steps = applyAgentEventToWebSearchSteps(steps, result)

    expect(steps).toHaveLength(1)
    expect(steps[0]?.queries).toEqual(['from:@qqqqqf_'])
    expect(steps[0]?.sourceKind).toBe('x')
    expect(steps[0]?.sources).toEqual([
      { title: '@qqqqqf_', url: 'https://x.com/qqqqqf_/status/2056736604845404380', snippet: undefined },
    ])
  })

  it('多次 search 时只给对应 call 绑定自己的 sources', () => {
    let steps = applyAgentEventToWebSearchSteps([], agentEvent({
      type: 'tool-call',
      order: 10,
      timestamp: '',
      id: 'e1',
      streamId: 'r',
      data: { tool_name: 'web_search', tool_call_id: 's1', arguments: { query: 'first' } },
    }))
    steps = applyAgentEventToWebSearchSteps(steps, agentEvent({
      type: 'tool-call',
      order: 11,
      timestamp: '',
      id: 'e2',
      streamId: 'r',
      data: { tool_name: 'web_search', tool_call_id: 's2', arguments: { query: 'second' } },
    }))
    steps = applyAgentEventToWebSearchSteps(steps, agentEvent({
      type: 'tool-result',
      order: 20,
      timestamp: '',
      id: 'e3',
      streamId: 'r',
      data: {
        tool_name: 'web_search',
        tool_call_id: 's1',
        result: { results: [{ title: 'one', url: 'https://one.test' }] },
      },
    }))

    expect(steps.find((step) => step.id === 's1')?.sources).toEqual([{ title: 'one', url: 'https://one.test', snippet: undefined }])
    expect(steps.find((step) => step.id === 's2')?.sources).toBeUndefined()
  })

  it('run.interrupted 也会把主会话搜索步骤收口为 done', () => {
    const active = applyAgentEventToWebSearchSteps([], agentEvent({
      type: 'tool-call',
      order: 1,
      timestamp: '',
      id: 'e1',
      streamId: 'r',
      data: {
        tool_name: 'web_search',
        tool_call_id: 'host',
        arguments: { query: 'resume me' },
      },
    }))
    const interrupted = applyAgentEventToWebSearchSteps(active, agentEvent({
      type: 'run-interrupted',
      order: 2,
      timestamp: '',
      id: 'e2',
      streamId: 'r',
      data: {},
    }))
    expect(interrupted).toHaveLength(1)
    expect(interrupted[0]?.status).toBe('done')
    expect(interrupted[0]?.text).toEqual({ kind: 'search_completed' })
  })

  it('segment search 默认标题使用语义文本，并在结束时更新为完成语义', () => {
    let steps = applyAgentEventToWebSearchSteps([], agentEvent({
      type: 'segment-start',
      order: 1,
      timestamp: '',
      id: 'e1',
      streamId: 'r',
      data: {
        segment_id: 'seg1',
        kind: 'search_queries',
        display: {},
      },
    }))

    expect(steps[0]?.text).toEqual({ kind: 'search', tense: 'live' })

    steps = applyAgentEventToWebSearchSteps(steps, agentEvent({
      type: 'segment-end',
      order: 2,
      timestamp: '',
      id: 'e2',
      streamId: 'r',
      data: { segment_id: 'seg1' },
    }))

    expect(steps[0]?.label).toBe(COMPLETED_SEARCHING_LABEL)
    expect(steps[0]?.text).toEqual({ kind: 'search_completed' })
  })
})
