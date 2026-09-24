// sources.js -- "possible production sources" for a good an island needs but
// does not produce (KONZEPT.md section 2.4), shared by the goods table and
// the warnings page.
//
// The server picks, groups and sorts the islands; this module only asks for
// them when a row is opened, and shows them. Nothing here decides what a
// source is. And nothing here calls one a supplier: the game data has no
// trade routes and no deliveries, only each island's local balance.

import { i18n } from "./i18n.js";
import { api, ApiError } from "./api.js";

// createSources keeps track of which rows are open and what the server said
// for each. onChange is called whenever an open row has something new to
// show; the view re-renders then, and asks panel() for the content.
export function createSources(onChange) {
  // key -> { islandId, guid, dto, error, request }
  const open = new Map();
  let disposed = false;

  const keyOf = (islandId, guid) => `${islandId}|${guid}`;

  async function load(key) {
    const entry = open.get(key);
    if (!entry) return;
    const current = ++entry.request;
    try {
      const dto = await api.sources(entry.islandId, entry.guid);
      // Closed, reopened or asked again in the meantime: the newer answer
      // is the one that counts.
      if (disposed || open.get(key) !== entry || current !== entry.request) return;
      // Most snapshots do not move the complete tick on, and the answer is
      // the same as before. Re-rendering for it would rebuild the table ten
      // times a tick, and take the keyboard focus with it every time.
      if (!entry.error && JSON.stringify(dto) === JSON.stringify(entry.dto)) return;
      entry.dto = dto;
      entry.error = "";
    } catch (err) {
      if (disposed || open.get(key) !== entry || current !== entry.request) return;
      entry.error = err instanceof ApiError ? err.message : String(err);
    }
    onChange();
  }

  return {
    isOpen: (islandId, guid) => open.has(keyOf(islandId, guid)),

    toggle(islandId, guid) {
      const key = keyOf(islandId, guid);
      if (open.has(key)) {
        open.delete(key);
      } else {
        open.set(key, { islandId, guid, dto: null, error: "", request: 0 });
        load(key);
      }
      onChange();
    },

    // refresh asks again for every open row: after a snapshot, since the
    // server's complete tick may have moved on, and after a language switch,
    // since the session and product names come from the server.
    refresh() {
      for (const key of open.keys()) load(key);
    },

    // closeAllBut drops the open rows that are no longer listed, so a row
    // that disappears and comes back later does not come back open.
    closeAllBut(listed) {
      for (const key of [...open.keys()]) {
        if (!listed.has(key)) open.delete(key);
      }
    },

    keyOf,

    panel(islandId, guid) {
      const entry = open.get(keyOf(islandId, guid));
      return renderPanel(entry ?? { dto: null, error: "" });
    },

    dispose() {
      disposed = true;
      open.clear();
    },
  };
}

// toggleButton is the control that opens and closes a row's sources. It
// carries the tag it replaces ("import needed"), so the row looks the same
// as before; aria-expanded tells a screen reader it opens something. The
// button itself is invisible and only as large as a finger needs (see
// app.css); the tag inside it keeps its quiet size. title is the tooltip,
// which defaults to what the button does.
export function toggleButton(label, expanded, onClick, title = i18n.t("sourcesShow")) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "sources-toggle";
  const tag = document.createElement("span");
  tag.className = "import-tag";
  tag.textContent = `${label} ${expanded ? "▾" : "▸"}`;
  button.append(tag);
  button.title = title;
  button.setAttribute("aria-expanded", String(expanded));
  button.setAttribute("aria-label", `${label}: ${i18n.t("sourcesShow")}`);
  button.addEventListener("click", onClick);
  return button;
}

// detailRow wraps a panel in a table row spanning every column, which is what
// keeps it readable on a phone: it sits under its row instead of squeezing
// into one cell, and the phone layout already leaves a spanning cell alone.
export function detailRow(content, columns) {
  const tr = document.createElement("tr");
  tr.className = "sources-row";
  const td = document.createElement("td");
  td.colSpan = columns;
  td.append(content);
  tr.append(td);
  return tr;
}

function renderPanel({ dto, error }) {
  const box = document.createElement("div");
  box.className = "sources";

  const heading = document.createElement("p");
  heading.className = "sources-heading";
  heading.textContent = i18n.t("sourcesHeading");
  box.append(heading);

  if (error) {
    const p = document.createElement("p");
    p.className = "error-line";
    p.textContent = `${i18n.t("errorPrefix")} ${error}`;
    box.append(p);
  } else if (!dto) {
    box.append(muted(i18n.t("sourcesLoading")));
  } else if (!dto.ready) {
    // Right after connecting or loading a save: not known yet, which is not
    // the same as "none found".
    box.append(muted(i18n.t("sourcesPending")));
  } else if (dto.groups.length === 0) {
    box.append(muted(i18n.t("sourcesNone")));
  } else {
    for (const group of dto.groups) {
      const section = document.createElement("div");
      section.className = "sources-group";
      const session = document.createElement("p");
      session.className = "sources-session";
      session.textContent = group.sessionName;
      const list = document.createElement("ul");
      for (const source of group.sources) {
        const li = document.createElement("li");
        const link = document.createElement("a");
        link.href = `#/island/${encodeURIComponent(source.id)}`;
        link.textContent = source.name;
        const balance = document.createElement("span");
        balance.className = "sources-balance delta-positive";
        balance.textContent = i18n.t("sourcesBalance", { value: `+${formatBalance(source.delta)}` });
        li.append(link, " ", balance);
        list.append(li);
      }
      section.append(session, list);
      box.append(section);
    }
  }

  const note = muted(i18n.t("sourcesNote"));
  note.classList.add("sources-note");
  box.append(note);
  return box;
}

function muted(text) {
  const p = document.createElement("p");
  p.className = "muted";
  p.textContent = text;
  return p;
}

// formatBalance writes a balance with one decimal, like the goods table. A
// source's balance is positive by definition, and 0.03 written as "+0.0"
// would contradict that - so below 0.1 it gets a second decimal.
function formatBalance(value) {
  const locale = i18n.lang === "de" ? "de-DE" : "en-US";
  const digits = value < 0.1 ? 2 : 1;
  return new Intl.NumberFormat(locale, { minimumFractionDigits: digits, maximumFractionDigits: digits }).format(value);
}
