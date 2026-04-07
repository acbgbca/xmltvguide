import { Page, Locator } from '@playwright/test';
import { AppPage } from './AppPage';

export class GuidePage extends AppPage {
  constructor(page: Page) {
    super(page);
  }

  async goto(): Promise<void> {
    await this.page.goto('/');
    await this.waitForAppReady();
  }

  get dateDisplay(): Locator {
    return this.page.locator('#dateDisplay');
  }

  async clickPrevDay(): Promise<void> {
    await this.page.locator('#prevDay').click();
  }

  async clickNextDay(): Promise<void> {
    await this.page.locator('#nextDay').click();
  }

  async clickNow(): Promise<void> {
    await this.page.locator('#nowBtn').click();
  }

  channelLabels(): Locator {
    return this.page.locator('.channel-label');
  }

  channelLabelByName(name: string): Locator {
    return this.page.locator('.channel-label').filter({ hasText: name });
  }

  programmeCells(): Locator {
    return this.page.locator('.programme');
  }

  programmeCellByTitle(title: string): Locator {
    return this.page.locator('.programme').filter({ has: this.page.locator('.prog-title', { hasText: title }) });
  }

  async clickProgramme(title: string): Promise<void> {
    await this.programmeCellByTitle(title).first().click();
    await this.page.locator('#programmeModal').waitFor({ state: 'visible' });
  }

  get nowLine(): Locator {
    return this.page.locator('#nowLine');
  }

  async isNowLineVisible(): Promise<boolean> {
    const display = await this.nowLine.evaluate((el) =>
      window.getComputedStyle(el).display
    );
    return display !== 'none';
  }

  async nowLineLeftPx(): Promise<number> {
    const left = await this.nowLine.evaluate((el) => (el as HTMLElement).style.left);
    return parseFloat(left);
  }

  async isEmptyStateVisible(): Promise<boolean> {
    const el = this.page.locator('#guideEmpty');
    return el.evaluate((node) => node.classList.contains('visible'));
  }
}
