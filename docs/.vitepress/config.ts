import { defineConfig } from 'vitepress'

const zhNav = [
  { text: '开发指南', link: '/guide/' },
  { text: 'API 参考', link: '/api/' },
  { text: '技术规范', link: '/specs/api-and-sse' },
]

const enNav = [
  { text: 'Guide', link: '/en/guide/' },
  { text: 'API Reference', link: '/en/api/' },
  { text: 'Specs', link: '/en/specs/api-and-sse' },
]

const zhSidebar = {
  '/guide/': [
    {
      text: '开发指南',
      items: [
        { text: '本地启动', link: '/guide/' },
        { text: '部署', link: '/guide/deployment' },
        { text: '测试与基准', link: '/guide/testing' },
      ],
    },
  ],
  '/api/': [
    {
      text: 'API 参考',
      items: [
        { text: '概览', link: '/api/' },
        { text: '认证', link: '/api/auth' },
        { text: '当前用户', link: '/api/me' },
        { text: '线程', link: '/api/threads' },
        { text: '消息', link: '/api/messages' },
        { text: '运行', link: '/api/runs' },
        { text: '项目', link: '/api/projects' },
        { text: '组织', link: '/api/orgs' },
      ],
    },
    {
      text: '配置',
      items: [
        { text: 'LLM 凭证', link: '/api/llm-credentials' },
        { text: 'MCP 配置', link: '/api/mcp-configs' },
        { text: 'Agent 配置', link: '/api/agent-configs' },
        { text: 'ASR 凭证', link: '/api/asr-credentials' },
      ],
    },
    {
      text: '计费与权益',
      items: [
        { text: '积分与用量', link: '/api/credits' },
        { text: '订阅与方案', link: '/api/subscriptions' },
        { text: '权益', link: '/api/entitlements' },
        { text: 'API 密钥', link: '/api/api-keys' },
      ],
    },
    {
      text: '通知与 Webhook',
      items: [
        { text: '通知', link: '/api/notifications' },
        { text: 'Webhooks', link: '/api/webhooks' },
      ],
    },
    {
      text: '管理',
      items: [
        { text: '管理员总览', link: '/api/admin' },
        { text: '审计日志', link: '/api/audit-logs' },
        { text: 'IP 规则', link: '/api/ip-rules' },
        { text: '功能开关', link: '/api/feature-flags' },
      ],
    },
    {
      text: '系统',
      items: [
        { text: '健康检查', link: '/api/health' },
        { text: '工具提供商', link: '/api/tool-providers' },
      ],
    },
  ],
  '/specs/': [
    {
      text: '技术规范',
      items: [
        { text: 'API & SSE', link: '/specs/api-and-sse' },
        { text: '数据库架构', link: '/specs/database-architecture' },
        { text: '日志与可观测性', link: '/specs/logging-and-observability' },
        { text: 'Run 执行架构', link: '/specs/run-execution-architecture' },
      ],
    },
  ],
  '/reference/': [
    {
      text: '参考',
      items: [
        { text: '配置注册表', link: '/reference/configuration' },
      ],
    },
  ],
  '/roadmap/': [
    {
      text: '路线图',
      items: [
        { text: '开源准备度', link: '/roadmap/open-source-readiness-roadmap' },
      ],
    },
  ],
}

