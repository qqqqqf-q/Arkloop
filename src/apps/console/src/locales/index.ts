import { zh } from './zh'
import { en } from './en'

export type Locale = 'zh' | 'en'

export interface LocaleStrings {
  // nav group labels
  groups: {
    operations: string
    configuration: string
    integration: string
    security: string
    organization: string
    billing: string
    platform: string
  }
  // nav item labels
  nav: {
    runs: string
    auditLogs: string
    credentials: string
    agentConfigs: string
    promptTemplates: string
    mcpConfigs: string
    skills: string
    apiKeys: string
    webhooks: string
    ipRules: string
    members: string
    teams: string
    projects: string
    plans: string
    subscriptions: string
    entitlements: string
    usage: string
    featureFlags: string
  }
  // settings
  account: string
  settings: string
  language: string
  appearance: string
  themeSystem: string
  themeLight: string
  themeDark: string
  platformAdmin: string
  signOut: string
  // access denied
  accessDenied: string
  noAdminAccess: string
  // auth
  username: string
  password: string
  signIn: string
  // common
  loading: string
  // pages
  pages: {
    credentials: {
      title: string
      addCredential: string
      // table columns
      colName: string
      colProvider: string
      colKeyPrefix: string
      colBaseUrl: string
      colApiMode: string
      colRoutes: string
      colCreatedAt: string
      // empty state
      empty: string
      // create modal
      modalTitle: string
      fieldName: string
      fieldProvider: string
      fieldApiKey: string
      fieldBaseUrl: string
      fieldApiMode: string
      fieldRoutes: string
      // route row
      routeModel: string
      routePriority: string
      routeDefault: string
      routeWhen: string
      addRoute: string
      // buttons
      cancel: string
      create: string
      // delete dialog
      deleteTitle: string
      deleteMessage: (name: string) => string
      deleteConfirm: string
      // errors
      errRequired: string
      errInvalidJson: (model: string) => string
      errEncryptionKey: string
      // toasts
      toastCreated: string
      toastDeleted: string
      toastLoadFailed: string
      toastDeleteFailed: string
    }
  }
}

export const locales: Record<Locale, LocaleStrings> = { zh, en }
