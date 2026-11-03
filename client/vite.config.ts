import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

function apiTarget(): string {
  const proc = (globalThis as { process?: { env?: Record<string, string | undefined> } }).process;
  return proc?.env?.EASYTIER_API ?? 'http://127.0.0.1:11211';
}
// The API lives on the Go daemon; in dev we proxy so cookies stay same-origin.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      // process is a Node global at config load time; declaring its shape here keeps
      // the build independent of @types/node, which the image does not install.
      '/api': { target: apiTarget(), changeOrigin: false },
    },
  },
  build: { outDir: 'dist', sourcemap: false },
});
