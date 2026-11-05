(() => {
  const ROOT = document.getElementById("security-hall");
  if (!ROOT) return;

  const LIST = document.getElementById("security-hall-list");
  const EMPTY = document.getElementById("security-hall-empty");
  const STAT_PAID = document.getElementById("hall-stat-paid");
  const STAT_PEOPLE = document.getElementById("hall-stat-people");
  const STAT_REPORTS = document.getElementById("hall-stat-reports");
  const UPDATED = document.getElementById("hall-updated");

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
        const title = esc(f.title || "Finding");
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

  function render(data) {
    const researchers = Array.isArray(data.researchers) ? [...data.researchers] : [];
    researchers.sort((a, b) => researcherPaid(b) - researcherPaid(a));

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
        : researchers.reduce((a, r) => a + (r.findings ? r.findings.length : r.reports || 0), 0);

    if (STAT_PAID) STAT_PAID.textContent = fmtHmc(paid);
    if (STAT_PEOPLE) STAT_PEOPLE.textContent = String(people);
    if (STAT_REPORTS) STAT_REPORTS.textContent = String(reports);
    if (UPDATED && data.updated) UPDATED.textContent = data.updated;

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
        EMPTY.querySelector(".hof-empty-title").textContent = "Board temporarily unavailable";
        EMPTY.querySelector(".hof-empty-copy").textContent =
          "Retry later — the reward program itself is still open.";
      }
      if (LIST) LIST.hidden = true;
    });
})();
