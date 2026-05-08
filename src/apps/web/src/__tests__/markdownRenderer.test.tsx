import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { MarkdownRenderer } from '../components/MarkdownRenderer'
import { LocaleProvider } from '../contexts/LocaleContext'
import type { WebSource } from '../storage'

function renderMarkdown(content: string, options?: {
  webSources?: WebSource[]
  disableMath?: boolean
  streaming?: boolean
  accessToken?: string
  runId?: string
  compact?: boolean
  typography?: 'default' | 'work'
}): string {
  return renderToStaticMarkup(
    <LocaleProvider>
      <MarkdownRenderer
        content={content}
        webSources={options?.webSources}
        disableMath={options?.disableMath}
        streaming={options?.streaming}
        accessToken={options?.accessToken}
        runId={options?.runId}
        compact={options?.compact}
        typography={options?.typography}
      />
    </LocaleProvider>,
  )
}

describe('MarkdownRenderer', () => {
  it('默认字号应保持 Chat 旧行为', () => {
    const html = renderMarkdown('# 一级标题\n\n正文\n\n- 列表项')

    expect(html).toMatch(/<h1 style="[^"]*font-size:16\.5px/)
    expect(html).toMatch(/<p style="[^"]*font-size:16\.5px/)
    expect(html).toMatch(/<ul style="[^"]*font-size:16\.5px/)
  })

  it('Work 字号应只调整正文，不改变 Markdown 一级标题', () => {
    const html = renderMarkdown('# 一级标题\n\n正文\n\n- 列表项', { typography: 'work' })

    expect(html).toMatch(/<h1 style="[^"]*font-size:16\.5px/)
    expect(html).toMatch(/<p style="[^"]*font-size:16px/)
    expect(html).toMatch(/<ul style="[^"]*font-size:16px/)
  })

  it('应解析大小写混合的 Web: 引用并关联到来源', () => {
    const html = renderMarkdown('参考 Web:1。', {
      webSources: [{ title: 'Example', url: 'https://example.com' }],
    })

    expect(html).toContain('example')
    expect(html).not.toContain('Web:1')
  })

  it('HTTP 链接点击应走外部浏览器并阻止默认导航', async () => {
    const originalArkloop = window.arkloop
    const openExternal = vi.fn().mockResolvedValue(undefined)
    window.arkloop = { app: { openExternal } }
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <MarkdownRenderer content="[Example](https://example.com/page)" />
        </LocaleProvider>,
      )
    })

    const link = container.querySelector('a[href="https://example.com/page"]')
    expect(link).not.toBeNull()

    const event = new MouseEvent('click', { bubbles: true, cancelable: true })
    const dispatched = link!.dispatchEvent(event)

    expect(openExternal).toHaveBeenCalledWith('https://example.com/page')
    expect(event.defaultPrevented).toBe(true)
    expect(dispatched).toBe(false)

    act(() => root.unmount())
    container.remove()
    window.arkloop = originalArkloop
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

  it('引用 hover 卡片应挂到 body', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    try {
      await act(async () => {
        root.render(
          <LocaleProvider>
            <MarkdownRenderer
              content="参考 web:1。"
              webSources={[{
                title: 'Example source',
                url: 'https://example.com/article',
                snippet: 'Source snippet',
              }]}
            />
          </LocaleProvider>,
        )
      })

      const badge = container.querySelector('button') as HTMLButtonElement | null
      expect(badge).toBeTruthy()
      if (!badge) return

      await act(async () => {
        badge.dispatchEvent(new MouseEvent('mouseover', { bubbles: true, relatedTarget: null }))
      })

      const popoverLink = document.body.querySelector('a[href="https://example.com/article"]')
      expect(popoverLink).not.toBeNull()
      expect(container.querySelector('a[href="https://example.com/article"]')).toBeNull()
    } finally {
      act(() => root.unmount())
      container.remove()
    }
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

  it('代码块复制按钮应常驻右上角', () => {
    const html = renderMarkdown('```bash\necho ok\n```')

    expect(html).toContain('data-md-code-copy="true"')
    expect(html).toMatch(/data-md-code-copy="true" style="[^"]*position:absolute;top:8px;right:8px/)
    expect(html).not.toContain('opacity-0')
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

    expect(html).toContain('stream-char')
    expect(html).toContain('<p ')
  })

  it('流式 markdown 语法时仍应保留 markdown 解析', () => {
    const html = renderMarkdown('**bold** text', { streaming: true })

    expect(html).toContain('<strong')
  })

  it('流式 markdown 应启用字符级渐显渲染', () => {
    const html = renderMarkdown('第一段\n\n第二段', { streaming: true })

    expect(html).toContain('md-stream-content')
    expect(html).toContain('stream-char')
    expect(html).toContain('>第</span>')
    expect(html).toContain('>一</span>')
    expect(html).toContain('>段</span>')
  })

  it('流式代码块中有空行时不应拆坏 fenced code', () => {
    const html = renderMarkdown('```python\nprint("a")\n\nprint("b")\n```', { streaming: true })

    expect(html).toContain('>python<')
    expect(html).toContain('print(&quot;a&quot;)')
    expect(html).toContain('print(&quot;b&quot;)')
    expect(html).not.toContain('stream-char')
  })

  it('应修复被压成单段的 GFM 表格行', () => {
    const html = renderMarkdown('| 产品 | 定位 | 核心优势 | 局限 ||------|-----------|--------| || ChatGPT | 消费级旗舰 | 生态完整 | 定制受限 |')

    expect(html).toContain('<table')
    expect(html).toContain('<td>ChatGPT</td>')
    expect(html).not.toContain('||------')
  })

  it('应补全缺失的表头分隔列', () => {
    const html = renderMarkdown('| 产品 | 定位 | 核心优势 | 局限 |\n|------|-----------|--------| |\n| ChatGPT | 消费级旗舰 | 生态完整 | 定制受限 |')

    expect(html).toContain('<table')
    expect(html).toContain('<th>局限</th>')
    expect(html).toContain('<td>定制受限</td>')
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

  it('应修复被压成单行的 pipe 表格', () => {
    const html = renderMarkdown([
      '竞品对比',
      '',
      '| 产品 | 定位 | 核心优势 | 局限 ||------|-----------|------------|------|| ChatGPT | 消费级旗舰 | GPT Store | 定制化受限 || Claude | 高信任 AI | 长上下文 | 价格偏高 |',
    ].join('\n'))

    expect(html).toContain('<table')
    expect(html).toContain('<td>ChatGPT</td>')
    expect(html).toContain('<td>Claude</td>')
  })

  it('不应修改格式正常的表格分隔行', () => {
    const input = [
      '| 名称 | 类型 | 说明 |',
      '|---------|----------|---------|',
      '| foo | string | 测试 |',
    ].join('\n')

    // 正常表格应正确渲染
    const html = renderMarkdown(input)
    expect(html).toContain('<table')
    expect(html).toContain('<td>foo</td>')

    // 原始内容中的长分隔符不应被压缩为 ---
    // 通过检查渲染结果包含正常的表格结构来验证（无 || 崩坏）
    expect(html).not.toContain('||')
    expect(html).not.toContain('---------|')
  })
})
