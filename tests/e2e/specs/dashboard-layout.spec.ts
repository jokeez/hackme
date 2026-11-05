import { test, expect } from '@playwright/test';

// Regressions from the 2026-11-05 visual pass.
test.describe('Dashboard layout regressions', () => {
  test('balance KPI fits its card at 1440px', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/#overview');
    await page.waitForSelector('body.js-ui', { timeout: 15_000 });
    await page.waitForTimeout(2_000);
    const overflow = await page.evaluate(() => {
      const card = document.getElementById('kpi-balance-card');
      const val = card?.querySelector('.font-display') as HTMLElement | null;
      if (!card || !val) return 'missing';
      return val.getBoundingClientRect().right - card.getBoundingClientRect().right;
    });
    expect(overflow).not.toBe('missing');
    expect(Number(overflow)).toBeLessThanOrEqual(1);
  });

  test('update check never offers a downgrade', async ({ request }) => {
    const r = await request.get('/api/updates/check');
    if (!r.ok()) test.skip(true, 'update feed unreachable from test runner');
    const j = await r.json();
    if (j.local_version && j.remote_version && j.local_version > j.remote_version) {
      expect(j.update_available).toBe(false);
    }
  });

  test('economic dashboard shows the v2 switch before activation', async ({ page }) => {
    await page.goto('/#mining');
    await page.waitForSelector('body.js-ui', { timeout: 15_000 });
    await expect(page.locator('#econ-next-halving-label')).toHaveText(/Economics v2 switch|Next halving block/, { timeout: 20_000 });
    const m = await (await page.request.get('/api/metrics')).json();
    if (Number(m.econ_v2_activation_height || 0) > Number(m.block_height || 0)) {
      await expect(page.locator('#econ-next-halving-label')).toHaveText('Economics v2 switch', { timeout: 20_000 });
      await expect(page.locator('#econ-next-halving')).toHaveText(Number(m.econ_v2_activation_height).toLocaleString('en-US'));
    }
  });

  test('no CSP violations on overview', async ({ page }) => {
    const csp: string[] = [];
    page.on('console', (m) => { if (/Content Security Policy/.test(m.text())) csp.push(m.text()); });
    await page.goto('/#overview');
    await page.waitForSelector('body.js-ui', { timeout: 15_000 });
    await page.waitForTimeout(6_000);
    expect(csp.filter((t) => !/frame-ancestors/.test(t))).toEqual([]);
  });
});
