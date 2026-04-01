import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ACP_DELEGATE_LAYER } from '@arkloop/shared'
import {
  assistantTurnPlainText,
  buildAssistantTurnFromRunEvents,
  copSegmentCalls,
  createEmptyAssistantTurnFoldState,
  finalizeAssistantTurnFoldState,
  foldAssistantTurnEvent,
  requestAssistantTurnThinkingBreak,
} from '../assistantTurnSegments'
import type { RunEvent } from '../sse'

function ev(runId: string, seq: number, type: string, data?: unknown, errorClass?: string): RunEvent {
  return {
    event_id: `evt_${seq}`,
    run_id: runId,
    seq,
    ts: `2026-03-20T00:00:${String(seq).padStart(2, '0')}.000Z`,
    type,
    data: data ?? {},
    error_class: errorClass,
  }
}

const FINALIZE_NOW_MS = Date.parse('2026-03-21T00:00:00.000Z')
const evMs = (seq: number) => Date.parse(`2026-03-20T00:00:${String(seq).padStart(2, '0')}.000Z`)

function th(content: string, seq: number, endedByEventSeq?: number) {
  const startedAtMs = evMs(seq)
  const endedAtMs = endedByEventSeq == null ? FINALIZE_NOW_MS : evMs(endedByEventSeq)
  return { kind: 'thinking' as const, content, seq, startedAtMs, endedAtMs }
}

