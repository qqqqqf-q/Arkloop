/**
 * COP thinking 单行预览：从 markdown 抽纯文本供 typewriter，与 Card 内正文同源不同形。
 */
export function markdownToSingleLinePreview(md: string): string {
  let s = md.replace(/\r\n/g, '\n')
  s = s.replace(/```[\s\S]*?```/g, ' ')
  s = s.replace(/`([^`]+)`/g, '$1')
  s = s.replace(/^#{1,6}\s+/gm, '')
  s = s.replace(/^\s*[-*+]\s+/gm, ' ')
  s = s.replace(/^\s*\d+\.\s+/gm, ' ')
  s = s.replace(/\[([^\]]*)]\([^)]*\)/g, '$1')
  s = s.replace(/[*_~]{1,3}([^*_~\n]+)[*_~]{1,3}/g, '$1')
  s = s.replace(/[*_#>[\]()|`]/g, ' ')
  s = s.replace(/\s+/g, ' ').trim()
  return s
}
