import { describe, expect, it } from 'vitest'
import { isTimelineText } from '../timelineText'

describe('timeline text validation', () => {
  it('rejects incomplete structured timeline text', () => {
    expect(isTimelineText({ kind: 'join' })).toBe(false)
    expect(isTimelineText({ kind: 'with_ellipsis' })).toBe(false)
    expect(isTimelineText({ kind: 'steps_completed' })).toBe(false)
    expect(isTimelineText({ kind: 'missing_kind' })).toBe(false)
  })

  it('accepts valid nested structured timeline text', () => {
    expect(isTimelineText({
      kind: 'join',
      separator: ', ',
      parts: [
        { kind: 'read_files', count: 2 },
        { kind: 'search', tense: 'done', query: 'Arkloop' },
      ],
    })).toBe(true)
  })
})
