import type { ApiClient } from "../api/client"
import { clearChat, setError } from "../store/chat"
import { setCurrentThreadId, setOverlay, applyCurrentEffort, applyCurrentModel, currentModelSupportsReasoning, setTokenUsage } from "../store/app"
import { parseEffort } from "./effort"
import { findModel, listFlatModels } from "./models"

export async function handleSlashCommand(client: ApiClient, input: string): Promise<boolean> {
  const trimmed = input.trim()
  if (!trimmed.startsWith("/")) return false

  try {
    if (trimmed === "/models" || trimmed === "/model") {
      setError(null)
      setOverlay("model")
      return true
    }

    if (trimmed === "/effort") {
      if (!currentModelSupportsReasoning()) {
        setError("current model does not support effort")
        return true
      }
      setError(null)
      setOverlay("effort")
      return true
    }

    if (trimmed === "/sessions" || trimmed === "/session") {
      setError(null)
      setOverlay("session")
      return true
    }

    if (trimmed === "/new") {
      clearChat()
      setCurrentThreadId(null)
      setTokenUsage({ input: 0, output: 0, context: 0 })
      setError(null)
      return true
    }

    if (trimmed.startsWith("/model ")) {
      const target = trimmed.slice("/model ".length).trim()
      if (!target) {
        setError("usage: /model <name>")
        return true
      }
      const models = await listFlatModels(client)
      const match = findModel(models, target)
      if (!match) {
        setError(`model not found: ${target}`)
        return true
      }
      applyCurrentModel(match.id, match.label, match.supportsReasoning, match.contextLength)
      setError(null)
      return true
    }

    if (trimmed.startsWith("/effort ")) {
      if (!currentModelSupportsReasoning()) {
        setError("current model does not support effort")
        return true
      }
      const target = trimmed.slice("/effort ".length).trim()
      const effort = parseEffort(target)
      if (!effort) {
        setError("usage: /effort <none|minimal|low|medium|high|max>")
        return true
      }
      applyCurrentEffort(effort)
      setError(null)
      return true
    }
  } catch (err) {
    setError(err instanceof Error ? err.message : String(err))
    return true
  }

  setError(`unknown command: ${trimmed}`)
  return true
}
