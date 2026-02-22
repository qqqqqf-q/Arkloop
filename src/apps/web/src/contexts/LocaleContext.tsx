import { createContext, useContext, useState, useMemo, type ReactNode } from 'react'
import { locales, type Locale, type LocaleStrings } from '../locales'
import { readLocaleFromStorage, writeLocaleToStorage } from '../storage'

type LocaleContextValue = {
  locale: Locale
  setLocale: (l: Locale) => void
  t: LocaleStrings
}

const LocaleContext = createContext<LocaleContextValue | null>(null)

export function LocaleProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(readLocaleFromStorage)

  const setLocale = (l: Locale) => {
    writeLocaleToStorage(l)
    setLocaleState(l)
  }

  const t = useMemo(() => locales[locale], [locale])

  return (
    <LocaleContext.Provider value={{ locale, setLocale, t }}>
      {children}
    </LocaleContext.Provider>
  )
}

export function useLocale(): LocaleContextValue {
  const ctx = useContext(LocaleContext)
  if (!ctx) throw new Error('useLocale must be used within LocaleProvider')
  return ctx
}
