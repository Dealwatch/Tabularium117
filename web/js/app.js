// app.js -- entry point: status bar, theme/language toggles, sidebar and
// hash router. The only module-level mutable state lives in `store` below.

import { i18n } from "./i18n.js";
import { api, ApiError } from "./api.js";
import { LiveConnection } from "./live.js";
import { renderIslandDetail } from "./views/islands.js";
import { renderHistory } from "./views/history.js";
import { renderEfficiency } from "./views/efficiency.js";
import { renderHelp } from "./views/help.js";
import { renderPhone } from "./views/phone.js";
import { renderAlerts } from "./views/alerts.js";
import { buildPipeHelp } from "./pipe-help.js";
import { alertKey, announce, countByIsland } from "./alerts.js";
import { formatAge } from "./format.js";

const THEME_KEY = "tabularium.theme";

// store is the one shared, mutable piece of state the views read. Nothing
// else keeps its own copy of server data across renders.
export const store = {
  status: null,
  islands: [], // islandDTO[] from /api/v1/islands, kept fresh by SSE snapshots
  alerts: [], // active alertDTO[], newest first, kept fresh by SSE alert events
};

// pageStart is when this page began listening. A warning raised before it is
// catch-up, not news, so it must not set off a notification or a sound on
// every reload (see applyAlertEvent).
const pageStart = Date.now();

const els = {
  connState: document.getElementById("conn-state"),
  protocolVersion: document.getElementById("protocol-version"),
  sessionHeadline: document.getElementById("session-headline"),
  lastFrame: document.getElementById("last-frame"),
  langToggle: document.getElementById("lang-toggle"),
  themeToggle: document.getElementById("theme-toggle"),
  islandList: document.getElementById("island-list"),
  view: document.getElementById("view"),
  alertBadge: document.getElementById("alert-badge"),
  alertCount: document.getElementById("alert-count"),
  phoneLink: document.getElementById("phone-link"),
  lanIndicator: document.getElementById("lan-indicator"),
  lanIndicatorText: document.getElementById("lan-indicator-text"),
  warmupHint: document.getElementById("warmup-hint"),
};

let currentUnmount = null;
let lastFrameAt = null;

function applyTheme(theme) {
  if (theme === "dark" || theme === "light") {
    document.documentElement.setAttribute("data-theme", theme);
  } else {
    document.documentElement.removeAttribute("data-theme");
  }
}

function initTheme() {
  let stored = null;
  try {
    stored = localStorage.getItem(THEME_KEY);
  } catch {
    // No storage available; default to following the system.
  }
  applyTheme(stored);
}

function toggleTheme() {
  const current = document.documentElement.getAttribute("data-theme")
    || (window.matchMedia?.("(prefers-color-scheme: dark)").matches ? "dark" : "light");
  const next = current === "dark" ? "light" : "dark";
  applyTheme(next);
  try {
    localStorage.setItem(THEME_KEY, next);
  } catch {
    // Ignore: the choice just does not survive a reload.
  }
}

function connectionPillClass(state) {
  switch (state) {
    case "connected": return "connected";
    case "waiting": return "waiting";
    case "replaying": return "replaying";
    default: return "disconnected";
  }
}

function connectionLabel(state) {
  switch (state) {
    case "connected": return i18n.t("statusConnected");
    case "waiting": return i18n.t("statusWaiting");
    case "replaying": return i18n.t("statusReplaying");
    default: return i18n.t("statusDisconnected");
  }
}

// renderWarmup shows the one line that explains an empty picture: right
// after a savegame is loaded the game sends every island with no products at
// all, and the real numbers only arrive with the next statistics tick, up to
// two minutes later (docs/protocol.md). Without this the UI claims for two
// minutes that nothing is being produced anywhere.
//
// The tick's own timestamp is deliberately not shown: it is the game clock in
// milliseconds, which means nothing to a player. "Last frame X ago" stays.
function renderWarmup(status) {
  const warming = !!status.warmingUp;
  els.warmupHint.hidden = !warming;
  els.warmupHint.textContent = warming ? i18n.t("warmingUp") : "";
}

