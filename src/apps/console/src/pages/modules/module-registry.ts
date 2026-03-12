import type { ModuleInfo, ModuleCategory } from '../../api/bridge'

export type ModuleCategoryTab = 'all' | ModuleCategory

export const CATEGORY_TABS: ModuleCategoryTab[] = [
  'all', 'memory', 'sandbox', 'search', 'browser', 'console',
]

const mod = (
  id: string,
  name: string,
  description: string,
  category: ModuleCategory,
  opts?: Partial<Pick<ModuleInfo, 'port' | 'depends_on' | 'mutually_exclusive' | 'capabilities'>>,
): ModuleInfo => ({
  id,
  name,
  description,
  category,
  status: 'not_installed',
  port: opts?.port,
  depends_on: opts?.depends_on ?? [],
  mutually_exclusive: opts?.mutually_exclusive ?? [],
  capabilities: {
    installable: true,
    configurable: true,
    healthcheck: true,
    bootstrap_supported: false,
    external_admin_supported: false,
    privileged_required: false,
    ...opts?.capabilities,
  },
})

export const STATIC_MODULES: ModuleInfo[] = [
  mod('openviking', 'OpenViking', 'Embedding + vector store memory system', 'memory', {
    port: 1933,
    capabilities: { installable: true, configurable: true, healthcheck: true, bootstrap_supported: true, external_admin_supported: true, privileged_required: false },
  }),
  mod('sandbox-docker', 'Sandbox (Docker)', 'Docker-based code execution sandbox', 'sandbox', {
    port: 8002,
    mutually_exclusive: ['sandbox-firecracker'],
    capabilities: { installable: true, configurable: true, healthcheck: true, bootstrap_supported: true, external_admin_supported: false, privileged_required: false },
  }),
  mod('searxng', 'SearXNG', 'Self-hosted metasearch engine', 'search', {
    port: 8888,
    capabilities: { installable: true, configurable: true, healthcheck: true, bootstrap_supported: true, external_admin_supported: false, privileged_required: false },
  }),
  mod('firecrawl', 'Firecrawl', 'Self-hosted web scraper and crawler', 'search', {
    port: 3002,
    capabilities: { installable: true, configurable: true, healthcheck: true, bootstrap_supported: true, external_admin_supported: false, privileged_required: false },
  }),
  mod('browser', 'Browser', 'Browser automation for web interaction', 'browser', {
    depends_on: ['sandbox-docker'],
    capabilities: { installable: true, configurable: true, healthcheck: true, bootstrap_supported: false, external_admin_supported: false, privileged_required: false },
  }),
  mod('console', 'Full Console', 'Full management console with ~35 pages', 'console', {
    port: 5174,
    mutually_exclusive: ['console-lite'],
    capabilities: { installable: true, configurable: false, healthcheck: true, bootstrap_supported: false, external_admin_supported: false, privileged_required: false },
  }),
]

export const INSTALL_COMMANDS: Record<string, string> = {
  'openviking': './setup.sh install --memory openviking',
  'sandbox-docker': './setup.sh install --sandbox docker',
  'searxng': './setup.sh install --web-tools self-hosted',
  'firecrawl': './setup.sh install --web-tools self-hosted',
  'browser': './setup.sh install --browser on',
  'console': './setup.sh install --console full',
}

export const AGENT_PROMPTS: Record<string, string> = {
  'openviking': 'Install OpenViking memory system for Arkloop. Run: ./setup.sh install --memory openviking',
  'sandbox-docker': 'Install Docker sandbox for Arkloop code execution. Run: ./setup.sh install --sandbox docker',
  'searxng': 'Install SearXNG self-hosted search for Arkloop. Run: ./setup.sh install --web-tools self-hosted',
  'firecrawl': 'Install Firecrawl web scraper for Arkloop. Run: ./setup.sh install --web-tools self-hosted',
  'browser': 'Install browser automation module for Arkloop. Run: ./setup.sh install --browser on',
  'console': 'Upgrade to full Arkloop console. Run: ./setup.sh install --console full',
}
