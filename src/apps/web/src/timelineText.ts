import type { Locale } from './locales'

export type TimelineText =
  | { kind: 'content'; text: string }
  | { kind: 'with_ellipsis'; value: TimelineText }
  | { kind: 'join'; parts: TimelineText[]; separator: string }
  | { kind: 'completed' }
  | { kind: 'working' }
  | { kind: 'running' }
  | { kind: 'editing' }
  | { kind: 'steps_completed'; count: number }
  | { kind: 'edit_completed' }
  | { kind: 'fetch_completed'; count?: number }
  | { kind: 'exploring_code' }
  | { kind: 'explored_code' }
  | { kind: 'searching_code' }
  | { kind: 'searched_code' }
  | { kind: 'listing_files' }
  | { kind: 'listed_files' }
  | { kind: 'reading_file' }
  | { kind: 'read_file' }
  | { kind: 'writing_file' }
  | { kind: 'wrote_file' }
  | { kind: 'editing_file' }
  | { kind: 'edited_file' }
  | { kind: 'running_command' }
  | { kind: 'run_command' }
  | { kind: 'agent_running' }
  | { kind: 'agent_completed'; count?: number }
  | { kind: 'plan_mode'; action: 'enter' | 'exit' }
  | { kind: 'image_generation'; status: 'live' | 'success' | 'failed'; count?: number }
  | { kind: 'updated_todos' }
  | { kind: 'read_todos' }
  | { kind: 'reviewing_sources' }
  | { kind: 'sources_checked' }
  | { kind: 'search'; tense: 'live' | 'done'; query?: string; extraCount?: number }
  | { kind: 'search_completed'; count?: number }
  | { kind: 'fetching'; target?: string }
  | { kind: 'loaded_resources'; tense: 'live' | 'done'; tools: number; skills: number }
  | { kind: 'read_files'; count: number }
  | { kind: 'listed_file_count'; count: number }
  | { kind: 'search_count'; count: number }
  | { kind: 'ran_commands'; count: number }
  | { kind: 'agent_tasks'; count: number }
  | { kind: 'fetch_count'; count: number }
  | { kind: 'wrote_path'; path: string }
  | { kind: 'edited_path'; path: string }
  | { kind: 'wrote_files'; count: number }
  | { kind: 'edited_files'; count: number }
  | {
      kind: 'tool_subject'
      action: 'read' | 'searched' | 'listed' | 'wrote' | 'edited' | 'reading' | 'searching' | 'listing' | 'writing' | 'editing'
      subject: string
    }

type CoreTimelineText = Exclude<TimelineText, { kind: 'content' } | { kind: 'with_ellipsis' } | { kind: 'join' }>

export function contentText(text: string): TimelineText {
  return { kind: 'content', text }
}

export function isTimelineText(value: unknown): value is TimelineText {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false
  const item = value as Record<string, unknown>
  switch (item.kind) {
    case 'content': return typeof item.text === 'string'
    case 'with_ellipsis': return isTimelineText(item.value)
    case 'join': return Array.isArray(item.parts) && item.parts.every(isTimelineText) && typeof item.separator === 'string'
    case 'completed':
    case 'working':
    case 'running':
    case 'editing':
    case 'edit_completed':
    case 'exploring_code':
    case 'explored_code':
    case 'searching_code':
    case 'searched_code':
    case 'listing_files':
    case 'listed_files':
    case 'reading_file':
    case 'read_file':
    case 'writing_file':
    case 'wrote_file':
    case 'editing_file':
    case 'edited_file':
    case 'running_command':
    case 'run_command':
    case 'updated_todos':
    case 'read_todos':
    case 'reviewing_sources':
    case 'sources_checked':
      return true
    case 'steps_completed':
    case 'read_files':
    case 'listed_file_count':
    case 'search_count':
    case 'ran_commands':
    case 'agent_tasks':
    case 'fetch_count':
    case 'wrote_files':
    case 'edited_files':
      return isFiniteNumber(item.count)
    case 'fetch_completed':
    case 'agent_completed':
    case 'search_completed':
      return item.count == null || isFiniteNumber(item.count)
    case 'plan_mode': return item.action === 'enter' || item.action === 'exit'
    case 'image_generation':
      return (item.status === 'live' || item.status === 'success' || item.status === 'failed') && (item.count == null || isFiniteNumber(item.count))
    case 'search':
      return (item.tense === 'live' || item.tense === 'done')
        && (item.query == null || typeof item.query === 'string')
        && (item.extraCount == null || isFiniteNumber(item.extraCount))
    case 'fetching': return item.target == null || typeof item.target === 'string'
    case 'loaded_resources':
      return (item.tense === 'live' || item.tense === 'done') && isFiniteNumber(item.tools) && isFiniteNumber(item.skills)
    case 'wrote_path':
    case 'edited_path':
      return typeof item.path === 'string'
    case 'tool_subject':
      return isToolSubjectAction(item.action) && typeof item.subject === 'string'
    default:
      return false
  }
}

