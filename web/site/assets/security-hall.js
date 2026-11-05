(() => {
  const ROOT = document.getElementById("security-hall");
  if (!ROOT) return;

  const MONTH_LIST = document.getElementById("security-hall-month");
  const EMPTY = document.getElementById("security-hall-empty");
  const STAT_PAID = document.getElementById("hall-stat-paid");
  const STAT_PEOPLE = document.getElementById("hall-stat-people");
  const STAT_REPORTS = document.getElementById("hall-stat-reports");
  const STAT_BUDGET = document.getElementById("hall-stat-budget");
  const UPDATED = document.getElementById("hall-updated");
  const BUDGET = document.getElementById("hof-budget");
  const MONTH_SUB = document.getElementById("hof-month-sub");
  const OPEN_BTN = document.getElementById("hof-open-ledger");
  const DIALOG = document.getElementById("hof-ledger");
  const CLOSE_BTN = document.getElementById("hof-ledger-close");
  const LEDGER_BODY = document.getElementById("hof-ledger-body");
  const DETAIL = document.getElementById("hof-detail");

  const SEV_LABEL = {
    informational: "Info",
    low: "Low",
    "low-ui": "Low · UI",
    "low-functional": "Low · Func",
    medium: "Medium",
    high: "High",
    critical: "Critical",
  };

  /** @type {{ researchers: any[], period: string, lifetime: number } | null} */
  let STATE = null;

  function esc(s) {
    return String(s ?? "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
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

  function findingsInPeriod(r, period) {
    const list = r.findings || [];
    if (!period) return list;
    return list.filter((f) => String(f.date || "").startsWith(period));
  }

  function periodPaid(r, period) {
    return findingsInPeriod(r, period).reduce((a, f) => a + (Number(f.paid_hmc) || 0), 0);
  }

  function displayName(r) {
    return r.name || r.handle || r.github || "Anonymous";
  }

  function identityBits(r) {
    const name = esc(displayName(r));
    const gh = (r.github || "").replace(/^@/, "").trim();
    if (gh) {
      return {
        name,
        handleHtml: `<span class="hof-handle">@${esc(gh)}</span>`,
        github: gh,
      };
    }
    const handle = r.handle ? esc(r.handle) : "";
    return {
      name,
      handleHtml: handle
        ? `<span class="hof-handle">${handle}</span>`
        : `<span class="hof-handle hof-handle--soft">nick credit</span>`,
      github: "",
    };
  }

  /** Self-hosted avatar (assets/hof/) only — the site CSP blocks third-party images, and a
   *  hotlinked github.com avatar would also leak visitors' IPs. No avatar_url → name only. */
  function avatarHtml(r, sizeClass) {
    const custom = (r.avatar_url || "").trim();
    const src = /^\.\/assets\/hof\/[A-Za-z0-9._-]+\.(png|jpe?g|webp)$/.test(custom) ? custom : "";
    if (!src) return "";
    const cls = sizeClass ? `hof-avatar ${sizeClass}` : "hof-avatar";
    return `<img class="${cls}" src="${esc(src)}" alt="" width="44" height="44" loading="lazy" decoding="async" referrerpolicy="no-referrer" onerror="this.remove()" />`;
  }

  function fixLinkHtml(f) {
    const bits = [];
    if (f.pr) {
      bits.push(
        `<a href="${esc(`https://github.com/jokeez/hackme/pull/${encodeURIComponent(f.pr)}`)}" target="_blank" rel="noreferrer">PR #${esc(f.pr)}</a>`
      );
    }
    if (f.commit) {
      const short = String(f.commit).slice(0, 8);
      bits.push(
        `<a href="${esc(`https://github.com/jokeez/hackme/commit/${encodeURIComponent(f.commit)}`)}" target="_blank" rel="noreferrer"><code>${esc(short)}</code></a>`
      );
    }
    return bits.length ? bits.join(" · ") : `<span class="hof-muted">fix pending / private</span>`;
  }

  function sevHtml(sevRaw) {
    const sev = String(sevRaw || "low").toLowerCase();
    const sevClass = sev.replace(/[^a-z0-9-]/g, "");
    const label = SEV_LABEL[sev] || sev;
    return `<span class="hof-sev hof-sev--${esc(sevClass)}">${esc(label)}</span>`;
  }

  function toneFor(pct) {
    if (pct >= 100) return "over";
    if (pct >= 75) return "warn";
    return "ok";
  }

  function renderMonthBudget(data, monthSpent) {
    if (!BUDGET) return;
    const bud = data.budget || {};
    const prog = data.program || {};
    const limit = Number(bud.limit_hmc ?? prog.monthly_budget_hmc ?? 200) || 200;
    const remaining = Math.max(0, limit - monthSpent);
    const pct = limit > 0 ? (monthSpent / limit) * 100 : 0;
    const label = bud.period_label || bud.period || "This month";

    BUDGET.hidden = false;
    BUDGET.className = "hof-budget hof-budget--month-only";
    BUDGET.dataset.tone = toneFor(pct);
    BUDGET.innerHTML = `
      <div class="hof-budget-top">
        <div>
          <p class="hof-budget-kicker">Program budget</p>
          <h3 class="hof-budget-title">${esc(label)}</h3>
        </div>
        <div class="hof-budget-nums">
          <span><strong>${esc(fmtHmc(monthSpent))}</strong> spent</span>
          <span class="hof-budget-sep">/</span>
          <span><strong>${esc(fmtHmc(limit))}</strong> HMC</span>
        </div>
      </div>
      <div class="hof-budget-track" role="progressbar" aria-valuemin="0" aria-valuemax="${esc(limit)}" aria-valuenow="${esc(monthSpent)}" aria-label="Monthly security budget used">
        <div class="hof-budget-fill" style="width:${Math.min(100, pct)}%"></div>
      </div>
      <div class="hof-budget-meta">
        <span>${esc(fmtPct(pct))}% used · <strong>${esc(fmtHmc(remaining))}</strong> HMC left</span>
        <span>Open the full table for lifetime history</span>
      </div>
    `;
    if (STAT_BUDGET) STAT_BUDGET.textContent = `${fmtHmc(monthSpent)}/${fmtHmc(limit)}`;
  }

  function renderMonthCards(monthResearchers, period) {
    if (!MONTH_LIST) return;
    if (!monthResearchers.length) {
      MONTH_LIST.hidden = true;
      if (EMPTY) EMPTY.hidden = false;
      return;
    }
    if (EMPTY) EMPTY.hidden = true;
    MONTH_LIST.hidden = false;
    MONTH_LIST.innerHTML = monthResearchers
      .map((r, i) => {
        const paid = periodPaid(r, period);
        const findings = findingsInPeriod(r, period);
        const id = esc(r.id || displayName(r));
        const bits = identityBits(r);
        const avatar = avatarHtml(r);
        const noAv = avatar ? "" : " hof-month-card--no-avatar";
        return `<button type="button" class="hof-month-card${noAv}" data-researcher-id="${id}" style="--hof-i:${i}">
          ${avatar}
          <div class="hof-main">
            <div class="hof-identity">
              <span class="hof-name">${bits.name}</span>
              ${bits.handleHtml}
            </div>
            <p class="hof-month-card-meta">${findings.length} payout${findings.length === 1 ? "" : "s"} this month · click for details</p>
          </div>
          <div class="hof-payout">
            <span class="hof-payout-label">This month</span>
            <span class="hof-payout-value"><strong>${esc(fmtHmc(paid))}</strong> HMC</span>
          </div>
        </button>`;
      })
      .join("");

    MONTH_LIST.querySelectorAll("[data-researcher-id]").forEach((el) => {
      el.addEventListener("click", () => openLedger(el.getAttribute("data-researcher-id")));
    });
  }

  function renderLedgerTable(researchers, period) {
    if (!LEDGER_BODY) return;
    LEDGER_BODY.innerHTML = researchers
      .map((r, i) => {
        const id = esc(r.id || displayName(r));
        const bits = identityBits(r);
        const life = researcherPaid(r);
        const month = periodPaid(r, period);
        const n = (r.findings || []).length;
        const gh = bits.github
          ? `<a class="hof-table-gh" href="${esc(`https://github.com/${encodeURIComponent(bits.github)}`)}" target="_blank" rel="noreferrer" onclick="event.stopPropagation()">@${esc(bits.github)}</a>`
          : bits.handleHtml;
        const avatar = avatarHtml(r, "hof-avatar--sm");
        return `<tr class="hof-ledger-row" tabindex="0" data-researcher-id="${id}">
          <td class="hof-td-rank">#${i + 1}</td>
          <td>
            <div class="hof-table-person${avatar ? "" : " hof-table-person--no-avatar"}">
              ${avatar}
              <div>
                <div class="hof-table-name">${bits.name}</div>
                ${gh}
              </div>
            </div>
          </td>
          <td>${n}</td>
          <td><strong class="hof-td-pay">${esc(fmtHmc(life))}</strong> HMC</td>
          <td>${month > 0 ? `<strong class="hof-td-pay">${esc(fmtHmc(month))}</strong> HMC` : `<span class="hof-muted">—</span>`}</td>
        </tr>`;
      })
      .join("");

    LEDGER_BODY.querySelectorAll(".hof-ledger-row").forEach((row) => {
      const id = row.getAttribute("data-researcher-id");
      const activate = () => selectResearcher(id);
      row.addEventListener("click", activate);
      row.addEventListener("keydown", (e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          activate();
        }
      });
    });
  }

  function setHofView(view) {
    if (!DIALOG) return;
    DIALOG.dataset.hofView = view === "detail" ? "detail" : "list";
  }

  function showLedgerList() {
    setHofView("list");
    if (DETAIL) {
      DETAIL.hidden = false;
      DETAIL.innerHTML =
        '<p class="hof-detail-placeholder">Select a researcher to see findings, fix links, and payouts.</p>';
    }
    LEDGER_BODY?.querySelectorAll(".hof-ledger-row").forEach((row) =>
      row.classList.remove("is-active")
    );
  }

  function selectResearcher(id) {
    if (!STATE || !DETAIL) return;
    const r = STATE.researchers.find((x) => String(x.id || displayName(x)) === String(id));
    if (!r) return;

    LEDGER_BODY?.querySelectorAll(".hof-ledger-row").forEach((row) => {
      row.classList.toggle("is-active", row.getAttribute("data-researcher-id") === String(id));
    });

    const bits = identityBits(r);
    const life = researcherPaid(r);
    const month = periodPaid(r, STATE.period);
    const findings = [...(r.findings || [])].sort((a, b) =>
      String(b.date || "").localeCompare(String(a.date || ""))
    );

    const ghBlock = bits.github
      ? `<a class="btn btn-secondary hof-detail-gh" href="${esc(`https://github.com/${encodeURIComponent(bits.github)}`)}" target="_blank" rel="noreferrer">GitHub @${esc(bits.github)}</a>`
      : "";
    const avatar = avatarHtml(r, "hof-avatar--lg");
    const topCls = avatar ? "hof-detail-top" : "hof-detail-top hof-detail-top--no-avatar";

    DETAIL.hidden = false;
    DETAIL.innerHTML = `
      <div class="hof-detail-head">
        <button type="button" class="hof-detail-back" id="hof-detail-back" aria-label="Back to researcher table">← Back to table</button>
        <div class="${topCls}">
          ${avatar}
          <div>
            <h3 class="hof-detail-name">${bits.name}</h3>
            ${bits.handleHtml}
          </div>
        </div>
        <div class="hof-detail-stats">
          <div><span>Lifetime</span><strong>${esc(fmtHmc(life))} HMC</strong></div>
          <div><span>This month</span><strong>${esc(fmtHmc(month))} HMC</strong></div>
          <div><span>Reports</span><strong>${findings.length}</strong></div>
        </div>
        ${ghBlock}
      </div>
      <div class="hof-detail-scroll">
        <h4 class="hof-detail-findings-title">Findings &amp; fixes</h4>
        <ul class="hof-detail-findings">
          ${findings
            .map((f) => {
              const reported = f.reported
                ? `<span class="hof-detail-reported">Reported ${esc(f.reported)}</span>`
                : "";
              return `<li>
                <div class="hof-detail-finding-head">
                  ${sevHtml(f.severity)}
                  <strong class="hof-detail-pay">${esc(fmtHmc(f.paid_hmc))} HMC</strong>
                </div>
                <p class="hof-detail-finding-title">${esc(f.title || "Finding")}</p>
                <div class="hof-detail-finding-meta">
                  <span>Paid ${esc(f.date || "—")}</span>
                  ${reported}
                  <span>${fixLinkHtml(f)}</span>
                </div>
                ${f.note ? `<p class="hof-detail-note">${esc(f.note)}</p>` : ""}
              </li>`;
            })
            .join("")}
        </ul>
      </div>
    `;
    DETAIL.querySelector("#hof-detail-back")?.addEventListener("click", (e) => {
      e.preventDefault();
      e.stopPropagation();
      showLedgerList();
      CLOSE_BTN?.focus({ preventScroll: true });
    });
    setHofView("detail");
    DETAIL.scrollTop = 0;
    const scroll = DETAIL.querySelector(".hof-detail-scroll");
    if (scroll) scroll.scrollTop = 0;
  }

  function openLedger(selectId) {
    if (!DIALOG) return;
    try {
      if (typeof DIALOG.showModal === "function") {
        if (!DIALOG.open) DIALOG.showModal();
      } else {
        DIALOG.setAttribute("open", "");
      }
    } catch (_) {
      DIALOG.setAttribute("open", "");
    }
    document.documentElement.classList.add("hof-ledger-open");
    if (selectId) selectResearcher(selectId);
    else showLedgerList();
    // Keep focus inside the sheet (helps VoiceOver / mobile browsers)
    CLOSE_BTN?.focus({ preventScroll: true });
  }

  function closeLedger() {
    if (!DIALOG) return;
    document.documentElement.classList.remove("hof-ledger-open");
    setHofView("list");
    try {
      if (typeof DIALOG.close === "function" && DIALOG.open) DIALOG.close();
      else DIALOG.removeAttribute("open");
    } catch (_) {
      DIALOG.removeAttribute("open");
    }
  }

  function render(data) {
    const researchers = Array.isArray(data.researchers) ? [...data.researchers] : [];
    researchers.sort((a, b) => researcherPaid(b) - researcherPaid(a));
    const period = (data.budget && data.budget.period) || "";
    const lifetime =
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
        : researchers.reduce((a, r) => a + (r.findings || []).length, 0);

    const monthResearchers = researchers
      .map((r) => ({ r, paid: periodPaid(r, period) }))
      .filter((x) => x.paid > 0)
      .sort((a, b) => b.paid - a.paid)
      .map((x) => x.r);
    const monthSpent = monthResearchers.reduce((a, r) => a + periodPaid(r, period), 0);

    STATE = { researchers, period, lifetime };

    if (STAT_PAID) STAT_PAID.textContent = fmtHmc(lifetime);
    if (STAT_PEOPLE) STAT_PEOPLE.textContent = String(people);
    if (STAT_REPORTS) STAT_REPORTS.textContent = String(reports);
    if (UPDATED && data.updated) {
      UPDATED.textContent = data.updated;
      UPDATED.setAttribute("datetime", data.updated);
    }
    if (MONTH_SUB) {
      MONTH_SUB.textContent = monthResearchers.length
        ? `${monthResearchers.length} researcher${monthResearchers.length === 1 ? "" : "s"} paid in ${data.budget?.period_label || period || "this period"}.`
        : "No payouts in the current period yet.";
    }

    renderMonthBudget(data, monthSpent);
    renderMonthCards(monthResearchers, period);
    renderLedgerTable(researchers, period);
    ROOT.dataset.state = researchers.length ? "ready" : "empty";
  }

  OPEN_BTN?.addEventListener("click", (e) => {
    e.preventDefault();
    openLedger();
  });
  CLOSE_BTN?.addEventListener("click", (e) => {
    e.preventDefault();
    e.stopPropagation();
    closeLedger();
  });
  // Only close when tapping the backdrop / dialog chrome — not the opaque shell
  DIALOG?.addEventListener("click", (e) => {
    if (e.target === DIALOG) closeLedger();
  });
  DIALOG?.addEventListener("cancel", (e) => {
    e.preventDefault();
    closeLedger();
  });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && DIALOG?.open) closeLedger();
  });

  function maybeOpenFromHash() {
    if (location.hash !== "#open-ledger") return;
    if (ROOT.dataset.state === "ready" || ROOT.dataset.state === "empty" || ROOT.dataset.state === "error") {
      openLedger();
    }
  }
  const stateObs = new MutationObserver(maybeOpenFromHash);
  stateObs.observe(ROOT, { attributes: true, attributeFilter: ["data-state"] });
  maybeOpenFromHash();

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
      if (MONTH_LIST) MONTH_LIST.hidden = true;
      if (BUDGET) BUDGET.hidden = true;
    });
})();
