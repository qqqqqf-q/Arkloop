import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Background,
  Controls,
  Handle,
  Position,
  ReactFlow,
  type NodeProps,
  type Viewport,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { RefreshCw } from 'lucide-react'
import {
  getConversationGraph,
  type ConversationGraphResponse,
} from '../../api'
import { rightPanelIconSize } from '../rightPanelControls'
import {
  buildConversationGraphFlow,
  type ConversationGraphFlowNode,
  type ConversationGraphNodeData,
} from './buildConversationGraph'
import { useLocale } from '../../contexts/LocaleContext'
import './ConversationGraphPanel.css'

type Props = {
  accessToken: string
  threadId: string
  selectedMessageId: string | null
  refreshKey?: string | number
  onSelectMessage: (threadId: string, messageId: string) => void
}

function ConversationMessageNode({ data }: NodeProps<ConversationGraphFlowNode>) {
  return (
    <div className="conversation-graph-node" data-role={data.role} data-active={data.active} data-selected={data.selected}>
      <Handle type="target" position={Position.Left} />
      <div className="conversation-graph-node__head">
        <span>{data.title}</span>
        {data.branchCount > 0 && <span className="conversation-graph-node__badge">+{data.branchCount}</span>}
      </div>
      <div className="conversation-graph-node__body">{data.body}</div>
      <Handle type="source" position={Position.Right} />
    </div>
  )
}

const nodeTypes = {
  'conversation-message': ConversationMessageNode,
}

const graphNodeWidth = 230
const graphNodeHeight = 112
const overviewWidth = 190
const overviewHeight = 128
const overviewPadding = 14

const graphViewportPrefix = 'arkloop:web:conversation_graph_viewport:'

function graphViewportKey(rootThreadId: string): string {
  return `${graphViewportPrefix}${rootThreadId}`
}

function readGraphViewport(rootThreadId: string): Viewport | null {
  if (typeof window === 'undefined') return null
  try {
    const raw = window.localStorage.getItem(graphViewportKey(rootThreadId))
    if (!raw) return null
    const value = JSON.parse(raw) as Partial<Viewport>
    if (!Number.isFinite(value.x) || !Number.isFinite(value.y) || !Number.isFinite(value.zoom)) return null
    return { x: Number(value.x), y: Number(value.y), zoom: Number(value.zoom) }
  } catch {
    try { window.localStorage.removeItem(graphViewportKey(rootThreadId)) } catch { /* ignore */ }
    return null
  }
}

function writeGraphViewport(rootThreadId: string, viewport: Viewport): void {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(graphViewportKey(rootThreadId), JSON.stringify(viewport))
  } catch { /* ignore */ }
}

function GraphOverview({
  nodes,
  edges,
}: {
  nodes: ConversationGraphFlowNode[]
  edges: ReturnType<typeof buildConversationGraphFlow>['edges']
}) {
  if (nodes.length === 0) return null
  const minX = Math.min(...nodes.map((node) => node.position.x))
  const minY = Math.min(...nodes.map((node) => node.position.y))
  const maxX = Math.max(...nodes.map((node) => node.position.x + graphNodeWidth))
  const maxY = Math.max(...nodes.map((node) => node.position.y + graphNodeHeight))
  const graphWidth = Math.max(1, maxX - minX)
  const graphHeight = Math.max(1, maxY - minY)
  const scale = Math.min(
    (overviewWidth - overviewPadding * 2) / graphWidth,
    (overviewHeight - overviewPadding * 2) / graphHeight,
  )
  const offsetX = (overviewWidth - graphWidth * scale) / 2
  const offsetY = (overviewHeight - graphHeight * scale) / 2
  const point = (x: number, y: number) => ({
    x: offsetX + (x - minX) * scale,
    y: offsetY + (y - minY) * scale,
  })
  const byId = new Map(nodes.map((node) => [node.id, node]))

  return (
    <svg className="conversation-graph-overview" viewBox={`0 0 ${overviewWidth} ${overviewHeight}`} aria-hidden="true">
      {edges.map((edge) => {
        const source = byId.get(edge.source)
        const target = byId.get(edge.target)
        if (!source || !target) return null
        const a = point(source.position.x + graphNodeWidth, source.position.y + graphNodeHeight / 2)
        const b = point(target.position.x, target.position.y + graphNodeHeight / 2)
        return (
          <line
            key={edge.id}
            className={edge.className?.includes('active') ? 'conversation-graph-overview__edge conversation-graph-overview__edge--active' : 'conversation-graph-overview__edge'}
            x1={a.x}
            y1={a.y}
            x2={b.x}
            y2={b.y}
          />
        )
      })}
      {nodes.map((node) => {
        const pos = point(node.position.x, node.position.y)
        return (
          <rect
            key={node.id}
            className="conversation-graph-overview__node"
            data-active={node.data.active}
            data-selected={node.data.selected}
            x={pos.x}
            y={pos.y}
            width={Math.max(4, graphNodeWidth * scale)}
            height={Math.max(3, graphNodeHeight * scale)}
            rx={2}
          />
        )
      })}
    </svg>
  )
}

