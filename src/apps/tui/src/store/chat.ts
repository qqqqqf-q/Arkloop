import { batch, createSignal } from "solid-js"
import { createStore, produce } from "solid-js/store"
import {
  createEmptyAssistantTurnFoldState,
  foldAssistantTurnEvent,
  snapshotAssistantTurn,
  type AssistantTurnFoldState,
  type AssistantTurnUi,
} from "../lib/assistantTurn"
import { buildTurns } from "../lib/runTurns"
import type { LlmTurn, RunEventRaw } from "../lib/runTurns"

const [historyTurns, setHistoryTurns] = createStore<LlmTurn[]>([])
const [liveRunEvents, setLiveRunEvents] = createStore<RunEventRaw[]>([])
const [liveAssistantTurn, setLiveAssistantTurn] = createSignal<AssistantTurnUi | null>(null)
const [streaming, setStreaming] = createSignal(false)
const [pendingUserInput, setPendingUserInput] = createSignal<string | null>(null)
const [error, setError] = createSignal<string | null>(null)
const [debugInfo, setDebugInfo] = createSignal("")
let assistantTurnFoldState: AssistantTurnFoldState = createEmptyAssistantTurnFoldState()

export {
  historyTurns, setHistoryTurns,
  liveRunEvents, setLiveRunEvents,
  liveAssistantTurn, setLiveAssistantTurn,
  streaming, setStreaming,
  pendingUserInput, setPendingUserInput,
  error, setError,
  debugInfo, setDebugInfo,
}

export function liveTurns(): LlmTurn[] {
  return buildTurns([...liveRunEvents])
}

export function allTurns(): LlmTurn[] {
  return [...historyTurns, ...liveTurns()]
}

function archiveTurns(turns: LlmTurn[]) {
  if (turns.length === 0) return
  setHistoryTurns(produce((items) => {
    items.push(...turns)
  }))
}

function resetLiveState(nextPendingUserInput: string | null) {
  setLiveRunEvents([])
  assistantTurnFoldState = createEmptyAssistantTurnFoldState()
  setLiveAssistantTurn(null)
  setPendingUserInput(nextPendingUserInput)
}

export function startLiveTurn(input: string) {
  const shouldArchive = liveRunEvents.length > 0 || pendingUserInput() !== null || liveAssistantTurn() !== null
  const turns = shouldArchive ? liveTurns() : []
  batch(() => {
    archiveTurns(turns)
    resetLiveState(input)
  })
}

export function appendRunEvent(event: RunEventRaw) {
  setLiveRunEvents(produce((items) => {
    items.push(event)
  }))
  if (event.type === "message.delta" || event.type === "tool.call" || event.type === "tool.result") {
    foldAssistantTurnEvent(assistantTurnFoldState, event)
    setLiveAssistantTurn(snapshotAssistantTurn(assistantTurnFoldState))
  }
}

export function commitLiveTurns() {
  const turns = liveTurns()
  batch(() => {
    archiveTurns(turns)
    resetLiveState(null)
  })
}

export function clearChat() {
  setHistoryTurns([])
  setLiveRunEvents([])
  assistantTurnFoldState = createEmptyAssistantTurnFoldState()
  setLiveAssistantTurn(null)
  setStreaming(false)
  setPendingUserInput(null)
  setError(null)
  setDebugInfo("")
}
