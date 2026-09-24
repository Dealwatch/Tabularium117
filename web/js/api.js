// api.js -- fetch wrappers for the /api/v1 REST endpoints (KONZEPT.md section 5).

import { i18n } from "./i18n.js";

// ApiError carries the HTTP status alongside the message, so callers can
// special-case 503 (history disabled) without parsing text.
export class ApiError extends Error {
  constructor(status, message) {
    super(message);
    this.status = status;
  }
}

function withLang(path) {
  const url = new URL(path, window.location.origin);
  url.searchParams.set("lang", i18n.apiLang());
  return url.pathname + url.search;
}

async function get(path) {
  const res = await fetch(withLang(path), { headers: { Accept: "application/json" } });
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`;
    try {
      const body = await res.json();
      if (body && body.error) message = body.error;
    } catch {
      // Body was not JSON; keep the status line.
    }
    throw new ApiError(res.status, message);
  }
  return res.json();
}

// post sends the one write this API has: the LAN switch. It is same-origin,
// so the browser attaches the Origin header the server checks (KONZEPT.md
// section 6).
async function post(path, body) {
  const res = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`;
    try {
      const parsed = await res.json();
      if (parsed && parsed.error) message = parsed.error;
    } catch {
      // Body was not JSON; keep the status line.
    }
    throw new ApiError(res.status, message);
  }
  return res.json();
}

export const api = {
  status: () => get("/api/v1/status"),
  // lan: the LAN state. The answer only carries the URL with the access
  // token when it is served to the PC itself.
  lan: () => get("/api/v1/lan"),
  // setLan is the only call in this file that changes anything. The server
  // accepts it from the loopback address alone.
  setLan: (enabled) => post("/api/v1/lan", { enabled: !!enabled }),
  // alerts: active=true is the live picture from the rule engine, false the
  // recorded history (which needs the database and answers 503 without it).
  // withInfo=false leaves the info alerts (no local production) out.
  alerts: (active = true, limit = 0, withInfo = true) => {
    const params = new URLSearchParams({ active: String(!!active) });
    if (limit > 0) params.set("limit", String(limit));
    if (!withInfo) params.set("info", "false");
    return get(`/api/v1/alerts?${params.toString()}`);
  },
  islands: () => get("/api/v1/islands"),
  products: (islandId) => get(`/api/v1/islands/${encodeURIComponent(islandId)}/products`),
  efficiency: (islandId) => get(`/api/v1/islands/${encodeURIComponent(islandId)}/efficiency`),
  history: (islandId, guid, range) =>
    get(
      `/api/v1/islands/${encodeURIComponent(islandId)}/products/${encodeURIComponent(guid)}/history?range=${encodeURIComponent(range)}`,
    ),
};