export function renderTimelineText(value: TimelineText, locale: Locale): string {
  if (value.kind === 'content') return value.text
  if (value.kind === 'with_ellipsis') return `${renderTimelineText(value.value, locale)}...`
  if (value.kind === 'join') return value.parts.map((part) => renderTimelineText(part, locale)).join(value.separator)
  return locale === 'zh' ? renderZh(value) : renderEn(value)
}

export function renderTimelineTextWithEllipsis(value: TimelineText, locale: Locale): string {
  return `${renderTimelineText(value, locale)}...`
}

function plural(count: number, singular: string, pluralForm = `${singular}s`): string {
  return `${count} ${count === 1 ? singular : pluralForm}`
}

function zhCount(count: number, unit: string): string {
  return `${count} ${unit}`
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}

function isToolSubjectAction(value: unknown): value is Extract<TimelineText, { kind: 'tool_subject' }>['action'] {
  return value === 'read'
    || value === 'searched'
    || value === 'listed'
    || value === 'wrote'
    || value === 'edited'
    || value === 'reading'
    || value === 'searching'
    || value === 'listing'
    || value === 'writing'
    || value === 'editing'
}

function renderEn(value: CoreTimelineText): string {
  switch (value.kind) {
    case 'completed': return 'Completed'
    case 'working': return 'Working'
    case 'running': return 'Running'
    case 'editing': return 'Editing'
    case 'steps_completed': return `${plural(value.count, 'step')} completed`
    case 'edit_completed': return 'Edit completed'
    case 'fetch_completed': return (value.count ?? 1) === 1 ? 'Fetch completed' : `${value.count} fetches completed`
    case 'exploring_code': return 'Exploring code'
    case 'explored_code': return 'Explored code'
    case 'searching_code': return 'Searching code'
    case 'searched_code': return 'Searched code'
    case 'listing_files': return 'Listing files'
    case 'listed_files': return 'Listed files'
    case 'reading_file': return 'Reading file'
    case 'read_file': return 'Read file'
    case 'writing_file': return 'Writing file'
    case 'wrote_file': return 'Wrote file'
    case 'editing_file': return 'Editing file'
    case 'edited_file': return 'Edited file'
    case 'running_command': return 'Running command'
    case 'run_command': return 'Run command'
    case 'agent_running': return 'Agent running'
    case 'agent_completed': return value.count && value.count > 1 ? `${value.count} agent tasks completed` : 'Agent completed'
    case 'plan_mode': return value.action === 'enter' ? 'Enter Plan Mode' : 'Exit Plan Mode'
    case 'image_generation': {
      if (value.status === 'live') return 'Generating image'
      if (value.status === 'failed') return value.count && value.count > 1 ? `${value.count} image generations failed` : 'Image generation failed'
      return value.count && value.count > 1 ? `Generated ${value.count} images` : 'Generated image'
    }
    case 'updated_todos': return 'Updated todos'
    case 'read_todos': return 'Read todos'
    case 'reviewing_sources': return 'Reviewing sources'
    case 'sources_checked': return 'Sources checked'
    case 'search': {
      if (!value.query) return value.tense === 'live' ? 'Searching' : 'Search completed'
      const extra = value.extraCount && value.extraCount > 0 ? ` +${value.extraCount}` : ''
      return `${value.tense === 'live' ? 'Searching for' : 'Searched for'} ${value.query}${extra}`
    }
    case 'search_completed': return (value.count ?? 1) === 1 ? 'Search completed' : `${value.count} searches completed`
    case 'fetching': return value.target ? `Fetching ${value.target}` : 'Fetching'
    case 'loaded_resources': {
      const verb = value.tense === 'live' ? 'Loading' : 'Loaded'
      const parts: string[] = []
      if (value.tools > 0) parts.push(plural(value.tools, 'tool'))
      if (value.skills > 0) parts.push(plural(value.skills, 'skill'))
      return parts.length > 0 ? `${verb} ${parts.join(', ')}` : `${verb} 0 tools`
    }
    case 'read_files': return `Read ${plural(value.count, 'file')}`
    case 'listed_file_count': return `Listed ${plural(value.count, 'file')}`
    case 'search_count': return plural(value.count, 'search', 'searches')
    case 'ran_commands': return `Ran ${plural(value.count, 'command')}`
    case 'agent_tasks': return plural(value.count, 'agent task', 'agent tasks')
    case 'fetch_count': return plural(value.count, 'fetch', 'fetches')
    case 'wrote_path': return `Wrote ${value.path}`
    case 'edited_path': return `Edited ${value.path}`
    case 'wrote_files': return `Wrote ${plural(value.count, 'file')}`
    case 'edited_files': return `Edited ${plural(value.count, 'file')}`
    case 'tool_subject': {
      const verbs = {
        read: 'Read',
        searched: 'Searched',
        listed: 'Listed',
        wrote: 'Wrote',
        edited: 'Edited',
        reading: 'Reading',
        searching: 'Searching',
        listing: 'Listing',
        writing: 'Writing',
        editing: 'Editing',
      } satisfies Record<typeof value.action, string>
      return `${verbs[value.action]} ${value.subject}`
    }
  }
}

