import { describe, expect, it } from 'vitest'
import type { CopBlockItem } from '../assistantTurnSegments'
import { aggregateMainTitle, buildSubSegments, categoryForTool, presentToProgressive } from '../copSubSegment'

function toolCall(
  id: string,
  toolName: string,
  seq: number,
  args: Record<string, unknown> = {},
  result?: unknown,
  errorClass?: string,
): CopBlockItem {
  return {
    kind: 'call',
    call: {
      toolCallId: id,
      toolName,
      arguments: args,
      result,
      errorClass,
    },
    seq,
  }
}

describe('copSubSegment image generation titles', () => {
  it('image_generate live 标题显示图片生成语义', () => {
    const segments = buildSubSegments([
      toolCall('img1', 'image_generate', 1, { prompt: 'a mountain at dawn' }),
    ])
    const openSegment = {
      ...segments[0]!,
      status: 'open' as const,
      title: 'Working...',
    }

    expect(categoryForTool('image_generate')).toBe('image')
    expect(presentToProgressive('image_generate', { prompt: 'a mountain at dawn' })).toBe('Generating image')
    expect(aggregateMainTitle([openSegment], true, false)).toBe('Generating image...')
    expect(aggregateMainTitle([openSegment], true, false)).not.toContain('image_generate')
  })

  it('image_generate 完成和失败标题不退化为 step completed', () => {
    const successSegments = buildSubSegments([
      toolCall('img1', 'image_generate', 1, { prompt: 'a mountain at dawn' }, { image: 'ok' }),
    ])
    const failedSegments = buildSubSegments([
      toolCall('img2', 'image_generate', 1, { prompt: 'a mountain at dawn' }, undefined, 'quota exceeded'),
    ])

    expect(successSegments[0]?.title).toBe('Generated image')
    expect(aggregateMainTitle(successSegments, false, true)).toBe('Generated image')
    expect(aggregateMainTitle(successSegments, false, true)).not.toBe('1 step completed')
    expect(failedSegments[0]?.title).toBe('Image generation failed')
    expect(aggregateMainTitle(failedSegments, false, true)).toBe('Image generation failed')
  })
})

describe('copSubSegment web search titles', () => {
  it('把 web_search 归为搜索分类，而不是 generic', () => {
    expect(categoryForTool('web_search')).toBe('search')
    expect(categoryForTool('web_search.tavily')).toBe('search')
  })

  it('live adaptive title 使用搜索语义和 query，不裸露工具名', () => {
    const segments = buildSubSegments([
      toolCall('ws1', 'web_search', 1, { query: 'rust crate niche' }),
    ])
    const openSegment = {
      ...segments[0]!,
      status: 'open' as const,
      title: 'Searching...',
    }

    expect(presentToProgressive('web_search', { query: 'rust crate niche' })).toBe('Searching for rust crate niche')
    expect(aggregateMainTitle([openSegment], true, false)).toBe('Searching for rust crate niche...')
    expect(aggregateMainTitle([openSegment], true, false)).not.toContain('web_search')
  })

  it('live adaptive title 使用抓取语义和域名，不裸露 web_fetch', () => {
    const segments = buildSubSegments([
      toolCall('wf1', 'web_fetch', 1, { url: 'https://www.example.com/docs/page' }),
    ])
    const openSegment = {
      ...segments[0]!,
      status: 'open' as const,
      title: 'Fetching...',
    }

    expect(categoryForTool('web_fetch')).toBe('fetch')
    expect(presentToProgressive('web_fetch', { url: 'https://www.example.com/docs/page' })).toBe('Fetching example.com')
    expect(aggregateMainTitle([openSegment], true, false)).toBe('Fetching example.com...')
    expect(aggregateMainTitle([openSegment], true, false)).not.toContain('web_fetch')
  })

  it('完成态保留搜索标题，不退化成 step completed', () => {
    const segments = buildSubSegments([
      toolCall('ws1', 'web_search', 1, { query: 'rust crate niche' }),
    ])

    expect(segments[0]?.title).toBe('Searched for rust crate niche')
    expect(aggregateMainTitle(segments, false, true)).toBe('Searched for rust crate niche')
    expect(aggregateMainTitle(segments, false, true)).not.toBe('1 step completed')
  })

  it('多次搜索完成态显示搜索数量，不退化成 steps completed', () => {
    const segments = buildSubSegments([
      toolCall('ws1', 'web_search', 1, { query: 'rust crate niche' }),
      toolCall('ws2', 'web_search', 2, { query: 'python library rewrite' }),
    ])

    expect(aggregateMainTitle(segments, false, true)).toBe('Searched for rust crate niche +1')
    expect(aggregateMainTitle(segments, false, true)).not.toBe('2 steps completed')
  })
})

describe('copSubSegment load tool titles', () => {
  it('load_tools 完成态标题不退化为代码探索', () => {
    const segments = buildSubSegments([
      toolCall('lt1', 'load_tools', 1, { queries: ['todo_write'] }),
    ])

    expect(segments[0]?.title).toBe('Loaded 1 tool')
    expect(aggregateMainTitle(segments, false, true)).toBe('Loaded 1 tool')
    expect(aggregateMainTitle(segments, false, true)).not.toBe('Explored code')
  })

  it('load_tools live 标题使用加载语义', () => {
    const segments = buildSubSegments([
      toolCall('lt1', 'load_tools', 1, { queries: ['spawn_agent', 'wait_agent'] }),
    ])
    const openSegment = {
      ...segments[0]!,
      status: 'open' as const,
      title: 'Exploring code...',
    }

    expect(aggregateMainTitle([openSegment], true, false)).toBe('Loading 2 tools...')
  })

  it('load_skill 完成态标题显示技能加载语义', () => {
    const segments = buildSubSegments([
      toolCall('ls1', 'load_skill', 1),
    ])

    expect(segments[0]?.title).toBe('Loaded 1 skill')
    expect(aggregateMainTitle(segments, false, true)).toBe('Loaded 1 skill')
  })

  it('thought 不影响 load_tools/load_skill 专属标题判断', () => {
    const segments = buildSubSegments([
      { kind: 'thinking', content: 'need tools', seq: 1 },
      toolCall('lt1', 'load_tools', 2, { queries: ['spawn_agent', 'wait_agent'] }),
      toolCall('ls1', 'load_skill', 3),
      { kind: 'thinking', content: 'skills too', seq: 4 },
      toolCall('ls2', 'load_skill', 5),
      toolCall('ls3', 'load_skill', 6),
    ])

    expect(segments[0]?.title).toBe('Loaded 2 tools, 3 skills')
    expect(aggregateMainTitle(segments, false, true)).toBe('Loaded 2 tools, 3 skills')
  })

  it('同一 COP 混入代码探索工具时不显示加载标题', () => {
    const segments = buildSubSegments([
      { kind: 'thinking', content: 'load then read', seq: 1 },
      toolCall('lt1', 'load_tools', 2, { queries: ['read'] }),
      toolCall('r1', 'read_file', 3, { file_path: '/tmp/a.ts' }),
    ])

    expect(segments[0]?.title).toBe('Read 1 file')
    expect(aggregateMainTitle(segments, false, true)).toBe('Read 1 file')
    expect(aggregateMainTitle(segments, false, true)).not.toContain('Loaded')
  })
})
