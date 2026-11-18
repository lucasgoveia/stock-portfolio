import { writeFileSync } from 'node:fs'
import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import { tanstackRouter } from '@tanstack/router-plugin/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    tanstackRouter({ target: 'react', autoCodeSplitting: true }),
    react(),
    tailwindcss(),
    {
      // Vite empties dist/; restore the tracked placeholder so `go:embed all:dist`
      // compiles on a fresh checkout and the working tree stays clean.
      name: 'keep-dist-placeholder',
      closeBundle() {
        writeFileSync(path.resolve(import.meta.dirname, 'dist/.gitkeep'), '')
      },
    },
  ],
  resolve: {
    alias: { '@': path.resolve(import.meta.dirname, 'src') },
  },
  server: {
    proxy: {
      '/api': process.env.API_URL ?? 'http://127.0.0.1:8080',
    },
  },
})
