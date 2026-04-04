import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { MarkdownRenderer } from '../components/MarkdownRenderer'
import { LocaleProvider } from '../contexts/LocaleContext'
import type { WebSource } from '../storage'

function renderMarkdown(content: string, options?: { webSources?: WebSource[]; disableMath?: boolean; streaming?: boolean; accessToken?: string; runId?: string }): string {
  return renderToStaticMarkup(
    <LocaleProvider>
      <MarkdownRenderer
        content={content}
        webSources={options?.webSources}
        disableMath={options?.disableMath}
        streaming={options?.streaming}
        accessToken={options?.accessToken}
        runId={options?.runId}
      />
    </LocaleProvider>,
  )
}

describe('MarkdownRenderer', () => {
  it('应解析大小写混合的 Web: 引用并关联到来源', () => {
    const html = renderMarkdown('参考 Web:1。', {
      webSources: [{ title: 'Example', url: 'https://example.com' }],
    })

    expect(html).toContain('example')
    expect(html).not.toContain('Web:1')
  })

  it('应把连续来源引用聚合为同一个 badge', () => {
    const html = renderMarkdown('来源 web:1, Web:2。', {
      webSources: [
        { title: 'Example', url: 'https://example.com' },
        { title: 'GitHub', url: 'https://github.com' },
      ],
    })

    expect(html).toContain('+1')
  })

  it('不应替换代码片段中的 web: 引用文本', () => {
    const html = renderMarkdown('命令 `web:1` 保持原样。', {
      webSources: [{ title: 'Example', url: 'https://example.com' }],
    })

    expect(html).toContain('<code')
    expect(html).toContain('web:1')
  })

  it('应显示代码块语言标签', () => {
    const pythonHtml = renderMarkdown('```python\nprint("ok")\n```')
    const bashHtml = renderMarkdown('```bash\necho ok\n```')
    const latexCodeHtml = renderMarkdown('```latex\n\\\\frac{a}{b}\n```')
    const textHtml = renderMarkdown('```\nplain text\n```')

    expect(pythonHtml).toContain('>python<')
    expect(bashHtml).toContain('>bash<')
    expect(latexCodeHtml).toContain('>latex<')
    expect(textHtml).toContain('>text<')
  })

  it('应在数学模式开启时渲染 KaTeX，关闭时保持原文', () => {
    const mathEnabled = renderMarkdown('行内公式 $a^2+b^2$')
    const mathDisabled = renderMarkdown('行内公式 $a^2+b^2$', { disableMath: true })
    const rawLatex = renderMarkdown('\\alpha + \\beta')

    expect(mathEnabled).toContain('class="katex"')
    expect(mathDisabled).not.toContain('class="katex"')
    expect(rawLatex).not.toContain('class="katex"')
  })

  it('应将 \\[...\\] 定界符转换为 $$ 并渲染为块级公式', () => {
    const html = renderMarkdown('距离公式\n\\[d=\\sqrt{a^2+b^2}\\]')
    expect(html).toContain('class="katex-display"')
  })

  it('应将 \\(...\\) 定界符转换为 $ 并渲染为行内公式', () => {
    const html = renderMarkdown('行内 \\(a^2+b^2\\) 结束')
    expect(html).toContain('class="katex"')
  })

  it('不应转换代码块内的 LaTeX 定界符', () => {
    const fenced = renderMarkdown('```\n\\[a^2\\]\n```')
    expect(fenced).not.toContain('class="katex"')

    const inline = renderMarkdown('代码 `\\(x\\)` 保持原样')
    expect(inline).not.toContain('class="katex"')
    expect(inline).toContain('\\(x\\)')
  })

  it('disableMath 时不应转换 \\[...\\] 定界符', () => {
    const html = renderMarkdown('\\[a^2\\]', { disableMath: true })
    expect(html).not.toContain('class="katex"')
  })

  it('artifact 查不到真实 key 时不应再猜测伪文件', () => {
    const html = renderMarkdown('![图表](artifact:missing.png)', { accessToken: 'token' })

    expect(html).not.toContain('missing.png')
    expect(html).not.toContain('artifact:missing.png')
  })

  it('应识别 workspace 图片引用并渲染按需预览占位', () => {
    const html = renderMarkdown('![图表](workspace:/charts/study.png)', { accessToken: 'token', runId: 'run-1' })

    expect(html).toContain('data-workspace-kind="loading"')
    expect(html).toContain('data-workspace-preview="image"')
    expect(html).toContain('study.png')
  })

  it('应识别 workspace 文本引用并渲染按需预览占位', () => {
    const html = renderMarkdown('[代码](workspace:/notes/example.py)', { accessToken: 'token', runId: 'run-1' })

    expect(html).toContain('data-workspace-kind="loading"')
    expect(html).toContain('data-workspace-preview="text"')
    expect(html).toContain('example.py')
  })

  it('流式纯文本时仍保持 markdown 容器结构稳定', () => {
    const html = renderMarkdown('plain text only\nnext line', { streaming: true })

    expect(html).toContain('plain text only')
    expect(html).toContain('<p ')
  })

  it('流式 markdown 语法时仍应保留 markdown 解析', () => {
    const html = renderMarkdown('**bold** text', { streaming: true })

    expect(html).toContain('<strong')
  })

  it('流式数学公式时应继续渲染 KaTeX', () => {
    const html = renderMarkdown('行内公式 $a^2+b^2$', { streaming: true })

    expect(html).toContain('class="katex"')
  })

  it('流式数学公式时应降频提交渲染内容', async () => {
    vi.useFakeTimers()
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <MarkdownRenderer content="公式 $a$" streaming />
        </LocaleProvider>,
      )
    })

    await act(async () => {
      root.render(
        <LocaleProvider>
          <MarkdownRenderer content="公式 $a+b$" streaming />
        </LocaleProvider>,
      )
    })

    expect(container.textContent).toContain('a')
    expect(container.textContent).not.toContain('a+b')

    await act(async () => {
      vi.advanceTimersByTime(96)
    })

    expect(container.textContent).toContain('a+b')

    act(() => root.unmount())
    container.remove()
    vi.useRealTimers()
  })
})