function renderStatusBar(status) {
  const wasWarming = store.status ? !!store.status.warmingUp : null;
  store.status = status;
  const conn = status.connection || {};
  els.connState.textContent = connectionLabel(conn.state);
  els.connState.className = `pill ${connectionPillClass(conn.state)}`;
  els.protocolVersion.textContent = conn.protocolVersion
    ? i18n.t("protocolVersion", { version: conn.protocolVersion })
    : "";
  els.sessionHeadline.textContent = status.session?.headline || "";
  renderWarmup(status);
  // The island list greys out while warming up, so it has to be repainted
  // when that changes - the snapshots themselves may not change at all.
  if (wasWarming !== !!status.warmingUp) renderIslandList();
  renderLAN(status.lan || {});
  lastFrameAt = conn.lastFrameAt ? new Date(conn.lastFrameAt) : null;
  tickLastFrame();
  // Views that care about the status (the phone panel) listen for this
  // rather than fetching it again.
  document.dispatchEvent(new CustomEvent("tabularium-status", { detail: status }));
}

// renderLAN decides what this browser gets to see of LAN mode. lan.local is
// the server's answer to "did this request arrive on the loopback listener":
// the PC sees the panel with the switch, the QR code and the address, a
// phone sees only a small indicator that it is connected over the network
// (KONZEPT.md section 6).
function renderLAN(lan) {
  const local = !!lan.local;
  els.phoneLink.hidden = !local;
  els.lanIndicator.hidden = local;
  els.lanIndicatorText.textContent = i18n.t("phoneIndicator");
  els.lanIndicator.title = i18n.t("phoneIndicatorTitle");
  if (!local && window.location.hash.startsWith("#/phone")) {
    // A phone that guessed the address gets the overview instead; the panel
    // would be a switch it is not allowed to operate.
    window.location.hash = "";
  }
}

function tickLastFrame() {
  if (!lastFrameAt) {
    els.lastFrame.textContent = i18n.t("lastFrameNever");
    return;
  }
  els.lastFrame.textContent = i18n.t("lastFrame", { age: formatAge(Date.now() - lastFrameAt.getTime()) });
}
setInterval(tickLastFrame, 1000);

// --- warnings ---

function sortAlerts() {
  store.alerts.sort((a, b) => new Date(b.raisedAt) - new Date(a.raisedAt));
}

function renderAlertBadge() {
  const count = store.alerts.length;
  els.alertBadge.hidden = count === 0;
  els.alertCount.textContent = String(count);
  els.alertBadge.title = i18n.t("alertsBadge", { count });
  els.alertBadge.setAttribute("aria-label", i18n.t("alertsBadge", { count }));
}

// alertsChanged repaints everything that shows warnings and lets the mounted
// view know, so that a table can mark its rows without polling.
function alertsChanged() {
  renderAlertBadge();
  renderIslandList();
  document.dispatchEvent(new CustomEvent("tabularium-alerts-changed"));
}

function applyAlertEvent(event) {
  if (!event || !event.alert) return;
  const key = alertKey(event.alert);
  const index = store.alerts.findIndex((a) => alertKey(a) === key);

  if (event.kind === "raised") {
    if (index >= 0) {
      store.alerts[index] = event.alert;
    } else {
      store.alerts.push(event.alert);
      // Only a warning that started after this page did is news. The stream
      // replays the open ones on every (re)connect, and announcing those
      // would mean a burst of notifications after every reload.
      if (new Date(event.alert.raisedAt).getTime() >= pageStart) announce(event.alert);
    }
  } else if (index >= 0) {
    store.alerts.splice(index, 1);
  }
  sortAlerts();
  alertsChanged();
}

async function refreshAlerts() {
  try {
    store.alerts = await api.alerts(true);
    sortAlerts();
    alertsChanged();
  } catch (err) {
    console.error("tabularium117: cannot load the warnings", err);
  }
}

// upsertIsland is the fast path: a snapshot event adds or replaces exactly
// one island. It is deliberately the only thing the stream can do to the
// list, because a snapshot says "this island is now like this" and never
// "that island is gone" - removals arrive through resyncIslands.
function upsertIsland(island) {
  const idx = store.islands.findIndex((i) => i.id === island.id);
  if (idx >= 0) store.islands[idx] = island;
  else store.islands.push(island);
  store.islands.sort((a, b) => (a.sessionGuid - b.sessionGuid) || (a.islandId - b.islandId));
}

