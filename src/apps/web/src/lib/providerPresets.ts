export type ProviderPreset = {
  key: string
  label: string
  provider: string
  openai_api_mode: string | undefined
  base_url: string
  isCustom?: true
  isBrand?: true
}

export const PROVIDER_PRESETS: ProviderPreset[] = [
  // brand providers
  { key: 'openrouter',              label: 'OpenRouter',                provider: 'openai',    openai_api_mode: 'chat_completions', base_url: 'https://openrouter.ai/api/v1',        isBrand: true },
  { key: 'minimax_cn',              label: 'MiniMax CN',                provider: 'anthropic', openai_api_mode: undefined,          base_url: 'https://api.minimaxi.com/anthropic',  isBrand: true },
  { key: 'minimax_global',          label: 'MiniMax Global',            provider: 'anthropic', openai_api_mode: undefined,          base_url: 'https://api.minimax.io/anthropic',    isBrand: true },
  { key: 'zai',                     label: 'z.ai',                      provider: 'openai',    openai_api_mode: 'chat_completions', base_url: 'https://api.z.ai/api/paas/v4',        isBrand: true },
  // protocol presets
  { key: 'openai_responses',        label: 'OpenAI',                    provider: 'openai',    openai_api_mode: 'responses',        base_url: 'https://api.openai.com/v1'            },
  { key: 'openai_chat_completions', label: 'OpenAI (Chat Completions)', provider: 'openai',    openai_api_mode: 'chat_completions', base_url: 'https://api.openai.com/v1'            },
  { key: 'anthropic',               label: 'Anthropic API',             provider: 'anthropic', openai_api_mode: undefined,          base_url: 'https://api.anthropic.com/v1'         },
  // custom
  { key: 'custom',                  label: 'Custom',                    provider: 'openai',    openai_api_mode: 'chat_completions', base_url: '',                         isCustom: true },
]

export const RECOMMENDED_PATTERNS: Record<string, string[]> = {
  openrouter:              [],
  minimax_cn:              [],
  minimax_global:          [],
  zai:                     [],
  openai_responses:        ['gpt-4o', 'gpt-4o-mini'],
  openai_chat_completions: ['gpt-4o', 'gpt-4o-mini'],
  anthropic:               ['claude-3-5-sonnet', 'claude-3-5-haiku', 'claude-3-haiku'],
  custom:                  [],
}
