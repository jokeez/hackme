import { test, expect } from '@playwright/test';

// Regressions from the 2026-11-05 visual pass (version ordering is covered by the Go table
// test TestUpdateAvailableNeverDowngrades — string comparison here would be wrong).
test.describe('Dashboard layout regressions', () => {
  test('a long balance stays inside its KPI card at 1440px', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/#overview');
    await page.waitForSelector('body.js-ui', { timeout: 15_000 });
    const res = await page.evaluate(() => {
      const card = document.getElementById('kpi-balance-card') as HTMLElement | null;
      const bal = document.getElementById('kpi-balance');
      const val = card?.querySelector('.font-display') as HTMLElement | null;
      if (!card || !bal || !val) return null;
      bal.textContent = '123456.78901234'; // worst case: 8 decimals, 6 integer digits
      const c = card.getBoundingClientRect();
      const r = document.createRange();
      r.selectNodeContents(val);
      const t = r.getBoundingClientRect();
      return { textRight: t.right, cardRight: c.right, scroll: card.scrollWidth, client: card.clientWidth };
    });
    expect(res).not.toBeNull();
    expect(res!.textRight).toBeLessThanOrEqual(res!.cardRight + 1);
    expect(res!.scroll).toBeLessThanOrEqual(res!.client + 1);
  });

  test('economic dashboard shows the v2 switch before activation', async ({ page }) => {
    await page.goto('/#mining');
    await page.waitForSelector('body.js-ui', { timeout: 15_000 });
    const m = await (await page.request.get('/api/metrics')).json();
    const st = await (await page.request.get('/api/status?lite=1')).json();
    const tip = Math.max(Number(m.block_height || 0), Number(st.display_tip_height || st.canonical_tip_height || 0));
    const v2 = Number(m.econ_v2_activation_height || 0);
    test.skip(!(v2 > 0 && tip < v2), 'no pending v2 switch on this node');
    await expect(page.locator('#econ-next-halving-label')).toHaveText('Economics v2 switch', { timeout: 20_000 });
    await expect(page.locator('#econ-next-halving')).toHaveText(v2.toLocaleString('en-US'));
  });

  test('no CSP violations on overview', async ({ page }) => {
    const csp: string[] = [];
    page.on('console', (msg) => { if (/Content Security Policy/.test(msg.text())) csp.push(msg.text()); });
    await page.goto('/#overview');
    await page.waitForSelector('body.js-ui', { timeout: 15_000 });
    await page.waitForTimeout(6_000);
    expect(csp.filter((t) => !/frame-ancestors/.test(t))).toEqual([]);
  });
});
