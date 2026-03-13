/// <reference types="vitest" />
import { defineConfig, loadEnv } from 'vite'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), 'ARKLOOP_')
  const apiProxyTarget =
    env.ARKLOOP_API_PROXY_TARGET ??
    process.env.ARKLOOP_API_PROXY_TARGET ??
    'http://127.0.0.1:19001'

  const bridgeProxyTarget =
    env.ARKLOOP_BRIDGE_PROXY_TARGET ??
    process.env.ARKLOOP_BRIDGE_PROXY_TARGET ??
    'http://127.0.0.1:19003'

  return {
    plugins: [tailwindcss(), react()],
    server: {
      port: 19081,
      proxy: {
        '/bridge': {
          target: bridgeProxyTarget,
          changeOrigin: true,
          rewrite: (path: string) => path.replace(/^\/bridge/, ''),
        },
        '/v1': {
          target: apiProxyTarget,
          changeOrigin: true,
        },
      },
    },
    test: {
      globals: true,
      environment: 'jsdom',
      passWithNoTests: true,
    },
  }
})
