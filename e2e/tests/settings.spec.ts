import { test, expect } from '../fixtures/index';
import { SettingsPage } from '../pages/SettingsPage';
import { GuidePage } from '../pages/GuidePage';
import channelsFixture from '../fixtures/api/channels.json';

// FIXED_NOW = 2025-06-10T14:00:00.000Z (Tuesday, 14:00 UTC)

test.describe('Settings tab — Rendering', () => {
  test('shows all channels in the list', async ({ page }) => {
    const settings = new SettingsPage(page);
    await settings.goto();

    await expect(settings.channelItems()).toHaveCount(5);

    for (const ch of channelsFixture) {
      await expect(settings.channelItemByName(ch.displayName)).toBeVisible();
    }
  });

  test('channels appear in source order (no prefs set)', async ({ page }) => {
    const settings = new SettingsPage(page);
    await settings.goto();

    const names = await settings.channelItems().locator('.settings-name').allTextContents();
    const expected = channelsFixture.map((ch) => ch.displayName);
    expect(names).toEqual(expected);
  });

  test('each row has favourite and hide buttons', async ({ page }) => {
    const settings = new SettingsPage(page);
    await settings.goto();

    const items = settings.channelItems();
    const count = await items.count();

    for (let i = 0; i < count; i++) {
      const item = items.nth(i);
      await expect(item.locator('.fav-btn')).toBeVisible();
      await expect(item.locator('.hide-btn')).toBeVisible();
    }
  });

  test('renders Channels section containing the channel list', async ({ page }) => {
    const settings = new SettingsPage(page);
    await settings.goto();

    await expect(settings.channelsSection()).toBeVisible();
    await expect(settings.channelsSection().locator('.settings-item')).toHaveCount(5);
  });

  test('renders Advanced section collapsed by default', async ({ page }) => {
    const settings = new SettingsPage(page);
    await settings.goto();

    await expect(settings.advancedSection()).toBeVisible();
    await expect(settings.advancedHeader()).toBeVisible();
    await expect(settings.advancedHeader()).toContainText('Advanced');
    expect(await settings.isAdvancedExpanded()).toBe(false);
  });
});

test.describe('Settings tab — Advanced accordion', () => {
  test('clicking the header expands and a second click collapses', async ({ page }) => {
    const settings = new SettingsPage(page);
    await settings.goto();

    expect(await settings.isAdvancedExpanded()).toBe(false);

    await settings.toggleAdvanced();
    expect(await settings.isAdvancedExpanded()).toBe(true);

    await settings.toggleAdvanced();
    expect(await settings.isAdvancedExpanded()).toBe(false);
  });

  test('Refresh guide button is rendered inside the Advanced section', async ({ page }) => {
    const settings = new SettingsPage(page);
    await settings.goto();

    await settings.toggleAdvanced();

    const refresh = settings.refreshButton();
    await expect(refresh).toBeVisible();
    await expect(refresh).toContainText(/refresh guide/i);
  });
});

test.describe('Settings tab — Refresh guide action', () => {
  test('successful refresh shows success message and re-fetches channels + guide', async ({
    page,
  }) => {
    const settings = new SettingsPage(page);
    await settings.goto();
    await settings.toggleAdvanced();

    let channelsRefetched = 0;
    let guideRefetched = 0;
    await page.route('/api/channels', (route) => {
      channelsRefetched += 1;
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(channelsFixture),
      });
    });
    await page.route('/api/guide**', (route) => {
      guideRefetched += 1;
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([]),
      });
    });

    let refreshCalled = false;
    await page.route('**/api/guide/refresh**', (route) => {
      refreshCalled = true;
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ok: true }),
      });
    });

    await settings.clickRefresh();

    const status = settings.refreshStatus();
    await expect(status).toBeVisible();
    await expect(status).toHaveClass(/is-success/);
    await expect(status).toContainText(/refreshed/i);

    expect(refreshCalled).toBe(true);
    expect(channelsRefetched).toBeGreaterThanOrEqual(1);
    expect(guideRefetched).toBeGreaterThanOrEqual(1);

    // Button is re-enabled after success
    await expect(settings.refreshButton()).toBeEnabled();
  });

  test('failure response shows the error message and re-enables the button', async ({
    page,
  }) => {
    const settings = new SettingsPage(page);
    await settings.goto();
    await settings.toggleAdvanced();

    await page.route('**/api/guide/refresh**', (route) =>
      route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'upstream timeout' }),
      })
    );

    await settings.clickRefresh();

    const status = settings.refreshStatus();
    await expect(status).toBeVisible();
    await expect(status).toHaveClass(/is-error/);
    await expect(status).toContainText('upstream timeout');

    await expect(settings.refreshButton()).toBeEnabled();
  });

  test('button is disabled while the refresh request is in flight', async ({ page }) => {
    const settings = new SettingsPage(page);
    await settings.goto();
    await settings.toggleAdvanced();

    await page.route('**/api/guide/refresh**', async (route) => {
      await new Promise((resolve) => setTimeout(resolve, 400));
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ok: true }),
      });
    });

    // Don't await — let the click resolve in the background
    const clickPromise = settings.clickRefresh();

    // While the request is in flight, the button is disabled
    await expect(settings.refreshButton()).toBeDisabled();
    await expect(settings.refreshSpinner()).toBeVisible();

    await clickPromise;

    // After completion, the button is re-enabled
    await expect(settings.refreshButton()).toBeEnabled();
  });
});

