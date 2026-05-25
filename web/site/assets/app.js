(() => {
  /** Bump together with dist/release_<VERSION>/ and scripts/release/make_release_bundle.sh */
  const RELEASE_VER = "0.1.0-rc11h";

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
    annMd: "https://github.com/jokeez/hackme/blob/main/docs/BITCOINTALK_ANN.md",
    tgChannel: "https://t.me/hackme_tech",
    tgEn: "https://t.me/hackme_en",
    tgRu: "https://t.me/hackme_ru",
  };

  const CONFIG = {
    explorerUrl: "/pool/explorer",
    newsUrl: "./news.html",
    newsFeed: "./assets/news.json",
    releaseChannel: RELEASE_VER,
    releaseChannelNote: "desktop mode candidate",
    releaseBase: `/dist/release_${RELEASE_VER}`,
    windowsInstaller: `/dist/release_${RELEASE_VER}/HackMe-Setup-${RELEASE_VER}.exe`,
    windowsBundle: `/dist/release_${RELEASE_VER}/hackme_${RELEASE_VER}_windows_setup.zip`,
    windowsBundleLegacy: `/dist/release_${RELEASE_VER}/hackme_${RELEASE_VER}_windows.zip`,
    linuxBundle: `/dist/release_${RELEASE_VER}/hackme_${RELEASE_VER}_linux.tar.gz`,
    hackmeOSIso: `/dist/release_${RELEASE_VER}/HackMe-OS-${RELEASE_VER}-amd64.iso`,
    hackmeOSIsoLegacy: `/dist/release_${RELEASE_VER}/HackMe-Miner-${RELEASE_VER}-amd64.iso`,
    hackmeOSSha: `/dist/release_${RELEASE_VER}/SHA256SUMS-iso.txt`,
    fuzzingLinux: `/dist/release_${RELEASE_VER}/hackme-fuzzing-${RELEASE_VER}-linux-amd64`,
    fuzzingWindows: `/dist/release_${RELEASE_VER}/hackme-fuzzing-${RELEASE_VER}-windows-amd64.exe`,
    shaSums: `/dist/release_${RELEASE_VER}/SHA256SUMS.txt`,
    manifest: `/dist/release_${RELEASE_VER}/RELEASE_MANIFEST.json`,
    buildInfo: `/dist/release_${RELEASE_VER}/BUILD_INFO.txt`,
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
    ensureNewsLink(".nav");
    ensureNewsLink(".footer-nav");
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
    document.querySelectorAll(".footer-nav").forEach((nav) => {
      ensureFooterLink(nav, COMMUNITY.github, "GitHub");
    });
  }

  async function resolveWindowsDownloadHref() {
    const inst = CONFIG.windowsInstaller;
    if (!inst) return CONFIG.windowsBundle;
    try {
      const r = await fetch(inst, { method: "HEAD", cache: "no-store" });
      if (r.ok) return inst;
    } catch (_) {}
    return CONFIG.windowsBundle;
  }

  async function resolveHackMeOSIsoHref() {
    const candidates = [CONFIG.hackmeOSIso, CONFIG.hackmeOSIsoLegacy].filter(Boolean);
    for (const url of candidates) {
      try {
        const r = await fetch(url, { method: "HEAD", cache: "no-store" });
        if (r.ok) return url;
      } catch (_) {}
    }
    return "";
  }

  function wireDownloadLinks() {
    setHref("download-win", CONFIG.windowsInstaller || CONFIG.windowsBundle);
    setHref("download-win-zip", CONFIG.windowsBundle);
    setHref("download-linux", CONFIG.linuxBundle);
    setHref("download-fuzzing-linux", CONFIG.fuzzingLinux);
    setHref("download-fuzzing-win", CONFIG.fuzzingWindows);
    setHref("download-sha", CONFIG.shaSums);
    setHref("download-iso-sha", CONFIG.hackmeOSSha);
    setHref("download-iso-sha-card", CONFIG.hackmeOSSha);
    setHref("download-manifest", CONFIG.manifest);
    setHref("download-buildinfo", CONFIG.buildInfo);
    const verEl = document.getElementById("dl-release-ver");
    if (verEl) verEl.textContent = CONFIG.releaseChannel || RELEASE_VER;
    void resolveHackMeOSIsoHref().then((isoHref) => {
      const isoBtn = document.getElementById("download-iso");
      const isoStat = document.getElementById("download-iso-status");
      if (!isoBtn) return;
      if (isoHref) {
        isoBtn.href = isoHref;
        isoBtn.classList.remove("btn-disabled");
        if (isoStat) isoStat.textContent = "ISO available — verify SHA256SUMS-iso.txt before flashing.";
      } else {
        isoBtn.href = "#hackme-os";
        if (isoStat) {
          isoStat.textContent =
            "ISO build publishing soon — watch News or build from source: scripts/release/iso/build_hackme_miner_iso.sh";
        }
      }
    });
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
    const global = await fetchJson(`${apiRoot()}/global/metrics`);
    if (!global) {
      heightEl.textContent = "—";
      if (tipEl) tipEl.textContent = "—";
      if (hashrateEl) hashrateEl.textContent = "—";
      if (miningEl) miningEl.textContent = "degraded";
      return;
    }
    const height = Number(global.chain && global.chain.tip_height != null ? global.chain.tip_height : 0);
    const tip = String((global.chain && global.chain.tip_hash) || "");
    const poolTH = Number(global.network && global.network.global_hashrate_th_s);
    const mock = global.network && global.network.global_mock === true;
    heightEl.textContent = Number.isFinite(height) && height > 0 ? Math.floor(height).toLocaleString("en-US") : "—";
    if (tipEl) {
      tipEl.textContent = shortHash(tip, 22);
      tipEl.title = tip || "";
    }
    if (hashrateEl) {
      hashrateEl.textContent = Number.isFinite(poolTH) && poolTH > 0 ? fmtPoolHashrateTHS(poolTH, mock) : "—";
    }
    if (miningEl) {
      const miners = Number(global.work && global.work.workers_count);
      miningEl.textContent =
        global.work && global.work.ok
          ? miners > 0
            ? `online · ${miners} worker${miners === 1 ? "" : "s"}`
            : "online"
          : "online";
    }
  }

  async function renderNewsPage() {
    const listEl = document.getElementById("news-list");
    if (!listEl) return;
    const filtersEl = document.getElementById("news-tag-filters");
    const searchEl = document.getElementById("news-search");
    const featuredEl = document.getElementById("news-featured");
    const emptyEl = document.getElementById("news-empty");
    let items = [];
    try {
      const resp = await fetch(CONFIG.newsFeed, { cache: "no-store" });
      if (!resp.ok) throw new Error("news unavailable");
      const body = await resp.json();
      items = Array.isArray(body.items) ? body.items.slice() : [];
    } catch (_) {
      if (emptyEl) {
        emptyEl.classList.remove("hidden");
        emptyEl.textContent = "News feed is temporarily unavailable.";
      }
      return;
    }
    items.sort((a, b) => parseDateSafe(b.date) - parseDateSafe(a.date));
    const tags = Array.from(
      new Set(items.flatMap((it) => Array.isArray(it.tags) ? it.tags : []))
    ).sort((a, b) => String(a).localeCompare(String(b)));

    let activeTag = "all";
    let query = "";

    function renderFilters() {
      if (!filtersEl) return;
      const all = ["all", ...tags];
      filtersEl.innerHTML = all
        .map((tag) => {
          const active = activeTag === tag ? " active" : "";
          const label = tag === "all" ? "All" : String(tag);
          return `<button type="button" class="news-filter-btn${active}" data-tag="${escapeHtml(tag)}">${escapeHtml(label)}</button>`;
        })
        .join("");
      filtersEl.querySelectorAll(".news-filter-btn").forEach((btn) => {
        btn.addEventListener("click", () => {
          activeTag = btn.getAttribute("data-tag") || "all";
          renderList();
          renderFilters();
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
      const tagsHtml = (Array.isArray(it.tags) ? it.tags : [])
        .map((tag) => `<span class="news-tag">#${escapeHtml(tag)}</span>`)
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
      if (activeTag !== "all") {
        const t = Array.isArray(it.tags) ? it.tags : [];
        if (!t.includes(activeTag)) return false;
      }
      if (!query) return true;
      const hay = [
        it.title,
        it.summary,
        it.impact,
        it.action,
        ...(Array.isArray(it.tags) ? it.tags : []),
      ].join(" ").toLowerCase();
      return hay.includes(query);
    }

    function renderList() {
      const filtered = items.filter(matches);
      if (featuredEl) {
        featuredEl.innerHTML = filtered.length > 0 ? renderItem(filtered[0]) : "";
      }
      const listItems = filtered.slice(1);
      listEl.innerHTML = listItems.map(renderItem).join("");
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
    const hEl = document.getElementById("pool-live-height");
    const rEl = document.getElementById("pool-live-reward");
    const dEl = document.getElementById("pool-live-diff");
    const sEl = document.getElementById("pool-live-status");
    if (!hashEl || !hEl || !rEl || !dEl || !sEl) return;

    const root = apiRoot();
    const [global, work] = await Promise.all([
      fetchJson(`${root}/global/metrics`),
      fetchJson(coordinatorApi("/api/work/stats")),
    ]);

    if (!global && !work) {
      hashEl.textContent = "—";
      hashEl.removeAttribute("title");
      hEl.textContent = "—";
      rEl.textContent = "—";
      dEl.textContent = "—";
      sEl.textContent = "degraded";
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
    }

    if (Number.isFinite(poolTH) && poolTH > 0) {
      hashEl.textContent = fmtPoolHashrateTHS(poolTH, poolMock);
      hashEl.title = poolMock
        ? "Simulated totals (HACKME_NETWORK_MOCK)."
        : "Live coordinator aggregate (sub-1 TH/s shown as GH/s).";
    } else {
      hashEl.textContent = "—";
      hashEl.title = "Pool aggregate unavailable.";
    }

    let tipHeight = global && global.chain ? Number(global.chain.tip_height) : NaN;
    if (!Number.isFinite(tipHeight) || tipHeight < 0) {
      tipHeight = Number(work && (work.canonical_tip_height != null ? work.canonical_tip_height : work.tip_height));
    }
    hEl.textContent =
      Number.isFinite(tipHeight) && tipHeight >= 0
        ? Math.floor(tipHeight).toLocaleString("en-US")
        : "—";

    let diff = global && global.work ? Number(global.work.target_mod) : NaN;
    if (!Number.isFinite(diff) || diff <= 0) {
      diff = Number(work && work.target_mod);
    }
    dEl.textContent =
      Number.isFinite(diff) && diff > 0 ? Math.floor(diff).toLocaleString("en-US") : "—";

    let reward = Number(work && work.base_reward_hmc);
    if (!Number.isFinite(reward) || reward <= 0) {
      reward = Number(work && work.found_bonus_hmc);
    }
    rEl.textContent = Number.isFinite(reward) && reward > 0 ? `${reward.toFixed(6)} HMC` : "—";

    const miners = Number(
      (global && global.work && global.work.workers_count) ||
        (work && work.workers_count) ||
        (work && work.workers && Object.keys(work.workers).length) ||
        0
    );
    const poolOk = (global && global.work && global.work.ok) || (work && work.issued_ranges != null);
    if (poolOk) {
      sEl.textContent = miners > 0 ? `online · ${miners} worker${miners === 1 ? "" : "s"}` : "online";
    } else {
      sEl.textContent = "degraded";
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
  wireCommunityFooter();
  wireDownloadLinks();
  void resolveWindowsDownloadHref().then((href) => {
    setHref("download-win", href);
    const primary = document.getElementById("download-win");
    if (primary && href && href.endsWith(".exe")) {
      primary.textContent = "Download HackMe Setup (.exe)";
    }
  });
  void renderNewsPage();
  void renderNewsHealth();
  void updateLiveStatus(liveEl);
  void loadPoolOverview();
  setInterval(() => updateLiveStatus(liveEl), 15000);
  setInterval(loadPoolOverview, 12000);
  setInterval(renderNewsHealth, 20000);
})();
