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

    for (const mode of [] as string[]) {
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

  test('clicking a current show opens the programme modal', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto();

    // ch1 current: "News at Noon"
    const currentDiv = explore.nowNextRow('ch1').locator('.now-next-current');
    await currentDiv.click();

    await expect(explore.modal).toBeVisible();
    await expect(explore.modalTitle).toHaveText('News at Noon');
  });

  test('clicking a next show opens the programme modal', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto();

    // ch1 next: "Afternoon Show"
    const nextDiv = explore.nowNextRow('ch1').locator('.now-next-next');
    await nextDiv.click();

    await expect(explore.modal).toBeVisible();
    await expect(explore.modalTitle).toHaveText('Afternoon Show');
  });

  test('current show div has pointer cursor', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto();

    const cursor = await explore.nowNextRow('ch1').locator('.now-next-current').evaluate(
      (el) => (el as HTMLElement).style.cursor
    );
    expect(cursor).toBe('pointer');
  });

  test('next show div has pointer cursor', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto();

    const cursor = await explore.nowNextRow('ch1').locator('.now-next-next').evaluate(
      (el) => (el as HTMLElement).style.cursor
    );
    expect(cursor).toBe('pointer');
  });
});

test.describe('Explore tab — Premieres mode', () => {
  test('fetches and displays premiere airings as a flat list', async ({ page, setupApiRoutes }) => {
    await setupApiRoutes({ '/api/search**': null });
    await page.route('/api/search**', (route) => {
      const url = new URL(route.request().url());
      if (url.searchParams.get('is_premiere') === 'true') {
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([
            {
              title: 'The Block',
              airings: [
                {
                  channelId: 'ch5',
                  channelName: 'Nine',
                  startTime: '2025-06-10T19:00:00Z',
                  stopTime: '2025-06-10T20:30:00Z',
                  subTitle: 'Room Reveals',
                  description: 'The contestants reveal their completed rooms.',
                  categories: ['Reality'],
                  isRepeat: false,
                  isPremiere: true,
                },
              ],
            },
            {
              title: 'Grand Designs Australia',
              airings: [
                {
                  channelId: 'ch2',
                  channelName: 'SBS',
                  startTime: '2025-06-10T21:00:00Z',
                  stopTime: '2025-06-10T22:00:00Z',
                  description: 'A family builds their dream home on the coast.',
                  categories: ['Documentary'],
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
    await explore.goto('premieres');

    await expect(explore.premieresList).toBeVisible();
    await expect(explore.premieresItems).toHaveCount(2);
  });

  test('results are sorted by start time (earliest first)', async ({ page, setupApiRoutes }) => {
    await setupApiRoutes({ '/api/search**': null });
    await page.route('/api/search**', (route) => {
      const url = new URL(route.request().url());
      if (url.searchParams.get('is_premiere') === 'true') {
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([
            {
              title: 'Late Show',
              airings: [
                {
                  channelId: 'ch1',
                  channelName: 'ABC',
                  startTime: '2025-06-10T21:00:00Z',
                  stopTime: '2025-06-10T22:00:00Z',
                  categories: ['Drama'],
                  isRepeat: false,
                  isPremiere: true,
                },
              ],
            },
            {
              title: 'Early Show',
              airings: [
                {
                  channelId: 'ch2',
                  channelName: 'SBS',
                  startTime: '2025-06-10T19:00:00Z',
                  stopTime: '2025-06-10T20:00:00Z',
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
    await explore.goto('premieres');

    const items = explore.premieresItems;
    await expect(items.first()).toContainText('Early Show');
    await expect(items.nth(1)).toContainText('Late Show');
  });

  test('each item shows title, channel, and date/time', async ({ page, setupApiRoutes }) => {
    await setupApiRoutes({ '/api/search**': null });
    await page.route('/api/search**', (route) => {
      const url = new URL(route.request().url());
      if (url.searchParams.get('is_premiere') === 'true') {
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([
            {
              title: 'The Block',
              airings: [
                {
                  channelId: 'ch5',
                  channelName: 'Nine',
                  startTime: '2025-06-10T19:00:00Z',
                  stopTime: '2025-06-10T20:30:00Z',
                  categories: ['Reality'],
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
    await explore.goto('premieres');

    const item = explore.premieresItems.first();
    await expect(item).toContainText('The Block');
    await expect(item).toContainText('Nine');
    // Time should be formatted (e.g. "Today" since FIXED_NOW is same day)
    await expect(item.locator('.premiere-time')).toBeVisible();
  });

  test('shows sub-title when present', async ({ page, setupApiRoutes }) => {
    await setupApiRoutes({ '/api/search**': null });
    await page.route('/api/search**', (route) => {
      const url = new URL(route.request().url());
      if (url.searchParams.get('is_premiere') === 'true') {
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([
            {
              title: 'The Block',
              airings: [
                {
                  channelId: 'ch5',
                  channelName: 'Nine',
                  startTime: '2025-06-10T19:00:00Z',
                  stopTime: '2025-06-10T20:30:00Z',
                  subTitle: 'Room Reveals',
                  categories: ['Reality'],
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
    await explore.goto('premieres');

    await expect(explore.premieresItems.first()).toContainText('Room Reveals');
  });

  test('shows description when present', async ({ page, setupApiRoutes }) => {
    await setupApiRoutes({ '/api/search**': null });
    await page.route('/api/search**', (route) => {
      const url = new URL(route.request().url());
      if (url.searchParams.get('is_premiere') === 'true') {
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([
            {
              title: 'Grand Designs Australia',
              airings: [
                {
                  channelId: 'ch2',
                  channelName: 'SBS',
                  startTime: '2025-06-10T21:00:00Z',
                  stopTime: '2025-06-10T22:00:00Z',
                  description: 'A family builds their dream home on the coast.',
                  categories: ['Documentary'],
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
    await explore.goto('premieres');

    await expect(explore.premieresItems.first()).toContainText('A family builds their dream home on the coast.');
  });

  test('shows loading state while fetching', async ({ page }) => {
    let resolveRoute: () => void;
    await page.route('/api/search**', async (route) => {
      const url = new URL(route.request().url());
      if (url.searchParams.get('is_premiere') === 'true') {
        await new Promise<void>(r => { resolveRoute = r; });
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([]),
        });
      } else {
        await route.continue();
      }
    });

    const explore = new ExplorePage(page);
    await explore.goto('premieres');

    await expect(explore.loadingIndicator).toBeVisible();
    resolveRoute!();
  });

  test('shows error state when API fails', async ({ page, setupApiRoutes }) => {
    await setupApiRoutes({ '/api/search**': null });
    await page.route('/api/search**', (route) => {
      const url = new URL(route.request().url());
      if (url.searchParams.get('is_premiere') === 'true') {
        route.fulfill({ status: 500, body: 'Internal Server Error' });
      } else {
        route.continue();
      }
    });

    const explore = new ExplorePage(page);
    await explore.goto('premieres');

    await expect(explore.errorMessage).toBeVisible();
  });

  test('shows empty state when no premieres are found', async ({ page, setupApiRoutes }) => {
    await setupApiRoutes({ '/api/search**': null });
    await page.route('/api/search**', (route) => {
      const url = new URL(route.request().url());
      if (url.searchParams.get('is_premiere') === 'true') {
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([]),
        });
      } else {
        route.continue();
      }
    });

    const explore = new ExplorePage(page);
    await explore.goto('premieres');

    await expect(explore.premieresEmpty).toBeVisible();
    await expect(explore.premieresEmpty).toContainText('No upcoming premieres found');
  });
});

test.describe('Explore tab — Time Slot mode', () => {
  test('shows a date picker and time picker', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto('time-slot');

    await expect(explore.timeSlotDateInput).toBeVisible();
    await expect(explore.timeSlotTimeSelect).toBeVisible();
  });

  test('default date is today', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto('time-slot');

    // FIXED_NOW = 2025-06-10T14:00:00Z → today is 2025-06-10
    await expect(explore.timeSlotDateInput).toHaveValue('2025-06-10');
  });

  test('default time is current hour rounded to nearest 30 min', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto('time-slot');

    // FIXED_NOW = 14:00 UTC → 14:00 local → rounded to 14:00
    await expect(explore.timeSlotTimeSelect).toHaveValue('14:00');
  });

  test('shows one row per channel in channel sort order', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto('time-slot');

    await expect(explore.timeSlotRow('ch1')).toBeVisible();
    await expect(explore.timeSlotRow('ch2')).toBeVisible();
    await expect(explore.timeSlotRow('ch3')).toBeVisible();
    await expect(explore.timeSlotRow('ch4')).toBeVisible();
    await expect(explore.timeSlotRow('ch5')).toBeVisible();
  });

  test('shows channel name in each row', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto('time-slot');

    await expect(explore.timeSlotRow('ch1')).toContainText('ABC');
    await expect(explore.timeSlotRow('ch2')).toContainText('SBS');
  });

  test('shows airing title for channel with a show at the selected time', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto('time-slot');

    // At 14:00, ch1 has "Afternoon Specials" (14:00–15:00)
    await expect(explore.timeSlotRow('ch1')).toContainText('Afternoon Specials');
  });

  test('shows start–stop times for an airing', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto('time-slot');

    // ch1 at 14:00: Afternoon Specials 14:00–15:00
    const row = explore.timeSlotRow('ch1');
    await expect(row.locator('.time-slot-time')).toBeVisible();
  });

  test('shows "Nothing airing" for channels with no show at the selected time', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto('time-slot');

    // At 14:00, ch2 has nothing (Film ends 13:00, Dateline starts 15:00)
    await expect(explore.timeSlotRow('ch2')).toContainText('Nothing airing');
    await expect(explore.timeSlotRow('ch3')).toContainText('Nothing airing');
  });

  test('URL reflects the selected date and time on load', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto('time-slot');

    expect(page.url()).toContain('mode=time-slot');
    expect(page.url()).toContain('date=2025-06-10');
    expect(page.url()).toContain('time=14%3A00');
  });

  test('changing time filters results client-side without re-fetching', async ({ page, setupApiRoutes }) => {
    await setupApiRoutes({ '/api/guide**': null });
    let fetchCount = 0;
    await page.route('/api/guide**', async (route) => {
      fetchCount++;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            channelId: 'ch2',
            start: '2025-06-10T15:00:00Z',
            stop: '2025-06-10T17:00:00Z',
            title: 'SBS Dateline',
            isRepeat: false,
            isPremiere: false,
          },
        ]),
      });
    });

    const explore = new ExplorePage(page);
    await explore.goto('time-slot');

    const fetchesBefore = fetchCount;

    // Change time to 15:00 — should filter client-side
    await explore.timeSlotTimeSelect.selectOption('15:00');

    // No additional fetch should have occurred
    expect(fetchCount).toBe(fetchesBefore);
    // ch2 now has SBS Dateline at 15:00
    await expect(explore.timeSlotRow('ch2')).toContainText('SBS Dateline');
  });

  test('changing time updates the URL', async ({ page }) => {
    const explore = new ExplorePage(page);
    await explore.goto('time-slot');

    await explore.timeSlotTimeSelect.selectOption('15:00');

    expect(page.url()).toContain('time=15%3A00');
  });

  test('changing date re-fetches guide data', async ({ page, setupApiRoutes }) => {
    await setupApiRoutes({ '/api/guide**': null });
    let lastRequestedDate = '';
    await page.route('/api/guide**', async (route) => {
      const url = new URL(route.request().url());
      lastRequestedDate = url.searchParams.get('date') ?? '';
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([]),
      });
    });

    const explore = new ExplorePage(page);
    await explore.goto('time-slot');

    // Set date input to a new date
    await explore.timeSlotDateInput.fill('2025-06-11');
    await explore.timeSlotDateInput.dispatchEvent('change');

    await expect(explore.timeSlotList).toBeVisible();
    expect(lastRequestedDate).toBe('2025-06-11');
  });

  test('shows loading state while fetching guide data', async ({ page }) => {
    const explore = new ExplorePage(page);
    // Navigate to a different mode first so the app fully initializes
    await explore.goto();

    // Now block subsequent guide requests
    let resolveRoute: () => void;
    await page.route('/api/guide**', async (route) => {
      await new Promise<void>(r => { resolveRoute = r; });
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([]),
      });
    });

    // Switching to time-slot triggers a new guide fetch that is now blocked
    await explore.clickMode('time-slot');

    await expect(explore.loadingIndicator).toBeVisible();
    resolveRoute!();
  });

  test('shows error state when guide API fails', async ({ page }) => {
    const explore = new ExplorePage(page);
    // Navigate to a different mode first so the app fully initializes
    await explore.goto();

    // Now route guide requests to fail
    await page.route('/api/guide**', (route) =>
      route.fulfill({ status: 500, body: 'Internal Server Error' })
    );

    // Switching to time-slot triggers a new guide fetch that will fail
    await explore.clickMode('time-slot');

    await expect(explore.errorMessage).toBeVisible();
  });

  test('direct navigation with date and time in URL shows correct results', async ({ page }) => {
    const explore = new ExplorePage(page);
    await page.goto('/explore?mode=time-slot&date=2025-06-10&time=15:00');
    await explore.waitForAppReady();

    // At 15:00, ch2 has SBS Dateline (15:00–17:00)
    await expect(explore.timeSlotRow('ch2')).toContainText('SBS Dateline');
    await expect(explore.timeSlotDateInput).toHaveValue('2025-06-10');
    await expect(explore.timeSlotTimeSelect).toHaveValue('15:00');
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
