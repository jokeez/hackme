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
    transparency: { href: "/token-transparency.html", label: "Transparency" },
    roadmap: { href: "/roadmap.html", label: "Roadmap" },
    listing: { href: "/listing.html", label: "Listing" },
    mine: { href: "/downloads.html#start", label: "Mine" },
    downloads: { href: "/downloads.html", label: "Download" },
    fuzz: { href: "/fuzz-campaigns.html", label: "Fuzz" },
    research: { href: "/research.html", label: "Research" },
    developers: { href: "/developers.html", label: "Developers" },
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

  const PRIMARY = ["mine", "coins", "research", "docs"];
  const MORE = ["fuzz", "developers", "news", "economics", "transparency", "roadmap", "listing", "rewards", "contacts", "legal", "privacy", "explorer", "downloads"];

  const FOOTER_GROUPS = [
    { title: "Network", keys: ["home", "coins", "transparency", "roadmap", "research", "github", "explorer"] },
    { title: "Start", keys: ["mine", "downloads", "docs", "listing"] },
    { title: "Product", keys: ["fuzz", "developers", "economics", "news"] },
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

  function wireNavMoreDismiss() {
    document.querySelectorAll(".nav-more").forEach((details) => {
      if (details.dataset.navWired === "1") return;
      details.dataset.navWired = "1";
      document.addEventListener("click", (ev) => {
        if (!details.open) return;
        const t = ev.target;
        if (t instanceof Node && details.contains(t)) return;
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

  function ensureDisclosureTicker() {
    if (!document.querySelector("header.topbar") || document.querySelector(".disclosure-ticker")) {
      return;
    }
    if (document.querySelector('script[data-disclosure-ticker="1"]')) {
      return;
    }
    const s = document.createElement("script");
    s.src = "/assets/disclosure-ticker.js?v=20260807wow2";
    s.defer = true;
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

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", mount);
  } else {
    mount();
  }
})();
