/// <reference types="vitest" />
import { defineConfig, loadEnv } from 'vite'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), 'ARKLOOP_')
  const apiProxyTarget =
    env.ARKLOOP_API_PROXY_TARGET ??
    process.env.ARKLOOP_API_PROXY_TARGET ??
    'http://127.0.0.1:8001'
  const consolePort = Number(env.ARKLOOP_CONSOLE_PORT ?? process.env.ARKLOOP_CONSOLE_PORT ?? '5174')

  return {
    plugins: [tailwindcss(), react()],
    server: {
      port: consolePort,
      strictPort: true,
      proxy: {
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
