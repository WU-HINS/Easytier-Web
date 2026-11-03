import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The API lives on the Go daemon; in dev we proxy so cookies stay same-origin.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: process.env.EASYTIER_API ?? 'http://127.0.0.1:11211', changeOrigin: false },
    },
  },
  build: { outDir: 'dist', sourcemap: false },
});
