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
    credits: string
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
  // invite code
  inviteCode: string
  inviteCodeDesc: string
  inviteCodeCopy: string
  inviteCodeCopied: string
  inviteCodeReset: string
  inviteCodeResetting: string
  inviteCodeUses: (used: number, max: number) => string
  inviteCodeResetCooldown: string
  // credits
  creditsBalance: string
  creditsBalanceUnit: string
  creditsRedeem: string
  creditsRedeemPlaceholder: string
  creditsRedeemBtn: string
  creditsRedeemSuccess: (value: string) => string
  creditsRedeemError: (code: string) => string
  creditsUsage: string
  creditsUsageQuery: string
  creditsUsageInputTokens: string
  creditsUsageOutputTokens: string
  creditsUsageRuns: string
  creditsUsageEmpty: string
  // auth
  loginMode: string
  registerMode: string
  enterDisplayName: string
  enterUsername: string
  enterPassword: string
  enterInviteCode: string
  continueBtn: string
  orDivider: string
  githubLogin: string
  noAccount: string
  hasAccount: string
  // common
  requestFailed: string
  newChatTitle: string
  chatPlaceholder: string
  // notifications
  notificationsTitle: string
  notificationsEmpty: string
  notificationsMarkRead: string
  // free plan badge
  freePlan: string
  freeTrial: string
  freeTrialDesc: string
  // chat input menu
  addFromLocal: string
  addFromGitHub: string
  // chats search modal
  searchChatsPlaceholder: string
  searchNoResults: string
  searchToday: string
  searchYesterday: string
  searchLastWeek: string
  searchEarlier: string
}

export const locales: Record<Locale, LocaleStrings> = { zh, en }

export const SUPPORTED_LOCALES: Locale[] = ['zh', 'en']
