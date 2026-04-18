import { test, expect } from '../fixtures/index';
import { FavouritesPage } from '../pages/FavouritesPage';
import { SearchPage } from '../pages/SearchPage';
import searchResultsFixture from '../fixtures/api/search-results.json';

// FIXED_NOW = 2025-06-10T14:00:00.000Z (Tuesday, 14:00 UTC)

const SIMPLE_FAV_SEED = [
  { id: 'fav-1', name: 'Evening News', query: 'news', mode: 'simple' },
];

const ADVANCED_FAV_SEED = [
  { id: 'fav-2', name: 'Drama Shows', query: 'drama', mode: 'advanced', categories: ['Drama'], includePast: false, includeRepeats: true },
];

const MULTI_FAV_SEED = [
  { id: 'fav-1', name: 'Evening News', query: 'news', mode: 'simple' },
  { id: 'fav-2', name: 'Drama Shows', query: 'drama', mode: 'advanced', categories: ['Drama'] },
];

test.describe('Favourites tab — Empty state', () => {
  test('shows empty message when no favourites are saved', async ({ page }) => {
    const fav = new FavouritesPage(page);
    await fav.goto();

    await expect(fav.emptyMessage).toBeVisible();
    await expect(fav.favGroups()).toHaveCount(0);
  });
});

test.describe('Favourites tab — Single saved search', () => {
  test('renders a saved search group with its name', async ({ page, seedLocalStorage }) => {
    await seedLocalStorage('tvguide-favourites', SIMPLE_FAV_SEED);

    const fav = new FavouritesPage(page);
    await fav.goto();
    await fav.waitForSearchesComplete();

    await expect(fav.favGroups()).toHaveCount(1);
    await expect(fav.favGroupByName('Evening News').locator('.fav-group-name')).toBeVisible();
  });

  test('shows airings for a saved search', async ({ page, seedLocalStorage }) => {
    await seedLocalStorage('tvguide-favourites', SIMPLE_FAV_SEED);

    const fav = new FavouritesPage(page);
    await fav.goto();
    await fav.waitForSearchesComplete();

    await expect(fav.titleGroupsInFav('Evening News')).toHaveCount(searchResultsFixture.length);
  });

  test('shows "No upcoming airings" when search returns empty', async ({
    page,
    seedLocalStorage,
    setupApiRoutes,
  }) => {
    await seedLocalStorage('tvguide-favourites', SIMPLE_FAV_SEED);
    await setupApiRoutes({ '/api/search**': [] });

    const fav = new FavouritesPage(page);
    await fav.goto();
    await fav.waitForSearchesComplete();

    await expect(fav.noResultsMessageInGroup('Evening News')).toBeVisible();
    await expect(fav.noResultsMessageInGroup('Evening News')).toContainText('No upcoming airings');
  });

  test('shows error message when search API fails', async ({ page, seedLocalStorage }) => {
    await seedLocalStorage('tvguide-favourites', SIMPLE_FAV_SEED);

    await page.route('/api/search**', (route) => {
      route.fulfill({ status: 500, body: 'Internal Server Error' });
    });

    const fav = new FavouritesPage(page);
    await fav.goto();
    await fav.waitForSearchesComplete();

    await expect(fav.noResultsMessageInGroup('Evening News')).toBeVisible();
    await expect(fav.noResultsMessageInGroup('Evening News')).toContainText('Search failed:');
  });

  test('airing row shows channel name and time', async ({ page, seedLocalStorage }) => {
    await seedLocalStorage('tvguide-favourites', SIMPLE_FAV_SEED);

    const fav = new FavouritesPage(page);
    await fav.goto();
    await fav.waitForSearchesComplete();

    const firstAiring = fav.favGroupByName('Evening News').locator('.fav-airing').first();
    await expect(firstAiring.locator('.fav-airing-channel')).toBeVisible();
    await expect(firstAiring.locator('.fav-airing-time')).toBeVisible();
  });

  test('clicking an airing opens the modal', async ({ page, seedLocalStorage }) => {
    await seedLocalStorage('tvguide-favourites', SIMPLE_FAV_SEED);

    const fav = new FavouritesPage(page);
    await fav.goto();
    await fav.waitForSearchesComplete();

    await fav.clickAiring('Evening News', 0);

    await expect(fav.modal).toBeVisible();
  });
});

