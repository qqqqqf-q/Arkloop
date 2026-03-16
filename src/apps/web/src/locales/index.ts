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
  recentsEmpty: string
  untitled: string
  loading: string
  enterprisePlan: string
  // settings nav
  nav: {
    account: string
    settings: string
    skills: string
    credits: string
    models: string
    agents: string
  }
  // settings
  getHelp: string
  submitSuggestion: string
  suggestionTitle: string
  suggestionPlaceholder: string
  suggestionSubmit: string
  suggestionSuccess: string
  comingSoon: string
  requestFailed: string
  logout: string
  language: string
  appearance: string
  themeSystem: string
  themeLight: string
  themeDark: string
  skills: {
    title: string
    subtitle: string
    searchPlaceholder: string
    searchPlaceholderMarketplace: string
    registryTab: string
    localTab: string
    officialOnly: string
    officialOnlyShort: string
    officialUnconfigured: string
    marketUnconfiguredTitle: string
    marketUnconfiguredBody: string
    marketLoading: string
    add: string
    addFromUpload: string
    addFromSkillsmp: string
    addFromSkillsmpDesc: string
    addFromGitHub: string
    addFromGitHubDesc: string
    createWithArkloop: string
    createWithArkloopHint: string
    uploadTitle: string
    githubTitle: string
    officialImportTitle: string
    localSectionTitle: string
    localSectionDesc: string
    emptyTitle: string
    emptyDesc: string
    emptyBodyNoMarket: string
    sourceOfficial: string
    sourceCustom: string
    sourceGitHub: string
    sourcePlatform: string
    sourceBuiltin: string
    installed: string
    notInstalled: string
    enabledByDefault: string
    install: string
    installing: string
    remove: string
    removing: string
    update: string
    viewDetail: string
    more: string
    updatedAt: (value: string) => string
    importFailed: string
    repositoryMissing: string
    loadFailed: string
    officialSearchFailed: string
    uploadFileLabel: string
    uploadFileHint: string
    uploadImmediateInstall: string
    uploadAction: string
    uploading: string
    githubUrlLabel: string
    githubRefLabel: string
    githubAction: string
    githubInvalidUrl: string
    githubSkillNotFound: string
    importing: string
    importOfficialAction: string
    noResults: string
    searchResults: (count: number) => string
    deleteConflict: string
    candidatesTitle: string
    chooseCandidate: string
    installSuccess: (name: string) => string
    removeSuccess: (name: string) => string
    updateSuccess: (name: string) => string
    importSuccess: (name: string) => string
    updateFailed: string
    updateBadge: string
    descriptionFallback: string
    githubLabel: string
    githubUrlRequired: string
    githubUrlPlaceholder: string
    githubRefPlaceholder: string
    githubDialogTitle: string
    githubDialogSubtitle: string
    uploadDialogTitle: string
    uploadDialogSubtitle: string
    uploadArchive: string
    uploadArchiveDesc: string
    uploadFolder: string
    uploadFolderDesc: string
    uploadSelect: string
    uploadSelected: (count: number) => string
    officialDialogTitle: string
    officialDialogSubtitle: string
    officialDialogHintTitle: string
    officialDialogHintBody: string
    scanStatusLabel: (status: string) => string
    riskConfirm: (name: string, status: string, summary?: string) => string
    cancelAction: string
    trySkill: string
    download: string
    replace: string
    disable: string
    disableFailed: string
    detailTitle: string
    trySkillPrompt: (skillKey: string) => string
    detailVersion: string
    detailSource: string
    detailUpdatedAt: string
    detailDescription: string
    noDescription: string
    builtinTab: string
    builtinTitle: string
    builtinEmpty: string
    restore: string
    backToSkills: string
  }
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
  creditsHistoryDetails: string
  creditsHistoryDate: string
  creditsHistoryCreditChange: string
  creditsHistoryEmpty: string
  creditsHistoryRecent: string
  creditsHistoryMonthly: string
  creditsTxTypeLabel: (type: string) => string
  // auth
  loginMode: string
  registerMode: string
  identityPlaceholder: string
  enterUsername: string
  enterEmail: string
  enterPassword: string
  enterInviteCode: string
  enterInviteCodeOptional: string
  editProfile: string
  continueBtn: string
  orDivider: string
  githubLogin: string
  noAccount: string
  hasAccount: string
  useEmailOtpHint: string
  creatingAccountHint: string
  enterYourPasswordTitle: string
  fieldIdentity: string
  fieldPassword: string
  registerPasswordHint: string
  backBtn: string
  editIdentity: string
  // profile editing
  profileTitle: string
  profileName: string
  profileUsername: string
  profileUserId: string
  profileSave: string
  profileEmail: string
  emailUnverified: string
  emailVerified: string
  emailVerifySend: string
  emailVerifySent: string
  emailVerifyCodePlaceholder: string
  emailVerifyConfirmBtn: string
  emailVerifySuccess: string
  emailVerifyFailed: string
  emailVerifyGoToApp: string
  // EmailVerificationGate
  emailGateTitle: string
  emailGateDesc: (email: string) => string
  emailGateResend: string
  emailGateResent: string
  emailGateUseOtp: string
  emailGateAlreadyVerified: string
  otpLoginTab: string
  passwordLoginTab: string
  otpEmailPlaceholder: string
  otpCodePlaceholder: string
  otpSendBtn: string
  otpSendingCountdown: (s: number) => string
  otpVerifyBtn: string
  emailNotVerifiedHint: string
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
  learningMode: string
  dragToAttach: string
  // chats search modal
  searchChatsPlaceholder: string
  searchNoResults: string
  searchToday: string
  searchYesterday: string
  searchLastWeek: string
  searchEarlier: string
  // incognito
  incognitoMode: string
  incognitoHistoryNote: string
  youAreIncognito: string
  enableIncognito: string
  disableIncognito: string
  incognitoThreadNote: string
  incognitoLabel: string
  thisThreadIsIncognito: string
  toggleIncognito: string
  incognitoForkDivider: string
  // thread context menu
  starThread: string
  unstarThread: string
  shareThread: string
  renameThread: string
  deleteThread: string
  deleteThreadConfirmTitle: string
  deleteThreadConfirmBody: string
  deleteThreadConfirm: string
  deleteThreadCancel: string
  // share
  shareTitle: string
  sharePublic: string
  sharePassword: string
  sharePasswordPlaceholder: string
  shareCreate: string
  shareCreating: string
  shareCopyLink: string
  shareCopied: string
  shareRevoke: string
  shareRevoking: string
  shareRevokeConfirm: string
  shareCurrentLink: string
  shareNoLink: string
  shareLiveUpdate: string
  shareFrozen: string
  shareTurnCount: (n: number) => string
  shareCreateNew: string
  shareListEmpty: string
  shareLinkCopied: string
  // share page
  sharePageLogin: string
  sharePageRegister: string
  sharePagePasswordTitle: string
  sharePagePasswordPlaceholder: string
  sharePagePasswordSubmit: string
  sharePagePasswordWrong: string
  sharePageNotFound: string
  sharePagePoweredBy: string
  // report
  reportButton: string
  reportTitle: string
  reportSubtitle: string
  reportInaccurate: string
  reportOutOfDate: string
  reportTooShort: string
  reportTooLong: string
  reportHarmful: string
  reportWrongSources: string
  reportFeedbackPlaceholder: string
  reportFeedbackLabel: string
  reportSubmit: string
  reportSubmitting: string
  reportCancel: string
  reportSuccess: string
  // shell execution
  shellRan: string
  shellRanCommand: string
  shellSuccess: string
  shellFailed: string
  shellCompleted: string
  shellNoOutput: string
  // sub-agent
  agentSubAgent: string
  agentSpawning: string
  agentRunning: string
  agentCompleted: string
  agentFailed: string
  agentClosed: string
  agentInput: string
  agentOutput: string
  agentNoOutput: string
  // pasted content
  pastedContent: string
  pastedLines: (n: number) => string
  // user input card
  userInput: {
    dismiss: string
    submit: string
    submitting: string
    next: string
    back: string
  }
  // document panel
  documentPanel: {
    preview: string
    source: string
    download: string
    previewUnsupported: string
    downloadFile: string
    loadFailed: (detail: string) => string
  }
  // message actions
  copyAction: string
  editAction: string
  // model config
  models: {
    addProvider: string
    editProvider: string
    deleteProvider: string
    deleteProviderConfirm: string
    providerName: string
    providerVendor: string
    apiKey: string
    apiKeyPlaceholder: string
    baseUrl: string
    baseUrlPlaceholder: string
    save: string
    saving: string
    cancel: string
    noProviders: string
    noProvidersDesc: string
    modelsSection: string
    importAll: string
    importing: string
    noModels: string
    deleteModel: string
    addModel: string
    modelName: string
    modelNamePlaceholder: string
    loadFailed: string
    saveFailed: string
    deleteFailed: string
    vendorOpenaiResponses: string
    vendorOpenaiChatCompletions: string
    vendorAnthropicMessage: string
    applyToAll: string
    applyingToAll: string
    applyDone: string
    deleteAll: string
  }
  // agent settings
  agentSettings: {
    title: string
    subtitle: string
    reasoningMode: string
    temperature: string
    maxOutputTokens: string
    noPersonas: string
    saveFailed: string
    resetAll: string
    resetDone: string
    reasoningModes: {
      default: string
      enabled: string
      disabled: string
    }
  }
  // model picker (chat input)
  modelPicker: {
    defaultLabel: string
    addApiKey: string
    byokSection: string
    noByok: string
  }
  // claw right panel
  claw: {
    progress: string
    progressEmpty: string
    workingFolder: string
    workingFolderEmpty: string
    workingFolderLoading: string
    workingFolderError: string
    workingFolderEmptyDir: string
    workingFolderSelectFile: string
    context: string
    contextDesc: string
    contextEmpty: string
    stepComplete: string
    stepInProgress: string
    stepPending: string
    toolsCalled: string
    filesReferenced: string
    agents: string
    agentsEmpty: string
    agentSpawning: string
    agentRunning: string
    agentCompleted: string
    agentFailed: string
    agentClosed: string
  }
  // chat page
  checkInPlaceholder: string
  replyPlaceholder: string
}

export const locales: Record<Locale, LocaleStrings> = { zh, en }

export const SUPPORTED_LOCALES: Locale[] = ['zh', 'en']