export function ConversationGraphPanel({ accessToken, threadId, selectedMessageId, refreshKey, onSelectMessage }: Props) {
  const { t } = useLocale()
  const [graph, setGraph] = useState<ConversationGraphResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [reloadKey, setReloadKey] = useState(0)
  const graphRef = useRef<ConversationGraphResponse | null>(null)

  useEffect(() => {
    graphRef.current = graph
  }, [graph])

  useEffect(() => {
    let disposed = false
    void Promise.resolve()
      .then(() => {
        if (disposed) return null
        setLoading(!graphRef.current)
        setError(null)
        return getConversationGraph(accessToken, threadId)
      })
      .then((value) => {
        if (disposed || !value) return
        setGraph(value)
      })
      .catch((err) => {
        if (disposed) return
        setError(err instanceof Error ? err.message : '加载失败')
        setGraph(null)
      })
      .finally(() => {
        if (!disposed) setLoading(false)
      })
    return () => {
      disposed = true
    }
  }, [accessToken, threadId, reloadKey, refreshKey])

  const flow = useMemo(() => (
    graph
      ? buildConversationGraphFlow(graph, selectedMessageId, {
        user: t.rightPanel.conversationGraphUser,
        assistant: t.rightPanel.conversationGraphAssistant,
        system: t.rightPanel.conversationGraphSystem,
      })
      : { nodes: [], edges: [] }
  ), [
    graph,
    selectedMessageId,
    t.rightPanel.conversationGraphAssistant,
    t.rightPanel.conversationGraphSystem,
    t.rightPanel.conversationGraphUser,
  ])

  const handleNodeClick = useCallback((_: React.MouseEvent, node: ConversationGraphFlowNode) => {
    const data = node.data as ConversationGraphNodeData
    onSelectMessage(data.threadId, data.messageId)
  }, [onSelectMessage])

  const rootThreadId = graph?.root_thread_id ?? null
  const savedViewport = rootThreadId ? readGraphViewport(rootThreadId) : null
  const showGraph = graph && flow.nodes.length > 0

  return (
    <div className="conversation-graph-panel">
      <div className="conversation-graph-panel__body">
        <button
          type="button"
          className="conversation-graph-panel__reload"
          data-loading={loading}
          title={t.browserPanel.reload}
          onClick={() => setReloadKey((value) => value + 1)}
        >
          <RefreshCw size={rightPanelIconSize} />
        </button>
        {!showGraph && loading ? (
          <div className="conversation-graph-panel__state">{t.loading}</div>
        ) : error ? (
          <div className="conversation-graph-panel__state">{error}</div>
        ) : flow.nodes.length === 0 ? (
          <div className="conversation-graph-panel__state">{t.rightPanel.empty}</div>
        ) : (
          <ReactFlow
            key={rootThreadId ?? threadId}
            className="conversation-graph-flow"
            nodes={flow.nodes}
            edges={flow.edges}
            nodeTypes={nodeTypes}
            defaultViewport={savedViewport ?? undefined}
            fitView={!savedViewport}
            fitViewOptions={{ padding: 0.18 }}
            minZoom={0.25}
            maxZoom={1.6}
            nodesDraggable={false}
            nodesConnectable={false}
            elementsSelectable
            panOnScroll
            panOnDrag
            zoomOnPinch
            zoomOnScroll={false}
            selectionOnDrag={false}
            proOptions={{ hideAttribution: true }}
            onNodeClick={handleNodeClick}
            onMoveEnd={(_, viewport) => {
              if (rootThreadId) writeGraphViewport(rootThreadId, viewport)
            }}
          >
            <Background gap={22} size={1.15} color="var(--c-border-subtle)" />
            <Controls showInteractive={false} />
            <GraphOverview nodes={flow.nodes} edges={flow.edges} />
          </ReactFlow>
        )}
      </div>
    </div>
  )
}
