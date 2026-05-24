import { test, expect } from '../fixtures';
import { GuidePage } from '../pages/GuidePage';

const PLEX_LINK = {
  webUrl: 'https://plex.example.com/web/index.html#!/livetv/tv.plex.providers.epg.cloud:4/channels/plex-ch1',
  appUrl: 'plex://livetv?lineup=tv.plex.providers.epg.cloud:4&channel=plex-ch1',
};

test.describe('Modal — Watch now buttons', () => {
  test('shows both Plex Web and Plex App buttons when the channel has a Plex mapping', async ({ page }) => {
    await page.route('**/api/channels/ch1/plex-link', (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(PLEX_LINK) })
    );

    const guide = new GuidePage(page);
    await guide.goto();
    await guide.clickProgramme('ABC Breakfast');
    await expect(guide.modal).toBeVisible();

    const watchWeb = page.locator('#modalWatchWeb');
    const watchApp = page.locator('#modalWatchApp');
    await expect(watchWeb).toBeVisible();
    await expect(watchApp).toBeVisible();
    await expect(watchWeb).toHaveAttribute('href', PLEX_LINK.webUrl);
    await expect(watchApp).toHaveAttribute('href', PLEX_LINK.appUrl);
  });

  test('hides Watch now buttons when the channel has no Plex mapping', async ({ page }) => {
    // 404 = no mapping / Plex unconfigured / unknown channel
    await page.route('**/api/channels/ch3/plex-link', (route) =>
      route.fulfill({ status: 404, contentType: 'text/plain', body: 'not found' })
    );

    const guide = new GuidePage(page);
    await guide.goto();
    await guide.clickProgramme('Sunrise');
    await expect(guide.modal).toBeVisible();

    // The Watch now container should remain hidden — the buttons must not be
    // surfaced when the channel can't be tuned via Plex.
    await expect(page.locator('#modalWatchNow')).toBeHidden();
  });

  test('does not leak buttons from a previous airing into one without a mapping', async ({ page }) => {
    await page.route('**/api/channels/ch1/plex-link', (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(PLEX_LINK) })
    );
    await page.route('**/api/channels/ch3/plex-link', (route) =>
      route.fulfill({ status: 404, contentType: 'text/plain', body: 'not found' })
    );

    const guide = new GuidePage(page);
    await guide.goto();

    // First open: ch1 → buttons visible
    await guide.clickProgramme('ABC Breakfast');
    await expect(page.locator('#modalWatchNow')).toBeVisible();
    await guide.closeModal();
    await expect(page.locator('#modalBackdrop')).not.toBeVisible();

    // Second open: ch3 → buttons hidden again
    await guide.clickProgramme('Sunrise');
    await expect(guide.modal).toBeVisible();
    await expect(page.locator('#modalWatchNow')).toBeHidden();
  });
});
