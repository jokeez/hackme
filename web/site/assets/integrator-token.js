/* Self-service integrator token on developers.html — session only, never sent to static host. */
(function () {
  const SK = "hackme_developer_token";
  const base = location.origin;
  const out = document.getElementById("integrator-token-out");
  const status = document.getElementById("integrator-status");
  const btnReg = document.getElementById("btn-integrator-register");
  const btnRot = document.getElementById("btn-integrator-rotate");
  const labelIn = document.getElementById("integrator-label");

  function getTok() {
    return sessionStorage.getItem(SK) || "";
  }
  function setTok(t) {
    if (t) sessionStorage.setItem(SK, t);
    else sessionStorage.removeItem(SK);
    btnRot.disabled = !t;
  }

  async function api(method, path, body) {
    const h = { "Content-Type": "application/json" };
    const tok = getTok();
    if (tok) h["X-Hackme-Developer-Token"] = tok;
    const r = await fetch(base + path, {
      method,
      headers: h,
      body: body ? JSON.stringify(body) : undefined,
      cache: "no-store",
    });
    const t = await r.text();
    let j = {};
    try {
      j = JSON.parse(t);
    } catch (_) {
      j = { error: t.slice(0, 300) };
    }
    return { ok: r.ok, status: r.status, json: j };
  }

  function showToken(tok, msg) {
    if (!out) return;
    out.style.display = "block";
    out.textContent = tok + "\n\n" + (msg || "Copy now. Not stored on hackme.tech servers.");
  }

  async function refreshStatus() {
    const r = await api("GET", "/api/integrator/status");
    if (status && r.ok) {
      status.textContent =
        "Self-register: " +
        (r.json.self_register_enabled ? "on" : "off") +
        " · active tokens: " +
        (r.json.active_tokens ?? "—");
    }
  }

  btnReg?.addEventListener("click", async () => {
    if (status) status.textContent = "Registering…";
    const label = (labelIn?.value || "").trim();
    const r = await api("POST", "/api/integrator/register", { label });
    if (!r.ok) {
      if (status) status.textContent = "Error " + r.status + ": " + (r.json.error || r.json.code || "");
      return;
    }
    const tok = r.json.developer_token;
    if (tok) setTok(tok);
    showToken(tok, r.json.warning);
    if (status) status.textContent = "Token issued (sessionStorage). Use hackme-fuzzing rotate to replace.";
  });

  btnRot?.addEventListener("click", async () => {
    if (!getTok()) return;
    if (status) status.textContent = "Rotating…";
    const r = await api("POST", "/api/integrator/rotate");
    if (!r.ok) {
      if (status) status.textContent = "Rotate failed: " + (r.json.error || r.status);
      return;
    }
    const tok = r.json.developer_token;
    setTok(tok);
    showToken(tok, r.json.warning);
    if (status) status.textContent = "Rotated — old token invalid.";
  });

  setTok(getTok());
  refreshStatus();
})();
