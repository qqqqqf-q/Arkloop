export type Locale = 'zh' | 'en';
export type SectionId = 'docs' | 'api' | 'research';

export interface SidebarItem {
  label: string;
  href: string;
}

export interface SidebarGroup {
  label: string;
  items: SidebarItem[];
}

export interface NavigationItem {
  id: SectionId;
  label: string;
  href: string;
  locale: Locale;
  children?: NavigationItem[];
  sidebar?: SidebarGroup[];
}

export interface LocaleNavigation {
  top: NavigationItem[];
  docsSidebar: SidebarGroup[];
  apiSidebar: SidebarGroup[];
}

const zhDocsSidebar: SidebarGroup[] = [
  {
    label: '开发指南',
    items: [
      { label: '本地启动', href: '/docs/guide' },
      { label: '部署指南', href: '/docs/guide/deployment' },
      { label: '测试与基准', href: '/docs/guide/testing' },
      { label: 'SaaS 部署指南', href: '/docs/guide/saas-deployment' },
      { label: 'Markdown 渲染测试', href: '/docs/guide/markdown-test' },
    ],
  },
  {
    label: '技术规范',
    items: [
      { label: '后端 API 与 SSE 规范', href: '/docs/specs/api-and-sse' },
      { label: '数据库架构与数据模型', href: '/docs/specs/database-architecture' },
      { label: '日志与可观测性方案', href: '/docs/specs/logging-and-observability' },
      { label: 'Run 执行架构', href: '/docs/specs/run-execution-architecture' },
      { label: 'Console Lite Design Specification', href: '/docs/specs/console-lite' },
      { label: 'Shell Execute 设计方案', href: '/docs/specs/shell-execute-architecture' },
      { label: 'Installer Bridge 设计方案', href: '/docs/specs/installer-bridge-architecture' },
      { label: 'Browser Automation 设计方案', href: '/docs/specs/browser-automation-architecture' },
      { label: 'Claw 设计方案', href: '/docs/specs/claw-architecture' },
      { label: '社交平台 Channel 接入架构', href: '/docs/specs/channel-integration-architecture' },
      { label: 'OpenCode + ACP 接入架构', href: '/docs/specs/opencode-acp-architecture' },
      { label: 'Web App 移动端适配方案', href: '/docs/specs/mobile-adaptation' },
      { label: 'Sub-agent 协作架构', href: '/docs/specs/sub-agent-architecture' },
      { label: 'Platform Agent 架构', href: '/docs/specs/platform-agent-architecture' },
      { label: 'Prompt Injection 防护', href: '/docs/specs/prompt-injection-protection' },
    ],
  },
  {
    label: '参考',
    items: [
      { label: '配置注册表', href: '/docs/reference/configuration' },
    ],
  },
  {
    label: '路线图',
    items: [
      { label: '开源准备度', href: '/docs/roadmap/open-source-readiness-roadmap' },
    ],
  },
];

const enDocsSidebar: SidebarGroup[] = [
  {
    label: 'Guide',
    items: [
      { label: 'Local Setup', href: '/en/docs/guide' },
      { label: 'Deployment Guide', href: '/en/docs/guide/deployment' },
      { label: 'Testing & Benchmarks', href: '/en/docs/guide/testing' },
    ],
  },
  {
    label: 'Specifications',
    items: [
      { label: 'Backend API and SSE Specifications', href: '/en/docs/specs/api-and-sse' },
      { label: 'Database Architecture and Data Models', href: '/en/docs/specs/database-architecture' },
      { label: 'Logging and Observability Strategy', href: '/en/docs/specs/logging-and-observability' },
      { label: 'Run Execution Architecture', href: '/en/docs/specs/run-execution-architecture' },
      { label: 'Browser Automation Architecture', href: '/en/docs/specs/browser-automation-architecture' },
    ],
  },
  {
    label: 'Reference',
    items: [
      { label: 'Configuration Registry', href: '/en/docs/reference/configuration' },
    ],
  },
  {
    label: 'Roadmap',
    items: [
      { label: 'Open Source Readiness', href: '/en/docs/roadmap/open-source-readiness-roadmap' },
    ],
  },
];

const zhApiSidebar: SidebarGroup[] = [
  {
    label: '概览',
    items: [
      { label: 'API 概览', href: '/api' },
      { label: '认证', href: '/api/auth' },
      { label: '当前用户', href: '/api/me' },
      { label: '线程', href: '/api/threads' },
      { label: '消息', href: '/api/messages' },
      { label: '运行', href: '/api/runs' },
      { label: '项目', href: '/api/projects' },
      { label: '组织', href: '/api/orgs' },
    ],
  },
  {
    label: '配置',
    items: [
      { label: 'LLM Providers', href: '/api/llm-providers' },
      { label: 'MCP Configs', href: '/api/mcp-configs' },
      { label: 'ASR Credentials', href: '/api/asr-credentials' },
      { label: 'Tool Providers', href: '/api/tool-providers' },
    ],
  },
  {
    label: '计费与权限',
    items: [
      { label: 'Credits & Usage', href: '/api/credits' },
      { label: 'Subscriptions & Plans', href: '/api/subscriptions' },
      { label: 'Entitlements', href: '/api/entitlements' },
      { label: 'API Keys', href: '/api/api-keys' },
    ],
  },
  {
    label: '通知与 Webhook',
    items: [
      { label: 'Notifications', href: '/api/notifications' },
      { label: 'Webhooks', href: '/api/webhooks' },
    ],
  },
  {
    label: '管理',
    items: [
      { label: 'Admin 总览', href: '/api/admin' },
      { label: '审计日志', href: '/api/audit-logs' },
      { label: 'IP 规则', href: '/api/ip-rules' },
      { label: 'Feature Flags', href: '/api/feature-flags' },
      { label: '健康检查', href: '/api/health' },
    ],
  },
];

