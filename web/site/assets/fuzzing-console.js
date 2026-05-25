/* Read-only fuzzing status on hackme.tech — no admin token in browser. */
(function () {
  const base = location.origin;
  const poolHint = document.getElementById("pool-hint");
  const walletAddress = document.getElementById("wallet-address");
  const walletBalance = document.getElementById("wallet-balance");
  const walletNonce = document.getElementById("wallet-nonce");
  const walletSource = document.getElementById("wallet-source");
  const walletStatus = document.getElementById("wallet-status");
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

  function setWallet(w) {
    walletAddress.textContent = w.address || "—";
    walletBalance.textContent =
      w.balance_hmc != null ? String(w.balance_hmc) + " HMC" : "—";
    walletNonce.textContent = w.next_nonce != null ? String(w.next_nonce) : "—";
    walletSource.textContent = w.wallet_source || "canonical";
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
    walletStatus.textContent = "Loading…";
    tasksStatus.textContent = "";
    const [w, t, p] = await Promise.all([
      get("/api/wallet"),
      get("/api/tasks"),
      get("/pool/coordinator/api/pool/stats"),
    ]);
    if (w.ok) {
      setWallet(w.json);
      walletStatus.textContent = "";
    } else {
      walletStatus.textContent = "Wallet HTTP " + w.status;
    }
    if (t.ok && Array.isArray(t.json.tasks)) {
      renderTasks(t.json.tasks);
      tasksStatus.textContent = "Public summary only (no manifest). Create orders: Developer Console or hackme-fuzzing CLI.";
    } else {
      tasksStatus.textContent = "Tasks HTTP " + t.status;
    }
    if (p.ok && p.json.status === "ok") {
      const mode = p.json.scheduler_mode || "—";
      const active = p.json.orders_active ?? "—";
      poolHint.textContent =
        "Pool: scheduler=" + mode + ", orders_active=" + active + " — miners fill open fuzzing work.";
    }
  }

  document.getElementById("btn-refresh")?.addEventListener("click", refresh);
  refresh();
})();
