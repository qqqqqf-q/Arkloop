import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  assistantTurnPlainText,
  buildAssistantTurnFromAgentEvents,
  copSegmentCalls,
  createEmptyAssistantTurnFoldState,
  finalizeAssistantTurnFoldState,
  foldAssistantTurnEvent,
  requestAssistantTurnThinkingBreak,
} from '../assistantTurnSegments'
import {
  normalizeAgentEventData,
  normalizeAgentEventToolName,
  normalizeAgentEventType,
  type AgentUIEvent,
} from '../agent-ui'

function ev(runId: string, seq: number, type: string, data?: unknown, errorClass?: string): AgentUIEvent {
  const id = `evt_${seq}`
  const normalizedType = normalizeAgentEventType(type)
  const normalizedData = normalizeAgentEventData({
    type: normalizedType,
    rawType: type,
    eventId: id,
    data: data ?? {},
    errorCode: errorClass,
  })
  return {
    id,
    streamId: runId,
    order: seq,
    timestamp: `2026-03-20T00:00:${String(seq).padStart(2, '0')}.000Z`,
    type: normalizedType,
    data: normalizedData,
    toolName: normalizeAgentEventToolName({ type: normalizedType, data: normalizedData }),
    errorCode: errorClass,
  }
}

const FINALIZE_NOW_MS = Date.parse('2026-03-21T00:00:00.000Z')
const evMs = (seq: number) => Date.parse(`2026-03-20T00:00:${String(seq).padStart(2, '0')}.000Z`)

function th(content: string, seq: number, endedByEventSeq?: number) {
  const startedAtMs = evMs(seq)
  const endedAtMs = endedByEventSeq == null ? FINALIZE_NOW_MS : evMs(endedByEventSeq)
  return { kind: 'thinking' as const, content, seq, startedAtMs, endedAtMs }
}

