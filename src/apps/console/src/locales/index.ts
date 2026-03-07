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
    dashboard: string
    runs: string
    auditLogs: string
    credentials: string
    toolProviders: string
    agentConfigs: string
    promptTemplates: string
    mcpConfigs: string
    personas: string
    apiKeys: string
    webhooks: string
    ipRules: string
    captcha: string
    gatewayConfig: string
    accessLog: string
    members: string
    teams: string
    projects: string
    plans: string
    subscriptions: string
    entitlements: string
    usage: string
    myUsage: string
    featureFlags: string
    users: string
    registration: string
    inviteCodes: string
    redemptionCodes: string
    creditsAdmin: string
    broadcasts: string
    asrCredentials: string
    email: string
    titleSummarizer: string
    sandboxConfig: string
    memoryConfig: string
    executionGovernance: string
    reports: string
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
  requestFailed: string
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
      fieldAdvancedJson: string
      fieldRoutes: string
      // route row
      routeModel: string
      routePriority: string
      routeDefault: string
      routeWhen: string
      routeMultiplier: string
      routeCostInput: string
      routeCostOutput: string
      routeCostCacheWrite: string
      routeCostCacheRead: string
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
      // edit cred modal
      fieldApiKeyOptional: string
      editRoutesTitle: string
      editRoutesSave: string
      editCredTitle: string
      editCredSave: string
      toastCredUpdated: string
      toastCredUpdateFailed: string
      toastRouteUpdated: string
      toastRouteUpdateFailed: string
      copyTitle: string
      toastCopied: string
      toastCopyFailed: string
    }
    toolProviders: {
      title: string
      fieldScope: string
      colProvider: string
      colStatus: string
      colKeyPrefix: string
      colBaseUrl: string
      statusActive: string
      statusInactive: string
      statusUnconfigured: string
      activate: string
      deactivate: string
      configure: string
      clearCredential: string
      modalTitle: string
      fieldApiKey: string
      fieldBaseUrl: string
      fieldBaseUrlOptional: string
      currentKeyPrefix: string
      errApiKeyRequired: string
      errBaseUrlRequired: string
      cancel: string
      save: string
      clearTitle: string
      clearMessage: (providerName: string) => string
      clearConfirm: string
      toastLoadFailed: string
      toastUpdated: string
      toastUpdateFailed: string
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
      fieldPromptCacheControl: string
      fieldReasoningMode: string
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
    personas: {
      title: string
      addPersona: string
      // table columns
      colPersonaKey: string
      colDisplayName: string
      colVersion: string
      colDefaultModel: string
      colActive: string
      colSelectable: string
      colCreatedAt: string
      // empty state
      empty: string
      // create/edit modal
      modalTitleCreate: string
      modalTitleEdit: string
      fieldPersonaKey: string
      fieldVersion: string
      fieldDisplayName: string
      fieldDescription: string
      fieldPrompt: string
      fieldToolAllowlist: string
      fieldToolAllowlistPlaceholder: string
      fieldBudgetsJSON: string
      fieldIsActive: string
      fieldExecutorType: string
      fieldDefaultModel: string
      fieldExecutorConfig: string
      fieldPreferredCredential: string
      selectorMeta: (name: string, order: number) => string
      valuePlatformDefault: string
      // read-only label for global personas
      labelGlobal: string
      labelHybrid: string
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
    gatewayConfig: {
      title: string
      fieldIPMode: string
      ipModeDirect: string
      ipModeCloudflare: string
      ipModeTrustedProxy: string
      fieldTrustedCIDRs: string
      fieldTrustedCIDRsHint: string
      fieldRiskThreshold: string
      fieldRiskThresholdHint: string
      save: string
      toastSaved: string
      toastLoadFailed: string
      toastSaveFailed: string
    }
    titleSummarizer: {
      title: string
      fieldAgent: string
      fieldAgentHint: string
      agentNone: string
      save: string
      toastSaved: string
      toastLoadFailed: string
      toastSaveFailed: string
    }
    accessLog: {
      title: string
      colTimestamp: string
      colIdentity: string
      colIP: string
      colLocation: string
      colUserAgent: string
      colRisk: string
      colMethod: string
      colPath: string
      colStatus: string
      colDuration: string
      empty: string
      emptyFiltered: string
      loadMore: string
      toastLoadFailed: string
      riskLow: string
      riskMedium: string
      riskHigh: string
      riskCritical: string
      identityAnonymous: string
      filterMethod: string
      filterPath: string
      filterIP: string
      filterRiskMin: string
      filterAll: string
      apply: string
      clearFilters: string
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
      queryButton: string
      cardInputTokens: string
      cardOutputTokens: string
      cardCostUSD: string
      cardRecordCount: string
      chartDailyTitle: string
      chartModelTitle: string
      chartNoData: string
      emptyHint: string
      toastLoadFailed: string
    }
    dashboard: {
      title: string
      refresh: string
      cardTotalUsers: string
      cardActiveUsers30d: string
      cardTotalRuns: string
      cardRunsToday: string
      cardInputTokens: string
      cardOutputTokens: string
      cardCostUSD: string
      cardActiveOrgs: string
      toastLoadFailed: string
    }
    myUsage: {
      title: string
      queryButton: string
      cardInputTokens: string
      cardOutputTokens: string
      cardCostUSD: string
      cardRecordCount: string
      cardCreditBalance: string
      chartDailyTitle: string
      chartModelTitle: string
      chartNoData: string
      transactionsTitle: string
      transactionsEmpty: string
      colDate: string
      colType: string
      colAmount: string
      colNote: string
      emptyHint: string
      toastLoadFailed: string
    }
    users: {
      title: string
      colId: string
      colLogin: string
      colEmail: string
      colStatus: string
      colLastLogin: string
      colCreatedAt: string
      statusActive: string
      statusSuspended: string
      searchPlaceholder: string
      filterAll: string
      filterActive: string
      filterSuspended: string
      empty: string
      loadMore: string
      detailTitle: string
      detailId: string
      detailLogin: string
      detailEmail: string
      detailEmailVerified: string
      detailEmailNotVerified: string
      detailLocale: string
      detailTimezone: string
      detailOrgs: string
      detailOrgId: string
      detailOrgRole: string
      detailNoOrgs: string
      suspendTitle: string
      suspendMessage: (name: string) => string
      suspendConfirm: string
      activateTitle: string
      activateMessage: (name: string) => string
      activateConfirm: string
      toastSuspended: string
      toastActivated: string
      toastLoadFailed: string
      toastStatusFailed: string
      toastDetailFailed: string
      editButton: string
      editTitle: string
      editUsername: string
      editEmail: string
      editEmailVerified: string
      editLocale: string
      editTimezone: string
      editCancel: string
      editSave: string
      editErrNameRequired: string
      toastEditSaved: string
      toastEditFailed: string
      creditAdjustButton: string
      creditAdjustTitle: string
      creditAdjustAmount: string
      creditAdjustNote: string
      creditAdjustNotePlaceholder: string
      creditAdjustConfirm: string
      creditAdjustCancel: string
      creditAdjustErrAmount: string
      creditAdjustErrNote: string
      toastCreditAdjusted: string
      toastCreditAdjustFailed: string
      deleteButton: string
      deleteTitle: string
      deleteMessage: (name: string) => string
      deleteConfirm: string
      toastDeleted: string
      toastDeleteFailed: string
    }
    inviteCodes: {
      title: string
      searchPlaceholder: string
      colId: string
      colCode: string
      colUser: string
      colEmail: string
      colMaxUses: string
      colUseCount: string
      colStatus: string
      colCreatedAt: string
      statusActive: string
      statusInactive: string
      empty: string
      loadMore: string
      // edit modal
      editTitle: string
      editMaxUses: string
      editCancel: string
      editSave: string
      editErrPositive: string
      // deactivate dialog
      deactivateTitle: string
      deactivateMessage: (code: string) => string
      deactivateConfirm: string
      activateTitle: string
      activateMessage: (code: string) => string
      activateConfirm: string
      // referrals
      referralsTitle: string
      referralsEmpty: string
      refColInvitee: string
      refColCredited: string
      refColCreatedAt: string
      refCreditedYes: string
      refCreditedNo: string
      // toasts
      toastLoadFailed: string
      toastUpdated: string
      toastUpdateFailed: string
      toastStatusChanged: string
      toastStatusFailed: string
      toastReferralsFailed: string
    }
    redemptionCodes: {
      title: string
      searchPlaceholder: string
      addBatch: string
      colId: string
      colCode: string
      colType: string
      colValue: string
      colMaxUses: string
      colUseCount: string
      colExpiresAt: string
      colStatus: string
      colBatchId: string
      colCreatedAt: string
      statusActive: string
      statusInactive: string
      typeCredit: string
      typeFeature: string
      filterAllTypes: string
      empty: string
      loadMore: string
      // batch create modal
      batchTitle: string
      fieldCount: string
      fieldType: string
      fieldValue: string
      fieldMaxUses: string
      fieldExpiresAt: string
      fieldBatchId: string
      cancel: string
      create: string
      // deactivate dialog
      deactivateTitle: string
      deactivateMessage: (code: string) => string
      deactivateConfirm: string
      // errors
      errRequired: string
      errCountRange: string
      // toasts
      toastLoadFailed: string
      toastCreated: (count: number) => string
      toastCreateFailed: string
      toastDeactivated: string
      toastDeactivateFailed: string
    }
    creditsAdmin: {
      title: string
      addCard: string
      addDesc: string
      deductCard: string
      deductDesc: string
      resetCard: string
      resetDesc: string
      fieldAmount: string
      fieldNote: string
      fieldNotePlaceholder: string
      submit: string
      submitting: string
      confirmAdd: (amount: number) => string
      confirmDeduct: (amount: number) => string
      confirmReset: string
      toastAddOk: string
      toastDeductOk: string
      toastResetOk: string
      toastFailed: string
    }
    broadcasts: {
      title: string
      addBroadcast: string
      empty: string
      loadMore: string
      // table columns
      colType: string
      colTitle: string
      colTarget: string
      colSentCount: string
      colStatus: string
      colCreatedAt: string
      // target labels
      targetAll: string
      // create modal
      modalTitle: string
      fieldType: string
      fieldTitleZh: string
      fieldTitleEn: string
      fieldBodyZh: string
      fieldBodyEn: string
      fieldTarget: string
      fieldTargetPlaceholder: string
      typeAnnouncement: string
      typeMaintenance: string
      typeUpdate: string
      cancel: string
      create: string
      // errors
      errTitleRequired: string
      // toasts
      toastLoadFailed: string
      toastCreated: string
      toastCreateFailed: string
      // delete
      deleteTitle: string
      deleteMessage: (title: string) => string
      deleteConfirm: string
      toastDeleted: string
      toastDeleteFailed: string
    }
    featureFlags: {
      title: string
      addFlag: string
      colKey: string
      colDescription: string
      colDefaultValue: string
      colCreatedAt: string
      empty: string
      enabled: string
      disabled: string
      // create modal
      modalTitleCreate: string
      fieldKey: string
      fieldDescription: string
      fieldDefaultValue: string
      cancel: string
      create: string
      // delete dialog
      deleteTitle: string
      deleteMessage: (key: string) => string
      deleteConfirm: string
      // org overrides
      orgOverrides: string
      overridesEmpty: string
      addOverride: string
      overrideOrgId: string
      overrideEnabled: string
      deleteOverride: string
      deleteOverrideTitle: string
      deleteOverrideMessage: (orgId: string) => string
      deleteOverrideConfirm: string
      // errors
      errKeyRequired: string
      errOrgIdRequired: string
      // toasts
      toastCreated: string
      toastUpdated: string
      toastDeleted: string
      toastLoadFailed: string
      toastCreateFailed: string
      toastUpdateFailed: string
      toastDeleteFailed: string
      toastOverrideSet: string
      toastOverrideSetFailed: string
      toastOverrideDeleted: string
      toastOverrideDeleteFailed: string
    }
    registration: {
      title: string
      modeLabel: string
      modeOpen: string
      modeInvite: string
      modeOpenDesc: string
      modeInviteDesc: string
      switchToOpen: string
      switchToInvite: string
      inviteCodeTitle: string
      inviteCodeOpenHint: string
      inviteCodeInviteHint: string
      referralTitle: string
      initialGrantLabel: string
      inviteRewardLabel: string
      inviteeRewardLabel: string
      saveSettings: string
      settingsErrPositive: string
      toastLoadFailed: string
      toastUpdated: string
      toastUpdateFailed: string
      toastSettingsSaved: string
      toastSettingsFailed: string
      emailVerifyTitle: string
      emailVerifyDesc: string
      emailVerifyOn: string
      emailVerifyOff: string
      emailVerifyToggleOn: string
      emailVerifyToggleOff: string
      toastEmailVerifyUpdated: string
      toastEmailVerifyFailed: string
    }
    asrCredentials: {
      title: string
      addCredential: string
      colName: string
      colScope: string
      colProvider: string
      colModel: string
      colKeyPrefix: string
      colBaseUrl: string
      colCreatedAt: string
      empty: string
      modalTitle: string
      fieldName: string
      fieldScope: string
      fieldProvider: string
      fieldModel: string
      fieldApiKey: string
      fieldBaseUrl: string
      fieldIsDefault: string
      setDefault: string
      cancel: string
      create: string
      deleteTitle: string
      deleteMessage: (name: string) => string
      deleteConfirm: string
      errRequired: string
      errEncryptionKey: string
      toastCreated: string
      toastDeleted: string
      toastDefaultSet: string
      toastLoadFailed: string
      toastDeleteFailed: string
      toastDefaultFailed: string
    }
    sandboxConfig: {
      title: string
      sectionProvider: string
      sectionPool: string
      sectionTimeout: string
      fieldBaseUrl: string
      fieldProvider: string
      fieldDockerImage: string
      fieldMaxSessions: string
      fieldBootTimeout: string
      fieldRefillInterval: string
      fieldRefillConcurrency: string
      fieldMaxLifetime: string
      save: string
      toastSaved: string
      toastLoadFailed: string
      toastSaveFailed: string
    }
    memoryConfig: {
      title: string
      providerTag: string
      sectionBilling: string
      fieldBaseUrl: string
      fieldApiKey: string
      fieldCostPerCommit: string
      fieldCostPerCommitHint: string
      save: string
      toastSaved: string
      toastLoadFailed: string
      toastSaveFailed: string
    }
    executionGovernance: {
      title: string
      filterTitle: string
      filterPlatformOnly: string
      filterActive: (orgId: string) => string
      fieldOrgId: string
      fieldOrgIdPlaceholder: string
      apply: string
      reset: string
      gotoEntitlements: string
      gotoAgentConfigs: string
      gotoPersonas: string
      limitsTitle: string
      colLimit: string
      colEffective: string
      colSource: string
      colLayers: string
      layerEnv: string
      layerOrg: string
      layerPlatform: string
      layerDefault: string
      agentConfigsTitle: string
      agentConfigsHint: string
      orgDefaultTitle: string
      platformDefaultTitle: string
      defaultEmpty: string
      colAgentConfig: string
      colModel: string
      colScope: string
      colProject: string
      colReasoningMode: string
      agentConfigEmpty: string
      defaultBadge: string
      personasTitle: string
      personasHint: string
      personasPlatformOnly: string
      personaEmpty: string
      colPersona: string
      colRequested: string
      colEffectiveBudget: string
      colResolvedConfig: string
      colSoftLimits: string
      boundAgentConfig: string
      labelReasoningMode: string
      rulesTitle: string
      ruleSources: string
      ruleClamp: string
      toastLoadFailed: string
    }
    captcha: {
      title: string
      statusTitle: string
      statusConfigured: string
      statusNotConfigured: string
      sourceDb: string
      sourceEnv: string
      configTitle: string
      fieldSiteKey: string
      fieldSecretKey: string
      fieldSecretKeySet: string
      fieldSecretKeyPlaceholder: string
      fieldAllowedHost: string
      fieldAllowedHostPlaceholder: string
      fieldAllowedHostHint: string
      save: string
      toastLoadFailed: string
      toastSaved: string
      toastSaveFailed: string
    }
    email: {
      title: string
      statusTitle: string
      statusConfigured: string
      statusNotConfigured: string
      sourceDb: string
      sourceEnv: string
      configTitle: string
      fieldFrom: string
      fieldHost: string
      fieldPort: string
      fieldUser: string
      fieldPass: string
      fieldPassPlaceholder: string
      fieldPassSet: string
      fieldTLSMode: string
      tls_starttls: string
      tls_tls: string
      tls_none: string
      save: string
      testTitle: string
      testPlaceholder: string
      testSend: string
      errInvalidTo: string
      toastLoadFailed: string
      toastSaved: string
      toastSaveFailed: string
      toastSent: string
      toastSendFailed: string
      appUrlTitle: string
      appUrlField: string
      appUrlPlaceholder: string
      appUrlHint: string
      appUrlSaved: string
      appUrlSaveFailed: string
    }
    runs: {
      title: string
      // filters
      filterAll: string
      filterRunning: string
      filterCompleted: string
      filterFailed: string
      filterCancelled: string
      filterRunPlaceholder: string
      filterThreadPlaceholder: string
      filterUserPlaceholder: string
      filterOrgPlaceholder: string
      filterParentRunPlaceholder: string
      filterStatusLabel: string
      filterModelLabel: string
      filterPersonaLabel: string
      filterModelPlaceholder: string
      filterPersonaPlaceholder: string
      filterSinceLabel: string
      filterUntilLabel: string
      applyFilters: string
      resetFilters: string
      filterActiveCount: (count: number) => string
      refresh: string
      // table columns
      colId: string
      colUser: string
      colOrg: string
      colThread: string
      colStatus: string
      colModel: string
      colPersona: string
      colDuration: string
      colTokens: string
      colCost: string
      colCacheHit: string
      colCredits: string
      colCreatedAt: string
      // empty state
      empty: string
      // actions
      cancel: string
      // cancel dialog
      cancelTitle: string
      cancelMessage: (id: string) => string
      cancelConfirm: string
      // toasts
      toastLoadFailed: string
      toastCancelFailed: string
      // pagination
      prev: string
      next: string
      // detail panel
      sectionOverview: string
      sectionUsage: string
      sectionConversation: string
      sectionRawEvents: string
      usageColStage: string
      usageColModel: string
      usageColTokens: string
      usageColCost: string
      usageColCache: string
      usageColRun: string
      usageStageMain: string
      usageStageFinal: string
      usageStageChild: string
      usageTotal: string
      labelUser: string
      labelThread: string
      labelOrg: string
      labelPersona: string
      labelAgentConfig: string
      labelCredential: string
      labelModel: string
      labelTokens: string
      labelCost: string
      labelCacheHit: string
      labelCreditsUsed: string
      labelCreated: string
      labelCompleted: string
      labelFailedAt: string
      loading: string
      noConversation: string
      noEvents: string
      userPrompt: string
    }
    reports: {
      title: string
      colCreatedAt: string
      colReportId: string
      colReporter: string
      colThread: string
      colCategories: string
      colFeedback: string
      filterReportPlaceholder: string
      filterThreadPlaceholder: string
      filterReporterPlaceholder: string
      filterReporterEmailPlaceholder: string
      filterCategoryAll: string
      filterFeedbackPlaceholder: string
      filterSinceLabel: string
      filterUntilLabel: string
      applyFilters: string
      resetFilters: string
      filterActiveCount: (count: number) => string
      gotoRuns: string
      empty: string
      refresh: string
      prev: string
      next: string
      toastLoadFailed: string
    }
  }
}

export const locales: Record<Locale, LocaleStrings> = { zh, en }
