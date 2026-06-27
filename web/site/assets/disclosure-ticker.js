/**
 * Live OSS disclosure + B2B trust banner — data from disclosure-ticker.json
 */
(() => {
  const JSON_URL = "/assets/disclosure-ticker.json";
  const FALLBACK = {
    interval_ms: 6500,
    slides: [
      {
        kind: "hub",
        badge: "Research",
        title: "OSS coordinated disclosure pipeline",
        detail: "Tier-D ASAN hunts on real upstream clones",
        cta: "OSS CVE hub",
        href: "/reports/oss-cve/",
        accent: "gold",
      },
    ],
  };

  const ACCENT = {
    gold: { border: "rgba(255, 200, 87, 0.55)", glow: "rgba(255, 200, 87, 0.18)", dot: "#ffc857" },
    cyan: { border: "rgba(77, 228, 255, 0.55)", glow: "rgba(77, 228, 255, 0.18)", dot: "#4de4ff" },
    violet: { border: "rgba(167, 139, 250, 0.55)", glow: "rgba(167, 139, 250, 0.2)", dot: "#a78bfa" },
    success: { border: "rgba(110, 255, 173, 0.55)", glow: "rgba(110, 255, 173, 0.18)", dot: "#6effad" },
    medium: { border: "rgba(255, 200, 87, 0.55)", glow: "rgba(255, 200, 87, 0.18)", dot: "#ffc857" },
  };

  let state = { slides: [], index: 0, interval: 6500, timer: null, progress: null };

  function esc(s) {
    const d = document.createElement("div");
    d.textContent = s || "";
    return d.innerHTML;
  }

  function accentOf(slide) {
    return ACCENT[slide.accent] || ACCENT.cyan;
  }

  function kindIcon(kind) {
    switch (kind) {
      case "cve":
        return "◆";
      case "fixed":
        return "✓";
      case "b2b":
        return "⚡";
      case "case":
        return "◎";
      default:
        return "●";
    }
  }

  function renderSlide(slide, entering) {
    const a = accentOf(slide);
    const cls = entering ? "dt-slide dt-slide--enter" : "dt-slide dt-slide--active";
    return `
      <div class="${cls}" data-accent="${esc(slide.accent || "cyan")}" style="--dt-accent:${a.dot};--dt-glow:${a.glow};--dt-border:${a.border}">
        <span class="dt-kind" aria-hidden="true">${kindIcon(slide.kind)}</span>
        <div class="dt-copy">
          <span class="dt-badge">${esc(slide.badge)}</span>
          <strong class="dt-title">${esc(slide.title)}</strong>
          ${slide.detail ? `<span class="dt-detail">${esc(slide.detail)}</span>` : ""}
        </div>
        <a class="dt-cta" href="${esc(slide.href || "/reports/oss-cve/")}">${esc(slide.cta || "Learn more")}<span aria-hidden="true">→</span></a>
      </div>`;
  }

  function buildShell() {
    const wrap = document.createElement("div");
    wrap.className = "disclosure-ticker";
    wrap.setAttribute("role", "region");
    wrap.setAttribute("aria-label", "Live security research and disclosure updates");
    wrap.innerHTML = `
      <div class="dt-aurora" aria-hidden="true"></div>
      <div class="dt-scanlines" aria-hidden="true"></div>
      <div class="dt-inner">
        <div class="dt-live">
          <span class="dt-pulse" aria-hidden="true"></span>
          <span class="dt-live-label">LIVE</span>
        </div>
        <div class="dt-stage" aria-live="polite"></div>
        <div class="dt-dots" aria-hidden="true"></div>
      </div>
      <div class="dt-progress" aria-hidden="true"><span class="dt-progress-bar"></span></div>
    `;
    return wrap;
  }

  function mountAfterHeader(shell) {
    const header = document.querySelector("header.topbar");
    if (!header || document.querySelector(".disclosure-ticker")) return false;
    header.insertAdjacentElement("afterend", shell);
    return true;
  }

  function renderDots() {
    const dots = document.querySelector(".dt-dots");
    if (!dots) return;
    dots.innerHTML = state.slides
      .map(
        (_, i) =>
          `<button type="button" class="dt-dot${i === state.index ? " dt-dot--on" : ""}" data-i="${i}" aria-label="Slide ${i + 1}"></button>`
      )
      .join("");
    dots.querySelectorAll(".dt-dot").forEach((btn) => {
      btn.addEventListener("click", () => {
        const i = Number(btn.dataset.i);
        if (Number.isFinite(i)) goTo(i);
      });
    });
  }

  function restartProgress() {
    const bar = document.querySelector(".dt-progress-bar");
    if (!bar) return;
    bar.style.animation = "none";
    void bar.offsetWidth;
    bar.style.animation = "";
    bar.style.animationDuration = `${state.interval}ms`;
  }

  function showSlide(i, animate) {
    const stage = document.querySelector(".dt-stage");
    if (!stage || !state.slides.length) return;
    state.index = ((i % state.slides.length) + state.slides.length) % state.slides.length;
    const slide = state.slides[state.index];
    if (!animate) {
      stage.innerHTML = renderSlide(slide, false);
    } else {
      const old = stage.querySelector(".dt-slide--active");
      if (old) {
        old.classList.remove("dt-slide--active");
        old.classList.add("dt-slide--exit");
        setTimeout(() => old.remove(), 420);
      }
      stage.insertAdjacentHTML("beforeend", renderSlide(slide, true));
      requestAnimationFrame(() => {
        const el = stage.querySelector(".dt-slide--enter");
        if (el) {
          el.classList.remove("dt-slide--enter");
          el.classList.add("dt-slide--active");
        }
      });
    }
    document.querySelectorAll(".dt-dot").forEach((d, j) => {
      d.classList.toggle("dt-dot--on", j === state.index);
    });
    restartProgress();
  }

  function goTo(i) {
    if (i === state.index) return;
    showSlide(i, true);
    schedule();
  }

  function next() {
    showSlide(state.index + 1, true);
    schedule();
  }

  function schedule() {
    if (state.timer) clearTimeout(state.timer);
    state.timer = setTimeout(next, state.interval);
  }

  function bindPause() {
    const ticker = document.querySelector(".disclosure-ticker");
    if (!ticker) return;
    ticker.addEventListener("mouseenter", () => {
      if (state.timer) clearTimeout(state.timer);
      const bar = document.querySelector(".dt-progress-bar");
      if (bar) bar.style.animationPlayState = "paused";
    });
    ticker.addEventListener("mouseleave", () => {
      const bar = document.querySelector(".dt-progress-bar");
      if (bar) bar.style.animationPlayState = "running";
      schedule();
    });
  }

  async function init() {
    if (!mountAfterHeader(buildShell())) return;

    let data = FALLBACK;
    try {
      const res = await fetch(JSON_URL, { cache: "no-store" });
      if (res.ok) data = await res.json();
    } catch {
      /* fallback */
    }

    state.slides = Array.isArray(data.slides) && data.slides.length ? data.slides : FALLBACK.slides;
    state.interval = Number(data.interval_ms) > 2000 ? Number(data.interval_ms) : 6500;

    const stage = document.querySelector(".dt-stage");
    if (stage) stage.innerHTML = renderSlide(state.slides[0], false);
    renderDots();
    bindPause();
    restartProgress();
    schedule();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
