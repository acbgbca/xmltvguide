import { Page, Locator } from '@playwright/test';
import { AppPage } from './AppPage';

export class FavouritesPage extends AppPage {
  constructor(page: Page) {
    super(page);
  }

  async goto(): Promise<void> {
    await this.page.goto('/favourites');
    await this.waitForAppReady();
  }

  // Empty state
  get emptyMessage(): Locator {
    return this.page.locator('#favouritesEmpty');
  }

  // Loading
  get loadingSpinner(): Locator {
    return this.page.locator('#favouritesLoading');
  }

  // Saved search groups
  favGroups(): Locator {
    return this.page.locator('.fav-group');
  }

  favGroupByName(name: string): Locator {
    return this.page.locator('.fav-group').filter({
      has: this.page.locator('.fav-group-name', { hasText: name }),
    });
  }

  noResultsMessageInGroup(name: string): Locator {
    return this.favGroupByName(name).locator('.fav-no-results');
  }

  titleGroupsInFav(favName: string): Locator {
    return this.favGroupByName(favName).locator('.fav-title-group');
  }

  airingsInTitleGroup(favName: string, title: string): Locator {
    return this.favGroupByName(favName)
      .locator('.fav-title-group')
      .filter({ has: this.page.locator('.fav-title-name', { hasText: title }) })
      .locator('.fav-airing');
  }

  // Actions
  async clickEdit(favName: string): Promise<void> {
    await this.favGroupByName(favName).locator('.fav-action-btn', { hasText: 'Edit' }).click();
  }

  async clickDelete(favName: string): Promise<void> {
    await this.favGroupByName(favName).locator('.fav-delete-btn').click();
  }

  // Airing click → modal
  async clickAiring(favName: string, index = 0): Promise<void> {
    await this.favGroupByName(favName).locator('.fav-airing').nth(index).click();
    await this.page.locator('#programmeModal').waitFor({ state: 'visible' });
  }

  // Wait helpers
  async waitForSearchesComplete(): Promise<void> {
    await this.page.locator('#favouritesLoading').waitFor({ state: 'hidden' });
  }
}
