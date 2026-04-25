import { describe, expect, it } from 'vitest'
import { copTimelinePayloadForSegment, promotedCopTimelineEntries, toolCallIdsInCopTimelines } from '../copSegmentTimeline'

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

  it('exec_command 会切断 Explore 聚合，后续 read 进入新的 Explore', () => {
    const r = copTimelinePayloadForSegment(
      {
        type: 'cop',
        title: null,
        items: [
          call('grep_1', 'grep', 1),
          call('glob_1', 'glob', 2),
          call('read_1', 'read', 3),
          call('cmd_1', 'exec_command', 4),
          call('read_2', 'read', 5),
          call('read_3', 'read', 6),
          call('cmd_2', 'exec_command', 7),
          call('grep_2', 'grep', 8),
        ],
      },
      {
        codeExecutions: [
          { id: 'cmd_1', language: 'shell', code: 'ls -la src/apps/web/src/components/cop-timeline/', status: 'success', seq: 4 },
          { id: 'cmd_2', language: 'shell', code: 'cd src/apps/web && pnpm type-check', status: 'success', seq: 7 },
        ],
        fileOps: [
          { id: 'grep_1', toolName: 'grep', label: 'Searched copTimeline', status: 'success', seq: 1, displayKind: 'grep' },
          { id: 'glob_1', toolName: 'glob', label: 'Listed cop-timeline', status: 'success', seq: 2, displayKind: 'glob' },
          { id: 'read_1', toolName: 'read_file', label: 'Read CopTimeline.tsx', status: 'success', seq: 3, filePath: 'CopTimeline.tsx', displayKind: 'read' },
          { id: 'read_2', toolName: 'read_file', label: 'Read ToolRows.tsx', status: 'success', seq: 5, filePath: 'ToolRows.tsx', displayKind: 'read' },
          { id: 'read_3', toolName: 'read_file', label: 'Read SourceList.tsx', status: 'success', seq: 6, filePath: 'SourceList.tsx', displayKind: 'read' },
          { id: 'grep_2', toolName: 'grep', label: 'Searched key symbols', status: 'success', seq: 8, displayKind: 'grep' },
        ],
        sources: [],
      },
    )

    expect(r.exploreGroups?.map((group) => group.items.map((item) => item.id))).toEqual([
      ['grep_1', 'glob_1', 'read_1'],
      ['read_2', 'read_3'],
      ['grep_2'],
    ])
    expect(r.exploreGroups?.map((group) => group.label)).toEqual([
      'Searched code, Listed files, Read a file',
      'Read files',
      'Searched code',
    ])
    expect(r.codeExecutions?.map((item) => item.id)).toEqual(['cmd_1', 'cmd_2'])
  })

  it('promotedCopTimelineEntries 按真实 seq 混排 Explore 和 Edit', () => {
    const payload = copTimelinePayloadForSegment(
      {
        type: 'cop',
        title: null,
        items: [
          call('read_1', 'read', 1),
          call('edit_1', 'edit', 2),
          call('read_2', 'read', 3),
        ],
      },
      {
        fileOps: [
          { id: 'read_1', toolName: 'read_file', label: 'Read a.ts', status: 'success', seq: 1, filePath: 'a.ts', displayKind: 'read' },
          { id: 'edit_1', toolName: 'edit', label: 'Edited a.ts', status: 'failed', seq: 2, filePath: 'a.ts', displayKind: 'edit' },
          { id: 'read_2', toolName: 'read_file', label: 'Read b.ts', status: 'success', seq: 3, filePath: 'b.ts', displayKind: 'read' },
        ],
        sources: [],
      },
    )

    expect(payload.exploreGroups?.map((group) => group.items.map((item) => item.id))).toEqual([
      ['read_1'],
      ['read_2'],
    ])
    expect(promotedCopTimelineEntries({
      payload,
      hasTimelineBody: false,
      bodyFileOps: [],
    }).map((entry) => `${entry.kind}:${entry.id}`)).toEqual([
      `explore:${payload.exploreGroups?.[0]?.id}`,
      'edit:edit_1',
      `explore:${payload.exploreGroups?.[1]?.id}`,
    ])
  })

  it('promotedCopTimelineEntries 将 timeline body 按提升 segment 切片', () => {
    const payload = copTimelinePayloadForSegment(
      {
        type: 'cop',
        title: null,
        items: [
          call('cmd_1', 'exec_command', 1),
          call('glob_1', 'glob', 2),
          call('read_1', 'read', 3),
          call('cmd_2', 'exec_command', 4),
        ],
      },
      {
        codeExecutions: [
          { id: 'cmd_1', language: 'shell', code: 'ls -la', status: 'success', seq: 1 },
          { id: 'cmd_2', language: 'shell', code: 'cat a.txt', status: 'success', seq: 4 },
        ],
        fileOps: [
          { id: 'glob_1', toolName: 'glob', label: 'Listed files', status: 'failed', seq: 2, displayKind: 'glob' },
          { id: 'read_1', toolName: 'read_file', label: 'Read a.txt', status: 'success', seq: 3, filePath: 'a.txt', displayKind: 'read' },
        ],
        sources: [],
      },
    )

    expect(promotedCopTimelineEntries({
      payload,
      hasTimelineBody: true,
      bodyFileOps: [],
    }).map((entry) => entry.kind === 'timeline' ? `${entry.kind}:${entry.slice.codeExecutions?.map((item) => item.id).join(',')}` : `${entry.kind}:${entry.id}`)).toEqual([
      'timeline:cmd_1',
      `explore:${payload.exploreGroups?.[0]?.id}`,
      'timeline:cmd_2',
    ])
  })

  it('promotedCopTimelineEntries 将 barrier 后的 thinking 附着到 barrier', () => {
    const payload = copTimelinePayloadForSegment(
      {
        type: 'cop',
        title: null,
        items: [
          call('grep_1', 'grep', 1),
          { kind: 'thinking', content: 'next thought', seq: 2 },
        ],
      },
      {
        fileOps: [
          { id: 'grep_1', toolName: 'grep', label: 'Searched code', status: 'success', seq: 1, displayKind: 'grep' },
        ],
        sources: [],
      },
    )

    const entries = promotedCopTimelineEntries({
      payload,
      hasTimelineBody: true,
      bodyFileOps: [],
      thinkingRows: [{ id: 'think-0-1-2', seq: 2 }],
    })

    expect(entries).toHaveLength(1)
    expect(entries[0]?.kind).toBe('explore')
    if (entries[0]?.kind !== 'explore') throw new Error('expected explore')
    expect(entries[0].attachedSlice?.thinkingRows?.map((row) => row.id)).toEqual(['think-0-1-2'])
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
