import { describe, expect, it } from 'vitest'

import { shouldDelayLocalSession } from '../appAuthStartup'

describe('app auth startup', () => {
  it('delays local session exchange until desktop runtime is ready', () => {
    expect(shouldDelayLocalSession(true, false, true)).toBe(true)
    expect(shouldDelayLocalSession(true, true, null)).toBe(true)
    expect(shouldDelayLocalSession(true, true, false)).toBe(true)
    expect(shouldDelayLocalSession(true, true, true)).toBe(false)
  })

  it('does not delay non-local auth startup', () => {
    expect(shouldDelayLocalSession(false, false, null)).toBe(false)
  })
})
