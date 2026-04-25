import { describe, expect, it, vi } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import type { ReactElement } from 'react'
import { LocaleProvider } from '../contexts/LocaleContext'
import { ErrorCallout } from '../components/ErrorCallout'

vi.mock('../storage', async () => {
  const actual = await vi.importActual<typeof import('../storage')>('../storage')
  return {
    ...actual,
    readLocaleFromStorage: vi.fn(() => 'zh'),
    writeLocaleToStorage: vi.fn(),
  }
})

function renderWithLocale(ui: ReactElement): string {
  return renderToStaticMarkup(<LocaleProvider>{ui}</LocaleProvider>)
}

describe('ErrorCallout', () => {
  it('应将错误码映射为用户可读文案，并默认收起详情', () => {
    const html = renderWithLocale(
      <ErrorCallout error={{ message: 'invalid credentials', code: 'auth.invalid_credentials' }} />,
    )

    expect(html).toContain('账号或密码错误')
    expect(html).not.toContain('invalid credentials')
    expect(html).not.toContain('auth.invalid_credentials')
  })
})
