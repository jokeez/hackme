/**
 * Live OSS disclosure + pool pulse banner — data from disclosure-ticker.json
 */
(() => {
  const CACHE = "20260807wow5";
  const JSON_URL = `/assets/disclosure-ticker.json?v=${CACHE}`;
  const CSS_URL = `/assets/disclosure-ticker.css?v=${CACHE}`;
  const POOL_URL = "/pool/coordinator/api/pool/stats";
  const FALLBACK = {
    interval_ms: 6500,
    pool_poll_ms: 30000,
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
    gold: { border: "rgba(255, 200, 87, 0.5)", dot: "#ffc857", glow: "rgba(255, 200, 87, 0.18)" },
    cyan: { border: "rgba(77, 228, 255, 0.5)", dot: "#4de4ff", glow: "rgba(77, 228, 255, 0.16)" },
    violet: { border: "rgba(167, 139, 250, 0.5)", dot: "#a78bfa", glow: "rgba(167, 139, 250, 0.16)" },
    success: { border: "rgba(110, 255, 173, 0.5)", dot: "#6effad", glow: "rgba(110, 255, 173, 0.16)" },
    medium: { border: "rgba(255, 200, 87, 0.5)", dot: "#ffc857", glow: "rgba(255, 200, 87, 0.14)" },
  };

  let state = {
    slides: [],
    index: 0,
    interval: 6500,
    poolPollMs: 30000,
    timer: null,
    poolTimer: null,
    paused: false,
    pool: null,
  };

  function esc(s) {
    const d = document.createElement("div");
    d.textContent = s == null ? "" : String(s);
    return d.innerHTML;
  }

  function accentOf(slide) {
    return ACCENT[slide.accent] || ACCENT.cyan;
  }

  function fmtHashrate(hs) {
    const n = Number(hs);
    if (!Number.isFinite(n) || n <= 0) return "—";
    if (n >= 1e12) return `${(n / 1e12).toFixed(2)} TH/s`;
    if (n >= 1e9) return `${(n / 1e9).toFixed(1)} GH/s`;
    if (n >= 1e6) return `${(n / 1e6).toFixed(1)} MH/s`;
    return `${Math.round(n)} H/s`;
  }

  function poolStatsList(pool) {
    if (!pool) {
      return [
        { k: "hash", v: "…" },
        { k: "miners", v: "…" },
        { k: "tip", v: "…" },
      ];
    }
    return [
      { k: "hash", v: fmtHashrate(pool.hashrate) },
      { k: "miners", v: String(pool.miners ?? pool.workers ?? "—") },
      { k: "tip", v: `#${pool.tip_height ?? pool.block_height ?? "—"}` },
    ];
  }

  function renderStats(slide) {
    if (slide.live_pool) {
      return poolStatsList(state.pool)
        .map(
          (s) =>
            `<span class="dt-chip" data-k="${esc(s.k)}"><em>${esc(s.k)}</em><b>${esc(s.v)}</b></span>`
        )
        .join("");
    }
    if (!Array.isArray(slide.stats) || !slide.stats.length) return "";
    return slide.stats
      .map((s) => `<span class="dt-chip"><b>${esc(s)}</b></span>`)
      .join("");
  }

  function renderSlide(slide, entering) {
    const a = accentOf(slide);
    const cls = entering ? "dt-slide dt-slide--enter" : "dt-slide dt-slide--active";
    const kind = esc(slide.kind || "hub");
    const chips = renderStats(slide);
    const title =
      slide.live_pool && state.pool
        ? `Official pool · ${fmtHashrate(state.pool.hashrate)}`
        : slide.title;
    const detail =
      slide.live_pool && state.pool
        ? `${state.pool.miners ?? state.pool.workers ?? "—"} miners · tip ${
            state.pool.tip_height ?? state.pool.block_height ?? "—"
          }`
        : slide.detail;
    return `
      <div class="${cls}" data-kind="${kind}" style="--dt-accent:${a.dot};--dt-border:${a.border};--dt-glow:${a.glow}">
        <span class="dt-rail" aria-hidden="true"></span>
        <span class="dt-badge">${esc(slide.badge)}</span>
        <div class="dt-copy">
          <strong class="dt-title">${esc(title)}</strong>
          ${detail ? `<span class="dt-detail">${esc(detail)}</span>` : ""}
        </div>
        ${chips ? `<div class="dt-chips">${chips}</div>` : ""}
        <a class="dt-cta" href="${esc(slide.href || "/reports/oss-cve/")}">${esc(
          slide.cta || "Learn more"
        )}<span aria-hidden="true">→</span></a>
      </div>`;
  }

  function buildShell() {
    const wrap = document.createElement("div");
    wrap.className = "disclosure-ticker";
    wrap.setAttribute("role", "region");
    wrap.setAttribute("aria-label", "Live research, pool, and disclosure updates");
    wrap.innerHTML = shellInnerHTML();
    return wrap;
  }

  function shellInnerHTML() {
    return `
      <div class="dt-shell">
        <div class="dt-bar glass">
          <div class="dt-aurora" aria-hidden="true"></div>
          <div class="dt-scan" aria-hidden="true"></div>
          <div class="dt-live" title="Live site pulse">
            <span class="dt-pulse" aria-hidden="true"></span>
            <span class="dt-live-label">LIVE</span>
          </div>
          <div class="dt-poolchip dt-poolchip--pending" title="Official pool">
            <span class="dt-poolchip-dot" aria-hidden="true"></span>
            <span class="dt-poolchip-val">pool …</span>
          </div>
          <div class="dt-stage" aria-live="polite"></div>
          <div class="dt-dots" aria-hidden="true"></div>
          <div class="dt-progress" aria-hidden="true"><span class="dt-progress-bar"></span></div>
        </div>
      </div>`;
  }

  function ensureStyles() {
    if (document.querySelector('link[data-disclosure-ticker-css="1"]')) return;
    const link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = CSS_URL;
    link.dataset.disclosureTickerCss = "1";
    document.head.appendChild(link);
  }

  /**
   * Hydrate existing slot in place (same outer box / height) — no replaceWith CLS.
   * Only create a new node if the page has no ticker markup yet.
   */
  function mountAfterHeader() {
    const header = document.querySelector("header.topbar");
    let wrap = document.querySelector(".disclosure-ticker");
    if (!wrap) {
      if (!header) return null;
      wrap = buildShell();
      header.insertAdjacentElement("afterend", wrap);
      return wrap;
    }
    // Keep outer element; swap inner once from skeleton → live chrome.
    wrap.classList.remove("disclosure-ticker-slot");
    wrap.removeAttribute("aria-hidden");
    wrap.setAttribute("role", "region");
    wrap.setAttribute("aria-label", "Live research, pool, and disclosure updates");
    if (!wrap.querySelector(".dt-aurora") || wrap.querySelector(".dt-bar--skeleton")) {
      wrap.innerHTML = shellInnerHTML();
    }
    return wrap;
  }

  function updatePoolChip() {
    const chip = document.querySelector(".dt-poolchip");
    const val = document.querySelector(".dt-poolchip-val");
    if (!chip || !val) return;
    if (!state.pool) {
      chip.classList.add("dt-poolchip--pending");
      val.textContent = "pool …";
      return;
    }
    chip.classList.remove("dt-poolchip--pending");
    val.textContent = `${fmtHashrate(state.pool.hashrate)} · ${
      state.pool.miners ?? state.pool.workers ?? "—"
    }m`;
  }

  function refreshLivePoolSlide() {
    const slide = state.slides[state.index];
    if (!slide || !slide.live_pool) return;
    paintClean(state.index);
  }

  async function pollPool() {
    try {
      const res = await fetch(POOL_URL, { cache: "no-store" });
      if (!res.ok) return;
      const data = await res.json();
      if (!data || data.status === "error") return;
      state.pool = data;
      updatePoolChip();
      refreshLivePoolSlide();
    } catch {
      /* keep last */
    }
  }

  function renderDots() {
    const dots = document.querySelector(".dt-dots");
    if (!dots) return;
    dots.innerHTML = state.slides
      .map(
        (_, i) =>
          `<button type="button" class="dt-dot${
            i === state.index ? " dt-dot--on" : ""
          }" data-i="${i}" aria-label="Slide ${i + 1} of ${state.slides.length}"></button>`
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

  function applyAccent(slide) {
    const bar = document.querySelector(".dt-bar");
    if (!bar || !slide) return;
    const a = accentOf(slide);
    bar.style.setProperty("--dt-accent", a.dot);
    bar.style.setProperty("--dt-border", a.border);
    bar.style.setProperty("--dt-glow", a.glow);
  }

  function paintClean(i) {
    const stage = document.querySelector(".dt-stage");
    if (!stage || !state.slides.length) return;
    state.index = ((i % state.slides.length) + state.slides.length) % state.slides.length;
    const slide = state.slides[state.index];
    applyAccent(slide);
    stage.replaceChildren();
    stage.insertAdjacentHTML("beforeend", renderSlide(slide, false));
    document.querySelectorAll(".dt-dot").forEach((d, j) => {
      d.classList.toggle("dt-dot--on", j === state.index);
    });
    restartProgress();
  }

  function showSlide(i, animate) {
    const stage = document.querySelector(".dt-stage");
    if (!stage || !state.slides.length) return;

    if (document.hidden || state.paused || !animate) {
      paintClean(i);
      return;
    }

    state.index = ((i % state.slides.length) + state.slides.length) % state.slides.length;
    const slide = state.slides[state.index];
    applyAccent(slide);
    const old = stage.querySelector(".dt-slide--active");
    if (old) {
      old.classList.remove("dt-slide--active");
      old.classList.add("dt-slide--exit");
      setTimeout(() => {
        if (old.parentNode) old.remove();
      }, 360);
    }
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

  function resume() {
    state.paused = false;
    const ticker = document.querySelector(".disclosure-ticker");
    if (ticker) ticker.classList.remove("is-hidden");
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
      else resume();
    });
  }

  async function init() {
    ensureStyles();
    if (!mountAfterHeader()) return;

    let data = FALLBACK;
    try {
      const res = await fetch(JSON_URL, { cache: "no-store" });
      if (res.ok) data = await res.json();
    } catch {
      /* fallback */
    }

    state.slides = Array.isArray(data.slides) && data.slides.length ? data.slides : FALLBACK.slides;
    state.interval = Number(data.interval_ms) > 2000 ? Number(data.interval_ms) : 6500;
    state.poolPollMs = Number(data.pool_poll_ms) >= 10000 ? Number(data.pool_poll_ms) : 30000;

    paintClean(0);
    renderDots();
    bindPause();
    pollPool();
    state.poolTimer = setInterval(() => {
      if (!document.hidden) pollPool();
    }, state.poolPollMs);

    if (!document.hidden) schedule();
    else pause("hidden");
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
