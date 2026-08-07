/**
 * Live OSS disclosure + B2B trust banner — data from disclosure-ticker.json
 */
(() => {
  const JSON_URL = "/assets/disclosure-ticker.json?v=20260807ticker";
  const FALLBACK = {
    interval_ms: 7000,
    slides: [
      {
        kind: "hub",
        badge: "Research",
        title: "OSS coordinated disclosure pipeline",
        detail: "Tier-D ASAN hunts · public case ledgers",
        cta: "OSS CVE hub",
        href: "/reports/oss-cve/",
        accent: "gold",
      },
    ],
  };

  const ACCENT = {
    gold: { border: "rgba(255, 200, 87, 0.45)", dot: "#ffc857" },
    cyan: { border: "rgba(77, 228, 255, 0.4)", dot: "#4de4ff" },
    violet: { border: "rgba(167, 139, 250, 0.45)", dot: "#a78bfa" },
    success: { border: "rgba(110, 255, 173, 0.45)", dot: "#6effad" },
    medium: { border: "rgba(255, 200, 87, 0.45)", dot: "#ffc857" },
  };

  let state = { slides: [], index: 0, interval: 7000, timer: null, paused: false };

  function esc(s) {
    const d = document.createElement("div");
    d.textContent = s || "";
    return d.innerHTML;
  }

  function accentOf(slide) {
    return ACCENT[slide.accent] || ACCENT.cyan;
  }

  function renderSlide(slide, entering) {
    const a = accentOf(slide);
    const cls = entering ? "dt-slide dt-slide--enter" : "dt-slide dt-slide--active";
    return `
      <div class="${cls}" style="--dt-accent:${a.dot};--dt-border:${a.border}">
        <span class="dt-badge">${esc(slide.badge)}</span>
        <strong class="dt-title">${esc(slide.title)}</strong>
        ${slide.detail ? `<span class="dt-detail">${esc(slide.detail)}</span>` : ""}
        <a class="dt-cta" href="${esc(slide.href || "/reports/oss-cve/")}">${esc(slide.cta || "Learn more")}<span aria-hidden="true">→</span></a>
      </div>`;
  }

  function buildShell() {
    const wrap = document.createElement("div");
    wrap.className = "disclosure-ticker";
    wrap.setAttribute("role", "region");
    wrap.setAttribute("aria-label", "Live security research and disclosure updates");
    wrap.innerHTML = `
      <div class="dt-shell">
        <div class="dt-bar glass">
          <div class="dt-live" title="Research pipeline status">
            <span class="dt-pulse" aria-hidden="true"></span>
            <span class="dt-live-label">LIVE</span>
          </div>
          <div class="dt-stage" aria-live="polite"></div>
          <div class="dt-dots" aria-hidden="true"></div>
          <div class="dt-progress" aria-hidden="true"><span class="dt-progress-bar"></span></div>
        </div>
      </div>`;
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
          `<button type="button" class="dt-dot${i === state.index ? " dt-dot--on" : ""}" data-i="${i}" aria-label="Slide ${i + 1} of ${state.slides.length}"></button>`
      )
      .join("");
    dots.querySelectorAll(".dt-dot").forEach((btn) => {
      btn.addEventListener("click", () => {
        const i = Number(btn.dataset.i);
        if (Number.isFinite(i)) goTo(i);
      });
    });
  }

  function setProgressPaused(paused) {
    const bar = document.querySelector(".dt-progress-bar");
    if (bar) bar.style.animationPlayState = paused ? "paused" : "running";
    const ticker = document.querySelector(".disclosure-ticker");
    if (ticker) ticker.classList.toggle("is-paused", !!paused);
  }

  function restartProgress() {
    const bar = document.querySelector(".dt-progress-bar");
    if (!bar) return;
    bar.style.animation = "none";
    void bar.offsetWidth;
    bar.style.animation = "";
    bar.style.animationDuration = `${state.interval}ms`;
    bar.style.animationPlayState = state.paused || document.hidden ? "paused" : "running";
  }

  /** Hard-reset stage to one slide — avoids stacked/ghost slides after tab throttle. */
  function paintClean(i) {
    const stage = document.querySelector(".dt-stage");
    if (!stage || !state.slides.length) return;
    state.index = ((i % state.slides.length) + state.slides.length) % state.slides.length;
    stage.replaceChildren();
    stage.insertAdjacentHTML("beforeend", renderSlide(state.slides[state.index], false));
    document.querySelectorAll(".dt-dot").forEach((d, j) => {
      d.classList.toggle("dt-dot--on", j === state.index);
    });
    restartProgress();
  }

  function showSlide(i, animate) {
    const stage = document.querySelector(".dt-stage");
    if (!stage || !state.slides.length) return;

    // Hidden / background tabs throttle timers → exit/enter overlap and "blend".
    if (document.hidden || state.paused || !animate) {
      paintClean(i);
      return;
    }

    state.index = ((i % state.slides.length) + state.slides.length) % state.slides.length;
    const slide = state.slides[state.index];
    const old = stage.querySelector(".dt-slide--active");
    if (old) {
      old.classList.remove("dt-slide--active");
      old.classList.add("dt-slide--exit");
      setTimeout(() => {
        if (old.parentNode) old.remove();
      }, 360);
    }
    // Drop any leftover ghosts from a prior interrupted transition.
    stage.querySelectorAll(".dt-slide:not(.dt-slide--exit)").forEach((el) => el.remove());
    stage.insertAdjacentHTML("beforeend", renderSlide(slide, true));
    requestAnimationFrame(() => {
      if (document.hidden) {
        paintClean(state.index);
        return;
      }
      const el = stage.querySelector(".dt-slide--enter");
      if (el) {
        el.classList.remove("dt-slide--enter");
        el.classList.add("dt-slide--active");
      }
    });
    // Final sweep: only one visible slide after transition (prevents blend on tab sleep).
    setTimeout(() => {
      if (document.hidden) return;
      const alive = stage.querySelector(".dt-slide--active");
      stage.querySelectorAll(".dt-slide").forEach((el) => {
        if (el !== alive) el.remove();
      });
      if (!alive) paintClean(state.index);
    }, 400);
    document.querySelectorAll(".dt-dot").forEach((d, j) => {
      d.classList.toggle("dt-dot--on", j === state.index);
    });
    restartProgress();
  }

  function goTo(i) {
    if (i === state.index && !document.querySelector(".dt-slide--exit")) return;
    showSlide(i, !document.hidden);
    schedule();
  }

  function next() {
    showSlide(state.index + 1, !document.hidden);
    schedule();
  }

  function schedule() {
    if (state.timer) clearTimeout(state.timer);
    state.timer = null;
    if (state.paused || document.hidden) return;
    state.timer = setTimeout(next, state.interval);
  }

  function pause(reason) {
    state.paused = true;
    if (state.timer) clearTimeout(state.timer);
    state.timer = null;
    setProgressPaused(true);
    const ticker = document.querySelector(".disclosure-ticker");
    if (ticker && reason === "hidden") ticker.classList.add("is-hidden");
  }

  function resume(reason) {
    state.paused = false;
    const ticker = document.querySelector(".disclosure-ticker");
    if (ticker) ticker.classList.remove("is-hidden");
    // Always repaint one clean slide after background — kills merged layers.
    paintClean(state.index);
    setProgressPaused(false);
    schedule();
  }

  function bindPause() {
    const ticker = document.querySelector(".disclosure-ticker");
    if (!ticker) return;
    ticker.addEventListener("mouseenter", () => {
      if (document.hidden) return;
      if (state.timer) clearTimeout(state.timer);
      state.timer = null;
      setProgressPaused(true);
    });
    ticker.addEventListener("mouseleave", () => {
      if (document.hidden) return;
      setProgressPaused(false);
      schedule();
    });
    document.addEventListener("visibilitychange", () => {
      if (document.hidden) pause("hidden");
      else resume("visible");
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
    state.interval = Number(data.interval_ms) > 2000 ? Number(data.interval_ms) : 7000;

    paintClean(0);
    renderDots();
    bindPause();
    if (!document.hidden) schedule();
    else pause("hidden");
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
