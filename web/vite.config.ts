import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import { createDemoDashboard } from './src/mock/demo.ts'

/** Serves dummy data at /api/dashboard during `npm run dev` only. */
const demoApi = (): Plugin => ({
  name: 'demo-api',
  apply: 'serve',
  configureServer(server) {
    server.middlewares.use('/api/dashboard', (_req, res) => {
      res.setHeader('Content-Type', 'application/json; charset=utf-8')
      res.end(JSON.stringify(createDemoDashboard(new Date())))
    })
  },
})

export default defineConfig({
  plugins: [react(), demoApi()],
  server: {
    port: 5173,
    allowedHosts: true,
  },
})