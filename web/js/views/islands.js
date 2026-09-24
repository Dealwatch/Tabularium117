// views/islands.js -- the product table for one island (KONZEPT.md section 2.1/2.2).

import { i18n } from "../i18n.js";
import { api } from "../api.js";
import { byProduct, isWarning, ruleLabel } from "../alerts.js";
import { renderIslandHeader } from "./island-header.js";

const numberFormatCache = new Map();

function numberFormat(fractionDigits) {
  const locale = i18n.lang === "de" ? "de-DE" : "en-US";
  const key = `${locale}:${fractionDigits}`;
  if (!numberFormatCache.has(key)) {
    numberFormatCache.set(
      key,
      new Intl.NumberFormat(locale, { minimumFractionDigits: fractionDigits, maximumFractionDigits: fractionDigits }),
    );
  }
  return numberFormatCache.get(key);
}

const columns = [
  { key: "name", label: "colName", numeric: false },
  { key: "generation", label: "colGeneration", numeric: true },
  { key: "consumption", label: "colConsumption", numeric: true },
  { key: "delta", label: "colDelta", numeric: true },
  { key: "buildings", label: "colBuildings", numeric: true },
];

// renderIslandDetail fetches the island's products and mounts the table. It
// returns an unmount function that stops listening for live updates.
export async function renderIslandDetail(container, islandId, store) {
  let sortKey = "delta";
  let sortAsc = true; // deficits (most negative delta) first by default
  let search = "";
  let filter = "all";
  // A request that was overtaken must not paint over the newer one: switching
  // island quickly, or a snapshot arriving during a reload, can land two
  // answers out of order, and the older one would win by arriving last.
  let disposed = false;
  let request = 0;

  // The island's name, the island switch and the tab to the efficiency view
  // all live in the shared header now, so this view only owns its table.
  const header = renderIslandHeader(container, islandId, store, "products");

  const wrapper = document.createElement("div");
  const summaryLine = document.createElement("p");
  summaryLine.className = "muted summary-line";

  const toolbar = document.createElement("div");
  toolbar.className = "toolbar";
  const search_input = document.createElement("input");
  search_input.type = "search";
  search_input.placeholder = i18n.t("searchPlaceholder");
  search_input.setAttribute("data-i18n-placeholder", "searchPlaceholder");
  search_input.setAttribute("aria-label", i18n.t("searchPlaceholder"));

  // Search answers "where is X"; the filters answer "what needs me now",
  // which is the question a deficit or a warning raises. They intersect.
  const filters = document.createElement("div");
  filters.className = "filter-buttons";
  filters.setAttribute("role", "group");
  filters.setAttribute("aria-label", i18n.t("filtersLabel"));
  const filterButtons = [];
  for (const [value, key] of [["all", "allGoods"], ["deficits", "onlyDeficits"], ["warnings", "onlyWarnings"]]) {
    const button = document.createElement("button");
    button.type = "button";
    button.textContent = i18n.t(key);
    button.setAttribute("aria-pressed", String(value === filter));
    button.addEventListener("click", () => {
      filter = value;
      for (const [other, otherValue] of filterButtons) {
        other.setAttribute("aria-pressed", String(otherValue === filter));
      }
      renderTable();
    });
    filterButtons.push([button, value]);
    filters.append(button);
  }
  toolbar.append(search_input, filters);

  const table = document.createElement("table");
  const thead = document.createElement("thead");
  const tbody = document.createElement("tbody");
  table.append(thead, tbody);

  // A phone screen is narrower than this table; it scrolls inside its own
  // box instead of dragging the whole page sideways.
  const tableBox = document.createElement("div");
  tableBox.className = "table-scroll";
  tableBox.append(table);
  wrapper.append(summaryLine, toolbar, tableBox);
  container.append(wrapper);

  let productsDTO = null;

  function headerRow() {
    const tr = document.createElement("tr");
    for (const col of columns) {
      const th = document.createElement("th");
      if (col.numeric) th.className = "num";
      th.scope = "col";
      // A screen reader has no arrow to look at, so the sorted column says
      // so itself.
      if (sortKey === col.key) th.setAttribute("aria-sort", sortAsc ? "ascending" : "descending");
      const btn = document.createElement("button");
      btn.type = "button";
      btn.textContent = i18n.t(col.label) + (sortKey === col.key ? (sortAsc ? " ↑" : " ↓") : "");
      btn.addEventListener("click", () => {
        if (sortKey === col.key) sortAsc = !sortAsc;
        else { sortKey = col.key; sortAsc = true; }
        renderTable();
        // renderTable replaces the header, and with it the button that was
        // just clicked: without this, sorting by keyboard drops the focus
        // back to the top of the page on every press.
        thead.querySelectorAll("button")[columns.indexOf(col)]?.focus();
      });
      th.append(btn);
      tr.append(th);
    }
    thead.replaceChildren(tr);
  }

  function deltaClass(delta) {
    if (delta < 0) return "delta-negative";
    if (delta > 0) return "delta-positive";
    return "";
  }

  function renderTable() {
    if (!productsDTO) return;
    headerRow();
    const island = productsDTO.island;
    const alertCount = store.alerts.filter((a) => a.islandId === islandId && isWarning(a)).length;
    // The name and the session are in the header; repeating them here only
    // made the line long enough to be skipped. What is left are the counts.
    header.update(island);
    summaryLine.textContent = `${island.products} ${i18n.t("products")} · `
      + `${island.deficits} ${i18n.t("deficits")}`
      + (alertCount > 0 ? ` · ${alertCount} ${i18n.t("alertsHeading")}` : "");

    // Warnings are per (island, product); a row carries a marker when the
    // rule engine has one open for it. A good the island does not produce
    // itself is info, not a warning: it gets a quiet tag of its own and stays
    // out of the warnings filter.
    const alerted = byProduct(store.alerts.filter(isWarning), islandId);
    const imports = byProduct(store.alerts.filter((a) => !isWarning(a)), islandId);

    let rows = productsDTO.products;
    if (filter === "deficits") rows = rows.filter((p) => p.delta < 0);
    if (filter === "warnings") rows = rows.filter((p) => alerted.has(String(p.guid)));
    if (search.trim()) {
      const needle = search.trim().toLowerCase();
      rows = rows.filter((p) => p.name.toLowerCase().includes(needle));
    }
    rows = [...rows].sort((a, b) => {
      let cmp;
      if (sortKey === "name") cmp = a.name.localeCompare(b.name);
      else cmp = a[sortKey] - b[sortKey];
      return sortAsc ? cmp : -cmp;
    });

    tbody.replaceChildren();
    if (rows.length === 0) {
      const tr = document.createElement("tr");
      const td = document.createElement("td");
      td.colSpan = columns.length;
      if (productsDTO.products.length === 0) {
        // An empty island right after a savegame is loaded is not an island
        // without production - it is one whose first statistics tick has not
        // arrived yet (docs/protocol.md). Saying "no production" there would
        // be wrong for up to two minutes.
        td.textContent = store.status?.warmingUp ? i18n.t("warmingUp") : i18n.t("noProduction");
        if (store.status?.warmingUp) tr.className = "no-data";
      } else {
        // The island has goods, the filter or the search has none. Saying so
        // is the difference between "nothing here" and a table that looks
        // broken.
        td.textContent = i18n.t("noMatches");
      }
      tr.append(td);
      tbody.append(tr);
      return;
    }
    const fmt1 = numberFormat(1);
    const fmt0 = numberFormat(0);
    for (const p of rows) {
      const tr = document.createElement("tr");
      tr.className = "product-row";

      const nameTd = document.createElement("td");
      const warnings = alerted.get(String(p.guid));
      if (warnings) {
        const marker = document.createElement("span");
        marker.className = "alert-marker";
        marker.textContent = "\u26A0";
        marker.title = `${i18n.t("alertMarker")}: ${warnings.map((a) => `${ruleLabel(a.rule)} (${a.detail})`).join(", ")}`;
        marker.setAttribute("aria-label", i18n.t("alertMarker"));
        nameTd.append(marker, document.createTextNode(` ${p.name}`));
      } else {
        nameTd.textContent = p.name;
      }
      // The way into the history is a link, not a row that listens for
      // clicks and for Enter. A link is what a keyboard, a screen reader and
      // "open in a new tab" all already know how to use; the row only
      // highlights.
      const historyLink = document.createElement("a");
      historyLink.className = "product-link";
      historyLink.href = `#/island/${encodeURIComponent(islandId)}/product/${encodeURIComponent(p.guid)}`;
      historyLink.title = i18n.t("openHistory");
      historyLink.setAttribute("aria-label", `${p.name}: ${i18n.t("openHistory")}`);
      while (nameTd.firstChild) historyLink.append(nameTd.firstChild);
      const hint = document.createElement("span");
      hint.className = "history-hint";
      hint.textContent = "↗";
      hint.setAttribute("aria-hidden", "true");
      historyLink.append(hint);
      nameTd.append(historyLink);
      // Outside the link: the tag explains the row, it is not a way into the
      // history.
      if (imports.has(String(p.guid))) {
        const tag = document.createElement("span");
        tag.className = "import-tag";
        tag.textContent = i18n.t("importTag");
        tag.title = i18n.t("importTagTitle");
        nameTd.append(" ", tag);
      }

      const genTd = document.createElement("td");
      genTd.className = "num";
      genTd.textContent = fmt1.format(p.generation);
      const consTd = document.createElement("td");
      consTd.className = "num";
      consTd.textContent = fmt1.format(p.consumption);
      const deltaTd = document.createElement("td");
      deltaTd.className = `num ${deltaClass(p.delta)}`;
      // A column holding -4.0 and 2.0 reads better as -4.0 and +2.0: the
      // sign is the whole message of this column.
      deltaTd.textContent = (p.delta > 0 ? "+" : "") + fmt1.format(p.delta);
      const buildingsTd = document.createElement("td");
      buildingsTd.className = "num";
      buildingsTd.textContent = fmt0.format(p.buildings);

      tr.append(nameTd, genTd, consTd, deltaTd, buildingsTd);
      tbody.append(tr);
    }
  }

  search_input.addEventListener("input", () => {
    search = search_input.value;
    renderTable();
  });

  async function load() {
    const current = ++request;
    const dto = await api.products(islandId);
    if (disposed || current !== request) return;
    productsDTO = dto;
    renderTable();
  }

  await load();

  const onSnapshot = (e) => {
    if (e.detail && e.detail.id === islandId) {
      load().catch((err) => console.error("tabularium117: cannot refresh products", err));
    }
  };
  document.addEventListener("tabularium-snapshot", onSnapshot);

  const onLang = () => renderTable();
  document.addEventListener("tabularium-lang-changed", onLang);

  const onAlerts = () => renderTable();
  document.addEventListener("tabularium-alerts-changed", onAlerts);

  return () => {
    disposed = true;
    document.removeEventListener("tabularium-snapshot", onSnapshot);
    document.removeEventListener("tabularium-lang-changed", onLang);
    document.removeEventListener("tabularium-alerts-changed", onAlerts);
  };
}
