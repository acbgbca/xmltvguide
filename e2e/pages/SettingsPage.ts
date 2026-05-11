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

  // Section containers
  channelsSection(): Locator {
    return this.page.locator('.settings-section.channels-section');
  }

  advancedSection(): Locator {
    return this.page.locator('.settings-accordion.advanced-section');
  }

  advancedHeader(): Locator {
    return this.advancedSection().locator('.settings-accordion-header');
  }

  advancedBody(): Locator {
    return this.advancedSection().locator('.settings-accordion-body');
  }

  async isAdvancedExpanded(): Promise<boolean> {
    return this.advancedSection().evaluate((el) =>
      el.classList.contains('is-expanded')
    );
  }

  async toggleAdvanced(): Promise<void> {
    await this.advancedHeader().click();
  }

  // Refresh-guide action
  refreshButton(): Locator {
    return this.page.locator('.settings-action-refresh');
  }

  refreshSpinner(): Locator {
    return this.page.locator('.settings-action-refresh-wrap .loading-spinner');
  }

  refreshStatus(): Locator {
    return this.page.locator('.settings-status');
  }

  async clickRefresh(): Promise<void> {
    await this.refreshButton().click();
  }

  // Status block
  statusBlock(): Locator {
    return this.page.locator('.settings-status-block');
  }

  statusRow(label: string): Locator {
    return this.page.locator('.settings-status-row').filter({
      has: this.page.locator('.settings-status-label', { hasText: label }),
    });
  }

  statusValue(label: string): Locator {
    return this.statusRow(label).locator('.settings-status-value');
  }

  statusUnavailable(): Locator {
    return this.page.locator('.settings-status-unavailable');
  }

  // Reset preferences action
  resetButton(): Locator {
    return this.page.locator('.settings-action-reset');
  }

  async clickReset(): Promise<void> {
    await this.resetButton().click();
  }

  // Deep / system check action
  deepCheckButton(): Locator {
    return this.page.locator('.settings-action-deepcheck');
  }

  deepCheckSpinner(): Locator {
    return this.page
      .locator('.settings-action-deepcheck-wrap .loading-spinner');
  }

  deepCheckResults(): Locator {
    return this.page.locator('.settings-deepcheck-results');
  }

  deepCheckSummary(): Locator {
    return this.page.locator('.settings-deepcheck-summary');
  }

  deepCheckRows(): Locator {
    return this.page.locator('.settings-deepcheck-row');
  }

  deepCheckRowByName(name: string): Locator {
    return this.page.locator('.settings-deepcheck-row').filter({
      has: this.page.locator('.settings-deepcheck-name', { hasText: name }),
    });
  }

  async clickDeepCheck(): Promise<void> {
    await this.deepCheckButton().click();
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
