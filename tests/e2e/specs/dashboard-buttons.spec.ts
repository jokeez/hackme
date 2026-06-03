import { test, expect, type Page } from '@playwright/test';

const ADMIN = process.env.E2E_ADMIN_TOKEN || 'e2e-admin-token-test';

async function openOrdersAdvanced(page: Page) {
  await page.click('#tab-bar .tab-btn[data-tab="orders"]');
  await page.click('#btn-orders-toggle-poh-advanced');
  await expect(page.locator('#orders-admin-create')).toBeVisible({ timeout: 10_000 });
}

test.describe('Dashboard UI buttons and API wiring', () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript((token) => {
      sessionStorage.setItem('hackme_admin_token', token);
      localStorage.setItem('hackme_admin_token', token);
    }, ADMIN);
    await page.goto('/#overview');
    await page.waitForSelector('body.js-ui', { timeout: 15_000 });
  });

  test('tab navigation switches visible panel', async ({ page }) => {
    await page.click('#tab-bar .tab-btn[data-tab="orders"]');
    await expect(page.locator('#panel-orders')).toHaveClass(/active/);
    await expect(page.locator('#panel-overview')).not.toHaveClass(/active/);
  });

  test('Refresh now triggers status or metrics fetch', async ({ page }) => {
    const resp = page.waitForResponse(
      (r) => r.url().includes('/api/status') || r.url().includes('/api/metrics'),
      { timeout: 15_000 }
    );
    await page.click('#btn-quick-refresh');
    const got = await resp;
    expect(got.ok()).toBeTruthy();
  });

  test('Mining stop button POSTs mining stop', async ({ page }) => {
    await page.click('#tab-bar .tab-btn[data-tab="mining"]');
    const stopReq = page.waitForRequest(
      (r) => r.method() === 'POST' && r.url().includes('/api/mining/stop'),
      { timeout: 15_000 }
    );
    await page.click('#btn-mining-stop');
    const req = await stopReq;
    expect(req.method()).toBe('POST');
  });

  test('Orders language buttons fill code editor', async ({ page }) => {
    await openOrdersAdvanced(page);
    const ta = page.locator('#orders-code-input');
    await expect(ta).toBeVisible();
    await page.click('#btn-orders-code-rust');
    await expect(ta).toHaveValue(/extern "C"/, { timeout: 15_000 });
    await page.click('#btn-orders-code-zig');
    await expect(ta).toHaveValue(/export fn check/);
    await page.click('#btn-orders-code-cpp');
    await expect(ta).toHaveValue(/extern "C"/);
  });

  test('Orders POST /api/tasks sends JSON body', async ({ page }) => {
    await openOrdersAdvanced(page);
    await page.fill('#orders-form-id', 'e2e-order-' + Date.now());
    await page.fill('#orders-form-reward', '0.01');
    const postReq = page.waitForRequest(
      (r) => r.method() === 'POST' && /\/api\/tasks$/.test(r.url()),
      { timeout: 15_000 }
    );
    await page.click('#btn-orders-submit');
    const req = await postReq;
    const body = req.postDataJSON();
    expect(body).toBeTruthy();
    expect(body.id || body.task_id).toBeTruthy();
  });

  test('Fuzz create then start posts campaign status', async ({ page, request }) => {
    const cid = `campaign-ui-e2e-${Date.now()}`;
    const hdrs = { 'X-Hackme-Admin-Token': ADMIN, 'Content-Type': 'application/json' };
    const seeded = await request.post('/api/fuzz/campaigns', {
      headers: hdrs,
      data: {
        id: cid,
        campaign_type: 'fuzz',
        status: 'planned',
        title: 'e2e fuzz',
        budget_runs: 50,
        owner_ref: 'hackme:security-note-01',
        task_id: 'order-security-script-push-001',
      },
    });
    expect(seeded.ok(), `seed create HTTP ${seeded.status()}`).toBeTruthy();

    await page.fill('#admin-token-input', ADMIN);
    await page.click('#btn-admin-token-save');
    await page.click('#tab-bar .tab-btn[data-tab="fuzz"]');
    const row = page.locator(`#fuzz-campaign-rows tr[data-campaign-id="${cid}"]`);
    await expect(row).toBeVisible({ timeout: 20_000 });
    await row.click();
    await expect(page.locator('#fuzz-selected-id')).toContainText(cid, { timeout: 10_000 });
    const statusReq = page.waitForRequest(
      (r) => r.method() === 'POST' && r.url().includes(`/api/fuzz/campaigns/${encodeURIComponent(cid)}/status`),
      { timeout: 20_000 }
    );
    await page.click('#btn-fuzz-quick-running');
    const req = await statusReq;
    expect(req.postDataJSON()?.status).toBe('running');
  });

  test('Mining calculator GH/s slider stays capped', async ({ page }) => {
    await page.click('#tab-bar .tab-btn[data-tab="mining"]');
    const slider = page.locator('#mine-calc-gh-slider');
    await expect(slider).toBeVisible();
    await page.waitForFunction(() => {
      const el = document.querySelector('#mine-calc-gh-slider') as HTMLInputElement | null;
      return el && Number(el.max) > 0 && Number(el.max) <= 500;
    });
    const maxBefore = await slider.evaluate((el) => Number((el as HTMLInputElement).max));
    expect(maxBefore).toBeGreaterThan(0);
    expect(maxBefore).toBeLessThanOrEqual(500);
    await slider.evaluate((el) => {
      const s = el as HTMLInputElement;
      s.value = String(Number(s.max));
      s.dispatchEvent(new Event('input', { bubbles: true }));
    });
    const maxAfter = await slider.evaluate((el) => Number((el as HTMLInputElement).max));
    expect(maxAfter).toBe(maxBefore);
    const label = await page.locator('#mine-calc-gh-label').textContent();
    expect(label || '').toMatch(/GH\/s$/);
    await expect(page.locator('#p2p-peers-section')).toHaveClass(/hidden/);
    await expect(page.locator('#mining-pool-status-line')).toBeVisible();
  });

  test('Hardware refresh loads tune API', async ({ page }) => {
    await page.click('#tab-bar .tab-btn[data-tab="hardware"]');
    const tuneResp = page.waitForResponse(
      (r) => r.url().includes('/api/hardware/tune') && r.request().method() === 'GET',
      { timeout: 20_000 }
    );
    await page.click('#btn-hardware-refresh');
    const resp = await tuneResp;
    expect(resp.ok()).toBeTruthy();
  });
});
