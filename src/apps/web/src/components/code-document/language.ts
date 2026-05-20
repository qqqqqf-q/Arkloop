const EXT_TO_LANGUAGE: Record<string, string> = {
  bash: 'shell',
  cjs: 'javascript',
  css: 'css',
  cts: 'typescript',
  go: 'go',
  htm: 'html',
  html: 'html',
  js: 'javascript',
  json: 'json',
  jsx: 'javascript',
  md: 'markdown',
  mjs: 'javascript',
  mts: 'typescript',
  py: 'python',
  sh: 'shell',
  ts: 'typescript',
  tsx: 'typescript',
  yaml: 'yaml',
  yml: 'yaml',
  zsh: 'shell',
}

const MIME_TO_LANGUAGE: Record<string, string> = {
  'application/json': 'json',
  'application/xml': 'html',
  'image/svg+xml': 'html',
  'text/css': 'css',
  'text/html': 'html',
  'text/javascript': 'javascript',
  'text/jsx': 'javascript',
  'text/markdown': 'markdown',
  'text/tsx': 'typescript',
  'text/typescript': 'typescript',
  'text/x-python': 'python',
  'text/x-shellscript': 'shell',
  'text/yaml': 'yaml',
}

function filenameExt(filename: string): string {
  const lower = filename.trim().toLowerCase()
  if (lower === 'dockerfile') return 'dockerfile'
  if (!lower.includes('.')) return ''
  return lower.split('.').pop() ?? ''
}

export function codeLanguageFromFilename(filename?: string, mimeType?: string): string {
  const ext = filename ? filenameExt(filename) : ''
  if (ext && EXT_TO_LANGUAGE[ext]) return EXT_TO_LANGUAGE[ext]
  const normalizedMime = (mimeType ?? '').split(';', 1)[0].trim().toLowerCase()
  if (normalizedMime && MIME_TO_LANGUAGE[normalizedMime]) return MIME_TO_LANGUAGE[normalizedMime]
  return 'plaintext'
}