test.describe('Settings tab — Initial state from localStorage', () => {
  test('pre-existing favourite is shown with active fav-btn', async ({ page, seedLocalStorage }) => {
    await seedLocalStorage('tvguide-prefs', { hidden: {}, favourites: { ch3: true } });

    const settings = new SettingsPage(page);
    await settings.goto();

    expect(await settings.isFavourite('Seven')).toBe(true);
  });

  test('pre-existing hidden channel has active hide-btn and is-hidden class', async ({
    page,
    seedLocalStorage,
  }) => {
    await seedLocalStorage('tvguide-prefs', { hidden: { ch2: true }, favourites: {} });

    const settings = new SettingsPage(page);
    await settings.goto();

    expect(await settings.isHidden('SBS')).toBe(true);
    expect(await settings.isItemHiddenStyling('SBS')).toBe(true);
  });
});

test.describe('Settings tab — Toggling favourite', () => {
  test('clicking fav button adds favourite', async ({ page }) => {
    const settings = new SettingsPage(page);
    await settings.goto();

    await settings.toggleFavourite('ABC');

    expect(await settings.isFavourite('ABC')).toBe(true);

    const stored = await page.evaluate(() => localStorage.getItem('tvguide-prefs'));
    const prefs = JSON.parse(stored ?? '{}');
    expect(prefs.favourites).toEqual({ ch1: true });
  });

  test('clicking active fav button removes favourite', async ({ page, seedLocalStorage }) => {
    await seedLocalStorage('tvguide-prefs', { hidden: {}, favourites: { ch1: true } });

    const settings = new SettingsPage(page);
    await settings.goto();

    await settings.toggleFavourite('ABC');

    expect(await settings.isFavourite('ABC')).toBe(false);

    const stored = await page.evaluate(() => localStorage.getItem('tvguide-prefs'));
    const prefs = JSON.parse(stored ?? '{}');
    expect(prefs.favourites?.ch1).toBeFalsy();
  });

  test('toggling favourite re-renders the settings list', async ({ page }) => {
    const settings = new SettingsPage(page);
    await settings.goto();

    await settings.toggleFavourite('ABC');

    expect(await settings.isFavourite('ABC')).toBe(true);
  });
});

test.describe('Settings tab — Toggling hide', () => {
  test('clicking hide button hides a channel', async ({ page }) => {
    const settings = new SettingsPage(page);
    await settings.goto();

    await settings.toggleHide('SBS');

    expect(await settings.isHidden('SBS')).toBe(true);
    expect(await settings.isItemHiddenStyling('SBS')).toBe(true);

    const stored = await page.evaluate(() => localStorage.getItem('tvguide-prefs'));
    const prefs = JSON.parse(stored ?? '{}');
    expect(prefs.hidden).toEqual({ ch2: true });
  });

  test('clicking active hide button un-hides a channel', async ({ page, seedLocalStorage }) => {
    await seedLocalStorage('tvguide-prefs', { hidden: { ch2: true }, favourites: {} });

    const settings = new SettingsPage(page);
    await settings.goto();

    await settings.toggleHide('SBS');

    expect(await settings.isHidden('SBS')).toBe(false);
    expect(await settings.isItemHiddenStyling('SBS')).toBe(false);

    const stored = await page.evaluate(() => localStorage.getItem('tvguide-prefs'));
    const prefs = JSON.parse(stored ?? '{}');
    expect(prefs.hidden?.ch2).toBeFalsy();
  });
});

test.describe('Settings tab — Persistence across navigation', () => {
  test('preferences persist after navigating away and back', async ({ page }) => {
    const settings = new SettingsPage(page);
    await settings.goto();

    await settings.toggleFavourite('Seven');

    await settings.navigateTo('guide');
    await page.locator('.bottom-nav-btn[data-page="guide"]').waitFor({ state: 'attached' });

    await settings.navigateTo('settings');
    await page.locator('.bottom-nav-btn[data-page="settings"]').waitFor({ state: 'attached' });

    expect(await settings.isFavourite('Seven')).toBe(true);
  });

  test('preferences persist after page reload', async ({ page, setupApiRoutes }) => {
    const settings = new SettingsPage(page);
    await settings.goto();

    await settings.toggleHide('Nine');

    await page.reload();
    await setupApiRoutes();

    await settings.goto();

    expect(await settings.isHidden('Nine')).toBe(true);
  });
});

test.describe('Settings tab — Effect on Guide tab', () => {
  test('hiding a channel removes it from the guide', async ({ page, seedLocalStorage }) => {
    await seedLocalStorage('tvguide-prefs', { hidden: { ch2: true }, favourites: {} });

    const settings = new SettingsPage(page);
    await settings.goto();

    const guide = new GuidePage(page);
    await guide.navigateTo('guide');
    await page.locator('.bottom-nav-btn[data-page="guide"]').waitFor({ state: 'attached' });

    const labels = guide.channelLabels();
    const count = await labels.count();
    for (let i = 0; i < count; i++) {
      const text = await labels.nth(i).textContent();
      expect(text).not.toContain('SBS');
    }
  });

  test('making a channel a favourite moves it to the top of the guide', async ({
    page,
    seedLocalStorage,
  }) => {
    await seedLocalStorage('tvguide-prefs', { hidden: {}, favourites: { ch5: true } });

    const guide = new GuidePage(page);
    await guide.goto();

    const firstLabel = guide.channelLabels().first();
    await expect(firstLabel).toContainText('Ten');
  });
});
