import { Page, Locator } from '@playwright/test';

export class AppPage {
  constructor(protected page: Page) {}

  // Navigation
  async navigateTo(tab: 'guide' | 'search' | 'favourites' | 'settings' | 'explore'): Promise<void> {
    await this.page.getByRole('navigation').getByRole('button', { name: new RegExp(tab, 'i') }).click(); // nosemgrep: javascript.lang.security.audit.detect-non-literal-regexp.detect-non-literal-regexp
  }

  async activeTab(): Promise<string> {
    const active = this.page.getByRole('navigation').locator('.bottom-nav-btn.active');
    return (await active.getAttribute('data-page')) ?? '';
  }

  // Loading screen
  async waitForAppReady(): Promise<void> {
    await this.page.locator('#loadingScreen.hidden').waitFor({ state: 'attached' });
  }

  // Programme detail modal
  get modal(): Locator {
    return this.page.locator('#programmeModal');
  }

  get modalTitle(): Locator {
    return this.page.locator('#modalTitle');
  }

  get modalTime(): Locator {
    return this.page.locator('#modalTime');
  }

  get modalDesc(): Locator {
    return this.page.locator('#modalDesc');
  }

  get modalCategory(): Locator {
    return this.page.locator('#modalCategory');
  }

  get modalEpisode(): Locator {
    return this.page.locator('#modalEpisode');
  }

  async closeModal(): Promise<void> {
    await this.page.locator('#modalClose').click();
  }

  async isModalVisible(): Promise<boolean> {
    return this.page.locator('#modalBackdrop').isVisible();
  }
}
