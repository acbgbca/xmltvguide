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
}
