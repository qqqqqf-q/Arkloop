import type { Locale } from '../../locales'
import type { TitleSpan } from '../../copSubSegment'
import { contentText, renderTimelineText } from '../../timelineText'

export function localizeTimelineTitleSpan(span: TitleSpan, locale: Locale): TitleSpan {
  if ('diffKind' in span) return span
  return { text: contentText(renderTimelineText(span.text, locale)) }
}

// Legacy boundary for already-materialized historical strings. New timeline paths should use TimelineText.
export function localizeTimelineLabel(label: string, locale: Locale): string {
  if (locale !== 'zh') return label
  const text = label.trim()
  const trailingDots = text.endsWith('...')
  const core = trailingDots ? text.slice(0, -3) : text

  const exact: Record<string, string> = {
    Completed: '已完成',
    Done: '完成',
    Working: '处理中',
    Running: '运行中',
    Explore: '查看',
    Command: '命令',
    Edit: '编辑',
    Agent: '子代理',
    Search: '搜索',
    Image: '图片',
    Plan: '计划',
    Todos: '待办',
    Document: '文档',
    Artifact: '工件',
    Browser: '浏览器',
    Review: '检查',
    Editing: '编辑中',
    'Edit completed': '编辑完成',
    'Exploring code': '正在查看代码',
    'Explored code': '查看代码',
    'Searching code': '正在搜索代码',
    'Searched code': '搜索代码',
    'Listing files': '正在列出文件',
    'Listed files': '列出文件',
    'Reading file': '正在读取文件',
    'Read file': '读取文件',
    'Writing file': '正在写入文件',
    'Wrote file': '写入文件',
    'Editing file': '正在编辑文件',
    'Edited file': '编辑文件',
    'Running command': '正在运行命令',
    'Run command': '运行命令',
    'Loaded tools': '加载工具',
    'Loading tools': '正在加载工具',
    'Loaded skill': '加载技能',
    'Loading skill': '正在加载技能',
    'Agent running': '子代理运行中',
    'Agent completed': '子代理完成',
    'Fetch completed': '获取完成',
    'Fetching': '正在获取',
    'Search completed': '搜索完成',
    'Searching': '搜索中',
    'Reviewing sources': '正在检查来源',
    'Sources checked': '已检查来源',
    'Enter Plan Mode': '进入计划模式',
    'Exit Plan Mode': '退出计划模式',
    enter_plan_mode: '进入计划模式',
    exit_plan_mode: '退出计划模式',
    'Generating image': '正在生成图片',
    'Generated image': '生成图片',
    'Image generation failed': '图片生成失败',
    'Updated todos': '更新待办',
    'Read todos': '读取待办',
  }
  if (exact[core]) return trailingDots ? `${exact[core]}...` : exact[core]

  return label
}
