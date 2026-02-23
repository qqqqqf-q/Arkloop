import { useCallback, useState } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import { ConsoleLayout } from './layouts/ConsoleLayout'
import { PageHeader } from './components/PageHeader'
import { AuthPage } from './pages/AuthPage'
import { AuditPage } from './pages/AuditPage'
import { RunsPage } from './pages/RunsPage'
import { DashboardPage } from './pages/dashboard/DashboardPage'
import { CredentialsPage } from './pages/credentials/CredentialsPage'
import { AgentConfigsPage } from './pages/agent-configs/AgentConfigsPage'
import { PromptTemplatesPage } from './pages/prompt-templates/PromptTemplatesPage'
import { MCPConfigsPage } from './pages/mcp-configs/MCPConfigsPage'
import { SkillsPage } from './pages/skills/SkillsPage'
import { APIKeysPage } from './pages/api-keys/APIKeysPage'
import { IPRulesPage } from './pages/ip-rules/IPRulesPage'
import { TeamsPage } from './pages/teams/TeamsPage'
import { UsagePage } from './pages/usage/UsagePage'
import { MyUsagePage } from './pages/my-usage/MyUsagePage'
import { OrgsPage } from './pages/OrgsPage'
import {
  readAccessTokenFromStorage,
  writeAccessTokenToStorage,
  clearAccessTokenFromStorage,
} from './storage'

function PlaceholderPage({ title }: { title: string }) {
  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={title} />
      <div className="flex flex-1 items-center justify-center">
        <p className="text-sm text-[var(--c-text-muted)]">--</p>
      </div>
    </div>
  )
}

function App() {
  const [accessToken, setAccessToken] = useState<string | null>(() => readAccessTokenFromStorage())

  const handleLoggedIn = useCallback((token: string) => {
    writeAccessTokenToStorage(token)
    setAccessToken(token)
  }, [])

  const handleLoggedOut = useCallback(() => {
    clearAccessTokenFromStorage()
    setAccessToken(null)
  }, [])

  if (!accessToken) {
    return <AuthPage onLoggedIn={handleLoggedIn} />
  }

  return (
    <Routes>
      <Route
        element={<ConsoleLayout accessToken={accessToken} onLoggedOut={handleLoggedOut} />}
      >
        <Route index element={<Navigate to="/dashboard" replace />} />
        {/* Operations */}
        <Route path="dashboard" element={<DashboardPage />} />
        <Route path="runs" element={<RunsPage />} />
        <Route path="audit" element={<AuditPage />} />
        {/* Configuration */}
        <Route path="credentials" element={<CredentialsPage />} />
        <Route path="agent-configs" element={<AgentConfigsPage />} />
        <Route path="prompt-templates" element={<PromptTemplatesPage />} />
        <Route path="mcp-configs" element={<MCPConfigsPage />} />
        <Route path="skills" element={<SkillsPage />} />
        {/* Integration */}
        <Route path="api-keys" element={<APIKeysPage />} />
        <Route path="webhooks" element={<PlaceholderPage title="Webhooks" />} />
        {/* Security */}
        <Route path="ip-rules" element={<IPRulesPage />} />
        {/* Organization */}
        <Route path="members" element={<OrgsPage />} />
        <Route path="teams" element={<TeamsPage />} />
        <Route path="projects" element={<PlaceholderPage title="Projects" />} />
        {/* Billing */}
        <Route path="plans" element={<PlaceholderPage title="Plans" />} />
        <Route path="subscriptions" element={<PlaceholderPage title="Subscriptions" />} />
        <Route path="entitlements" element={<PlaceholderPage title="Entitlements" />} />
        <Route path="usage" element={<UsagePage />} />
        <Route path="my-usage" element={<MyUsagePage />} />
        {/* Platform */}
        <Route path="feature-flags" element={<PlaceholderPage title="Feature Flags" />} />
        {/* Redirects */}
        <Route path="providers" element={<Navigate to="/credentials" replace />} />
        <Route path="orgs" element={<Navigate to="/members" replace />} />
        <Route path="*" element={<Navigate to="/dashboard" replace />} />
      </Route>
    </Routes>
  )
}

export default App
