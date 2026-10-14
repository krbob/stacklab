// Optional verification proxy. Set STACKLAB_VERIFY_API to an explicitly chosen test instance.
import { fileURLToPath, URL } from 'node:url'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

const target = process.env.STACKLAB_VERIFY_API
if (!target) throw new Error('Set STACKLAB_VERIFY_API to the test instance URL before using the verification proxy.')
const origin = new URL(target).origin

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
        target,
        changeOrigin: true,
        secure: true,
        ws: true,
        cookieDomainRewrite: '127.0.0.1',
        configure(proxy) {
          proxy.on('proxyReq', (proxyReq) => {
            proxyReq.setHeader('origin', origin)
            proxyReq.setHeader('referer', `${origin}/`)
          })
          proxy.on('proxyReqWs', (proxyReq) => {
            proxyReq.setHeader('origin', origin)
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
