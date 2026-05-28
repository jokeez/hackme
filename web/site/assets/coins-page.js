(() => {
  const POOL_STATS = "https://hackme.tech/pool/coordinator/api/pool/stats";
  const WORK_STATS = "https://hackme.tech/pool/coordinator/api/work/stats";

  function esc(s) {
    return String(s ?? "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  function statusClass(st) {
    if (st === "live" || st === "accrual") return "live";
    if (st === "prelaunch") return "prelaunch";
    return "planned";
  }

  function iconHtml(coin) {
    if (coin.iconType === "image" && coin.icon) {
      const cls =
        coin.id === "sup" ? "sup" : coin.id === "hmc" ? "live" : coin.id === "hms" ? "hms" : "planned";
      return `<div class="coin-icon ${cls}"><img src="${esc(coin.icon)}" alt="${esc(coin.symbol)}" width="52" height="52" /></div>`;
    }
    const accent = coin.accent || "planned";
    return `<div class="coin-icon planned" style="--coin-glow:#fbbf24">${esc(coin.iconLetter || coin.symbol)}</div>`;
  }

  function renderCard(coin) {
    const st = statusClass(coin.status);
    const highlights =
      Array.isArray(coin.highlights) && coin.highlights.length
        ? `<ul class="coin-highlights">${coin.highlights.map((h) => `<li>${esc(h)}</li>`).join("")}</ul>`
        : "";
    const links = coin.links
      ? Object.entries(coin.links)
          .map(([k, url]) => `<a href="${esc(url)}" target="_blank" rel="noreferrer">${esc(k)}</a>`)
          .join("")
      : "";
    return `
      <article class="coin-card glass" data-coin-id="${esc(coin.id)}" style="--coin-glow:${coin.id === "sup" ? "#a855f7" : coin.id === "hms" ? "#34d399" : "#4de4ff"}">
        <div class="coin-card-head">
          ${iconHtml(coin)}
          <div>
            <span class="coin-status ${st}">${esc(coin.status)}</span>
            <h3>${esc(coin.name)} <span class="muted">(${esc(coin.symbol)})</span></h3>
          </div>
        </div>
        <p class="coin-tagline">${esc(coin.tagline)}</p>
        <ul class="coin-meta-list">
          <li><strong>Algorithm</strong> ${esc(coin.algorithm)}</li>
          <li><strong>Listing</strong> ${esc(coin.listing)}</li>
          <li><strong>Earn</strong> ${esc(coin.earn)}</li>
          <li><strong>Trade</strong> ${esc(coin.trade)}</li>
        </ul>
        ${highlights}
        ${links ? `<div class="coin-links">${links}</div>` : ""}
      </article>`;
  }

  async function fetchJson(url) {
    try {
      const r = await fetch(url, { cache: "no-store" });
      if (!r.ok) return null;
      return await r.json();
    } catch (_) {
      return null;
    }
  }

  function fmtHash(h) {
    const n = Number(h);
    if (!Number.isFinite(n) || n <= 0) return "—";
    if (n >= 1e9) return (n / 1e9).toFixed(2) + " GH/s";
    if (n >= 1e6) return (n / 1e6).toFixed(2) + " MH/s";
    return n.toFixed(0) + " H/s";
  }

  async function paintLiveStats() {
    const pool = await fetchJson(POOL_STATS);
    const work = await fetchJson(WORK_STATS);
    const el = document.getElementById("coins-live-stats");
    if (!el) return;
    const miners = pool && pool.miners != null ? pool.miners : work && work.miners;
    const hash = pool && pool.hashrate != null ? pool.hashrate : work && work.hashrate_hs;
    el.innerHTML = `
      <span class="pill">Pool miners: <strong>${esc(miners ?? "—")}</strong></span>
      <span class="pill">Pool hashrate: <strong>${esc(fmtHash(hash))}</strong></span>
      <span class="pill">HMC settlement: live on hackme.tech</span>`;
  }

  async function init() {
    const grid = document.getElementById("coins-grid");
    if (!grid) return;
    let data;
    try {
      const r = await fetch("./assets/ecosystem-coins.json", { cache: "no-store" });
      data = await r.json();
    } catch (_) {
      grid.innerHTML = "<p class=\"muted\">Could not load coin registry.</p>";
      return;
    }
    const coins = data.coins || [];
    const sup = coins.find((c) => c.id === "sup");
    const rest = coins.filter((c) => c.id !== "sup");
    const order = sup ? [sup, ...rest.filter((c) => c.id === "hmc"), ...rest.filter((c) => c.id !== "hmc" && c.id !== "sup")] : coins;
    grid.innerHTML = order.map(renderCard).join("");
    void paintLiveStats();
    setInterval(paintLiveStats, 20000);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