function renderIslandList() {
  const hash = parseHash();
  const alertCounts = countByIsland(store.alerts);
  const warming = !!store.status?.warmingUp;
  els.islandList.replaceChildren();
  for (const island of store.islands) {
    const li = document.createElement("li");
    li.className = warming ? "island-item warming" : "island-item";
    if (warming) li.title = i18n.t("warmingUp");
    const btn = document.createElement("button");
    btn.type = "button";
    if (hash.islandId === island.id) btn.setAttribute("aria-current", "true");

    const left = document.createElement("span");
    const name = document.createElement("div");
    name.className = "name";
    name.textContent = island.name || `#${island.islandId}`;
    const meta = document.createElement("div");
    meta.className = "meta";
    meta.textContent = warming
      ? `${island.sessionName} · ${i18n.t("warmingUpShort")}`
      : `${island.sessionName} · ${island.products} ${i18n.t("products")}`;
    left.append(name, meta);

    btn.append(left);
    const badges = document.createElement("span");
    badges.className = "badges";
    const alertCount = alertCounts.get(island.id) || 0;
    if (alertCount > 0) {
      // Two different things, so two different badges: deficits are "delta
      // is negative right now", warnings are "a rule has held for a while".
      const badge = document.createElement("span");
      badge.className = "badge alert";
      badge.textContent = `\u26A0 ${alertCount}`;
      badge.title = i18n.t("alertsBadge", { count: alertCount });
      badges.append(badge);
    }
    if (island.deficits > 0) {
      const badge = document.createElement("span");
      badge.className = "badge";
      badge.textContent = String(island.deficits);
      badge.title = `${island.deficits} ${i18n.t("deficits")}`;
      badges.append(badge);
    }
    if (badges.childElementCount > 0) btn.append(badges);
    btn.addEventListener("click", () => {
      window.location.hash = `#/island/${encodeURIComponent(island.id)}`;
    });
    li.append(btn);
    els.islandList.append(li);
  }
}

// resyncing holds the resync that is running, so that the status events of
// one statistics tick cannot start a request each. A resync asked for while
// one is in flight is remembered and served by one more round, because the
// answer already on its way may predate what asked for it.
let resyncing = null;
let resyncAgain = false;

// resyncIslands replaces the island list with the server's, rather than
// merging into it. Merging is what left the islands of a finished session on
// screen: the stream only ever adds and updates islands, so an island the
// server has dropped - at a session boundary, or while this page was away -
// has no event that would remove it.
function resyncIslands() {
  if (resyncing) {
    resyncAgain = true;
    return resyncing;
  }
  resyncing = (async () => {
    try {
      do {
        resyncAgain = false;
        store.islands = await api.islands();
      } while (resyncAgain);
      renderIslandList();
      renderHomeIfShown();
      dropSelectionIfGone();
    } catch (err) {
      console.error("tabularium117: cannot load islands", err);
    } finally {
      resyncing = null;
    }
  })();
  return resyncing;
}

// dropSelectionIfGone leaves a view whose island no longer exists. Without it
// the detail, history and efficiency views keep showing the numbers of a
// session that has ended, and every request they make answers 404.
function dropSelectionIfGone() {
  const { islandId } = parseHash();
  if (!islandId) return;
  if (store.islands.some((island) => island.id === islandId)) return;
  window.location.hash = "#/";
}

// statusNeedsResync decides when the island list has to be fetched again
// instead of trusted. The stream cannot express a removal, so these are the
// three moments at which the server's list may be shorter than, or entirely
// different from, the one on screen:
//
//   - the count disagrees with what is on screen;
//   - the session changed, which resets the server's islands;
//   - the connection came or went, so events may have been missed.
function statusNeedsResync(previous, status) {
  if ((status.islands || 0) !== store.islands.length) return true;
  const before = previous?.session || {};
  const now = status.session || {};
  if (before.headline !== now.headline || before.startedAt !== now.startedAt) return true;
  return (previous?.connection?.state === "connected") !== (status.connection?.state === "connected");
}

// --- routing ---

