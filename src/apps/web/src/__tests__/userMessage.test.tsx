import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { renderToStaticMarkup } from 'react-dom/server'
import { LocaleProvider } from '../contexts/LocaleContext'
import { UserMessage } from '../components/messagebubble/UserMessage'
import type { AgentMessage } from '../agent-ui'
import {
  getUserPromptEnterScale,
  USER_PROMPT_ENTER_BASE_SCALE,
  USER_PROMPT_ENTER_MAX_SCALE,
  USER_PROMPT_MAX_WIDTH,
} from '../components/messagebubble/utils'

function makeMessage(overrides: Partial<AgentMessage>): AgentMessage {
  const content = overrides.content ?? ''
  return {
    id: 'msg-1',
    role: 'user',
    parts: content ? [{ type: 'text', text: content, state: 'done' }] : [],
    content,
    createdAt: '2026-03-24T00:00:00.000Z',
    metadata: { createdAt: '2026-03-24T00:00:00.000Z' },
    ...overrides,
  }
}

function renderUserMessage(message: AgentMessage): string {
  return renderToStaticMarkup(
    <LocaleProvider>
      <UserMessage message={message} accessToken="token" />
    </LocaleProvider>,
  )
}

function flushMicrotasks(): Promise<void> {
  return Promise.resolve()
    .then(() => Promise.resolve())
    .then(() => Promise.resolve())
}

const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

beforeEach(() => {
  actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
})

afterEach(() => {
  if (originalActEnvironment === undefined) {
    delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
  } else {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
  }
})

describe('UserMessage attachments', () => {
  it('pasted 文件只显示 pasted card，不再重复渲染文件名 chip', () => {
    const html = renderUserMessage(makeMessage({
      contentJson: {
        parts: [
          {
            type: 'file',
            attachment: {
              key: 'file-1',
              filename: 'pasted-1774270948.txt',
              mediaType: 'text/plain',
              size: 992,
            },
            extractedText: '第一行\n第二行',
          },
        ],
      },
    }))

    expect(html).toContain('PASTED')
    expect(html).not.toContain('pasted-1774270948.txt')
  })

  it('普通文件仍然显示下载 chip', () => {
    const html = renderUserMessage(makeMessage({
      contentJson: {
        parts: [
          {
            type: 'file',
            attachment: {
              key: 'file-2',
              filename: 'notes.txt',
              mediaType: 'text/plain',
              size: 128,
            },
            extractedText: 'hello',
          },
        ],
      },
    }))

    expect(html).toContain('notes.txt')
    expect(html).not.toContain('PASTED')
  })

  it('图片缩略图点击后把预览层挂到 body', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const originalFetch = globalThis.fetch
    const originalRaf = globalThis.requestAnimationFrame
    const createObjectURLSpy = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:uploaded-image')

    Object.defineProperty(globalThis, 'fetch', {
      configurable: true,
      writable: true,
      value: vi.fn(async () => new Response(new Blob(['image'], { type: 'image/png' }), { status: 200 })),
    })
    Object.defineProperty(globalThis, 'requestAnimationFrame', {
      configurable: true,
      writable: true,
      value: (callback: FrameRequestCallback) => {
        callback(0)
        return 1
      },
    })

    try {
      await act(async () => {
        root.render(
          <LocaleProvider>
            <UserMessage
              accessToken="token"
              message={makeMessage({
                contentJson: {
                  parts: [{
                    type: 'image',
                    attachment: {
                      key: 'image-1',
                      filename: 'shot.png',
                      mediaType: 'image/png',
                      size: 128,
                    },
                  }],
                },
              })}
            />
          </LocaleProvider>,
        )
      })
      await act(async () => {
        await flushMicrotasks()
      })

      const thumbnail = container.querySelector('img[alt="shot.png"]') as HTMLImageElement | null
      expect(thumbnail).toBeTruthy()
      if (!thumbnail) return

      await act(async () => {
        thumbnail.click()
        await flushMicrotasks()
      })

      expect(document.body.querySelector('a[href="blob:uploaded-image"]')).not.toBeNull()
      expect(container.querySelector('a[href="blob:uploaded-image"]')).toBeNull()
    } finally {
      act(() => {
        root.unmount()
      })
      container.remove()
      createObjectURLSpy.mockRestore()
      Object.defineProperty(globalThis, 'fetch', {
        configurable: true,
        writable: true,
        value: originalFetch,
      })
      Object.defineProperty(globalThis, 'requestAnimationFrame', {
        configurable: true,
        writable: true,
        value: originalRaf,
      })
    }
  })
})

describe('UserMessage enter animation compensation', () => {
  it('最宽消息保持基础倍率，短消息得到补偿', () => {
    expect(getUserPromptEnterScale(USER_PROMPT_MAX_WIDTH)).toBe(USER_PROMPT_ENTER_BASE_SCALE)
    expect(getUserPromptEnterScale(USER_PROMPT_MAX_WIDTH / 2)).toBeCloseTo(1.0425, 6)
    expect(getUserPromptEnterScale(120)).toBeGreaterThan(USER_PROMPT_ENTER_BASE_SCALE)
    expect(getUserPromptEnterScale(120)).toBeLessThanOrEqual(USER_PROMPT_ENTER_MAX_SCALE)
  })
})

describe('UserMessage overflow toggle', () => {
  it('长文本应渲染 show more 按钮并在点击后切换为 show less', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const originalScrollHeight = Object.getOwnPropertyDescriptor(HTMLDivElement.prototype, 'scrollHeight')

    Object.defineProperty(HTMLDivElement.prototype, 'scrollHeight', {
      configurable: true,
      get() {
        return 400
      },
    })

    try {
      await act(async () => {
        root.render(
          <LocaleProvider>
            <UserMessage
              accessToken="token"
              message={makeMessage({
                content: Array.from({ length: 20 }, (_, i) => `line ${i + 1}`).join('\n'),
              })}
            />
          </LocaleProvider>,
        )
      })
      await act(async () => {
        await flushMicrotasks()
      })

      const toggle = Array.from(container.querySelectorAll('button')).find(
        (button) => button.textContent?.trim() === '展开',
      ) as HTMLButtonElement | undefined
      expect(toggle).toBeTruthy()
      if (!toggle) return

      await act(async () => {
        toggle.click()
        await flushMicrotasks()
      })

      expect(container.textContent).toContain('收起')
    } finally {
      if (originalScrollHeight) {
        Object.defineProperty(HTMLDivElement.prototype, 'scrollHeight', originalScrollHeight)
      } else {
        // @ts-expect-error test cleanup
        delete HTMLDivElement.prototype.scrollHeight
      }
      act(() => {
        root.unmount()
      })
      container.remove()
    }
  })
})
