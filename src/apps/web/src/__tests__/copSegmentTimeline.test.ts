import { describe, expect, it } from 'vitest'
import { copTimelinePayloadForSegment, toolCallIdsInCopTimelines } from '../copSegmentTimeline'

const call = (id: string, name: string, seq: number) =>
  ({ kind: 'call' as const, call: { toolCallId: id, toolName: name, arguments: {} }, seq })

describe('copTimelinePayloadForSegment', () => {
  it('无匹配富数据时仍返回空壳，供 COP 标题行挂载', () => {
    const r = copTimelinePayloadForSegment(
      { type: 'cop', title: null, items: [call('x', 'load_tools', 1)] },
      { sources: [] },
    )
    expect(r).toEqual({ steps: [], sources: [] })
  })

  it('按 tool_call_id 筛出代码执行', () => {
    const r = copTimelinePayloadForSegment(
      {
        type: 'cop',
        title: 't',
        items: [
          call('a', 'python_execute', 2),
          call('b', 'unknown', 3),
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
    expect(r.codeExecutions).toEqual([{ id: 'a', language: 'python', code: '1', status: 'success', seq: 2 }])
    expect(r.steps).toEqual([])
  })

  it('含 searching 步骤时附带 sources', () => {
    const r = copTimelinePayloadForSegment(
      {
        type: 'cop',
        title: null,
        items: [call('ws1', 'web_search', 1)],
      },
      {
        searchSteps: [
          { id: 'ws1', kind: 'searching', label: 'q', status: 'done', seq: 1, sources: [{ title: 'u', url: 'https://u.test' }] },
        ],
        sources: [{ title: 'u', url: 'https://u.test' }],
      },
    )
    expect(r.steps.map((step) => step.kind)).toEqual(['searching', 'reviewing'])
    expect(r.steps[1]?.sources).toEqual([{ title: 'u', url: 'https://u.test' }])
  })

  it('缺少 searchSteps 池子时，会从段内 web_search call 恢复时间线', () => {
    const r = copTimelinePayloadForSegment(
      {
        type: 'cop',
        title: null,
        items: [
          {
            kind: 'call',
            call: {
              toolCallId: 'ws1',
              toolName: 'web_search',
              arguments: { query: 'Claude Desktop 更新' },
              result: {
                results: [{ title: 'u', url: 'https://u.test' }],
              },
            },
            seq: 3,
          },
        ],
      },
      {
        sources: [{ title: 'u', url: 'https://u.test' }],
      },
    )
    expect(r.steps).toEqual([
      {
        id: 'ws1',
        kind: 'searching',
        label: 'Search completed',
        status: 'done',
        queries: ['Claude Desktop 更新'],
        seq: 3,
        sources: [{ title: 'u', url: 'https://u.test', snippet: undefined }],
      },
      {
        id: 'ws1::reviewing',
        kind: 'reviewing',
        label: 'Reviewing sources',
        status: 'done',
        sources: [{ title: 'u', url: 'https://u.test', snippet: undefined }],
        seq: 3.5,
      },
    ])
    expect(r.sources).toEqual([{ title: 'u', url: 'https://u.test', snippet: undefined }])
  })

  it('reviewing 按 resultSeq 排序，不抢到其他工具前面', () => {
    const r = copTimelinePayloadForSegment(
      {
        type: 'cop',
        title: null,
        items: [call('ws1', 'web_search', 10), call('cmd1', 'exec_command', 11)],
      },
      {
        codeExecutions: [{ id: 'cmd1', language: 'shell', code: 'ls', status: 'success', seq: 11 }],
        searchSteps: [
          {
            id: 'ws1',
            kind: 'searching',
            label: 'q',
            status: 'done',
            seq: 10,
            resultSeq: 20,
            sources: [{ title: 'u', url: 'https://u.test' }],
          },
        ],
        sources: [{ title: 'u', url: 'https://u.test' }],
      },
    )
    expect(r.steps[1]?.seq).toBe(20)
    expect(r.codeExecutions?.[0]?.seq).toBe(11)
  })

  it('未专门映射的工具进入 generic fallback', () => {
    const r = copTimelinePayloadForSegment(
      {
        type: 'cop',
        title: null,
        items: [call('tool_1', 'fetch_url', 1)],
      },
      { sources: [] },
    )
    expect(r.genericTools).toEqual([
      expect.objectContaining({
        id: 'tool_1',
        toolName: 'fetch_url',
        label: 'fetch_url',
        status: 'running',
        seq: 1,
      }),
    ])
  })

  it('generic fallback 只显示结果摘要，不裸露 raw JSON', () => {
    const r = copTimelinePayloadForSegment(
      {
        type: 'cop',
        title: null,
        items: [{ kind: 'call', call: { toolCallId: 'tool_1', toolName: 'fetch_url', arguments: {}, result: { a: 1, b: 2 } }, seq: 1 }],
      },
      { sources: [] },
    )
    expect(r.genericTools).toEqual([
      {
        id: 'tool_1',
        toolName: 'fetch_url',
        label: 'fetch_url',
        output: 'returned object · 2 keys',
        status: 'success',
        seq: 1,
      },
    ])
    expect(r.genericTools?.[0]?.output).not.toContain('{"a"')
  })

  it('show_widget、create_artifact、browser 不进入 generic fallback', () => {
    const r = copTimelinePayloadForSegment(
      {
        type: 'cop',
        title: null,
        items: [
          call('w1', 'show_widget', 1),
          call('a1', 'create_artifact', 2),
          call('b1', 'browser', 3),
        ],
      },
      { sources: [] },
    )
    expect(r.genericTools).toBeUndefined()
  })

  it('read provider 名不进入 generic fallback', () => {
    const r = copTimelinePayloadForSegment(
      {
        type: 'cop',
        title: null,
        items: [call('r1', 'read.minimax', 1)],
      },
      { sources: [] },
    )
    expect(r.genericTools).toBeUndefined()
  })

  it('read、grep、glob、lsp 读取类工具聚合为 Explore', () => {
    const r = copTimelinePayloadForSegment(
      {
        type: 'cop',
        title: null,
        items: [
          call('r1', 'read', 1),
          call('g1', 'grep', 2),
          call('l1', 'lsp', 3),
        ],
      },
      {
        fileOps: [
          { id: 'r1', toolName: 'read_file', label: 'Read ChatInput.tsx', status: 'success', seq: 1, filePath: 'src/ChatInput.tsx', displayKind: 'read' },
          { id: 'g1', toolName: 'grep', label: 'Searched PersonaChip', status: 'success', seq: 2, pattern: 'PersonaChip', displayKind: 'grep' },
          { id: 'l1', toolName: 'lsp', label: 'Found references', status: 'running', seq: 3, operation: 'references', displayKind: 'lsp' },
        ],
        sources: [],
      },
    )
    expect(r.fileOps).toBeUndefined()
    expect(r.exploreGroups).toHaveLength(1)
    expect(r.exploreGroups?.[0]?.status).toBe('running')
    expect(r.exploreGroups?.[0]?.items.map((item) => item.id)).toEqual(['r1', 'g1', 'l1'])
  })

  it('edit 和 lsp rename 不进入 Explore', () => {
    const r = copTimelinePayloadForSegment(
      {
        type: 'cop',
        title: null,
        items: [call('e1', 'edit', 1), call('l1', 'lsp', 2)],
      },
      {
        fileOps: [
          { id: 'e1', toolName: 'edit', label: 'Edited a.ts', status: 'success', seq: 1, displayKind: 'edit' },
          { id: 'l1', toolName: 'lsp', label: 'Renamed symbol', status: 'success', seq: 2, operation: 'rename', displayKind: 'edit' },
        ],
        sources: [],
      },
    )
    expect(r.exploreGroups).toBeUndefined()
    expect(r.fileOps?.map((item) => item.id)).toEqual(['e1', 'l1'])
  })

  it('toolCallIdsInCopTimelines 汇总 COP 时间轴已占用的 id', () => {
    const ids = toolCallIdsInCopTimelines(
      {
        segments: [
          {
            type: 'cop',
            title: null,
            items: [call('fo1', 'load_tools', 1)],
          },
        ],
      },
      {
        fileOps: [{ id: 'fo1', toolName: 'load_tools', label: 'x', status: 'success' }],
        sources: [],
      },
    )
    expect(ids.has('fo1')).toBe(true)
  })

  it('toolCallIdsInCopTimelines 包含 Explore 内部工具 id', () => {
    const ids = toolCallIdsInCopTimelines(
      {
        segments: [
          {
            type: 'cop',
            title: null,
            items: [call('r1', 'read', 1)],
          },
        ],
      },
      {
        fileOps: [{ id: 'r1', toolName: 'read_file', label: 'Read a.ts', status: 'success', seq: 1, displayKind: 'read' }],
        sources: [],
      },
    )
    expect(ids.has('r1')).toBe(true)
  })
})
