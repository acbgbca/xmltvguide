import { Page, Locator } from '@playwright/test';
import { AppPage } from './AppPage';

export class SearchPage extends AppPage {
  constructor(page: Page) {
    super(page);
  }

  async goto(): Promise<void> {
    await this.page.goto('/search');
    await this.waitForAppReady();
  }

  // Input and controls
  get searchInput(): Locator {
    return this.page.locator('#searchInput');
  }

  get searchHint(): Locator {
    return this.page.locator('#searchHint');
  }

  get searchSpinner(): Locator {
    return this.page.locator('#searchSpinner');
  }

  get clearButton(): Locator {
    return this.page.locator('#searchClear');
  }

  async typeQuery(q: string): Promise<void> {
    await this.searchInput.fill(q);
    // Wait for debounce (300ms) plus results to appear or spinner to disappear
    await this.page.waitForTimeout(350);
    await this.searchSpinner.waitFor({ state: 'hidden' });
    await this.page.locator('#searchResults').waitFor({ state: 'attached' });
  }

  // Advanced options
  get advancedToggle(): Locator {
    return this.page.locator('#advancedToggle');
  }

  async openAdvancedOptions(): Promise<void> {
    const panel = this.page.locator('#advancedOptions');
    const isVisible = await panel.evaluate((el: HTMLElement) => el.style.display !== 'none');
    if (!isVisible) {
      await this.advancedToggle.click();
    }
  }

  async toggleSearchDescriptions(): Promise<void> {
    await this.page.locator('#searchDescriptions').click();
  }

  async toggleIncludePast(): Promise<void> {
    await this.page.locator('#includePast').click();
  }

  async toggleHideRepeats(): Promise<void> {
    await this.page.locator('#hideRepeats').click();
  }

  // Category chips
  categoryChips(): Locator {
    return this.page.locator('#categoryChips .category-chip');
  }

  categoryChipByName(name: string): Locator {
    return this.page.locator('#categoryChips .category-chip').filter({ hasText: name });
  }

  async selectCategory(name: string): Promise<void> {
    await this.categoryChipByName(name).click();
  }

  // Results
  resultGroups(): Locator {
    return this.page.locator('#searchResults .search-group');
  }

  resultGroupByTitle(title: string): Locator {
    return this.page.locator('#searchResults .search-group').filter({
      has: this.page.locator('.search-group-title', { hasText: title }),
    });
  }

  airingsInGroup(title: string): Locator {
    return this.resultGroupByTitle(title).locator('.search-airing');
  }

  async clickAiring(groupTitle: string, index = 0): Promise<void> {
    await this.airingsInGroup(groupTitle).nth(index).click();
    await this.page.locator('#programmeModal').waitFor({ state: 'visible' });
  }

  // Star / favourite
  starButtonForGroup(title: string): Locator {
    return this.resultGroupByTitle(title).locator('.search-fav-btn');
  }

  async isGroupSaved(title: string): Promise<boolean> {
    const text = await this.starButtonForGroup(title).textContent();
    return text?.includes('\u2605') ?? false; // ★
  }

  async clickStarForGroup(title: string): Promise<void> {
    await this.starButtonForGroup(title).click();
  }

  // Empty state
  get emptyMessage(): Locator {
    return this.page.locator('#searchResults .search-empty');
  }
}