describe('buildAssistantTurnFromRunEvents', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(FINALIZE_NOW_MS)
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('requestAssistantTurnThinkingBreak 将连续 thinking 拆成多项', () => {
    const state = createEmptyAssistantTurnFoldState()
    foldAssistantTurnEvent(state, ev('r1', 1, 'message.delta', { role: 'assistant', channel: 'thinking', content_delta: 'a' }))
    requestAssistantTurnThinkingBreak(state)
    foldAssistantTurnEvent(state, ev('r1', 2, 'message.delta', { role: 'assistant', channel: 'thinking', content_delta: 'b' }))
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
    const turn = buildAssistantTurnFromRunEvents([
      ev('r1', 1, 'message.delta', { role: 'assistant', content_delta: 'a' }),
      ev('r1', 2, 'message.delta', { role: 'assistant', content_delta: 'b' }),
    ])
    expect(turn.segments).toEqual([{ type: 'text', content: 'ab' }])
  })

  it('thinking 后主通道正文独立成 text 段（不并进 COP 时间轴）', () => {
    const turn = buildAssistantTurnFromRunEvents([
      ev('r1', 1, 'message.delta', { role: 'assistant', channel: 'thinking', content_delta: 't1' }),
      ev('r1', 2, 'message.delta', { role: 'assistant', content_delta: 'visible' }),
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
    const turn = buildAssistantTurnFromRunEvents([
      ev('r1', 1, 'message.delta', { role: 'assistant', channel: 'thinking', content_delta: 'a' }),
      ev('r1', 2, 'message.delta', { role: 'assistant', content_delta: 'hi' }),
      ev('r1', 3, 'tool.call', { tool_name: 'read_file', tool_call_id: 'c1', arguments: {} }),
      ev('r1', 4, 'tool.result', { tool_name: 'read_file', tool_call_id: 'c1', result: {} }),
      ev('r1', 5, 'message.delta', { role: 'assistant', channel: 'thinking', content_delta: 'b' }),
      ev('r1', 6, 'message.delta', { role: 'assistant', content_delta: 'bye' }),
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
          {
            kind: 'call',
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
    const turn = buildAssistantTurnFromRunEvents([
      ev('r1', 1, 'message.delta', { role: 'assistant', channel: 'thinking', content_delta: 'plan' }),
      ev('r1', 2, 'message.delta', { role: 'assistant', content_delta: '我来查一下。' }),
      ev('r1', 3, 'tool.call', { tool_name: 'read_file', tool_call_id: 'c1', arguments: {} }),
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
          {
            kind: 'call',
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

  it('thinking 与首个 tool 同 cop（中间无正文）', () => {
    const turn = buildAssistantTurnFromRunEvents([
      ev('r1', 1, 'message.delta', { role: 'assistant', channel: 'thinking', content_delta: 'plan' }),
      ev('r1', 2, 'tool.call', { tool_name: 'read_file', tool_call_id: 'c1', arguments: {} }),
    ])
    expect(turn.segments).toEqual([
      {
        type: 'cop',
        title: null,
        items: [
          th('plan', 1, 2),
          {
            kind: 'call',
            call: { toolCallId: 'c1', toolName: 'read_file', arguments: {}, result: undefined },
            seq: 2,
          },
        ],
      },
    ])
  })

  it('忽略 ACP delegate_layer 的 delta 与工具事件', () => {
    const d = { delegate_layer: ACP_DELEGATE_LAYER }
    const turn = buildAssistantTurnFromRunEvents([
      ev('r1', 1, 'message.delta', { ...d, role: 'assistant', content_delta: 'inner' }),
      ev('r1', 2, 'tool.call', { ...d, tool_name: 'read_file', tool_call_id: 'x', arguments: {} }),
      ev('r1', 3, 'tool.result', { ...d, tool_name: 'read_file', tool_call_id: 'x', result: {} }),
      ev('r1', 4, 'message.delta', { role: 'assistant', content_delta: 'host' }),
    ])
    expect(turn.segments).toEqual([{ type: 'text', content: 'host' }])
  })

  it('tool 之间的正文拆成独立 text，且位于两个 cop 之间（规范 §6 结构）', () => {
    const events: RunEvent[] = [
      ev('r1', 1, 'message.delta', { role: 'assistant', content_delta: '我来帮你读取 skills...' }),
      ev('r1', 2, 'tool.call', {
        tool_name: 'load_tools',
        tool_call_id: 'c1',
        arguments: { q: 'x' },
      }),
      ev('r1', 3, 'tool.call', {
        tool_name: 'read_file',
        tool_call_id: 'c2',
        arguments: { path: '/a' },
      }),
      ev('r1', 4, 'tool.result', {
        tool_name: 'load_tools',
        tool_call_id: 'c1',
        result: { ok: true },
      }),
      ev('r1', 5, 'tool.result', {
        tool_name: 'read_file',
        tool_call_id: 'c2',
        result: null,
      }),
      ev('r1', 6, 'message.delta', { role: 'assistant', content_delta: '让我重新读取：' }),
      ev('r1', 7, 'tool.call', {
        tool_name: 'read_file',
        tool_call_id: 'c3',
        arguments: { path: '/b' },
      }),
      ev('r1', 8, 'tool.result', {
        tool_name: 'read_file',
        tool_call_id: 'c3',
        result: { content: 'x' },
      }),
    ]

    const turn = buildAssistantTurnFromRunEvents(events)
    expect(turn.segments).toEqual([
      { type: 'text', content: '我来帮你读取 skills...' },
      {
        type: 'cop',
        title: null,
        items: [
          {
            kind: 'call',
            call: {
              toolCallId: 'c1',
              toolName: 'load_tools',
              arguments: { q: 'x' },
              result: { ok: true },
            },
            seq: 2,
          },
          {
            kind: 'call',
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
          {
            kind: 'call',
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
    const turn = buildAssistantTurnFromRunEvents([
      ev('r1', 1, 'message.delta', { role: 'assistant', content_delta: '读 skills：' }),
      ev('r1', 2, 'tool.call', { tool_name: 'cat', tool_call_id: 't1', arguments: {} }),
      ev('r1', 3, 'tool.call', { tool_name: 'cat', tool_call_id: 't2', arguments: {} }),
      ev('r1', 4, 'tool.call', { tool_name: 'cat', tool_call_id: 't3', arguments: {} }),
      ev('r1', 5, 'tool.call', { tool_name: 'cat', tool_call_id: 't4', arguments: {} }),
      ev('r1', 6, 'message.delta', { role: 'assistant', content_delta: '\n' }),
      ev('r1', 7, 'tool.call', { tool_name: 'cat', tool_call_id: 't5', arguments: {} }),
    ])
    expect(turn.segments).toHaveLength(2)
    expect(turn.segments[1]?.type).toBe('cop')
    if (turn.segments[1]?.type !== 'cop') throw new Error('expected cop')
    expect(copSegmentCalls(turn.segments[1])).toHaveLength(5)
  })

  it('timeline_title 仅设置 cop.title，不进入 items', () => {
    const turn = buildAssistantTurnFromRunEvents([
      ev('r1', 1, 'tool.call', {
        tool_name: 'timeline_title',
        tool_call_id: 't1',
        arguments: { label: '读取 Skills' },
      }),
      ev('r1', 2, 'tool.call', {
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
        {
          kind: 'call',
          call: { toolCallId: 'c1', toolName: 'load_tools', arguments: {}, result: undefined },
          seq: 2,
        },
      ],
    })
  })

  it('seq 乱序时按 seq+ts 排序后折叠', () => {
    const turn = buildAssistantTurnFromRunEvents([
      ev('r1', 2, 'message.delta', { role: 'assistant', content_delta: 'second' }),
      ev('r1', 1, 'message.delta', { role: 'assistant', content_delta: 'first' }),
    ])
    expect(turn.segments).toEqual([{ type: 'text', content: 'firstsecond' }])
  })

  it('cop 内找不到 call 时挂占位 tool 行', () => {
    const turn = buildAssistantTurnFromRunEvents([
      ev('r1', 1, 'tool.call', { tool_name: 'exec_command', tool_call_id: 'c1', arguments: { command: 'ls' } }),
      ev('r1', 2, 'tool.result', {
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
    const turn = buildAssistantTurnFromRunEvents([
      ev('r1', 1, 'tool.call', { tool_name: 'fetch_url', tool_call_id: 'c1', arguments: { url: 'https://a.test' } }),
      ev('r1', 2, 'message.delta', { role: 'assistant', content_delta: '先给你结论。' }),
      ev('r1', 3, 'tool.result', {
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
          {
            kind: 'call',
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
    const turn = buildAssistantTurnFromRunEvents([
      ev('r1', 1, 'tool.call', {
        tool_name: 'timeline_title',
        tool_call_id: 't1',
        arguments: { label: '' },
      }),
      ev('r1', 2, 'message.delta', { role: 'assistant', content_delta: 'hi' }),
    ])
    expect(turn.segments).toEqual([{ type: 'text', content: 'hi' }])
  })

  it('run events 重放时 open thinking 用最后事件时间收口，而不是当前时间', () => {
    const turn = buildAssistantTurnFromRunEvents([
      ev('r1', 1, 'message.delta', { role: 'assistant', channel: 'thinking', content_delta: 'plan' }),
      ev('r1', 2, 'run.completed', {}),
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
