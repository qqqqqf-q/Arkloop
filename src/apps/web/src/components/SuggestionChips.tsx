import { useState, useEffect } from 'react'
import { apiFetch } from '../api'
import { isDesktop } from '@arkloop/shared/desktop'
import { secondaryButtonXsCls } from './buttonStyles'

const suggestionBorderStyle = {
  border: '0.65px solid color-mix(in srgb, var(--c-border) 55%, transparent 45%)',
}

export type Suggestion = {
  short_title: string
  full_prompt: string
}

type SuggestionsResponse = {
  suggestions: Suggestion[]
  expires_at?: string
  updated_at?: string
}

type SuggestionChipsProps = {
  mode: 'chat' | 'work'
  onSelect: (suggestion: Suggestion) => void
  visible: boolean
  accessToken: string
}

export function SuggestionChips({ mode, onSelect, visible, accessToken }: SuggestionChipsProps) {
  const [suggestions, setSuggestions] = useState<Suggestion[]>([])

  useEffect(() => {
    if (!visible || !accessToken) {
      setSuggestions([])
      return
    }
    const controller = new AbortController()
    const path = isDesktop()
      ? `/v1/desktop/memory/suggestions?mode=${mode}`
      : `/v1/me/suggestions?mode=${mode}`

    apiFetch<SuggestionsResponse>(path, { accessToken, signal: controller.signal })
      .then((res) => {
        setSuggestions((res.suggestions ?? []).slice(0, 3))
      })
      .catch(() => {
        setSuggestions([])
      })

    return () => controller.abort()
  }, [visible, accessToken, mode])

  if (!visible || suggestions.length === 0) return null

  return (
    <div
      className="mt-3 flex justify-center gap-2"
      style={{
        opacity: visible ? 1 : 0,
        transform: visible ? 'translateY(0)' : 'translateY(6px)',
        transition: 'opacity 0.3s ease, transform 0.3s ease',
      }}
    >
      {suggestions.map((s) => (
        <button
          key={s.short_title}
          type="button"
          onClick={() => onSelect(s)}
          className={`${secondaryButtonXsCls} h-auto py-1.5 text-[13px] font-normal`}
          style={suggestionBorderStyle}
        >
          {s.short_title}
        </button>
      ))}
    </div>
  )
}
