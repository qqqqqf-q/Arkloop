import { useCallback, useEffect, useState } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import { ConsoleLayout } from './layouts/ConsoleLayout'
import { PageHeader } from './components/PageHeader'
import { AuthPage } from './pages/AuthPage'
import { AuditPage } from './pages/AuditPage'
import { ReportsPage } from './pages/reports/ReportsPage'
import { RunsPage } from './pages/RunsPage'
import { DashboardPage } from './pages/dashboard/DashboardPage'
import { ProvidersPage } from './pages/providers/ProvidersPage'
import { MCPConfigsPage } from './pages/mcp-configs/MCPConfigsPage'
import { PersonasPage } from './pages/personas/PersonasPage'
import { ToolsPage } from './pages/tools/ToolsPage'
import { APIKeysPage } from './pages/api-keys/APIKeysPage'
import { IPRulesPage } from './pages/ip-rules/IPRulesPage'
import { CaptchaPage } from './pages/captcha/CaptchaPage'
import { UsagePage } from './pages/usage/UsagePage'
import { MyUsagePage } from './pages/my-usage/MyUsagePage'
import { UsersPage } from './pages/users/UsersPage'
import { InviteCodesPage } from './pages/invite-codes/InviteCodesPage'
import { RedemptionCodesPage } from './pages/redemption-codes/RedemptionCodesPage'
import { CreditsAdminPage } from './pages/credits-admin/CreditsAdminPage'
import { BroadcastsPage } from './pages/broadcasts/BroadcastsPage'
import { FeatureFlagsPage } from './pages/feature-flags/FeatureFlagsPage'
import { RegistrationPage } from './pages/registration/RegistrationPage'
import { AsrCredentialsPage } from './pages/asr-credentials/AsrCredentialsPage'
import { EmailPage } from './pages/email/EmailPage'
import { TitleSummarizerPage } from './pages/title-summarizer/TitleSummarizerPage'
import { SkillsPage } from './pages/skills/SkillsPage'
import { GatewayConfigPage } from './pages/gateway-config/GatewayConfigPage'

import { ExecutionGovernancePage } from './pages/execution-governance/ExecutionGovernancePage'
import { AccessLogPage } from './pages/access-log/AccessLogPage'
import { EntitlementsPage } from './pages/entitlements/EntitlementsPage'
import { ModulesPage } from './pages/modules/ModulesPage'

import { PromptInjectionPage } from './pages/prompt-injection/PromptInjectionPage'
import { BootstrapPage } from './pages/BootstrapPage'

import { OperationProvider, useOperations } from '@arkloop/shared'
import { OperationHistoryModal } from './components/OperationHistoryModal'
import { bridgeClient } from './api/bridge'

import {
  writeAccessTokenToStorage,
  clearAccessTokenFromStorage,
} from './storage'
import { setUnauthenticatedHandler, setAccessTokenHandler, restoreAccessSession } from './api'
import { setClientApp } from '@arkloop/shared/api'

const sessionRestoreRetries = 12
const sessionRestoreDelayMs = 1000

