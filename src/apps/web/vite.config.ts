/// <reference types="vitest" />
import { defineConfig, loadEnv } from 'vite'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'

const ARKLOOP_HOME = path.join(os.homedir(), '.arkloop')
const TRUE_VALUES = new Set(['1', 'true'])

function isEnabled(value: string | undefined): boolean {
  return TRUE_VALUES.has(value?.trim().toLowerCase() ?? '')
}

function readDesktopDevConfig(): { token: string; apiBaseUrl: string } | null {
  try {
    const token = fs.readFileSync(path.join(ARKLOOP_HOME, 'desktop.token'), 'utf-8').trim()
    if (!token) return null
    let port = 19001
    try {
      const p = parseInt(fs.readFileSync(path.join(ARKLOOP_HOME, 'desktop.port'), 'utf-8').trim(), 10)
      if (p > 0) port = p
    } catch {
      // Keep the default API port when the desktop port file is absent.
    }
    return { token, apiBaseUrl: `http://127.0.0.1:${port}` }
  } catch {
    return null
  }
}

function desktopDevPlugin(enabled: boolean) {
  return {
    name: 'arkloop-desktop-dev',
    transformIndexHtml() {
      if (!enabled) return []
      const cfg = readDesktopDevConfig()
      if (!cfg) return []
      const { token, apiBaseUrl } = cfg
      const platform = process.platform
      const obj = JSON.stringify({ apiBaseUrl, bridgeBaseUrl: '', accessToken: token, mode: 'local', platform })
      const children = [
        '(()=>{',
        `const injected=Object.assign(${obj},{`,
        `getApiBaseUrl:function(){return ${JSON.stringify(apiBaseUrl)}},`,
        "getBridgeBaseUrl:function(){return ''},",
        `getAccessToken:function(){return ${JSON.stringify(token)}},`,
        "getMode:function(){return 'local'},",
        `getPlatform:function(){return ${JSON.stringify(platform)}}`,
        '});',
        'window.__ARKLOOP_DESKTOP__=Object.assign({},injected,window.__ARKLOOP_DESKTOP__||{});',
        '})();',
      ].join('')
      return [
        {
          tag: 'script',
          injectTo: 'head-prepend' as const,
          children,
        },
      ]
    },
  }
}

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), 'ARKLOOP_')
  const desktopShellDev = isEnabled(process.env.ARKLOOP_DESKTOP_SHELL_DEV ?? env.ARKLOOP_DESKTOP_SHELL_DEV)
  const apiProxyTarget = (() => {
    const explicitTarget = process.env.ARKLOOP_API_PROXY_TARGET ?? env.ARKLOOP_API_PROXY_TARGET
    if (explicitTarget) return explicitTarget
    if (mode === 'development' && desktopShellDev) {
      const cfg = readDesktopDevConfig()
      if (cfg) return cfg.apiBaseUrl
    }
    return 'http://127.0.0.1:19000'
  })()
  const base =
    process.env.ARKLOOP_WEB_BASE ??
    env.ARKLOOP_WEB_BASE ??
    '/'

  return {
    base,
    plugins: [
      tailwindcss(),
      react(),
      ...(mode === 'development' ? [desktopDevPlugin(desktopShellDev)] : []),
    ],
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
      setupFiles: ['./src/test-setup.ts'],
    },
  }
})
