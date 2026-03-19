import { describe, expect, it } from 'vitest'
import { buildThreadTurns, type ThreadMessage } from '@arkloop/shared'

function message(params: Partial<ThreadMessage> & Pick<ThreadMessage, 'id' | 'role' | 'content' | 'created_at'>): ThreadMessage {
  return {
    id: params.id,
    role: params.role,
    content: params.content,
    created_at: params.created_at,
    run_id: params.run_id,
    content_json: params.content_json,
  }
}

describe('buildThreadTurns', () => {
  it('groups thread messages into user/assistant turns and marks the current run', () => {
    const turns = buildThreadTurns([
      message({ id: 'u1', role: 'user', content: '你是谁', created_at: '2026-03-19T10:19:34Z' }),
      message({ id: 'a1', role: 'assistant', content: '我是 Arkloop。', created_at: '2026-03-19T10:19:35Z', run_id: 'run-1' }),
      message({ id: 'u2', role: 'user', content: '我上一句话说的什么', created_at: '2026-03-19T10:19:42Z' }),
      message({ id: 'a2', role: 'assistant', content: '你的上一句话是“你是谁”。', created_at: '2026-03-19T10:19:43Z', run_id: 'run-2' }),
    ], 'run-2')

    expect(turns).toEqual([
      {
        key: 'u1',
        userText: '你是谁',
        assistantText: '我是 Arkloop。',
        isCurrent: false,
      },
      {
        key: 'u2',
        userText: '我上一句话说的什么',
        assistantText: '你的上一句话是“你是谁”。',
        isCurrent: true,
      },
    ])
  })
})