function renderZh(value: CoreTimelineText): string {
  switch (value.kind) {
    case 'completed': return '已完成'
    case 'working': return '处理中'
    case 'running': return '运行中'
    case 'editing': return '编辑中'
    case 'steps_completed': return `完成 ${value.count} 步`
    case 'edit_completed': return '编辑完成'
    case 'fetch_completed': return (value.count ?? 1) === 1 ? '获取完成' : `${value.count} 次获取完成`
    case 'exploring_code': return '正在查看代码'
    case 'explored_code': return '查看代码'
    case 'searching_code': return '正在搜索代码'
    case 'searched_code': return '搜索代码'
    case 'listing_files': return '正在列出文件'
    case 'listed_files': return '列出文件'
    case 'reading_file': return '正在读取文件'
    case 'read_file': return '读取文件'
    case 'writing_file': return '正在写入文件'
    case 'wrote_file': return '写入文件'
    case 'editing_file': return '正在编辑文件'
    case 'edited_file': return '编辑文件'
    case 'running_command': return '正在运行命令'
    case 'run_command': return '运行命令'
    case 'agent_running': return '子代理运行中'
    case 'agent_completed': return value.count && value.count > 1 ? `${value.count} 个子代理任务完成` : '子代理完成'
    case 'plan_mode': return value.action === 'enter' ? '进入计划模式' : '退出计划模式'
    case 'image_generation': {
      if (value.status === 'live') return '正在生成图片'
      if (value.status === 'failed') return value.count && value.count > 1 ? `${value.count} 次图片生成失败` : '图片生成失败'
      return value.count && value.count > 1 ? `生成 ${value.count} 张图片` : '生成图片'
    }
    case 'updated_todos': return '更新待办'
    case 'read_todos': return '读取待办'
    case 'reviewing_sources': return '正在检查来源'
    case 'sources_checked': return '已检查来源'
    case 'search': {
      if (!value.query) return value.tense === 'live' ? '搜索中' : '搜索完成'
      const extra = value.extraCount && value.extraCount > 0 ? ` +${value.extraCount}` : ''
      return `${value.tense === 'live' ? '正在搜索' : '搜索'} ${value.query}${extra}`
    }
    case 'search_completed': return (value.count ?? 1) === 1 ? '搜索完成' : `${value.count} 次搜索完成`
    case 'fetching': return value.target ? `正在获取 ${value.target}` : '正在获取'
    case 'loaded_resources': {
      const verb = value.tense === 'live' ? '正在加载' : '加载'
      const parts: string[] = []
      if (value.tools > 0) parts.push(zhCount(value.tools, '个工具'))
      if (value.skills > 0) parts.push(zhCount(value.skills, '个技能'))
      return parts.length > 0 ? `${verb} ${parts.join(', ')}` : `${verb} 0 个工具`
    }
    case 'read_files': return `读取 ${zhCount(value.count, '个文件')}`
    case 'listed_file_count': return `列出 ${zhCount(value.count, '个文件')}`
    case 'search_count': return `${value.count} 次搜索`
    case 'ran_commands': return `运行 ${zhCount(value.count, '条命令')}`
    case 'agent_tasks': return `${value.count} 个子代理任务`
    case 'fetch_count': return `${value.count} 次获取`
    case 'wrote_path': return `写入 ${value.path}`
    case 'edited_path': return `编辑 ${value.path}`
    case 'wrote_files': return `写入 ${zhCount(value.count, '个文件')}`
    case 'edited_files': return `编辑 ${zhCount(value.count, '个文件')}`
    case 'tool_subject': {
      const verbs = {
        read: '读取',
        searched: '搜索',
        listed: '列出',
        wrote: '写入',
        edited: '编辑',
        reading: '正在读取',
        searching: '正在搜索',
        listing: '正在列出',
        writing: '正在写入',
        editing: '正在编辑',
      } satisfies Record<typeof value.action, string>
      return `${verbs[value.action]} ${value.subject}`
    }
  }
}
