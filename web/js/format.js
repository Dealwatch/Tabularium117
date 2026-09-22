// format.js -- the small formatting helpers more than one view needs.

import { i18n } from "./i18n.js";

// formatAge renders a duration in milliseconds as "12 s", "5 min" or "2 h".
// It is deliberately coarse: the status bar and the warning list both answer
// "how long ago", not "exactly when".
export function formatAge(ms) {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return `${s} s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} min`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h} h`;
  return `${Math.floor(h / 24)} d`;
}

// formatTime renders an ISO timestamp as a local time of day, or an empty
// string when there is none. The data can be a replayed recording, so the
// date matters too - it is shown in the title attribute by the callers.
export function formatTime(iso) {
  if (!iso) return "";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleTimeString(i18n.lang === "de" ? "de-DE" : "en-US");
}

// formatDateTime is formatTime with the date, for tooltips.
export function formatDateTime(iso) {
  if (!iso) return "";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleString(i18n.lang === "de" ? "de-DE" : "en-US");
}
