import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { CopSegmentBlocks } from '../components/CopSegmentBlocks'
import { LocaleProvider } from '../contexts/LocaleContext'
import type { AssistantTurnSegment } from '../assistantTurnSegments'
import type { TodoWriteRef } from '../copSegmentTimeline'
import type { ArtifactRef, SubAgentRef } from '../storage'

const originalMatchMedia = window.matchMedia
const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

function defaultMatchMedia(query: string) {
  return {
    matches: false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(() => false),
  }
}

beforeEach(() => {
  actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
  window.matchMedia = vi.fn(defaultMatchMedia)
})

afterEach(() => {
  window.matchMedia = originalMatchMedia
  if (originalActEnvironment === undefined) {
    delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
  } else {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
  }
})

async function renderBlocks(
  segment: Extract<AssistantTurnSegment, { type: 'cop' }>,
  options: {
    todoWritesForFinalDisplay?: TodoWriteRef[]
    live?: boolean
    isComplete?: boolean
    subAgents?: SubAgentRef[]
    artifacts?: ArtifactRef[]
    onOpenSubAgent?: (agent: SubAgentRef) => void
    onOpenDocument?: (artifact: ArtifactRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => void
  } = {},
) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  await act(async () => {
    root.render(
      <LocaleProvider>
        <CopSegmentBlocks
          segment={segment}
          keyPrefix="test"
          fileOps={[{ id: 'read-1', toolName: 'read_file', label: 'Read app.tsx', status: 'success', seq: 2, filePath: 'app.tsx', displayKind: 'read' }]}
          subAgents={options.subAgents}
          artifacts={options.artifacts}
          sources={[]}
          isComplete={options.isComplete ?? true}
          live={options.live}
          onOpenSubAgent={options.onOpenSubAgent}
          onOpenDocument={options.onOpenDocument}
          todoWritesForFinalDisplay={options.todoWritesForFinalDisplay}
        />
      </LocaleProvider>,
    )
  })
  return {
    container,
    cleanup: () => {
      act(() => { root.unmount() })
      container.remove()
    },
  }
}

