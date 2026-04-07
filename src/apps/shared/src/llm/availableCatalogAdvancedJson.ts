/**
 * 与 worker/API 约定：路由 advanced_json 里嵌一份可用模型列表的标准化快照，
 * 新增能力只读此处，避免每来一个字段改一次前后端。
 *
 * Worker compact 只读快照内的 context_length。
 */
export const AVAILABLE_CATALOG_ADVANCED_KEY = 'available_catalog'

export type AvailableModelCatalogInput = {
  id: string
  name: string
  type?: string | null
  context_length?: number | null
  context_length_override?: number | null
  max_output_tokens?: number | null
  tool_calling?: boolean | null
  reasoning?: boolean | null
  default_temperature?: number | null
  input_modalities?: string[] | null
  output_modalities?: string[] | null
}

/** 从 GET available-models 的单条结果生成写入 llm_route.advanced_json 的对象。 */
export function routeAdvancedJsonFromAvailableCatalog(am: AvailableModelCatalogInput): Record<string, unknown> {
  const cat: Record<string, unknown> = {
    id: am.id,
    name: am.name,
  }
  if (am.type != null && String(am.type).trim() !== '') cat.type = am.type
  if (am.context_length != null) cat.context_length = am.context_length
  if (am.max_output_tokens != null) cat.max_output_tokens = am.max_output_tokens
  if (am.tool_calling === true) cat.tool_calling = true
  if (am.reasoning === true) cat.reasoning = true
  if (am.default_temperature != null) cat.default_temperature = am.default_temperature
  if (am.input_modalities != null && am.input_modalities.length > 0) {
    cat.input_modalities = [...am.input_modalities]
  }
  if (am.output_modalities != null && am.output_modalities.length > 0) {
    cat.output_modalities = [...am.output_modalities]
  }
  return {
    [AVAILABLE_CATALOG_ADVANCED_KEY]: cat,
  }
}

export function getAvailableCatalogFromAdvancedJson(
  advancedJSON?: Record<string, unknown> | null,
): Record<string, unknown> | null {
  if (!advancedJSON) return null
  const raw = advancedJSON[AVAILABLE_CATALOG_ADVANCED_KEY]
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return null
  return { ...(raw as Record<string, unknown>) }
}

export function stripAvailableCatalogFromAdvancedJson(
  advancedJSON?: Record<string, unknown> | null,
): Record<string, unknown> {
  if (!advancedJSON) return {}
  const next: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(advancedJSON)) {
    if (key === AVAILABLE_CATALOG_ADVANCED_KEY) continue
    next[key] = value
  }
  return next
}

export function mergeAvailableCatalogIntoAdvancedJson(
  catalog: Record<string, unknown> | null,
  rest?: Record<string, unknown> | null,
): Record<string, unknown> | null {
  const next: Record<string, unknown> = { ...(rest ?? {}) }
  if (catalog && Object.keys(catalog).length > 0) {
    next[AVAILABLE_CATALOG_ADVANCED_KEY] = catalog
  }
  return Object.keys(next).length > 0 ? next : null
}
