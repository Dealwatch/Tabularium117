// views/help.js -- the #/help page (KONZEPT.md section 8), the two
// notification opt-ins and the "keep the screen on" opt-in.
//
// The settings live here rather than in the phone panel because this page is
// the one a phone can reach: the panel is for the PC only.

import { i18n } from "../i18n.js";
import { buildPipeHelp } from "../pipe-help.js";
import { prefs } from "../alerts.js";
import { wakeLock } from "../wakelock.js";
import { api } from "../api.js";

// buildToggle returns a labelled checkbox. onChange reports the state that
// was actually reached - enabling notifications can be refused by the
// browser - so the checkbox follows reality, not the click.
function buildToggle(labelKey, checked, disabled, onChange) {
  const label = document.createElement("label");
  label.className = "setting";
  const input = document.createElement("input");
  input.type = "checkbox";
  input.checked = checked;
  input.disabled = disabled;
  const text = document.createElement("span");
  text.textContent = i18n.t(labelKey);
  label.append(input, text);

  input.addEventListener("change", async () => {
    const reached = await onChange(input.checked);
    input.checked = !!reached;
  });
  return label;
}

function buildSettings() {
  const section = document.createElement("div");
  section.className = "panel";

  const heading = document.createElement("h3");
  heading.textContent = i18n.t("settingsHeading");

  const support = prefs.notificationSupport();
  section.append(
    heading,
    buildToggle("settingsNotify", prefs.notifications, support !== "ok",
      (on) => prefs.setNotifications(on)),
    buildToggle("settingsSound", prefs.sound, false,
      (on) => prefs.setSound(on)),
  );

  if (support !== "ok") {
    const note = document.createElement("p");
    note.className = "muted";
    // "none" over a plain-HTTP address is not a browser without the API:
    // notifications are a secure context feature, so the API is not there
    // for a phone on the LAN address (wakelock.js, secure()).
    const insecure = support === "none" && !wakeLock.secure();
    note.textContent = support === "denied"
      ? i18n.t("settingsNotifyDenied")
      : insecure
        ? i18n.t("settingsNotifyInsecure")
        : i18n.t("settingsNotifyUnsupported");
    section.append(note);
  }

  const explain = document.createElement("p");
  explain.className = "muted";
  explain.textContent = i18n.t("settingsExplain");
  section.append(explain);
  return section;
}

// buildScreenSettings is the phone opt-in: a second screen that
// turns itself off after a minute is not a second screen.
function buildScreenSettings() {
  const section = document.createElement("div");
  section.className = "panel";

  const heading = document.createElement("h3");
  heading.textContent = i18n.t("screenHeading");
  section.append(
    heading,
    buildToggle("wakeLock", wakeLock.enabled, !wakeLock.supported(),
      (on) => wakeLock.setEnabled(on)),
  );

  const note = document.createElement("p");
  note.className = "muted";
  note.textContent = wakeLock.supported()
    ? i18n.t("wakeLockExplain")
    : wakeLock.secure() ? i18n.t("wakeLockUnsupported") : i18n.t("wakeLockInsecure");
  section.append(note);
  return section;
}

export function renderHelp(container) {
  const heading = document.createElement("h2");
  heading.textContent = i18n.t("helpHeading");

  const panel = document.createElement("div");
  panel.className = "panel help-panel";
  panel.append(buildPipeHelp());

  const numbers = document.createElement("p");
  numbers.textContent = i18n.t("helpNumbers");

  const unknown = document.createElement("p");
  unknown.textContent = i18n.t("helpUnknownGuid");

  const alertsExplain = document.createElement("p");
  alertsExplain.textContent = i18n.t("alertsExplain");

  const version = document.createElement("p");
  version.className = "muted";
  api.status()
    .then((status) => {
      version.textContent = i18n.t("helpVersion", { version: status.version });
    })
    .catch(() => {
      // No version to show if the status request fails; the rest of the
      // help page still works.
    });

  panel.append(numbers, unknown, alertsExplain, version);
  container.append(heading, panel, buildSettings(), buildScreenSettings());

  const onLang = () => {
    container.replaceChildren();
    const cleanup = renderHelp(container);
    // renderHelp re-attaches its own listener; nothing further to do here.
    return cleanup;
  };
  document.addEventListener("tabularium-lang-changed", onLang, { once: true });

  return () => document.removeEventListener("tabularium-lang-changed", onLang);
}
