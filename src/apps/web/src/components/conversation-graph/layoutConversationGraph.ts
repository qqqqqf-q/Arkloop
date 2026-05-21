import dagre from 'dagre'
import type { Edge, Node } from '@xyflow/react'

export function layoutConversationGraph<N extends Node, E extends Edge>(nodes: N[], edges: E[]): N[] {
  const graph = new dagre.graphlib.Graph()
  graph.setDefaultEdgeLabel(() => ({}))
  graph.setGraph({
    rankdir: 'LR',
    nodesep: 34,
    ranksep: 80,
    marginx: 24,
    marginy: 24,
  })

  for (const node of nodes) {
    graph.setNode(node.id, { width: 230, height: 112 })
  }
  for (const edge of edges) {
    graph.setEdge(edge.source, edge.target)
  }
  dagre.layout(graph)

  return nodes.map((node) => {
    const position = graph.node(node.id)
    if (!position) return node
    return {
      ...node,
      position: {
        x: position.x - 115,
        y: position.y - 56,
      },
    }
  })
}
