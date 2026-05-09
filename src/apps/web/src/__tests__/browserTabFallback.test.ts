import { describe, expect, it } from 'vitest'

import { buildBrowserTabFailureFallbackUrl } from '../../../desktop/src/main/browser-tab-fallback'

describe('browser tab failure fallback', () => {
  it('builds a local fallback page that preserves the failed url and error details', () => {
    const fallbackUrl = buildBrowserTabFailureFallbackUrl(
      'https://bad.test/path?q=1',
      'net::ERR_NAME_NOT_RESOLVED',
    )

    expect(fallbackUrl.startsWith('data:text/html;charset=utf-8,')).toBe(true)

    const html = decodeURIComponent(fallbackUrl.slice('data:text/html;charset=utf-8,'.length))
    expect(html).toContain('Unable to open this page')
    expect(html).toContain('https://bad.test/path?q=1')
    expect(html).toContain('net::ERR_NAME_NOT_RESOLVED')
    expect(html).toContain('Try editing the address or retrying the request.')
  })
})
