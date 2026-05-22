import { test, expect } from '@playwright/test';

const ADMIN = process.env.E2E_ADMIN_TOKEN || 'e2e-admin-token-test';
const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:8080';

test.describe('SoloPool multi-vector dashboard', () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript((token) => {
      sessionStorage.setItem('hackme_admin_token', token);
      localStorage.setItem('hackme_admin_token', token);
    }, ADMIN);
    await page.goto('/#overview');
    await page.waitForSelector('body.js-ui', { timeout: 20_000 });
  });

  test('vector tabs update ticker, units, and neon accent', async ({ page }) => {
    await expect(page.locator('#solopool-vector-bar')).toBeVisible();
    const status = page.locator('#solopool-vector-status');

    await page.click('.solopool-vector-btn[data-vector="hmc"]');
    await expect(status).toContainText(/HMC.*GH\/s/i);
    await expect(page.locator('.solopool-vector-btn[data-vector="hmc"]')).toHaveClass(/active/);
    await expect(page.locator('#kpi-hash-label')).toContainText(/Pool hashrate|PoH/i);

    await page.click('.solopool-vector-btn[data-vector="hms"]');
    await expect(status).toContainText(/HMS.*MB\/s/i);
    await expect(page.locator('.solopool-vector-btn[data-vector="hms"]')).toHaveClass(/active/);
    await expect(page.locator('#solopool-dashboard-card')).toHaveClass(/border-neon/);

    await page.click('.solopool-vector-btn[data-vector="hmai"]');
    await expect(status).toContainText(/HMAI.*TFLOPS/i);
    await expect(page.locator('.solopool-vector-btn[data-vector="hmai"]')).toHaveClass(/active/);
    await expect(page.locator('#solopool-dashboard-card')).toHaveClass(/border-warn/);

    await page.click('#tab-bar .tab-btn[data-tab="mining"]');
    await page.selectOption('#mine-calc-coin', 'hmai');
    await expect(page.locator('#mine-calc-gh-label')).toContainText(/TFLOPS/);
    await expect(page.locator('#mine-calc-coin-label')).toContainText(/HMAI/);
  });

  test('wallet lookup validation — valid, bad, empty', async ({ page }) => {
    const inp = page.locator('#overview-miner-search');
    const out = page.locator('#overview-miner-result');

    await inp.fill('');
    await page.click('#btn-overview-miner-search');
    await expect(out).toHaveText('—');

    await inp.fill('not-a-wallet');
    await page.click('#btn-overview-miner-search');
    await expect(out).not.toHaveClass(/miner-lookup-err/);

    await inp.fill('HMC-broken');
    await page.click('#btn-overview-miner-search');
    await expect(out).toHaveClass(/miner-lookup-err/);
    await expect(out).toContainText(/ERROR.*invalid HMC/i);

    await inp.fill('HMC-91fe007e4036c602');
    await page.click('#btn-overview-miner-search');
    await expect(out).not.toHaveClass(/miner-lookup-err/);
    const box = await out.boundingBox();
    expect(box?.width).toBeGreaterThan(100);
  });

  test('pool hashrate and payout % columns are distinct', async ({ page }) => {
    const headers = page.locator('.overview-miners-table thead th');
    const texts = await headers.allTextContents();
    expect(texts.join('|')).toMatch(/Pool hashrate/i);
    expect(texts.join('|')).toMatch(/Payout %/i);
    expect(texts.join('|')).toMatch(/Hash %/i);
    await expect(page.getByText('payout % ≠ hashrate %')).toBeVisible();
  });
});

test.describe('Dashboard responsive layout', () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript((token) => {
      sessionStorage.setItem('hackme_admin_token', token);
    }, ADMIN);
  });

  for (const vp of [
    { name: 'desktop', width: 1920, height: 1080 },
    { name: 'tablet', width: 768, height: 1024 },
    { name: 'mobile', width: 375, height: 812 },
  ]) {
    test(`overview layout @ ${vp.name}`, async ({ page }) => {
      await page.setViewportSize({ width: vp.width, height: vp.height });
      await page.goto('/#overview');
      await page.waitForSelector('body.js-ui', { timeout: 20_000 });

      await expect(page.locator('.overview-miners-table')).toBeVisible();
      const wrap = page.locator('#panel-overview div.overflow-x-auto').first();
      const wrapBox = await wrap.boundingBox();
      expect(wrapBox).toBeTruthy();
      expect((wrapBox?.width || 0)).toBeLessThanOrEqual(vp.width + 8);

      if (vp.width < 1024) {
        await expect(page.locator('#btn-sidebar-toggle')).toBeVisible();
        const sidebar = page.locator('#app-sidebar');
        await expect(sidebar).not.toHaveClass(/sidebar-open/);
        await page.click('#btn-sidebar-toggle');
        await expect(sidebar).toHaveClass(/sidebar-open/);
      }
    });
  }
});
