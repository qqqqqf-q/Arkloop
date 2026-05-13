import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { CopSegmentBlocks } from '../components/CopSegmentBlocks'
import { LocaleProvider } from '../contexts/LocaleContext'
import type { AssistantTurnSegment } from '../assistantTurnSegments'
import type { TodoWriteRef } from '../copSegmentTimeline'

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
  options: { todoWritesForFinalDisplay?: TodoWriteRef[]; live?: boolean; isComplete?: boolean } = {},
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
          sources={[]}
          isComplete={options.isComplete ?? true}
          live={options.live}
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

  it('renders todo_write as a top-level sibling; remaining single tool renders in card mode without COP shell', async () => {
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
      expect(container.textContent).toContain('1 of 2 Done')
      // todo_write is promoted as top-level, read renders in card mode without COP shell
      expect(container.querySelector('.cop-timeline-root')).toBeNull()
    } finally {
      cleanup()
    }
  })

  it('renders single document_write in card mode without COP shell', async () => {
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
      expect(container.textContent).toContain('report.md')
      expect(container.querySelector('[aria-label="Document"]')).not.toBeNull()
      expect(container.querySelector('.cop-timeline-root')).toBeNull()
    } finally {
      cleanup()
    }
  })

  it('timeline_title with single document_write renders in card mode without COP shell', async () => {
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
      expect(container.textContent).toContain('report.md')
      expect(container.querySelector('[aria-label="Document"]')).not.toBeNull()
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
      expect(container.textContent).toContain('document_write')
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
      expect(container.textContent).toContain('Completed 1 of 2')
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
      expect(summary?.textContent).toContain('Started 2 of 3')
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
      expect(summary?.textContent).toContain('Completed 1 of 3')
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
      expect(container.textContent).toContain('Completed 1 of 2')
      const fullCardHeader = Array.from(container.querySelectorAll('button'))
        .find((button) => button.textContent?.includes('Todos')) as HTMLButtonElement | undefined
      expect(fullCardHeader?.textContent).toContain('1 of 2 Done')
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
      expect(container.textContent).toContain('2 of 2 Done')
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
      expect(container.textContent).toContain('0 of 2 Done')
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
