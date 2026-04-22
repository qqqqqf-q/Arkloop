import { createResource } from "solid-js"
import { currentThreadId, setOverlay, setCurrentThreadId, setTokenUsage } from "../store/app"
import { clearChat } from "../store/chat"
import type { ApiClient } from "../api/client"
import { PickerOverlay } from "./PickerOverlay"
import { isWorkThread } from "../lib/threadMode"

interface Props {
  client: ApiClient
}

export function SessionList(props: Props) {
  const [threads] = createResource(() => props.client.listThreads(20))

  function handleSelect(value: string) {
    clearChat()
    if (value === "__new__") {
      setCurrentThreadId(null)
    } else {
      setCurrentThreadId(value)
    }
    setTokenUsage({ input: 0, output: 0, context: 0 })
    setOverlay(null)
  }

  return (
    <PickerOverlay
      title="Sessions"
      items={[
        { value: "__new__", title: "New session", meta: "ctrl+n" },
        ...((threads() ?? [])
          .filter((thread) => isWorkThread(thread))
          .map((thread) => ({
          value: thread.id,
          title: thread.title || thread.id.slice(0, 8),
          description: new Date(thread.created_at).toLocaleDateString(),
          meta: thread.id.slice(0, 8),
        }))),
      ]}
      currentValue={currentThreadId()}
      loading={threads.loading}
      emptyText="No sessions"
      placeholder="Search session"
      onClose={() => setOverlay(null)}
      onSelect={(item) => handleSelect(item.value)}
    />
  )
}
