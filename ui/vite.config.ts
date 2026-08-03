import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The Go binary embeds ui/dist via go:embed and serves it from the gateway
// root, so assets must be referenced relatively (base: './') and the output
// directory name must stay 'dist'.
export default defineConfig({
  plugins: [react()],
  base: './',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    assetsDir: 'assets',
    // Everything must work offline: no CDN chunks, no dynamic remote imports.
    modulePreload: { polyfill: false },
    target: 'es2020',
    sourcemap: false,
  },
  server: {
    port: 5173,
    proxy: {
      '/v1': 'http://127.0.0.1:8130',
      '/admin': 'http://127.0.0.1:8130',
    },
  },
})
