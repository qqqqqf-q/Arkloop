import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'Arkloop Docs',
  description: '内部工程文档',
  lang: 'zh-CN',
  cleanUrls: true,

  themeConfig: {
    nav: [
      { text: '开发指南', link: '/guide/' },
      { text: 'API 参考', link: '/api/' },
      { text: '规范', link: '/specs/api-and-sse.zh-CN' },
    ],

    sidebar: {
      '/guide/': [
        {
          text: '开发指南',
          items: [
            { text: '本地启动', link: '/guide/' },
            { text: '部署', link: '/guide/deployment' },
            { text: 'Testing & Benchmarks', link: '/guide/testing' },
          ],
        },
      ],
      '/api/': [
        {
          text: 'API 参考',
          items: [
            { text: '概览', link: '/api/' },
            { text: '认证 (Auth)', link: '/api/auth' },
            { text: '当前用户 (Me)', link: '/api/me' },
            { text: '线程 (Threads)', link: '/api/threads' },
            { text: '消息 (Messages)', link: '/api/messages' },
            { text: '运行 (Runs)', link: '/api/runs' },
            { text: '项目 (Projects)', link: '/api/projects' },
            { text: '组织 (Orgs)', link: '/api/orgs' },
          ],
        },
        {
          text: '配置',
          items: [
            { text: 'LLM Credentials', link: '/api/llm-credentials' },
            { text: 'MCP Configs', link: '/api/mcp-configs' },
            { text: 'Agent Configs', link: '/api/agent-configs' },
            { text: 'ASR Credentials', link: '/api/asr-credentials' },
          ],
        },
        {
          text: '计费与权益',
          items: [
            { text: 'Credits & Usage', link: '/api/credits' },
            { text: 'Subscriptions & Plans', link: '/api/subscriptions' },
            { text: 'Entitlements', link: '/api/entitlements' },
            { text: 'API Keys', link: '/api/api-keys' },
          ],
        },
        {
          text: '通知与 Webhook',
          items: [
            { text: 'Notifications', link: '/api/notifications' },
            { text: 'Webhooks', link: '/api/webhooks' },
          ],
        },
        {
          text: '管理员 (Admin)',
          items: [
            { text: '总览', link: '/api/admin' },
            { text: '审计日志', link: '/api/audit-logs' },
            { text: 'IP 规则', link: '/api/ip-rules' },
            { text: 'Feature Flags', link: '/api/feature-flags' },
          ],
        },
        {
          text: '系统',
          items: [
            { text: '健康检查', link: '/api/health' },
          ],
        },
      ],
      '/reference/': [
        {
          text: 'Reference',
          items: [
            { text: 'Configuration Registry', link: '/reference/configuration.zh-CN' },
          ],
        },
      ],
      '/specs/': [
        {
          text: '技术规范',
          items: [
            { text: 'API & SSE 规范', link: '/specs/api-and-sse.zh-CN' },
            { text: '数据库架构', link: '/specs/database-architecture.zh-CN' },
            { text: '日志与可观测性', link: '/specs/logging-and-observability.zh-CN' },
            { text: 'Run 执行架构', link: '/specs/run-execution-architecture.zh-CN' },
          ],
        },
      ],
      '/roadmap/': [
        {
          text: 'Roadmap',
          items: [
            { text: 'Open Source Readiness', link: '/roadmap/open-source-readiness-roadmap.zh-CN' },
          ],
        },
      ],
    },

    socialLinks: [],
    search: { provider: 'local' },
    outline: { level: [2, 3], label: '目录' },
    darkModeSwitchLabel: '主题',
    sidebarMenuLabel: '菜单',
    returnToTopLabel: '回到顶部',
    docFooter: { prev: '上一页', next: '下一页' },
  },
})
