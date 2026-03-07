import { zhCN } from './zh-CN'
import { en } from './en'

export type Locale = 'zh' | 'en'

export interface LocaleStrings {
  nav: {
    dashboard: string
    agents: string
    models: string
    tools: string
    memory: string
    runs: string
    settings: string
  }
  dashboard: {
    title: string
    runsTotal: string
    runsToday: string
    inputTokens: string
    outputTokens: string
    tokenUsage30d: string
    refresh: string
  }
  agents: {
    title: string
    newAgent: string
    editAgent: string
    name: string
    model: string
    systemPrompt: string
    tools: string
    setDefault: string
    active: string
    advanced: string
    temperature: string
    maxOutputTokens: string
    reasoningMode: string
    reasoningDisabled: string
    reasoningEnabled: string
    deleteConfirm: string
    noAgents: string
    overview: string
    persona: string
    builtIn: string
    platformDefault: string
    hybrid: string
  }
  common: {
    save: string
    cancel: string
    edit: string
    delete: string
    confirm: string
    loading: string
    default: string
    signOut: string
  }
  // auth
  loginMode: string
  enterYourPasswordTitle: string
  fieldIdentity: string
  fieldPassword: string
  identityPlaceholder: string
  enterPassword: string
  continueBtn: string
  backBtn: string
  editIdentity: string
  useEmailOtpHint: string
  otpLoginTab: string
  otpEmailPlaceholder: string
  otpCodePlaceholder: string
  otpSendBtn: string
  otpSendingCountdown: (s: number) => string
  otpVerifyBtn: string
  requestFailed: string
  loading: string
  // access denied
  accessDenied: string
  noAdminAccess: string
  signOut: string
  // settings modal
  account: string
  settings: string
  language: string
  appearance: string
  themeSystem: string
  themeLight: string
  themeDark: string
}

export const locales: Record<Locale, LocaleStrings> = { zh: zhCN, en }
