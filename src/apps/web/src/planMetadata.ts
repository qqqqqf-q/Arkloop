const PLAN_FRONT_MATTER_RE = /^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/
const PLAN_NAME_RE = /^name:\s*(.+?)\s*$/m

export function isPlanMarkdownPath(path: string | undefined): boolean {
  if (!path) return false
  return path.replace(/\\/g, '/').split('/').pop()?.toLowerCase().endsWith('.plan.md') ?? false
}

export function extractPlanNameFromMarkdown(content: string | undefined): string | null {
  if (!content) return null
  const frontMatter = PLAN_FRONT_MATTER_RE.exec(content)?.[1]
  if (!frontMatter) return null
  const raw = PLAN_NAME_RE.exec(frontMatter)?.[1]?.trim()
  if (!raw) return null
  const unquoted = raw.replace(/^["']|["']$/g, '').trim()
  return unquoted || null
}

export function planDisplayNameFromArgs(args: Record<string, unknown>): string | null {
  const path = typeof args.file_path === 'string' ? args.file_path : ''
  if (!isPlanMarkdownPath(path)) return null
  const content = typeof args.content === 'string' ? args.content : ''
  return extractPlanNameFromMarkdown(content)
}

export function planDisplayNameFromResult(result: unknown): string | null {
  if (!result || typeof result !== 'object' || Array.isArray(result)) return null
  const record = result as Record<string, unknown>
  const plan = typeof record.plan === 'string' ? record.plan : ''
  return extractPlanNameFromMarkdown(plan)
}
