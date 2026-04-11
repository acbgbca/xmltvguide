import { Page, Locator } from '@playwright/test';
import { AppPage } from './AppPage';

export class ExplorePage extends AppPage {
  constructor(page: Page) {
    super(page);
  }

  async goto(mode?: string): Promise<void> {
    const url = mode ? `/explore?mode=${mode}` : '/explore';
    await this.page.goto(url);
    await this.waitForAppReady();
  }

  // Page container
  get pageContainer(): Locator {
    return this.page.locator('#page-explore');
  }

  // Mode switcher
  get modeSwitcher(): Locator {
    return this.page.locator('.explore-mode-switcher');
  }

  modeButton(mode: string): Locator {
    return this.page.locator(`.explore-mode-btn[data-mode="${mode}"]`);
  }

  get activeModeButton(): Locator {
    return this.page.locator('.explore-mode-btn.active');
  }

  // Content area
  get contentArea(): Locator {
    return this.page.locator('.explore-content');
  }

  async clickMode(mode: string): Promise<void> {
    await this.modeButton(mode).click();
  }

  async activeMode(): Promise<string> {
    return (await this.activeModeButton.getAttribute('data-mode')) ?? '';
  }

  // Now/Next mode
  get nowNextList(): Locator {
    return this.page.locator('.now-next-list');
  }

  nowNextRow(channelId: string): Locator {
    return this.page.locator(`.now-next-row[data-channel-id="${channelId}"]`);
  }

  get loadingIndicator(): Locator {
    return this.page.locator('.explore-loading');
  }

  get errorMessage(): Locator {
    return this.page.locator('.explore-error');
  }

  // Categories mode
  get categoryPicker(): Locator {
    return this.page.locator('.category-picker');
  }

  categoryButton(name: string): Locator {
    return this.page.locator(`.category-picker-btn[data-category="${name}"]`);
  }

  get categoryResults(): Locator {
    return this.page.locator('.category-results');
  }

  get categoryBackButton(): Locator {
    return this.page.locator('.category-back-btn');
  }

  get categoryResultsTitle(): Locator {
    return this.page.locator('.category-results-title');
  }

  get categorySearchGroups(): Locator {
    return this.page.locator('.category-results .search-group');
  }

  get categoryEmpty(): Locator {
    return this.page.locator('.category-empty');
  }

  // Premieres mode
  get premieresList(): Locator {
    return this.page.locator('.premieres-list');
  }

  get premieresItems(): Locator {
    return this.page.locator('.premiere-item');
  }

  get premieresEmpty(): Locator {
    return this.page.locator('.premieres-empty');
  }

  // Time Slot mode
  get timeSlotDateInput(): Locator {
    return this.page.locator('.time-slot-date-input');
  }

  get timeSlotTimeSelect(): Locator {
    return this.page.locator('.time-slot-time-select');
  }

  get timeSlotList(): Locator {
    return this.page.locator('.time-slot-list');
  }

  timeSlotRow(channelId: string): Locator {
    return this.page.locator(`.time-slot-row[data-channel-id="${channelId}"]`);
  }
}
