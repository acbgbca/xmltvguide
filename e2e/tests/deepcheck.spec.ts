import { test, expect } from '../fixtures/index';
import { SettingsPage } from '../pages/SettingsPage';

// FIXED_NOW = 2025-06-10T14:00:00.000Z

const SUCCESS_REPORT = {
  status: 'SUCCESS',
  checks: [
    { name: 'database', status: 'SUCCESS' },
    { name: 'database_writable', status: 'SUCCESS' },
    { name: 'fts', status: 'SUCCESS' },
    { name: 'data_presence', status: 'SUCCESS', info: 'channels=5 airings=120' },
    { name: 'data_freshness', status: 'SUCCESS', info: 'last_refresh=2025-06-10T13:00:00Z' },
    { name: 'xmltv_url', status: 'SUCCESS', info: 'status=200' },
    { name: 'disk_data', status: 'SUCCESS', info: 'mode=0755' },
    { name: 'disk_tmp', status: 'SUCCESS', info: 'mode=0755' },
    { name: 'image_cache', status: 'SUCCESS', info: 'mode=0755' },
  ],
};

const FAILURE_REPORT = {
  status: 'FAILURE',
  checks: [
    { name: 'database', status: 'SUCCESS' },
    { name: 'database_writable', status: 'SUCCESS' },
    { name: 'fts', status: 'SUCCESS' },
    { name: 'data_presence', status: 'SUCCESS', info: 'channels=5 airings=120' },
    { name: 'data_freshness', status: 'SUCCESS', info: 'last_refresh=2025-06-10T13:00:00Z' },
    {
      name: 'xmltv_url',
      status: 'FAILURE',
      error: 'HEAD https://example/xmltv: connection refused',
    },
    { name: 'disk_data', status: 'SUCCESS', info: 'mode=0755' },
    { name: 'disk_tmp', status: 'SUCCESS', info: 'mode=0755' },
    { name: 'image_cache', status: 'SUCCESS', info: 'mode=0755' },
  ],
};

