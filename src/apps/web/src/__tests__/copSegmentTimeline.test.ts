import { describe, expect, it } from 'vitest'
import { copTimelinePayloadForSegment, toolCallIdsInCopTimelines } from '../copSegmentTimeline'

describe('copTimelinePayloadForSegment', () => {
  it('无匹配富数据时返回 null', () => {
    const r = copTimelinePayloadForSegment(
      { type: 'cop', title: null, calls: [{ toolCallId: 'x', toolName: 'search_tools', arguments: {} }] },
      { sources: [] },
    )
    expect(r).toBeNull()
  })

  it('按 tool_call_id 筛出代码执行', () => {
    const r = copTimelinePayloadForSegment(
      {
        type: 'cop',
        title: 't',
        calls: [
          { toolCallId: 'a', toolName: 'python_execute', arguments: {} },
          { toolCallId: 'b', toolName: 'unknown', arguments: {} },
        ],
      },
      {
        codeExecutions: [
          { id: 'a', language: 'python', code: '1', status: 'success', seq: 2 },
          { id: 'z', language: 'python', code: '2', status: 'success', seq: 1 },
        ],
        sources: [],
      },
    )
    expect(r).not.toBeNull()
    expect(r!.codeExecutions).toEqual([{ id: 'a', language: 'python', code: '1', status: 'success', seq: 2 }])
    expect(r!.steps).toEqual([])
  })

  it('含 searching 步骤时附带 sources', () => {
    const r = copTimelinePayloadForSegment(
      {
        type: 'cop',
        title: null,
        calls: [{ toolCallId: 'ws1', toolName: 'web_search', arguments: {} }],
      },
      {
        searchSteps: [
          { id: 'ws1', kind: 'searching', label: 'q', status: 'done', seq: 1 },
        ],
        sources: [{ title: 'u', url: 'https://u.test' }],
      },
    )
    expect(r).not.toBeNull()
    expect(r!.sources).toHaveLength(1)
  })

  it('toolCallIdsInCopTimelines 汇总 COP 时间轴已占用的 id', () => {
    const ids = toolCallIdsInCopTimelines(
      {
        segments: [
          {
            type: 'cop',
            title: null,
            calls: [{ toolCallId: 'fo1', toolName: 'search_tools', arguments: {} }],
          },
        ],
      },
      {
        fileOps: [{ id: 'fo1', toolName: 'search_tools', label: 'x', status: 'success' }],
        sources: [],
      },
    )
    expect(ids.has('fo1')).toBe(true)
  })
})
