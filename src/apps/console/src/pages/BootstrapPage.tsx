import { BootstrapPage as SharedBootstrapPage, type ConsoleTarget } from '@arkloop/shared'
import { useLocale } from '../contexts/LocaleContext'
import { useMemo } from 'react'

type Props = {
  onLoggedIn: (accessToken: string) => void
}

export function BootstrapPage({ onLoggedIn }: Props) {
  const { t, locale } = useLocale()

  const consoles = useMemo<ConsoleTarget[]>(() => {
    const consoleLiteUrl = import.meta.env.VITE_CONSOLE_LITE_URL || 'http://localhost:19000'
    return [
      { name: t.bootstrap.consoleLabel, description: t.bootstrap.consoleDescription, url: '/dashboard', current: true },
      { name: t.bootstrap.consoleLiteLabel, description: t.bootstrap.consoleLiteDescription, url: consoleLiteUrl },
    ]
  }, [t])

  return <SharedBootstrapPage onLoggedIn={onLoggedIn} t={t} locale={locale} consoles={consoles} />
}
