import {
  assistantTurnPlainText,
  buildAssistantTurnFromEvents as buildSharedAssistantTurnFromEvents,
  copSegmentCalls,
  createEmptyAssistantTurnFoldState,
  finalizeAndDrainTurn,
  finalizeAssistantTurnFoldState,
  foldAssistantTurnEvent as foldSharedAssistantTurnEvent,
  requestAssistantTurnThinkingBreak,
  snapshotAssistantTurn,
  splitWorkGroup,
  type AssistantTurnEvent,
  type AssistantTurnFoldState,
  type AssistantTurnSegment,
  type AssistantTurnUi,
  type CopBlockItem,
  type TurnToolCallRef,
  type WorkGroup,
} from '../../shared/src/assistantTurn'
import {
  agentEventDataRecord,
  agentEventToolInput,
  agentEventToolOutput,
} from './agent-ui/event-data'
import type { AgentUIEvent } from './agent-ui/contract'

export {
  assistantTurnPlainText,
  copSegmentCalls,
  createEmptyAssistantTurnFoldState,
  finalizeAndDrainTurn,
  finalizeAssistantTurnFoldState,
  requestAssistantTurnThinkingBreak,
  snapshotAssistantTurn,
  splitWorkGroup,
  type AssistantTurnEvent,
  type AssistantTurnFoldState,
  type AssistantTurnSegment,
  type AssistantTurnUi,
  type CopBlockItem,
  type TurnToolCallRef,
  type WorkGroup,
}

function toAssistantTurnEventType(type: string): string {
  switch (type) {
    case 'assistant-delta':
      return 'message.delta'
    case 'tool-call':
      return 'tool.call'
    case 'tool-result':
      return 'tool.result'
    case 'segment-start':
      return 'run.segment.start'
    case 'segment-end':
      return 'run.segment.end'
    default:
      return type
  }
}

function toAssistantTurnEventData(event: AgentUIEvent): unknown {
  const data = agentEventDataRecord(event.data)
  switch (event.type) {
    case 'assistant-delta':
      return {
        role: data?.role,
        channel: data?.channel,
        content_delta: data?.delta,
      }
    case 'tool-call':
      return {
        tool_call_id: data?.toolCallId,
        tool_name: data?.toolName ?? event.toolName,
        arguments: agentEventToolInput(event.data),
        display_description: data?.displayDescription,
      }
    case 'tool-result':
      return {
        tool_call_id: data?.toolCallId,
        tool_name: data?.toolName ?? event.toolName,
        result: agentEventToolOutput(event.data),
        error: data?.error,
      }
    case 'segment-start':
      return {
        segment_id: data?.segmentId,
        kind: data?.kind,
        display: data?.display,
      }
    case 'segment-end':
      return {
        segment_id: data?.segmentId,
      }
    default:
      return event.data
  }
}

function toAssistantTurnEvent(event: AgentUIEvent): AssistantTurnEvent {
  return {
    event_id: event.id,
    run_id: event.streamId,
    seq: event.order,
    ts: event.timestamp,
    type: toAssistantTurnEventType(event.type),
    data: toAssistantTurnEventData(event),
    tool_name: event.toolName,
    error_class: event.errorCode,
  }
}

export function foldAssistantTurnEvent(
  state: AssistantTurnFoldState,
  event: AgentUIEvent,
): void {
  foldSharedAssistantTurnEvent(state, toAssistantTurnEvent(event))
}

export function buildAssistantTurnFromAgentEvents(
  events: readonly AgentUIEvent[],
): AssistantTurnUi {
  return buildSharedAssistantTurnFromEvents(events.map(toAssistantTurnEvent))
}
