import { describe, expect, it } from 'vitest'
import { resolveMessageSourcesForRender } from '../components/chatSourceResolver'
import type { MessageResponse } from '../api'
import type { WebSource } from '../storage'

function message(id: string, role: string, content: string): MessageResponse {
  return {
    id,
    role,
    content,
    account_id: 'org_1',
    thread_id: 'thread_1',
    created_by_user_id: 'user_1',
    created_at: '2026-01-01T00:00:00Z',
  }
}

const exampleSources: WebSource[] = [{ title: 'Example', url: 'https://example.com' }]

describe('resolveMessageSourcesForRender', () => {
  it('应优先使用消息自身的 sources', () => {
    const messages = [message('a1', 'assistant', '回答')]
    const map = new Map<string, WebSource[]>([['a1', exampleSources]])

    const resolved = resolveMessageSourcesForRender(messages, map)

    expect(resolved.get('a1')).toBe(exampleSources)
  })

  it('后续消息仅有 Web:n 引用时应回退到最近来源', () => {
    const messages = [
      message('a1', 'assistant', '第一轮搜索结果'),
      message('a2', 'assistant', '补充说明 Web:1'),
    ]
    const map = new Map<string, WebSource[]>([['a1', exampleSources]])

    const resolved = resolveMessageSourcesForRender(messages, map)

    expect(resolved.get('a2')).toBe(exampleSources)
  })

  it('无引用标记时不应错误复用前文来源', () => {
    const messages = [
      message('a1', 'assistant', '第一轮搜索结果'),
      message('a2', 'assistant', '普通追问回答'),
    ]
    const map = new Map<string, WebSource[]>([['a1', exampleSources]])

    const resolved = resolveMessageSourcesForRender(messages, map)

    expect(resolved.has('a2')).toBe(false)
  })
})
