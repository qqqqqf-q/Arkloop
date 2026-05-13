const canonicalToolNameAliases: Record<string, string> = {
  'read.minimax': 'read',
  'web_fetch.basic': 'web_fetch',
  'web_fetch.firecrawl': 'web_fetch',
  'web_fetch.jina': 'web_fetch',
  'web_search.basic': 'web_search',
  'web_search.exa': 'web_search',
  'web_search.searxng': 'web_search',
  'web_search.tavily': 'web_search',
}

export function canonicalToolName(raw: string): string {
  const cleaned = raw.trim()
  if (!cleaned) return ''
  return canonicalToolNameAliases[cleaned.toLowerCase()] ?? cleaned
}

export function pickLogicalToolName(
  data: unknown,
  fallbackToolName?: string,
): string {
  if (data && typeof data === 'object') {
    const typed = data as {
      toolName?: unknown
      tool_name?: unknown
      resolved_tool_name?: unknown
    }
    if (typeof typed.toolName === 'string' && typed.toolName.trim() !== '') {
      return canonicalToolName(typed.toolName)
    }
    if (typeof typed.tool_name === 'string' && typed.tool_name.trim() !== '') {
      return canonicalToolName(typed.tool_name)
    }
    if (typeof typed.resolved_tool_name === 'string' && typed.resolved_tool_name.trim() !== '') {
      return canonicalToolName(typed.resolved_tool_name)
    }
  }
  return canonicalToolName(fallbackToolName ?? '')
}
