import { describe, expect, it } from 'vitest'
import { jsonStringifyForDebugDisplay, redactDataUrlsInString } from './debugPayloadRedact'

describe('redactDataUrlsInString', () => {
  it('leaves short data URLs intact', () => {
    const s = 'data:image/png;base64,Zm9v' // "foo"
    expect(redactDataUrlsInString(s)).toBe(s)
  })

  it('redacts long base64 payload', () => {
    const b64 = 'A'.repeat(120)
    const s = `prefix data:image/jpeg;base64,${b64} suffix`
    const out = redactDataUrlsInString(s)
    expect(out).toMatch(/\[data:image\/jpeg;base64 redacted ~1[0-9]{2} chars\]/)
    expect(out).not.toContain('AAAA')
  })
})

describe('jsonStringifyForDebugDisplay', () => {
  it('redacts nested image url strings', () => {
    const long = 'B'.repeat(100)
    const obj = { image_url: { url: `data:image/png;base64,${long}` } }
    const out = jsonStringifyForDebugDisplay(obj, 2)
    expect(out).toContain('redacted')
    expect(out).not.toContain('BBBB')
  })
})
