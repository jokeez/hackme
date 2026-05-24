import { test, expect } from '@playwright/test';

const SITE = process.env.SITE_BASE || 'https://hackme.tech';

const PAGES = [
  '/',
  '/index.html',
  '/downloads.html',
  '/contacts.html',
  '/security-rewards.html',
];

test.describe('Public site hackme.tech', () => {
  for (const path of PAGES) {
    test(`HTTP 200 ${path}`, async ({ request }) => {
      const url = SITE.replace(/\/$/, '') + (path === '/' ? '/' : path);
      let lastStatus = 0;
      for (let attempt = 0; attempt < 3; attempt++) {
        const res = await request.get(url, { timeout: 90_000 });
        lastStatus = res.status();
        if (lastStatus === 200) return;
        await new Promise((r) => setTimeout(r, 2000));
      }
      expect(lastStatus).toBe(200);
    });
  }

  test('ISO download returns Content-Length and starts transfer', async ({ request }) => {
    const isoUrl =
      process.env.ISO_URL ||
      'https://hackme.tech/dist/release_0.1.0-rc11g/HackMe-OS-0.1.0-rc11g-amd64.iso';
    const head = await request.head(isoUrl, { timeout: 120_000 });
    expect(head.status()).toBe(200);
    const len = head.headers()['content-length'];
    expect(len).toBeTruthy();
    const bytes = Number(len);
    expect(bytes).toBeGreaterThan(800_000_000);

    const range = await request.get(isoUrl, {
      timeout: 180_000,
      headers: { Range: 'bytes=0-65535' },
    });
    expect([200, 206]).toContain(range.status());
    const body = await range.body();
    expect(body.length).toBeGreaterThan(100);
  });
});
