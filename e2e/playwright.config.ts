import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  timeout: 10_000,
  retries: process.env.CI ? 1 : 0,
  reporter: [['html', { outputFolder: 'playwright-report' }]],
  use: {
    baseURL: 'http://localhost:4173',
    browserName: 'chromium',
    serviceWorkers: 'block',
  },
  webServer: {
    command: 'npx serve web --single -l 4173',
    url: 'http://localhost:4173',
    reuseExistingServer: !process.env.CI,
    cwd: '..',
  },
});
