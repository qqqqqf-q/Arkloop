import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import hljs from 'highlight.js/lib/core'

import { CodeExecutionPanel } from '../components/CodeExecutionPanel'
import type { CodeExecution } from '../components/CodeExecutionCard'

describe('CodeExecutionPanel', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('highlight 失败时也必须对 code 做 HTML 转义，避免注入', () => {
    vi.spyOn(hljs, 'highlight').mockImplementation(() => {
      throw new Error('boom')
    })

    const execution: CodeExecution = {
      id: '1',
      language: 'python',
      code: '<img src=x onerror=alert(1)>',
      status: 'completed',
    }

    const html = renderToStaticMarkup(
      <CodeExecutionPanel execution={execution} onClose={() => {}} />,
    )

    expect(html).not.toMatch(/<img\\b/i)
    expect(html).toContain('&lt;img')
  })

  it('执行完成但无输出时显示语义空状态', () => {
    const execution: CodeExecution = {
      id: '1',
      language: 'python',
      code: 'print() ',
      status: 'success',
      emptyLabel: 'Execution completed with no output',
    }

    const html = renderToStaticMarkup(
      <CodeExecutionPanel execution={execution} onClose={() => {}} />,
    )

    expect(html).toContain('Execution completed with no output')
  })
})
