/// <reference types="vitest/config" />
import react from '@vitejs/plugin-react'
import { defineConfig, loadEnv } from 'vite'

// In mock mode (the default) the MSW service worker answers every API call in the browser.
// With VITE_USE_MOCKS=false the same /api calls are proxied to the API Gateway instead, minus
// the prefix, so the app code is identical in both modes.
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const gateway = env.VITE_GATEWAY_URL || 'http://localhost:8000'
  return {
    plugins: [react()],
    server: {
      port: 5173,
      proxy: {
        '/api': {
          target: gateway,
          rewrite: (path) => path.replace(/^\/api/, ''),
          // The gateway scopes the refresh cookie to /auth; the browser calls /api/auth.
          cookiePathRewrite: { '/auth': '/api/auth' },
        },
      },
    },
    test: {
      environment: 'happy-dom',
      globals: true,
      setupFiles: ['./src/test/setup.ts'],
      css: false,
    },
  }
})
