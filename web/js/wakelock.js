// wakelock.js -- the opt-in "keep the screen on" of task T7.4.
//
// A phone used as a second screen turns itself off after a minute, which is
// exactly wrong for a dashboard. The Screen Wake Lock API fixes that, but the
// lock is released by the browser whenever the page is hidden, so it has to
// be taken again when the page comes back. The choice is remembered in this
// browser only - like the notification opt-ins, nothing about it is sent
// anywhere.

const STORAGE_KEY = "tabularium.wakelock";

function readFlag() {
  try {
    return localStorage.getItem(STORAGE_KEY) === "on";
  } catch {
    // No storage (private browsing): the opt-in stays off, which is the
    // default anyway.
    return false;
  }
}

function writeFlag(on) {
  try {
    localStorage.setItem(STORAGE_KEY, on ? "on" : "off");
  } catch {
    // Ignore: the choice just does not survive a reload.
  }
}

export const wakeLock = {
  enabled: readFlag(),
  _sentinel: null,

  // supported reports whether this browser has the API at all. Desktop
  // Firefox and older iOS do not, and the UI says so instead of offering a
  // switch that does nothing.
  supported() {
    return typeof navigator !== "undefined" && "wakeLock" in navigator;
  },

  // setEnabled turns the lock on or off and reports the state that was
  // actually reached: a browser may refuse the request (a hidden page, a
  // battery saver), and the checkbox has to follow reality.
  async setEnabled(on) {
    if (!on) {
      this.enabled = false;
      writeFlag(false);
      await this._release();
      return false;
    }
    if (!this.supported()) return false;
    const ok = await this._acquire();
    this.enabled = ok;
    writeFlag(ok);
    return ok;
  },

  async _acquire() {
    if (this._sentinel) return true;
    try {
      const sentinel = await navigator.wakeLock.request("screen");
      this._sentinel = sentinel;
      // The browser releases the lock on its own when the page is hidden;
      // forgetting the sentinel here is what lets us take it again later.
      sentinel.addEventListener("release", () => {
        if (this._sentinel === sentinel) this._sentinel = null;
      });
      return true;
    } catch (err) {
      console.warn("tabularium117: the screen wake lock was refused", err);
      this._sentinel = null;
      return false;
    }
  },

  async _release() {
    const sentinel = this._sentinel;
    this._sentinel = null;
    if (!sentinel) return;
    try {
      await sentinel.release();
    } catch {
      // Already gone; nothing to do.
    }
  },

  // restore takes the lock again after the page was hidden, if the user
  // asked for it. It is safe to call at any time.
  async restore() {
    if (!this.enabled || !this.supported()) return;
    if (document.visibilityState !== "visible") return;
    await this._acquire();
  },
};

document.addEventListener("visibilitychange", () => {
  wakeLock.restore();
});

// A page that was loaded with the opt-in already on takes the lock right
// away, without waiting for the user to visit the settings again.
wakeLock.restore();