function OperationHistoryModalWrapper() {
  const { historyOpen, setHistoryOpen } = useOperations()
  if (!historyOpen) return null
  return <OperationHistoryModal onClose={() => setHistoryOpen(false)} />
}

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
  const [accessToken, setAccessToken] = useState<string | null>(null)
  const [authChecked, setAuthChecked] = useState(false)

  useEffect(() => {
    const controller = new AbortController()

    // bootstrap token handoff from another console
    const params = new URLSearchParams(window.location.search)
    const handoffToken = params.get('_t')
    if (handoffToken) {
      params.delete('_t')
      const qs = params.toString()
      window.history.replaceState({}, '', `${window.location.pathname}${qs ? '?' + qs : ''}`)
      writeAccessTokenToStorage(handoffToken)
      const raf = requestAnimationFrame(() => {
        setAccessToken(handoffToken)
        setAuthChecked(true)
      })
      return () => {
        controller.abort()
        cancelAnimationFrame(raf)
      }
    }

    setClientApp('console')
    setUnauthenticatedHandler(() => {
      clearAccessTokenFromStorage()
      setAccessToken(null)
    })
    setAccessTokenHandler((token: string) => {
      writeAccessTokenToStorage(token)
      setAccessToken(token)
    })

    restoreAccessSession({
      signal: controller.signal,
      retries: sessionRestoreRetries,
      retryDelayMs: sessionRestoreDelayMs,
    })
      .then((resp) => {
        if (controller.signal.aborted) return
        writeAccessTokenToStorage(resp.access_token)
        setAccessToken(resp.access_token)
      })
      .catch(() => {})
      .finally(() => {
        if (controller.signal.aborted) return
        setAuthChecked(true)
      })

    return () => {
      controller.abort()
    }
  }, [])

  const handleLoggedIn = useCallback((token: string) => {
    writeAccessTokenToStorage(token)
    setAccessToken(token)
  }, [])

  const handleLoggedOut = useCallback(() => {
    clearAccessTokenFromStorage()
    setAccessToken(null)
  }, [])

  if (!authChecked) return null

  return (
    <Routes>
      <Route path="/bootstrap/:token" element={<BootstrapPage onLoggedIn={handleLoggedIn} />} />

      {!accessToken ? (
        <Route path="*" element={<AuthPage onLoggedIn={handleLoggedIn} />} />
      ) : (
        <Route
          element={
            <OperationProvider client={bridgeClient}>
              <OperationHistoryModalWrapper />
              <ConsoleLayout accessToken={accessToken} onLoggedOut={handleLoggedOut} />
            </OperationProvider>
          }
        >
          <Route index element={<Navigate to="/dashboard" replace />} />
          {/* Operations */}
          <Route path="dashboard" element={<DashboardPage />} />
          <Route path="runs" element={<RunsPage />} />
          <Route path="audit" element={<AuditPage />} />
          <Route path="reports" element={<ReportsPage />} />
          {/* Configuration */}
          <Route path="providers" element={<ProvidersPage />} />
          <Route path="mcp-configs" element={<MCPConfigsPage />} />
          <Route path="tools" element={<ToolsPage />} />
          <Route path="personas" element={<PersonasPage />} />
          <Route path="asr-credentials" element={<AsrCredentialsPage />} />
          <Route path="title-summarizer" element={<TitleSummarizerPage />} />
          <Route path="skills" element={<SkillsPage />} />
          <Route path="execution-governance" element={<ExecutionGovernancePage />} />
          {/* Integration */}
          <Route path="api-keys" element={<APIKeysPage />} />
          <Route path="webhooks" element={<PlaceholderPage title="Webhooks" />} />
          {/* Security */}
          <Route path="prompt-injection" element={<PromptInjectionPage />} />
          <Route path="ip-rules" element={<IPRulesPage />} />
          <Route path="captcha" element={<CaptchaPage />} />
          <Route path="gateway-config" element={<GatewayConfigPage />} />
          <Route path="access-log" element={<AccessLogPage />} />
          {/* Billing */}
          <Route path="plans" element={<PlaceholderPage title="Plans" />} />
          <Route path="subscriptions" element={<PlaceholderPage title="Subscriptions" />} />
          <Route path="entitlements" element={<EntitlementsPage />} />
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
          {/* Infrastructure */}
          <Route path="modules" element={<ModulesPage />} />
          {/* Redirects */}
          <Route path="tool-providers" element={<Navigate to="/tools" replace />} />
          <Route path="sandbox-config" element={<Navigate to="/tools" replace />} />
          <Route path="memory-config" element={<Navigate to="/tools" replace />} />
          <Route path="members" element={<Navigate to="/users" replace />} />
          <Route path="teams" element={<Navigate to="/users" replace />} />
          <Route path="*" element={<Navigate to="/dashboard" replace />} />
        </Route>
      )}
    </Routes>
  )
}

export default App
