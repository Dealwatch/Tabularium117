// alerts.js -- everything the UI shares about warnings: identity, the
// per-browser opt-ins, and how a new warning is announced.
//
// The list itself lives in app.js's store; this module only knows how to
// recognise, group and announce one alert.

import { i18n } from "./i18n.js";

const NOTIFY_KEY = "tabularium.notify";
const SOUND_KEY = "tabularium.sound";

// alertKey identifies one warning the way the rule engine does: island,
// product and rule. The same triple can only ever be open once.
export function alertKey(alert) {
  return `${alert.islandId}|${alert.productGuid}|${alert.rule}`;
}

// ruleLabel translates a rule name. An unknown rule is shown as it came -
// the same principle as an unresolved GUID: never swallow it.
export function ruleLabel(rule) {
  switch (rule) {
    case "deficit": return i18n.t("ruleDeficit");
    case "import": return i18n.t("ruleImport");
    case "productivity_drop": return i18n.t("ruleProductivityDrop");
    default: return rule;
  }
}

// isWarning tells a warning from an info alert. An import - a good the island
// consumes but has no building for - is info: it is listed, but it is not
// counted, badged, filtered for or announced as a problem. Anything that is
// not explicitly info counts as a warning, so an unknown severity is never
// quietly swallowed.
export function isWarning(alert) {
  return alert.severity !== "info";
}

// countByIsland returns a map of island id -> number of active warnings.
export function countByIsland(alerts) {
  const counts = new Map();
  for (const a of alerts) {
    if (!isWarning(a)) continue;
    counts.set(a.islandId, (counts.get(a.islandId) || 0) + 1);
  }
  return counts;
}

// byProduct returns a map of product GUID (as a string) -> the island's
// active warnings for that product.
export function byProduct(alerts, islandId) {
  const out = new Map();
  for (const a of alerts) {
    if (a.islandId !== islandId) continue;
    const key = String(a.productGuid);
    const list = out.get(key);
    if (list) list.push(a);
    else out.set(key, [a]);
  }
  return out;
}

// --- opt-ins ---

function readFlag(key) {
  try {
    return localStorage.getItem(key) === "on";
  } catch {
    // No storage (private browsing): the opt-ins stay off, which is the
    // documented default anyway.
    return false;
  }
}

function writeFlag(key, on) {
  try {
    localStorage.setItem(key, on ? "on" : "off");
  } catch {
    // Ignore: the choice just does not survive a reload.
  }
}

// prefs holds the two opt-ins. Both are off by default and live in this
// browser only - nothing about them is sent anywhere.
export const prefs = {
  notifications: readFlag(NOTIFY_KEY),
  sound: readFlag(SOUND_KEY),

  // notificationSupport reports what the browser offers: "none" when there
  // is no Notification API, "denied" when the user has blocked this page,
  // "ok" otherwise.
  notificationSupport() {
    if (typeof Notification === "undefined") return "none";
    if (Notification.permission === "denied") return "denied";
    return "ok";
  },

  // setNotifications turns browser notifications on or off. Permission is
  // requested here and nowhere else: asking on page load would be a prompt
  // nobody asked for. It returns the state that was actually reached.
  async setNotifications(on) {
    if (!on) {
      this.notifications = false;
      writeFlag(NOTIFY_KEY, false);
      return false;
    }
    if (typeof Notification === "undefined") return false;
    let permission = Notification.permission;
    if (permission === "default") {
      try {
        permission = await Notification.requestPermission();
      } catch {
        permission = "denied";
      }
    }
    this.notifications = permission === "granted";
    writeFlag(NOTIFY_KEY, this.notifications);
    return this.notifications;
  },

  // setSound turns the beep on or off. Enabling it is a click, which is the
  // user gesture browsers require before audio may start, so the audio
  // context is created here.
  setSound(on) {
    this.sound = !!on;
    writeFlag(SOUND_KEY, this.sound);
    if (this.sound) audioContext();
    return this.sound;
  },
};

// --- announcing ---

let audioCtx = null;

// audioContext creates (or resumes) the one context. Returns null when the
// browser has no Web Audio support.
function audioContext() {
  const Ctor = window.AudioContext || window.webkitAudioContext;
  if (!Ctor) return null;
  if (!audioCtx) {
    try {
      audioCtx = new Ctor();
    } catch {
      return null;
    }
  }
  if (audioCtx.state === "suspended") audioCtx.resume().catch(() => {});
  return audioCtx;
}

// beep plays a short two-tone chime. It is synthesised rather than played
// from a file, so the executable ships no audio data.
export function beep() {
  const ctx = audioContext();
  if (!ctx) return;
  const now = ctx.currentTime;
  const gain = ctx.createGain();
  gain.gain.setValueAtTime(0.0001, now);
  gain.gain.exponentialRampToValueAtTime(0.15, now + 0.02);
  gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.45);
  gain.connect(ctx.destination);

  for (const [frequency, at] of [[880, now], [660, now + 0.18]]) {
    const osc = ctx.createOscillator();
    osc.type = "sine";
    osc.frequency.setValueAtTime(frequency, at);
    osc.connect(gain);
    osc.start(at);
    osc.stop(at + 0.2);
  }
}

// notify shows a browser notification for one alert, if the user enabled
// them and the permission is still there.
function notify(alert) {
  if (typeof Notification === "undefined" || Notification.permission !== "granted") return;
  const body = `${alert.islandName} · ${ruleLabel(alert.rule)} · ${alert.detail}`;
  try {
    new Notification(`${i18n.t("notificationTitle")}: ${alert.productName}`, {
      body,
      tag: alertKey(alert),
    });
  } catch {
    // Some browsers refuse the constructor outside a service worker; a
    // missing notification is not worth breaking the page over.
  }
}

// announce is called for a warning that is new to this page. Cleared alerts
// never announce - a problem going away is not something to interrupt for -
// and neither does an info alert: an island that imports a good is not news.
export function announce(alert) {
  if (!isWarning(alert)) return;
  if (prefs.notifications) notify(alert);
  if (prefs.sound) beep();
}
