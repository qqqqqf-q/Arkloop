import { describe, expect, it } from 'vitest'
import type { AgentMessage } from '../agent-ui'
import {
  buildOptimisticUserMessage,
  mergeUndeliveredLocalUserMessages,
  withMessageDeliveryStatus,
} from '../messageContent'

function userMessage(id: string, content: string, createdAt: string, clientMessageId: string): AgentMessage {
  return {
    id,
    role: 'user',
    content,
    createdAt,
    metadata: {
      createdAt,
      clientMessageId,
      deliveryStatus: 'sent',
    },
    parts: [{ type: 'text', text: content, state: 'done' }],
  }
}

function assistantMessage(id: string, content: string, createdAt: string): AgentMessage {
  return {
    id,
    role: 'assistant',
    content,
    createdAt,
    metadata: { createdAt },
    parts: [{ type: 'text', text: content, state: 'done' }],
  }
}

describe('mergeUndeliveredLocalUserMessages', () => {
  it('保留远端刷新里还不存在的本地 failed user prompt', () => {
    const firstClientMessageId = '11111111-1111-4111-8111-111111111111'
    const firstLocal = withMessageDeliveryStatus(
      buildOptimisticUserMessage({ content: 'first' }, firstClientMessageId, '2026-03-10T00:00:00Z'),
      'failed',
      firstClientMessageId,
    )
    const remote = [
      userMessage('msg-2', 'second', '2026-03-10T00:00:02Z', '22222222-2222-4222-8222-222222222222'),
      assistantMessage('msg-3', 'reply', '2026-03-10T00:00:03Z'),
    ]

    const merged = mergeUndeliveredLocalUserMessages(remote, [firstLocal])

    expect(merged.map((message) => message.content)).toEqual(['first', 'second', 'reply'])
    expect(merged[0].metadata?.deliveryStatus).toBe('failed')
  })

  it('远端已经确认同一个 client message 时不重复保留本地 prompt', () => {
    const clientMessageId = '33333333-3333-4333-8333-333333333333'
    const local = buildOptimisticUserMessage({ content: 'hello' }, clientMessageId, '2026-03-10T00:00:00Z')
    const remote = [
      userMessage('msg-remote', 'hello', '2026-03-10T00:00:01Z', clientMessageId),
    ]

    const merged = mergeUndeliveredLocalUserMessages(remote, [local])

    expect(merged).toHaveLength(1)
    expect(merged[0].id).toBe('msg-remote')
  })
})
