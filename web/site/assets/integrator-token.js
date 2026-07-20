/* Self-service integrator token on developers.html — session only, never sent to static host. */
(function () {
  const Dev = window.HackmeDev;
  const SK = Dev ? Dev.TOKEN_KEY : "hackme_developer_token";
  const base = location.origin;
  const out = document.getElementById("integrator-token-out");
  const status = document.getElementById("integrator-status");
  const btnReg = document.getElementById("btn-integrator-register");
  const btnRot = document.getElementById("btn-integrator-rotate");
  const labelIn = document.getElementById("integrator-label");
  const consoleLink = document.getElementById("integrator-console-link");
  let selfRegisterOn = false;

  function getTok() {
    return Dev ? Dev.getToken() : sessionStorage.getItem(SK) || "";
  }
  function setTok(t) {
    if (Dev) Dev.setToken(t);
    else if (t) sessionStorage.setItem(SK, t);
    else sessionStorage.removeItem(SK);
    btnRot.disabled = !t;
    updateConsoleLink();
  }

  function updateConsoleLink() {
    if (!consoleLink) return;
    const url = Dev ? Dev.developerConsoleURL() : "./downloads.html#local-node";
    consoleLink.href = url;
    consoleLink.style.display = getTok() ? "inline-flex" : "none";
  }

  function setRegisterEnabled(on) {
    selfRegisterOn = !!on;
    if (!btnReg) return;
    btnReg.disabled = !on;
    btnReg.title = on
      ? ""
      : "Self-register is off on hackme.tech — run hackme-node locally (127.0.0.1:8080) to issue tokens.";
    btnReg.classList.toggle("btn-disabled", !on);
    if (labelIn) labelIn.disabled = !on;
  }

  async function api(method, path, body) {
    if (Dev) return Dev.api(method, path, body);
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
    out.textContent =
      tok + "\n\n" + (msg || "Copy now. Not stored on hackme.tech servers.");
    updateConsoleLink();
  }

  async function refreshStatus() {
    const r = await api("GET", "/api/integrator/status");
    if (r.ok) {
      setRegisterEnabled(!!r.json.self_register_enabled);
      if (status) {
        status.textContent =
          "Self-register: " +
          (r.json.self_register_enabled ? "on" : "off (hub — use local node)") +
          " · active tokens: " +
          (r.json.active_tokens ?? "—");
      }
    } else if (status) {
      status.textContent = "Integrator status unavailable (" + r.status + ")";
      setRegisterEnabled(false);
    }
  }

  btnReg?.addEventListener("click", async () => {
    if (!selfRegisterOn) {
      if (status) {
        status.textContent =
          "Self-register off on this host. Start hackme-node locally → http://127.0.0.1:8080/#orders";
      }
      return;
    }
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
    if (status) {
      status.textContent =
        "Token issued (session). For orders: run hackme-node locally → http://127.0.0.1:8080/#orders (not hackme.tech).";
    }
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
