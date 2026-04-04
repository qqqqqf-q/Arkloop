import { describe, expect, it } from 'vitest'
import { ACP_DELEGATE_LAYER } from './runEventDelegate'
import { buildTurns, type RunEventRaw } from './run-turns'
import { buildRequestThreadTurns } from './thread-turns'

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

  it('extracts compacted telegram group burst input', () => {
    const turns = buildTurns([
      makeEvent({
        seq: 1,
        type: 'llm.request',
        data: {
          llm_call_id: 'call_group_1',
          provider_kind: 'openai',
          api_mode: 'chat_completions',
          payload: {
            messages: [
              { role: 'system', content: '你是Arkloop' },
              { role: 'assistant', content: '在的，有什么事吗？' },
              {
                role: 'user',
                content: `---
channel: "telegram"
conversation-type: "supergroup"
conversation-title: "Arkloop"
---
[13:31:00] A ck: xhelogo
[13:31:05] A ck: 怎么那么像
[13:31:16] 清凤: 哈`,
              },
            ],
          },
        },
      }),
      makeEvent({
        seq: 2,
        type: 'message.delta',
        data: {
          role: 'assistant',
          content_delta: '我看到了你们刚才的几条群消息。',
        },
      }),
      makeEvent({
        seq: 3,
        type: 'llm.turn.completed',
        data: {
          llm_call_id: 'call_group_1',
        },
      }),
    ])

    expect(turns).toHaveLength(1)
    expect(turns[0]?.userInput).toBe(`[13:31:00] A ck: xhelogo
[13:31:05] A ck: 怎么那么像
[13:31:16] 清凤: 哈`)
    expect(turns[0]?.inputMeta).toEqual({
      channel: 'telegram',
      'conversation-type': 'supergroup',
      'conversation-title': 'Arkloop',
    })
    expect(turns[0]?.assistantText).toBe('我看到了你们刚才的几条群消息。')
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

  it('strips split end_turn control tokens from visible assistant output', () => {
    const turns = buildTurns([
      makeEvent({
        seq: 1,
        type: 'llm.request',
        data: {
          llm_call_id: 'call_1',
          provider_kind: 'anthropic',
          api_mode: 'messages',
          payload: { messages: [{ role: 'user', content: 'heartbeat' }] },
        },
      }),
      makeEvent({
        seq: 2,
        type: 'message.delta',
        data: {
          role: 'assistant',
          content_delta: '<end',
        },
      }),
      makeEvent({
        seq: 3,
        type: 'message.delta',
        data: {
          role: 'assistant',
          content_delta: '_turn>\n看到了',
        },
      }),
      makeEvent({
        seq: 4,
        type: 'llm.turn.completed',
        data: {
          llm_call_id: 'call_1',
        },
      }),
    ])

    expect(turns).toHaveLength(1)
    expect(turns[0]?.assistantText).toBe('看到了')
    expect(turns[0]?.segments).toEqual([
      { kind: 'assistant', text: '看到了', isFinal: true },
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

  it('uses completed usage as final context and preserves estimate for debug', () => {
    const turns = buildTurns([
      makeEvent({
        seq: 1,
        type: 'llm.request',
        data: {
          llm_call_id: 'call_1',
          provider_kind: 'anthropic',
          api_mode: 'messages',
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
          last_request_context_estimate_tokens: 123,
          usage: {
            input_tokens: 9000,
            cache_read_input_tokens: 200,
            cache_creation_input_tokens: 50,
            output_tokens: 10,
          },
        },
      }),
    ])

    expect(turns).toHaveLength(1)
    expect(turns[0]?.estimatedInputTokens).toBe(123)
    expect(turns[0]?.contextTokens).toBe(9250)
    expect(turns[0]?.inputTokens).toBe(9000)
  })

  it('captures actual request messages for each llm request', () => {
    const turns = buildTurns([
      makeEvent({
        seq: 1,
        type: 'llm.request',
        data: {
          llm_call_id: 'call_1',
          provider_kind: 'anthropic',
          api_mode: 'messages',
          payload: {
            messages: [
              { role: 'system', content: 'system prompt' },
              { role: 'user', content: 'first input' },
            ],
          },
        },
      }),
      makeEvent({
        seq: 2,
        type: 'tool.call',
        data: {
          tool_call_id: 'tool_1',
          tool_name: 'test_tool',
          arguments: { ok: true },
        },
      }),
      makeEvent({
        seq: 3,
        type: 'llm.request',
        data: {
          llm_call_id: 'call_2',
          provider_kind: 'anthropic',
          api_mode: 'messages',
          payload: {
            messages: [
              { role: 'system', content: 'system prompt' },
              { role: 'user', content: 'first input' },
              { role: 'assistant', content: 'thinking' },
              { role: 'tool', content: '{"ok":true}' },
            ],
          },
        },
      }),
    ])

    expect(turns).toHaveLength(1)
    expect(turns[0]?.requests).toEqual([
      {
        llmCallId: 'call_1',
        messageCount: 2,
        messages: [
          { role: 'system', text: 'system prompt' },
          { role: 'user', text: 'first input' },
        ],
      },
      {
        llmCallId: 'call_2',
        messageCount: 4,
        messages: [
          { role: 'system', text: 'system prompt' },
          { role: 'user', text: 'first input' },
          { role: 'assistant', text: 'thinking' },
          { role: 'tool', text: '{"ok":true}' },
        ],
      },
    ])
  })

  it('builds thread tab data from latest request snapshot instead of database thread history', () => {
    const turns = buildTurns([
      makeEvent({
        seq: 1,
        type: 'llm.request',
        data: {
          llm_call_id: 'call_1',
          provider_kind: 'anthropic',
          api_mode: 'messages',
          payload: {
            messages: [
              { role: 'system', content: 'sys' },
              { role: 'user', content: '07:03 message' },
            ],
          },
        },
      }),
      makeEvent({
        seq: 2,
        type: 'llm.turn.completed',
        data: {
          llm_call_id: 'call_1',
          assistant_text: 'done',
        },
      }),
    ])

    expect(buildRequestThreadTurns(turns)).toEqual([
      {
        key: 'call_1',
        messages: [
          { role: 'system', text: 'sys' },
          { role: 'user', text: '07:03 message' },
        ],
        assistantText: 'done',
        isCurrent: true,
      },
    ])
  })

  it('normalizes telegram envelope request messages for debug display', () => {
    const turns = buildTurns([
      makeEvent({
        seq: 1,
        type: 'llm.request',
        data: {
          llm_call_id: 'call_1',
          provider_kind: 'openai',
          api_mode: 'chat_completions',
          payload: {
            messages: [
              {
                role: 'user',
                content: `---
display-name: "清凤"
channel: "telegram"
conversation-type: "private"
sender-ref: "cf842dbb-a8e5-4d0c-876d-533c8d0d1b11"
platform-username: "chiffoncha"
conversation-title: "chiffoncha"
forward-from: "清凤"
message-id: "616"
time: "2026-04-04T06:21:00Z"
---
[Telegram] 还几把是在高速服务区？？`,
              },
            ],
          },
        },
      }),
      makeEvent({
        seq: 2,
        type: 'llm.turn.completed',
        data: {
          llm_call_id: 'call_1',
          assistant_text: 'ok',
        },
      }),
    ])

    expect(buildRequestThreadTurns(turns)).toEqual([
      {
        key: 'call_1',
        messages: [
          {
            role: 'user',
            text: `[Fwd: 清凤]
还几把是在高速服务区？？`,
          },
        ],
        assistantText: 'ok',
        isCurrent: true,
      },
    ])
  })

  it('keeps tool segments when Anthropic wraps tool_result in role:user messages', () => {
    const turns = buildTurns([
      makeEvent({
        seq: 1,
        type: 'llm.request',
        data: {
          llm_call_id: 'call_1',
          provider_kind: 'anthropic',
          api_mode: 'messages',
          payload: {
            messages: [
              { role: 'user', content: '搜一下群里之前的聊天' },
            ],
          },
        },
      }),
      makeEvent({
        seq: 2,
        type: 'message.delta',
        data: { role: 'assistant', content_delta: '能搜喵，试一下——' },
      }),
      makeEvent({ seq: 3, type: 'llm.turn.completed', data: { llm_call_id: 'call_1' } }),
      makeEvent({
        seq: 4,
        type: 'tool.call',
        data: {
          tool_call_id: 'tc_1',
          tool_name: 'group_history_search',
          arguments: { query: '连云港煎蛋' },
        },
      }),
      makeEvent({
        seq: 5,
        type: 'tool.result',
        data: {
          tool_call_id: 'tc_1',
          tool_name: 'group_history_search',
          result: { messages: ['found something'] },
        },
      }),
      // Anthropic 风格: tool_result 以 role:"user" 出现
      makeEvent({
        seq: 6,
        type: 'llm.request',
        data: {
          llm_call_id: 'call_2',
          provider_kind: 'anthropic',
          api_mode: 'messages',
          payload: {
            messages: [
              { role: 'user', content: '搜一下群里之前的聊天' },
              { role: 'assistant', content: [{ type: 'text', text: '能搜喵' }, { type: 'tool_use', id: 'tc_1', name: 'group_history_search', input: {} }] },
              { role: 'user', content: [{ type: 'tool_result', tool_use_id: 'tc_1', content: 'found something' }] },
            ],
          },
        },
      }),
      makeEvent({
        seq: 7,
        type: 'message.delta',
        data: { role: 'assistant', content_delta: '搜到了一些历史消息。' },
      }),
      makeEvent({ seq: 8, type: 'llm.turn.completed', data: { llm_call_id: 'call_2' } }),
      makeEvent({ seq: 9, type: 'run.completed', data: {} }),
    ])

    // isToolResultOnlyMessage 过滤 tool_result user 消息后，userMessageCount 不膨胀 -> 1 turn
    expect(turns).toHaveLength(1)
    const toolCalls = turns[0]?.segments.filter((s) => s.kind === 'tool_call')
    const toolResults = turns[0]?.segments.filter((s) => s.kind === 'tool_result')
    expect(toolCalls).toHaveLength(1)
    expect(toolResults).toHaveLength(1)
    expect(turns[0]?.assistantText).toBe('搜到了一些历史消息。')
  })

  it('keeps tool segments when Anthropic tool_result increases userMessageCount', () => {
    // Anthropic 场景下 tool_result 以 role:"user" 出现，
    // 导致 userMessageCount 增加，shouldStartNewTurn 为 true
    const turns = buildTurns([
      makeEvent({
        seq: 1,
        type: 'llm.request',
        data: {
          llm_call_id: 'call_1',
          provider_kind: 'anthropic',
          api_mode: 'messages',
          payload: {
            messages: [
              { role: 'user', content: '查一下历史记录' },
            ],
          },
        },
      }),
      makeEvent({
        seq: 2,
        type: 'message.delta',
        data: { role: 'assistant', content_delta: '好的' },
      }),
      makeEvent({ seq: 3, type: 'llm.turn.completed', data: { llm_call_id: 'call_1' } }),
      makeEvent({
        seq: 4,
        type: 'tool.call',
        data: { tool_call_id: 'tc_1', tool_name: 'search', arguments: { q: 'test' } },
      }),
      makeEvent({
        seq: 5,
        type: 'tool.result',
        data: { tool_call_id: 'tc_1', tool_name: 'search', result: { data: 'ok' } },
      }),
      makeEvent({
        seq: 6,
        type: 'llm.request',
        data: {
          llm_call_id: 'call_2',
          provider_kind: 'anthropic',
          api_mode: 'messages',
          payload: {
            messages: [
              { role: 'user', content: '查一下历史记录' },
              { role: 'assistant', content: '好的' },
              // Anthropic: tool result 使用 role:"user"
              { role: 'user', content: [{ type: 'tool_result', tool_use_id: 'tc_1', content: 'ok' }] },
            ],
          },
        },
      }),
      makeEvent({
        seq: 7,
        type: 'message.delta',
        data: { role: 'assistant', content_delta: '查到了结果。' },
      }),
      makeEvent({ seq: 8, type: 'llm.turn.completed', data: { llm_call_id: 'call_2' } }),
      makeEvent({ seq: 9, type: 'run.completed', data: {} }),
    ])

    // 无论分成几个 turn，总 segments 中必须有 tool_call 和 tool_result
    const allSegments = turns.flatMap((t) => t.segments)
    const toolCallSegments = allSegments.filter((s) => s.kind === 'tool_call')
    const toolResultSegments = allSegments.filter((s) => s.kind === 'tool_result')
    expect(toolCallSegments).toHaveLength(1)
    expect(toolResultSegments).toHaveLength(1)

    // 检查是否因为 userMessageCount 增加而分成了 2 个 turn
    if (turns.length === 2) {
      // 如果分成 2 个 turn，tool segments 应该在第一个 turn 中
      const firstTurnToolCalls = turns[0]?.segments.filter((s) => s.kind === 'tool_call')
      const firstTurnToolResults = turns[0]?.segments.filter((s) => s.kind === 'tool_result')
      expect(firstTurnToolCalls).toHaveLength(1)
      expect(firstTurnToolResults).toHaveLength(1)
    }
  })

  it('ignores ACP delegate deltas/tools and inner run.completed; keeps host acp_agent tool result', () => {
    const delegate = { delegate_layer: ACP_DELEGATE_LAYER }
    const turns = buildTurns([
      makeEvent({
        seq: 1,
        type: 'llm.request',
        data: {
          llm_call_id: 'call_1',
          provider_kind: 'openai',
          api_mode: 'chat_completions',
          payload: {
            messages: [{ role: 'user', content: '用 opencode' }],
          },
        },
      }),
      makeEvent({
        seq: 2,
        type: 'message.delta',
        data: { ...delegate, role: 'assistant', content_delta: 'inner stream noise' },
      }),
      makeEvent({
        seq: 3,
        type: 'tool.call',
        data: {
          ...delegate,
          tool_call_id: 'inner_1',
          tool_name: 'read_file',
          arguments: {},
        },
      }),
      makeEvent({
        seq: 4,
        type: 'tool.result',
        data: {
          ...delegate,
          tool_call_id: 'inner_1',
          tool_name: 'read_file',
          result: { ok: true },
        },
      }),
      makeEvent({
        seq: 5,
        type: 'run.completed',
        data: { ...delegate, summary: 'inner done' },
      }),
      makeEvent({
        seq: 6,
        type: 'tool.call',
        data: {
          tool_call_id: 'host_acp',
          tool_name: 'acp_agent',
          arguments: { task: 'x' },
        },
      }),
      makeEvent({
        seq: 7,
        type: 'tool.result',
        data: {
          tool_call_id: 'host_acp',
          tool_name: 'acp_agent',
          result: { output: '最终结果' },
        },
      }),
      makeEvent({
        seq: 8,
        type: 'run.completed',
        data: {},
      }),
    ])

    expect(turns).toHaveLength(1)
    expect(turns[0]?.assistantText).toBe('')
    expect(turns[0]?.segments).toEqual([
      {
        kind: 'tool_call',
        toolCallId: 'host_acp',
        toolName: 'acp_agent',
        argsJSON: { task: 'x' },
      },
      {
        kind: 'tool_result',
        toolCallId: 'host_acp',
        toolName: 'acp_agent',
        resultJSON: { output: '最终结果' },
        errorClass: undefined,
      },
    ])
  })
})
