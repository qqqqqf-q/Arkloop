import { useMemo } from 'react'
import { silentRefresh } from '@arkloop/shared'
import { apiBaseUrl } from '@arkloop/shared/api'
import { useAuth } from '../contexts/auth'
import { createArkloopAgentClient } from './arkloop-adapter'
import type { AgentClient } from './contract'

export function useAgentClient(): AgentClient {
  const { accessToken } = useAuth()
  const baseUrl = apiBaseUrl()

  return useMemo(() => createArkloopAgentClient({
    accessToken,
    baseUrl,
    refreshAccessToken: async () => {
      return await silentRefresh()
    },
  }), [accessToken, baseUrl])
}
