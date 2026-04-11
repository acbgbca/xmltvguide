import { test, expect } from '../fixtures/index';
import { AppPage } from '../pages/AppPage';
import { ExplorePage } from '../pages/ExplorePage';
import { GuidePage } from '../pages/GuidePage';

// FIXED_NOW = 2025-06-10T14:00:00.000Z (Tuesday, 14:00 UTC)

test.describe('Explore tab — Navigation', () => {
  test('Explore button appears in bottom nav', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    const exploreBtn = page.locator('.bottom-nav-btn[data-page="explore"]');
    await expect(exploreBtn).toBeVisible();
  });

  test('clicking Explore tab shows the Explore page', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    await guide.navigateTo('explore');

    await expect(page.locator('#page-explore')).toBeVisible();
    expect(await guide.activeTab()).toBe('explore');
  });

  test('direct navigation to /explore loads the app on Explore tab', async ({ page }) => {
    const app = new AppPage(page);
    await page.goto('/explore');
    await app.waitForAppReady();

    await expect(page.locator('#page-explore')).toBeVisible();
    const exploreBtn = page.locator('.bottom-nav-btn[data-page="explore"]');
    await expect(exploreBtn).toHaveClass(/active/);
  });

  test('top bar is hidden on Explore tab', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto();

    const topBarDisplay = await page.locator('#topBar').evaluate(
      (el) => (el as HTMLElement).style.display
    );
    expect(topBarDisplay).toBe('none');
  });
});

test.describe('Explore tab — Mode switcher', () => {
  test('renders all four mode buttons', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto();

    await expect(explore.modeButton('now-next')).toBeVisible();
    await expect(explore.modeButton('categories')).toBeVisible();
    await expect(explore.modeButton('premieres')).toBeVisible();
    await expect(explore.modeButton('time-slot')).toBeVisible();
  });

  test('defaults to now-next mode', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto();

    expect(await explore.activeMode()).toBe('now-next');
  });

  test('navigating to /explore?mode=premieres activates Premieres mode', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto('premieres');

    expect(await explore.activeMode()).toBe('premieres');
  });

  test('navigating to /explore?mode=categories activates Categories mode', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto('categories');

    expect(await explore.activeMode()).toBe('categories');
  });

  test('navigating to /explore?mode=time-slot activates Time Slot mode', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto('time-slot');

    expect(await explore.activeMode()).toBe('time-slot');
  });

  test('clicking a mode button updates the URL', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto();

    await explore.clickMode('premieres');

    expect(page.url()).toContain('mode=premieres');
  });

  test('clicking a mode button activates that mode', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto();

    await explore.clickMode('categories');

    expect(await explore.activeMode()).toBe('categories');
  });

  test('non-implemented modes show placeholder content', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto();

    for (const mode of ['premieres', 'time-slot']) {
      await explore.clickMode(mode);
      await expect(explore.contentArea).toContainText('Coming soon');
    }
  });
});

