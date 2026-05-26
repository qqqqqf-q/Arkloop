import { describe, expect, it } from 'vitest'
import type { AgentUIEvent } from '../agent-ui'
import {
  appendGeneratedImages,
  buildGeneratedImagesFromAgentEvents,
  generatedImageKeySet,
  type GeneratedImageItem,
} from '../generatedImages'

function makeToolResultEvent(params: {
  seq: number
  toolCallId: string
  toolCallIndex?: number
  toolName?: string
  artifacts: Array<{
    key: string
    filename: string
    mime_type: string
    size?: number
    title?: string
    display?: 'inline' | 'panel'
  }>
}): AgentUIEvent {
  return {
    id: `evt_${params.seq}`,
    streamId: 'run_1',
    order: params.seq,
    timestamp: '2026-01-01T00:00:00.000Z',
    type: 'tool-result',
    toolName: params.toolName ?? 'image_generate',
    data: {
      toolCallId: params.toolCallId,
      toolCallIndex: params.toolCallIndex ?? params.seq,
      toolName: params.toolName ?? 'image_generate',
      output: { artifacts: params.artifacts },
    },
  }
}

describe('appendGeneratedImages', () => {
  it('同一 tool call 返回多张图片时应完整保留并按返回顺序排列', () => {
    const next = appendGeneratedImages([], {
      toolCallId: 'call_img_1',
      toolCallIndex: 3,
      artifacts: [
        { key: 'img-1', filename: '1.png', size: 1, mime_type: 'image/png' },
        { key: 'img-2', filename: '2.png', size: 1, mime_type: 'image/png' },
      ],
    })

    expect(next.map((item) => item.artifact.key)).toEqual(['img-1', 'img-2'])
    expect(next.map((item) => item.order)).toEqual([0, 1])
  })

  it('同一消息内重复 key 应只保留第一次出现', () => {
    const initial: GeneratedImageItem[] = [{
      artifact: { key: 'img-1', filename: '1.png', size: 1, mime_type: 'image/png' },
      toolCallId: 'call_img_1',
      toolCallIndex: 1,
      order: 0,
    }]

    const next = appendGeneratedImages(initial, {
      toolCallId: 'call_img_2',
      toolCallIndex: 2,
      artifacts: [
        { key: 'img-1', filename: '1-copy.png', size: 1, mime_type: 'image/png' },
        { key: 'img-3', filename: '3.png', size: 1, mime_type: 'image/png' },
      ],
    })

    expect(next.map((item) => item.artifact.key)).toEqual(['img-1', 'img-3'])
  })
})

describe('buildGeneratedImagesFromAgentEvents', () => {
  it('只应从 image_generate 的图片 artifact 重建 generatedImages', () => {
    const events = [
      makeToolResultEvent({
        seq: 1,
        toolCallId: 'call_img_1',
        artifacts: [
          { key: 'img-1', filename: '1.png', size: 1, mime_type: 'image/png' },
          { key: 'doc-1', filename: 'notes.md', size: 1, mime_type: 'text/markdown' },
        ],
      }),
      makeToolResultEvent({
        seq: 2,
        toolCallId: 'call_art_1',
        toolName: 'create_artifact',
        artifacts: [
          { key: 'img-2', filename: 'chart.png', size: 1, mime_type: 'image/png' },
        ],
      }),
      makeToolResultEvent({
        seq: 3,
        toolCallId: 'call_img_1',
        artifacts: [
          { key: 'img-3', filename: '3.png', size: 1, mime_type: 'image/png' },
        ],
      }),
    ]

    const result = buildGeneratedImagesFromAgentEvents(events)

    expect(result.map((item) => item.artifact.key)).toEqual(['img-1', 'img-3'])
    expect(generatedImageKeySet(result)).toEqual(new Set(['img-1', 'img-3']))
  })
})
