/**
 * Unified header + footer for hackme.tech static pages.
 * Mark pages: <nav class="nav" data-site-nav data-site-page="coins"></nav>
 */
(() => {
  const PAGES = {
    home: { href: "./index.html", label: "Home" },
    coins: { href: "./coins.html", label: "Coins" },
    transparency: { href: "./token-transparency.html", label: "Transparency" },
    roadmap: { href: "./roadmap.html", label: "Roadmap" },
    listing: { href: "./listing.html", label: "Listing" },
    mine: { href: "./downloads.html#start", label: "Mine" },
    downloads: { href: "./downloads.html", label: "Download" },
    fuzz: { href: "./fuzz-campaigns.html", label: "Fuzz" },
    research: { href: "./research.html", label: "Research" },
    developers: { href: "./developers.html", label: "Developers" },
    docs: { href: "./docs.html", label: "Docs" },
    news: { href: "./news.html", label: "News" },
    economics: { href: "./economics-model.html", label: "Economics" },
    operators: { href: "./operator-checklist.html", label: "Operators" },
    contacts: { href: "./contacts.html", label: "Contacts" },
    legal: { href: "./legal.html", label: "Legal" },
    privacy: { href: "./legal-privacy.html", label: "Privacy" },
    explorer: { href: "/pool/explorer", label: "Explorer", external: true },
    github: { href: "https://github.com/jokeez/hackme", label: "GitHub", external: true },
  };

  const PRIMARY = ["mine", "coins", "fuzz", "research", "downloads", "docs"];
  const MORE = ["news", "economics", "transparency", "roadmap", "listing", "operators", "developers", "contacts", "legal", "privacy", "explorer"];

  const FOOTER_GROUPS = [
    { title: "Network", keys: ["home", "coins", "transparency", "roadmap", "research", "github", "explorer"] },
    { title: "Start", keys: ["mine", "downloads", "docs", "listing", "operators"] },
    { title: "Product", keys: ["fuzz", "developers", "economics", "news"] },
    { title: "Legal", keys: ["contacts", "legal", "privacy"] },
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
      "operator-checklist": "operators",
      "legal-privacy": "privacy",
      "legal-risk": "legal",
      "legal-eula": "legal",
      "legal-terms": "legal",
      "api-reference": "developers",
      "developer-console": "developers",
      "developer-dashboard": "developers",
      "security-notes": "docs",
      "security-rewards": "docs",
    };
    return map[base] || base.replace(/-/g, "_");
  }

  function link(p, active) {
    const meta = PAGES[p];
    if (!meta) return "";
    const cur = active === p ? ' aria-current="page"' : "";
    const ext = meta.external ? ' target="_blank" rel="noreferrer"' : "";
    return `<a href="${meta.href}"${cur}${ext}>${meta.label}</a>`;
  }

  function renderNav(active) {
    const home = active !== "home" ? link("home", active) : "";
    const primary = PRIMARY.map((k) => link(k, active)).join("");
  const moreItems = MORE.map((k) => link(k, active)).join("");
    return `
      ${home}
      ${primary}
      <details class="nav-more">
        <summary>More</summary>
        <div class="nav-more-menu">${moreItems}</div>
      </details>
    `;
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
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", mount);
  } else {
    mount();
  }
})();
