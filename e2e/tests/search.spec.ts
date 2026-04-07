import { test, expect } from '../fixtures/index';
import { SearchPage } from '../pages/SearchPage';
import searchResultsFixture from '../fixtures/api/search-results.json';
import searchCh1OnlyFixture from '../fixtures/api/search-ch1-only.json';

// FIXED_NOW = 2025-06-10T14:00:00.000Z (Tuesday, 14:00 UTC)

test.describe('Search tab — Basic input behaviour', () => {
  test('shows hint text initially', async ({ page }) => {
    const search = new SearchPage(page);
    await search.goto();

    await expect(search.searchHint).toBeVisible();
    await expect(search.searchHint).toContainText('Enter at least 2 characters to search');
  });

  test('does not search for single character', async ({ page }) => {
    const search = new SearchPage(page);
    await search.goto();

    await search.searchInput.fill('A');
    await page.waitForTimeout(400);

    await expect(search.searchHint).toBeVisible();
    await expect(search.resultGroups()).toHaveCount(0);
  });

  test('clears input when clear button is clicked', async ({ page }) => {
    const search = new SearchPage(page);
    await search.goto();

    await search.typeQuery('news');
    await search.clearButton.click();

    await expect(search.searchInput).toHaveValue('');
    await expect(search.resultGroups()).toHaveCount(0);
  });
});

test.describe('Search tab — Search results', () => {
  test('shows results after typing a valid query', async ({ page }) => {
    const search = new SearchPage(page);
    await search.goto();

    await search.typeQuery('news');

    await expect(search.resultGroups()).toHaveCount(searchResultsFixture.length);
    await expect(search.resultGroups().first().locator('.search-group-title')).toContainText(
      searchResultsFixture[0].title
    );
  });

  test('shows empty message when no results', async ({ page, setupApiRoutes }) => {
    await setupApiRoutes({ '/api/search**': [] });

    const search = new SearchPage(page);
    await search.goto();

    await search.typeQuery('xyz');

    await expect(search.emptyMessage).toBeVisible();
    await expect(search.emptyMessage).toContainText('No programmes found');
  });

  test('each result group shows airing rows', async ({ page }) => {
    const search = new SearchPage(page);
    await search.goto();

    await search.typeQuery('news');

    for (const group of searchResultsFixture) {
      const airings = search.airingsInGroup(group.title);
      await expect(airings).toHaveCount(group.airings.length);
    }
  });

  test('airing row shows channel name and time', async ({ page }) => {
    const search = new SearchPage(page);
    await search.goto();

    await search.typeQuery('news');

    const firstGroup = searchResultsFixture[0];
    const firstAiring = search.airingsInGroup(firstGroup.title).first();
    await expect(firstAiring.locator('.search-airing-channel')).toBeVisible();
    await expect(firstAiring.locator('.search-airing-time')).toBeVisible();
  });
});

test.describe('Search tab — Advanced options', () => {
  test('advanced panel is hidden by default', async ({ page }) => {
    const search = new SearchPage(page);
    await search.goto();

    const panel = page.locator('#advancedOptions');
    const display = await panel.evaluate((el: HTMLElement) => el.style.display);
    expect(display).toBe('none');
  });

  test('advanced toggle reveals the options panel', async ({ page }) => {
    const search = new SearchPage(page);
    await search.goto();

    await search.openAdvancedOptions();

    const panel = page.locator('#advancedOptions');
    const display = await panel.evaluate((el: HTMLElement) => el.style.display);
    expect(display).not.toBe('none');
  });

  test('category chips are loaded from /api/categories', async ({ page }) => {
    const search = new SearchPage(page);
    await search.goto();

    await search.openAdvancedOptions();

    // Wait for chips to render
    await expect(search.categoryChips()).toHaveCount(5);
    await expect(search.categoryChipByName('Documentary')).toBeVisible();
    await expect(search.categoryChipByName('Drama')).toBeVisible();
    await expect(search.categoryChipByName('Film')).toBeVisible();
    await expect(search.categoryChipByName('News')).toBeVisible();
    await expect(search.categoryChipByName('Sport')).toBeVisible();
  });

  test('selecting a category chip marks it selected', async ({ page }) => {
    const search = new SearchPage(page);
    await search.goto();

    await search.openAdvancedOptions();
    await search.selectCategory('News');

    await expect(search.categoryChipByName('News')).toHaveClass(/selected/);
  });

  test('deselecting a chip removes selected class', async ({ page }) => {
    const search = new SearchPage(page);
    await search.goto();

    await search.openAdvancedOptions();
    await search.selectCategory('News');
    await expect(search.categoryChipByName('News')).toHaveClass(/selected/);

    await search.selectCategory('News');
    await expect(search.categoryChipByName('News')).not.toHaveClass(/selected/);
  });
});

