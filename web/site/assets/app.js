(() => {
  /** Bump together with dist/release_<VERSION>/ and scripts/release/make_release_bundle.sh */
  const RELEASE_VER = "0.1.0-rc10";

  /** Sub-0.1 TH/s → GH/s (matches dashboard / explorer pool strip). */
  function fmtPoolHashrateTHS(ths, mock) {
    const th = Number(ths);
    if (!Number.isFinite(th) || th <= 0) return "—";
    const star = mock ? " *" : "";
    if (th < 0.1) {
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
    windowsBundle: `/dist/release_${RELEASE_VER}/hackme_${RELEASE_VER}_windows.zip`,
    linuxBundle: `/dist/release_${RELEASE_VER}/hackme_${RELEASE_VER}_linux.tar.gz`,
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

  function wireDownloadLinks() {
    setHref("download-win", CONFIG.windowsBundle);
    setHref("download-linux", CONFIG.linuxBundle);
    setHref("download-sha", CONFIG.shaSums);
    setHref("download-manifest", CONFIG.manifest);
    setHref("download-buildinfo", CONFIG.buildInfo);
  }

  /** Public hackme.tech nginx exposes read APIs under /pool/api; dev localhost uses /api. */
  function apiRoot() {
    const h = (window.location.hostname || "").toLowerCase();
    if (h === "localhost" || h === "127.0.0.1" || h === "") return "/api";
    return "/pool/api";
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
    try {
      const root = apiRoot();
      const [statusR, globalR] = await Promise.all([
        fetch(`${root}/status`, { cache: "no-store" }),
        fetch(`${root}/global/metrics`, { cache: "no-store" }),
      ]);
      if (!statusR.ok) throw new Error("status unavailable");
      const status = await statusR.json();
      let global = null;
      if (globalR.ok) global = await globalR.json();
      const height = Number(status.tip_height || 0);
      const tip = String(status.tip_hash || "");
      let poolTH = NaN;
      if (global && global.network) {
        poolTH = Number(global.network.global_hashrate_th_s);
      }
      heightEl.textContent = Number.isFinite(height) ? Math.floor(height).toLocaleString("en-US") : "—";
      tipEl.textContent = shortHash(tip, 22);
      tipEl.title = tip || "";
      hashrateEl.textContent = Number.isFinite(poolTH) ? `${poolTH.toFixed(4)} TH/s` : "—";
      miningEl.textContent = status.mining ? "online / mining" : "online";
    } catch (_) {
      heightEl.textContent = "—";
      if (tipEl) tipEl.textContent = "—";
      if (hashrateEl) hashrateEl.textContent = "—";
      if (miningEl) miningEl.textContent = "degraded";
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
    try {
      const root = apiRoot();
      const [globalR, statusR, metricsR] = await Promise.all([
        fetch(`${root}/global/metrics`, { cache: "no-store" }),
        fetch(`${root}/status`, { cache: "no-store" }),
        fetch(`${root}/metrics`, { cache: "no-store" }),
      ]);
      if (!statusR.ok || !metricsR.ok) throw new Error("api unavailable");
      const status = await statusR.json();
      const metrics = await metricsR.json();

      let global = null;
      if (globalR.ok) {
        global = await globalR.json();
      }

      let poolTH = NaN;
      let poolMock = false;
      if (global && global.network) {
        poolTH = Number(global.network.global_hashrate_th_s);
        poolMock = global.network.global_mock === true;
      }
      if (!Number.isFinite(poolTH)) {
        const nsR = await fetch(`${root}/network/stats`, { cache: "no-store" });
        if (nsR.ok) {
          const ns = await nsR.json();
          poolTH = Number(ns.global_hashrate_th_s);
          poolMock = ns.global_mock === true;
        }
      }

      if (Number.isFinite(poolTH) && poolTH >= 0) {
        hashEl.textContent = fmtPoolHashrateTHS(poolTH, poolMock);
        hashEl.title = poolMock
          ? "Simulated totals (HACKME_NETWORK_MOCK)."
          : "Coordinator aggregate hashrate (GH/s below 0.1 TH/s for readability).";
      } else {
        hashEl.textContent = "—";
        hashEl.title = "Pool aggregate unavailable.";
      }

      const tipRaw =
        global && global.chain != null && global.chain.tip_height != null
          ? global.chain.tip_height
          : status.tip_height;
      const tipHeight = Number(tipRaw);
      hEl.textContent =
        Number.isFinite(tipHeight) && tipHeight >= 0
          ? Math.floor(tipHeight).toLocaleString("en-US")
          : "—";

      let diff = global && global.work ? Number(global.work.target_mod) : NaN;
      if (!Number.isFinite(diff) || diff <= 0) {
        diff = Number(metrics.mining_target_mod || 0);
      }
      dEl.textContent =
        Number.isFinite(diff) && diff > 0 ? Math.floor(diff).toLocaleString("en-US") : "—";

      const reward = Number(metrics.econ_base_reward_now_hmc || 0);
      rEl.textContent = Number.isFinite(reward) ? `${reward.toFixed(6)} HMC` : "—";

      sEl.textContent = status.mining ? "online / mining" : "online";
    } catch (_) {
      hashEl.textContent = "—";
      hashEl.removeAttribute("title");
      hEl.textContent = "—";
      rEl.textContent = "—";
      dEl.textContent = "—";
      sEl.textContent = "degraded";
    }
  }

  async function updateLiveStatus(liveEl) {
    if (!liveEl) return;
    try {
      const resp = await fetch(`${apiRoot()}/status`, { cache: "no-store" });
      liveEl.textContent = resp.ok ? "online" : "degraded";
    } catch (_) {
      liveEl.textContent = "degraded";
    }
  }

  const { liveEl } = paintDomainMeta();
  wireExplorerLinks();
  wireNewsLinks();
  wireCommunityFooter();
  wireDownloadLinks();
  void renderNewsPage();
  void renderNewsHealth();
  void updateLiveStatus(liveEl);
  void loadPoolOverview();
  setInterval(() => updateLiveStatus(liveEl), 10000);
  setInterval(loadPoolOverview, 5000);
  setInterval(renderNewsHealth, 12000);
})();
