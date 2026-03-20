import { describe, expect, it } from 'vitest'
import { buildTurns, type RunEventRaw } from './run-turns'

function makeEvent(params: {
  seq: number
  type: string
  data?: Record<string, unknown>
}): RunEventRaw {
  return {
    event_id: `evt_${params.seq}`,
    run_id: 'run_1',
    seq: params.seq,
    ts: '2026-03-19T10:19:42.000Z',
    type: params.type,
    data: params.data ?? {},
  }
}

describe('buildTurns', () => {
  it('extracts telegram envelope input and final assistant output', () => {
    const turns = buildTurns([
      makeEvent({
        seq: 1,
        type: 'llm.request',
        data: {
          llm_call_id: 'call_1',
          provider_kind: 'openai',
          api_mode: 'chat_completions',
          payload: {
            input: `---
channel-identity-id: "u1"
display-name: "清风"
channel: "telegram"
conversation-type: "private"
time: "2026-03-19T10:19:42Z"
---
我上一句话说的什么`,
          },
        },
      }),
      makeEvent({
        seq: 2,
        type: 'message.delta',
        data: {
          role: 'assistant',
          content_delta: '你上一句是：',
        },
      }),
      makeEvent({
        seq: 3,
        type: 'message.delta',
        data: {
          role: 'assistant',
          content_delta: '我上一句话说的什么',
        },
      }),
      makeEvent({
        seq: 4,
        type: 'llm.turn.completed',
        data: {
          llm_call_id: 'call_1',
          usage: { input_tokens: 10, output_tokens: 8 },
        },
      }),
    ])

    expect(turns).toHaveLength(1)
    expect(turns[0]?.userInput).toBe('我上一句话说的什么')
    expect(turns[0]?.inputMeta).toEqual({
      'channel-identity-id': 'u1',
      'display-name': '清风',
      channel: 'telegram',
      'conversation-type': 'private',
      time: '2026-03-19T10:19:42Z',
    })
    expect(turns[0]?.assistantText).toBe('你上一句是：我上一句话说的什么')
    expect(turns[0]?.segments).toEqual([
      {
        kind: 'assistant',
        text: '你上一句是：我上一句话说的什么',
        isFinal: true,
      },
    ])
  })

  it('keeps assistant preface, tool call, tool result and final output in one turn', () => {
    const turns = buildTurns([
      makeEvent({
        seq: 1,
        type: 'llm.request',
        data: {
          llm_call_id: 'call_1',
          provider_kind: 'openai',
          api_mode: 'chat_completions',
          payload: {
            messages: [{ role: 'user', content: '我需要多久才能翻倍到20万？' }],
            tools: [{ name: 'exec_command' }],
          },
        },
      }),
      makeEvent({
        seq: 2,
        type: 'message.delta',
        data: { role: 'assistant', content_delta: '我先算一下。' },
      }),
      makeEvent({
        seq: 3,
        type: 'tool.call',
        data: {
          tool_call_id: 'tool_1',
          tool_name: 'exec_command',
          arguments: { command: 'node calc.js' },
        },
      }),
      makeEvent({
        seq: 4,
        type: 'tool.result',
        data: {
          tool_call_id: 'tool_1',
          tool_name: 'exec_command',
          result: { output: '14.21 年' },
        },
      }),
      makeEvent({
        seq: 5,
        type: 'llm.request',
        data: {
          llm_call_id: 'call_2',
          provider_kind: 'openai',
          api_mode: 'chat_completions',
          payload: {
            messages: [
              { role: 'user', content: '我需要多久才能翻倍到20万？' },
              { role: 'assistant', content: '我先算一下。' },
              { role: 'tool', content: '{"output":"14.21 年"}' },
            ],
          },
        },
      }),
      makeEvent({
        seq: 6,
        type: 'message.delta',
        data: { role: 'assistant', content_delta: '需要 14.21 年。' },
      }),
      makeEvent({
        seq: 7,
        type: 'llm.turn.completed',
        data: {
          llm_call_id: 'call_2',
          usage: { input_tokens: 10, output_tokens: 8 },
        },
      }),
      makeEvent({
        seq: 8,
        type: 'run.completed',
        data: {},
      }),
    ])

    expect(turns).toHaveLength(1)
    expect(turns[0]?.userInput).toBe('我需要多久才能翻倍到20万？')
    expect(turns[0]?.assistantText).toBe('需要 14.21 年。')
    expect(turns[0]?.segments).toEqual([
      { kind: 'assistant', text: '我先算一下。', isFinal: false },
      {
        kind: 'tool_call',
        toolCallId: 'tool_1',
        toolName: 'exec_command',
        argsJSON: { command: 'node calc.js' },
      },
      {
        kind: 'tool_result',
        toolCallId: 'tool_1',
        toolName: 'exec_command',
        resultJSON: { output: '14.21 年' },
        errorClass: undefined,
      },
      { kind: 'assistant', text: '需要 14.21 年。', isFinal: true },
    ])
  })

  it('falls back to completed assistant text when no visible delta exists', () => {
    const turns = buildTurns([
      makeEvent({
        seq: 1,
        type: 'llm.request',
        data: {
          llm_call_id: 'call_1',
          provider_kind: 'openai',
          api_mode: 'responses',
          payload: { input: 'hello' },
        },
      }),
      makeEvent({
        seq: 2,
        type: 'message.delta',
        data: {
          channel: 'thinking',
          content_delta: 'internal',
        },
      }),
      makeEvent({
        seq: 3,
        type: 'llm.turn.completed',
        data: {
          llm_call_id: 'call_1',
          assistant_text: 'done',
        },
      }),
      makeEvent({
        seq: 4,
        type: 'run.completed',
        data: {},
      }),
    ])

    expect(turns).toHaveLength(1)
    expect(turns[0]?.assistantText).toBe('done')
    expect(turns[0]?.segments).toEqual([
      { kind: 'assistant', text: 'done', isFinal: true },
    ])
  })

  it('estimates context tokens from llm.request payload byte stats', () => {
    const turns = buildTurns([
      makeEvent({
        seq: 1,
        type: 'llm.request',
        data: {
          llm_call_id: 'call_1',
          provider_kind: 'openai',
          api_mode: 'chat_completions',
          system_bytes: 40,
          tools_bytes: 40,
          messages_bytes: 320,
          payload: { messages: [{ role: 'user', content: 'hi' }] },
        },
      }),
      makeEvent({
        seq: 2,
        type: 'llm.turn.completed',
        data: {
          llm_call_id: 'call_1',
          usage: { input_tokens: 9000, output_tokens: 10 },
        },
      }),
    ])

    expect(turns).toHaveLength(1)
    expect(turns[0]?.estimatedInputTokens).toBe(Math.floor((400 + 3) / 4))
    expect(turns[0]?.inputTokens).toBe(9000)
  })
})
