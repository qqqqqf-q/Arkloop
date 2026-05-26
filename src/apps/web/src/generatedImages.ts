import { canonicalToolName } from '@arkloop/shared'
import type { AgentUIEvent } from './agent-ui'
import { agentEventDataRecord, agentEventToolOutput } from './agent-ui/event-data'
import { IMAGE_GENERATE_TOOL_NAME } from './copSubSegment'
import type { ArtifactRef } from './storage'

export type GeneratedImageItem = {
  artifact: ArtifactRef
  toolCallId?: string
  toolCallIndex: number
  order: number
}

type AppendGeneratedImagesArgs = {
  toolCallId?: string
  toolCallIndex?: number
  artifacts: ArtifactRef[]
}

function isImageArtifact(artifact: ArtifactRef): boolean {
  return artifact.mime_type.startsWith('image/')
}

function extractArtifacts(result: unknown): ArtifactRef[] {
  if (!result || typeof result !== 'object') return []
  const artifacts = (result as { artifacts?: unknown[] }).artifacts
  if (!Array.isArray(artifacts)) return []
  return artifacts
    .filter((item): item is Record<string, unknown> => !!item && typeof item === 'object')
    .filter((item) => typeof item.key === 'string' && typeof item.filename === 'string')
    .map((item) => ({
      key: item.key as string,
      filename: item.filename as string,
      size: typeof item.size === 'number' ? item.size : 0,
      mime_type: typeof item.mime_type === 'string' ? item.mime_type : '',
      title: typeof item.title === 'string' ? item.title : undefined,
      display: item.display === 'panel' ? 'panel' : item.display === 'inline' ? 'inline' : undefined,
    }))
}

export function appendGeneratedImages(
  prev: GeneratedImageItem[],
  args: AppendGeneratedImagesArgs,
): GeneratedImageItem[] {
  const seen = new Set(prev.map((item) => item.artifact.key))
  const additions: GeneratedImageItem[] = []
  let order = 0

  for (const artifact of args.artifacts) {
    if (!isImageArtifact(artifact)) continue
    if (seen.has(artifact.key)) continue
    seen.add(artifact.key)
    additions.push({
      artifact,
      toolCallId: args.toolCallId,
      toolCallIndex: args.toolCallIndex ?? Number.MAX_SAFE_INTEGER,
      order,
    })
    order += 1
  }

  return [...prev, ...additions].sort((left, right) => {
    if (left.toolCallIndex !== right.toolCallIndex) return left.toolCallIndex - right.toolCallIndex
    return left.order - right.order
  })
}

export function buildGeneratedImagesFromAgentEvents(events: AgentUIEvent[]): GeneratedImageItem[] {
  let items: GeneratedImageItem[] = []
  for (const event of events) {
    if (event.type !== 'tool-result') continue
    if (canonicalToolName(event.toolName ?? '') !== IMAGE_GENERATE_TOOL_NAME) continue
    const data = agentEventDataRecord(event.data)
    items = appendGeneratedImages(items, {
      toolCallId: typeof data?.toolCallId === 'string' ? data.toolCallId : undefined,
      toolCallIndex: typeof data?.toolCallIndex === 'number' ? data.toolCallIndex : event.order,
      artifacts: extractArtifacts(agentEventToolOutput(event.data)),
    })
  }
  return items
}

export function generatedImageKeySet(items: GeneratedImageItem[]): Set<string> {
  return new Set(items.map((item) => item.artifact.key))
}
