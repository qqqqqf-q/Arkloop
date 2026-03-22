import { describe, expect, it } from 'vitest'
import { markdownToSingleLinePreview } from '../copThinkingPlainPreview'

describe('markdownToSingleLinePreview', () => {
  it('去 fenced 与行内 code', () => {
    expect(markdownToSingleLinePreview('foo `bar` baz')).toBe('foo bar baz')
    expect(markdownToSingleLinePreview('x ```js\na=1\n``` y')).toBe('x y')
  })

  it('标题与列表记号弱化', () => {
    expect(markdownToSingleLinePreview('# Hi\n\n- a\n- b')).toBe('Hi a b')
  })

  it('链接保留锚文', () => {
    expect(markdownToSingleLinePreview('[go](https://x.com)')).toBe('go')
  })
})
