// Temporary verification config: dev server proxied to production API.
// Not part of the build; safe to delete.
import { fileURLToPath, URL } from 'node:url'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    host: '127.0.0.1',
    port: 5199,
    proxy: {
      '/api': {
        target: 'https://stacklab.bobinski.net',
        changeOrigin: true,
        secure: true,
        ws: true,
        cookieDomainRewrite: '127.0.0.1',
        configure(proxy) {
          proxy.on('proxyReq', (proxyReq) => {
            proxyReq.setHeader('origin', 'https://stacklab.bobinski.net')
            proxyReq.setHeader('referer', 'https://stacklab.bobinski.net/')
          })
          proxy.on('proxyRes', (proxyRes) => {
            const cookies = proxyRes.headers['set-cookie']
            if (cookies) {
              proxyRes.headers['set-cookie'] = cookies.map((c) =>
                c.replace(/;\s*Secure/gi, '').replace(/;\s*SameSite=None/gi, '; SameSite=Lax'),
              )
            }
          })
        },
      },
    },
  },
})
