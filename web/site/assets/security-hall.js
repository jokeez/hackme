(() => {
  const ROOT = document.getElementById("security-hall");
  if (!ROOT) return;

  const LIST = document.getElementById("security-hall-list");
  const EMPTY = document.getElementById("security-hall-empty");
  const STAT_PAID = document.getElementById("hall-stat-paid");
  const STAT_PEOPLE = document.getElementById("hall-stat-people");
  const STAT_REPORTS = document.getElementById("hall-stat-reports");
  const STAT_BUDGET = document.getElementById("hall-stat-budget");
  const UPDATED = document.getElementById("hall-updated");
  const BUDGET = document.getElementById("hof-budget");

  const SEV_LABEL = {
    informational: "Info",
    low: "Low",
    "low-ui": "Low · UI",
    "low-functional": "Low · Func",
    medium: "Medium",
    high: "High",
    critical: "Critical",
  };

  const MONTH_NAMES = [
    "Jan", "Feb", "Mar", "Apr", "May", "Jun",
    "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
  ];

  function esc(s) {
    return String(s ?? "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  function initials(name) {
    const parts = String(name || "?").trim().split(/\s+/).filter(Boolean);
    if (!parts.length) return "?";
    if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
    return (parts[0][0] + parts[1][0]).toUpperCase();
  }

  function fmtHmc(n) {
    const x = Number(n);
    if (!Number.isFinite(x)) return "—";
    return x % 1 === 0 ? String(x) : x.toFixed(2).replace(/\.?0+$/, "");
  }

  function fmtPct(n) {
    const x = Number(n);
    if (!Number.isFinite(x)) return "—";
    return x >= 10 ? String(Math.round(x)) : x.toFixed(1).replace(/\.0$/, "");
  }

  function researcherPaid(r) {
    if (typeof r.total_paid_hmc === "number") return r.total_paid_hmc;
    return (r.findings || []).reduce((a, f) => a + (Number(f.paid_hmc) || 0), 0);
  }

  function allFindings(researchers) {
    const out = [];
    for (const r of researchers) {
      for (const f of r.findings || []) {
        out.push({ ...f, _researcher: r.name || r.handle || r.github || "Anonymous" });
      }
    }
    return out;
  }

  function periodSpent(findings, period) {
    if (!period) {
      return findings.reduce((a, f) => a + (Number(f.paid_hmc) || 0), 0);
    }
    return findings
      .filter((f) => String(f.date || "").startsWith(period))
      .reduce((a, f) => a + (Number(f.paid_hmc) || 0), 0);
  }

  function monthKey(dateStr) {
    const m = String(dateStr || "").match(/^(\d{4})-(\d{2})/);
    return m ? `${m[1]}-${m[2]}` : null;
  }

  function monthLabel(key) {
    const m = String(key || "").match(/^(\d{4})-(\d{2})$/);
    if (!m) return key || "—";
    const idx = Number(m[2]) - 1;
    const name = MONTH_NAMES[idx] || m[2];
    return `${name} ${m[1]}`;
  }

  function monthlySeries(findings) {
    const map = new Map();
    for (const f of findings) {
      const key = monthKey(f.date);
      if (!key) continue;
      map.set(key, (map.get(key) || 0) + (Number(f.paid_hmc) || 0));
    }
    return [...map.entries()]
      .sort((a, b) => a[0].localeCompare(b[0]))
      .map(([key, paid]) => ({ key, paid, label: monthLabel(key) }));
  }

  function identityHtml(r) {
    const name = esc(r.name || r.handle || "Anonymous");
    const gh = (r.github || "").replace(/^@/, "").trim();
    if (gh) {
      const url = `https://github.com/${encodeURIComponent(gh)}`;
      return `<a class="hof-identity" href="${esc(url)}" target="_blank" rel="noreferrer">
        <span class="hof-name">${name}</span>
        <span class="hof-handle">@${esc(gh)}</span>
      </a>`;
    }
    const handle = r.handle ? esc(r.handle) : "";
    return `<div class="hof-identity">
      <span class="hof-name">${name}</span>
      ${handle ? `<span class="hof-handle">${handle}</span>` : `<span class="hof-handle hof-handle--soft">nick credit</span>`}
    </div>`;
  }

  function findingsHtml(findings) {
    if (!findings || !findings.length) return "";
    return `<ul class="hof-findings">${findings
      .map((f) => {
        const sev = String(f.severity || "low").toLowerCase();
        const sevClass = sev.replace(/[^a-z0-9-]/g, "");
        const label = SEV_LABEL[sev] || sev;
        const paid = fmtHmc(f.paid_hmc);
        let title = esc(f.title || "Finding");
        if (f.pr) {
          const url = `https://github.com/jokeez/hackme/pull/${encodeURIComponent(f.pr)}`;
          title = `<a class="hof-finding-link" href="${esc(url)}" target="_blank" rel="noreferrer">${title}</a>`;
        }
        const date = f.date ? `<time datetime="${esc(f.date)}">${esc(f.date)}</time>` : "";
        return `<li class="hof-finding">
          <span class="hof-sev hof-sev--${esc(sevClass)}">${esc(label)}</span>
          <span class="hof-finding-title">${title}</span>
          <span class="hof-finding-pay"><strong>${esc(paid)}</strong> HMC</span>
          ${date}
        </li>`;
      })
      .join("")}</ul>`;
  }

  function rankClass(i) {
    if (i === 0) return "hof-row--gold";
    if (i === 1) return "hof-row--silver";
    if (i === 2) return "hof-row--bronze";
    return "";
  }

  function toneFor(pct) {
    if (pct >= 100) return "over";
    if (pct >= 75) return "warn";
    return "ok";
  }

  function renderInsights(data, researchers, findings, lifetime, periodSpend) {
    if (!BUDGET) return;
    const prog = data.program || {};
    const bud = data.budget || {};
    const limit = Number(bud.limit_hmc ?? prog.monthly_budget_hmc ?? 200) || 200;
    const monthSpent =
      typeof bud.spent_hmc === "number" ? bud.spent_hmc : periodSpend;
    const remaining = Math.max(0, limit - monthSpent);
    const monthPct = limit > 0 ? (monthSpent / limit) * 100 : 0;
    const monthFill = Math.min(100, monthPct);
    const monthLabelText = bud.period_label || bud.period || "This month";
    const months = monthlySeries(findings);
    const maxMonth = Math.max(limit, ...months.map((m) => m.paid), 1);
    const ranked = [...researchers].sort((a, b) => researcherPaid(b) - researcherPaid(a));
    const shareMax = Math.max(...ranked.map(researcherPaid), 1);

    const sharesHtml = ranked.length
      ? ranked
          .map((r, i) => {
            const paid = researcherPaid(r);
            const pct = lifetime > 0 ? (paid / lifetime) * 100 : 0;
            const bar = (paid / shareMax) * 100;
            const name = esc(r.name || r.handle || r.github || "Anonymous");
            const gh = (r.github || "").replace(/^@/, "").trim();
            const label = gh
              ? `<a href="${esc(`https://github.com/${encodeURIComponent(gh)}`)}" target="_blank" rel="noreferrer">${name}</a>`
              : name;
            return `<div class="hof-share-row" style="--hof-i:${i}">
              <div class="hof-share-label">${label}</div>
              <div class="hof-share-track"><div class="hof-share-fill" style="width:${bar}%"></div></div>
              <div class="hof-share-val"><strong>${esc(fmtHmc(paid))}</strong> <span>${esc(fmtPct(pct))}%</span></div>
            </div>`;
          })
          .join("")
      : `<p class="hof-insight-empty">No lifetime payouts yet.</p>`;

    const monthsHtml = months.length
      ? `<div class="hof-months" role="img" aria-label="Payouts by month">
          ${months
            .map((m) => {
              const h = Math.max(8, (m.paid / maxMonth) * 100);
              const over = m.paid > limit;
              return `<div class="hof-month ${over ? "hof-month--over" : ""}" title="${esc(m.label)}: ${esc(fmtHmc(m.paid))} HMC">
                <div class="hof-month-bar-wrap">
                  <div class="hof-month-cap" style="bottom:${(limit / maxMonth) * 100}%"></div>
                  <div class="hof-month-bar" style="height:${h}%"></div>
                </div>
                <span class="hof-month-amt">${esc(fmtHmc(m.paid))}</span>
                <span class="hof-month-label">${esc(m.label.replace(/ (\d{4})$/, ""))}</span>
              </div>`;
            })
            .join("")}
        </div>
        <p class="hof-months-legend">Bars = HMC paid that month · faint line = monthly envelope (${esc(fmtHmc(limit))})</p>`
      : `<p class="hof-insight-empty">No dated payouts yet.</p>`;

    BUDGET.hidden = false;
    BUDGET.className = "hof-budget hof-insight";
    BUDGET.innerHTML = `
      <div class="hof-insight-grid">
        <article class="hof-insight-card hof-insight-card--month" data-tone="${toneFor(monthPct)}">
          <div class="hof-budget-top">
            <div>
              <p class="hof-budget-kicker">This month</p>
              <h3 class="hof-budget-title">${esc(monthLabelText)}</h3>
            </div>
            <div class="hof-budget-nums">
              <span><strong>${esc(fmtHmc(monthSpent))}</strong> spent</span>
              <span class="hof-budget-sep">/</span>
              <span><strong>${esc(fmtHmc(limit))}</strong></span>
            </div>
          </div>
          <div class="hof-budget-track" role="progressbar" aria-valuemin="0" aria-valuemax="${esc(limit)}" aria-valuenow="${esc(monthSpent)}" aria-label="Monthly security budget used">
            <div class="hof-budget-fill" style="width:${monthFill}%"></div>
          </div>
          <div class="hof-budget-meta">
            <span>${esc(fmtPct(monthPct))}% of envelope · <strong>${esc(fmtHmc(remaining))}</strong> left</span>
            <span>${months.find((m) => m.key === (bud.period || "")) ? "current period" : "by finding dates"}</span>
          </div>
        </article>

        <article class="hof-insight-card hof-insight-card--all">
          <div class="hof-budget-top">
            <div>
              <p class="hof-budget-kicker">All time</p>
              <h3 class="hof-budget-title">Who earned what</h3>
            </div>
            <div class="hof-budget-nums">
              <span>Lifetime <strong>${esc(fmtHmc(lifetime))}</strong> HMC</span>
            </div>
          </div>
          <div class="hof-shares">${sharesHtml}</div>
        </article>
      </div>

      <article class="hof-insight-card hof-insight-card--history">
        <div class="hof-budget-top">
          <div>
            <p class="hof-budget-kicker">History</p>
            <h3 class="hof-budget-title">Paid by month</h3>
          </div>
          <div class="hof-budget-nums">
            <span>${esc(String(months.length))} month${months.length === 1 ? "" : "s"} · avg <strong>${esc(fmtHmc(months.length ? lifetime / months.length : 0))}</strong></span>
          </div>
        </div>
        ${monthsHtml}
      </article>
    `;
    if (STAT_BUDGET) STAT_BUDGET.textContent = `${fmtHmc(monthSpent)}/${fmtHmc(limit)}`;
  }

  function render(data) {
    const researchers = Array.isArray(data.researchers) ? [...data.researchers] : [];
    researchers.sort((a, b) => researcherPaid(b) - researcherPaid(a));

    const findings = allFindings(researchers);
    const paid =
      data.stats && typeof data.stats.total_paid_hmc === "number"
        ? data.stats.total_paid_hmc
        : researchers.reduce((a, r) => a + researcherPaid(r), 0);
    const people =
      data.stats && typeof data.stats.researchers === "number"
        ? data.stats.researchers
        : researchers.length;
    const reports =
      data.stats && typeof data.stats.reports_accepted === "number"
        ? data.stats.reports_accepted
        : findings.length;
    const period = (data.budget && data.budget.period) || "";
    const periodSpend = periodSpent(findings, period);

    if (STAT_PAID) STAT_PAID.textContent = fmtHmc(paid);
    if (STAT_PEOPLE) STAT_PEOPLE.textContent = String(people);
    if (STAT_REPORTS) STAT_REPORTS.textContent = String(reports);
    if (UPDATED && data.updated) {
      UPDATED.textContent = data.updated;
      UPDATED.setAttribute("datetime", data.updated);
    }

    renderInsights(data, researchers, findings, paid, periodSpend);

    if (!researchers.length) {
      if (LIST) LIST.hidden = true;
      if (EMPTY) EMPTY.hidden = false;
      ROOT.dataset.state = "empty";
      return;
    }

    if (EMPTY) EMPTY.hidden = true;
    if (LIST) {
      LIST.hidden = false;
      LIST.innerHTML = researchers
        .map((r, i) => {
          const total = fmtHmc(researcherPaid(r));
          const n = r.findings ? r.findings.length : r.reports || 0;
          return `<article class="hof-row ${rankClass(i)}" style="--hof-i:${i}">
            <div class="hof-rank" aria-label="Rank ${i + 1}">#${i + 1}</div>
            <div class="hof-avatar" aria-hidden="true">${esc(initials(r.name || r.handle || r.github))}</div>
            <div class="hof-main">
              ${identityHtml(r)}
              ${findingsHtml(r.findings)}
            </div>
            <div class="hof-payout">
              <span class="hof-payout-label">Paid</span>
              <span class="hof-payout-value"><strong>${esc(total)}</strong> HMC</span>
              <span class="hof-payout-meta">${n} report${n === 1 ? "" : "s"}</span>
            </div>
          </article>`;
        })
        .join("");
    }
    ROOT.dataset.state = "ready";
  }

  fetch("./assets/security-hall.json", { cache: "no-store" })
    .then((r) => {
      if (!r.ok) throw new Error("hall fetch " + r.status);
      return r.json();
    })
    .then(render)
    .catch(() => {
      ROOT.dataset.state = "error";
      if (EMPTY) {
        EMPTY.hidden = false;
        const t = EMPTY.querySelector(".hof-empty-title");
        const c = EMPTY.querySelector(".hof-empty-copy");
        if (t) t.textContent = "Board temporarily unavailable";
        if (c)
          c.textContent = "Retry later — the reward program itself is still open.";
      }
      if (LIST) LIST.hidden = true;
      if (BUDGET) BUDGET.hidden = true;
    });
})();