test.describe('Search tab — Modal', () => {
  test('clicking an airing opens the programme detail modal', async ({ page }) => {
    const search = new SearchPage(page);
    await search.goto();

    await search.typeQuery('news');
    await search.clickAiring(searchResultsFixture[0].title, 0);

    await expect(search.modal).toBeVisible();
    const title = await search.modalTitle.textContent();
    expect(title?.trim().length).toBeGreaterThan(0);
  });

  test('modal displays channel and time from airing', async ({ page }) => {
    const search = new SearchPage(page);
    await search.goto();

    await search.typeQuery('news');
    await search.clickAiring(searchResultsFixture[0].title, 0);

    const timeText = await search.modalTime.textContent();
    expect(timeText?.trim().length).toBeGreaterThan(0);
  });
});

test.describe('Search tab — Save/remove favourite searches', () => {
  test('star button is hollow (☆) for an unsaved search', async ({ page }) => {
    const search = new SearchPage(page);
    await search.goto();

    await search.typeQuery('news');

    const firstTitle = searchResultsFixture[0].title;
    await expect(search.starButtonForGroup(firstTitle)).toContainText('\u2606');
  });

  test('clicking star button saves the search as a favourite', async ({ page }) => {
    const search = new SearchPage(page);
    await search.goto();

    await search.typeQuery('news');

    const firstTitle = searchResultsFixture[0].title;
    await search.clickStarForGroup(firstTitle);

    await expect(search.starButtonForGroup(firstTitle)).toContainText('\u2605');

    const stored = await page.evaluate(() => localStorage.getItem('tvguide-favourites'));
    const favourites = JSON.parse(stored ?? '[]');
    expect(favourites.some((f: { query: string }) => f.query === 'news')).toBe(true);
  });

  test('clicking a filled star removes the favourite', async ({ page, seedLocalStorage }) => {
    await seedLocalStorage('tvguide-favourites', [
      { id: '1', name: 'News', query: 'news', mode: 'simple' },
    ]);

    const search = new SearchPage(page);
    await search.goto();

    await search.typeQuery('news');

    const firstTitle = searchResultsFixture[0].title;
    await expect(search.starButtonForGroup(firstTitle)).toContainText('\u2605');

    await search.clickStarForGroup(firstTitle);

    await expect(search.starButtonForGroup(firstTitle)).toContainText('\u2606');

    const stored = await page.evaluate(() => localStorage.getItem('tvguide-favourites'));
    const favourites = JSON.parse(stored ?? '[]');
    expect(favourites.some((f: { query: string }) => f.query === 'news')).toBe(false);
  });

  test('star is pre-filled when search matches a saved favourite', async ({ page, seedLocalStorage }) => {
    await seedLocalStorage('tvguide-favourites', [
      { id: '1', name: 'News', query: 'news', mode: 'simple' },
    ]);

    const search = new SearchPage(page);
    await search.goto();

    await search.typeQuery('news');

    const firstTitle = searchResultsFixture[0].title;
    await expect(search.starButtonForGroup(firstTitle)).toContainText('\u2605');
  });
});

test.describe('Search tab — Channel hiding', () => {
  test('airings from hidden channels are excluded from results', async ({
    page,
    seedLocalStorage,
    setupApiRoutes,
  }) => {
    await seedLocalStorage('tvguide-prefs', { hidden: { ch1: true }, favourites: {} });
    await setupApiRoutes({ '/api/search**': searchCh1OnlyFixture });

    const search = new SearchPage(page);
    await search.goto();

    await search.typeQuery('news');

    // All airings are on ch1 which is hidden — results should show empty state
    await expect(search.emptyMessage).toBeVisible();
    await expect(search.resultGroups()).toHaveCount(0);
  });
});
