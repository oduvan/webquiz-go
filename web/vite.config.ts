import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import { resolve } from 'node:path';

// Five pages, built to the root of dist/ with assets/ beside them.
// base './' keeps URLs relative, so the quiz page also works under a path prefix.
const pages = ['index', 'admin', 'files', 'live-stats', 'csv-table'];

export default defineConfig({
  base: './',
  plugins: [react()],
  build: {
    target: ['es2019', 'safari13'],
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      input: Object.fromEntries(pages.map((name) => [name, resolve(import.meta.dirname, `${name}.html`)])),
    },
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.{ts,tsx}'],
  },
});
