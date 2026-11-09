/**
 * HackMe deliverable report viewer — fuzz_report_v2 HTML in a side drawer.
 * Usage: data-rh-report="/path/to/deliverable.html" on a button, or
 *        HackMeReportViewer.open({ href, title })
 */
(() => {
  const ROOT_ID = "hm-report-drawer";

  function ensureDrawer() {
    let root = document.getElementById(ROOT_ID);
    if (root) return root;
    root = document.createElement("div");
    root.id = ROOT_ID;
    root.className = "hm-report-drawer";
    root.hidden = true;
    root.innerHTML = `
      <div class="hm-report-backdrop" data-hm-close tabindex="-1" aria-hidden="true"></div>
      <aside class="hm-report-panel" role="dialog" aria-modal="true" aria-labelledby="hm-report-title">
        <header class="hm-report-head">
          <div>
            <p class="hm-report-kicker">Customer deliverable · fuzz_report_v2</p>
            <h2 id="hm-report-title">Security report</h2>
          </div>
          <div class="hm-report-actions">
            <a class="btn btn-secondary hm-report-open" href="#" target="_blank" rel="noopener">Open full</a>
            <button type="button" class="hm-report-x" data-hm-close aria-label="Close report">✕</button>
          </div>
        </header>
        <div class="hm-report-body">
          <iframe class="hm-report-frame" title="HackMe security report" loading="lazy"></iframe>
        </div>
      </aside>`;
    document.body.appendChild(root);
    root.addEventListener("click", (e) => {
      if (e.target.closest("[data-hm-close]")) close();
    });
    document.addEventListener("keydown", (e) => {
      if (e.key === "Escape" && !root.hidden) close();
    });
    return root;
  }

  function open({ href, title } = {}) {
    if (!href) return;
    const root = ensureDrawer();
    const frame = root.querySelector(".hm-report-frame");
    const openLink = root.querySelector(".hm-report-open");
    const titleEl = root.querySelector("#hm-report-title");
    if (titleEl) titleEl.textContent = title || "Security report";
    if (openLink) openLink.href = href;
    if (frame) frame.src = href;
    root.hidden = false;
    document.body.classList.add("hm-report-open");
    requestAnimationFrame(() => root.classList.add("is-open"));
    const closeBtn = root.querySelector(".hm-report-x");
    if (closeBtn) closeBtn.focus();
  }

  function close() {
    const root = document.getElementById(ROOT_ID);
    if (!root || root.hidden) return;
    root.classList.remove("is-open");
    document.body.classList.remove("hm-report-open");
    const frame = root.querySelector(".hm-report-frame");
    window.setTimeout(() => {
      root.hidden = true;
      if (frame) frame.removeAttribute("src");
    }, 220);
  }

  function bindDelegates() {
    document.addEventListener("click", (e) => {
      const btn = e.target.closest("[data-rh-report]");
      if (!btn) return;
      e.preventDefault();
      e.stopPropagation();
      open({
        href: btn.getAttribute("data-rh-report"),
        title: btn.getAttribute("data-rh-report-title") || btn.getAttribute("aria-label") || "Security report",
      });
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", bindDelegates);
  } else {
    bindDelegates();
  }

  window.HackMeReportViewer = { open, close };
})();
