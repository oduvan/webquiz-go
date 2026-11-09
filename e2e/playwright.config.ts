import { defineConfig } from '@playwright/test';
import os from 'node:os';

// Every test starts its own server on a free port, so tests can run in parallel.
export default defineConfig({
  testDir: './tests',
  globalSetup: './global-setup.ts',
  workers: Math.max(1, Math.floor(os.cpus().length / 2)),
  fullyParallel: true,
  timeout: 60_000,
  expect: { timeout: 10_000 },
  reporter: [['list']],
  use: {
    trace: 'off',
    headless: true,
    launchOptions: { args: ['--no-sandbox'] },
  },
});