test.describe('Settings tab — Run system check action', () => {
  test('button is rendered inside the Advanced section', async ({ page }) => {
    const settings = new SettingsPage(page);
    await settings.goto();
    await settings.toggleAdvanced();

    const btn = settings.deepCheckButton();
    await expect(btn).toBeVisible();
    await expect(btn).toContainText(/run system check/i);
  });

  test('results panel is hidden until the first run', async ({ page }) => {
    const settings = new SettingsPage(page);
    await settings.goto();
    await settings.toggleAdvanced();

    await expect(settings.deepCheckResults()).toBeHidden();
  });

  test('all checks pass: shows summary and nine ✓ rows', async ({ page }) => {
    await page.route('/api/deepcheck', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(SUCCESS_REPORT),
      })
    );

    const settings = new SettingsPage(page);
    await settings.goto();
    await settings.toggleAdvanced();

    await settings.clickDeepCheck();

    const summary = settings.deepCheckSummary();
    await expect(summary).toBeVisible();
    await expect(summary).toHaveText(/all checks passed/i);
    await expect(summary).toHaveClass(/is-success/);

    await expect(settings.deepCheckRows()).toHaveCount(9);

    // Every row should have a ✓ icon
    const icons = settings.deepCheckRows().locator('.settings-deepcheck-icon');
    const iconCount = await icons.count();
    for (let i = 0; i < iconCount; i++) {
      const text = (await icons.nth(i).textContent()) ?? '';
      expect(text.trim()).toBe('✓');
      await expect(icons.nth(i)).toHaveClass(/is-success/);
    }

    // The XMLTV row uses the humanised label "XMLTV source"
    await expect(settings.deepCheckRowByName('XMLTV source')).toBeVisible();
    // And it should include its info text
    await expect(
      settings.deepCheckRowByName('XMLTV source').locator('.settings-deepcheck-info')
    ).toContainText('status=200');

    // No error blocks should be present
    await expect(page.locator('.settings-deepcheck-error')).toHaveCount(0);

    // Button is re-enabled after success
    await expect(settings.deepCheckButton()).toBeEnabled();
  });

  test('one check fails: summary in error colour and ✗ row with error text', async ({ page }) => {
    await page.route('/api/deepcheck', (route) =>
      route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify(FAILURE_REPORT),
      })
    );

    const settings = new SettingsPage(page);
    await settings.goto();
    await settings.toggleAdvanced();

    await settings.clickDeepCheck();

    const summary = settings.deepCheckSummary();
    await expect(summary).toBeVisible();
    await expect(summary).toHaveText(/1 of 9 checks failed/);
    await expect(summary).toHaveClass(/is-error/);

    const failingRow = settings.deepCheckRowByName('XMLTV source');
    await expect(failingRow).toBeVisible();
    const icon = failingRow.locator('.settings-deepcheck-icon');
    await expect(icon).toHaveText('✗');
    await expect(icon).toHaveClass(/is-error/);

    const errorEl = failingRow.locator('.settings-deepcheck-error');
    await expect(errorEl).toBeVisible();
    await expect(errorEl).toContainText('HEAD https://example/xmltv: connection refused');
  });

  test('network failure shows a single graceful error row', async ({ page }) => {
    await page.route('/api/deepcheck', (route) => route.abort('failed'));

    const settings = new SettingsPage(page);
    await settings.goto();
    await settings.toggleAdvanced();

    await settings.clickDeepCheck();

    const results = settings.deepCheckResults();
    await expect(results).toBeVisible();
    await expect(results).toContainText(/system check could not be run/i);

    // No summary line because we never got a report
    await expect(settings.deepCheckSummary()).toHaveCount(0);

    // Button is re-enabled even on failure
    await expect(settings.deepCheckButton()).toBeEnabled();
  });

  test('button is disabled and spinner is visible while the request is in flight', async ({
    page,
  }) => {
    await page.route('/api/deepcheck', async (route) => {
      await new Promise((resolve) => setTimeout(resolve, 400));
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(SUCCESS_REPORT),
      });
    });

    const settings = new SettingsPage(page);
    await settings.goto();
    await settings.toggleAdvanced();

    // Don't await — let the click resolve in the background
    const clickPromise = settings.clickDeepCheck();

    await expect(settings.deepCheckButton()).toBeDisabled();
    await expect(settings.deepCheckSpinner()).toBeVisible();

    await clickPromise;

    await expect(settings.deepCheckButton()).toBeEnabled();
  });

  test('re-running replaces previous results', async ({ page }) => {
    let callCount = 0;
    await page.route('/api/deepcheck', (route) => {
      callCount += 1;
      const body = callCount === 1 ? SUCCESS_REPORT : FAILURE_REPORT;
      const status = callCount === 1 ? 200 : 503;
      route.fulfill({
        status,
        contentType: 'application/json',
        body: JSON.stringify(body),
      });
    });

    const settings = new SettingsPage(page);
    await settings.goto();
    await settings.toggleAdvanced();

    await settings.clickDeepCheck();
    await expect(settings.deepCheckSummary()).toHaveText(/all checks passed/i);

    await settings.clickDeepCheck();
    await expect(settings.deepCheckSummary()).toHaveText(/1 of 9 checks failed/);

    // Still exactly nine rows after the rerun, not eighteen
    await expect(settings.deepCheckRows()).toHaveCount(9);
  });

  test('results are cleared when the Advanced accordion is collapsed', async ({ page }) => {
    await page.route('/api/deepcheck', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(SUCCESS_REPORT),
      })
    );

    const settings = new SettingsPage(page);
    await settings.goto();
    await settings.toggleAdvanced();

    await settings.clickDeepCheck();
    await expect(settings.deepCheckSummary()).toBeVisible();

    // Collapse the accordion
    await settings.toggleAdvanced();
    expect(await settings.isAdvancedExpanded()).toBe(false);

    // Re-expand — the panel should be hidden again (cleared)
    await settings.toggleAdvanced();
    await expect(settings.deepCheckResults()).toBeHidden();
  });
});
