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
    agentConfigs: {
      title: string
      addConfig: string
      // table columns
      colName: string
      colModel: string
      colTemperature: string
      colMaxOutputTokens: string
      colToolPolicy: string
      colIsDefault: string
      colProject: string
      colCreatedAt: string
      // empty state
      empty: string
      // create/edit modal
      modalTitleCreate: string
      modalTitleEdit: string
      fieldName: string
      fieldSystemPromptTemplate: string
      fieldSystemPromptTemplateNone: string
      fieldSystemPromptOverride: string
      fieldModel: string
      fieldTemperature: string
      fieldMaxOutputTokens: string
      fieldTopP: string
      fieldToolPolicy: string
      fieldToolAllowlist: string
      fieldToolDenylist: string
      fieldContentFilterLevel: string
      fieldIsDefault: string
      // buttons
      cancel: string
      create: string
      save: string
      // delete dialog
      deleteTitle: string
      deleteMessage: (name: string) => string
      deleteConfirm: string
      // errors
      errRequired: string
      // toasts
      toastCreated: string
      toastUpdated: string
      toastDeleted: string
      toastLoadFailed: string
      toastSaveFailed: string
      toastDeleteFailed: string
    }
    promptTemplates: {
      title: string
      addTemplate: string
      // table columns
      colName: string
      colIsDefault: string
      colVersion: string
      colVariablesCount: string
      colCreatedAt: string
      // empty state
      empty: string
      // create/edit modal
      modalTitleCreate: string
      modalTitleEdit: string
      fieldName: string
      fieldContent: string
      fieldVariables: string
      fieldIsDefault: string
      // buttons
      cancel: string
      create: string
      save: string
      // delete dialog
      deleteTitle: string
      deleteMessage: (name: string) => string
      deleteConfirm: string
      // errors
      errRequired: string
      // toasts
      toastCreated: string
      toastUpdated: string
      toastDeleted: string
      toastLoadFailed: string
      toastSaveFailed: string
      toastDeleteFailed: string
    }
    mcpConfigs: {
      title: string
      addConfig: string
      // table columns
      colName: string
      colTransport: string
      colActive: string
      colCreatedAt: string
      // empty state
      empty: string
      // create/edit modal
      modalTitleCreate: string
      modalTitleEdit: string
      fieldName: string
      fieldTransport: string
      fieldUrl: string
      fieldBearerToken: string
      fieldBearerTokenPlaceholder: string
      fieldBearerTokenSet: string
      fieldCommand: string
      fieldArgs: string
      fieldIsActive: string
      // buttons
      cancel: string
      create: string
      save: string
      // delete dialog
      deleteTitle: string
      deleteMessage: (name: string) => string
      deleteConfirm: string
      // errors
      errRequired: string
      errUrlRequired: string
      errCommandRequired: string
      // toasts
      toastCreated: string
      toastUpdated: string
      toastDeleted: string
      toastLoadFailed: string
      toastSaveFailed: string
      toastDeleteFailed: string
    }
    skills: {
      title: string
      addSkill: string
      // table columns
      colSkillKey: string
      colDisplayName: string
      colVersion: string
      colActive: string
      colCreatedAt: string
      // empty state
      empty: string
      // create/edit modal
      modalTitleCreate: string
      modalTitleEdit: string
      fieldSkillKey: string
      fieldVersion: string
      fieldDisplayName: string
      fieldDescription: string
      fieldPrompt: string
      fieldToolAllowlist: string
      fieldToolAllowlistPlaceholder: string
      fieldBudgetsJSON: string
      fieldIsActive: string
      // read-only label for global skills
      labelGlobal: string
      // buttons
      cancel: string
      create: string
      save: string
      // errors
      errRequired: string
      errInvalidJSON: string
      // toasts
      toastCreated: string
      toastUpdated: string
      toastLoadFailed: string
      toastSaveFailed: string
    }
    apiKeys: {
      title: string
      addKey: string
      // table columns
      colKeyPrefix: string
      colName: string
      colScopes: string
      colLastUsedAt: string
      colStatus: string
      colCreatedAt: string
      // status badge labels
      statusActive: string
      statusRevoked: string
      // empty state
      empty: string
      // create modal
      modalTitleCreate: string
      fieldName: string
      fieldScopes: string
      fieldScopesPlaceholder: string
      // key reveal modal
      modalTitleKeyCreated: string
      keyRevealNote: string
      copyKey: string
      copied: string
      done: string
      // revoke dialog
      revokeTitle: string
      revokeMessage: (prefix: string) => string
      revokeConfirm: string
      // buttons
      cancel: string
      create: string
      // errors
      errRequired: string
      // toasts
      toastCreated: string
      toastRevoked: string
      toastLoadFailed: string
      toastCreateFailed: string
      toastRevokeFailed: string
    }
    ipRules: {
      title: string
      addRule: string
      // table columns
      colType: string
      colCIDR: string
      colNote: string
      colCreatedAt: string
      // badge labels
      typeAllowlist: string
      typeBlocklist: string
      // empty state
      empty: string
      // create modal
      modalTitleCreate: string
      fieldType: string
      typeOptionAllowlist: string
      typeOptionBlocklist: string
      fieldCIDR: string
      fieldCIDRPlaceholder: string
      fieldNote: string
      fieldNotePlaceholder: string
      // delete dialog
      deleteTitle: string
      deleteMessage: (cidr: string) => string
      deleteConfirm: string
      // buttons
      cancel: string
      create: string
      // errors
      errRequired: string
      // toasts
      toastCreated: string
      toastDeleted: string
      toastLoadFailed: string
      toastCreateFailed: string
      toastDeleteFailed: string
    }
    teams: {
      title: string
      addTeam: string
      // table columns
      colName: string
      colMembersCount: string
      colCreatedAt: string
      // empty state
      empty: string
      // create modal
      modalTitleCreate: string
      fieldName: string
      // members section
      addMember: string
      colUserId: string
      colRole: string
      colMemberCreatedAt: string
      emptyMembers: string
      // add member modal
      addMemberTitle: string
      fieldUserId: string
      fieldRole: string
      // remove member dialog
      removeTitle: string
      removeMessage: (userId: string) => string
      removeConfirm: string
      // delete team dialog
      deleteTitle: string
      deleteMessage: (name: string) => string
      deleteConfirm: string
      // buttons
      cancel: string
      create: string
      // errors
      errRequired: string
      errRequiredMember: string
      // toasts
      toastCreated: string
      toastDeleted: string
      toastLoadFailed: string
      toastCreateFailed: string
      toastDeleteFailed: string
      toastMemberAdded: string
      toastMemberAddFailed: string
      toastMemberRemoved: string
      toastMemberRemoveFailed: string
      toastMembersLoadFailed: string
    }
    usage: {
      title: string
      labelOrgId: string
      placeholderOrgId: string
      labelYear: string
      labelMonth: string
      queryButton: string
      cardInputTokens: string
      cardOutputTokens: string
      cardCostUSD: string
      cardRecordCount: string
      emptyHint: string
      // toasts
      toastLoadFailed: string
      // errors
      errOrgIdRequired: string
    }
  }
}

export const locales: Record<Locale, LocaleStrings> = { zh, en }
