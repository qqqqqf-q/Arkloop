import { createContext, useContext, useEffect, useState, useMemo, type ReactNode } from 'react'

export type Locale = 'zh' | 'en'

type LocaleContextValue<T> = {
  locale: Locale
  setLocale: (l: Locale) => void
  t: T
}

export function createLocaleContext<T>() {
  const Ctx = createContext<LocaleContextValue<T> | null>(null)

  function resolveDocumentLang(locale: Locale): string {
    if (typeof document === 'undefined') return locale === 'zh' ? 'zh-CN' : 'en'
    const bodyFont = document.documentElement.dataset.bodyFont
    if (bodyFont === 'default') return 'zh-CN'
    if (bodyFont === 'system') return 'en'
    return locale === 'zh' ? 'zh-CN' : 'en'
  }

  function LocaleProvider({
    children,
    locales,
    readLocale,
    writeLocale,
  }: {
    children: ReactNode
    locales: Record<Locale, T>
    readLocale: () => Locale
    writeLocale: (l: Locale) => void
  }) {
    const [locale, setLocaleState] = useState<Locale>(readLocale)

    useEffect(() => {
      if (typeof document === 'undefined') return
      const applyDocumentLang = () => {
        document.documentElement.lang = resolveDocumentLang(locale)
      }

      applyDocumentLang()

      const observer = new MutationObserver(() => {
        applyDocumentLang()
      })
      observer.observe(document.documentElement, {
        attributes: true,
        attributeFilter: ['data-body-font'],
      })

      return () => observer.disconnect()
    }, [locale])

    const setLocale = (l: Locale) => {
      writeLocale(l)
      setLocaleState(l)
    }

    const t = useMemo(() => locales[locale], [locales, locale])

    return (
      <Ctx.Provider value={{ locale, setLocale, t }}>
        {children}
      </Ctx.Provider>
    )
  }

  function useLocale(): LocaleContextValue<T> {
    const ctx = useContext(Ctx)
    if (!ctx) throw new Error('useLocale must be used within LocaleProvider')
    return ctx
  }

  return { LocaleProvider, useLocale }
}
