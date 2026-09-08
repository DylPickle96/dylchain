import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// `npm run dev` sets VITE_BACKEND=http (.env.development), so the app calls
// /state, /events, /tx as same-origin paths and Vite forwards them to a
// local dyld. A plain `npm run build` instead bundles the chain as wasm and
// needs no server. `npm run build:http` keeps the proxy paths for a dyld
// that serves web/dist itself.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/state': 'http://localhost:8080',
      '/account': 'http://localhost:8080',
      '/tx': 'http://localhost:8080',
      '/faucet': 'http://localhost:8080',
      '/fault': 'http://localhost:8080',
      '/events': { target: 'http://localhost:8080', changeOrigin: true },
    },
  },
})