describe('CopSegmentBlocks', () => {
  it('renders exec_command inside CopTimeline, not as top-level sibling', async () => {
    const { container, cleanup } = await renderBlocks({
      type: 'cop',
      title: null,
      items: [
        { kind: 'call', call: { toolCallId: 'cmd-1', toolName: 'exec_command', arguments: { command: 'pwd' } }, seq: 1 },
        { kind: 'call', call: { toolCallId: 'read-1', toolName: 'read', arguments: { file_path: 'app.tsx' } }, seq: 2 },
      ],
    })
    try {
      const timeline = container.querySelector('.cop-timeline-root')
      expect(container.textContent).toContain('pwd')
      expect(timeline).not.toBeNull()
      expect(timeline?.textContent).toContain('pwd')
    } finally {
      cleanup()
    }
  })

  it('renders todo_write and remaining tools inside the same process timeline', async () => {
    const { container, cleanup } = await renderBlocks({
      type: 'cop',
      title: null,
      items: [
        {
          kind: 'call',
          call: {
            toolCallId: 'todo-1',
            toolName: 'todo_write',
            arguments: {
              todos: [
                { id: 'a', content: 'Write focused test', status: 'completed' },
                { id: 'b', content: 'Wire the renderer', status: 'pending' },
              ],
            },
          },
          seq: 1,
        },
        { kind: 'call', call: { toolCallId: 'read-1', toolName: 'read', arguments: { file_path: 'app.tsx' } }, seq: 2 },
      ],
    })
    try {
      expect(container.textContent).toContain('Write focused test')
      expect(container.textContent).toContain('1/2 完成')
      expect(container.querySelector('.cop-timeline-root')).not.toBeNull()
    } finally {
      cleanup()
    }
  })

  it('renders single document_write as an action placeholder without document card', async () => {
    const { container, cleanup } = await renderBlocks({
      type: 'cop',
      title: null,
      items: [
        {
          kind: 'call',
          call: {
            toolCallId: 'doc-1',
            toolName: 'document_write',
            arguments: { filename: 'report.md', content: '# Report\nBody' },
          },
          seq: 1,
        },
      ],
    })
    try {
      expect(container.textContent).toContain('正在写入文档')
      expect(container.querySelector('[aria-label="Document"]')).toBeNull()
      expect(container.querySelector('.cop-timeline-root')).toBeNull()
    } finally {
      cleanup()
    }
  })

  it('renders completed document_write as an action placeholder without document card', async () => {
    const { container, cleanup } = await renderBlocks({
      type: 'cop',
      title: null,
      items: [
        {
          kind: 'call',
          call: {
            toolCallId: 'doc-1',
            toolName: 'document_write',
            arguments: { filename: 'report.md', content: '# Report\nBody' },
            result: { artifacts: [{ filename: 'report.md', title: 'report' }] },
          },
          seq: 1,
        },
      ],
    })
    try {
      expect(container.querySelector('[aria-label="Document"]')).toBeNull()
      expect(container.textContent).toContain('写入了文档')
      expect(container.querySelector('.cop-timeline-root')).toBeNull()
    } finally {
      cleanup()
    }
  })

  it('opens the document artifact from a document_write leaf row', async () => {
    const onOpenDocument = vi.fn()
    const artifact: ArtifactRef = { key: 'artifact-doc', filename: 'report.md', size: 0, mime_type: 'text/markdown', title: 'report' }
    const { container, cleanup } = await renderBlocks({
      type: 'cop',
      title: null,
      items: [
        {
          kind: 'call',
          call: {
            toolCallId: 'doc-1',
            toolName: 'document_write',
            arguments: { filename: 'report.md', content: '# Report\nBody' },
            result: { artifacts: [artifact] },
          },
          seq: 1,
        },
      ],
    }, { artifacts: [artifact], onOpenDocument })
    try {
      const button = container.querySelector('button')
      expect(button?.textContent).toContain('写入了文档')
      await act(async () => {
        button?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      })
      expect(onOpenDocument).toHaveBeenCalledWith(artifact, expect.objectContaining({ artifacts: [artifact] }))
    } finally {
      cleanup()
    }
  })

  it('opens the existing sub-agent from a wait_agent leaf row', async () => {
    const onOpenSubAgent = vi.fn()
    const agent: SubAgentRef = { id: 'spawn-1', subAgentId: 'sub-1', nickname: 'yansu-research', status: 'active' }
    const { container, cleanup } = await renderBlocks({
      type: 'cop',
      title: null,
      items: [
        {
          kind: 'call',
          call: {
            toolCallId: 'wait-1',
            toolName: 'wait_agent',
            arguments: { sub_agent_id: 'sub-1' },
            result: { sub_agent_id: 'sub-1', status: 'running' },
          },
          seq: 1,
        },
      ],
    }, { subAgents: [agent], onOpenSubAgent })
    try {
      const button = container.querySelector('button')
      expect(button?.textContent).toContain('等待子代理 yansu-research')
      await act(async () => {
        button?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      })
      expect(onOpenSubAgent).toHaveBeenCalledWith(agent)
    } finally {
      cleanup()
    }
  })

  it('does not duplicate the leaf icon when the timeline axis already shows it', async () => {
    const { container, cleanup } = await renderBlocks({
      type: 'cop',
      title: null,
      items: [
        {
          kind: 'call',
          call: {
            toolCallId: 'doc-1',
            toolName: 'document_write',
            arguments: { filename: 'report.md' },
          },
          seq: 1,
        },
        {
          kind: 'call',
          call: {
            toolCallId: 'wait-1',
            toolName: 'wait_agent',
            arguments: { sub_agent_id: 'sub-1' },
          },
          seq: 2,
        },
      ],
    }, { subAgents: [{ id: 'spawn-1', subAgentId: 'sub-1', nickname: 'yansu-research', status: 'active' }] })
    try {
      const documentIcons = container.querySelectorAll('.lucide-file-text')
      const agentIcons = container.querySelectorAll('.lucide-bot-message-square')
      expect(documentIcons).toHaveLength(1)
      expect(agentIcons).toHaveLength(1)
    } finally {
      cleanup()
    }
  })

  it('renders single web_search through the existing search timeline path', async () => {
    const { container, cleanup } = await renderBlocks({
      type: 'cop',
      title: null,
      items: [
        {
          kind: 'call',
          call: {
            toolCallId: 'search-1',
            toolName: 'web_search',
            arguments: { query: 'Arkloop MCP' },
            result: {
              results: [
                { title: 'Arkloop docs', url: 'https://example.test/arkloop', snippet: 'Docs' },
              ],
            },
          },
          seq: 1,
        },
      ],
    })
    try {
      expect(container.textContent).toContain('搜索 Arkloop MCP')
      expect(container.textContent).toContain('Arkloop docs')
      expect(container.textContent).not.toContain('web_search')
      expect(container.querySelector('.cop-timeline-root')).not.toBeNull()
    } finally {
      cleanup()
    }
  })

  it('renders single x_search through the search timeline path', async () => {
    const { container, cleanup } = await renderBlocks({
      type: 'cop',
      title: null,
      items: [
        {
          kind: 'call',
          call: {
            toolCallId: 'x-search-1',
            toolName: 'x_search',
            arguments: { query: 'from:@qqqqqf_' },
            result: {
              answer: 'Recent posts',
              citations: ['https://x.com/qqqqqf_/status/2056736604845404380'],
            },
          },
          seq: 1,
        },
      ],
    })
    try {
      expect(container.textContent).toContain('搜索 from:@qqqqqf_')
      expect(container.textContent).toContain('@qqqqqf_')
      expect(container.textContent).not.toContain('已检查来源')
      expect(container.textContent).not.toContain('x_search')
      expect(container.querySelector('.cop-timeline-root')).not.toBeNull()
    } finally {
      cleanup()
    }
  })

  it('timeline_title with single document_write renders an action placeholder without document card', async () => {
    const { container, cleanup } = await renderBlocks({
      type: 'cop',
      title: 'Writing report',
      items: [
        {
          kind: 'call',
          call: {
            toolCallId: 'doc-1',
            toolName: 'document_write',
            arguments: { filename: 'report.md', content: '# Report\nBody' },
          },
          seq: 1,
        },
      ],
    })
    try {
      expect(container.textContent).toContain('正在写入文档')
      expect(container.querySelector('[aria-label="Document"]')).toBeNull()
      expect(container.querySelector('.cop-timeline-root')).toBeNull()
    } finally {
      cleanup()
    }
  })

  it('keeps single document_write with thought inside COP', async () => {
    const { container, cleanup } = await renderBlocks({
      type: 'cop',
      title: null,
      items: [
        { kind: 'thinking', content: 'Need to write the report', seq: 1 },
        {
          kind: 'call',
          call: {
            toolCallId: 'doc-1',
            toolName: 'document_write',
            arguments: { filename: 'report.md', content: '# Report\nBody' },
          },
          seq: 2,
        },
      ],
    })
    try {
      expect(container.textContent).toContain('正在写入文档')
      expect(container.querySelector('.cop-timeline-root')).not.toBeNull()
    } finally {
      cleanup()
    }
  })

  it('renders a single todo status change as a compact top-level summary', async () => {
    const { container, cleanup } = await renderBlocks({
      type: 'cop',
      title: null,
      items: [
        {
          kind: 'call',
          call: {
            toolCallId: 'todo-1',
            toolName: 'todo_write',
            arguments: {
              todos: [
                { id: 'a', content: 'Write focused test', status: 'pending' },
                { id: 'b', content: 'Wire the renderer', status: 'pending' },
              ],
            },
            result: {
              old_todos: [
                { id: 'a', content: 'Write focused test', status: 'pending' },
                { id: 'b', content: 'Wire the renderer', status: 'pending' },
              ],
              todos: [
                { id: 'a', content: 'Write focused test', status: 'completed' },
                { id: 'b', content: 'Wire the renderer', status: 'pending' },
              ],
              changes: [
                { type: 'updated', id: 'a', content: 'Write focused test', previous_status: 'pending', status: 'completed', index: 0 },
              ],
              completed_count: 1,
              total_count: 2,
            },
          },
          seq: 1,
        },
      ],
    })
    try {
      const summary = container.querySelector('[data-testid="todo-change-summary"]') as HTMLButtonElement | null
      const expand = container.querySelector('[data-testid="todo-summary-expand"]') as HTMLElement | null
      expect(summary).not.toBeNull()
      expect(summary?.classList.contains('todo-summary-trigger')).toBe(true)
      expect(summary?.getAttribute('style')).toContain('color: var(--c-cop-row-fg, var(--c-text-tertiary))')
      expect(summary?.getAttribute('style')).toContain('font-weight: 400')
      expect(summary?.getAttribute('style')).toContain('font-family: inherit')
      expect(expand?.style.gridTemplateRows).toBe('0fr')
      expect(expand?.getAttribute('aria-hidden')).toBe('true')
      expect(container.textContent).toContain('完成 1/2')
      expect(container.textContent).toContain('Write focused test')
      expect(container.textContent).not.toContain('Todos')
      await act(async () => {
        summary?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      })
      expect(summary?.getAttribute('aria-expanded')).toBe('true')
      expect(expand?.style.gridTemplateRows).toBe('1fr')
      expect(expand?.getAttribute('aria-hidden')).toBe('false')
      expect(container.textContent).toContain('Wire the renderer')
    } finally {
      cleanup()
    }
  })

  it('uses activeForm for an in-progress todo summary', async () => {
    const { container, cleanup } = await renderBlocks({
      type: 'cop',
      title: null,
      items: [
        {
          kind: 'call',
          call: {
            toolCallId: 'todo-1',
            toolName: 'todo_write',
            arguments: {
              todos: [
                { id: 'a', content: 'Write focused test', status: 'completed' },
                { id: 'b', content: 'Wire the renderer', active_form: 'Wiring the renderer', status: 'pending' },
                { id: 'c', content: 'Run verification', status: 'pending' },
              ],
            },
            result: {
              old_todos: [
                { id: 'a', content: 'Write focused test', status: 'completed' },
                { id: 'b', content: 'Wire the renderer', active_form: 'Wiring the renderer', status: 'pending' },
                { id: 'c', content: 'Run verification', status: 'pending' },
              ],
              todos: [
                { id: 'a', content: 'Write focused test', status: 'completed' },
                { id: 'b', content: 'Wire the renderer', active_form: 'Wiring the renderer', status: 'in_progress' },
                { id: 'c', content: 'Run verification', status: 'pending' },
              ],
              changes: [
                { type: 'updated', id: 'b', content: 'Wire the renderer', active_form: 'Wiring the renderer', previous_status: 'pending', status: 'in_progress', index: 1 },
              ],
              completed_count: 1,
              total_count: 3,
            },
          },
          seq: 1,
        },
      ],
    })
    try {
      const summary = container.querySelector('[data-testid="todo-change-summary"]') as HTMLButtonElement | null
      expect(summary).not.toBeNull()
      expect(summary?.textContent).toContain('开始 2/3')
      expect(summary?.textContent).toContain('Wiring the renderer')
      expect(summary?.textContent).not.toContain('Wire the renderer')
    } finally {
      cleanup()
    }
  })

  it('prefers the completed todo summary when a snapshot also starts the next item', async () => {
    const { container, cleanup } = await renderBlocks({
      type: 'cop',
      title: null,
      items: [
        {
          kind: 'call',
          call: {
            toolCallId: 'todo-1',
            toolName: 'todo_write',
            arguments: {
              todos: [
                { id: 'a', content: 'Write focused test', status: 'in_progress' },
                { id: 'b', content: 'Wire the renderer', active_form: 'Wiring the renderer', status: 'pending' },
                { id: 'c', content: 'Run verification', status: 'pending' },
              ],
            },
            result: {
              old_todos: [
                { id: 'a', content: 'Write focused test', status: 'in_progress' },
                { id: 'b', content: 'Wire the renderer', active_form: 'Wiring the renderer', status: 'pending' },
                { id: 'c', content: 'Run verification', status: 'pending' },
              ],
              todos: [
                { id: 'a', content: 'Write focused test', status: 'completed' },
                { id: 'b', content: 'Wire the renderer', active_form: 'Wiring the renderer', status: 'in_progress' },
                { id: 'c', content: 'Run verification', status: 'pending' },
              ],
              changes: [
                { type: 'updated', id: 'a', content: 'Write focused test', previous_status: 'in_progress', status: 'completed', index: 0 },
                { type: 'updated', id: 'b', content: 'Wire the renderer', active_form: 'Wiring the renderer', previous_status: 'pending', status: 'in_progress', index: 1 },
              ],
              completed_count: 1,
              total_count: 3,
            },
          },
          seq: 1,
        },
      ],
    })
    try {
      const summary = container.querySelector('[data-testid="todo-change-summary"]') as HTMLButtonElement | null
      expect(summary).not.toBeNull()
      expect(summary?.textContent).toContain('完成 1/3')
      expect(summary?.textContent).toContain('Write focused test')
      expect(summary?.textContent).not.toContain('Wiring the renderer')
      expect(container.textContent).not.toContain('Todos')
    } finally {
      cleanup()
    }
  })

  it('infers a compact summary from the previous todo snapshot when old_todos is empty', async () => {
    const { container, cleanup } = await renderBlocks({
      type: 'cop',
      title: null,
      items: [
        {
          kind: 'call',
          call: {
            toolCallId: 'todo-1',
            toolName: 'todo_write',
            arguments: {
              todos: [
                { id: 'a', content: 'Write focused test', status: 'pending' },
                { id: 'b', content: 'Wire the renderer', status: 'pending' },
              ],
            },
            result: {
              todos: [
                { id: 'a', content: 'Write focused test', status: 'pending' },
                { id: 'b', content: 'Wire the renderer', status: 'pending' },
              ],
              old_todos: [],
              changes: [
                { type: 'created', id: 'a', content: 'Write focused test', status: 'pending', index: 0 },
                { type: 'created', id: 'b', content: 'Wire the renderer', status: 'pending', index: 1 },
              ],
              completed_count: 0,
              total_count: 2,
            },
          },
          seq: 1,
        },
        {
          kind: 'call',
          call: {
            toolCallId: 'todo-2',
            toolName: 'todo_write',
            arguments: {
              todos: [
                { id: 'a', content: 'Write focused test', status: 'completed' },
                { id: 'b', content: 'Wire the renderer', status: 'pending' },
              ],
            },
            result: {
              todos: [
                { id: 'a', content: 'Write focused test', status: 'completed' },
                { id: 'b', content: 'Wire the renderer', status: 'pending' },
              ],
              old_todos: [],
              changes: [
                { type: 'created', id: 'a', content: 'Write focused test', status: 'completed', index: 0 },
                { type: 'created', id: 'b', content: 'Wire the renderer', status: 'pending', index: 1 },
              ],
              completed_count: 1,
              total_count: 2,
            },
          },
          seq: 2,
        },
      ],
    })
    try {
      expect(container.querySelectorAll('[data-testid="todo-change-summary"]')).toHaveLength(1)
      expect(container.textContent).toContain('完成 1/2')
      const fullCardHeader = Array.from(container.querySelectorAll('button'))
        .find((button) => button.textContent?.includes('Todos')) as HTMLButtonElement | undefined
      expect(fullCardHeader?.textContent).toContain('1/2 完成')
    } finally {
      cleanup()
    }
  })

  it('renders a todo card with the turn-level final todo state', async () => {
    const segment: Extract<AssistantTurnSegment, { type: 'cop' }> = {
      type: 'cop',
      title: null,
      items: [
        {
          kind: 'call',
          call: {
            toolCallId: 'todo-1',
            toolName: 'todo_write',
            arguments: {
              todos: [
                { id: 'a', content: 'Write focused test', status: 'pending' },
                { id: 'b', content: 'Wire the renderer', status: 'pending' },
              ],
            },
            result: {
              todos: [
                { id: 'a', content: 'Write focused test', status: 'pending' },
                { id: 'b', content: 'Wire the renderer', status: 'pending' },
              ],
              completed_count: 0,
              total_count: 2,
            },
          },
          seq: 1,
        },
      ],
    }
    const { container, cleanup } = await renderBlocks(segment, {
      todoWritesForFinalDisplay: [
        {
          id: 'todo-1',
          toolName: 'todo_write',
          todos: [
            { id: 'a', content: 'Write focused test', status: 'pending' },
            { id: 'b', content: 'Wire the renderer', status: 'pending' },
          ],
          completedCount: 0,
          totalCount: 2,
          status: 'success',
          seq: 1,
        },
        {
          id: 'todo-2',
          toolName: 'todo_write',
          todos: [
            { id: 'a', content: 'Write focused test', status: 'completed' },
            { id: 'b', content: 'Wire the renderer', status: 'completed' },
          ],
          completedCount: 2,
          totalCount: 2,
          status: 'success',
          seq: 2,
        },
      ],
    })
    try {
      expect(container.textContent).toContain('2/2 完成')
      expect(container.querySelectorAll('svg')).not.toHaveLength(0)
    } finally {
      cleanup()
    }
  })

  it('keeps the full todo card for structural todo updates', async () => {
    const { container, cleanup } = await renderBlocks({
      type: 'cop',
      title: null,
      items: [
        {
          kind: 'call',
          call: {
            toolCallId: 'todo-1',
            toolName: 'todo_write',
            arguments: { todos: [] },
            result: {
              old_todos: [],
              todos: [
                { id: 'a', content: 'Write focused test', status: 'pending' },
                { id: 'b', content: 'Wire the renderer', status: 'pending' },
              ],
              changes: [
                { type: 'created', id: 'a', content: 'Write focused test', status: 'pending', index: 0 },
                { type: 'created', id: 'b', content: 'Wire the renderer', status: 'pending', index: 1 },
              ],
              completed_count: 0,
              total_count: 2,
            },
          },
          seq: 1,
        },
      ],
    })
    try {
      expect(container.querySelector('[data-testid="todo-change-summary"]')).toBeNull()
      expect(container.textContent).toContain('Todos')
      expect(container.textContent).toContain('0/2 完成')
      expect(container.textContent).toContain('Write focused test')
      expect(container.textContent).toContain('Wire the renderer')
      const firstItem = container.querySelector('.todo-list-item-rise') as HTMLElement | null
      expect(firstItem).not.toBeNull()
      expect(firstItem?.style.borderTop).toBe('')
    } finally {
      cleanup()
    }
  })
})
