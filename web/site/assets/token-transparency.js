(() => {
  const API = typeof window.HackMeApiRoot === "function" ? window.HackMeApiRoot() : "/api";

  function esc(s) {
    return String(s ?? "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");
  }

  function fmt(n, digits = 4) {
    const x = Number(n);
    if (!Number.isFinite(x)) return "—";
    if (x >= 1e6) return x.toLocaleString(undefined, { maximumFractionDigits: 2 });
    return x.toLocaleString(undefined, { minimumFractionDigits: 0, maximumFractionDigits: digits });
  }

  function pct(part, whole) {
    const p = Number(part);
    const w = Number(whole);
    if (!Number.isFinite(p) || !Number.isFinite(w) || w <= 0) return "—";
    return ((p / w) * 100).toFixed(4) + "%";
  }

  async function fetchJson(path) {
    try {
      const r = await fetch(API.replace(/\/$/, "") + path, { cache: "no-store" });
      if (!r.ok) return null;
      return await r.json();
    } catch (_) {
      return null;
    }
  }

  function setHtml(id, html) {
    const el = document.getElementById(id);
    if (el) el.innerHTML = html;
  }

  async function paintHMC(st) {
    const ec = st?.economics || {};
    const rows = [
      ["Max supply", fmt(ec.max_supply_hmc, 0) + " HMC"],
      ["Total minted", fmt(ec.total_minted_hmc) + " HMC"],
      ["Burned (tally)", fmt(ec.total_burned_hmc) + " HMC"],
      ["Circulating", fmt(ec.circulating_hmc) + " HMC"],
      ["Mint remaining", fmt(ec.mint_remaining_hmc) + " HMC"],
      ["Base reward now", fmt(ec.base_reward_now_hmc) + " HMC/block"],
      ["Tail floor", fmt(ec.reward_tail_floor_hmc) + " HMC"],
      ["Policy hash", `<span class="code-inline">${esc(ec.policy_hash || st?.policy_hash || "—")}</span>`],
      ["Treasury (DevFee)", `<a href="/explorer-lite.html">HMC-719006d93916ad52</a>`],
    ];
    setHtml(
      "transparency-hmc",
      `<table class="pool-table"><tbody>${rows
        .map(([k, v]) => `<tr><th>${esc(k)}</th><td>${v}</td></tr>`)
        .join("")}</tbody></table>`
    );
    setHtml("transparency-hmc-pct", `Circulating is <strong>${pct(ec.circulating_hmc, ec.max_supply_hmc)}</strong> of max supply (live).`);
  }

  async function paintSUP(sup) {
    const ec = sup?.economics || {};
    const rows = [
      ["Max supply", fmt(ec.max_supply_sup, 0) + " SUP"],
      ["Total minted", fmt(ec.total_minted_sup) + " SUP"],
      ["Remaining", fmt(ec.remaining_sup) + " SUP"],
      ["Mint enabled", ec.mint_enabled ? "Yes" : "No"],
      ["On-chain settle", ec.on_chain_settle_live ? "Live" : "Pending"],
      ["Genesis unix", ec.genesis_unix ? new Date(ec.genesis_unix * 1000).toISOString() : "—"],
    ];
    setHtml(
      "transparency-sup",
      `<table class="pool-table"><tbody>${rows
        .map(([k, v]) => `<tr><th>${esc(k)}</th><td>${v}</td></tr>`)
        .join("")}</tbody></table>`
    );
    setHtml("transparency-sup-pct", `Minted is <strong>${pct(ec.total_minted_sup, ec.max_supply_sup)}</strong> of max supply (live).`);
  }

  async function paintHMS(hms) {
    const ec = hms?.economics || hms || {};
    if (!ec.max_supply_hms && !ec.max_supply) {
      setHtml("transparency-hms", `<p class="muted">HMS preview lane — economics API not reachable. See <a href="./coin-hms.html">HMS overview</a>.</p>`);
      return;
    }
    const max = ec.max_supply_hms ?? ec.max_supply;
    const rows = [
      ["Lane status", "Preview — economics API live; not CEX-listed"],
      ["Max supply", fmt(max, 0) + " HMS"],
      ["Total minted", fmt(ec.total_minted_hms ?? ec.total_minted) + " HMS"],
      ["Treasury genesis float", (ec.treasury_genesis_float_pct ?? 0.5) + "%"],
      ["Mint enabled", ec.mint_enabled ? "Yes (preview)" : "Prelaunch"],
    ];
    setHtml(
      "transparency-hms",
      `<table class="pool-table"><tbody>${rows
        .map(([k, v]) => `<tr><th>${esc(k)}</th><td>${v}</td></tr>`)
        .join("")}</tbody></table>`
    );
  }

  function wireTabs() {
    document.querySelectorAll("[data-transparency-tab]").forEach((btn) => {
      btn.addEventListener("click", () => {
        const tab = btn.dataset.transparencyTab;
        document.querySelectorAll("[data-transparency-tab]").forEach((b) => b.classList.toggle("active", b === btn));
        document.querySelectorAll("[data-transparency-panel]").forEach((p) => {
          p.hidden = p.dataset.transparencyPanel !== tab;
        });
      });
    });
  }

  async function main() {
    wireTabs();
    const [st, sup, hms] = await Promise.all([
      fetchJson("/status"),
      fetchJson("/sup/economics"),
      fetchJson("/hms/economics"),
    ]);
    if (st) await paintHMC(st);
    if (sup) await paintSUP(sup);
    await paintHMS(hms);
    const stamp = document.getElementById("transparency-updated");
    if (stamp) stamp.textContent = "Updated " + new Date().toISOString().replace("T", " ").slice(0, 19) + " UTC";
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", main);
  else main();
})();
