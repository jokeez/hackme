/* Shared helpers for developers.html and developer-console.html (session token only). */
(function () {
  const TOKEN_KEY = "hackme_developer_token";
  const base = location.origin;

  function getToken() {
    return sessionStorage.getItem(TOKEN_KEY) || "";
  }

  function setToken(tok) {
    const t = String(tok || "").trim();
    if (t) sessionStorage.setItem(TOKEN_KEY, t);
    else sessionStorage.removeItem(TOKEN_KEY);
  }

  async function api(method, path, body, extraHeaders) {
    const h = Object.assign({ "Content-Type": "application/json" }, extraHeaders || {});
    const tok = getToken();
    if (tok) h["X-Hackme-Developer-Token"] = tok;
    const r = await fetch(base + path, {
      method,
      headers: h,
      body: body != null ? JSON.stringify(body) : undefined,
      cache: "no-store",
    });
    const t = await r.text();
    let j = {};
    try {
      j = JSON.parse(t);
    } catch (_) {
      j = { error: t.slice(0, 400) };
    }
    return { ok: r.ok, status: r.status, json: j, text: t };
  }

  function developerConsoleURL() {
    const h = location.hostname;
    if (h === "hackme.tech" || h === "www.hackme.tech" || h === "localhost" || h === "127.0.0.1") {
      return "./developer-console.html";
    }
    return "https://hackme.tech/developer-console.html";
  }

  function escapeHtml(s) {
    return String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  async function sha256Hex(buf) {
    const hash = await crypto.subtle.digest("SHA-256", buf);
    return Array.from(new Uint8Array(hash))
      .map((b) => b.toString(16).padStart(2, "0"))
      .join("");
  }

  function bytesToHex(bytes) {
    return Array.from(bytes)
      .map((b) => b.toString(16).padStart(2, "0"))
      .join("");
  }

  window.HackmeDev = {
    TOKEN_KEY,
    base,
    getToken,
    setToken,
    api,
    developerConsoleURL,
    escapeHtml,
    sha256Hex,
    bytesToHex,
  };
})();
