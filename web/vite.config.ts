import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// In dev the app calls /state, /events, /tx and so on as same-origin paths;
// Vite forwards them to a locally running dyld. In production dyld serves
// web/dist itself, so the same paths are already same-origin.
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