const enApiSidebar: SidebarGroup[] = [
  {
    label: 'Overview',
    items: [
      { label: 'API Overview', href: '/en/api' },
      { label: 'Auth', href: '/en/api/auth' },
      { label: 'Current User', href: '/en/api/me' },
      { label: 'Threads', href: '/en/api/threads' },
      { label: 'Messages', href: '/en/api/messages' },
      { label: 'Runs', href: '/en/api/runs' },
      { label: 'Projects', href: '/en/api/projects' },
      { label: 'Organizations', href: '/en/api/orgs' },
    ],
  },
  {
    label: 'Configuration',
    items: [
      { label: 'LLM Providers', href: '/en/api/llm-providers' },
      { label: 'MCP Configs', href: '/en/api/mcp-configs' },
      { label: 'ASR Credentials', href: '/en/api/asr-credentials' },
      { label: 'Tool Providers', href: '/en/api/tool-providers' },
    ],
  },
  {
    label: 'Billing & Entitlements',
    items: [
      { label: 'Credits & Usage', href: '/en/api/credits' },
      { label: 'Subscriptions & Plans', href: '/en/api/subscriptions' },
      { label: 'Entitlements', href: '/en/api/entitlements' },
      { label: 'API Keys', href: '/en/api/api-keys' },
    ],
  },
  {
    label: 'Notifications & Webhooks',
    items: [
      { label: 'Notifications', href: '/en/api/notifications' },
      { label: 'Webhooks', href: '/en/api/webhooks' },
    ],
  },
  {
    label: 'Admin',
    items: [
      { label: 'Admin Overview', href: '/en/api/admin' },
      { label: 'Audit Logs', href: '/en/api/audit-logs' },
      { label: 'IP Rules', href: '/en/api/ip-rules' },
      { label: 'Feature Flags', href: '/en/api/feature-flags' },
      { label: 'Health Checks', href: '/en/api/health' },
    ],
  },
];

export const navigation: Record<Locale, LocaleNavigation> = {
  zh: {
    top: [
      { id: 'docs', label: 'Docs', href: '/docs', locale: 'zh', sidebar: zhDocsSidebar },
      { id: 'api', label: 'API', href: '/api', locale: 'zh', sidebar: zhApiSidebar },
      { id: 'research', label: 'Research', href: '/research', locale: 'zh' },
    ],
    docsSidebar: zhDocsSidebar,
    apiSidebar: zhApiSidebar,
  },
  en: {
    top: [
      { id: 'docs', label: 'Docs', href: '/en/docs', locale: 'en', sidebar: enDocsSidebar },
      { id: 'api', label: 'API', href: '/en/api', locale: 'en', sidebar: enApiSidebar },
      { id: 'research', label: 'Research', href: '/research', locale: 'en' },
    ],
    docsSidebar: enDocsSidebar,
    apiSidebar: enApiSidebar,
  },
};

export function getTopNavigation(locale: Locale) {
  return navigation[locale].top;
}

export function getSidebar(locale: Locale, section: 'docs' | 'api') {
  return navigation[locale][section === 'docs' ? 'docsSidebar' : 'apiSidebar'];
}

export function flattenSidebar(groups: SidebarGroup[]) {
  return groups.flatMap((group) => group.items);
}

export function getPrevNext(locale: Locale, section: 'docs' | 'api', currentPath: string) {
  const items = flattenSidebar(getSidebar(locale, section));
  const index = items.findIndex((item) => item.href === currentPath);
  return {
    prev: index > 0 ? items[index - 1] : null,
    next: index >= 0 && index < items.length - 1 ? items[index + 1] : null,
  };
}

export function getSectionFromPath(pathname: string): SectionId | null {
  if (pathname === '/research' || pathname.startsWith('/research/')) return 'research';
  if (pathname === '/api' || pathname.startsWith('/api/') || pathname === '/en/api' || pathname.startsWith('/en/api/')) return 'api';
  if (pathname === '/docs' || pathname.startsWith('/docs/') || pathname === '/en/docs' || pathname.startsWith('/en/docs/')) return 'docs';
  return null;
}

export function getLocaleSwitchPath(pathname: string): string | null {
  if (pathname === '/') return '/en';
  if (pathname === '/en') return '/';
  if (pathname.startsWith('/docs') || pathname.startsWith('/api')) {
    return `/en${pathname}`;
  }
  if (pathname.startsWith('/en/docs') || pathname.startsWith('/en/api')) {
    return pathname.slice(3) || '/';
  }
  return null;
}

export function getHomeCards(locale: Locale) {
  if (locale === 'zh') {
    return [
      { title: '工程文档', description: '架构设计、部署指南、配置注册表与演进路线图。', href: '/docs' },
      { title: 'API 参考', description: '认证、线程、运行、组织、计费与管理端点。', href: '/api' },
      { title: 'Research', description: 'Arkloop 运行时、Agent Loop 与系统设计研究。', href: '/research' },
    ];
  }

  return [
    { title: 'Documentation', description: 'Architecture, deployment, configuration registry, and roadmap.', href: '/en/docs' },
    { title: 'API Reference', description: 'Auth, threads, runs, orgs, billing, and admin endpoints.', href: '/en/api' },
    { title: 'Research', description: 'Runtime, Agent Loop, and system design papers from Arkloop.', href: '/research' },
  ];
}
