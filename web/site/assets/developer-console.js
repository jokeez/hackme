/* Developer console — orders + WASM upload + customer fuzz reports (Option A). */
(function () {
  const Dev = window.HackmeDev;
  if (!Dev) return;

  const MAX_WASM_BYTES = 256 * 1024;
  let wasmBytes = null;
  let wasmArtifactHash = "";
  let wasmHex = "";

  const $ = (id) => document.getElementById(id);

  function setStatus(el, text, ok) {
    if (!el) return;
    el.textContent = text || "";
    el.classList.remove("dev-status-ok", "dev-status-err");
    if (ok === true) el.classList.add("dev-status-ok");
    if (ok === false) el.classList.add("dev-status-err");
  }

  function minFairReward() {
    const diff = Math.max(1, Math.min(100, Math.floor(Number($("create-diff")?.value) || 1)));
    return diff * 0.0005;
  }

  function updateFairnessHint() {
    const el = $("create-fairness");
    if (!el) return;
    const minR = minFairReward();
    const rew = Number($("create-reward")?.value) || 0;
    const tgt = Math.max(1, Math.floor(Number($("create-target")?.value) || 1));
    el.textContent =
      "Fairness: min reward_hmc = " +
      minR.toFixed(6) +
      " · prepaid ≈ " +
      (rew * tgt).toFixed(6) +
      " HMC (+5% fee) · WASM: " +
      (wasmBytes ? wasmBytes.length + " bytes · " + wasmArtifactHash.slice(0, 16) + "…" : "not loaded");
  }

  function buildManifestObject() {
    const id = String($("create-id")?.value || "").trim();
    const obj = {
      id,
      kind: String($("create-kind")?.value || "").trim() || "synthetic_poh_v1",
      difficulty_score: Math.max(1, Math.min(100, Math.floor(Number($("create-diff")?.value) || 1))),
      reward_hmc: Number($("create-reward")?.value) || 0,
      target_solves: Math.max(1, Math.floor(Number($("create-target")?.value) || 1)),
      payer_ref: String($("create-payer")?.value || "").trim() || "integrator:web-console",
    };
    if (wasmHex && wasmArtifactHash) {
      obj.artifact_hash = wasmArtifactHash;
      obj.wasm_check_hex = wasmHex;
    }
    return obj;
  }

  function refreshManifestPreview() {
    const pre = $("create-manifest-pre");
    if (!pre) return;
    const obj = buildManifestObject();
    const copy = Object.assign({}, obj);
    if (copy.wasm_check_hex && copy.wasm_check_hex.length > 80) {
      copy.wasm_check_hex = copy.wasm_check_hex.slice(0, 64) + "…(" + copy.wasm_check_hex.length + " hex chars)";
    }
    pre.textContent = JSON.stringify(copy, null, 2);
    updateFairnessHint();
  }

  function applyTemplate(name) {
    const ts = Date.now().toString(36);
    if (name === "small") {
      $("create-id").value = "order-web-small-" + ts;
      $("create-diff").value = "5";
      $("create-reward").value = "0.0025";
      $("create-target").value = "1";
    } else if (name === "medium") {
      $("create-id").value = "order-web-medium-" + ts;
      $("create-diff").value = "15";
      $("create-reward").value = "0.01";
      $("create-target").value = "5";
    } else {
      $("create-id").value = "order-web-audit-" + ts;
      $("create-diff").value = "25";
      $("create-reward").value = "0.02";
      $("create-target").value = "3";
      $("create-payer").value = "company:security-audit";
    }
    refreshManifestPreview();
  }

  async function loadWasmFile(file) {
    const meta = $("create-wasm-meta");
    if (!file) {
      wasmBytes = null;
      wasmArtifactHash = "";
      wasmHex = "";
      if (meta) meta.textContent = "No WASM file selected.";
      refreshManifestPreview();
      return;
    }
    if (!/\.wasm$/i.test(file.name) && file.type !== "application/wasm") {
      setStatus($("create-status"), "Pick a .wasm file (build locally first).", false);
      return;
    }
    const buf = await file.arrayBuffer();
    if (buf.byteLength > MAX_WASM_BYTES) {
      setStatus(
        $("create-status"),
        "WASM too large (" + buf.byteLength + " B). Build a smaller check module or use CLI.",
        false
      );
      return;
    }
    wasmBytes = new Uint8Array(buf);
    wasmArtifactHash = await Dev.sha256Hex(wasmBytes);
    wasmHex = Dev.bytesToHex(wasmBytes);
    if (meta) {
      meta.textContent =
        file.name +
        " · " +
        wasmBytes.length +
        " bytes · sha256 " +
        wasmArtifactHash.slice(0, 20) +
        "…";
    }
    setStatus($("create-status"), "WASM ready for manifest.", true);
    refreshManifestPreview();
  }

  async function refreshWallet() {
    const r = await Dev.api("GET", "/api/wallet");
    if (r.ok) {
      $("wallet-address").textContent = r.json.address || "—";
      $("wallet-balance").textContent =
        r.json.balance_hmc != null ? String(r.json.balance_hmc) + " HMC" : "—";
      $("wallet-nonce").textContent = r.json.next_nonce != null ? String(r.json.next_nonce) : "—";
      setStatus($("wallet-status"), "", true);
    } else {
      setStatus($("wallet-status"), "Wallet HTTP " + r.status, false);
    }
    const p = await Dev.api("GET", "/pool/coordinator/api/pool/stats");
    if (p.ok && p.json.status === "ok" && $("pool-hint")) {
      $("pool-hint").textContent =
        "Pool scheduler=" +
        (p.json.scheduler_mode || "—") +
        " · orders_active=" +
        (p.json.orders_active ?? "—") +
        " — miners fill your open orders.";
    }
  }

  async function refreshOrders() {
    const body = $("orders-table-body");
    const st = $("orders-status");
    if (!Dev.getToken()) {
      if (body) {
        body.innerHTML =
          '<tr><td colspan="7" class="subtle">Save developer token first.</td></tr>';
      }
      setStatus(st, "Token required for detailed list.", false);
      return;
    }
    setStatus(st, "Loading…");
    const r = await Dev.api("GET", "/api/tasks");
    if (!r.ok) {
      setStatus(st, "Tasks HTTP " + r.status + ": " + (r.json.error || r.json.code || ""), false);
      return;
    }
    const tasks = Array.isArray(r.json.tasks) ? r.json.tasks : [];
    if (!tasks.length) {
      body.innerHTML = '<tr><td colspan="7" class="subtle">No orders yet.</td></tr>';
      setStatus(st, "Empty list.", true);
      $("orders-manifest-pre").textContent = "—";
      return;
    }
    body.innerHTML = tasks
      .map((t) => {
        const pct =
          t.progress_pct != null
            ? t.progress_pct.toFixed(1) + "%"
            : t.target_solves
              ? ((100 * (t.progress_count || 0)) / t.target_solves).toFixed(1) + "%"
              : "—";
        const rw = t.reward != null ? t.reward : t.reward_hmc;
        return `<tr data-order-id="${Dev.escapeHtml(t.id || "")}" class="order-row" style="cursor:pointer">
          <td>${Dev.escapeHtml(t.id || "")}</td>
          <td>${Dev.escapeHtml(t.status || "")}</td>
          <td>${t.difficulty_score ?? "—"}</td>
          <td>${rw != null ? rw : "—"}</td>
          <td>${t.target_solves ?? "—"}</td>
          <td>${pct}</td>
          <td>${t.prepaid_hmc != null ? t.prepaid_hmc : "—"}</td>
        </tr>`;
      })
      .join("");
    setStatus(st, tasks.length + " order(s). Click a row for manifest.", true);
    body.querySelectorAll(".order-row").forEach((row) => {
      row.addEventListener("click", () => {
        const id = row.getAttribute("data-order-id");
        const t = tasks.find((x) => x.id === id);
        const pre = $("orders-manifest-pre");
        if (!pre || !t) return;
        let man = t.manifest_json;
        if (man && typeof man === "object") man = JSON.stringify(man, null, 2);
        else if (man) {
          try {
            man = JSON.stringify(JSON.parse(man), null, 2);
          } catch (_) {
            /* keep raw */
          }
        } else man = "(no manifest in public summary — save token and refresh)";
        pre.textContent = man;
      });
    });
  }

  async function submitOrder() {
    const st = $("create-status");
    if (!Dev.getToken()) {
      setStatus(st, "Save developer token first.", false);
      return;
    }
    if (!wasmHex || !wasmArtifactHash) {
      setStatus(st, "Upload a .wasm file before submit.", false);
      return;
    }
    const manifest = buildManifestObject();
    if (!manifest.id) {
      setStatus(st, "Order ID required.", false);
      return;
    }
    setStatus(st, "Submitting…");
    const r = await Dev.api("POST", "/api/tasks", manifest);
    if (!r.ok) {
      const err = r.json.error || r.json.code || r.text.slice(0, 200);
      setStatus(st, "HTTP " + r.status + ": " + err, false);
      return;
    }
    setStatus(
      st,
      "Created · id=" +
        (r.json.id || manifest.id) +
        (r.json.balance_after != null ? " · balance_after=" + r.json.balance_after : ""),
      true
    );
    await refreshOrders();
    await refreshWallet();
  }

  async function openReport(format) {
    const st = $("report-status");
    const cid = String($("report-campaign-id")?.value || "").trim();
    const tok = String($("report-token")?.value || "").trim();
    if (!cid || !tok) {
      setStatus(st, "Campaign ID and report token required.", false);
      return;
    }
    const q = format === "json" ? "?format=json" : "";
    const path = "/api/fuzz/campaigns/" + encodeURIComponent(cid) + "/report" + q;
    setStatus(st, "Loading report…");
    try {
      const r = await fetch(Dev.base + path, {
        headers: { "X-Hackme-Report-Token": tok },
        cache: "no-store",
      });
      if (!r.ok) {
        const t = await r.text();
        setStatus(st, "HTTP " + r.status + ": " + t.slice(0, 180), false);
        return;
      }
      const blob = await r.blob();
      if (format === "json") {
        const a = document.createElement("a");
        a.href = URL.createObjectURL(blob);
        a.download = cid + "-report.json";
        a.click();
        URL.revokeObjectURL(a.href);
        setStatus(st, "JSON downloaded.", true);
        return;
      }
      const html = await blob.text();
      const url = URL.createObjectURL(new Blob([html], { type: "text/html;charset=utf-8" }));
      window.open(url, "_blank", "noopener,noreferrer");
      setTimeout(() => URL.revokeObjectURL(url), 60000);
      setStatus(st, "HTML report opened in new tab.", true);
    } catch (e) {
      setStatus(st, String(e && e.message ? e.message : e), false);
    }
  }

  function wireTabs() {
    document.querySelectorAll(".dev-tab").forEach((btn) => {
      btn.addEventListener("click", () => {
        const tab = btn.getAttribute("data-dev-tab");
        document.querySelectorAll(".dev-tab").forEach((b) => b.classList.toggle("active", b === btn));
        document.querySelectorAll(".dev-panel").forEach((p) => {
          p.classList.toggle("active", p.id === "dev-panel-" + tab);
        });
      });
    });
  }

  function syncTokenInput() {
    const inp = $("dev-token-input");
    if (inp && !inp.value) inp.value = Dev.getToken();
    const st = $("dev-token-status");
    if (Dev.getToken()) {
      setStatus(st, "Token in session (" + Dev.getToken().slice(0, 8) + "…).", true);
    } else {
      setStatus(st, "No token — issue one on developers.html.", false);
    }
  }

  $("btn-token-save")?.addEventListener("click", () => {
    Dev.setToken($("dev-token-input")?.value || "");
    syncTokenInput();
    void refreshOrders();
  });
  $("btn-token-clear")?.addEventListener("click", () => {
    Dev.setToken("");
    if ($("dev-token-input")) $("dev-token-input").value = "";
    syncTokenInput();
  });
  $("btn-console-refresh")?.addEventListener("click", () => {
    void refreshWallet();
    void refreshOrders();
  });
  $("btn-orders-refresh")?.addEventListener("click", () => void refreshOrders());
  $("create-wasm-file")?.addEventListener("change", (e) => {
    const f = e.target.files && e.target.files[0];
    void loadWasmFile(f);
  });
  ["create-id", "create-kind", "create-diff", "create-reward", "create-target", "create-payer"].forEach((id) => {
    $(id)?.addEventListener("input", refreshManifestPreview);
  });
  $("btn-template-small")?.addEventListener("click", () => applyTemplate("small"));
  $("btn-template-medium")?.addEventListener("click", () => applyTemplate("medium"));
  $("btn-template-audit")?.addEventListener("click", () => applyTemplate("audit"));
  $("btn-fair-fill")?.addEventListener("click", () => {
    $("create-reward").value = String(minFairReward().toFixed(6));
    refreshManifestPreview();
  });
  $("btn-create-submit")?.addEventListener("click", () => void submitOrder());
  $("btn-report-open")?.addEventListener("click", () => void openReport("html"));
  $("btn-report-json")?.addEventListener("click", () => void openReport("json"));

  wireTabs();
  syncTokenInput();
  refreshManifestPreview();
  void refreshWallet();

  const params = new URLSearchParams(location.search);
  if (params.get("token")) {
    Dev.setToken(params.get("token"));
    if ($("dev-token-input")) $("dev-token-input").value = Dev.getToken();
    syncTokenInput();
  }
})();
