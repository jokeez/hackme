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

  function researcherPaid(r) {
    if (typeof r.total_paid_hmc === "number") return r.total_paid_hmc;
    return (r.findings || []).reduce((a, f) => a + (Number(f.paid_hmc) || 0), 0);
  }

  function allFindings(researchers) {
    const out = [];
    for (const r of researchers) {
      for (const f of r.findings || []) out.push(f);
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

  function renderBudget(data, lifetime, periodSpend) {
    if (!BUDGET) return;
    const prog = data.program || {};
    const bud = data.budget || {};
    const limit = Number(bud.limit_hmc ?? prog.monthly_budget_hmc ?? 200) || 200;
    const spent =
      typeof bud.spent_hmc === "number" ? bud.spent_hmc : periodSpend;
    const remaining = Math.max(0, limit - spent);
    const pctRaw = limit > 0 ? (spent / limit) * 100 : 0;
    const pctFill = Math.min(100, pctRaw);
    const over = spent > limit;
    const label = bud.period_label || bud.period || "This month";
    let tone = "ok";
    if (pctRaw >= 100) tone = "over";
    else if (pctRaw >= 75) tone = "warn";

    BUDGET.hidden = false;
    BUDGET.dataset.tone = tone;
    BUDGET.innerHTML = `
      <div class="hof-budget-top">
        <div>
          <p class="hof-budget-kicker">Program budget</p>
          <h3 class="hof-budget-title">${esc(label)}</h3>
        </div>
        <div class="hof-budget-nums" aria-label="Budget numbers">
          <span><strong>${esc(fmtHmc(spent))}</strong> spent</span>
          <span class="hof-budget-sep">/</span>
          <span><strong>${esc(fmtHmc(limit))}</strong> HMC</span>
        </div>
      </div>
      <div class="hof-budget-track" role="progressbar" aria-valuemin="0" aria-valuemax="${esc(limit)}" aria-valuenow="${esc(spent)}" aria-label="Monthly security budget used">
        <div class="hof-budget-fill" style="width:${pctFill}%"></div>
        ${over ? `<div class="hof-budget-overmark" style="left:100%"></div>` : ""}
      </div>
      <div class="hof-budget-meta">
        <span>${esc(fmtHmc(pctRaw))}% used · <strong>${esc(fmtHmc(remaining))}</strong> HMC left this period</span>
        <span>Lifetime paid <strong>${esc(fmtHmc(lifetime))}</strong> HMC</span>
      </div>
      ${over ? `<p class="hof-budget-note">Period cap exceeded — exceptional accepted reports; operator may raise the monthly envelope.</p>` : ""}
    `;
    if (STAT_BUDGET) STAT_BUDGET.textContent = `${fmtHmc(spent)}/${fmtHmc(limit)}`;
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

    renderBudget(data, paid, periodSpend);

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
