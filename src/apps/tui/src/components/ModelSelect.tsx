import type { InputRenderable, ScrollBoxRenderable } from "@opentui/core"
import { useKeyboard } from "@opentui/solid"
import { createEffect, createMemo, createResource, createSignal, For, on, Show } from "solid-js"
import type { ApiClient } from "../api/client"
import { cycleEffort, effortSymbol, formatEffort } from "../lib/effort"
import { activeText, tuiTheme } from "../lib/theme"
import { defaultModel, listFlatModels, type FlatModel } from "../lib/models"
import { applyCurrentEffort, applyCurrentModel, currentEffort, currentModel, setOverlay } from "../store/app"
import { OverlaySurface } from "./OverlaySurface"

interface Props {
  client: ApiClient
}

interface IndexedModel extends FlatModel {
  index: number
}

interface ModelGroup {
  provider: string
  items: IndexedModel[]
}

export function ModelSelect(props: Props) {
  const [models] = createResource(() => listFlatModels(props.client))
  const [query, setQuery] = createSignal("")
  const [selectedIndex, setSelectedIndex] = createSignal(0)
  const [pendingEffort, setPendingEffort] = createSignal(currentEffort())
  let scroll: ScrollBoxRenderable | undefined
  let input: InputRenderable | undefined

  const visibleModels = createMemo(() => {
    const items = (models() ?? []).filter((item) => item.showInPicker)
    const needle = query().trim().toLowerCase()
    if (!needle) return items
    return items.filter((item) => {
      return [item.id, item.provider].some((value) => value.toLowerCase().includes(needle))
    })
  })

  const groupedModels = createMemo<ModelGroup[]>(() => {
    const groups: ModelGroup[] = []
    const map = new Map<string, ModelGroup>()
    visibleModels().forEach((item, index) => {
      const group = map.get(item.provider)
      const indexed = { ...item, index }
      if (group) {
        group.items.push(indexed)
        return
      }
      const next: ModelGroup = {
        provider: item.provider,
        items: [indexed],
      }
      map.set(item.provider, next)
      groups.push(next)
    })
    // 把当前选中模型的分组移到最前面
    const current = currentModel()
    if (current) {
      const idx = groups.findIndex((g) => g.items.some((item) => item.id === current))
      if (idx > 0) {
        const [group] = groups.splice(idx, 1)
        groups.unshift(group)
      }
    }
    return groups
  })

  createEffect(on(
    [visibleModels, query, currentModel],
    ([items, currentQuery, current]) => {
      if (items.length === 0) {
        setSelectedIndex(0)
        return
      }
      const currentIndex = current ? items.findIndex((item) => item.id === current) : -1
      if (currentIndex >= 0 && currentQuery.trim() === "") {
        setSelectedIndex(currentIndex)
        return
      }
      if (selectedIndex() >= items.length) {
        setSelectedIndex(items.length - 1)
      }
    },
  ))

  createEffect(() => {
    const idx = selectedIndex()
    const selected = visibleModels()[idx]
    if (!selected || !scroll) return
    setTimeout(() => {
      if (!scroll) return
      const row = scroll.getChildren().find((child) => child.id === selected.id)
      if (!row) return
      const relativeTop = row.y - scroll.y
      if (relativeTop < 0) {
        scroll.scrollBy(relativeTop)
        return
      }
      if (relativeTop >= scroll.height) {
        scroll.scrollBy(relativeTop - scroll.height + 1)
      }
    }, 0)
  })

  createEffect(() => {
    if (!input || input.isDestroyed) return
    setTimeout(() => {
      if (!input || input.isDestroyed || input.focused) return
      input.focus()
    }, 0)
  })

  useKeyboard((key) => {
    if (key.name === "escape") {
      setOverlay(null)
      return
    }

    if (key.name === "up") {
      move(-1)
      return
    }

    if (key.name === "down") {
      move(1)
      return
    }

    if (key.name === "pageup") {
      move(-8)
      return
    }

    if (key.name === "pagedown") {
      move(8)
      return
    }

    if (key.name === "left") {
      if (!visibleModels()[selectedIndex()]?.supportsReasoning) return
      setPendingEffort((prev) => cycleEffort(prev, "left"))
      return
    }

    if (key.name === "right") {
      if (!visibleModels()[selectedIndex()]?.supportsReasoning) return
      setPendingEffort((prev) => cycleEffort(prev, "right"))
      return
    }

    if (key.name === "return") {
      const item = visibleModels()[selectedIndex()]
      if (!item) return
      handleSelect(item.id)
    }
  })

  function move(delta: number) {
    const items = visibleModels()
    if (items.length === 0) return
    let next = selectedIndex() + delta
    if (next < 0) next = 0
    if (next > items.length - 1) next = items.length - 1
    setSelectedIndex(next)
  }

  function handleSelect(value: string) {
    const options = models() ?? []
    const picked = options.find((item) => item.id === value) ?? defaultModel(options)
    if (picked) {
      applyCurrentModel(picked.id, picked.label, picked.supportsReasoning, picked.contextLength)
      if (picked.supportsReasoning) {
        applyCurrentEffort(pendingEffort())
      }
    }
    setOverlay(null)
  }

  return (
    <OverlaySurface title="Models" width={96}>
      <box paddingLeft={3} paddingRight={3} paddingTop={1} paddingBottom={1} backgroundColor={tuiTheme.panel}>
        <input
          ref={(item: InputRenderable) => {
            input = item
          }}
          placeholder="Search model"
          placeholderColor={tuiTheme.textMuted}
          textColor={tuiTheme.text}
          focusedTextColor={tuiTheme.text}
          focusedBackgroundColor={tuiTheme.element}
          cursorColor={tuiTheme.primary}
          onInput={(value) => setQuery(value)}
        />
      </box>
      <box paddingLeft={2} paddingRight={2} paddingBottom={1}>
        <Show
          when={!models.loading && visibleModels().length > 0}
          fallback={
            <box paddingLeft={2} paddingRight={2} paddingTop={1} paddingBottom={1}>
              <text content={models.loading ? "Loading..." : "No models available"} fg={tuiTheme.textMuted} />
            </box>
          }
        >
          <scrollbox ref={(item: ScrollBoxRenderable) => { scroll = item }} maxHeight={18} scrollbarOptions={{ visible: false }}>
            <box flexDirection="column">
              <For each={groupedModels()}>
                {(group, groupIndex) => (
                  <box flexDirection="column" paddingTop={groupIndex() === 0 ? 0 : 1}>
                    <box paddingLeft={1}>
                      <text content={group.provider} fg={tuiTheme.textMuted} />
                    </box>
                    <For each={group.items}>
                      {(item) => {
                        const active = () => item.index === selectedIndex()
                        const current = () => item.id === currentModel()
                        const showEffort = () => active() && item.supportsReasoning
                        return (
                          <box
                            id={item.id}
                            flexDirection="row"
                            justifyContent="space-between"
                            paddingLeft={2}
                            paddingRight={2}
                            backgroundColor={active() ? tuiTheme.primary : tuiTheme.panel}
                            onMouseDown={() => setSelectedIndex(item.index)}
                            onMouseUp={() => handleSelect(item.id)}
                          >
                            <box flexDirection="row" gap={1} flexGrow={1}>
                              <text content={current() ? "✓" : " "} fg={active() ? activeText : tuiTheme.primary} />
                              <text content={item.id} fg={active() ? activeText : tuiTheme.text} />
                              <Show when={showEffort()}>
                                <text content="·" fg={activeText} />
                                <Show when={effortSymbol(pendingEffort())}>
                                  <text content={effortSymbol(pendingEffort())} fg={activeText} />
                                </Show>
                                <text content={formatEffort(pendingEffort())} fg={activeText} />
                              </Show>
                            </box>
                          </box>
                        )
                      }}
                    </For>
                  </box>
                )}
              </For>
            </box>
          </scrollbox>
        </Show>
      </box>
      <box
        flexDirection="row"
        justifyContent="space-between"
        paddingLeft={3}
        paddingRight={3}
        paddingTop={1}
        paddingBottom={1}
        border={["top"]}
        borderColor={tuiTheme.borderSubtle}
      >
        <text
          content={visibleModels()[selectedIndex()]?.supportsReasoning
            ? `↑↓ model · ←→ ${effortSymbol(pendingEffort())}${effortSymbol(pendingEffort()) ? " " : ""}${formatEffort(pendingEffort())}`
            : "↑↓ model · effort not supported"}
          fg={tuiTheme.textMuted}
        />
        <text content="enter select · esc close" fg={tuiTheme.textMuted} />
      </box>
    </OverlaySurface>
  )
}
