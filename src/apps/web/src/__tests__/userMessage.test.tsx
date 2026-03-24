import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { LocaleProvider } from '../contexts/LocaleContext'
import { UserMessage } from '../components/messagebubble/UserMessage'
import type { MessageResponse } from '../api'

function makeMessage(overrides: Partial<MessageResponse>): MessageResponse {
  return {
    id: 'msg-1',
    account_id: 'acc-1',
    thread_id: 'thread-1',
    created_by_user_id: 'user-1',
    role: 'user',
    content: '',
    created_at: '2026-03-24T00:00:00.000Z',
    ...overrides,
  }
}

function renderUserMessage(message: MessageResponse): string {
  return renderToStaticMarkup(
    <LocaleProvider>
      <UserMessage message={message} accessToken="token" />
    </LocaleProvider>,
  )
}

describe('UserMessage attachments', () => {
  it('pasted 文件只显示 pasted card，不再重复渲染文件名 chip', () => {
    const html = renderUserMessage(makeMessage({
      content_json: {
        parts: [
          {
            type: 'file',
            attachment: {
              key: 'file-1',
              filename: 'pasted-1774270948.txt',
              mime_type: 'text/plain',
              size: 992,
            },
            extracted_text: '第一行\n第二行',
          },
        ],
      },
    }))

    expect(html).toContain('PASTED')
    expect(html).not.toContain('pasted-1774270948.txt')
  })

  it('普通文件仍然显示下载 chip', () => {
    const html = renderUserMessage(makeMessage({
      content_json: {
        parts: [
          {
            type: 'file',
            attachment: {
              key: 'file-2',
              filename: 'notes.txt',
              mime_type: 'text/plain',
              size: 128,
            },
            extracted_text: 'hello',
          },
        ],
      },
    }))

    expect(html).toContain('notes.txt')
    expect(html).not.toContain('PASTED')
  })
})
