// views/phone.js -- the "#/phone" panel: the LAN switch,
// the QR code, the address with the access token, and the Windows firewall
// hint.
//
// This view exists on the PC only. The server answers /api/v1/lan without the
// URL when the request came over the LAN listener, and app.js hides the entry
// point there (status.lan.local), so a phone never sees a switch it could not
// operate anyway.

import { i18n } from "../i18n.js";
import { api, ApiError } from "../api.js";

// qrSource adds a cache-busting parameter. The image is served with
// Cache-Control: no-store, but a browser that re-uses the element's old src
// would show the QR code of a previous LAN session.
function qrSource() {
  return `/api/v1/lan/qr.png?t=${Date.now()}`;
}

export async function renderPhone(container, store) {
  const heading = document.createElement("h2");
  heading.textContent = i18n.t("phoneHeading");

  const intro = document.createElement("p");
  intro.className = "muted";
  intro.textContent = i18n.t("phoneIntro");

  const panel = document.createElement("div");
  panel.className = "panel";

  // --- the switch ---
  const toggleLabel = document.createElement("label");
  toggleLabel.className = "setting";
  const toggle = document.createElement("input");
  toggle.type = "checkbox";
  toggle.id = "lan-toggle";
  const toggleText = document.createElement("span");
  toggleText.textContent = i18n.t("phoneToggle");
  toggleLabel.append(toggle, toggleText);

  const stateLine = document.createElement("p");
  stateLine.id = "lan-state";
  stateLine.className = "muted";

  const errorLine = document.createElement("p");
  errorLine.className = "error-line";
  errorLine.hidden = true;

  // --- the code and the address ---
  const share = document.createElement("div");
  share.id = "lan-share";
  share.hidden = true;

  const scanLine = document.createElement("p");
  scanLine.textContent = i18n.t("phoneScan");
  const qr = document.createElement("img");
  qr.id = "lan-qr";
  qr.className = "lan-qr";
  qr.width = 256;
  qr.height = 256;
  qr.alt = i18n.t("phoneQrAlt");

  const urlLine = document.createElement("p");
  urlLine.textContent = i18n.t("phoneUrl");
  const urlRow = document.createElement("div");
  urlRow.className = "lan-url-row";
  const urlText = document.createElement("code");
  urlText.id = "lan-url";
  urlText.className = "lan-url";
  const copy = document.createElement("button");
  copy.type = "button";
  copy.id = "lan-copy";
  copy.textContent = i18n.t("phoneCopy");
  urlRow.append(urlText, copy);

  const tokenNote = document.createElement("p");
  tokenNote.className = "muted";
  tokenNote.textContent = i18n.t("phoneTokenNote");

  share.append(scanLine, qr, urlLine, urlRow, tokenNote);

  // --- the firewall hint, which is the part users trip over ---
  const firewall = document.createElement("p");
  firewall.className = "muted";
  firewall.textContent = i18n.t("phoneFirewall");

  panel.append(toggleLabel, stateLine, errorLine, share, firewall);
  container.append(heading, intro, panel);

  function showError(message) {
    errorLine.hidden = !message;
    errorLine.textContent = message ? `${i18n.t("phoneError")} ${message}` : "";
  }

  // render paints one LAN state (the DTO of KONZEPT.md section 5).
  function render(state) {
    toggle.checked = !!state.enabled;
    // Unavailable means there is nothing to bind; the switch would fail on
    // every click, so it is disabled and the reason is shown instead.
    toggle.disabled = !state.enabled && !state.available;
    if (state.enabled) {
      const where = state.interface ? `${state.ip} (${state.interface})` : state.ip;
      stateLine.textContent = `${i18n.t("phoneOn")} ${where}:${state.port}`;
    } else if (state.available) {
      stateLine.textContent = i18n.t("phoneOff");
    } else {
      stateLine.textContent = `${i18n.t("phoneUnavailable")} ${state.reason || ""}`.trim();
    }

    const url = state.enabled ? (state.url || "") : "";
    share.hidden = !url;
    if (url) {
      urlText.textContent = url;
      qr.src = qrSource();
    } else {
      urlText.textContent = "";
      // Drop the image rather than leave a stale code on screen.
      qr.removeAttribute("src");
    }
  }

  toggle.addEventListener("change", async () => {
    const wanted = toggle.checked;
    toggle.disabled = true;
    showError("");
    try {
      render(await api.setLan(wanted));
    } catch (err) {
      // The checkbox follows what the server did, not what was clicked.
      showError(err instanceof ApiError ? err.message : String(err));
      try {
        render(await api.lan());
      } catch {
        toggle.checked = !wanted;
      }
    } finally {
      toggle.disabled = false;
    }
  });

  copy.addEventListener("click", async () => {
    const text = urlText.textContent;
    if (!text) return;
    try {
      await navigator.clipboard.writeText(text);
      copy.textContent = i18n.t("phoneCopied");
      setTimeout(() => { copy.textContent = i18n.t("phoneCopy"); }, 2000);
    } catch {
      // No clipboard permission: select the address so it can be copied by
      // hand instead of failing silently.
      const range = document.createRange();
      range.selectNodeContents(urlText);
      const selection = window.getSelection();
      selection.removeAllRanges();
      selection.addRange(range);
    }
  });

  try {
    render(await api.lan());
  } catch (err) {
    showError(err instanceof ApiError ? err.message : String(err));
  }

  // The status event carries lan.enabled, so a switch flipped in another tab
  // (or by --lan at start) is picked up without polling.
  const onStatus = () => {
    const enabled = !!store.status?.lan?.enabled;
    if (enabled === toggle.checked) return;
    api.lan().then(render).catch(() => { /* the panel keeps what it shows */ });
  };
  document.addEventListener("tabularium-status", onStatus);

  // A language change is handled by the router, which unmounts this view
  // and renders it again; there is nothing to translate in place here.
  return () => document.removeEventListener("tabularium-status", onStatus);
}
