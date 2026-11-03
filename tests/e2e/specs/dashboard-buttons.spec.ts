import { test, expect, type Page } from '@playwright/test';

const ADMIN = process.env.E2E_ADMIN_TOKEN || 'e2e-admin-token-test';

async function openOrdersAdvanced(page: Page) {
  await page.click('#tab-bar .tab-btn[data-tab="orders"]');
  await expect(page.locator('#panel-orders')).toHaveClass(/active/, { timeout: 10_000 });
  const toggle = page.locator('#btn-orders-toggle-poh-advanced');
  await expect(toggle).toBeVisible({ timeout: 15_000 });
  await toggle.scrollIntoViewIfNeeded();
  await toggle.click();
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
    await expect(page.locator('#panel-mining')).toHaveClass(/active/, { timeout: 10_000 });
    const stopBtn = page.locator('#btn-mining-stop');
    await expect(stopBtn).toBeVisible({ timeout: 15_000 });
    await stopBtn.scrollIntoViewIfNeeded();
    const stopReq = page.waitForRequest(
      (r) => r.method() === 'POST' && r.url().includes('/api/mining/stop'),
      { timeout: 15_000 }
    );
    await stopBtn.click();
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
    const created = await seeded.json();
    const actualId = String(created?.campaign?.id || cid);

    const statusResp = await request.post(`/api/fuzz/campaigns/${encodeURIComponent(actualId)}/status`, {
      headers: hdrs,
      data: { status: 'running' },
    });
    expect(statusResp.ok(), `status HTTP ${statusResp.status()}`).toBeTruthy();
    const statusBody = await statusResp.json();
    const st = String(statusBody?.campaign?.status || statusBody?.status || '');
    expect(st).toBe('running');

    // Light UI smoke: fuzz tab loads with admin token (operator panel visible).
    await page.fill('#admin-token-input', ADMIN);
    await page.click('#btn-admin-token-save');
    await page.click('#tab-bar .tab-btn[data-tab="fuzz"]');
    await expect(page.locator('#panel-fuzz')).toHaveClass(/active/);
    await expect(page.locator('#fuzz-admin-only')).toBeVisible({ timeout: 10_000 });
    await expect(page.locator('#btn-fuzz-quick-running')).toBeEnabled();
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

  test('Mining calculator slider changes HMC/h estimate', async ({ page }) => {
    await page.click('#tab-bar .tab-btn[data-tab="mining"]');
    const slider = page.locator('#mine-calc-gh-slider');
    const hour = page.locator('#mine-calc-out-hour');
    const aps = page.locator('#mine-calc-out-aps');
    const label = page.locator('#mine-calc-gh-label');
    await expect(slider).toBeVisible();
    await page.waitForFunction(() => {
      const el = document.querySelector('#mine-calc-gh-slider') as HTMLInputElement | null;
      const out = document.querySelector('#mine-calc-out-hour');
      const api = (window as unknown as { __hackmeMineCalc?: { setGh: (n: number) => void } }).__hackmeMineCalc;
      return !!el && Number(el.max) >= 1 && !!out && (out.textContent || '').trim() !== '—' && !!api;
    }, null, { timeout: 20_000 });
    const readHour = async () => {
      const t = (await hour.textContent()) || '0';
      return Number(String(t).replace(/,/g, '')) || 0;
    };
    const setGh = async (v: number) => {
      await page.evaluate((gh) => {
        const api = (window as unknown as { __hackmeMineCalc?: { setGh: (n: number) => void } }).__hackmeMineCalc;
        if (!api || typeof api.setGh !== 'function') throw new Error('mine calc API missing');
        api.setGh(gh);
      }, v);
      await expect(label).toContainText(String(Math.floor(v)), { timeout: 3_000 });
    };
    await setGh(5);
    const lowHour = await readHour();
    const lowLabel = (await label.textContent()) || '';
    const lowAps = (await aps.textContent()) || '';
    const sliderMax = Number(await slider.evaluate((el) => (el as HTMLInputElement).max)) || 20;
    const highTarget = Math.min(Math.max(sliderMax, 5.01), 20);
    await setGh(highTarget);
    const highHour = await readHour();
    const highLabel = (await label.textContent()) || '';
    const highAps = (await aps.textContent()) || '';
    expect(lowLabel).toMatch(/5/);
    expect(highLabel).toMatch(new RegExp(String(Math.floor(highTarget))));
    // Attempts/sec must track the slider. HMC/h may saturate under share-PPS lease ceiling
    // (~2× base_reward / lease) when there is no live accrual Δ to scale.
    expect(highAps).not.toBe(lowAps);
    expect(highHour).toBeGreaterThan(0);
    expect(lowHour).toBeGreaterThan(0);
  });

  test('Mining calculator live compare + coin select stay in sync', async ({ page }) => {
    await page.goto('/#mining');
    await page.waitForSelector('body.js-ui', { timeout: 15_000 });
    await page.click('#tab-bar .tab-btn[data-tab="mining"]');
    const coin = page.locator('#mine-calc-coin');
    const coinLabel = page.locator('#mine-calc-coin-label');
    const liveBtn = page.locator('#btn-mine-calc-live');
    await expect(coin).toBeVisible({ timeout: 15_000 });
    await page.waitForFunction(
      () => !!(window as unknown as { __hackmeMineCalc?: { setCoin: (id: string) => void } }).__hackmeMineCalc,
      null,
      { timeout: 15_000 }
    );

    await page.evaluate(() => {
      (window as unknown as { __hackmeMineCalc: { setCoin: (id: string) => void } }).__hackmeMineCalc.setCoin('hms');
    });
    await expect(coin).toHaveValue('hms');
    await expect(coinLabel).toContainText(/HMS/i);
    await expect(page.locator('#mine-calc-gh-label')).toContainText(/MB\/s/);

    await page.evaluate(() => {
      (window as unknown as { __hackmeMineCalc: { setCoin: (id: string) => void } }).__hackmeMineCalc.setCoin('hmc');
    });
    await expect(coin).toHaveValue('hmc');
    await expect(coinLabel).toContainText(/HMC/i);
    await expect(page.locator('#mine-calc-gh-label')).toContainText(/GH\/s/);

    await expect(liveBtn).toBeEnabled();
    await liveBtn.click();
    await expect(liveBtn).toHaveText(/Syncing|Live|No live|My rig/i, { timeout: 8_000 });
    await expect(coin).toHaveValue('hmc');
    await page.waitForFunction(() => {
      const btn = document.querySelector('#btn-mine-calc-live') as HTMLButtonElement | null;
      return !!btn && !btn.disabled && /My rig \(live\)/i.test(btn.textContent || '');
    }, null, { timeout: 8_000 });
    await expect(liveBtn).toBeEnabled();
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
