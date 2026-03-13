import { BootstrapPage as SharedBootstrapPage, type ConsoleTarget } from '@arkloop/shared'
import { useLocale } from '../contexts/LocaleContext'
import { useMemo } from 'react'

type Props = {
  onLoggedIn: (accessToken: string) => void
}

export function BootstrapPage({ onLoggedIn }: Props) {
  const { t, locale } = useLocale()

  const consoles = useMemo<ConsoleTarget[]>(() => {
    const consoleUrl = import.meta.env.VITE_CONSOLE_URL || 'http://localhost:19081'
    return [
      { name: t.bootstrap.consoleLiteLabel, description: t.bootstrap.consoleLiteDescription, url: '/dashboard', current: true },
      { name: t.bootstrap.consoleLabel, description: t.bootstrap.consoleDescription, url: consoleUrl },
    ]
  }, [t])

  return <SharedBootstrapPage onLoggedIn={onLoggedIn} t={t} locale={locale} consoles={consoles} />
}
