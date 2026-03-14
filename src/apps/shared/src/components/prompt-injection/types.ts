export const SETTING_KEYS = {
  REGEX_ENABLED: 'security.injection_scan.regex_enabled',
  TRUST_SOURCE_ENABLED: 'security.injection_scan.trust_source_enabled',
  SEMANTIC_ENABLED: 'security.injection_scan.semantic_enabled',
  BLOCKING_ENABLED: 'security.injection_scan.blocking_enabled',
  TOOL_SCAN_ENABLED: 'security.injection_scan.tool_output_scan_enabled',
  SEMANTIC_PROVIDER: 'security.semantic_scanner.provider',
  SEMANTIC_API_ENDPOINT: 'security.semantic_scanner.api_endpoint',
  SEMANTIC_API_KEY: 'security.semantic_scanner.api_key',
} as const

export const AUDIT_ACTION = 'security.injection_detected'
export const AUDIT_PAGE_SIZE = 30

export type LayerNameKey =
  | 'layerRegex'
  | 'layerSemantic'
  | 'layerTrustSource'
  | 'layerBlocking'
  | 'layerToolScan'

export type LayerDescKey =
  | 'layerRegexDesc'
  | 'layerSemanticDesc'
  | 'layerTrustSourceDesc'
  | 'layerBlockingDesc'
  | 'layerToolScanDesc'

export interface Layer {
  id: string
  nameKey: LayerNameKey
  descKey: LayerDescKey
  settingsKey: string
  defaultEnabled?: boolean
}

export const LAYERS: Layer[] = [
  { id: 'regex', nameKey: 'layerRegex', descKey: 'layerRegexDesc', settingsKey: SETTING_KEYS.REGEX_ENABLED },
  { id: 'trust-source', nameKey: 'layerTrustSource', descKey: 'layerTrustSourceDesc', settingsKey: SETTING_KEYS.TRUST_SOURCE_ENABLED },
  { id: 'semantic', nameKey: 'layerSemantic', descKey: 'layerSemanticDesc', settingsKey: SETTING_KEYS.SEMANTIC_ENABLED },
  { id: 'blocking', nameKey: 'layerBlocking', descKey: 'layerBlockingDesc', settingsKey: SETTING_KEYS.BLOCKING_ENABLED, defaultEnabled: false },
  { id: 'tool-scan', nameKey: 'layerToolScan', descKey: 'layerToolScanDesc', settingsKey: SETTING_KEYS.TOOL_SCAN_ENABLED },
]

export type Tab = 'layers' | 'audit'
export const TABS: Tab[] = ['layers', 'audit']

export interface AuditLog {
  id: string
  account_id?: string
  actor_user_id?: string
  action: string
  target_type?: string
  target_id?: string
  trace_id: string
  metadata: Record<string, unknown>
  ip_address?: string
  user_agent?: string
  created_at: string
}

// 共享组件需要的翻译文案接口
export interface PromptInjectionTexts {
  tabLayers: string
  tabAudit: string
  layerRegex: string
  layerSemantic: string
  layerTrustSource: string
  layerBlocking: string
  layerToolScan: string
  layerRegexDesc: string
  layerSemanticDesc: string
  layerTrustSourceDesc: string
  layerBlockingDesc: string
  layerToolScanDesc: string
  statusEnabled: string
  statusDisabled: string
  statusNotConfigured: string
  statusPendingInstall: string
  actionEnable: string
  actionDisable: string
  actionConfigure: string
  actionReconfigure: string
  actionSave: string
  actionInstallModel: string
  semanticProviderLocal: string
  semanticProviderApi: string
  semanticLocalDesc: string
  semanticModelVariant: string
  semanticModel22m: string
  semanticModel86m: string
  semanticBridgeRequired: string
  semanticApiDesc: string
  semanticApiEndpointHint: string
  semanticApiKeyHint: string
  semanticInstallStarted: string
  auditEmpty: string
  auditColTime: string
  auditColRunId: string
  auditColCount: string
  auditColPatterns: string
  toastUpdated: string
  toastFailed: string
  toastLoadFailed: string
}