const enSidebar = {
  '/en/guide/': [
    {
      text: 'Guide',
      items: [
        { text: 'Getting Started', link: '/en/guide/' },
        { text: 'Deployment', link: '/en/guide/deployment' },
        { text: 'Testing & Benchmarks', link: '/en/guide/testing' },
      ],
    },
  ],
  '/en/api/': [
    {
      text: 'API Reference',
      items: [
        { text: 'Overview', link: '/en/api/' },
        { text: 'Auth', link: '/en/api/auth' },
        { text: 'Me', link: '/en/api/me' },
        { text: 'Threads', link: '/en/api/threads' },
        { text: 'Messages', link: '/en/api/messages' },
        { text: 'Runs', link: '/en/api/runs' },
        { text: 'Projects', link: '/en/api/projects' },
        { text: 'Orgs', link: '/en/api/orgs' },
      ],
    },
    {
      text: 'Configuration',
      items: [
        { text: 'LLM Credentials', link: '/en/api/llm-credentials' },
        { text: 'MCP Configs', link: '/en/api/mcp-configs' },
        { text: 'Agent Configs', link: '/en/api/agent-configs' },
        { text: 'ASR Credentials', link: '/en/api/asr-credentials' },
      ],
    },
    {
      text: 'Billing & Entitlements',
      items: [
        { text: 'Credits & Usage', link: '/en/api/credits' },
        { text: 'Subscriptions & Plans', link: '/en/api/subscriptions' },
        { text: 'Entitlements', link: '/en/api/entitlements' },
        { text: 'API Keys', link: '/en/api/api-keys' },
      ],
    },
    {
      text: 'Notifications & Webhooks',
      items: [
        { text: 'Notifications', link: '/en/api/notifications' },
        { text: 'Webhooks', link: '/en/api/webhooks' },
      ],
    },
    {
      text: 'Admin',
      items: [
        { text: 'Overview', link: '/en/api/admin' },
        { text: 'Audit Logs', link: '/en/api/audit-logs' },
        { text: 'IP Rules', link: '/en/api/ip-rules' },
        { text: 'Feature Flags', link: '/en/api/feature-flags' },
      ],
    },
    {
      text: 'System',
      items: [
        { text: 'Health Check', link: '/en/api/health' },
        { text: 'Tool Providers', link: '/en/api/tool-providers' },
      ],
    },
  ],
  '/en/specs/': [
    {
      text: 'Specifications',
      items: [
        { text: 'API & SSE', link: '/en/specs/api-and-sse' },
        { text: 'Database Architecture', link: '/en/specs/database-architecture' },
        { text: 'Logging & Observability', link: '/en/specs/logging-and-observability' },
        { text: 'Run Execution', link: '/en/specs/run-execution-architecture' },
      ],
    },
  ],
  '/en/reference/': [
    {
      text: 'Reference',
      items: [
        { text: 'Configuration Registry', link: '/en/reference/configuration' },
      ],
    },
  ],
  '/en/roadmap/': [
    {
      text: 'Roadmap',
      items: [
        { text: 'Open Source Readiness', link: '/en/roadmap/open-source-readiness-roadmap' },
      ],
    },
  ],
}

export default defineConfig({
  title: 'Arkloop Docs',
  cleanUrls: true,
  srcExclude: [
    'zh-CN/**',
    'OPEN-SOURCE-BOUNDARY.md',
    'THIRD-PARTY-LICENSES.md',
    'README.md',
  ],

  locales: {
    root: {
      label: '简体中文',
      lang: 'zh-CN',
      description: 'Arkloop 工程文档',
      themeConfig: {
        nav: zhNav,
        sidebar: zhSidebar,
        outline: { level: [2, 3], label: '目录' },
        darkModeSwitchLabel: '主题',
        sidebarMenuLabel: '菜单',
        returnToTopLabel: '回到顶部',
        docFooter: { prev: '上一页', next: '下一页' },
      },
    },
    en: {
      label: 'English',
      lang: 'en-US',
      description: 'Arkloop Engineering Docs',
      themeConfig: {
        nav: enNav,
        sidebar: enSidebar,
        outline: { level: [2, 3], label: 'On this page' },
        darkModeSwitchLabel: 'Theme',
        sidebarMenuLabel: 'Menu',
        returnToTopLabel: 'Back to top',
        docFooter: { prev: 'Previous', next: 'Next' },
      },
    },
  },

  themeConfig: {
    socialLinks: [],
    search: { provider: 'local' },
  },
})
