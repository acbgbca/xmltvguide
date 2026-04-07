import { test, expect } from '../fixtures/index';
import { AppPage } from '../pages/AppPage';
import { GuidePage } from '../pages/GuidePage';
import { SearchPage } from '../pages/SearchPage';

// FIXED_NOW = 2025-06-10T14:00:00.000Z (Tuesday, 14:00 UTC)

test.describe('Tab switching via bottom nav', () => {
  test('clicking Guide tab shows the guide page', async ({ page }) => {
    const app = new AppPage(page);
    await page.goto('/search');
    await app.waitForAppReady();

    await app.navigateTo('guide');

    await expect(page.locator('#page-guide')).toBeVisible();
    await expect(page.locator('#page-search')).not.toBeVisible();
    expect(await app.activeTab()).toBe('guide');
    // Search nav button should not be active
    const searchBtn = page.locator('.bottom-nav-btn[data-page="search"]');
    await expect(searchBtn).not.toHaveClass(/active/);
  });

  test('clicking Search tab shows the search page', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    await guide.navigateTo('search');

    await expect(page.locator('#page-search')).toBeVisible();
    expect(await guide.activeTab()).toBe('search');
  });

  test('clicking Favourites tab shows the favourites page', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    await guide.navigateTo('favourites');

    await expect(page.locator('#page-favourites')).toBeVisible();
    expect(await guide.activeTab()).toBe('favourites');
  });

  test('clicking Settings tab shows the settings page', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    await guide.navigateTo('settings');

    await expect(page.locator('#page-settings')).toBeVisible();
    expect(await guide.activeTab()).toBe('settings');
  });

  test('top bar is visible on Guide tab', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    await expect(page.locator('#topBar')).toBeVisible();
  });

  test('top bar is hidden on Search tab', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    await guide.navigateTo('search');

    // topBar is hidden by setting display:none via the router
    const topBarDisplay = await page.locator('#topBar').evaluate(
      (el) => (el as HTMLElement).style.display
    );
    expect(topBarDisplay).toBe('none');
  });
});

test.describe('URL and History API', () => {
  test('switching tabs updates the URL without a page reload', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    // Track that no full reload occurred — performance.navigation.type should be 0 (TYPE_NAVIGATE)
    // after initial load. After pushState navigation it stays 0 but we track a marker instead.
    await page.evaluate(() => {
      (window as any)._loadCount = 0;
      window.addEventListener('load', () => { (window as any)._loadCount++; });
    });

    await guide.navigateTo('search');

    const url = page.url();
    expect(url).toMatch(/\/search$/);

    // A pushState navigation should NOT fire a load event — count should still be 0
    const loadCount = await page.evaluate(() => (window as any)._loadCount);
    expect(loadCount).toBe(0);
  });

  test('Guide tab preserves the ?date= parameter in URL', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    // Navigate to next day — URL should become ?date=2025-06-11
    await guide.clickNextDay();
    const urlAfterNextDay = page.url();
    expect(urlAfterNextDay).toContain('date=2025-06-11');

    // Navigate to Search and back to Guide
    await guide.navigateTo('search');
    await guide.navigateTo('guide');

    // URL should still contain the date parameter
    const urlAfterReturn = page.url();
    expect(urlAfterReturn).toContain('date=2025-06-11');
  });

  test('direct navigation to /search loads the app on Search tab', async ({ page }) => {
    const app = new AppPage(page);
    await page.goto('/search');
    await app.waitForAppReady();

    await expect(page.locator('#page-search')).toBeVisible();
    const searchBtn = page.locator('.bottom-nav-btn[data-page="search"]');
    await expect(searchBtn).toHaveClass(/active/);
  });

  test('direct navigation to /favourites loads the app on Favourites tab', async ({ page }) => {
    const app = new AppPage(page);
    await page.goto('/favourites');
    await app.waitForAppReady();

    await expect(page.locator('#page-favourites')).toBeVisible();
    const favouritesBtn = page.locator('.bottom-nav-btn[data-page="favourites"]');
    await expect(favouritesBtn).toHaveClass(/active/);
  });

  test('direct navigation to /settings loads the app on Settings tab', async ({ page }) => {
    const app = new AppPage(page);
    await page.goto('/settings');
    await app.waitForAppReady();

    await expect(page.locator('#page-settings')).toBeVisible();
    const settingsBtn = page.locator('.bottom-nav-btn[data-page="settings"]');
    await expect(settingsBtn).toHaveClass(/active/);
  });
});

test.describe('Browser back/forward', () => {
  test('browser back button returns to previous tab', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    await guide.navigateTo('search');
    expect(page.url()).toMatch(/\/search$/);

    await page.goBack();

    await expect(page.locator('#page-guide')).toBeVisible();
    const url = page.url();
    expect(url).toMatch(/\/(guide)?(\?.*)?$/);
  });

  test('browser forward button navigates forward', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    await guide.navigateTo('search');
    await page.goBack();
    await expect(page.locator('#page-guide')).toBeVisible();

    await page.goForward();

    await expect(page.locator('#page-search')).toBeVisible();
  });

  test('back button on Guide restores date', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    // Navigate to next day
    await guide.clickNextDay();
    const tomorrowText = await guide.dateDisplay.textContent();
    expect(tomorrowText).toContain('Jun');

    // Go back
    await page.goBack();

    // Should show today's date (2025-06-10)
    const todayText = await guide.dateDisplay.textContent();
    expect(todayText).toContain('Jun');
    expect(todayText).toMatch(/10/);
  });
});

test.describe('Auth redirect handling', () => {
  // The handleRedirect function in web/js/api.js checks res.type === 'opaqueredirect'.
  // Playwright's page.route() cannot produce a true opaque redirect (which requires
  // fetch() with redirect:'manual' against a cross-origin redirect in a real browser).
  // This scenario requires a real Traefik/Authelia proxy to test end-to-end.
  test.skip('opaque redirect triggers a full-page navigation', async ({ page }) => {
    // Requires a real proxy to produce an opaque redirect response type.
    // Cannot be reliably simulated via Playwright route interception.
  });
});

test.describe('Loading screen', () => {
  test('loading screen is hidden after data loads', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    // After waitForAppReady(), the loading screen should have class 'hidden'
    await expect(page.locator('#loadingScreen')).toHaveClass(/hidden/);
  });

  test('loading screen shows error text if API fails', async ({ page, setupApiRoutes }) => {
    // Override /api/channels to return 500
    await setupApiRoutes({ '/api/channels': null });
    await page.route('/api/channels', (route) =>
      route.fulfill({ status: 500, body: 'Internal Server Error' })
    );

    await page.goto('/');

    await expect(page.locator('#loadingText')).toContainText('Failed to load guide data', {
      timeout: 10_000,
    });
  });
});
