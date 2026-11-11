/**
 * HackMe Research hub — domain tables + search.
 * Case data lives in ./research-cases.json (domain shelves + outcome kinds).
 */
(() => {
  let DOMAINS = [];
  let CASES = [];

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

  function sortKeyWhen(c) {
    if (c.whenSort) return String(c.whenSort);
    const w = String(c.when || "");
    if (/^\d{4}-\d{2}-\d{2}$/.test(w)) return w;
    if (/^\d{4}-\d{2}$/.test(w)) return `${w}-28`;
    if (/^\d{4}$/.test(w)) return `${w}-12-31`;
    const m = w.match(/^(\d{4})/);
    return m ? `${m[1]}-06-30` : "1970-01-01";
  }

  function relativeWhen(c) {
    const iso = sortKeyWhen(c);
    const d = new Date(`${iso}T12:00:00Z`);
    if (Number.isNaN(d.getTime())) return esc(c.when);
    const now = new Date();
    const today = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate());
    const then = Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate());
    const days = Math.round((today - then) / 86400000);
    let rel = "";
    if (days === 0) rel = "today";
    else if (days === 1) rel = "yesterday";
    else if (days > 1 && days < 14) rel = `${days}d ago`;
    else if (days >= 14 && days < 60) rel = `${Math.round(days / 7)}w ago`;
    const label = esc(c.when);
    if (!rel) return `<span title="${esc(iso)}">${label}</span>`;
    return `<span title="${esc(iso)}">${label}</span><span class="rh-rel">${esc(rel)}</span>`;
  }

  function sorted(list) {
    const out = list.slice();
    const dir = state.sortDir === "asc" ? 1 : -1;
    const key = state.sortKey;
    out.sort((a, b) => {
      let cmp = 0;
      if (key === "when") cmp = sortKeyWhen(a).localeCompare(sortKeyWhen(b));
      else if (key === "id") cmp = String(a.id).localeCompare(String(b.id));
      else if (key === "title") cmp = a.title.localeCompare(b.title);
      else if (key === "kind") cmp = (KIND_RANK[a.kind] ?? 9) - (KIND_RANK[b.kind] ?? 9);
      else if (key === "critical") {
        const ca = critNum(a);
        const cb = critNum(b);
        cmp = (Number.isNaN(ca) ? -1 : ca) - (Number.isNaN(cb) ? -1 : cb);
      } else if (key === "runs") {
        cmp = String(a.stats?.runs ?? "").localeCompare(String(b.stats?.runs ?? ""), undefined, { numeric: true });
      } else cmp = sortKeyWhen(a).localeCompare(sortKeyWhen(b));
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

  function tapeItemHtml(c) {
    return `
      <a class="rh-activity-item rh-kind-${esc(c.kind)}" href="${esc(c.href)}">
        <span class="${kindClass(c.kind)}${c.pulse ? " rh-pill-pulse" : ""}">${esc(KIND_LABEL[c.kind])}</span>
        <span class="rh-activity-id notranslate">${esc(c.id)}</span>
        <span class="rh-activity-title">${esc(c.title)}</span>
        <span class="rh-activity-when notranslate">${esc(c.when)}</span>
      </a>`;
  }

  function renderActivity(host) {
    if (!host) return;
    const tapeRank = { found: 0, live: 1, pilot: 2, clean: 3 };
    const recent = CASES.slice()
      .filter((c) => c.kind === "found" || c.kind === "live" || c.kind === "clean" || c.kind === "pilot")
      .sort((a, b) => {
        const kr = (tapeRank[a.kind] ?? 9) - (tapeRank[b.kind] ?? 9);
        if (kr !== 0) return kr;
        return sortKeyWhen(b).localeCompare(sortKeyWhen(a));
      })
      .slice(0, 8);
    if (!recent.length) {
      host.hidden = true;
      return;
    }
    host.hidden = false;
    const items = recent.map(tapeItemHtml).join("");
    // Duplicate track for seamless CSS marquee; pause on hover via CSS.
    host.innerHTML = `
      <div class="rh-activity" aria-label="Recent ledger motion">
        <span class="rh-activity-label">Live tape</span>
        <div class="rh-activity-viewport">
          <div class="rh-activity-marquee">
            <div class="rh-activity-track">${items}</div>
            <div class="rh-activity-track" aria-hidden="true">${items}</div>
          </div>
        </div>
      </div>`;
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
    const reportBtn = feat.report
      ? `<button type="button" class="btn btn-secondary rh-spot-report" data-rh-report="${esc(feat.report)}" data-rh-report-title="${esc(feat.title)}">Show report</button>`
      : "";
    host.innerHTML = `
      <div class="rh-spotlight">
        <div class="rh-spot-glow" aria-hidden="true"></div>
        <div class="rh-spot-top">
          <span class="${kindClass(feat.kind)}${feat.pulse ? " rh-pill-pulse" : ""}">${esc(KIND_LABEL[feat.kind])} · ${esc(feat.id)}</span>
          <span class="rh-domain-chip rh-domain-${esc(feat.domain)}">${esc(dom.label)}</span>
          <span class="rh-spot-when notranslate">${relativeWhen(feat)}</span>
        </div>
        <h2>${highlight(feat.title)}</h2>
        <p>${highlight(feat.blurb)}</p>
        <div class="rh-spot-metrics notranslate">
          <span><b>${esc(feat.stats.runs)}</b> runs</span>
          <span><b>${esc(feat.stats.critical)}</b> critical</span>
          <span class="rh-note">${esc(feat.stats.note || "")}</span>
        </div>
        <div class="rh-spot-cta-row">
          ${reportBtn}
          <a class="rh-spot-cta" href="${esc(feat.href)}">Open case →</a>
        </div>
      </div>`;
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
          <th scope="col" class="rh-th rh-th-report">Report</th>
          <th scope="col" class="rh-th rh-th-go"><span class="sr-only">Open</span></th>
        </tr>
      </thead>`;
  }

  function rowHtml(c, i) {
    const crit = c.stats?.critical;
    const critCls = typeof crit === "number" && crit > 0 ? "rh-crit-hot" : "rh-crit-ok";
    const reportCell = c.report
      ? `<button type="button" class="rh-report-btn" data-rh-report="${esc(c.report)}" data-rh-report-title="${esc(c.title)}" title="Show fuzz_report_v2 deliverable">Report</button>`
      : `<span class="rh-report-none" title="No Orders deliverable snapshot">—</span>`;
    return `
      <tr class="rh-tr rh-kind-${esc(c.kind)}${c.pulse ? " rh-tr-pulse" : ""}" style="--rh-i:${i}" tabindex="0" role="link" data-href="${esc(c.href)}">
        <td class="rh-td-status"><span class="${kindClass(c.kind)}${c.pulse ? " rh-pill-pulse" : ""}">${esc(KIND_LABEL[c.kind] || c.kind)}</span></td>
        <td class="rh-td-id notranslate"><code>${highlight(c.id)}</code></td>
        <td class="rh-td-case">
          <div class="rh-case-title">${highlight(c.title)}</div>
          <div class="rh-case-blurb">${highlight(c.blurb)}</div>
          <div class="rh-tags">${(c.tags || []).map((t) => `<span>${highlight(t)}</span>`).join("")}</div>
        </td>
        <td class="rh-td-num notranslate">${esc(c.stats?.runs ?? "—")}</td>
        <td class="rh-td-num notranslate ${critCls}">${esc(crit ?? "—")}</td>
        <td class="rh-td-when notranslate">${relativeWhen(c)}</td>
        <td class="rh-td-report">${reportCell}</td>
        <td class="rh-td-go" aria-hidden="true">→</td>
      </tr>`;
  }

  function bindRows(root) {
    root.querySelectorAll("tr.rh-tr[data-href]").forEach((tr) => {
      const go = () => {
        window.location.href = tr.getAttribute("data-href");
      };
      tr.addEventListener("click", (e) => {
        if (e.target.closest("[data-rh-report]")) return;
        go();
      });
      tr.addEventListener("keydown", (e) => {
        if (e.key === "Enter" || e.key === " ") {
          if (e.target.closest("[data-rh-report]")) return;
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
    renderActivity(document.getElementById("rh-activity"));
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

  async function boot() {
    const host = document.getElementById("rh-domains");
    try {
      const url = new URL("./assets/research-cases.json", window.location.href);
      url.searchParams.set("v", "20261111tape1");
      const res = await fetch(url.href, { credentials: "same-origin" });
      if (!res.ok) throw new Error("HTTP " + res.status);
      const data = await res.json();
      if (!Array.isArray(data.cases) || !Array.isArray(data.domains)) {
        throw new Error("invalid research-cases.json shape");
      }
      CASES = data.cases;
      DOMAINS = data.domains;
      bind();
    } catch (err) {
      console.error("[research-ledger]", err);
      if (host) {
        host.innerHTML = '<p class="rh-empty">Could not load case ledger. Refresh or check assets/research-cases.json.</p>';
      }
    }
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", boot);
  else boot();

  window.HackMeResearchLedger = {
    get cases() { return CASES; },
    get domains() { return DOMAINS; },
    render,
  };
})();
