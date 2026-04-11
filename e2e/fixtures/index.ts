import { test as base, Page } from '@playwright/test';
import channelsFixture from './api/channels.json';
import guideTodayFixture from './api/guide-today.json';
import categoriesFixture from './api/categories.json';
import searchResultsFixture from './api/search-results.json';
import exploreNowNextFixture from './api/explore-now-next.json';

export const FIXED_NOW = new Date('2025-06-10T14:00:00.000Z').getTime();

export async function installClock(page: Page): Promise<void> {
  await page.clock.install({ time: FIXED_NOW });
}

export async function setupApiRoutes(
  page: Page,
  overrides: Record<string, object | null> = {}
): Promise<void> {
  const defaultRoutes: Record<string, object> = {
    '/api/channels': channelsFixture,
    '/api/guide**': guideTodayFixture,
    '/api/categories': categoriesFixture,
    '/api/search**': searchResultsFixture,
    '/api/explore/now-next': exploreNowNextFixture,
  };

  for (const [pattern, body] of Object.entries(defaultRoutes)) {
    const override = overrides[pattern];
    const responseBody = override !== undefined ? override : body;

    if (responseBody === null) {
      continue;
    }

    await page.route(pattern, (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(responseBody),
      })
    );
  }

  await page.route('/api/debug/log', (route) => route.fulfill({ status: 204 }));
}

export async function seedLocalStorage(
  page: Page,
  key: string,
  value: unknown
): Promise<void> {
  await page.addInitScript(
    ({ k, v }: { k: string; v: string }) => {
      localStorage.setItem(k, JSON.stringify(v));
    },
    { k: key, v: value as string }
  );
}

type Fixtures = {
  seedLocalStorage: (key: string, value: unknown) => Promise<void>;
  setupApiRoutes: (overrides?: Record<string, object | null>) => Promise<void>;
};

export const test = base.extend<Fixtures>({
  page: async ({ page }, use) => {
    await installClock(page);
    await setupApiRoutes(page);
    await use(page);
  },
  seedLocalStorage: async ({ page }, use) => {
    await use((key: string, value: unknown) => seedLocalStorage(page, key, value));
  },
  setupApiRoutes: async ({ page }, use) => {
    await use((overrides?: Record<string, object | null>) =>
      setupApiRoutes(page, overrides)
    );
  },
});

export { expect } from '@playwright/test';
