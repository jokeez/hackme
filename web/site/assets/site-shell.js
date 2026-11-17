/**
 * Unified header + footer for hackme.tech static pages.
 * Mark pages: <nav class="nav" data-site-nav data-site-page="coins"></nav>
 */
(() => {
  // Root-absolute hrefs so nested pages (/reports/oss-cve/…) don't resolve to
  // /reports/oss-cve/coins.html etc.
  const PAGES = {
    home: { href: "/", label: "Home" },
    coins: { href: "/coins.html", label: "Coins" },
    exchange: {
      href: "https://exchange.hackme.tech/",
      label: "Exchange",
      external: true,
    },
    transparency: { href: "/token-transparency.html", label: "Transparency" },
    roadmap: { href: "/roadmap.html", label: "Roadmap" },
    listing: { href: "/listing.html", label: "Listing" },
    mine: { href: "/downloads.html#start", label: "Mine" },
    downloads: { href: "/downloads.html", label: "Download" },
    fuzz: { href: "/fuzz-campaigns.html", label: "Fuzz" },
    research: { href: "/research.html", label: "Research" },
    developers: { href: "/developers.html", label: "Developers" },
    orders: { href: "/orders.html", label: "Orders" },
    docs: { href: "/docs.html", label: "Docs" },
    news: { href: "/news.html", label: "News" },
    economics: { href: "/economics-model.html", label: "Economics" },
    contacts: { href: "/contacts.html", label: "Contacts" },
    rewards: { href: "/security-rewards.html", label: "Bug bounty" },
    legal: { href: "/legal.html", label: "Legal" },
    privacy: { href: "/legal-privacy.html", label: "Privacy" },
    explorer: { href: "/explorer-lite.html", label: "Explorer" },
    github: { href: "https://github.com/jokeez/hackme", label: "GitHub", external: true },
  };

  const PRIMARY = ["mine", "coins", "exchange", "research", "docs"];
  const MORE = ["orders", "fuzz", "developers", "news", "economics", "transparency", "roadmap", "listing", "rewards", "contacts", "legal", "privacy", "explorer", "downloads"];

  const FOOTER_GROUPS = [
    { title: "Network", keys: ["home", "coins", "exchange", "transparency", "roadmap", "research", "github", "explorer"] },
    { title: "Start", keys: ["mine", "downloads", "docs", "listing"] },
    { title: "Product", keys: ["orders", "fuzz", "developers", "economics", "news"] },
    { title: "Legal", keys: ["contacts", "rewards", "legal", "privacy"] },
  ];

  function pageKey() {
    const nav = document.querySelector("[data-site-page]");
    if (nav && nav.dataset.sitePage) return nav.dataset.sitePage;
    const path = (location.pathname || "").split("/").pop() || "index.html";
    if (path === "" || path === "index.html") return "home";
    const base = path.replace(/\.html$/, "");
    const map = {
      "fuzz-guide": "fuzz",
      "fuzz-marketplace": "fuzz",
      "fuzz-campaigns": "fuzz",
      "fuzzing-console": "fuzz",
      "economics-model": "economics",
      "token-transparency": "transparency",
      "roadmap": "roadmap",
      "listing": "listing",
      "coin-hmc": "coins",
      "coin-sup": "coins",
      "coin-hms": "coins",
      "legal-privacy": "privacy",
      "legal-risk": "legal",
      "legal-eula": "legal",
      "legal-terms": "legal",
      "api-reference": "developers",
      "developer-console": "developers",
      "developer-dashboard": "developers",
      "orders": "orders",
      "security-notes": "docs",
      "security-rewards": "rewards",
    };
    return map[base] || base.replace(/-/g, "_");
  }

  function link(p, active) {
    const meta = PAGES[p];
    if (!meta) return "";
    const cur = active === p ? ' aria-current="page"' : "";
    const ext = meta.external ? ' target="_blank" rel="noreferrer"' : "";
    let cls = "";
    if (p === "mine") cls = ' class="nav-mine-cta"';
    else if (p === "exchange") cls = ' class="nav-link-exchange"';
    else if (p === "fuzz") cls = ' class="nav-link-fuzz"';
    return `<a href="${meta.href}"${cls}${cur}${ext}>${meta.label}</a>`;
  }

  function renderNav(active) {
    const home = active !== "home" ? link("home", active) : "";
    const primary = PRIMARY.map((k) => link(k, active)).join("");
    const moreItems = MORE.map((k) => link(k, active)).join("");
    return `
      ${home}
      ${primary}
      <details class="nav-more">
        <summary aria-haspopup="true">More</summary>
        <div class="nav-more-menu" role="menu">${moreItems}</div>
      </details>
    `;
  }

  function navMoreMenu(details) {
    if (details._navMoreMenu) return details._navMoreMenu;
    const menu = details.querySelector(".nav-more-menu");
    if (menu) details._navMoreMenu = menu;
    return menu;
  }

  function restoreNavMoreMenu(details) {
    const menu = details._navMoreMenu || details.querySelector(".nav-more-menu");
    if (!menu) return;
    if (menu.parentElement !== details) details.appendChild(menu);
    menu.style.left = "";
    menu.style.top = "";
    menu.style.right = "";
    menu.style.maxWidth = "";
  }

  function placeNavMoreMenu(details) {
    const menu = navMoreMenu(details);
    const summary = details.querySelector("summary");
    if (!menu || !summary) return;
    if (!details.open) {
      restoreNavMoreMenu(details);
      return;
    }
    // Portal to <body> so sibling stacking (LIVE ticker) cannot cover the panel.
    if (menu.parentElement !== document.body) {
      document.body.appendChild(menu);
    }
    const pad = 12;
    const gap = 6;
    const rect = summary.getBoundingClientRect();
    const prevVis = menu.style.visibility;
    menu.style.visibility = "hidden";
    menu.style.left = "0";
    menu.style.right = "auto";
    menu.style.top = "0";
    const mw = Math.min(menu.offsetWidth || 176, window.innerWidth - pad * 2);
    const mh = menu.offsetHeight || 0;
    menu.style.visibility = prevVis || "";
    let left = rect.right - mw;
    left = Math.max(pad, Math.min(left, window.innerWidth - pad - mw));
    let top = rect.bottom + gap;
    if (top + mh > window.innerHeight - pad && rect.top - gap - mh >= pad) {
      top = rect.top - gap - mh;
    }
    top = Math.max(pad, Math.min(top, window.innerHeight - pad - Math.min(mh, window.innerHeight - pad * 2)));
    menu.style.left = `${Math.round(left)}px`;
    menu.style.right = "auto";
    menu.style.top = `${Math.round(top)}px`;
    menu.style.maxWidth = `${Math.round(window.innerWidth - pad * 2)}px`;
  }

  function wireNavMoreDismiss() {
    document.querySelectorAll(".nav-more").forEach((details) => {
      if (details.dataset.navWired === "1") return;
      details.dataset.navWired = "1";
      const menu = () => navMoreMenu(details);
      const reposition = () => placeNavMoreMenu(details);
      details.addEventListener("toggle", () => {
        if (details.open) {
          requestAnimationFrame(reposition);
        } else {
          restoreNavMoreMenu(details);
        }
      });
      window.addEventListener("resize", () => {
        if (details.open) reposition();
      });
      window.addEventListener(
        "scroll",
        () => {
          if (details.open) reposition();
        },
        { passive: true }
      );
      document.addEventListener("click", (ev) => {
        if (!details.open) return;
        const t = ev.target;
        const m = menu();
        if (t instanceof Node && (details.contains(t) || (m && m.contains(t)))) return;
        details.open = false;
      });
      document.addEventListener("keydown", (ev) => {
        if (ev.key === "Escape" && details.open) details.open = false;
      });
    });
  }

  function renderFooter(active) {
    return FOOTER_GROUPS.map(
      (g) => `
      <div class="footer-col">
        <span class="footer-col-title">${g.title}</span>
        ${g.keys.map((k) => link(k, active)).join("")}
      </div>`
    ).join("");
  }

  const TICKER_CACHE = "20261117rc20";

  function ensureDisclosureTickerStyles() {
    if (document.querySelector('link[data-disclosure-ticker-css="1"]')) return;
    const link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = `/assets/disclosure-ticker.css?v=${TICKER_CACHE}`;
    link.dataset.disclosureTickerCss = "1";
    // Insert early in <head> so mobile ticker rules win over FOUC on inner pages.
    const first = document.head.firstChild;
    if (first) document.head.insertBefore(link, first);
    else document.head.appendChild(link);
  }
  // Paint dark shell immediately even before styles.css finishes (inner pages).
  try {
    document.documentElement.style.backgroundColor = "#05070d";
    document.documentElement.style.colorScheme = "dark";
  } catch (_) {}

  /** Placeholder under topbar — only if page did not ship a static slot. */
  function ensureDisclosureTickerSlot() {
    ensureDisclosureTickerStyles();
    if (document.querySelector(".disclosure-ticker")) return;
    const header = document.querySelector("header.topbar");
    if (!header) return;
    const slot = document.createElement("div");
    slot.className = "disclosure-ticker disclosure-ticker-slot";
    slot.setAttribute("aria-hidden", "true");
    slot.innerHTML = `
      <div class="dt-shell">
        <div class="dt-bar glass dt-bar--skeleton">
          <div class="dt-live">
            <span class="dt-pulse" aria-hidden="true"></span>
            <span class="dt-live-label">LIVE</span>
          </div>
          <div class="dt-poolchip dt-poolchip--pending">
            <span class="dt-poolchip-dot" aria-hidden="true"></span>
            <span class="dt-poolchip-val">pool …</span>
          </div>
          <div class="dt-stage"><div class="dt-slide dt-slide--active dt-slide--skeleton"><span class="dt-skel"></span></div></div>
          <div class="dt-dots" aria-hidden="true"></div>
        </div>
      </div>`;
    header.insertAdjacentElement("afterend", slot);
  }

  function ensureDisclosureTicker() {
    ensureDisclosureTickerSlot();
    if (document.querySelector('script[data-disclosure-ticker="1"]')) {
      return;
    }
    const s = document.createElement("script");
    s.src = `/assets/disclosure-ticker.js?v=${TICKER_CACHE}`;
    // async false / no defer: run ASAP after parse so hydrate is quick; script at end of body is fine
    s.dataset.disclosureTicker = "1";
    document.body.appendChild(s);
  }

  function fixBrandLinks() {
    document.querySelectorAll("a.brand").forEach((a) => {
      a.setAttribute("href", "/");
    });
  }

  function mount() {
    const active = pageKey();
    document.querySelectorAll(".topbar .nav:not([data-site-nav-skip])").forEach((nav) => {
      nav.innerHTML = renderNav(active);
    });
    document.querySelectorAll(".footer .footer-nav:not([data-site-nav-skip])").forEach((foot) => {
      foot.classList.add("footer-nav-cols");
      foot.innerHTML = renderFooter(active);
    });
    document.querySelectorAll("#explorer-link-top, #explorer-link-hero, #explorer-link-card").forEach((el) => {
      if (!el.getAttribute("href")) el.setAttribute("href", PAGES.explorer.href);
    });
    fixBrandLinks();
    wireNavMoreDismiss();
    ensureDisclosureTicker();
  }

  // Slot first (no-op if static HTML already present), then full mount.
  ensureDisclosureTickerSlot();

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", mount);
  } else {
    mount();
  }
})();
