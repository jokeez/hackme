(() => {
  /** Bump with scripts/release/CURRENT_VERSION, dist/release_<VERSION>/, main.go Version */
  const RELEASE_VER = "0.1.0-rc15";
  /** HackMe OS ISO — aligned with Win/Linux (scripts/release/CURRENT_ISO_VERSION). */
  const ISO_CHANNEL = "0.1.0-rc15";

  /** Sub-1 TH/s → GH/s (matches dashboard / explorer pool strip). */
  function fmtPoolHashrateTHS(ths, mock) {
    const th = Number(ths);
    if (!Number.isFinite(th) || th <= 0) return "—";
    const star = mock ? " *" : "";
    if (th < 1) {
      const gh = th * 1000;
      const ghStr = gh < 0.01 ? gh.toFixed(4) : gh < 1 ? gh.toFixed(2) : gh.toFixed(1);
      return `${ghStr} GH/s${star}`;
    }
    const thStr = th < 1 ? th.toFixed(3) : th.toFixed(2);
    return `${thStr} TH/s${star}`;
  }

  const COMMUNITY = {
    github: "https://github.com/jokeez/hackme",
    annMd: "https://bitcointalk.org/index.php?topic=5583373.0",
    x: "https://x.com/HackMeTech",
    reddit: "https://www.reddit.com/user/Hackme-Official/",
    discord: "https://discord.gg/QMxSeaTSrQ",
    tgChannel: "https://t.me/hackme_tech",
    tgEn: "https://t.me/hackme_en",
    tgRu: "https://t.me/hackme_ru",
  };

  const CONFIG = {
    explorerUrl: "/explorer-lite.html",
    newsUrl: "/news.html",
    newsFeed: "/assets/news-feed.json",
    newsDisplayIndex: "/assets/news-display-index.json",
    newsDisplay: "/assets/news-display.json",
    newsArchive: "/assets/news.json",
    releaseChannel: RELEASE_VER,
    releaseChannelNote: "rc15 — B2B fuzz Phase 2 on #mining, pool anticheat hardening, libheif OSS series archived CLEAN",
    releaseBase: `/dist/release_${RELEASE_VER}`,
    // Primary downloads: GitHub Releases (Cloudflare /dist often stalls or truncates large files).
    // Mirrors under /dist/ remain for origin IP / grey-cloud bypass.
    ghRelease: `https://github.com/jokeez/hackme/releases/download/${RELEASE_VER}`,
    windowsInstaller: `https://github.com/jokeez/hackme/releases/download/${RELEASE_VER}/HackMe-Setup-${RELEASE_VER}.exe`,
    windowsInstallerMirror: `/dist/release_${RELEASE_VER}/HackMe-Setup-${RELEASE_VER}.exe?v=20260724`,
    // rc15 GitHub Releases publish installer + zip + fuzz CLI bundles.
    windowsBundle: `https://github.com/jokeez/hackme/releases/download/${RELEASE_VER}/hackme_${RELEASE_VER}_windows.zip`,
    windowsBundleMirror: `/dist/release_${RELEASE_VER}/hackme_${RELEASE_VER}_windows.zip`,
    windowsBundleLegacy: `https://github.com/jokeez/hackme/releases/download/${RELEASE_VER}/hackme_${RELEASE_VER}_windows.zip`,
    windowsBundleLegacyMirror: `/dist/release_${RELEASE_VER}/hackme_${RELEASE_VER}_windows.zip`,
    linuxBundle: `https://github.com/jokeez/hackme/releases/download/${RELEASE_VER}/hackme_${RELEASE_VER}_linux.tar.gz`,
    linuxBundleMirror: `/dist/release_${RELEASE_VER}/hackme_${RELEASE_VER}_linux.tar.gz`,
    hackmeOSIso: `https://github.com/jokeez/hackme/releases/download/${ISO_CHANNEL}/HackMe-OS-${ISO_CHANNEL}-amd64.iso`,
    hackmeOSIsoMirror: `/dist/release_${ISO_CHANNEL}/HackMe-OS-${ISO_CHANNEL}-amd64.iso`,
    hackmeOSIsoLegacy: `/dist/release_${ISO_CHANNEL}/HackMe-Miner-${ISO_CHANNEL}-amd64.iso`,
    hackmeOSSha: `https://github.com/jokeez/hackme/releases/download/${ISO_CHANNEL}/SHA256SUMS-iso.txt`,
    hackmeOSShaMirror: `/dist/release_${ISO_CHANNEL}/SHA256SUMS-iso.txt`,
    isoChannel: ISO_CHANNEL,
    fuzzingLinux: `https://github.com/jokeez/hackme/releases/download/${RELEASE_VER}/hackme-fuzzing-${RELEASE_VER}-linux-amd64`,
    fuzzingWindows: `https://github.com/jokeez/hackme/releases/download/${RELEASE_VER}/hackme-fuzzing-${RELEASE_VER}-windows-amd64.exe`,
    fuzzingBuildLinux: `https://github.com/jokeez/hackme/releases/download/${RELEASE_VER}/hackme-fuzzing-build-${RELEASE_VER}-linux-amd64`,
    fuzzingBuildWindows: `https://github.com/jokeez/hackme/releases/download/${RELEASE_VER}/hackme-fuzzing-build-${RELEASE_VER}-windows-amd64.exe`,
    shaSums: `https://github.com/jokeez/hackme/releases/download/${RELEASE_VER}/SHA256SUMS.txt`,
    shaSumsMirror: `/dist/release_${RELEASE_VER}/SHA256SUMS.txt`,
    manifest: `https://github.com/jokeez/hackme/releases/download/${RELEASE_VER}/RELEASE_MANIFEST.json`,
    buildInfo: `https://github.com/jokeez/hackme/releases/download/${RELEASE_VER}/BUILD_INFO.txt`,
  };

  function setHref(id, href) {
    const el = document.getElementById(id);
    if (!el || !href) return;
    el.href = href;
  }

  function wireExplorerLinks() {
    setHref("explorer-link-top", CONFIG.explorerUrl);
    setHref("explorer-link-hero", CONFIG.explorerUrl);
    setHref("explorer-link-card", CONFIG.explorerUrl);
  }

  function ensureNewsLink(selector) {
    const container = document.querySelector(selector);
    if (!container) return;
    const exists = Array.from(container.querySelectorAll("a")).some((a) => {
      const href = (a.getAttribute("href") || "").trim().toLowerCase();
      return href.endsWith("/news.html") || href === "./news.html" || href === "news.html";
    });
    if (exists) return;
    const link = document.createElement("a");
    link.href = CONFIG.newsUrl;
    link.textContent = "News";
    container.appendChild(link);
  }

  function wireNewsLinks() {
    if (document.querySelector(".nav-more")) return;
    ensureNewsLink(".nav");
    ensureNewsLink(".footer-nav");
  }

  function ensureCoinsLink(selector) {
    const container = document.querySelector(selector);
    if (!container) return;
    const exists = Array.from(container.querySelectorAll("a")).some((a) => {
      const href = (a.getAttribute("href") || "").trim().toLowerCase();
      return href.endsWith("/coins.html") || href === "./coins.html" || href === "coins.html";
    });
    if (exists) return;
    const link = document.createElement("a");
    link.href = "/coins.html";
    link.textContent = "Coins";
    const news = container.querySelector('a[href*="news"]');
    if (news && news.parentNode) {
      news.parentNode.insertBefore(link, news.nextSibling);
    } else {
      container.appendChild(link);
    }
  }

  function wireCoinsLinks() {
    if (document.querySelector(".nav-more")) return;
    ensureCoinsLink(".nav");
    ensureCoinsLink(".footer-nav");
  }

  /** Insert nav link if missing (fixes scattered menus on older pages). */
  function ensureNavLink(selector, href, text, afterHrefPart) {
    const container = document.querySelector(selector);
    if (!container || !href) return;
    const norm = href.toLowerCase();
    const exists = Array.from(container.querySelectorAll("a")).some((a) => {
      const h = (a.getAttribute("href") || "").trim().toLowerCase();
      return h === norm || h.endsWith(norm.replace("./", "").replace(/^\//, ""));
    });
    if (exists) return;
    const link = document.createElement("a");
    link.href = href;
    link.textContent = text;
    if (afterHrefPart) {
      const anchor = Array.from(container.querySelectorAll("a")).find((a) =>
        (a.getAttribute("href") || "").includes(afterHrefPart)
      );
      if (anchor && anchor.parentNode) {
        anchor.insertAdjacentElement("afterend", link);
        return;
      }
    }
    container.appendChild(link);
  }

  function wireStandardNav() {
    if (document.querySelector(".nav-more")) return;
    ensureNavLink(".nav", "/coins.html", "Coins", "index");
    ensureNavLink(".nav", "/developers.html", "Developers", "coins");
    ensureNavLink(".nav", "/downloads.html", "Downloads", "developers");
    ensureNavLink(".nav", "/fuzz-campaigns.html", "Fuzz", "downloads");
    ensureNavLink(".footer-nav", "/research.html", "Research", "coins");
  }

  function ensureFooterLink(nav, href, text) {
    if (!nav || !href) return;
    const norm = href.toLowerCase();
    const exists = Array.from(nav.querySelectorAll("a")).some((a) => {
      const h = (a.getAttribute("href") || "").trim().toLowerCase();
      return h === norm || h.endsWith(norm.replace("https://", ""));
    });
    if (exists) return;
    const link = document.createElement("a");
    link.href = href;
    link.textContent = text;
    if (href.startsWith("http")) {
      link.target = "_blank";
      link.rel = "noreferrer";
    }
    nav.prepend(link);
  }

  function wireCommunityFooter() {
    if (document.querySelector(".footer-nav-cols")) return;
    document.querySelectorAll(".footer-nav").forEach((nav) => {
      ensureFooterLink(nav, COMMUNITY.github, "GitHub");
    });
  }

  // GitHub Releases (and large /dist ISOs) must not be probed with fetch/HEAD from
  // hackme.tech: GitHub has no CORS ACAO, and HEAD of an 850MB ISO can stall the tab.
  // <a href> navigation is not CORS — set GitHub URLs directly.
  function resolveWindowsDownloadHref() {
    return CONFIG.windowsInstaller || CONFIG.windowsBundleLegacy || CONFIG.windowsBundle || "";
  }

  function resolveHackMeOSIsoHref() {
    return CONFIG.hackmeOSIso || CONFIG.hackmeOSIsoMirror || CONFIG.hackmeOSIsoLegacy || "";
  }

  function wireDownloadLinks() {
    setHref("download-win", CONFIG.windowsInstaller || CONFIG.windowsBundle);
    // "Portable ZIP" should point to the actually published asset.
    setHref("download-win-zip", CONFIG.windowsBundleLegacy || CONFIG.windowsBundle);
    setHref("download-linux", CONFIG.linuxBundle);
    setHref("download-fuzzing-linux", CONFIG.fuzzingLinux);
    setHref("download-fuzzing-win", CONFIG.fuzzingWindows);
    setHref("download-fuzzing-build-linux", CONFIG.fuzzingBuildLinux);
    setHref("download-fuzzing-build-win", CONFIG.fuzzingBuildWindows);
    setHref("download-sha", CONFIG.shaSums);
    setHref("download-iso-sha", CONFIG.hackmeOSSha);
    setHref("download-iso-sha-card", CONFIG.hackmeOSSha);
    setHref("download-manifest", CONFIG.manifest);
    setHref("download-buildinfo", CONFIG.buildInfo);
    const verEl = document.getElementById("dl-release-ver");
    const verMeta = document.getElementById("dl-release-meta");
    const verLabel = CONFIG.releaseChannel || RELEASE_VER;
    if (verEl) verEl.textContent = verLabel;
    if (verMeta) verMeta.textContent = verLabel;
    const contactsVer = document.getElementById("contacts-release-ver");
    if (contactsVer) contactsVer.textContent = `release ${verLabel}`;
    const isoHref = resolveHackMeOSIsoHref();
    const isoBtn = document.getElementById("download-iso");
    const isoStat = document.getElementById("download-iso-status");
    if (isoBtn) {
      if (isoHref) {
        isoBtn.href = isoHref;
        isoBtn.classList.remove("btn-disabled");
        if (isoHref.startsWith("http")) {
          isoBtn.target = "_blank";
          isoBtn.rel = "noreferrer";
        }
        if (isoStat) isoStat.textContent = "ISO available — verify SHA256SUMS-iso.txt before flashing.";
      } else {
        isoBtn.href = "#hackme-os";
        if (isoStat) {
          isoStat.textContent =
            "ISO build publishing soon — watch News or build from source: scripts/release/iso/build_hackme_miner_iso.sh";
        }
      }
    }
  }

  /** Public hackme.tech nginx exposes read APIs under /pool/api; dev localhost uses /api. */
  function apiRoot() {
    const h = (window.location.hostname || "").toLowerCase();
    if (h === "localhost" || h === "127.0.0.1" || h === "") return "/api";
    return "/pool/api";
  }

  /** Coordinator HTTP API (work stats — fast, authoritative for pool hash / difficulty). */
  function coordinatorApi(path) {
    const p = String(path || "").startsWith("/") ? path : `/${path}`;
    const h = (window.location.hostname || "").toLowerCase();
    if (h === "localhost" || h === "127.0.0.1" || h === "") {
      return `/pool/coordinator${p}`;
    }
    return `/pool/coordinator${p}`;
  }

  const API_PROBE_MS = 4500;
  const POOL_CACHE_KEY = "hackme.pool.stats.v1";

  function readPoolCache() {
    try {
      const raw = localStorage.getItem(POOL_CACHE_KEY);
      return raw ? JSON.parse(raw) : null;
    } catch (_) {
      return null;
    }
  }

  function writePoolCache(data) {
    try {
      localStorage.setItem(POOL_CACHE_KEY, JSON.stringify({ ...data, ts: Date.now() }));
    } catch (_) {}
  }

  function fmtAgo(ts) {
    const t = Number(ts);
    if (!Number.isFinite(t) || t <= 0) return "";
    const sec = Math.max(0, Math.floor((Date.now() - t) / 1000));
    if (sec < 60) return `${sec}s ago`;
    if (sec < 3600) return `${Math.floor(sec / 60)}m ago`;
    return `${Math.floor(sec / 3600)}h ago`;
  }

  function setPoolUpdated(label, degraded) {
    const el = document.getElementById("pool-live-updated");
    if (!el) return;
    el.textContent = label;
    el.classList.toggle("pool-degraded-label", !!degraded);
  }

  function paintPoolRow(data, opts) {
    const degraded = !!(opts && opts.degraded);
    const skeleton = !!(opts && opts.skeleton);
    const ids = [
      "pool-live-hash",
      "pool-live-workers",
      "pool-live-height",
      "pool-live-reward",
      "pool-live-diff",
      "pool-live-status",
    ];
    ids.forEach((id) => {
      const el = document.getElementById(id);
      if (!el) return;
      el.classList.toggle("pool-skeleton", skeleton);
      el.classList.toggle("pool-degraded", degraded);
    });
    if (!data) return;
    const hashEl = document.getElementById("pool-live-hash");
    const wEl = document.getElementById("pool-live-workers");
    const hEl = document.getElementById("pool-live-height");
    const rEl = document.getElementById("pool-live-reward");
    const dEl = document.getElementById("pool-live-diff");
    const sEl = document.getElementById("pool-live-status");
    if (hashEl && data.hash != null) {
      hashEl.textContent = data.hash;
      if (data.hashTitle) hashEl.title = data.hashTitle;
    }
    if (wEl && data.workers != null) wEl.textContent = data.workers;
    if (hEl && data.height != null) hEl.textContent = data.height;
    if (rEl && data.reward != null) rEl.textContent = data.reward;
    if (dEl && data.diff != null) dEl.textContent = data.diff;
    if (sEl && data.status != null) sEl.textContent = data.status;
    if (data.ts) {
      const ago = fmtAgo(data.ts);
      setPoolUpdated(
        degraded ? `Last known · ${ago} · degraded` : ago ? `Updated ${ago}` : "live",
        degraded
      );
    }
  }

  function hydratePoolFromCache() {
    const cache = readPoolCache();
    if (!cache) {
      setPoolUpdated("fetching…", false);
      paintPoolRow(null, { skeleton: true });
      return;
    }
    paintPoolRow(cache, { degraded: true });
  }

  async function fetchJson(url, timeoutMs = API_PROBE_MS) {
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), timeoutMs);
    try {
      const resp = await fetch(url, { cache: "no-store", signal: ctrl.signal });
      if (!resp.ok) return null;
      return await resp.json();
    } catch (_) {
      return null;
    } finally {
      clearTimeout(timer);
    }
  }

  function paintDomainMeta() {
    const domainEl = document.getElementById("site-domain-status");
    const liveEl = document.getElementById("site-live-status");
    const relEl = document.getElementById("site-release-channel");
    if (domainEl) domainEl.textContent = window.location.hostname || "hackme.tech";
    if (liveEl) liveEl.textContent = "probing API...";
    if (relEl) relEl.textContent = `${CONFIG.releaseChannel} (${CONFIG.releaseChannelNote})`;
    return { liveEl };
  }

  function escapeHtml(v) {
    return String(v || "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#39;");
  }

  function statusClass(status) {
    const s = String(status || "").toLowerCase();
    if (s === "deployed") return "news-status news-status-deployed";
    if (s === "monitoring") return "news-status news-status-monitoring";
    return "news-status";
  }

  function parseDateSafe(v) {
    const t = Date.parse(v);
    return Number.isFinite(t) ? t : 0;
  }

  function shortHash(v, n = 20) {
    const s = String(v || "").trim();
    if (!s) return "—";
    return s.length > n ? `${s.slice(0, n)}…` : s;
  }

  async function renderNewsHealth() {
    const heightEl = document.getElementById("news-health-height");
    if (!heightEl) return;
    const tipEl = document.getElementById("news-health-tip");
    const hashrateEl = document.getElementById("news-health-hashrate");
    const miningEl = document.getElementById("news-health-mining");
    const acceptEl = document.getElementById("news-health-accept");
    const schedEl = document.getElementById("news-health-sched");
    const [global, work] = await Promise.all([
      fetchJson(`${apiRoot()}/global/metrics`),
      fetchJson(coordinatorApi("/api/work/stats")),
    ]);
    if (!global && !work) {
      heightEl.textContent = "—";
      if (tipEl) tipEl.textContent = "—";
      if (hashrateEl) hashrateEl.textContent = "—";
      if (miningEl) miningEl.textContent = "degraded";
      if (acceptEl) acceptEl.textContent = "—";
      if (schedEl) schedEl.textContent = "—";
      return;
    }
    const height = Number(
      (global && global.chain && global.chain.tip_height != null && global.chain.tip_height) ||
        (work && work.chain_height) ||
        0
    );
    const tip = String((global && global.chain && global.chain.tip_hash) || "");
    let poolTH = Number(global && global.network && global.network.global_hashrate_th_s);
    const mock = !!(global && global.network && global.network.global_mock === true);
    if ((!Number.isFinite(poolTH) || poolTH <= 0) && work) {
      const gh = Number(work.pool_hashrate_gh_s);
      if (Number.isFinite(gh) && gh > 0) poolTH = gh / 1000;
    }
    heightEl.textContent = Number.isFinite(height) && height > 0 ? Math.floor(height).toLocaleString("en-US") : "—";
    if (tipEl) {
      tipEl.textContent = tip ? shortHash(tip, 22) : "—";
      tipEl.title = tip || "";
    }
    if (hashrateEl) {
      hashrateEl.textContent = Number.isFinite(poolTH) && poolTH > 0 ? fmtPoolHashrateTHS(poolTH, mock) : "—";
    }
    const online = Number(
      (work && (work.workers_online != null ? work.workers_online : work.workers_count)) ||
        (global && global.work && global.work.workers_count) ||
        0
    );
    if (miningEl) {
      miningEl.textContent = online > 0 ? `${online} online` : work || global ? "0 online" : "—";
    }
    if (acceptEl) {
      const acc = Number(work && work.signed_submits_accepted);
      const rej = Number(work && work.signed_submits_rejected);
      const tot = (Number.isFinite(acc) ? acc : 0) + (Number.isFinite(rej) ? rej : 0);
      acceptEl.textContent = tot > 0 ? `${((acc / tot) * 100).toFixed(1)}%` : "—";
    }
    if (schedEl) {
      const mode = String((work && work.scheduler_mode) || "");
      const orders = work && work.orders_active === true;
      schedEl.textContent = mode ? (orders ? `${mode} · orders` : mode) : "—";
    }
  }

  async function renderNewsPage() {
    const listEl = document.getElementById("news-list");
    if (!listEl) return;
    const filtersEl = document.getElementById("news-tag-filters");
    const searchEl = document.getElementById("news-search");
    const featuredEl = document.getElementById("news-featured");
    const emptyEl = document.getElementById("news-empty");
    // Collapse noisy tag cloud: aliases + primary row + "More tags".
    const TAG_ALIASES = {
      fuzz: "fuzzing",
      infra: "infrastructure",
      oss: "oss-cve",
      "hackme-os": "iso",
      product: "release",
      production: "release",
      news: null,
      announcement: "release",
      opensource: "oss-cve",
    };
    const PRIMARY_TAGS = [
      "release",
      "mining",
      "security",
      "fuzzing",
      "research",
      "pool",
      "ops",
      "infrastructure",
    ];
    function normalizeTag(tag) {
      const key = String(tag || "")
        .trim()
        .toLowerCase();
      if (!key) return null;
      if (Object.prototype.hasOwnProperty.call(TAG_ALIASES, key)) {
        return TAG_ALIASES[key];
      }
      return key;
    }
    function itemTags(it) {
      return Array.from(
        new Set((Array.isArray(it.tags) ? it.tags : []).map(normalizeTag).filter(Boolean))
      );
    }
    let items = [];
    async function fetchNewsItems(url) {
      const resp = await fetch(url, { cache: "no-store" });
      if (!resp.ok) throw new Error(`news unavailable (${url})`);
      const body = await resp.json();
      return Array.isArray(body.items) ? body.items.slice() : [];
    }
    try {
      // Chunked display archive (CDN-safe) → single display → full bot JSON → 12 recent.
      let loaded = false;
      try {
        const idxResp = await fetch(CONFIG.newsDisplayIndex, { cache: "no-store" });
        if (idxResp.ok) {
          const idx = await idxResp.json();
          const parts = await Promise.all(
            (idx.chunks || []).map((url) => fetchNewsItems(url))
          );
          items = parts.flat();
          if (items.length > 0) loaded = true;
        }
      } catch (_) {}
      if (!loaded) {
        for (const url of [CONFIG.newsDisplay, CONFIG.newsArchive, CONFIG.newsFeed]) {
          try {
            items = await fetchNewsItems(url);
            if (items.length > 0) break;
          } catch (_) {}
        }
      }
      // Enrich / override recent ids from compact feed (telegram/discord blocks).
      try {
        const recent = await fetchNewsItems(CONFIG.newsFeed);
        const byId = new Map(items.map((it) => [it.id, it]));
        for (const it of recent) {
          if (it.id) byId.set(it.id, { ...byId.get(it.id), ...it });
        }
        items = Array.from(byId.values());
      } catch (_) {}
    } catch (_) {
      if (emptyEl) {
        emptyEl.classList.remove("hidden");
        emptyEl.textContent = "News feed is temporarily unavailable.";
      }
      return;
    }
    items.sort((a, b) => parseDateSafe(b.date) - parseDateSafe(a.date));
    const tags = Array.from(new Set(items.flatMap((it) => itemTags(it)))).sort((a, b) =>
      String(a).localeCompare(String(b))
    );
    const primary = PRIMARY_TAGS.filter((t) => tags.includes(t));
    const secondary = tags.filter((t) => !PRIMARY_TAGS.includes(t));

    let activeTag = "all";
    let query = "";
    let showMore = false;
    try {
      const q = new URLSearchParams(window.location.search).get("tag");
      const n = normalizeTag(q);
      if (n && tags.includes(n)) activeTag = n;
      if (n && secondary.includes(n)) showMore = true;
    } catch (_) {}

    function setTag(tag) {
      activeTag = tag || "all";
      try {
        const url = new URL(window.location.href);
        if (activeTag === "all") url.searchParams.delete("tag");
        else url.searchParams.set("tag", activeTag);
        window.history.replaceState({}, "", url);
      } catch (_) {}
      renderList();
      renderFilters();
    }

    function renderFilters() {
      if (!filtersEl) return;
      const visible = showMore ? [...primary, ...secondary] : primary;
      const btns = ["all", ...visible]
        .map((tag) => {
          const active = activeTag === tag ? " active" : "";
          const label = tag === "all" ? "All" : String(tag);
          return `<button type="button" class="news-filter-btn${active}" data-tag="${escapeHtml(tag)}">${escapeHtml(label)}</button>`;
        })
        .join("");
      const moreLabel = showMore ? "Fewer tags" : `More tags (${secondary.length})`;
      const moreBtn =
        secondary.length > 0
          ? `<button type="button" class="news-filter-btn news-filter-more" data-more="1">${escapeHtml(moreLabel)}</button>`
          : "";
      filtersEl.innerHTML = btns + moreBtn;
      filtersEl.querySelectorAll(".news-filter-btn").forEach((btn) => {
        btn.addEventListener("click", () => {
          if (btn.getAttribute("data-more")) {
            showMore = !showMore;
            renderFilters();
            return;
          }
          setTag(btn.getAttribute("data-tag") || "all");
        });
      });
    }

    function renderItem(it) {
      const id = escapeHtml(it.id || "");
      const title = escapeHtml(it.title);
      const summary = escapeHtml(it.summary);
      const impact = escapeHtml(it.impact);
      const action = escapeHtml(it.action);
      const date = escapeHtml(it.date);
      const status = escapeHtml(it.status || "update");
      const tagsHtml = itemTags(it)
        .map(
          (tag) =>
            `<button type="button" class="news-tag" data-tag="${escapeHtml(tag)}">#${escapeHtml(tag)}</button>`
        )
        .join("");
      return `
        <article class="news-card" id="${id}">
          <div class="news-head">
            <h3 class="news-title">${title}</h3>
            <span class="${statusClass(status)}">${status}</span>
          </div>
          <p class="news-date">${date}</p>
          <p class="news-summary">${summary}</p>
          <div class="news-meta">
            <span><strong>Impact:</strong> ${impact}</span>
            <span><strong>Action:</strong> ${action}</span>
          </div>
          <div class="news-tags">${tagsHtml}</div>
        </article>
      `;
    }

    function matches(it) {
      const t = itemTags(it);
      if (activeTag !== "all" && !t.includes(activeTag)) return false;
      if (!query) return true;
      const hay = [it.title, it.summary, it.impact, it.action, ...t].join(" ").toLowerCase();
      return hay.includes(query);
    }

    function bindCardTags(root) {
      if (!root) return;
      root.querySelectorAll(".news-tag[data-tag]").forEach((el) => {
        el.addEventListener("click", () => {
          const tag = el.getAttribute("data-tag") || "all";
          if (secondary.includes(tag)) showMore = true;
          setTag(tag);
        });
      });
    }

    function renderList() {
      const filtered = items.filter(matches);
      if (featuredEl) {
        featuredEl.innerHTML = filtered.length > 0 ? renderItem(filtered[0]) : "";
        bindCardTags(featuredEl);
      }
      const listItems = filtered.slice(1);
      listEl.innerHTML = listItems.map(renderItem).join("");
      bindCardTags(listEl);
      if (emptyEl) emptyEl.classList.toggle("hidden", filtered.length > 0);
    }

    if (searchEl) {
      searchEl.addEventListener("input", () => {
        query = String(searchEl.value || "").trim().toLowerCase();
        renderList();
      });
    }
    renderFilters();
    renderList();
  }

  async function loadPoolOverview() {
    const hashEl = document.getElementById("pool-live-hash");
    const wEl = document.getElementById("pool-live-workers");
    const hEl = document.getElementById("pool-live-height");
    const rEl = document.getElementById("pool-live-reward");
    const dEl = document.getElementById("pool-live-diff");
    const sEl = document.getElementById("pool-live-status");
    if (!hashEl || !hEl || !rEl || !dEl || !sEl) return;

    const cached = readPoolCache();
    if (cached && hashEl.textContent === "—") {
      paintPoolRow(cached, { degraded: true });
    }

    const root = apiRoot();
    const [global, work] = await Promise.all([
      fetchJson(`${root}/global/metrics`),
      fetchJson(coordinatorApi("/api/work/stats")),
    ]);

    if (!global && !work) {
      if (cached) {
        paintPoolRow(cached, { degraded: true });
        return;
      }
      paintPoolRow(
        {
          hash: "—",
          workers: "—",
          height: "—",
          reward: "—",
          diff: "—",
          status: "degraded",
        },
        { degraded: true }
      );
      setPoolUpdated("API unreachable · no cache", true);
      return;
    }

    let poolTH = NaN;
    let poolMock = false;
    if (global && global.network) {
      poolTH = Number(global.network.global_hashrate_th_s);
      poolMock = global.network.global_mock === true;
    }
    if ((!Number.isFinite(poolTH) || poolTH <= 0) && global && global.work) {
      const gh = Number(global.work.pool_hashrate_gh_s);
      if (Number.isFinite(gh) && gh > 0) poolTH = gh / 1000;
    }
    if ((!Number.isFinite(poolTH) || poolTH <= 0) && work) {
      const hs = Number(work.hashrate || work.hashrate_hs || 0);
      if (hs > 0) poolTH = hs / 1e12;
      const ghDirect = Number(work.pool_hashrate_gh_s);
      if ((!Number.isFinite(poolTH) || poolTH <= 0) && Number.isFinite(ghDirect) && ghDirect > 0) {
        poolTH = ghDirect / 1000;
      }
    }

    let hashText = "—";
    let hashTitle = "Pool aggregate unavailable.";
    if (Number.isFinite(poolTH) && poolTH > 0) {
      hashText = fmtPoolHashrateTHS(poolTH, poolMock);
      hashTitle = poolMock
        ? "Simulated totals (HACKME_NETWORK_MOCK)."
        : "Live coordinator aggregate (sub-1 TH/s shown as GH/s).";
    }

    let tipHeight = NaN;
    if (global && global.chain) {
      const canon = Number(global.chain.canonical_tip_height);
      const local = Number(global.chain.tip_height);
      if (Number.isFinite(canon) && canon > 0) tipHeight = canon;
      else if (Number.isFinite(local) && local >= 0) tipHeight = local;
    }
    if (!Number.isFinite(tipHeight) || tipHeight < 0) {
      const wc = work && work.canonical_tip_height != null ? Number(work.canonical_tip_height) : NaN;
      if (Number.isFinite(wc) && wc > 0) tipHeight = wc;
      else tipHeight = Number(work && work.tip_height);
    }
    const heightText =
      Number.isFinite(tipHeight) && tipHeight >= 0
        ? Math.floor(tipHeight).toLocaleString("en-US")
        : "—";

    let diff = global && global.work ? Number(global.work.target_mod) : NaN;
    if (!Number.isFinite(diff) || diff <= 0) {
      diff = Number(work && work.target_mod);
    }
    const diffText =
      Number.isFinite(diff) && diff > 0 ? Math.floor(diff).toLocaleString("en-US") : "—";

    let reward = Number(work && work.base_reward_hmc);
    if (!Number.isFinite(reward) || reward <= 0) {
      reward = Number(work && work.found_bonus_hmc);
    }
    const rewardText = Number.isFinite(reward) && reward > 0 ? `${reward.toFixed(6)} HMC` : "—";

    const online = Number(
      (work && work.workers_online) ||
        (work && work.miners) ||
        (global && global.work && global.work.workers_online) ||
        NaN
    );
    const known = Number(
      (work && work.workers_count) ||
        (global && global.work && global.work.workers_count) ||
        (work && work.workers && Object.keys(work.workers).length) ||
        0
    );
    const miners = Number.isFinite(online) && online >= 0 ? online : known;
    const workersText = Number.isFinite(miners) && miners >= 0 ? String(Math.floor(miners)) : "—";

    const poolOk = (global && global.work && global.work.ok) || (work && work.issued_ranges != null);
    let statusText = "degraded";
    if (poolOk) {
      statusText = miners > 0 ? "online" : "online · idle";
    }

    const snapshot = {
      hash: hashText,
      hashTitle,
      workers: workersText,
      height: heightText,
      reward: rewardText,
      diff: diffText,
      status: statusText,
      ts: Date.now(),
    };
    writePoolCache(snapshot);
    paintPoolRow(snapshot, { degraded: statusText === "degraded" });
    if (hashEl) hashEl.title = hashTitle;
    if (wEl) {
      wEl.title = "Workers with recent submit activity (public coordinator).";
    }
  }

  async function updateLiveStatus(liveEl) {
    if (!liveEl) return;
    const work = await fetchJson(coordinatorApi("/api/work/stats"), 3500);
    if (work && work.issued_ranges != null) {
      liveEl.textContent = "online";
      return;
    }
    const global = await fetchJson(`${apiRoot()}/global/metrics`, 3500);
    liveEl.textContent = global && global.work && global.work.ok ? "online" : "degraded";
  }

  const { liveEl } = paintDomainMeta();
  wireExplorerLinks();
  wireNewsLinks();
  wireCoinsLinks();
  wireStandardNav();
  wireCommunityFooter();
  wireDownloadLinks();
  {
    const href = resolveWindowsDownloadHref();
    setHref("download-win", href);
    const primary = document.getElementById("download-win");
    if (primary && href && href.endsWith(".exe")) {
      primary.textContent = "Download HackMe Setup (.exe)";
    }
  }
  void renderNewsPage();
  void renderNewsHealth();
  hydratePoolFromCache();
  void updateLiveStatus(liveEl);
  void loadPoolOverview();
  setInterval(() => updateLiveStatus(liveEl), 15000);
  setInterval(loadPoolOverview, 12000);
  setInterval(renderNewsHealth, 20000);
})();