test.describe('Favourites tab — Multiple saved searches', () => {
  test('renders multiple saved search groups', async ({ page, seedLocalStorage }) => {
    await seedLocalStorage('tvguide-favourites', MULTI_FAV_SEED);

    const fav = new FavouritesPage(page);
    await fav.goto();
    await fav.waitForSearchesComplete();

    await expect(fav.favGroups()).toHaveCount(2);
  });

  test('loading spinner is shown while searches are pending', async ({ page, seedLocalStorage }) => {
    await seedLocalStorage('tvguide-favourites', SIMPLE_FAV_SEED);

    // Intercept the search route to add a delay
    await page.route('/api/search**', async (route) => {
      await new Promise((resolve) => setTimeout(resolve, 300));
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(searchResultsFixture),
      });
    });

    const fav = new FavouritesPage(page);

    // Don't await goto fully — start navigation and immediately check spinner
    const gotoPromise = fav.goto();

    // The spinner should be visible during load
    await expect(fav.loadingSpinner).toBeVisible();

    await gotoPromise;
    await fav.waitForSearchesComplete();
  });
});

test.describe('Favourites tab — Edit', () => {
  test('Edit button navigates to Search with query pre-filled', async ({
    page,
    seedLocalStorage,
  }) => {
    await seedLocalStorage('tvguide-favourites', SIMPLE_FAV_SEED);

    const fav = new FavouritesPage(page);
    await fav.goto();
    await fav.waitForSearchesComplete();

    await fav.clickEdit('Evening News');

    // Should navigate to search tab
    await expect(page.locator('.bottom-nav-btn[data-page="search"]')).toHaveClass(/active/);

    const search = new SearchPage(page);
    await expect(search.searchInput).toHaveValue('news');
  });

  test('Edit button pre-fills advanced options for an advanced favourite', async ({
    page,
    seedLocalStorage,
  }) => {
    await seedLocalStorage('tvguide-favourites', ADVANCED_FAV_SEED);

    const fav = new FavouritesPage(page);
    await fav.goto();
    await fav.waitForSearchesComplete();

    await fav.clickEdit('Drama Shows');

    // Advanced options panel should be open
    const panel = page.locator('#advancedOptions');
    const display = await panel.evaluate((el: HTMLElement) => el.style.display);
    expect(display).not.toBe('none');

    // Drama category chip should be selected
    const search = new SearchPage(page);
    await expect(search.categoryChipByName('Drama')).toHaveClass(/selected/);
  });
});

test.describe('Favourites tab — Delete', () => {
  test('Delete button removes the favourite after confirmation', async ({
    page,
    seedLocalStorage,
  }) => {
    await seedLocalStorage('tvguide-favourites', SIMPLE_FAV_SEED);

    const fav = new FavouritesPage(page);
    await fav.goto();
    await fav.waitForSearchesComplete();

    page.on('dialog', (dialog) => dialog.accept());
    await fav.clickDelete('Evening News');

    await expect(fav.favGroups()).toHaveCount(0);

    const stored = await page.evaluate(() => localStorage.getItem('tvguide-favourites'));
    const favourites = JSON.parse(stored ?? '[]');
    expect(favourites).toHaveLength(0);
  });

  test('Delete button with dismiss does not remove the favourite', async ({
    page,
    seedLocalStorage,
  }) => {
    await seedLocalStorage('tvguide-favourites', SIMPLE_FAV_SEED);

    const fav = new FavouritesPage(page);
    await fav.goto();
    await fav.waitForSearchesComplete();

    page.on('dialog', (dialog) => dialog.dismiss());
    await fav.clickDelete('Evening News');

    await expect(fav.favGroups()).toHaveCount(1);
  });
});

test.describe('Favourites tab — Result caching', () => {
  test('results are reused within 5 minutes (clock stays frozen)', async ({
    page,
    seedLocalStorage,
  }) => {
    await seedLocalStorage('tvguide-favourites', SIMPLE_FAV_SEED);

    let callCount = 0;

    await page.route('/api/search**', (route) => {
      callCount++;
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(searchResultsFixture),
      });
    });

    const fav = new FavouritesPage(page);
    await fav.goto();
    await fav.waitForSearchesComplete();

    // Navigate away to Guide tab
    await fav.navigateTo('guide');
    await page.locator('.bottom-nav-btn[data-page="guide"]').waitFor({ state: 'attached' });

    // Navigate back to Favourites
    await fav.navigateTo('favourites');
    await fav.waitForSearchesComplete();

    // The search API should have been called only once (cache hit on second visit)
    expect(callCount).toBe(1);
  });
});