function parseHash() {
  const raw = window.location.hash.replace(/^#\/?/, "");
  const [path, search] = raw.split("?");
  const query = new URLSearchParams(search || "");
  const parts = path.split("/").filter(Boolean);
  if (parts[0] === "help") return { route: "help" };
  if (parts[0] === "phone") return { route: "phone" };
  if (parts[0] === "alerts") return { route: "alerts", all: query.get("all") === "1" };
  if (parts[0] === "island" && parts[1]) {
    const islandId = decodeURIComponent(parts[1]);
    if (parts[2] === "efficiency") return { route: "efficiency", islandId };
    if (parts[2] === "product" && parts[3]) {
      return { route: "history", islandId, guid: decodeURIComponent(parts[3]) };
    }
    return { route: "island", islandId };
  }
  return { route: "home" };
}

function showErrorLine(container, message) {
  container.replaceChildren();
  const p = document.createElement("p");
  p.className = "error-line";
  p.textContent = `${i18n.t("errorPrefix")} ${message}`;
  container.append(p);
}

async function route() {
  if (typeof currentUnmount === "function") {
    try { currentUnmount(); } catch { /* ignore */ }
  }
  currentUnmount = null;

  const r = parseHash();
  renderIslandList();
  els.view.replaceChildren();

  try {
    switch (r.route) {
      case "help":
        currentUnmount = renderHelp(els.view);
        break;
      case "phone":
        currentUnmount = await renderPhone(els.view, store);
        break;
      case "alerts":
        currentUnmount = await renderAlerts(els.view, store, r.all);
        break;
      case "island":
        currentUnmount = await renderIslandDetail(els.view, r.islandId, store);
        break;
      case "history":
        currentUnmount = await renderHistory(els.view, r.islandId, r.guid, store);
        break;
      case "efficiency":
        currentUnmount = await renderEfficiency(els.view, r.islandId, store);
        break;
      default:
        renderHome(els.view);
    }
  } catch (err) {
    if (err instanceof ApiError) {
      showErrorLine(els.view, err.message);
    } else {
      console.error("tabularium117: routing failure", err);
      showErrorLine(els.view, String(err));
    }
  }
}

function renderHome(container) {
  const status = store.status;
  const noIslandsYet = store.islands.length === 0;
  const notConnected = !status || (status.connection && ["waiting", "disconnected"].includes(status.connection.state));

  if (noIslandsYet && notConnected) {
    const panel = document.createElement("div");
    panel.className = "panel help-panel";
    panel.append(buildPipeHelp());
    container.append(panel);
    return;
  }

  const p = document.createElement("p");
  p.className = "muted";
  p.textContent = i18n.t("selectIsland");
  container.append(p);
}

// renderHomeIfShown repaints the start page when it is the one on screen.
// It is the page whose content depends on the island list without being the
// island list: with nothing connected and nothing known it explains how to
// start the game, and otherwise it asks for an island to be picked.
function renderHomeIfShown() {
  if (parseHash().route !== "home") return;
  els.view.replaceChildren();
  renderHome(els.view);
}

window.addEventListener("hashchange", route);

// --- wiring ---

els.themeToggle.addEventListener("click", toggleTheme);
els.langToggle.addEventListener("click", () => i18n.toggle());
els.alertBadge.addEventListener("click", () => {
  window.location.hash = "#/alerts";
});
document.addEventListener("tabularium-lang-changed", () => {
  if (store.status) renderStatusBar(store.status);
  renderAlertBadge();
  route();
});

initTheme();
i18n.apply();

new LiveConnection({
  onStatus: (status) => {
    // The comparison has to happen before renderStatusBar, which is what
    // replaces store.status with the new one.
    const stale = statusNeedsResync(store.status, status);
    renderStatusBar(status);
    renderHomeIfShown();
    if (stale) resyncIslands();
  },
  onSnapshot: (island) => {
    upsertIsland(island);
    renderIslandList();
    document.dispatchEvent(new CustomEvent("tabularium-snapshot", { detail: island }));
  },
  onAlert: applyAlertEvent,
  onOpen: async () => {
    // A (re)connect is where this page and the server disagree most: the
    // stream replays the current status, every island and every open
    // warning, but it has no way of saying which islands and which warnings
    // have gone in the meantime. So the whole picture is fetched again, in
    // the order the views need it - status first, because the island list is
    // drawn differently while warming up, then the islands, then the
    // warnings, which point at islands. The catch-up snapshots that follow
    // only refresh what is already there.
    try {
      renderStatusBar(await api.status());
    } catch (err) {
      console.error("tabularium117: status", err);
    }
    await resyncIslands();
    await refreshAlerts();
  },
  onConnectionChange: (connected) => {
    if (!connected) {
      els.connState.textContent = i18n.t("connectionLost");
      els.connState.className = "pill disconnected";
    }
  },
});

// The stream delivers status and every island on connect, but the initial
// paint is faster from a plain fetch, and it is what still works if the
// stream is briefly unavailable.
api.status().then(renderStatusBar).catch((err) => console.error("tabularium117: status", err));
refreshAlerts();
resyncIslands().then(route);
route();
