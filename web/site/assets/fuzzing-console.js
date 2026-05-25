/* Read-only fuzzing status on hackme.tech — no admin token, no operator treasury. */
(function () {
  const base = location.origin;
  const poolHint = document.getElementById("pool-hint");
  const tasksBody = document.getElementById("tasks-table-body");
  const tasksStatus = document.getElementById("tasks-status");

  async function get(path) {
    const r = await fetch(base + path, { cache: "no-store" });
    const t = await r.text();
    let j = {};
    try {
      j = JSON.parse(t);
    } catch (_) {
      j = { error: t.slice(0, 200) };
    }
    return { ok: r.ok, status: r.status, json: j };
  }

  function renderTasks(tasks) {
    if (!tasks || !tasks.length) {
      tasksBody.innerHTML =
        '<tr><td colspan="6" class="subtle">No public orders yet.</td></tr>';
      return;
    }
    tasksBody.innerHTML = tasks
      .map((t) => {
        const pct =
          t.progress_pct != null
            ? t.progress_pct.toFixed(1) + "%"
            : t.target_solves
              ? ((100 * (t.progress_count || 0)) / t.target_solves).toFixed(1) + "%"
              : "—";
        return `<tr>
          <td>${escapeHtml(t.id || "")}</td>
          <td>${escapeHtml(t.status || "")}</td>
          <td>${t.difficulty_score ?? "—"}</td>
          <td>${t.target_solves ?? "—"}</td>
          <td>${pct}</td>
          <td>${t.prepaid_hmc != null ? t.prepaid_hmc : "—"}</td>
        </tr>`;
      })
      .join("");
  }

  function escapeHtml(s) {
    return String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");
  }

  async function refresh() {
    tasksStatus.textContent = "";
    const [t, p] = await Promise.all([
      get("/api/tasks"),
      get("/pool/coordinator/api/pool/stats"),
    ]);
    if (t.ok && Array.isArray(t.json.tasks)) {
      renderTasks(t.json.tasks);
      tasksStatus.textContent =
        "Public summary only (no manifest). Create orders: Developer Dashboard or hackme-fuzzing CLI.";
    } else {
      tasksStatus.textContent = "Tasks HTTP " + t.status;
    }
    if (p.ok && p.json.status === "ok" && poolHint) {
      const mode = p.json.scheduler_mode || "—";
      const active = p.json.orders_active ?? "—";
      poolHint.textContent =
        "Pool: scheduler=" + mode + ", orders_active=" + active + " — miners fill open fuzzing work.";
    }
  }

  document.getElementById("btn-refresh")?.addEventListener("click", refresh);
  refresh();
})();
