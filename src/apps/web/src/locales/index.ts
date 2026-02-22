import { zh } from './zh'
import { en } from './en'

export type Locale = 'zh' | 'en'

export interface LocaleStrings {
  // sidebar
  newChat: string
  chats: string
  projects: string
  retrieve: string
  legal: string
  recents: string
  untitled: string
  loading: string
  enterprisePlan: string
  // settings nav
  nav: {
    account: string
    settings: string
    usage: string
    scheduled: string
    mail: string
    data: string
    browser: string
    personal: string
    skills: string
    connectors: string
    integrations: string
  }
  // settings
  getHelp: string
  comingSoon: string
  logout: string
  language: string
  appearance: string
  themeSystem: string
  themeLight: string
  themeDark: string
  // auth
  loginMode: string
  registerMode: string
  enterDisplayName: string
  enterUsername: string
  enterPassword: string
  continueBtn: string
  orDivider: string
  githubLogin: string
  noAccount: string
  hasAccount: string
  // common
  requestFailed: string
  newChatTitle: string
  chatPlaceholder: string
}

export const locales: Record<Locale, LocaleStrings> = { zh, en }

export const SUPPORTED_LOCALES: Locale[] = ['zh', 'en']