describe('buildAssistantTurnFromAgentEvents', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(FINALIZE_NOW_MS)
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('requestAssistantTurnThinkingBreak 将连续 thinking 拆成多项', () => {
    const state = createEmptyAssistantTurnFoldState()
    foldAssistantTurnEvent(state, ev('r1', 1, 'assistant-delta', { role: 'assistant', channel: 'thinking', content_delta: 'a' }))
    requestAssistantTurnThinkingBreak(state)
    foldAssistantTurnEvent(state, ev('r1', 2, 'assistant-delta', { role: 'assistant', channel: 'thinking', content_delta: 'b' }))
    finalizeAssistantTurnFoldState(state)
    expect(state.segments).toEqual([
      {
        type: 'cop',
        title: null,
        items: [th('a', 1), th('b', 2)],
      },
    ])
  })

  it('合并连续 assistant 文本为单一 text segment', () => {
    const turn = buildAssistantTurnFromAgentEvents([
      ev('r1', 1, 'assistant-delta', { role: 'assistant', content_delta: 'a' }),
      ev('r1', 2, 'assistant-delta', { role: 'assistant', content_delta: 'b' }),
    ])
    expect(turn.segments).toEqual([{ type: 'text', content: 'ab' }])
  })

  it('thinking 后主通道正文独立成 text 段（不并进 COP 时间轴）', () => {
    const turn = buildAssistantTurnFromAgentEvents([
      ev('r1', 1, 'assistant-delta', { role: 'assistant', channel: 'thinking', content_delta: 't1' }),
      ev('r1', 2, 'assistant-delta', { role: 'assistant', content_delta: 'visible' }),
    ])
    expect(turn.segments).toEqual([
      {
        type: 'cop',
        title: null,
        items: [th('t1', 1, 2)],
      },
      { type: 'text', content: 'visible' },
    ])
  })

  it('工具后 thinking：thinking 与 tool 前短句分段，首个 tool 起新 cop', () => {
    const turn = buildAssistantTurnFromAgentEvents([
      ev('r1', 1, 'assistant-delta', { role: 'assistant', channel: 'thinking', content_delta: 'a' }),
      ev('r1', 2, 'assistant-delta', { role: 'assistant', content_delta: 'hi' }),
      ev('r1', 3, 'tool-call', { tool_name: 'read_file', tool_call_id: 'c1', arguments: {} }),
      ev('r1', 4, 'tool-result', { tool_name: 'read_file', tool_call_id: 'c1', result: {} }),
      ev('r1', 5, 'assistant-delta', { role: 'assistant', channel: 'thinking', content_delta: 'b' }),
      ev('r1', 6, 'assistant-delta', { role: 'assistant', content_delta: 'bye' }),
    ])
    expect(turn.segments).toEqual([
      {
        type: 'cop',
        title: null,
        items: [th('a', 1, 2)],
      },
      { type: 'text', content: 'hi' },
      {
        type: 'cop',
        title: null,
        items: [
          {kind: 'call',
            call: { toolCallId: 'c1', toolName: 'read_file', arguments: {}, result: {}, errorClass: undefined },
            seq: 3,
          },
          th('b', 5, 6),
        ],
      },
      { type: 'text', content: 'bye' },
    ])
    expect(assistantTurnPlainText(turn)).toBe('hibye')
  })

  it('thinking 后短句与首个 tool：短句为独立 text，tool 为下一段 cop', () => {
    const turn = buildAssistantTurnFromAgentEvents([
      ev('r1', 1, 'assistant-delta', { role: 'assistant', channel: 'thinking', content_delta: 'plan' }),
      ev('r1', 2, 'assistant-delta', { role: 'assistant', content_delta: '我来查一下。' }),
      ev('r1', 3, 'tool-call', { tool_name: 'read_file', tool_call_id: 'c1', arguments: {} }),
    ])
    expect(turn.segments).toEqual([
      {
        type: 'cop',
        title: null,
        items: [th('plan', 1, 2)],
      },
      { type: 'text', content: '我来查一下。' },
      {
        type: 'cop',
        title: null,
        items: [
          {kind: 'call',
            call: {
              toolCallId: 'c1',
              toolName: 'read_file',
              arguments: {},
              result: undefined,
              errorClass: undefined,
            },
            seq: 3,
          },
        ],
      },
    ])
  })

  it('tool.result error message 会保留到 COP call', () => {
    const turn = buildAssistantTurnFromAgentEvents([
      ev('r1', 1, 'tool-call', { tool_name: 'image_generate', tool_call_id: 'img_1', arguments: { prompt: 'a mountain at dawn' } }),
      ev('r1', 2, 'tool-result', {
        tool_name: 'image_generate',
        tool_call_id: 'img_1',
        error: { error_class: 'provider.retryable', message: 'request failed after retry' },
      }, 'provider.retryable'),
    ])

    const segment = turn.segments.find((item) => item.type === 'cop')
    expect(segment?.type).toBe('cop')
    if (segment?.type !== 'cop') throw new Error('missing cop segment')
    expect(copSegmentCalls(segment)[0]).toEqual(expect.objectContaining({
      toolCallId: 'img_1',
      errorClass: 'provider.retryable',
      errorMessage: 'request failed after retry',
    }))
  })

  it('thinking 与首个 tool 同 cop（中间无正文）', () => {
    const turn = buildAssistantTurnFromAgentEvents([
      ev('r1', 1, 'assistant-delta', { role: 'assistant', channel: 'thinking', content_delta: 'plan' }),
      ev('r1', 2, 'tool-call', { tool_name: 'read_file', tool_call_id: 'c1', arguments: {} }),
    ])
    expect(turn.segments).toEqual([
      {
        type: 'cop',
        title: null,
        items: [
          th('plan', 1, 2),
          {kind: 'call',
            call: { toolCallId: 'c1', toolName: 'read_file', arguments: {}, result: undefined },
            seq: 2,
          },
        ],
      },
    ])
  })

  it('忽略 end_reply，不让它进入 cop 段', () => {
    const turn = buildAssistantTurnFromAgentEvents([
      ev('r1', 1, 'tool-call', { tool_name: 'end_reply', tool_call_id: 'end_1', arguments: {} }),
      ev('r1', 2, 'tool-result', { tool_name: 'end_reply', tool_call_id: 'end_1', result: { status: 'reply_ended' } }),
      ev('r1', 3, 'assistant-delta', { role: 'assistant', content_delta: '可见正文' }),
    ])
    expect(turn.segments).toEqual([{ type: 'text', content: '可见正文' }])
  })

  it('tool 之间的正文拆成独立 text，且位于两个 cop 之间（规范 §6 结构）', () => {
    const events: AgentUIEvent[] = [
      ev('r1', 1, 'assistant-delta', { role: 'assistant', content_delta: '我来帮你读取 skills...' }),
      ev('r1', 2, 'tool-call', {
        tool_name: 'load_tools',
        tool_call_id: 'c1',
        arguments: { q: 'x' },
      }),
      ev('r1', 3, 'tool-call', {
        tool_name: 'read_file',
        tool_call_id: 'c2',
        arguments: { path: '/a' },
      }),
      ev('r1', 4, 'tool-result', {
        tool_name: 'load_tools',
        tool_call_id: 'c1',
        result: { ok: true },
      }),
      ev('r1', 5, 'tool-result', {
        tool_name: 'read_file',
        tool_call_id: 'c2',
        result: null,
      }),
      ev('r1', 6, 'assistant-delta', { role: 'assistant', content_delta: '让我重新读取：' }),
      ev('r1', 7, 'tool-call', {
        tool_name: 'read_file',
        tool_call_id: 'c3',
        arguments: { path: '/b' },
      }),
      ev('r1', 8, 'tool-result', {
        tool_name: 'read_file',
        tool_call_id: 'c3',
        result: { content: 'x' },
      }),
    ]

    const turn = buildAssistantTurnFromAgentEvents(events)
    expect(turn.segments).toEqual([
      { type: 'text', content: '我来帮你读取 skills...' },
      {
        type: 'cop',
        title: null,
        items: [
          {kind: 'call',
            call: {
              toolCallId: 'c1',
              toolName: 'load_tools',
              arguments: { q: 'x' },
              result: { ok: true },
            },
            seq: 2,
          },
          {kind: 'call',
            call: {
              toolCallId: 'c2',
              toolName: 'read_file',
              arguments: { path: '/a' },
              result: null,
            },
            seq: 3,
          },
        ],
      },
      { type: 'text', content: '让我重新读取：' },
      {
        type: 'cop',
        title: null,
        items: [
          {kind: 'call',
            call: {
              toolCallId: 'c3',
              toolName: 'read_file',
              arguments: { path: '/b' },
              result: { content: 'x' },
            },
            seq: 7,
          },
        ],
      },
    ])
    expect(assistantTurnPlainText(turn)).toBe('我来帮你读取 skills...让我重新读取：')
  })

  it('工具之间仅空白 message.delta 不拆分 cop', () => {
    const turn = buildAssistantTurnFromAgentEvents([
      ev('r1', 1, 'assistant-delta', { role: 'assistant', content_delta: '读 skills：' }),
      ev('r1', 2, 'tool-call', { tool_name: 'cat', tool_call_id: 't1', arguments: {} }),
      ev('r1', 3, 'tool-call', { tool_name: 'cat', tool_call_id: 't2', arguments: {} }),
      ev('r1', 4, 'tool-call', { tool_name: 'cat', tool_call_id: 't3', arguments: {} }),
      ev('r1', 5, 'tool-call', { tool_name: 'cat', tool_call_id: 't4', arguments: {} }),
      ev('r1', 6, 'assistant-delta', { role: 'assistant', content_delta: '\n' }),
      ev('r1', 7, 'tool-call', { tool_name: 'cat', tool_call_id: 't5', arguments: {} }),
    ])
    expect(turn.segments).toHaveLength(2)
    expect(turn.segments[1]?.type).toBe('cop')
    if (turn.segments[1]?.type !== 'cop') throw new Error('expected cop')
    expect(copSegmentCalls(turn.segments[1])).toHaveLength(5)
  })

  it('工具段后的 thinking 归入前一个 cop segment', () => {
    const turn = buildAssistantTurnFromAgentEvents([
      ev('r1', 1, 'assistant-delta', { role: 'assistant', content_delta: '先搜索：' }),
      ev('r1', 2, 'tool-call', { tool_name: 'grep', tool_call_id: 'grep_1', arguments: { pattern: 'platform' } }),
      ev('r1', 3, 'tool-result', { tool_name: 'grep', tool_call_id: 'grep_1', result: { matches: 'a.ts' } }),
      ev('r1', 4, 'assistant-delta', { role: 'assistant', channel: 'thinking', content_delta: '需要继续判断。' }),
      ev('r1', 5, 'assistant-delta', { role: 'assistant', content_delta: '结果是：' }),
    ])

    expect(turn.segments).toHaveLength(3)
    expect(turn.segments[1]?.type).toBe('cop')
    if (turn.segments[1]?.type !== 'cop') throw new Error('expected cop')
    expect(turn.segments[1].items.map((item) => item.kind)).toEqual(['call', 'thinking'])
    expect(turn.segments[2]).toEqual({ type: 'text', content: '结果是：' })
  })

  it('run.segment.end 后的新工具不能追加进前一个 cop segment', () => {
    const turn = buildAssistantTurnFromAgentEvents([
      ev('r1', 1, 'segment-start', { segment_id: 'seg_1', type: 'planning_round' }),
      ev('r1', 2, 'tool-call', { tool_name: 'grep', tool_call_id: 'grep_1', arguments: { pattern: 'export' } }),
      ev('r1', 3, 'tool-result', { tool_name: 'grep', tool_call_id: 'grep_1', result: { matches: 2 } }),
      ev('r1', 4, 'segment-end', { segment_id: 'seg_1' }),
      ev('r1', 5, 'segment-start', { segment_id: 'seg_2', type: 'planning_round' }),
      ev('r1', 6, 'tool-call', { tool_name: 'read', tool_call_id: 'read_1', arguments: { path: 'ExportPanel.tsx' } }),
      ev('r1', 7, 'tool-result', { tool_name: 'read', tool_call_id: 'read_1', result: { content: 'export function ExportPanel() {}' } }),
      ev('r1', 8, 'segment-end', { segment_id: 'seg_2' }),
    ])

    expect(turn.segments).toHaveLength(2)
    expect(turn.segments[0]?.type).toBe('cop')
    expect(turn.segments[1]?.type).toBe('cop')
    if (turn.segments[0]?.type !== 'cop' || turn.segments[1]?.type !== 'cop') throw new Error('expected cop segments')
    expect(copSegmentCalls(turn.segments[0]).map((call) => call.toolCallId)).toEqual(['grep_1'])
    expect(copSegmentCalls(turn.segments[1]).map((call) => call.toolCallId)).toEqual(['read_1'])
  })

  it('exec_command 在 reducer 层切断前后工具段', () => {
    const turn = buildAssistantTurnFromAgentEvents([
      ev('r1', 1, 'tool-call', { tool_name: 'memory_search', tool_call_id: 'mem_1', arguments: { query: '清风' } }),
      ev('r1', 2, 'tool-result', { tool_name: 'memory_search', tool_call_id: 'mem_1', result: { hits: [] } }),
      ev('r1', 3, 'tool-call', { tool_name: 'exec_command', tool_call_id: 'cmd_1', arguments: { command: 'pwd' } }),
      ev('r1', 4, 'tool-result', { tool_name: 'exec_command', tool_call_id: 'cmd_1', result: { exit_code: 0 } }),
      ev('r1', 5, 'tool-call', { tool_name: 'write_file', tool_call_id: 'write_1', arguments: { file_path: 'hello.py' } }),
    ])

    expect(turn.segments).toHaveLength(3)
    expect(turn.segments.map((segment) => segment.type)).toEqual(['cop', 'cop', 'cop'])
    const calls = turn.segments.map((segment) => {
      if (segment.type !== 'cop') throw new Error('expected cop')
      return copSegmentCalls(segment).map((call) => call.toolName)
    })
    expect(calls).toEqual([
      ['memory_search'],
      ['exec_command'],
      ['write_file'],
    ])
  })

  it('timeline_title 不会挂到 exec_command 段上', () => {
    const turn = buildAssistantTurnFromAgentEvents([
      ev('r1', 1, 'tool-call', {
        tool_name: 'timeline_title',
        tool_call_id: 'title_1',
        arguments: { label: 'Gathering my thoughts' },
      }),
      ev('r1', 2, 'tool-call', { tool_name: 'exec_command', tool_call_id: 'cmd_1', arguments: { command: 'pwd' } }),
    ])

    expect(turn.segments).toHaveLength(1)
    expect(turn.segments[0]).toMatchObject({
      type: 'cop',
      title: null,
    })
    if (turn.segments[0]?.type !== 'cop') throw new Error('expected cop')
    expect(copSegmentCalls(turn.segments[0]).map((call) => call.toolName)).toEqual(['exec_command'])
  })

  it('timeline_title 仅设置 cop.title，不进入 items', () => {
    const turn = buildAssistantTurnFromAgentEvents([
      ev('r1', 1, 'tool-call', {
        tool_name: 'timeline_title',
        tool_call_id: 't1',
        arguments: { label: '读取 Skills' },
      }),
      ev('r1', 2, 'tool-call', {
        tool_name: 'load_tools',
        tool_call_id: 'c1',
        arguments: {},
      }),
    ])
    expect(turn.segments).toHaveLength(1)
    expect(turn.segments[0]).toEqual({
      type: 'cop',
      title: '读取 Skills',
      items: [
        {kind: 'call',
          call: { toolCallId: 'c1', toolName: 'load_tools', arguments: {}, result: undefined },
          seq: 2,
        },
      ],
    })
  })

  it('seq 乱序时按 seq+ts 排序后折叠', () => {
    const turn = buildAssistantTurnFromAgentEvents([
      ev('r1', 2, 'assistant-delta', { role: 'assistant', content_delta: 'second' }),
      ev('r1', 1, 'assistant-delta', { role: 'assistant', content_delta: 'first' }),
    ])
    expect(turn.segments).toEqual([{ type: 'text', content: 'firstsecond' }])
  })

  it('cop 内找不到 call 时挂占位 tool 行', () => {
    const turn = buildAssistantTurnFromAgentEvents([
      ev('r1', 1, 'tool-call', { tool_name: 'exec_command', tool_call_id: 'c1', arguments: { command: 'ls' } }),
      ev('r1', 2, 'tool-result', {
        tool_name: 'exec_command',
        tool_call_id: 'orphan',
        result: { out: 1 },
      }),
    ])
    const first = turn.segments[0]
    expect(first?.type).toBe('cop')
    if (first?.type !== 'cop') throw new Error('expected cop')
    const calls = copSegmentCalls(first)
    expect(calls).toHaveLength(2)
    expect(calls[0]?.toolCallId).toBe('c1')
    expect(calls[1]).toMatchObject({
      toolCallId: 'orphan',
      toolName: 'exec_command',
      arguments: {},
      result: { out: 1 },
    })
  })

  it('可见正文切段后，晚到 tool.result 仍回填到旧 cop', () => {
    const turn = buildAssistantTurnFromAgentEvents([
      ev('r1', 1, 'tool-call', { tool_name: 'fetch_url', tool_call_id: 'c1', arguments: { url: 'https://a.test' } }),
      ev('r1', 2, 'assistant-delta', { role: 'assistant', content_delta: '先给你结论。' }),
      ev('r1', 3, 'tool-result', {
        tool_name: 'fetch_url',
        tool_call_id: 'c1',
        result: { title: 'A' },
      }),
    ])
    expect(turn.segments).toEqual([
      {
        type: 'cop',
        title: null,
        items: [
          {kind: 'call',
            call: {
              toolCallId: 'c1',
              toolName: 'fetch_url',
              arguments: { url: 'https://a.test' },
              result: { title: 'A' },
              errorClass: undefined,
            },
            seq: 1,
          },
        ],
      },
      { type: 'text', content: '先给你结论。' },
    ])
  })

  it('空 timeline_title 后短正文仍为独立 text 段', () => {
    const turn = buildAssistantTurnFromAgentEvents([
      ev('r1', 1, 'tool-call', {
        tool_name: 'timeline_title',
        tool_call_id: 't1',
        arguments: { label: '' },
      }),
      ev('r1', 2, 'assistant-delta', { role: 'assistant', content_delta: 'hi' }),
    ])
    expect(turn.segments).toEqual([{ type: 'text', content: 'hi' }])
  })

  it('run events 重放时 open thinking 用最后事件时间收口，而不是当前时间', () => {
    const turn = buildAssistantTurnFromAgentEvents([
      ev('r1', 1, 'assistant-delta', { role: 'assistant', channel: 'thinking', content_delta: 'plan' }),
      ev('r1', 2, 'run-completed', {}),
    ])
    expect(turn.segments).toEqual([
      {
        type: 'cop',
        title: null,
        items: [th('plan', 1, 2)],
      },
    ])
  })
})
