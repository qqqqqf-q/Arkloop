/// <reference types="vitest" />
import { defineConfig, loadEnv } from 'vite'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), 'ARKLOOP_')
  const apiProxyTarget =
    process.env.ARKLOOP_API_PROXY_TARGET ??
    env.ARKLOOP_API_PROXY_TARGET ??
    'http://127.0.0.1:19001'

  return {
    plugins: [tailwindcss(), react()],
    server: {
      proxy: {
        '/v1': {
          target: apiProxyTarget,
          changeOrigin: true,
          configure: (proxy) => {
            proxy.on('proxyRes', (proxyRes, _req, res) => {
              if (proxyRes.headers['content-type']?.startsWith('text/event-stream')) {
                res.setHeader('X-Accel-Buffering', 'no')
                res.setHeader('Cache-Control', 'no-cache, no-transform')
              }
            })
          },
        },
      },
    },
    test: {
      globals: true,
      environment: 'jsdom',
    },
  }
})
