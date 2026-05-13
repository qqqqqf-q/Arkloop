import type { ComponentType, SVGProps } from 'react'
import {
  BotMessageSquare,
  CheckCircle2,
  ClipboardList,
  FilePenLine,
  FileSearch,
  FileText,
  Globe,
  FileImage,
  PanelsTopLeft,
  Search,
  SquarePen,
  Terminal,
} from 'lucide-react'
import type { CopSegmentCategory } from '../../copSubSegment'
import type { FileOpRef } from '../../storage'
import { normalizeToolName } from '../../toolPresentation'
import { isWebSearchToolName } from '../../webSearchTimelineFromAgentEvent'
import type { WebSearchPhaseStep } from './types'

type Icon = ComponentType<SVGProps<SVGSVGElement>>

export type TimelineMarker =
  | { kind: 'dot' }
  | { kind: 'icon'; icon: Icon; label: string }

export const DOT_MARKER: TimelineMarker = { kind: 'dot' }

export function markerForCategory(category: CopSegmentCategory): TimelineMarker {
  switch (category) {
    case 'explore': return { kind: 'icon', icon: FileSearch, label: 'Explore' }
    case 'exec': return { kind: 'icon', icon: Terminal, label: 'Command' }
    case 'edit': return { kind: 'icon', icon: SquarePen, label: 'Edit' }
    case 'agent': return { kind: 'icon', icon: BotMessageSquare, label: 'Agent' }
    case 'fetch':
    case 'search': return { kind: 'icon', icon: Globe, label: 'Search' }
    case 'image': return { kind: 'icon', icon: FileImage, label: 'Image' }
    case 'plan': return { kind: 'icon', icon: ClipboardList, label: 'Plan' }
    case 'generic': return DOT_MARKER
  }
}

export function markerForToolName(toolName: string): TimelineMarker {
  const name = normalizeToolName(toolName)
  if (isWebSearchToolName(toolName)) return { kind: 'icon', icon: Globe, label: 'Search' }
  switch (name) {
    case 'grep':
    case 'glob':
    case 'read_file':
    case 'load_tools':
    case 'load_skill':
    case 'lsp':
      return { kind: 'icon', icon: FileSearch, label: 'Explore' }
    case 'edit':
    case 'edit_file':
    case 'write_file':
    case 'memory_edit':
    case 'notebook_edit':
    case 'memory_write':
    case 'notebook_write':
      return { kind: 'icon', icon: toolName === 'edit' || toolName === 'edit_file' ? SquarePen : FilePenLine, label: 'Edit' }
    case 'exec_command':
    case 'python_execute':
    case 'continue_process':
    case 'terminate_process':
      return { kind: 'icon', icon: Terminal, label: 'Command' }
    case 'spawn_agent':
    case 'send_input':
    case 'wait_agent':
    case 'resume_agent':
    case 'close_agent':
    case 'interrupt_agent':
      return { kind: 'icon', icon: BotMessageSquare, label: 'Agent' }
    case 'todo_write':
      return { kind: 'icon', icon: ClipboardList, label: 'Todos' }
    case 'enter_plan_mode':
    case 'exit_plan_mode':
      return { kind: 'icon', icon: ClipboardList, label: 'Plan' }
    case 'document_write':
      return { kind: 'icon', icon: FileText, label: 'Document' }
    case 'create_artifact':
    case 'show_widget':
      return { kind: 'icon', icon: PanelsTopLeft, label: 'Artifact' }
    case 'image_generate':
      return { kind: 'icon', icon: FileImage, label: 'Image' }
    case 'browser':
    case 'web_fetch':
      return { kind: 'icon', icon: Globe, label: 'Browser' }
    default:
      return DOT_MARKER
  }
}

export function markerForFileOp(op: FileOpRef): TimelineMarker {
  return markerForToolName(op.toolName)
}

export function markerForStep(step: WebSearchPhaseStep): TimelineMarker {
  if (step.kind === 'finished') return { kind: 'icon', icon: CheckCircle2, label: 'Done' }
  if (step.kind === 'planning') return DOT_MARKER
  if (step.kind === 'reviewing') return { kind: 'icon', icon: Globe, label: 'Review' }
  return { kind: 'icon', icon: Search, label: 'Search' }
}
