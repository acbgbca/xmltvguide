import { test, expect } from '../fixtures/index';
import { GuidePage } from '../pages/GuidePage';
import guideTomorrowFixture from '../fixtures/api/guide-tomorrow.json';
import guideYesterdayFixture from '../fixtures/api/guide-yesterday.json';
import guideEmptyFixture from '../fixtures/api/guide-empty.json';

// FIXED_NOW = 2025-06-10T14:00:00.000Z (Tuesday, 14:00 UTC)

test.describe('Guide tab — Rendering', () => {
  test('renders channel names in LCN order', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    const labels = guide.channelLabels();
    const names = await labels.evaluateAll((els) =>
      els.map((el) => el.textContent?.trim() ?? '')
    );

    // Channels sorted by LCN: ABC(2), SBS(3), Seven(7), Nine(9), Ten(10)
    expect(names[0]).toContain('ABC');
    expect(names[1]).toContain('SBS');
    expect(names[2]).toContain('Seven');
    expect(names[3]).toContain('Nine');
    expect(names[4]).toContain('Ten');
  });

  test('renders programme cells for each channel', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    const count = await guide.programmeCells().count();
    expect(count).toBeGreaterThan(0);
  });

  test('programme cell shows title text', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    const cell = guide.programmeCellByTitle('ABC Evening News');
    await expect(cell.first().locator('.prog-title')).toContainText('ABC Evening News');
  });

  test('date display shows the fixed date', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    // formatDateLong('2025-06-10') => something like "Tue, Jun 10" or "Tue 10 Jun"
    const text = await guide.dateDisplay.textContent();
    expect(text).toContain('Jun');
    expect(text).toMatch(/10/);
  });
});

test.describe('Guide tab — Now-line', () => {
  test('now-line is visible on today', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    expect(await guide.isNowLineVisible()).toBe(true);
  });

  test('now-line is positioned at current time', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    // FIXED_NOW = 14:00 UTC => 840 min from midnight => 840 * 4 = 3360px
    const left = await guide.nowLineLeftPx();
    expect(left).toBeGreaterThanOrEqual(3358);
    expect(left).toBeLessThanOrEqual(3362);
  });

  test('now-line is hidden when viewing another date', async ({ page, setupApiRoutes }) => {
    await setupApiRoutes({
      '/api/guide**': guideTomorrowFixture,
    });

    const guide = new GuidePage(page);
    await guide.goto();
    await guide.clickNextDay();
    await page.waitForTimeout(500);

    expect(await guide.isNowLineVisible()).toBe(false);
  });
});

test.describe('Guide tab — Date navigation', () => {
  test('next day button loads tomorrow\'s guide', async ({ page, setupApiRoutes }) => {
    await setupApiRoutes({
      '/api/guide**': guideTomorrowFixture,
    });

    const guide = new GuidePage(page);
    await guide.goto();
    await guide.clickNextDay();

    await expect(guide.dateDisplay).toContainText('11');
    const cell = guide.programmeCellByTitle('ABC Overnight');
    await expect(cell.first()).toBeVisible();
  });

  test('prev day button loads yesterday\'s guide', async ({ page, setupApiRoutes }) => {
    await setupApiRoutes({
      '/api/guide**': guideYesterdayFixture,
    });

    const guide = new GuidePage(page);
    await guide.goto();
    await guide.clickPrevDay();

    await expect(guide.dateDisplay).toContainText('9');
    const cell = guide.programmeCellByTitle('ABC Yesterday Special');
    await expect(cell.first()).toBeVisible();
  });
});

test.describe('Guide tab — Empty state', () => {
  test('shows empty state when guide has no data', async ({ page, setupApiRoutes }) => {
    await setupApiRoutes({
      '/api/guide**': guideEmptyFixture,
    });

    const guide = new GuidePage(page);
    await guide.goto();

    expect(await guide.isEmptyStateVisible()).toBe(true);
  });

  test('does not show empty state when airings exist', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    expect(await guide.isEmptyStateVisible()).toBe(false);
  });
});

test.describe('Guide tab — Modal', () => {
  test('clicking a programme opens the detail modal', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    await guide.clickProgramme('ABC Evening News');

    await expect(guide.modal).toBeVisible();
    await expect(guide.modalTitle).toContainText('ABC Evening News');
  });

  test('modal shows description when present', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    // "Afternoon Specials" has a description in the fixture
    await guide.clickProgramme('Afternoon Specials');

    const desc = await guide.modalDesc.textContent();
    expect(desc?.trim().length).toBeGreaterThan(0);
  });

  test('modal closes when close button is clicked', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    await guide.clickProgramme('ABC Evening News');
    await expect(guide.modal).toBeVisible();

    await page.locator('#modalClose').click();
    await expect(page.locator('#modalBackdrop')).not.toBeVisible();
  });

  test('modal closes when backdrop is clicked', async ({ page }) => {
    const guide = new GuidePage(page);
    await guide.goto();

    await guide.clickProgramme('ABC Evening News');
    await expect(guide.modal).toBeVisible();

    await page.locator('#modalBackdrop').click({ position: { x: 5, y: 5 } });
    await expect(page.locator('#modalBackdrop')).not.toBeVisible();
  });
});

test.describe('Guide tab — Channel ordering with preferences', () => {
  test('favourite channels appear first', async ({ page, seedLocalStorage }) => {
    await seedLocalStorage('tvguide-prefs', { hidden: {}, favourites: { ch3: true } });

    const guide = new GuidePage(page);
    await guide.goto();

    const labels = guide.channelLabels();
    const first = labels.first();
    await expect(first).toContainText('Seven');
  });

  test('hidden channels are excluded', async ({ page, seedLocalStorage }) => {
    await seedLocalStorage('tvguide-prefs', { hidden: { ch2: true }, favourites: {} });

    const guide = new GuidePage(page);
    await guide.goto();

    const sbsLabel = guide.channelLabelByName('SBS');
    await expect(sbsLabel).toHaveCount(0);
  });
});
