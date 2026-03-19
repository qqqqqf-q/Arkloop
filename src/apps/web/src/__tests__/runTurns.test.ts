import { describe, expect, it } from 'vitest'
import { buildTurns, type RunEventRaw } from '@arkloop/shared/run-turns'

function makeEvent(params: {
  seq: number
  type: string
  data?: Record<string, unknown>
}): RunEventRaw {
  return {
    event_id: `evt_${params.seq}`,
    run_id: 'run_1',
    seq: params.seq,
    ts: '2026-03-18T00:00:00.000Z',
    type: params.type,
    data: params.data ?? {},
  }
}

describe('buildTurns', () => {
  it('应忽略 thinking channel，只保留最终 assistant 输出', () => {
    const turns = buildTurns([
      makeEvent({
        seq: 1,
        type: 'llm.request',
        data: {
          llm_call_id: 'call_1',
          payload: {
            messages: [{ role: 'user', content: '帮我查一下' }],
          },
        },
      }),
      makeEvent({
        seq: 2,
        type: 'message.delta',
        data: { role: 'assistant', channel: 'thinking', content_delta: '先想一下' },
      }),
      makeEvent({
        seq: 3,
        type: 'message.delta',
        data: { role: 'assistant', content_delta: '我先检查一下现有工具。' },
      }),
      makeEvent({
        seq: 4,
        type: 'run.completed',
        data: {},
      }),
    ])

    expect(turns).toHaveLength(1)
    expect(turns[0]?.assistantText).toBe('我先检查一下现有工具。')
    expect(turns[0]?.segments).toEqual([
      { kind: 'assistant', text: '我先检查一下现有工具。', isFinal: true },
    ])
  })

  it('应把同一用户输入内的多次 llm.request 合并为一个 turn', () => {
    const turns = buildTurns([
      makeEvent({
        seq: 1,
        type: 'llm.request',
        data: {
          llm_call_id: 'call_1',
          provider_kind: 'openai',
          api_mode: 'chat_completions',
          payload: {
            model: 'minimax/minimax-m2.7',
            messages: [{ role: 'user', content: '我需要多久才能翻倍到20万？' }],
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
          arguments: { command: 'node -e "console.log(14.21)"' },
        },
      }),
      makeEvent({
        seq: 4,
        type: 'tool.result',
        data: {
          tool_call_id: 'tool_1',
          tool_name: 'exec_command',
          result: { output: '14.21' },
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
            model: 'minimax/minimax-m2.7',
            messages: [
              { role: 'user', content: '我需要多久才能翻倍到20万？' },
              { role: 'assistant', content: '我先算一下。' },
              { role: 'tool', content: '{"output":"14.21"}' },
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
        argsJSON: { command: 'node -e "console.log(14.21)"' },
      },
      {
        kind: 'tool_result',
        toolCallId: 'tool_1',
        toolName: 'exec_command',
        resultJSON: { output: '14.21' },
        errorClass: undefined,
      },
      { kind: 'assistant', text: '需要 14.21 年。', isFinal: true },
    ])
  })

  it('应从 telegram envelope 提取正文，不重复摊开 request payload 历史', () => {
    const turns = buildTurns([
      makeEvent({
        seq: 1,
        type: 'llm.request',
        data: {
          llm_call_id: 'call_1',
          provider_kind: 'openai',
          api_mode: 'chat_completions',
          payload: {
            model: 'openai/gpt-4o-mini',
            messages: [
              { role: 'system', content: '你是Arkloop' },
              {
                role: 'user',
                content: '---\nchannel-identity-id: "551ab5d6-0239-46d7-bf91-2ef87c9454d0"\ndisplay-name: "清凤"\nchannel: "telegram"\nconversation-type: "private"\ntime: "2026-03-19T10:19:34Z"\n---\n你是谁',
              },
              {
                role: 'assistant',
                content: '我是Arkloop，很高兴见到你！我可以帮助你回答问题、提供信息或讨论各种话题。有任何想了解的，请随时问我。',
              },
              {
                role: 'user',
                content: '---\nchannel-identity-id: "551ab5d6-0239-46d7-bf91-2ef87c9454d0"\ndisplay-name: "清凤"\nchannel: "telegram"\nconversation-type: "private"\ntime: "2026-03-19T10:19:42Z"\n---\n我上一句话说的什么',
              },
            ],
          },
        },
      }),
      makeEvent({
        seq: 2,
        type: 'message.delta',
        data: { role: 'assistant', content_delta: '你上一句话是问：“你是谁”。' },
      }),
      makeEvent({
        seq: 3,
        type: 'run.completed',
        data: {},
      }),
    ])

    expect(turns).toHaveLength(1)
    expect(turns[0]?.userInput).toBe('我上一句话说的什么')
    expect(turns[0]?.inputMeta?.channel).toBe('telegram')
    expect(turns[0]?.segments).toEqual([
      { kind: 'assistant', text: '你上一句话是问：“你是谁”。', isFinal: true },
    ])
  })
})
