import type { Edge, Node } from '@xyflow/react'
import type { ConversationGraphResponse, MessageResponse } from '../../api'
import { layoutConversationGraph } from './layoutConversationGraph'

export type ConversationGraphNodeData = {
  role: string
  title: string
  body: string
  threadId: string
  messageId: string
  active: boolean
  selected: boolean
  branchCount: number
}

export type ConversationGraphFlowNode = Node<ConversationGraphNodeData, 'conversation-message'>
export type ConversationGraphFlowEdge = Edge

function messageText(message: MessageResponse): string {
  const parts = message.content_json?.parts
  if (Array.isArray(parts)) {
    const text = parts
      .map((part) => part.type === 'text' && 'text' in part ? part.text : '')
      .join('')
      .trim()
    if (text) return text
  }
  return message.content.trim()
}

function summarizeMessage(message: MessageResponse): string {
  const text = messageText(message).replace(/\s+/g, ' ')
  if (!text) return message.role === 'assistant' ? 'Assistant' : 'User'
  return text.length > 118 ? `${text.slice(0, 118).trim()}...` : text
}

export function buildConversationGraphFlow(
  graph: ConversationGraphResponse,
  selectedMessageId: string | null,
  labels: { user: string; assistant: string; system: string },
): { nodes: ConversationGraphFlowNode[]; edges: ConversationGraphFlowEdge[] } {
  const selectedGraphNodeId = selectedMessageId
    ? graph.messages.find((item) => item.instances.some((instance) => instance.message_id === selectedMessageId))?.graph_node_id ?? null
    : null

  const activeThreadMessages = new Set(
    graph.messages
      .filter((item) => item.instances.some((instance) => instance.thread_id === graph.active_thread_id))
      .map((item) => item.graph_node_id),
  )

  const branchCountByNode = new Map<string, number>()
  for (const item of graph.messages) {
    const parent = item.parent_graph_node_id
    if (!parent) continue
    branchCountByNode.set(parent, (branchCountByNode.get(parent) ?? 0) + 1)
  }
  const nodes: ConversationGraphFlowNode[] = graph.messages.map((item) => {
    const instance = item.instances.find((candidate) => candidate.thread_id === graph.active_thread_id) ?? item.instances[0]
    const message = item.message
    return {
      id: item.graph_node_id,
      type: 'conversation-message',
      position: { x: 0, y: 0 },
      data: {
        role: message.role,
        title: message.role === 'user' ? labels.user : message.role === 'assistant' ? labels.assistant : labels.system,
        body: summarizeMessage(message),
        threadId: instance?.thread_id ?? message.thread_id,
        messageId: instance?.message_id ?? message.id,
        active: activeThreadMessages.has(item.graph_node_id),
        selected: selectedGraphNodeId === item.graph_node_id,
        branchCount: Math.max(0, (branchCountByNode.get(item.graph_node_id) ?? 0) - 1),
      },
    }
  })

  const edges: ConversationGraphFlowEdge[] = graph.edges.map((edge) => ({
    id: edge.id,
    source: edge.source,
    target: edge.target,
    type: 'straight',
    animated: false,
    className: activeThreadMessages.has(edge.source) && activeThreadMessages.has(edge.target)
      ? 'conversation-graph-edge conversation-graph-edge--active'
      : 'conversation-graph-edge',
  }))

  return {
    nodes: layoutConversationGraph(nodes, edges),
    edges,
  }
}
