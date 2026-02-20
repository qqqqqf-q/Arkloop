import { useOutletContext } from 'react-router-dom'
import type { ConsoleOutletContext } from '../layouts/ConsoleLayout'

export function ProvidersPage() {
  useOutletContext<ConsoleOutletContext>()

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <header className="flex min-h-[46px] items-center border-b border-[#2a2a28] px-6">
        <h2 className="text-sm font-medium text-[#c2c0b6]">Providers</h2>
      </header>
      <div className="flex flex-1 items-center justify-center">
        <p className="text-sm text-[#6b6b68]">P44</p>
      </div>
    </div>
  )
}