test.describe('Explore tab — Categories mode', () => {
  test('shows category picker with all available categories', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto('categories');

    await expect(explore.categoryPicker).toBeVisible();
    await expect(explore.categoryButton('Documentary')).toBeVisible();
    await expect(explore.categoryButton('Drama')).toBeVisible();
    await expect(explore.categoryButton('Film')).toBeVisible();
    await expect(explore.categoryButton('News')).toBeVisible();
    await expect(explore.categoryButton('Sport')).toBeVisible();
  });

  test('selecting a category fetches and displays results grouped by title', async ({ page, setupApiRoutes }) => {
    await setupApiRoutes({ '/api/search**': { title: 'Drama', airings: [] } });
    await page.route('/api/search**', (route) => {
      const url = new URL(route.request().url());
      if (url.searchParams.get('categories') === 'Drama') {
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([
            {
              title: 'MasterChef Australia',
              airings: [
                {
                  channelId: 'ch5',
                  channelName: 'Ten',
                  startTime: '2025-06-10T19:00:00Z',
                  stopTime: '2025-06-10T20:30:00Z',
                  categories: ['Drama'],
                  isRepeat: false,
                  isPremiere: true,
                },
              ],
            },
          ]),
        });
      } else {
        route.continue();
      }
    });

    const explore = new ExplorePage(page);
    await explore.goto('categories');
    await explore.categoryButton('Drama').click();

    await expect(explore.categoryResults).toBeVisible();
    await expect(explore.categorySearchGroups.first()).toContainText('MasterChef Australia');
  });

  test('selecting a category updates the URL', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto('categories');

    await explore.categoryButton('Sport').click();

    expect(page.url()).toContain('mode=categories');
    expect(page.url()).toContain('category=Sport');
  });

  test('back button returns to category picker', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto('categories');

    await explore.categoryButton('Drama').click();
    await expect(explore.categoryBackButton).toBeVisible();

    await explore.categoryBackButton.click();

    await expect(explore.categoryPicker).toBeVisible();
    await expect(explore.categoryResults).not.toBeVisible();
  });

  test('browser back from category results returns to category picker', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto('categories');

    await explore.categoryButton('Sport').click();
    expect(page.url()).toContain('category=Sport');

    await page.goBack();

    await expect(explore.categoryPicker).toBeVisible();
    await expect(explore.categoryResults).not.toBeVisible();
    expect(page.url()).not.toContain('category=');
  });

  test('shows loading state while fetching category results', async ({ page }) => {
    let resolveRoute: () => void;
    await page.route('/api/search**', async (route) => {
      await new Promise<void>(r => { resolveRoute = r; });
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([]),
      });
    });

    const explore = new ExplorePage(page);
    await explore.goto('categories');
    await explore.categoryButton('Drama').click();

    await expect(explore.loadingIndicator).toBeVisible();
    resolveRoute!();
  });

  test('shows error state when category search fails', async ({ page, setupApiRoutes }) => {
    await setupApiRoutes({ '/api/search**': null });
    await page.route('/api/search**', (route) =>
      route.fulfill({ status: 500, body: 'Internal Server Error' })
    );

    const explore = new ExplorePage(page);
    await explore.goto('categories');
    await explore.categoryButton('Drama').click();

    await expect(explore.errorMessage).toBeVisible();
  });

  test('shows empty state when no airings exist for a category', async ({ page, setupApiRoutes }) => {
    await setupApiRoutes({ '/api/search**': null });
    await page.route('/api/search**', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([]),
      })
    );

    const explore = new ExplorePage(page);
    await explore.goto('categories');
    await explore.categoryButton('Film').click();

    await expect(explore.categoryEmpty).toBeVisible();
  });

  test('shows results title indicating selected category', async ({ page, setupApiRoutes }) => {
    await setupApiRoutes({ '/api/search**': null });
    await page.route('/api/search**', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([]),
      })
    );

    const explore = new ExplorePage(page);
    await explore.goto('categories');
    await explore.categoryButton('Sport').click();

    await expect(explore.categoryResultsTitle).toContainText('Sport');
  });

  test('direct navigation to /explore?mode=categories&category=Sport shows results', async ({ page, setupApiRoutes }) => {
    await setupApiRoutes({ '/api/search**': null });
    await page.route('/api/search**', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            title: 'The Ashes',
            airings: [
              {
                channelId: 'ch2',
                channelName: 'SBS',
                startTime: '2025-06-10T22:00:00Z',
                stopTime: '2025-06-11T02:00:00Z',
                categories: ['Sport'],
                isRepeat: false,
                isPremiere: false,
              },
            ],
          },
        ]),
      })
    );

    const explore = new ExplorePage(page);
    await explore.goto();
    await page.goto('/explore?mode=categories&category=Sport');
    await explore.waitForAppReady();

    await expect(explore.categoryResults).toBeVisible();
    await expect(explore.categorySearchGroups.first()).toContainText('The Ashes');
  });

  test('shows error state when categories API fails', async ({ page, setupApiRoutes }) => {
    await setupApiRoutes({ '/api/categories': null });
    await page.route('/api/categories', (route) =>
      route.fulfill({ status: 500, body: 'Internal Server Error' })
    );

    const explore = new ExplorePage(page);
    await explore.goto('categories');

    await expect(explore.errorMessage).toBeVisible();
  });
});

test.describe('Explore tab — Now/Next mode', () => {
  test('renders a row per channel with data', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto();

    await expect(explore.nowNextRow('ch1')).toBeVisible();
    await expect(explore.nowNextRow('ch2')).toBeVisible();
    await expect(explore.nowNextRow('ch3')).toBeVisible();
    await expect(explore.nowNextRow('ch4')).toBeVisible();
  });

  test('shows channel name in each row', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto();

    await expect(explore.nowNextRow('ch1')).toContainText('ABC');
    await expect(explore.nowNextRow('ch2')).toContainText('SBS');
  });

  test('shows current show title with time remaining', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto();

    // ch1: "News at Noon", stops at 14:30, now is ~14:00 → ~30 min remaining
    await expect(explore.nowNextRow('ch1')).toContainText('News at Noon');
    await expect(explore.nowNextRow('ch1')).toContainText(/ends in \d+ min/);
  });

  test('shows next show title with start time', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto();

    // ch1 next: "Afternoon Show" starts at 14:30Z
    await expect(explore.nowNextRow('ch1')).toContainText('Afternoon Show');
  });

  test('shows "Nothing airing" when current is null', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto();

    // ch3 has no current show
    await expect(explore.nowNextRow('ch3')).toContainText('Nothing airing');
  });

  test('omits next section when next is null', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto();

    // ch4 has no next show — its row should exist but no "next" content
    const row = explore.nowNextRow('ch4');
    await expect(row).toBeVisible();
    await expect(row.locator('.now-next-next')).not.toBeVisible();
  });

  test('channels with both null current and null next are shown at the bottom', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto();

    // ch5 has no current and no next — should still appear but at the bottom
    const rows = explore.nowNextList.locator('.now-next-row');
    const count = await rows.count();
    const lastRow = rows.nth(count - 1);
    await expect(lastRow).toHaveAttribute('data-channel-id', 'ch5');
  });

  test('shows error state when API fails', async ({ page, setupApiRoutes }) => {
    await setupApiRoutes({ '/api/explore/now-next': null });
    await page.route('/api/explore/now-next', route =>
      route.fulfill({ status: 500, body: 'Internal Server Error' })
    );

    const explore = new ExplorePage(page);
    await explore.goto();

    await expect(explore.errorMessage).toBeVisible();
  });
});

test.describe('Explore tab — Browser back/forward', () => {
  test('browser back navigates from explore mode to previous state', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto();

    await explore.clickMode('premieres');
    expect(page.url()).toContain('mode=premieres');

    await page.goBack();

    // Should return to default mode (now-next)
    expect(page.url()).not.toContain('mode=premieres');
  });

  test('browser forward navigates forward between modes', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto();

    await explore.clickMode('categories');
    await page.goBack();

    await page.goForward();

    expect(page.url()).toContain('mode=categories');
    expect(await explore.activeMode()).toBe('categories');
  });
});
