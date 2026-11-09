/**
 * HackMe Research hub — domain tables + search.
 *
 * domain: where the target came from
 *   crypto    — L1 / Bitcoin / stratum & mining parsers
 *   libraries — OSS libs (catalog soaks, watches)
 *   bugbounty — bounty / disclosure-shaped picks
 *   custom    — our harnesses, positive controls, calibration
 *   customer  — published-with-consent pilots
 *
 * kind: outcome/status (found | clean | series | live | pilot | archive)
 */
(() => {
  const DOMAINS = [
    {
      id: "crypto",
      label: "Crypto & mining",
      blurb: "L1 stacks, Bitcoin Core series, stratum / pool parsers.",
    },
    {
      id: "libraries",
      label: "OSS libraries",
      blurb: "Catalog & watch soaks on public libraries — often honest CLEAN.",
    },
    {
      id: "bugbounty",
      label: "Bug bounty lane",
      blurb: "Targets chosen for disclosure / bounty surface (not a CVE claim by itself).",
    },
    {
      id: "custom",
      label: "Custom & lab",
      blurb: "Our inventory harnesses, positive controls, pipeline calibration.",
    },
    {
      id: "customer",
      label: "Customer pilots",
      blurb: "Published with consent · redacted customer Scans.",
    },
  ];

  const CASES = [
    {
      id: "P01",
      title: "traplib · ASAN positive control",
      blurb: "Intentional heap OOB / UAF / overflow. Pool Hunt found ASAN heap-buffer-overflow.",
      href: "./reports/portfolio/traplib.html",
      domain: "custom",
      kind: "found",
      tags: ["Hunt", "inventory", "ASAN", "positive-control"],
      when: "2026-11-09",
      stats: { runs: "~665+", crashes: 1, critical: 1, note: "0xA3 path" },
      featured: true,
    },
    {
      id: "P03",
      title: "Exiv2 · custom inventory Hunt",
      blurb: "Non-catalog lib · prebuilt ASAN harness · Ubuntu LTS 0.27.x · BB-shaped pick · CLEAN.",
      href: "./reports/portfolio/exiv2.html",
      domain: "bugbounty",
      kind: "clean",
      tags: ["Hunt", "Exiv2", "custom-harness", "GHSA-lane"],
      when: "2026-11-09",
      stats: { runs: "~3.6k", crashes: 0, critical: 0, note: "CLEAN" },
    },
    {
      id: "P02",
      title: "oniguruma · catalog Hunt soak",
      blurb: "Two hunt_standard pool campaigns on catalog target · ~4k shards each.",
      href: "./reports/portfolio/oniguruma.html",
      domain: "libraries",
      kind: "clean",
      tags: ["Hunt", "catalog", "oniguruma"],
      when: "2026-11-08",
      stats: { runs: "~8k", crashes: 0, critical: 0, note: "CLEAN×2" },
    },
    {
      id: "B2B-1",
      title: "C packet parser · Shannon Bautista",
      blurb: "Promo Scan with consent · seed1 clean · seed2 intentional crash path.",
      href: "./reports/b2b-fuzz/shannon-packet-scan.html",
      domain: "customer",
      kind: "pilot",
      tags: ["B2B", "Scan", "promo"],
      when: "2026-06",
      stats: { runs: 64, crashes: 0, critical: 0, note: "guard on seed2" },
    },
    {
      id: "HW-2026sep",
      title: "Hunt Watch · 2026sep marathon",
      blurb: "12/12 days · ~192.5M iterations · 8 finding families · honesty 2.0.",
      href: "./reports/hunt-watch-2026sep/",
      domain: "libraries",
      kind: "series",
      tags: ["Hunt Watch", "ASAN", "libucl"],
      when: "2026-09",
      stats: { runs: "50 targets", crashes: "8 families", critical: 0, note: "43 CLEAN" },
    },
    {
      id: "OSS-nghttp2",
      title: "OSS CVE Watch · nghttp2",
      blurb: "14/14 days libFuzzer + ASAN · HTTP/2 mem_recv · ~14.32B exec.",
      href: "./reports/oss-cve-watch/",
      domain: "libraries",
      kind: "series",
      tags: ["OSS", "libFuzzer", "nghttp2"],
      when: "2026",
      stats: { runs: "~14.32B", crashes: 0, critical: 0, note: "CLEAN" },
    },
    {
      id: "OSS-libheif",
      title: "OSS CVE Watch · libheif",
      blurb: "14/14 CLEAN · HEIF/AVIF decode · ~2.57B libFuzzer exec.",
      href: "./reports/oss-cve-watch-libheif/",
      domain: "libraries",
      kind: "series",
      tags: ["OSS", "libheif", "libFuzzer"],
      when: "2026",
      stats: { runs: "~2.57B", crashes: 0, critical: 0, note: "CLEAN" },
    },
    {
      id: "OSS-TIERD",
      title: "Upstream ASAN hunts · Tier D",
      blurb: "cfgpack / centijson / libucl disclosure lane · driver-ranked queue.",
      href: "./reports/oss-cve/",
      domain: "bugbounty",
      kind: "series",
      tags: ["OSS CVE", "ASAN", "disclosure"],
      when: "2026",
      stats: { runs: "waves 29+", crashes: "—", critical: 0, note: "mixed" },
    },
    {
      id: "BTC30",
      title: "Bitcoin Core · 30-Day series",
      blurb: "30/30 upstream WASM guards · Fuzz Depth v3 · honest non-CVE scope.",
      href: "./reports/bitcoin30.html",
      domain: "crypto",
      kind: "series",
      tags: ["Bitcoin", "WASM", "Depth v3"],
      when: "2026",
      stats: { runs: 8808, crashes: 1953, critical: 0, note: "signals≠CVE" },
    },
    {
      id: "L1-v4",
      title: "L1 Crypto Stack v4",
      blurb: "BTC · ETH · DOGE · LTC · HackMe — live prod pool tracker.",
      href: "./reports/l1-crypto-stack-v4.html",
      domain: "crypto",
      kind: "live",
      tags: ["L1", "prod-pool", "5-chain"],
      when: "2026-05",
      stats: { runs: "—", crashes: "—", critical: 0, note: "redacted" },
    },
    {
      id: "MKPOOL",
      title: "mkpool parser fuzz",
      blurb: "SV2 / submit hex / V1 framing · 0 critical · no patch requested.",
      href: "./reports/mkpool-fuzz/",
      domain: "crypto",
      kind: "clean",
      tags: ["Stratum", "mkpool"],
      when: "2026-06",
      stats: { runs: "~16k", crashes: 400, critical: 0, note: "guards" },
    },
    {
      id: "CKPOOL",
      title: "ckpool parser fuzz",
      blurb: "Con Kolivas ckpool · MAX_MSGSIZE / parse_submit / ntime.",
      href: "./reports/ckpool-fuzz/",
      domain: "crypto",
      kind: "clean",
      tags: ["Stratum", "ckpool"],
      when: "2026-06",
      stats: { runs: "~19.5k", crashes: 1000, critical: 0, note: "guards" },
    },
    {
      id: "SV2-REF",
      title: "stratum-mining reference",
      blurb: "Sv2 frame bounds · extranonce · user identity · open_channel.",
      href: "./reports/stratum-mining-fuzz/",
      domain: "crypto",
      kind: "clean",
      tags: ["Sv2", "stratum"],
      when: "2026-06",
      stats: { runs: "8k", crashes: 800, critical: 0, note: "guards" },
    },
    {
      id: "OSS-ROTATE",
      title: "OSS PR sweep · 5 targets",
      blurb: "Dogecoin · Litecoin · geth · Solana SPL · CentiJSON — ROTATE CLEAN.",
      href: "./reports/oss-pr-sweep/",
      domain: "crypto",
      kind: "clean",
      tags: ["rotation", "L1"],
      when: "2026",
      stats: { runs: 2560, crashes: 384, critical: 0, note: "guards" },
    },
    {
      id: "BTC5",
      title: "Bitcoin Core · 5-module study",
      blurb: "Companion guards · honest non-CVE scope · upstream excerpts.",
      href: "./reports/bitcoin-core-5module.html",
      domain: "crypto",
      kind: "archive",
      tags: ["Bitcoin", "archive"],
      when: "2026",
      stats: { runs: "—", crashes: "—", critical: 0, note: "study" },
    },
    {
      id: "L1-v3",
      title: "L1 Crypto Stack v3",
      blurb: "Bitcoin Core qa-assets · pinned upstream · 20k golden match.",
      href: "./reports/l1-crypto-stack-v3.html",
      domain: "crypto",
      kind: "archive",
      tags: ["L1", "archive"],
      when: "2026",
      stats: { runs: "—", crashes: "—", critical: 0, note: "corpus" },
    },
    {
      id: "L1-v2",
      title: "L1 Crypto Stack v2",
      blurb: "Five-chain property probes · no sandbox traps.",
      href: "./reports/l1-crypto-stack.html",
      domain: "crypto",
      kind: "archive",
      tags: ["L1", "archive"],
      when: "2025",
      stats: { runs: "—", crashes: "—", critical: 0, note: "archive" },
    },
  ];

  const KIND_LABEL = {
    found: "Found",
    clean: "CLEAN",
    series: "Series",
    live: "Live",
    pilot: "Pilot",
    archive: "Archive",
  };
  const KIND_RANK = { found: 0, live: 1, pilot: 2, clean: 3, series: 4, archive: 5 };

  const state = {
    domain: "all",
    kind: "all",
    q: "",
    sortKey: "when",
    sortDir: "desc",
  };

  function esc(v) {
    return String(v)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  function highlight(text) {
    const raw = String(text ?? "");
    const q = state.q.trim();
    if (!q) return esc(raw);
    const lower = raw.toLowerCase();
    const needle = q.toLowerCase();
    let out = "";
    let i = 0;
    while (i < raw.length) {
      const hit = lower.indexOf(needle, i);
      if (hit < 0) {
        out += esc(raw.slice(i));
        break;
      }
      out += esc(raw.slice(i, hit));
      out += `<mark class="rh-mark">${esc(raw.slice(hit, hit + needle.length))}</mark>`;
      i = hit + needle.length;
    }
    return out;
  }

  function matches(c) {
    if (state.domain !== "all" && c.domain !== state.domain) return false;
    if (state.kind !== "all" && c.kind !== state.kind) return false;
    const q = state.q.trim().toLowerCase();
    if (!q) return true;
    const hay = [
      c.id,
      c.title,
      c.blurb,
      c.when,
      c.domain,
      c.stats?.note,
      c.stats?.runs,
      ...(c.tags || []),
    ]
      .join(" ")
      .toLowerCase();
    return hay.includes(q);
  }

  function critNum(c) {
    const v = c.stats?.critical;
    return typeof v === "number" ? v : Number.NaN;
  }

  function sorted(list) {
    const out = list.slice();
    const dir = state.sortDir === "asc" ? 1 : -1;
    const key = state.sortKey;
    out.sort((a, b) => {
      let cmp = 0;
      if (key === "when") cmp = String(a.when).localeCompare(String(b.when));
      else if (key === "id") cmp = String(a.id).localeCompare(String(b.id));
      else if (key === "title") cmp = a.title.localeCompare(b.title);
      else if (key === "kind") cmp = (KIND_RANK[a.kind] ?? 9) - (KIND_RANK[b.kind] ?? 9);
      else if (key === "critical") {
        const ca = critNum(a);
        const cb = critNum(b);
        cmp = (Number.isNaN(ca) ? -1 : ca) - (Number.isNaN(cb) ? -1 : cb);
      } else if (key === "runs") {
        cmp = String(a.stats?.runs ?? "").localeCompare(String(b.stats?.runs ?? ""), undefined, { numeric: true });
      } else cmp = String(a.when).localeCompare(String(b.when));
      if (cmp === 0) cmp = String(a.id).localeCompare(String(b.id));
      return cmp * dir;
    });
    return out;
  }

  function kindClass(kind) {
    if (kind === "found") return "rh-pill rh-pill-found";
    if (kind === "clean") return "rh-pill rh-pill-clean";
    if (kind === "live") return "rh-pill rh-pill-live";
    if (kind === "series") return "rh-pill rh-pill-series";
    if (kind === "pilot") return "rh-pill rh-pill-pilot";
    return "rh-pill rh-pill-archive";
  }

  function domainMeta(id) {
    return DOMAINS.find((d) => d.id === id) || { id, label: id, blurb: "" };
  }

  function renderSpotlight(host) {
    if (!host) return;
    const feat = CASES.find((c) => c.featured) || CASES.find((c) => c.kind === "found");
    if (
      !feat ||
      (state.domain !== "all" && feat.domain !== state.domain) ||
      (state.kind !== "all" && state.kind !== "found" && feat.kind !== state.kind)
    ) {
      host.hidden = true;
      return;
    }
    const dom = domainMeta(feat.domain);
    host.hidden = false;
    host.innerHTML = `
      <a class="rh-spotlight" href="${esc(feat.href)}">
        <div class="rh-spot-glow" aria-hidden="true"></div>
        <div class="rh-spot-top">
          <span class="${kindClass(feat.kind)}">${esc(KIND_LABEL[feat.kind])} · ${esc(feat.id)}</span>
          <span class="rh-domain-chip rh-domain-${esc(feat.domain)}">${esc(dom.label)}</span>
          <span class="rh-spot-when notranslate">${esc(feat.when)}</span>
        </div>
        <h2>${highlight(feat.title)}</h2>
        <p>${highlight(feat.blurb)}</p>
        <div class="rh-spot-metrics notranslate">
          <span><b>${esc(feat.stats.runs)}</b> runs</span>
          <span><b>${esc(feat.stats.critical)}</b> critical</span>
          <span class="rh-note">${esc(feat.stats.note || "")}</span>
        </div>
        <span class="rh-spot-cta">Open case →</span>
      </a>`;
  }

  function renderCounts() {
    const domainCounts = { all: CASES.length };
    const kindCounts = { all: CASES.length };
    for (const c of CASES) {
      domainCounts[c.domain] = (domainCounts[c.domain] || 0) + 1;
      kindCounts[c.kind] = (kindCounts[c.kind] || 0) + 1;
    }
    document.querySelectorAll("[data-rh-domain-count]").forEach((n) => {
      n.textContent = String(domainCounts[n.getAttribute("data-rh-domain-count")] ?? 0);
    });
    document.querySelectorAll("[data-rh-count]").forEach((n) => {
      n.textContent = String(kindCounts[n.getAttribute("data-rh-count")] ?? 0);
    });
  }

  function theadHtml() {
    const ind = (key) => {
      if (state.sortKey !== key) return "";
      return state.sortDir === "asc" ? "↑" : "↓";
    };
    const sortedCls = (key) => (state.sortKey === key ? " is-sorted" : "");
    const aria = (key) =>
      state.sortKey === key ? (state.sortDir === "asc" ? "ascending" : "descending") : "none";
    return `
      <thead>
        <tr>
          <th scope="col" class="rh-th${sortedCls("kind")}" data-rh-sort="kind" tabindex="0" aria-sort="${aria("kind")}">Status <span class="rh-sort-ind">${ind("kind")}</span></th>
          <th scope="col" class="rh-th${sortedCls("id")}" data-rh-sort="id" tabindex="0" aria-sort="${aria("id")}">ID <span class="rh-sort-ind">${ind("id")}</span></th>
          <th scope="col" class="rh-th rh-th-case${sortedCls("title")}" data-rh-sort="title" tabindex="0" aria-sort="${aria("title")}">Case <span class="rh-sort-ind">${ind("title")}</span></th>
          <th scope="col" class="rh-th rh-th-num${sortedCls("runs")}" data-rh-sort="runs" tabindex="0" aria-sort="${aria("runs")}">Runs <span class="rh-sort-ind">${ind("runs")}</span></th>
          <th scope="col" class="rh-th rh-th-num${sortedCls("critical")}" data-rh-sort="critical" tabindex="0" aria-sort="${aria("critical")}">Crit <span class="rh-sort-ind">${ind("critical")}</span></th>
          <th scope="col" class="rh-th${sortedCls("when")}" data-rh-sort="when" tabindex="0" aria-sort="${aria("when")}">Date <span class="rh-sort-ind">${ind("when")}</span></th>
          <th scope="col" class="rh-th rh-th-go"><span class="sr-only">Open</span></th>
        </tr>
      </thead>`;
  }

  function rowHtml(c, i) {
    const crit = c.stats?.critical;
    const critCls = typeof crit === "number" && crit > 0 ? "rh-crit-hot" : "rh-crit-ok";
    return `
      <tr class="rh-tr rh-kind-${esc(c.kind)}" style="--rh-i:${i}" tabindex="0" role="link" data-href="${esc(c.href)}">
        <td class="rh-td-status"><span class="${kindClass(c.kind)}">${esc(KIND_LABEL[c.kind] || c.kind)}</span></td>
        <td class="rh-td-id notranslate"><code>${highlight(c.id)}</code></td>
        <td class="rh-td-case">
          <div class="rh-case-title">${highlight(c.title)}</div>
          <div class="rh-case-blurb">${highlight(c.blurb)}</div>
          <div class="rh-tags">${(c.tags || []).map((t) => `<span>${highlight(t)}</span>`).join("")}</div>
        </td>
        <td class="rh-td-num notranslate">${esc(c.stats?.runs ?? "—")}</td>
        <td class="rh-td-num notranslate ${critCls}">${esc(crit ?? "—")}</td>
        <td class="rh-td-when notranslate">${highlight(c.when)}</td>
        <td class="rh-td-go" aria-hidden="true">→</td>
      </tr>`;
  }

  function bindRows(root) {
    root.querySelectorAll("tr.rh-tr[data-href]").forEach((tr) => {
      const go = () => {
        window.location.href = tr.getAttribute("data-href");
      };
      tr.addEventListener("click", go);
      tr.addEventListener("keydown", (e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          go();
        }
      });
    });
    root.querySelectorAll("[data-rh-sort]").forEach((th) => {
      const run = () => setSort(th.getAttribute("data-rh-sort") || "when");
      th.addEventListener("click", run);
      th.addEventListener("keydown", (e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          run();
        }
      });
    });
  }

  function renderDomainSection(domain, cases, startIndex) {
    const meta = domainMeta(domain);
    const found = cases.filter((c) => c.kind === "found").length;
    const clean = cases.filter((c) => c.kind === "clean").length;
    const rows = cases.map((c, i) => rowHtml(c, startIndex + i)).join("");
    return `
      <section class="rh-domain" data-domain="${esc(domain)}" id="domain-${esc(domain)}">
        <header class="rh-domain-head">
          <div class="rh-domain-title">
            <span class="rh-domain-chip rh-domain-${esc(domain)}">${esc(meta.label)}</span>
            <h2>${esc(meta.label)}</h2>
            <p>${esc(meta.blurb)}</p>
          </div>
          <div class="rh-domain-stats notranslate">
            <span><b>${cases.length}</b> cases</span>
            ${found ? `<span class="rh-domain-found"><b>${found}</b> found</span>` : ""}
            ${clean ? `<span><b>${clean}</b> CLEAN</span>` : ""}
          </div>
        </header>
        <div class="rh-table-shell">
          <div class="rh-table-scroll">
            <table class="rh-table" aria-label="${esc(meta.label)} cases">
              ${theadHtml()}
              <tbody>${rows}</tbody>
            </table>
          </div>
        </div>
      </section>`;
  }

  function render() {
    const host = document.getElementById("rh-domains");
    const empty = document.getElementById("rh-empty");
    const countEl = document.getElementById("rh-visible-count");
    const clearBtn = document.getElementById("rh-clear");
    if (!host) return;

    const filtered = sorted(CASES.filter(matches));
    const order =
      state.domain === "all" ? DOMAINS.map((d) => d.id) : [state.domain];

    let html = "";
    let idx = 0;
    let sections = 0;
    for (const domain of order) {
      const group = filtered.filter((c) => c.domain === domain);
      if (!group.length) continue;
      html += renderDomainSection(domain, group, idx);
      idx += group.length;
      sections += 1;
    }
    host.innerHTML = html;
    bindRows(host);

    if (empty) empty.hidden = filtered.length > 0;
    if (countEl) countEl.textContent = String(filtered.length);
    if (clearBtn) clearBtn.hidden = !state.q.trim();
    renderSpotlight(document.getElementById("rh-spotlight"));
    renderCounts();

    document.querySelectorAll("[data-rh-domain]").forEach((b) => {
      const on = b.getAttribute("data-rh-domain") === state.domain;
      b.classList.toggle("is-active", on);
      b.setAttribute("aria-pressed", on ? "true" : "false");
    });
    document.querySelectorAll("[data-rh-filter]").forEach((b) => {
      const on = b.getAttribute("data-rh-filter") === state.kind;
      b.classList.toggle("is-active", on);
      b.setAttribute("aria-pressed", on ? "true" : "false");
    });
  }

  function setSort(key) {
    if (state.sortKey === key) state.sortDir = state.sortDir === "asc" ? "desc" : "asc";
    else {
      state.sortKey = key;
      state.sortDir = key === "title" || key === "id" ? "asc" : "desc";
    }
    render();
  }

  function bind() {
    const domains = document.getElementById("rh-domains-nav");
    const kinds = document.getElementById("rh-filters");
    const search = document.getElementById("rh-search");
    const clearBtn = document.getElementById("rh-clear");

    if (domains) {
      domains.addEventListener("click", (e) => {
        const btn = e.target.closest("[data-rh-domain]");
        if (!btn) return;
        state.domain = btn.getAttribute("data-rh-domain") || "all";
        render();
      });
    }
    if (kinds) {
      kinds.addEventListener("click", (e) => {
        const btn = e.target.closest("[data-rh-filter]");
        if (!btn) return;
        state.kind = btn.getAttribute("data-rh-filter") || "all";
        render();
      });
    }
    if (search) {
      search.addEventListener("input", () => {
        state.q = search.value || "";
        render();
      });
    }
    if (clearBtn && search) {
      clearBtn.addEventListener("click", () => {
        search.value = "";
        state.q = "";
        search.focus();
        render();
      });
    }
    document.addEventListener("keydown", (e) => {
      if (e.key === "/" && search && document.activeElement !== search && !e.metaKey && !e.ctrlKey && !e.altKey) {
        const tag = (document.activeElement && document.activeElement.tagName) || "";
        if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
        e.preventDefault();
        search.focus();
        search.select();
      }
      if (e.key === "Escape" && search && document.activeElement === search && search.value) {
        search.value = "";
        state.q = "";
        render();
      }
    });
    render();
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", bind);
  else bind();

  window.HackMeResearchLedger = { cases: CASES, domains: DOMAINS, render };
})();
