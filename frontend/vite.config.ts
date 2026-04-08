import { defineConfig } from 'vite'
import path from 'path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [
    // The React and Tailwind plugins are both required for Make, even if
    // Tailwind is not being actively used – do not remove them
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      // Alias @ to the src directory
      '@': path.resolve(__dirname, './src'),
    },
  },

  // File types to support raw imports. Never add .css, .tsx, or .ts files to this.
  assetsInclude: ['**/*.svg', '**/*.csv'],

  // Pre-bundle TomTom SDK and MapLibre so Vite handles their ESM format correctly.
  optimizeDeps: {
    include: [
      '@tomtom-org/maps-sdk',
      'maplibre-gl',
    ],
  },

  // Local dev proxy — mirrors Vercel rewrites so relative /api/* calls work
  // without setting VITE_API_URL. Assumes nginx running on localhost:80.
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost',
        changeOrigin: true,
      },
    },
  },
})
