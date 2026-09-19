import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

// Dev server proxies everything the Go server owns; in production the Go binary
// embeds `dist/` and serves it at `/` with an SPA fallback (internal/httpapi/spa.go).
const backend = process.env.SIGAME_BACKEND ?? 'http://localhost:8080';
const backendWs = backend.replace(/^http/, 'ws');

export default defineConfig({
  base: '/',
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    proxy: {
      '/api': backend,
      '/media': backend,
      '/openapi.json': backend,
      '/openapi.yaml': backend,
      '/docs': backend,
      '/ws': { target: backendWs, ws: true },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
    globals: false,
  },
});
