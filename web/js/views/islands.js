// views/islands.js -- the product table for one island (KONZEPT.md section 2.1/2.2).

import { islandNavigation } from "./navigation.js";
import { i18n } from "../i18n.js";
import { api } from "../api.js";
import { byProduct, ruleLabel } from "../alerts.js";

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
  let disposed = false;
  let request = 0;

  container.append(islandNavigation(islandId, store, "goods"));
  const wrapper = document.createElement("div");
  wrapper.className = "data-panel";
  const summaryLine = document.createElement("p");
  summaryLine.className = "muted";

  const toolbar = document.createElement("div");
  toolbar.className = "toolbar";
  const search_input = document.createElement("input");
  search_input.type = "search";
  search_input.placeholder = i18n.t("searchPlaceholder");
  search_input.setAttribute("data-i18n-placeholder", "searchPlaceholder");
  search_input.setAttribute("aria-label", i18n.t("searchPlaceholder"));
  const filters = document.createElement("div"); filters.className = "filter-buttons";
  filters.setAttribute("role", "group"); filters.setAttribute("aria-label", i18n.t("filtersLabel"));
  const filterButtons = [];
  for (const [value, key] of [["all", "allGoods"], ["deficits", "onlyDeficits"], ["warnings", "onlyWarnings"]]) {
    const button = document.createElement("button"); button.type = "button"; button.textContent = i18n.t(key);
    button.setAttribute("aria-pressed", String(value === filter));
    button.addEventListener("click", () => { filter = value; filterButtons.forEach(([b, v]) => b.setAttribute("aria-pressed", String(v === filter))); renderTable(); });
    filterButtons.push([button, value]); filters.append(button);
  }
  toolbar.append(search_input, filters);

  const table = document.createElement("table");
  const thead = document.createElement("thead");
  const tbody = document.createElement("tbody");
  table.append(thead, tbody);

  // A phone screen is narrower than this table; it scrolls inside its own
  // box instead of dragging the whole page sideways (task T7.4).
  const tableBox = document.createElement("div");
  tableBox.className = "table-scroll";
  tableBox.tabIndex = 0;
  tableBox.setAttribute("role", "region");
  tableBox.setAttribute("aria-label", i18n.t("goods"));
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
      if (sortKey === col.key) th.setAttribute("aria-sort", sortAsc ? "ascending" : "descending");
      const btn = document.createElement("button");
      btn.type = "button";
      btn.textContent = i18n.t(col.label) + (sortKey === col.key ? (sortAsc ? " ↑" : " ↓") : "");
      btn.addEventListener("click", () => {
        if (sortKey === col.key) sortAsc = !sortAsc;
        else { sortKey = col.key; sortAsc = true; }
        renderTable();
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
    const alertCount = store.alerts.filter((a) => a.islandId === islandId).length;
    summaryLine.textContent = `${island.name} · ${island.sessionName} · `
      + `${island.products} ${i18n.t("products")} · ${island.deficits} ${i18n.t("deficits")}`
      + (alertCount > 0 ? ` · ${alertCount} ${i18n.t("alertsHeading")}` : "");

    // Warnings are per (island, product); a row carries a marker when the
    // rule engine has one open for it.
    const alerted = byProduct(store.alerts, islandId);

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
      const historyLink = document.createElement("a");
      historyLink.className = "product-link";
      historyLink.href = `#/island/${encodeURIComponent(islandId)}/product/${encodeURIComponent(p.guid)}`;
      historyLink.setAttribute("aria-label", `${p.name}: ${i18n.t("openHistory")}`);
      while (nameTd.firstChild) historyLink.append(nameTd.firstChild);
      const hint = document.createElement("span"); hint.className = "history-hint"; hint.textContent = " ↗"; hint.setAttribute("aria-hidden", "true");
      historyLink.append(hint); historyLink.title = i18n.t("openHistory"); nameTd.append(historyLink);
      const genTd = document.createElement("td");
      genTd.className = "num";
      genTd.textContent = fmt1.format(p.generation);
      const consTd = document.createElement("td");
      consTd.className = "num";
      consTd.textContent = fmt1.format(p.consumption);
      const deltaTd = document.createElement("td");
      deltaTd.className = `num ${deltaClass(p.delta)}`;
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
