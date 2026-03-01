import { useCallback, useEffect, useState } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import { ConsoleLayout } from './layouts/ConsoleLayout'
import { PageHeader } from './components/PageHeader'
import { AuthPage } from './pages/AuthPage'
import { AuditPage } from './pages/AuditPage'
import { ReportsPage } from './pages/reports/ReportsPage'
import { RunsPage } from './pages/RunsPage'
import { DashboardPage } from './pages/dashboard/DashboardPage'
import { CredentialsPage } from './pages/credentials/CredentialsPage'
import { AgentConfigsPage } from './pages/agent-configs/AgentConfigsPage'
import { PromptTemplatesPage } from './pages/prompt-templates/PromptTemplatesPage'
import { MCPConfigsPage } from './pages/mcp-configs/MCPConfigsPage'
import { PersonasPage } from './pages/personas/PersonasPage'
import { APIKeysPage } from './pages/api-keys/APIKeysPage'
import { IPRulesPage } from './pages/ip-rules/IPRulesPage'
import { TeamsPage } from './pages/teams/TeamsPage'
import { UsagePage } from './pages/usage/UsagePage'
import { MyUsagePage } from './pages/my-usage/MyUsagePage'
import { OrgsPage } from './pages/OrgsPage'
import { UsersPage } from './pages/users/UsersPage'
import { InviteCodesPage } from './pages/invite-codes/InviteCodesPage'
import { RedemptionCodesPage } from './pages/redemption-codes/RedemptionCodesPage'
import { CreditsAdminPage } from './pages/credits-admin/CreditsAdminPage'
import { BroadcastsPage } from './pages/broadcasts/BroadcastsPage'
import { FeatureFlagsPage } from './pages/feature-flags/FeatureFlagsPage'
import { RegistrationPage } from './pages/registration/RegistrationPage'
import { AsrCredentialsPage } from './pages/asr-credentials/AsrCredentialsPage'
import { EmailPage } from './pages/email/EmailPage'
import { PlatformConfigPage } from './pages/platform-config/PlatformConfigPage'
import { AccessLogPage } from './pages/access-log/AccessLogPage'
import {
  readAccessTokenFromStorage,
  writeAccessTokenToStorage,
  clearAccessTokenFromStorage,
  writeRefreshTokenToStorage,
  clearRefreshTokenFromStorage,
} from './storage'
import { setUnauthenticatedHandler, setAccessTokenHandler } from './api'

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

  useEffect(() => {
    setUnauthenticatedHandler(() => {
      clearAccessTokenFromStorage()
      clearRefreshTokenFromStorage()
      setAccessToken(null)
    })
    setAccessTokenHandler((token: string) => {
      writeAccessTokenToStorage(token)
      setAccessToken(token)
    })
  }, [])

  const handleLoggedIn = useCallback((token: string, refreshToken: string) => {
    writeAccessTokenToStorage(token)
    writeRefreshTokenToStorage(refreshToken)
    setAccessToken(token)
  }, [])

  const handleLoggedOut = useCallback(() => {
    clearAccessTokenFromStorage()
    clearRefreshTokenFromStorage()
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
        <Route path="reports" element={<ReportsPage />} />
        {/* Configuration */}
        <Route path="credentials" element={<CredentialsPage />} />
        <Route path="agent-configs" element={<AgentConfigsPage />} />
        <Route path="prompt-templates" element={<PromptTemplatesPage />} />
        <Route path="mcp-configs" element={<MCPConfigsPage />} />
        <Route path="personas" element={<PersonasPage />} />
        <Route path="asr-credentials" element={<AsrCredentialsPage />} />
        <Route path="title-summarizer" element={<Navigate to="/platform-config" replace />} />
        <Route path="platform-config" element={<PlatformConfigPage />} />
        {/* Integration */}
        <Route path="api-keys" element={<APIKeysPage />} />
        <Route path="webhooks" element={<PlaceholderPage title="Webhooks" />} />
        {/* Security */}
        <Route path="ip-rules" element={<IPRulesPage />} />
        <Route path="captcha" element={<Navigate to="/platform-config" replace />} />
        <Route path="gateway-config" element={<Navigate to="/platform-config" replace />} />
        <Route path="access-log" element={<AccessLogPage />} />
        {/* Organization */}
        <Route path="members" element={<OrgsPage />} />
        <Route path="teams" element={<TeamsPage />} />
        <Route path="projects" element={<PlaceholderPage title="Projects" />} />
        {/* Billing */}
        <Route path="plans" element={<PlaceholderPage title="Plans" />} />
        <Route path="subscriptions" element={<PlaceholderPage title="Subscriptions" />} />
        <Route path="entitlements" element={<Navigate to="/platform-config" replace />} />
        <Route path="usage" element={<UsagePage />} />
        <Route path="my-usage" element={<MyUsagePage />} />
        {/* Platform */}
        <Route path="feature-flags" element={<FeatureFlagsPage />} />
        <Route path="users" element={<UsersPage />} />
        <Route path="registration" element={<RegistrationPage />} />
        <Route path="invite-codes" element={<InviteCodesPage />} />
        <Route path="redemption-codes" element={<RedemptionCodesPage />} />
        <Route path="credits-admin" element={<CreditsAdminPage />} />
        <Route path="broadcasts" element={<BroadcastsPage />} />
        <Route path="email" element={<EmailPage />} />
        {/* Redirects */}
        <Route path="providers" element={<Navigate to="/credentials" replace />} />
        <Route path="orgs" element={<Navigate to="/members" replace />} />
        <Route path="*" element={<Navigate to="/dashboard" replace />} />
      </Route>
    </Routes>
  )
}

export default App
