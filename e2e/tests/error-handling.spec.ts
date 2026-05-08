import { test, expect } from '@playwright/test';
import { installClock } from '../fixtures/index';

// Tests for API error handling on initial load.
// Note: the service worker is blocked in all Playwright tests (playwright.config.ts:
// serviceWorkers: 'block'), so opaqueredirect behaviour (Authelia auth expiry) cannot
// be simulated here. These tests cover non-redirect API failures.

test.describe('Error handling — initial load', () => {
  test('shows error message when channels API returns server error', async ({ page }) => {
    await installClock(page);
    await page.route('/api/channels', route =>
      route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"internal server error"}' })
    );
    await page.route('/api/guide**', route =>
      route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"internal server error"}' })
    );
    await page.route('/api/debug/log', route => route.fulfill({ status: 204 }));

    await page.goto('/');

    // Loading screen must stay visible — app cannot proceed without data
    await expect(page.locator('#loadingScreen')).toBeVisible();

    // Error message must replace "Loading TV guide..."
    await expect(page.locator('#loadingText')).toContainText('Failed to load guide data', { timeout: 5000 });
    await expect(page.locator('#loadingText')).not.toContainText('Loading TV guide...');
  });

  test('shows error message when channels API request fails with network error', async ({ page }) => {
    await installClock(page);
    await page.route('/api/channels', route => route.abort('failed'));
    await page.route('/api/guide**', route => route.abort('failed'));
    await page.route('/api/debug/log', route => route.fulfill({ status: 204 }));

    await page.goto('/');

    await expect(page.locator('#loadingScreen')).toBeVisible();
    await expect(page.locator('#loadingText')).toContainText('Failed to load guide data', { timeout: 5000 });
    await expect(page.locator('#loadingText')).not.toContainText('Loading TV guide...');
  });
});
