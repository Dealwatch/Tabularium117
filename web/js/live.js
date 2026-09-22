// live.js -- the SSE connection to /api/v1/events, with reconnect and state.
//
// The browser's EventSource already reconnects on its own after a dropped
// connection, but it does not tell listeners when that happens, and it does
// not restart when the language changes (the stream's names are localised
// server-side, per §5). This wraps it and re-opens on language change.

import { i18n } from "./i18n.js";

const RECONNECT_DELAY_MS = 2000;

export class LiveConnection {
  // onStatus(statusDTO), onSnapshot(islandDTO), onAlert({kind, alert}),
  // onConnectionChange(bool connected), onOpen()
  constructor({ onStatus, onSnapshot, onAlert, onConnectionChange, onOpen }) {
    this.onStatus = onStatus;
    this.onSnapshot = onSnapshot;
    this.onAlert = onAlert;
    this.onConnectionChange = onConnectionChange;
    this.onOpen = onOpen;
    this.source = null;
    this.reconnectTimer = null;
    this._open();
    document.addEventListener("tabularium-lang-changed", () => this._reopen());
  }

  _open() {
    const url = `/api/v1/events?lang=${encodeURIComponent(i18n.apiLang())}`;
    const source = new EventSource(url);
    this.source = source;

    source.addEventListener("status", (e) => {
      this.onConnectionChange?.(true);
      try {
        this.onStatus?.(JSON.parse(e.data));
      } catch (err) {
        console.error("tabularium117: bad status event", err);
      }
    });

    source.addEventListener("snapshot", (e) => {
      try {
        this.onSnapshot?.(JSON.parse(e.data));
      } catch (err) {
        console.error("tabularium117: bad snapshot event", err);
      }
    });

    source.addEventListener("alert", (e) => {
      try {
        this.onAlert?.(JSON.parse(e.data));
      } catch (err) {
        console.error("tabularium117: bad alert event", err);
      }
    });

    // The stream replays the current status, every island and every open
    // warning on connect, but a client that was away may also have missed a
    // warning being cleared. onOpen is where the page refetches.
    source.addEventListener("open", () => {
      this.onConnectionChange?.(true);
      this.onOpen?.();
    });

    source.addEventListener("error", () => {
      this.onConnectionChange?.(false);
      // The browser's own reconnect logic handles the retry; this only
      // reports the outage to the UI.
    });
  }

  _reopen() {
    if (this.source) this.source.close();
    this._open();
  }

  close() {
    if (this.source) this.source.close();
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
  }
}

// Kept for documentation: EventSource's built-in retry uses roughly this
// delay unless the server sends a "retry:" field, which Tabularium 117 does not.
export const _reconnectDelayMs = RECONNECT_DELAY_MS;
