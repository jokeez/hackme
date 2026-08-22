import { test, expect, type Page } from '@playwright/test';
import fs from 'fs';
import path from 'path';

const SITE = process.env.SITE_BASE || 'https://hackme.tech';

function resolveIsoVersion(): string {
  const root = path.resolve(__dirname, '../../..');
  const isoVerPath = path.join(root, 'scripts/release/CURRENT_ISO_VERSION');
  try {
    return fs.readFileSync(isoVerPath, 'utf8').trim();
  } catch {
    return '0.1.0-rc11s';
  }
}

/** CDN may lag CURRENT_ISO_VERSION; probe known hosted ISOs (newest first). */
const ISO_PROBE_VERSIONS = ['0.1.0-rc14x', '0.1.0-rc13', '0.1.0-rc11s'];

async function resolveWorkingIsoUrl(request: import('@playwright/test').APIRequestContext): Promise<string | null> {
  if (process.env.ISO_URL) return process.env.ISO_URL;
  const base = SITE.replace(/\/$/, '');
  const host = new URL(base).hostname;
  const tried = new Set<string>();
  const versions = [resolveIsoVersion(), ...ISO_PROBE_VERSIONS];
  for (const ver of versions) {
    if (!ver || tried.has(ver)) continue;
    tried.add(ver);
    const cdnUrl = `${base}/dist/release_${ver}/HackMe-OS-${ver}-amd64.iso`;
    try {
      let head = await request.head(cdnUrl, { timeout: 45_000 });
      if (head.status() === 200) return cdnUrl;
      const probeUrl = resolveIsoRangeProbeUrl(cdnUrl);
      head = await request.head(probeUrl, {
        timeout: 45_000,
        headers: { Host: host },
        ignoreHTTPSErrors: true,
      });
      if (head.status() === 200) return cdnUrl;
    } catch {
      // CDN/origin flake — try next known version.
    }
  }
  return null;
}

/** Cloudflare can stall range GET on large ISO; origin probe matches download_hackme_release.sh */
function resolveIsoRangeProbeUrl(isoUrl: string): string {
  if (process.env.ISO_RANGE_URL) return process.env.ISO_RANGE_URL;
  const originIp = process.env.HACKME_ORIGIN_IP || '132.243.112.100';
  const u = new URL(isoUrl);
  return `https://${originIp}${u.pathname}`;
}

const PAGES = [
  '/',
  '/index.html',
  '/downloads.html',
  '/contacts.html',
  '/security-rewards.html',
];

let publicSiteReachable = true;

test.describe('Public site hackme.tech', () => {
  test.beforeAll(async ({ request }) => {
    if (process.env.SKIP_PUBLIC_SITE_E2E === '1') {
      publicSiteReachable = false;
      return;
    }
    const url = SITE.replace(/\/$/, '') + '/';
    let status = 0;
    try {
      const res = await request.get(url, { timeout: 45_000 });
      status = res.status();
    } catch {
      status = 0;
    }
    // Origin can be temporarily down (Cloudflare 52x); do not fail dashboard CI for that.
    publicSiteReachable = status > 0 && status < 500;
  });

  test.beforeEach(() => {
    test.skip(!publicSiteReachable, 'hackme.tech origin unreachable — skip live canary');
  });

  for (const path of PAGES) {
    test(`HTTP 200 ${path}`, async ({ request }) => {
      const url = SITE.replace(/\/$/, '') + (path === '/' ? '/' : path);
      let lastStatus = 0;
      for (let attempt = 0; attempt < 3; attempt++) {
        try {
          // HEAD avoids chunked body stalls on large HTML via Cloudflare.
          const res = await request.head(url, { timeout: 45_000 });
          lastStatus = res.status();
          if (lastStatus === 200) return;
        } catch (err) {
          const msg = String(err);
          if (msg.includes('ETIMEDOUT') && attempt < 2) {
            await new Promise((r) => setTimeout(r, 3000));
            continue;
          }
          throw err;
        }
        await new Promise((r) => setTimeout(r, 2000));
      }
      expect(lastStatus).toBe(200);
    });
  }

  test('ISO download returns Content-Length and starts transfer', async ({ request }) => {
    const isoUrl = await resolveWorkingIsoUrl(request);
    test.skip(!isoUrl, 'no hosted ISO found on CDN/origin — skip live canary');
    const host = new URL(isoUrl!).hostname;
    let head = await request.head(isoUrl!, { timeout: 120_000 });
    if (head.status() !== 200) {
      // CDN may 404 new release paths before origin sync; probe VPS directly (same as range test).
      const probeUrl = resolveIsoRangeProbeUrl(isoUrl);
      head = await request.head(probeUrl, {
        timeout: 120_000,
        headers: { Host: host },
        ignoreHTTPSErrors: true,
      });
    }
    expect(head.status()).toBe(200);
    const len = head.headers()['content-length'];
    expect(len).toBeTruthy();
    const bytes = Number(len);
    expect(bytes).toBeGreaterThan(800_000_000);

    // HEAD via CDN (or origin fallback above); byte-range via origin (CF path stalls ~19KB on large ISO).
    const rangeUrl = resolveIsoRangeProbeUrl(isoUrl!);
    const range = await request.get(rangeUrl, {
      timeout: 90_000,
      headers: { Range: 'bytes=0-65535', Host: host },
      ignoreHTTPSErrors: true,
    });
    expect([200, 206]).toContain(range.status());
    const body = await range.body();
    expect(body.length).toBeGreaterThan(100);
  });
});
