import { Page, Locator } from '@playwright/test';
import { AppPage } from './AppPage';

export class SettingsPage extends AppPage {
  constructor(page: Page) {
    super(page);
  }

  async goto(): Promise<void> {
    await this.page.goto('/settings');
    await this.waitForAppReady();
  }

  // Channel rows
  channelItems(): Locator {
    return this.page.locator('.settings-item');
  }

  channelItemByName(name: string): Locator {
    return this.page.locator('.settings-item').filter({
      has: this.page.locator('.settings-name', { hasText: name }),
    });
  }

  // Favourite toggle
  favButtonForChannel(name: string): Locator {
    return this.channelItemByName(name).locator('.fav-btn');
  }

  async isFavourite(name: string): Promise<boolean> {
    return this.favButtonForChannel(name).evaluate((el) =>
      el.classList.contains('active')
    );
  }

  async toggleFavourite(name: string): Promise<void> {
    await this.favButtonForChannel(name).click();
  }

  // Hide toggle
  hideButtonForChannel(name: string): Locator {
    return this.channelItemByName(name).locator('.hide-btn');
  }

  async isHidden(name: string): Promise<boolean> {
    return this.hideButtonForChannel(name).evaluate((el) =>
      el.classList.contains('active')
    );
  }

  async toggleHide(name: string): Promise<void> {
    await this.hideButtonForChannel(name).click();
  }

  // Row state helpers
  async isItemHiddenStyling(name: string): Promise<boolean> {
    return this.channelItemByName(name).evaluate((el) =>
      el.classList.contains('is-hidden')
    );
  }
}
